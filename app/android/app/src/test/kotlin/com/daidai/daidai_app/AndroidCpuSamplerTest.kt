package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.io.File

class AndroidCpuSamplerTest {
    @Test
    fun `proc stat delta ignores a frozen or unreadable sample`() {
        val first = AndroidCpuSampler.parseProcStat("cpu  10 0 10 80 0 0 0 0 0 0\n")
        val second = AndroidCpuSampler.parseProcStat("cpu  20 0 20 140 0 0 0 0 0 0\n")
        assertEquals(25.0, AndroidCpuSampler.usageFromProcSamples(first!!, second!!)!!, 0.01)
        assertNull(AndroidCpuSampler.usageFromProcSamples(first, first))
        assertNull(AndroidCpuSampler.parseProcStat("cpu0 1 2 3\n"))
    }

    @Test
    fun `cpufreq averages online cores and skips missing maxima`() {
        assertEquals(75.0, AndroidCpuSampler.cpufreqPercent(listOf(500L, 1000L), listOf(1000L, 1000L))!!, 0.01)
        assertEquals(50.0, AndroidCpuSampler.cpufreqPercent(listOf(500L, 0L), listOf(1000L, 0L))!!, 0.01)
        assertNull(AndroidCpuSampler.cpufreqPercent(emptyList(), emptyList()))
        assertEquals(0.0, AndroidCpuSampler.cpufreqPercent(listOf(0L), listOf(1800000L))!!, 0.01)
    }

    @Test
    fun `load average and process time stay inside zero to one hundred`() {
        assertEquals(25.0, AndroidCpuSampler.loadAveragePercent("2.00 1.50 1.00 3/100 1", 8)!!, 0.01)
        assertEquals(100.0, AndroidCpuSampler.loadAveragePercent("20 0 0 1/1 1", 4)!!, 0.01)
        assertNull(AndroidCpuSampler.loadAveragePercent("not-a-load", 4))
        assertEquals(12.5, AndroidCpuSampler.processUsagePercent(100L, 200L, 4)!!, 0.01)
        assertNull(AndroidCpuSampler.processUsagePercent(10L, 0L, 4))
    }

    @Test
    fun `cpufreq reader accepts per-cpu and policy layouts`() {
        val root = File.createTempFile("cpu-root", "").apply { delete() }
        root.mkdirs()
        File(root, "cpu0/cpufreq").mkdirs()
        File(root, "cpu0/cpufreq/scaling_cur_freq").writeText("900000")
        File(root, "cpu0/cpufreq/cpuinfo_max_freq").writeText("1800000")
        assertEquals(50.0, AndroidCpuSampler.readCpufreq(root)!!, 0.01)

        val policyRoot = File.createTempFile("cpu-policy", "").apply { delete() }
        policyRoot.mkdirs()
        File(policyRoot, "cpufreq/policy0").mkdirs()
        File(policyRoot, "cpufreq/policy0/scaling_cur_freq").writeText("300000")
        File(policyRoot, "cpufreq/policy0/scaling_max_freq").writeText("1200000")
        assertEquals(25.0, AndroidCpuSampler.readCpufreq(policyRoot)!!, 0.01)
    }
}
