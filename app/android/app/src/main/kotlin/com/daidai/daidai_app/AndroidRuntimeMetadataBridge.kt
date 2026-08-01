package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.io.InputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption.ATOMIC_MOVE
import java.nio.file.StandardCopyOption.REPLACE_EXISTING

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
                copyAtomically(input, output)
            }
            output.absolutePath
        }
    }

    internal fun copyAtomically(input: InputStream, output: File) {
        val staging = Files.createTempFile(output.parentFile.toPath(), "${output.name}.", ".tmp")
        try {
            Files.newOutputStream(staging).use { outputStream ->
                input.copyTo(outputStream)
                outputStream.flush()
            }
            Files.move(staging, output.toPath(), ATOMIC_MOVE, REPLACE_EXISTING)
        } finally {
            Files.deleteIfExists(staging)
        }
    }
}
