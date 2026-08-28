package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class LocalBackupServiceTest {
    @Test
    fun `restore journal only advances through durable stages`() {
        assertTrue(RestoreJournalStateMachine.canAdvance(RestoreStage.PREPARED, RestoreStage.SCRIPTS_SWITCHED))
        assertTrue(RestoreJournalStateMachine.canAdvance(RestoreStage.SCRIPTS_SWITCHED, RestoreStage.DATABASE_COMMITTED))
        assertTrue(RestoreJournalStateMachine.canAdvance(RestoreStage.DATABASE_COMMITTED, RestoreStage.COMPLETED))
        assertFalse(RestoreJournalStateMachine.canAdvance(RestoreStage.PREPARED, RestoreStage.DATABASE_COMMITTED))
        assertFalse(RestoreJournalStateMachine.canAdvance(RestoreStage.COMPLETED, RestoreStage.PREPARED))
    }

    @Test
    fun `missing database marker always rolls scripts back`() {
        listOf(RestoreStage.PREPARED, RestoreStage.SCRIPTS_SWITCHED, RestoreStage.DATABASE_COMMITTED).forEach { stage ->
            assertEquals(RestoreRecovery.ROLL_BACK, RestoreJournalStateMachine.recovery(stage, databaseCommitMarker = false))
        }
    }

    @Test
    fun `database marker makes every incomplete journal roll forward`() {
        listOf(RestoreStage.PREPARED, RestoreStage.SCRIPTS_SWITCHED, RestoreStage.DATABASE_COMMITTED).forEach { stage ->
            assertEquals(RestoreRecovery.ROLL_FORWARD, RestoreJournalStateMachine.recovery(stage, databaseCommitMarker = true))
        }
    }

    @Test
    fun `completed journal only needs cleanup`() {
        assertEquals(RestoreRecovery.CLEAN_UP, RestoreJournalStateMachine.recovery(RestoreStage.COMPLETED, databaseCommitMarker = false))
        assertEquals(RestoreRecovery.CLEAN_UP, RestoreJournalStateMachine.recovery(RestoreStage.COMPLETED, databaseCommitMarker = true))
    }

    @Test
    fun `journal wire names remain stable`() {
        RestoreStage.entries.forEach { stage ->
            assertEquals(stage, RestoreStage.fromWireName(stage.wireName))
        }
    }

    @Test
    fun `fault injector can stop at every durable boundary`() {
        RestoreCheckpoint.entries.forEach { expected ->
            val injector = RestoreFaultInjector { actual ->
                if (actual == expected) throw SimulatedRestoreProcessDeath(actual)
            }
            try {
                injector.afterStage(expected)
                fail("checkpoint $expected should simulate process death")
            } catch (error: SimulatedRestoreProcessDeath) {
                assertEquals(expected.name, error.message)
            }
        }
    }

}
