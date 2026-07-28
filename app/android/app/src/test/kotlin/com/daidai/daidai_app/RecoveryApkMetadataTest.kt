package com.daidai.daidai_app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class RecoveryApkMetadataTest {
    @Test
    fun `release reserves ten version codes and recovery uses base plus one`() {
        val reservation = RecoveryApkMetadata.reserve(120)

        assertEquals(120, reservation.modernVersionCode)
        assertEquals(121, reservation.recoveryVersionCode)
        assertEquals(130, reservation.nextReleaseMinimumVersionCode)
    }

    @Test
    fun `metadata scaffolds stable core denylist and forward compatibility`() {
        val metadata = RecoveryApkMetadata.metadata(
            releaseBase = 120,
            stableCoreVersion = "core-0.1.55",
            stableRuntimeManifestSha256 = "a".repeat(64),
            deniedCoreVersions = listOf("core-0.1.56"),
            deniedRuntimeIds = listOf("python-bad"),
        )

        assertEquals(121, metadata.getJSONObject("recoveryApk").getInt("versionCode"))
        assertTrue(metadata.getJSONObject("recoveryApk").getBoolean("forwardReadingCompatibility"))
        assertEquals("core-0.1.55", metadata.getJSONObject("stableRuntime").getString("coreVersion"))
        assertEquals("core-0.1.56", metadata.getJSONObject("denylist").getJSONArray("coreVersions").getString(0))
        assertEquals("python-bad", metadata.getJSONObject("denylist").getJSONArray("runtimeIds").getString(0))
    }
}
