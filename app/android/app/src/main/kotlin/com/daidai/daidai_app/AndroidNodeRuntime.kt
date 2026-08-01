package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.security.MessageDigest

object AndroidNodeRuntime {
    private const val VERSION = "18.20.4"
    private const val ASSET_ROOT = "node-runtime/$VERSION/usr"
    private const val METADATA_ASSET = "$ASSET_ROOT/runtime-metadata.json"

    fun ensureReady(context: Context): NodeRuntimePaths? {
        val nativeDir = context.applicationInfo.nativeLibraryDir.orEmpty()
        val executable = File(nativeDir, "libnode_exec.so")
        if (!executable.isFile || !executable.canExecute()) return null

        val home = File(context.filesDir, "runtimes/node-$VERSION/usr")
        val marker = File(home, ".daidai-node-ready")
        val bundleHash = metadataValue(context, "bundle_sha256") ?: return null
        if (marker.readTextOrNull() != bundleHash) {
            copyAssetTree(context, ASSET_ROOT, home)
            markExecutable(File(home, "bin"))
            if (!verifyInstalledAssets(context, home)) return null
            marker.parentFile?.mkdirs()
            marker.writeText(bundleHash)
        }
        val modules = File(home, "lib/node_modules")
        if (!modules.isDirectory || !File(modules, "npm/bin/npm-cli.js").isFile || !File(modules, "typescript/bin/tsc").isFile) return null
        return NodeRuntimePaths(executable.absolutePath, home.absolutePath, modules.absolutePath, depsDir(context).absolutePath)
    }

    fun depsDir(context: Context): File = File(context.filesDir, "deps/nodejs").apply { mkdirs() }

    private fun copyAssetTree(context: Context, assetPath: String, target: File) {
        val children = context.assets.list(assetPath).orEmpty()
        if (children.isEmpty()) {
            target.parentFile?.mkdirs()
            context.assets.open(assetPath).use { input ->
                target.outputStream().use { output -> input.copyTo(output) }
            }
            return
        }
        target.mkdirs()
        for (child in children) {
            copyAssetTree(context, "$assetPath/$child", File(target, child))
        }
    }

    private fun markExecutable(file: File) {
        if (!file.exists()) return
        if (file.isFile) {
            file.setExecutable(true, true)
            return
        }
        file.listFiles()?.forEach(::markExecutable)
    }

    private fun verifyInstalledAssets(context: Context, home: File): Boolean {
        val expected = metadataValue(context, "bundle_sha256") ?: return false
        val files = listOf(
            "lib/node_modules/npm/package.json",
            "lib/node_modules/npm/bin/npm-cli.js",
            "lib/node_modules/npm/bin/npx-cli.js",
            "lib/node_modules/typescript/package.json",
            "lib/node_modules/typescript/bin/tsc",
            "etc/npmrc",
        )
        val digest = MessageDigest.getInstance("SHA-256")
        for (path in files) {
            val file = File(home, path)
            if (!file.isFile) return false
            digest.update(path.toByteArray())
            digest.update(0.toByte())
            file.inputStream().use { input ->
                val buffer = ByteArray(DEFAULT_BUFFER_SIZE)
                while (true) {
                    val count = input.read(buffer)
                    if (count < 0) break
                    digest.update(buffer, 0, count)
                }
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) } == expected
    }

    private fun metadataValue(context: Context, key: String): String? = runCatching {
        val metadata = context.assets.open(METADATA_ASSET).bufferedReader().use { it.readText() }
        Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"([^\"]+)\"").find(metadata)?.groupValues?.get(1)
    }.getOrNull()

    private fun File.readTextOrNull(): String? = runCatching { takeIf(File::isFile)?.readText() }.getOrNull()
}

data class NodeRuntimePaths(
    val executable: String,
    val home: String,
    val modules: String,
    val deps: String,
)
