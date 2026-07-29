package com.daidai.daidai_app

import android.content.Context
import java.io.File

object AndroidNodeRuntime {
    private const val VERSION = "18.20.4"
    private const val ASSET_ROOT = "node-runtime/$VERSION/usr"

    fun ensureReady(context: Context): NodeRuntimePaths? {
        val nativeDir = context.applicationInfo.nativeLibraryDir.orEmpty()
        val executable = File(nativeDir, "libnode_exec.so")
        if (!executable.isFile || !executable.canExecute()) return null

        val home = File(context.filesDir, "runtimes/node-$VERSION/usr")
        val marker = File(home, ".daidai-node-ready")
        if (!marker.isFile) {
            copyAssetTree(context, ASSET_ROOT, home)
            markExecutable(File(home, "bin"))
            marker.parentFile?.mkdirs()
            marker.writeText(VERSION)
        }
        val modules = File(home, "lib/node_modules")
        if (!modules.isDirectory) return null
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
}

data class NodeRuntimePaths(
    val executable: String,
    val home: String,
    val modules: String,
    val deps: String,
)
