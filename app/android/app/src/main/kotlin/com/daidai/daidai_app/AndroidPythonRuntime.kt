package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.util.zip.ZipInputStream
import org.json.JSONObject

object AndroidPythonRuntime {
    private const val VERSION = "3.14"
    private const val ASSET_ARCHIVE = "python-runtime/3.14/python-runtime.zip"
    private const val ASSET_MANIFEST = "python-runtime/3.14/prefix/runtime-manifest.json"

    data class PythonRuntimePaths(
        val executable: String,
        val home: String,
        val stdlib: String,
        val sitePackages: String,
        val wrapperScript: String = "",
    )

    private var cached: PythonRuntimePaths? = null
    private var cacheChecked = false

    fun ensureReady(context: Context): PythonRuntimePaths? {
        if (cacheChecked) return cached
        cacheChecked = true

        val home = File(context.filesDir, "runtimes/python-$VERSION/prefix")
        val marker = File(home, ".daidai-python-ready")

        if (!marker.exists() || !File(home, "bin/python3.14").isFile) {
            try {
                extractZipAsset(context, home)
                marker.writeText("ready")
            } catch (e: Exception) {
                return null
            }
        }

        // Use libpylauncher.so from nativeLibraryDir - it's a PIE executable with exec permission
        val nativeDir = context.applicationInfo.nativeLibraryDir.orEmpty()
        val launcherExe = File(nativeDir, "libpylauncher.so")
        if (!launcherExe.isFile) return null

        val libDir = File(home, "lib")

        // Create wrapper script
        val wrapper = File(home, "bin/python3.14-wrapper.sh")
        wrapper.writeText(
            "#!/system/bin/sh\n" +
            "export LD_LIBRARY_PATH=\"$libDir:$nativeDir:\$LD_LIBRARY_PATH\"\n" +
            "export PYTHONHOME=\"$home\"\n" +
            "export PYTHONPATH=\"$home/lib/python3.14:$home/lib/python3.14/lib-dynload:$home/lib/python3.14/site-packages\"\n" +
            "export HOME=\"$home\"\n" +
            "exec \"$launcherExe\" \"\$@\"\n"
        )

        cached = PythonRuntimePaths(
            executable = "/system/bin/sh",
            home = home.absolutePath,
            stdlib = File(home, "lib/python3.14").absolutePath,
            sitePackages = File(home, "lib/python3.14/site-packages").absolutePath,
            wrapperScript = wrapper.absolutePath,
        )
        return cached
    }

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

    private fun readAssetManifest(context: Context): JSONObject {
        return context.assets.open(ASSET_MANIFEST).use { input ->
            JSONObject(input.bufferedReader().readText())
        }
    }
}
