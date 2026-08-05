package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.util.zip.ZipInputStream
import org.json.JSONObject

object AndroidNodeRuntime {
    private const val VERSION = "18.20.4"
    private const val ASSET_ARCHIVE = "node-runtime/18.20.4/node-runtime.zip"

    data class NodeRuntimePaths(
        val executable: String,
        val home: String,
        val modules: String,
    )

    private var cached: NodeRuntimePaths? = null
    @Volatile private var cacheChecked = false

    fun ensureReady(context: Context): NodeRuntimePaths? {
        if (cacheChecked) return cached
        synchronized(this) {
            if (cacheChecked) return cached
            val result = doEnsureReady(context)
            cached = result
            cacheChecked = true
            return result
        }
    }

    fun preload(context: Context) {
        Thread { try { ensureReady(context) } catch (_: Exception) { } }.start()
    }

    private fun doEnsureReady(context: Context): NodeRuntimePaths? {
        val nativeDir = context.applicationInfo.nativeLibraryDir.orEmpty()
        val launcherExe = File(nativeDir, "libnodelauncher.so")
        if (!launcherExe.isFile) return null

        val home = File(context.filesDir, "runtimes/node-$VERSION/usr")
        val marker = File(home, ".daidai-node-ready")

        if (!marker.exists() || !File(home, "lib/node_modules/npm/bin/npm-cli.js").isFile) {
            try {
                home.deleteRecursively()
                home.mkdirs()
                extractZipAsset(context, home)
            } catch (e: Exception) {
                return null
            }
        }

        val modules = File(home, "lib/node_modules")
        if (!File(modules, "npm/bin/npm-cli.js").isFile) return null

        // Create compat-lib with versioned .so copies
        val compatLibDir = File(home, "compat-lib")
        compatLibDir.mkdirs()
        val versionedLibs = mapOf(
            "libicudata_v78.so" to listOf("libicudata.so.78"),
            "libicui18n_v78.so" to listOf("libicui18n.so.78"),
            "libicuuc_v78.so" to listOf("libicuuc.so.78"),
            "libicuio_v78.so" to listOf("libicuio.so.78"),
            "libicutu_v78.so" to listOf("libicutu.so.78"),
            "libicutest_v78.so" to listOf("libicutest.so.78"),
            "libcares.so" to listOf("libcares.so.2"),
            "libssl_v3.so" to listOf("libssl.so.3"),
            "libcrypto_v3.so" to listOf("libcrypto.so.3"),
            "libz.so" to listOf("libz.so.1"),
            "libsqlite3.so" to listOf("libsqlite3.so.0"),
            "libffi.so" to listOf("libffi.so.8"),
        )
        for ((source, targets) in versionedLibs) {
            val sourceFile = File(nativeDir, source)
            if (sourceFile.exists()) {
                for (target in targets) {
                    val targetFile = File(compatLibDir, target)
                    if (!targetFile.exists()) {
                        try { sourceFile.copyTo(targetFile, overwrite = true) } catch (_: Exception) { }
                    }
                }
            }
        }

        // Create wrapper script
        val wrapper = File(home, "bin/node-wrapper.sh")
        wrapper.parentFile?.mkdirs()
        wrapper.writeText(
            "#!/system/bin/sh\n" +
            "export LD_LIBRARY_PATH=\"$compatLibDir:$nativeDir:${home}/lib:\$LD_LIBRARY_PATH\"\n" +
            "export NODE_PATH=\"${modules}:\${NODE_PATH:-}\"\n" +
            "export HOME=\"$home\"\n" +
            "exec \"$launcherExe\" \"\$@\"\n"
        )

        marker.writeText("ready")

        return NodeRuntimePaths(
            executable = "/system/bin/sh",
            home = home.absolutePath,
            modules = modules.absolutePath,
        ).also { wrapperPath = wrapper.absolutePath }
    }

    @Volatile var wrapperPath: String = ""

    private fun extractZipAsset(context: Context, dest: File) {
        dest.mkdirs()
        context.assets.open(ASSET_ARCHIVE).use { input ->
            ZipInputStream(input).use { zis ->
                var entry = zis.nextEntry
                while (entry != null) {
                    val outFile = File(dest, entry.name)
                    if (entry.isDirectory) {
                        outFile.mkdirs()
                    } else {
                        outFile.parentFile?.mkdirs()
                        java.io.FileOutputStream(outFile).use { fos ->
                            val buf = ByteArray(8192)
                            var len: Int
                            while (zis.read(buf).also { len = it } > 0) {
                                fos.write(buf, 0, len)
                            }
                        }
                    }
                    entry = zis.nextEntry
                }
            }
        }
    }

    fun depsDir(context: Context): File = File(context.filesDir, "deps/nodejs").apply { mkdirs() }
}
