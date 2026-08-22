package com.daidai.daidai_app

import java.time.ZonedDateTime
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
    @Volatile private var lastCheckedMinute = Long.MIN_VALUE

    fun start() {
        if (started.compareAndSet(false, true)) {
            tickSafely()
            val initialDelay = 60 - (System.currentTimeMillis() / 1000 % 60)
            ticker.scheduleAtFixedRate(::tickSafely, initialDelay, 60, TimeUnit.SECONDS)
        }
    }

    private fun tickSafely() {
        try {
            val now = ZonedDateTime.now()
            val minute = now.toEpochSecond() / 60
            if (minute == lastCheckedMinute) return
            lastCheckedMinute = minute
            store.runScheduledBackupIfDue(now)
            store.enabledScheduledTasks()
                .filter { task -> task.cronExpression.lineSequence().map(String::trim).filter(String::isNotEmpty).any { CronExpression.matches(it, now) } }
                .forEach { task ->
                    try {
                        workers.execute { store.executeTaskAndSave(task.id) }
                    } catch (_: RejectedExecutionException) {
                        store.appLog("Cron", "Task ${task.id} rejected: fallback queue is full")
                    }
                }
            store.scheduledTaskStops()
                .filter { task -> task.stopSchedule.lineSequence().map(String::trim).filter(String::isNotEmpty).any { CronExpression.matches(it, now) } }
                .forEach { store.stopScheduledTask(it.id) }
        } catch (error: Exception) {
            store.appLog("Cron", error.message ?: error.javaClass.simpleName)
        }
    }

    override fun close() {
        started.set(false)
        ticker.shutdownNow()
        workers.shutdownNow()
    }
}

/** Standard five-field cron subset: wildcard, step, number, inclusive range, and lists. */
internal object CronExpression {
    fun matches(expression: String, time: ZonedDateTime): Boolean {
        val fields = expression.trim().split(Regex("\\s+"))
        if (fields.size != 5) return false
        val values = intArrayOf(time.minute, time.hour, time.dayOfMonth, time.monthValue, time.dayOfWeek.value % 7)
        val ranges = arrayOf(0..59, 0..23, 1..31, 1..12, 0..7)
        return fields.indices.all { matchesField(fields[it], values[it], ranges[it], it == 4) }
    }

    private fun matchesField(field: String, value: Int, range: IntRange, sundayAlias: Boolean): Boolean =
        field.split(',').any { part ->
            val pieces = part.split('/', limit = 2)
            val step = if (pieces.size == 2) pieces[1].toIntOrNull()?.takeIf { it > 0 } ?: return@any false else 1
            val base = pieces[0]
            val selected = when {
                base == "*" -> range
                '-' in base -> {
                    val bounds = base.split('-', limit = 2).map { it.toIntOrNull() }
                    if (bounds.any { it == null }) return@any false
                    bounds[0]!!..bounds[1]!!
                }
                else -> {
                    val number = base.toIntOrNull() ?: return@any false
                    number..number
                }
            }
            if (selected.first !in range || selected.last !in range || selected.first > selected.last) return@any false
            val normalized = if (sundayAlias && value == 0 && selected.last == 7) 7 else value
            normalized in selected && (normalized - selected.first) % step == 0
        }
}
