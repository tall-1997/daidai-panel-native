package com.daidai.daidai_app

import java.time.ZonedDateTime
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AndroidFallbackCronSchedulerTest {
    private val time = ZonedDateTime.parse("2026-08-05T12:34:00+08:00")

    @Test fun everyMinuteMatches() = assertTrue(CronExpression.matches("* * * * *", time))

    @Test fun supportsStepsRangesAndLists() {
        assertTrue(CronExpression.matches("*/2 10-14 1,5,9 * *", time))
        assertFalse(CronExpression.matches("*/5 10-14 1,5,9 * *", time))
    }

    @Test fun rejectsNonFiveFieldExpression() = assertFalse(CronExpression.matches("* * * *", time))
}
