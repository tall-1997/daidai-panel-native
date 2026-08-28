package com.daidai.daidai_app

import java.time.ZonedDateTime
import java.time.Instant
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.Executors
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

/** Lightweight scheduler whose lifetime is bounded by the Kotlin fallback HTTP server. */
internal class AndroidFallbackCronScheduler(private val store: LocalPanelStore) : AutoCloseable {
    private val started = AtomicBoolean(false)
    private val ticker: ScheduledExecutorService = Executors.newSingleThreadScheduledExecutor { runnable ->
        Thread(runnable, "android-fallback-cron").apply { isDaemon = true }
    }
    private val workers = ThreadPoolExecutor(
        2,
        2,
        0L,
        TimeUnit.MILLISECONDS,
        ArrayBlockingQueue(32),
        { runnable -> Thread(runnable, "android-fallback-task").apply { isDaemon = true } },
        ThreadPoolExecutor.AbortPolicy(),
    )
    private val tickGate = CronTickGate()
    private var lastResourceLimited = false
    private var lastPurgeDay: java.time.LocalDate? = null

    fun start() {
        if (started.compareAndSet(false, true)) {
            tickSafely()
            val initialDelay = 1_000 - (System.currentTimeMillis() % 1_000)
            ticker.scheduleAtFixedRate(::tickSafely, initialDelay, 1_000, TimeUnit.MILLISECONDS)
        }
    }

    private fun tickSafely() {
        try {
            val now = ZonedDateTime.now()
            if (!tickGate.claimSecond(now.toEpochSecond())) return
            val unprocessedMinutes = tickGate.claimUnprocessedMinutes(now.toEpochSecond() / 60)
            if (tickGate.claimMaintenanceMinute(now.toEpochSecond() / 60)) {
                runMinuteMaintenance(now)
            }
            store.enabledScheduledTasks()
                .filter { task -> task.cronExpression.lineSequence().map(String::trim).filter(String::isNotEmpty).any { CronExpression.matchesTick(it, now, unprocessedMinutes) } }
                .forEach { task -> submitTask(task.id) }
            store.scheduledTaskStops()
                .filter { task -> task.stopSchedule.lineSequence().map(String::trim).filter(String::isNotEmpty).any { CronExpression.matchesTick(it, now, unprocessedMinutes) } }
                .forEach { store.stopScheduledTask(it.id) }
        } catch (error: Exception) {
            store.appLog("Cron", error.message ?: error.javaClass.simpleName)
        }
    }

    private fun runMinuteMaintenance(now: ZonedDateTime) {
        try {
            val today = now.toLocalDate()
            if (lastPurgeDay != today) {
                lastPurgeDay = today
                runCatching { store.purgeExpiredRecords() }
            }
            val guarantee = store.evaluateResourceGuarantee()
            val limited = guarantee.state == "resource_limited"
            if (limited) {
                if (!lastResourceLimited) {
                    lastResourceLimited = true
                    store.notifyLowPrioritySkipped(guarantee.reasonCode)
                }
                store.appLog("Cron", "resource_limited 跳过备份: ${guarantee.reasonCode}")
            } else {
                lastResourceLimited = false
                runBackupAsync(now)
            }
        } catch (error: Exception) {
            store.appLog("Cron", error.message ?: error.javaClass.simpleName)
        }
    }

    private fun submitTask(taskId: Long) {
        if (!started.get()) return
        when (store.enqueueScheduledTask(taskId)) {
            LocalPanelStore.EnqueueTaskResult.ACCEPTED -> Unit
            LocalPanelStore.EnqueueTaskResult.ALREADY_RUNNING -> Unit
            LocalPanelStore.EnqueueTaskResult.NOT_FOUND -> {
                store.appLog("Cron", "Task $taskId no longer exists")
            }
            LocalPanelStore.EnqueueTaskResult.MAINTENANCE -> Unit
            LocalPanelStore.EnqueueTaskResult.QUEUE_FULL -> {
                store.appLog("Cron", "Task $taskId deferred: fallback queue is full")
                if (started.get()) runCatching { ticker.schedule({ submitTask(taskId) }, 10, TimeUnit.SECONDS) }
            }
        }
    }

    private fun runBackupAsync(now: ZonedDateTime) {
        try {
            workers.execute {
                if (!store.runScheduledBackupIfDue(now)) deferBackup(now)
            }
        } catch (_: RejectedExecutionException) {
            store.appLog("Cron", "backup deferred: queue is full")
            deferBackup(now)
        }
    }

    private fun deferBackup(now: ZonedDateTime) {
        val scheduledMinute = now.toEpochSecond() / 60
        if (started.get()) runCatching {
            ticker.schedule({
                if (ZonedDateTime.now().toEpochSecond() / 60 == scheduledMinute) runBackupAsync(now)
            }, 10, TimeUnit.SECONDS)
        }
    }

    fun tickNow() {
        start()
        tickSafely()
    }

    fun isIdle(): Boolean = workers.activeCount == 0 && store.activeTaskIds().isEmpty()

    override fun close() {
        started.set(false)
        ticker.shutdownNow()
        workers.shutdownNow()
    }
}

internal class MaintenanceGate {
    private val monitor = Object()
    private var maintenance = false
    private var activeTasks = 0

    fun tryEnterTask(): Boolean = synchronized(monitor) {
        if (maintenance) return@synchronized false
        activeTasks++
        true
    }

