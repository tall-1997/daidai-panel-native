package com.daidai.daidai_app

import android.content.Context
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest

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
        val paths = PythonRuntimePaths(executable.absolutePath, home.absolutePath, depsDir(context).absolutePath)
        ensureSeedPackages(context, paths)
        return paths
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

    private fun ensureSeedPackages(context: Context, paths: PythonRuntimePaths) {
        val deps = File(paths.deps).apply { mkdirs() }
        val marker = File(deps, ".daidai-python-seed-ready")
        if (marker.isFile) return
        val wheelhouse = File(paths.home, "wheelhouse")
        if (!wheelhouse.isDirectory) return
        verifyWheelhouse(wheelhouse)
        val command = listOf(
            paths.executable,
            paths.home,
            "-m",
            "pip",
            "install",
            "--no-index",
            "--find-links",
            wheelhouse.absolutePath,
            "--target",
            deps.absolutePath,
            "certifi==2026.5.20",
            "charset-normalizer==3.4.7",
            "idna==3.18",
            "requests==2.34.2",
            "urllib3==2.7.0",
        )
        val process = ProcessBuilder(command)
            .redirectErrorStream(true)
            .apply {
                environment()["LD_LIBRARY_PATH"] = context.applicationInfo.nativeLibraryDir.orEmpty()
                environment()["PYTHONPATH"] = deps.absolutePath
                environment()["PIP_TARGET"] = deps.absolutePath
                environment()["HOME"] = context.filesDir.absolutePath
                environment()["TMPDIR"] = context.cacheDir.absolutePath
            }
            .start()
        val output = process.inputStream.bufferedReader().readText()
        val exit = process.waitFor()
        check(exit == 0) { "Python seed dependency install failed: $output" }
        marker.writeText(VERSION)
    }

    private fun verifyWheelhouse(wheelhouse: File) {
        val manifest = File(wheelhouse, "wheelhouse-manifest.json")
        check(manifest.isFile) { "Python wheelhouse manifest is missing" }
        val json = JSONObject(manifest.readText())
        val wheels = json.optJSONArray("wheels") ?: error("Python wheelhouse manifest has no wheels")
        for (index in 0 until wheels.length()) {
            val item = wheels.getJSONObject(index)
            val file = File(wheelhouse, item.getString("filename"))
            check(file.isFile) { "Python wheel missing: ${file.name}" }
            val expected = item.getString("sha256")
            val actual = sha256(file)
            check(expected.equals(actual, ignoreCase = true)) {
                "Python wheel checksum mismatch: ${file.name}"
            }
        }
    }

    private fun sha256(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(8192)
            while (true) {
                val read = input.read(buffer)
                if (read <= 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { byte -> "%02x".format(byte) }
    }
}

data class PythonRuntimePaths(
    val executable: String,
    val home: String,
    val deps: String,
)
