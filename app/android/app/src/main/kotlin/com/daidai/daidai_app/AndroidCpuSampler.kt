package com.daidai.daidai_app

import java.io.File

/**
 * Android 10 起普通应用读不到 /proc/stat。采集按真实占用、频率占比、
 * 负载、本进程占用依次回退，避免主页把系统 CPU 固定显示成 0%。
 */
internal object AndroidCpuSampler {
    fun parseProcStat(text: String): Pair<Long, Long>? {
        val line = text.lineSequence().firstOrNull { it.startsWith("cpu ") } ?: return null
        val values = line.trim().split(Regex("\\s+")).drop(1).mapNotNull(String::toLongOrNull)
        if (values.size < 4) return null
        val idle = values[3] + values.getOrElse(4) { 0L }
        return idle to values.sum()
    }

    fun usageFromProcSamples(first: Pair<Long, Long>, second: Pair<Long, Long>): Double? {
        val total = second.second - first.second
        if (total <= 0L) return null
        val idle = (second.first - first.first).coerceAtLeast(0L)
        return ((total - idle).toDouble() * 100.0 / total).coerceIn(0.0, 100.0)
    }

    fun cpufreqPercent(currents: List<Long>, maxima: List<Long>): Double? {
        if (currents.isEmpty() || currents.size != maxima.size) return null
        var sum = 0.0
        var count = 0
        for (index in currents.indices) {
            val max = maxima[index]
            if (max <= 0L) continue
            val current = currents[index].coerceIn(0L, max)
            sum += current.toDouble() / max
            count += 1
        }
        if (count == 0) return null
        return (sum / count * 100.0).coerceIn(0.0, 100.0)
    }

    fun loadAveragePercent(text: String, cpuCount: Int): Double? {
        if (cpuCount <= 0) return null
        val load = text.trim().split(Regex("\\s+")).firstOrNull()?.toDoubleOrNull() ?: return null
        if (load < 0.0) return null
        return (load / cpuCount * 100.0).coerceIn(0.0, 100.0)
    }

    fun processUsagePercent(cpuDeltaMs: Long, wallDeltaMs: Long, cpuCount: Int): Double? {
        if (cpuDeltaMs < 0L || wallDeltaMs <= 0L || cpuCount <= 0) return null
        return (cpuDeltaMs.toDouble() / wallDeltaMs / cpuCount * 100.0).coerceIn(0.0, 100.0)
    }

    fun readCpufreq(cpuRoot: File): Double? {
        if (!cpuRoot.isDirectory) return null
        val currents = mutableListOf<Long>()
        val maxima = mutableListOf<Long>()
        fun add(current: Long?, max: Long?) {
            if (current == null || max == null) return
            currents += current
            maxima += max
        }
        cpuRoot.listFiles()
            ?.filter { it.isDirectory && it.name.matches(Regex("cpu\\d+")) }
            ?.sortedBy { it.name.removePrefix("cpu").toIntOrNull() ?: 0 }
            ?.forEach { cpu ->
                add(
                    readLong(File(cpu, "cpufreq/scaling_cur_freq"))
                        ?: readLong(File(cpu, "cpufreq/cpuinfo_cur_freq")),
                    readLong(File(cpu, "cpufreq/cpuinfo_max_freq"))
                        ?: readLong(File(cpu, "cpufreq/scaling_max_freq")),
                )
            }
        if (currents.isEmpty()) {
            File(cpuRoot, "cpufreq").listFiles()
                ?.filter { it.isDirectory && it.name.startsWith("policy") }
                ?.sortedBy { it.name.removePrefix("policy").toIntOrNull() ?: 0 }
                ?.forEach { policy ->
                    add(
                        readLong(File(policy, "scaling_cur_freq")),
                        readLong(File(policy, "cpuinfo_max_freq"))
                            ?: readLong(File(policy, "scaling_max_freq")),
                    )
                }
        }
        return cpufreqPercent(currents, maxima)
    }

    fun readLong(file: File): Long? = runCatching { file.readText().trim().toLong() }.getOrNull()

    fun readText(file: File): String? = runCatching { file.readText() }.getOrNull()
}
