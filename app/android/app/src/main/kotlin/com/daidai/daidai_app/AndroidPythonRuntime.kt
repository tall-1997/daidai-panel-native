package com.daidai.daidai_app

import android.content.Context
import java.io.File

object AndroidPythonRuntime {
    private const val VERSION = "3.14"
    private const val ASSET_ROOT = "python-runtime/$VERSION/prefix"

    fun ensureReady(context: Context): PythonRuntimePaths? {
        val nativeDir = context.applicationInfo.nativeLibraryDir.orEmpty()
        val executable = File(nativeDir, "libpython_exec.so")
        if (!executable.isFile || !executable.canExecute()) return null

        val home = File(context.filesDir, "runtimes/python-$VERSION/prefix")
        val marker = File(home, ".daidai-python-ready")
        if (!marker.isFile) {
            copyAssetTree(context, ASSET_ROOT, home)
            marker.parentFile?.mkdirs()
            marker.writeText(VERSION)
        }
        val stdlib = File(home, "lib/python$VERSION")
        if (!stdlib.isDirectory) return null
        return PythonRuntimePaths(executable.absolutePath, home.absolutePath, depsDir(context).absolutePath)
    }

    fun depsDir(context: Context): File = File(context.filesDir, "deps/python/$VERSION/site-packages").apply { mkdirs() }

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
}

data class PythonRuntimePaths(
    val executable: String,
    val home: String,
    val deps: String,
)