    fun leaveTask() = synchronized(monitor) {
        check(activeTasks > 0) { "maintenance gate task count underflow" }
        activeTasks--
        if (activeTasks == 0) monitor.notifyAll()
    }

    fun beginMaintenance(timeoutMillis: Long): Boolean = synchronized(monitor) {
        if (maintenance) return@synchronized false
        maintenance = true
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMillis.coerceAtLeast(0))
        while (activeTasks > 0) {
            val remainingNanos = deadline - System.nanoTime()
            if (remainingNanos <= 0) {
                maintenance = false
                monitor.notifyAll()
                return@synchronized false
            }
            try {
                TimeUnit.NANOSECONDS.timedWait(monitor, remainingNanos)
            } catch (_: InterruptedException) {
                Thread.currentThread().interrupt()
                maintenance = false
                monitor.notifyAll()
                return@synchronized false
            }
        }
        true
    }

    fun endMaintenance() = synchronized(monitor) {
        maintenance = false
        monitor.notifyAll()
    }

    fun isMaintenanceActive(): Boolean = synchronized(monitor) { maintenance }
    fun activeTaskCount(): Int = synchronized(monitor) { activeTasks }
}

internal class CronTickGate {
    private var lastClaimedSecond = Long.MIN_VALUE
    private var lastMaintenanceMinute = Long.MIN_VALUE
    private var lastProcessedMinute = Long.MIN_VALUE

    @Synchronized
    fun claimSecond(epochSecond: Long): Boolean {
        if (epochSecond == lastClaimedSecond) return false
        lastClaimedSecond = epochSecond
        return true
    }

    @Synchronized
    fun claimMaintenanceMinute(epochMinute: Long): Boolean {
        if (epochMinute == lastMaintenanceMinute) return false
        lastMaintenanceMinute = epochMinute
        return true
    }

    @Synchronized
    fun claimUnprocessedMinutes(epochMinute: Long): LongRange {
        val first = if (lastProcessedMinute == Long.MIN_VALUE || epochMinute <= lastProcessedMinute) epochMinute else lastProcessedMinute + 1
        if (epochMinute < lastProcessedMinute) lastProcessedMinute = epochMinute
        if (epochMinute == lastProcessedMinute) return LongRange.EMPTY
        lastProcessedMinute = epochMinute
        return first..epochMinute
    }
}

/** Five/six-field cron subset: wildcard, step, number, inclusive range, and lists. */
internal object CronExpression {
    fun isValid(expression: String): Boolean {
        val fields = expression.trim().split(Regex("\\s+"))
        val ranges = ranges(fields.size) ?: return false
        return fields.indices.all { fieldIndex ->
            val field = fields[fieldIndex]
            field.isNotBlank() && !field.startsWith(',') && !field.endsWith(',') && ",," !in field &&
                field.split(',').all { parseSelection(it, ranges[fieldIndex]) != null }
        }
    }

    fun matches(expression: String, time: ZonedDateTime): Boolean {
        val fields = expression.trim().split(Regex("\\s+"))
        val ranges = ranges(fields.size) ?: return false
        if (!isValid(expression)) return false
        if (fields.size == 5 && time.second != 0) return false
        val values = if (fields.size == 6) {
            intArrayOf(time.second, time.minute, time.hour, time.dayOfMonth, time.monthValue, time.dayOfWeek.value % 7)
        } else {
            intArrayOf(time.minute, time.hour, time.dayOfMonth, time.monthValue, time.dayOfWeek.value % 7)
        }
        return fields.indices.all { matchesField(fields[it], values[it], ranges[it], it == fields.lastIndex) }
    }

    fun matchesTick(expression: String, time: ZonedDateTime, unprocessedMinutes: LongRange): Boolean {
        val fieldCount = expression.trim().split(Regex("\\s+")).size
        if (fieldCount == 6) return matches(expression, time)
        if (fieldCount != 5) return false
        return unprocessedMinutes.any { epochMinute ->
            matches(expression, ZonedDateTime.ofInstant(Instant.ofEpochSecond(epochMinute * 60), time.zone))
        }
    }

    private fun ranges(fieldCount: Int): Array<IntRange>? = when (fieldCount) {
        5 -> arrayOf(0..59, 0..23, 1..31, 1..12, 0..7)
        6 -> arrayOf(0..59, 0..59, 0..23, 1..31, 1..12, 0..7)
        else -> null
    }

    private fun matchesField(field: String, value: Int, range: IntRange, sundayAlias: Boolean): Boolean =
        field.split(',').any { part ->
            val (selected, step) = parseSelection(part, range) ?: return@any false
            val normalized = if (sundayAlias && value == 0 && selected.last == 7) 7 else value
            normalized in selected && (normalized - selected.first) % step == 0
        }

    private fun parseSelection(part: String, range: IntRange): Pair<IntRange, Int>? {
        val pieces = part.split('/')
        if (pieces.size !in 1..2) return null
        val step = if (pieces.size == 2) pieces[1].toIntOrNull()?.takeIf { it > 0 } ?: return null else 1
        val base = pieces[0]
        val selected = when {
            base == "*" -> range
            '-' in base -> {
                val bounds = base.split('-')
                if (bounds.size != 2) return null
                val start = bounds[0].toIntOrNull() ?: return null
                val end = bounds[1].toIntOrNull() ?: return null
                start..end
            }
            else -> {
                val number = base.toIntOrNull() ?: return null
                number..number
            }
        }
        if (selected.first !in range || selected.last !in range || selected.first > selected.last) return null
        return selected to step
    }
}
