package com.daidai.daidai_app

import java.time.ZonedDateTime
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import org.junit.Test

class AndroidFallbackCronSchedulerTest {
    private val time = ZonedDateTime.parse("2026-08-05T12:34:00+08:00")

    @Test fun everyMinuteMatches() = assertTrue(CronExpression.matches("* * * * *", time))

    @Test fun supportsStepsRangesAndLists() {
        assertTrue(CronExpression.matches("*/2 10-14 1,5,9 * *", time))
        assertFalse(CronExpression.matches("*/5 10-14 1,5,9 * *", time))
    }

    @Test fun rejectsNonFiveFieldExpression() = assertFalse(CronExpression.matches("* * * *", time))

    @Test fun sixFieldStartAndStopSchedulesRunOnlyAtTheirExactSecond() {
        val expression = "15 34 12 * * *"

        assertTrue(CronExpression.isValid(expression))
        assertFalse(CronExpression.matches(expression, time.withSecond(14)))
        assertTrue(CronExpression.matches(expression, time.withSecond(15)))
        assertFalse(CronExpression.matches(expression, time.withSecond(16)))
    }

    @Test fun direct fiveFieldMatchingUsesMinuteBoundary() {
        val expression = "34 12 * * *"

        assertTrue(CronExpression.matches(expression, time.withSecond(0)))
        assertFalse(CronExpression.matches(expression, time.withSecond(1)))
        assertFalse(CronExpression.matches(expression, time.withSecond(59)))
    }

    @Test fun fiveFieldTickCompensatesEveryUnprocessedMinuteOnce() {
        val gate = CronTickGate()
        val firstMinute = time.toEpochSecond() / 60

        assertEquals(firstMinute..firstMinute, gate.claimUnprocessedMinutes(firstMinute))
        assertEquals(LongRange.EMPTY, gate.claimUnprocessedMinutes(firstMinute))
        assertEquals(firstMinute + 1..firstMinute + 3, gate.claimUnprocessedMinutes(firstMinute + 3))
        assertEquals(LongRange.EMPTY, gate.claimUnprocessedMinutes(firstMinute + 3))
    }

    @Test fun delayed fiveFieldTickMatchesMissedMinuteWhileSixFieldKeepsExactSecond() {
        val delayed = time.plusMinutes(1).withSecond(7)
        val missedMinute = time.toEpochSecond() / 60

        assertTrue(CronExpression.matchesTick("34 12 * * *", delayed, missedMinute..missedMinute + 1))
        assertFalse(CronExpression.matchesTick("15 34 12 * * *", delayed, missedMinute..missedMinute + 1))
        assertTrue(CronExpression.matchesTick("7 35 12 * * *", delayed, missedMinute..missedMinute + 1))
    }

    @Test fun tickNowCannotClaimTheSameSecondTwice() {
        val gate = CronTickGate()
        val epochSecond = time.toEpochSecond()

        assertTrue(gate.claimSecond(epochSecond))
        assertFalse(gate.claimSecond(epochSecond))
        assertTrue(gate.claimSecond(epochSecond + 1))
    }

    @Test fun maintenanceRunsOncePerMinuteAcrossSecondTicks() {
        val gate = CronTickGate()
        val epochMinute = time.toEpochSecond() / 60

        assertTrue(gate.claimMaintenanceMinute(epochMinute))
        assertFalse(gate.claimMaintenanceMinute(epochMinute))
        assertTrue(gate.claimMaintenanceMinute(epochMinute + 1))
    }

    @Test fun rejectsMalformedAndOutOfRangeFields() {
        listOf(
            "*/0 * * * *",
            "1/2/3 * * * *",
            "60 * * * *",
            "* 24 * * *",
            "* * 31-1 * *",
            "* * * * 8",
            "1, * * * *",
            "1,,2 * * * *",
            "60 34 12 * * *",
            "0 60 12 * * *",
        ).forEach { expression ->
            assertFalse(expression, CronExpression.isValid(expression))
            assertFalse(expression, CronExpression.matches(expression, time))
        }
    }

    @Test fun validatesSupportedFiveFieldSyntax() {
        assertTrue(CronExpression.isValid("*/2 10-14 1,5,9 * 0,7"))
    }

    @Test fun rejectsUnsupportedFieldCounts() {
        assertFalse(CronExpression.isValid("* * * *"))
        assertFalse(CronExpression.isValid("* * * * * * *"))
    }

    @Test fun maintenanceGateRejectsNewTasksAndWaitsForActiveTask() {
        val gate = MaintenanceGate()
        assertTrue(gate.tryEnterTask())
        val started = CountDownLatch(1)
        val completed = CountDownLatch(1)
        val thread = Thread {
            started.countDown()
            assertTrue(gate.beginMaintenance(1_000))
            completed.countDown()
        }
        thread.start()
        assertTrue(started.await(1, TimeUnit.SECONDS))
        while (!gate.isMaintenanceActive()) Thread.yield()
        assertFalse(gate.tryEnterTask())
        assertEquals(1, gate.activeTaskCount())
        gate.leaveTask()
        assertTrue(completed.await(1, TimeUnit.SECONDS))
        gate.endMaintenance()
        assertTrue(gate.tryEnterTask())
        gate.leaveTask()
    }

    @Test fun maintenanceGateTimeoutReopensTaskSubmission() {
        val gate = MaintenanceGate()
        assertTrue(gate.tryEnterTask())
        assertFalse(gate.beginMaintenance(1))
        assertFalse(gate.isMaintenanceActive())
        gate.leaveTask()
        assertTrue(gate.tryEnterTask())
        gate.leaveTask()
    }
}
