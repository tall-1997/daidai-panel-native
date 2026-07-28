package com.daidai.daidai_app

import org.json.JSONArray
import org.json.JSONObject

object RecoveryApkMetadata {
    const val RESERVED_CODES_PER_RELEASE = 10
    const val MODERN_OFFSET = 0
    const val RECOVERY_OFFSET = 1

    fun reserve(releaseBase: Int): Reservation {
        require(releaseBase >= 0) { "releaseBase must be non-negative" }
        require(releaseBase % RESERVED_CODES_PER_RELEASE == 0) {
            "releaseBase must align to $RESERVED_CODES_PER_RELEASE version codes"
        }
        return Reservation(
            releaseBase = releaseBase,
            modernVersionCode = releaseBase + MODERN_OFFSET,
            recoveryVersionCode = releaseBase + RECOVERY_OFFSET,
            nextReleaseMinimumVersionCode = releaseBase + RESERVED_CODES_PER_RELEASE,
        )
    }

    fun metadata(
        releaseBase: Int,
        stableCoreVersion: String,
        stableRuntimeManifestSha256: String,
        deniedCoreVersions: List<String> = emptyList(),
        deniedRuntimeIds: List<String> = emptyList(),
    ): JSONObject {
        val reservation = reserve(releaseBase)
        return JSONObject()
            .put("schemaVersion", 1)
            .put("versionCodeReservation", reservation.toJson())
            .put(
                "recoveryApk",
                JSONObject()
                    .put("versionCode", reservation.recoveryVersionCode)
                    .put("buildsFrom", "last_supported_stable_core")
                    .put("forwardReadingCompatibility", true)
                    .put("startsWorkersAfterCompatibilityCheck", true),
            )
            .put(
                "stableRuntime",
                JSONObject()
                    .put("coreVersion", stableCoreVersion)
                    .put("runtimeManifestSha256", stableRuntimeManifestSha256),
            )
            .put(
                "denylist",
                JSONObject()
                    .put("coreVersions", JSONArray(deniedCoreVersions))
                    .put("runtimeIds", JSONArray(deniedRuntimeIds)),
            )
    }

    data class Reservation(
        val releaseBase: Int,
        val modernVersionCode: Int,
        val recoveryVersionCode: Int,
        val nextReleaseMinimumVersionCode: Int,
    ) {
        fun toJson(): JSONObject = JSONObject()
            .put("releaseBase", releaseBase)
            .put("reservedCodesPerRelease", RESERVED_CODES_PER_RELEASE)
            .put("modernVersionCode", modernVersionCode)
            .put("recoveryVersionCode", recoveryVersionCode)
            .put("nextReleaseMinimumVersionCode", nextReleaseMinimumVersionCode)
    }
}
