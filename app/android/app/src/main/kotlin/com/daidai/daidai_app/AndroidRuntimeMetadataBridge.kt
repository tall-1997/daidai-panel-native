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
        "runtimeDependenciesPath" to "dependencies.json",
    )

    fun metadataOptions(context: Context): Map<String, String> {
        val outputDir = File(context.filesDir, "runtime-metadata").apply { mkdirs() }
        return ASSET_NAMES.mapValues { (_, assetName) ->
            val output = File(outputDir, assetName)
            try {
                context.assets.open(assetName).use { input ->
                    copyAtomically(input, output)
                }
            } catch (_: Exception) {
                generateDefaultMetadata(output, assetName, context)
            }
            output.absolutePath
        }
    }

    private fun generateDefaultMetadata(output: File, assetName: String, context: Context) {
        val content = when (assetName) {
            "manifest.json" -> {
                val pythonRuntimeDir = File(context.filesDir, "local-panel/python-runtime/3.14/prefix")
                pythonRuntimeDir.mkdirs()
                val runtimeManifest = File(pythonRuntimeDir, "runtime-manifest.json")
                if (!runtimeManifest.exists()) {
                    runtimeManifest.writeText("""{"runtime":"python","version":"3.14","platform":"android-arm64","status":"fallback","pip_available":false}""")
                }
                """{"version":"fallback","container_model":"layered-linux-runtime","runtimes":[{"name":"python-3.14","language":"python","version":"3.14","platform":"android-arm64","status":"fallback","prefix":"${pythonRuntimeDir.absolutePath}"},{"name":"shell","language":"shell","version":"android-16","status":"active"},{"name":"node-lts","language":"node","version":"lts","status":"fallback"},{"name":"git","language":"git","version":"android","status":"fallback"},{"name":"ssh","language":"ssh","version":"android","status":"fallback"}],"fallback_mode":true}"""
            }
            "compatibility.json" -> """{"compatibility":"android-16-fallback","container_model":"layered-linux-runtime","python":"limited","shell":"active"}"""
            "smoke-evidence.json" -> """{"status":"fallback","evidence":[]}"""
            "dependencies.json" -> """{"version":"fallback","python":{"runtime":"python-3.14-android-arm64"},"nodejs":{"runtime":"node-lts-android-arm64"}}"""
            else -> "{}"
        }
        output.writeText(content)
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
