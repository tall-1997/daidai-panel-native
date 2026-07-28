package com.daidai.daidai_app

import android.content.Context
import java.io.File

object AndroidRuntimeMetadataBridge {
    private val ASSET_NAMES = mapOf(
        "runtimeManifestPath" to "manifest.json",
        "runtimeCompatibilityPath" to "compatibility.json",
        "runtimeSmokeEvidencePath" to "smoke-evidence.json",
    )

    fun metadataOptions(context: Context): Map<String, String> {
        val outputDir = File(context.filesDir, "runtime-metadata").apply { mkdirs() }
        return ASSET_NAMES.mapValues { (_, assetName) ->
            val output = File(outputDir, assetName)
            context.assets.open(assetName).use { input ->
                output.outputStream().use { outputStream -> input.copyTo(outputStream) }
            }
            output.absolutePath
        }
    }
}
