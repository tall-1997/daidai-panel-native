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
    @Volatile private var cacheChecked = false

    fun ensureReady(context: Context): PythonRuntimePaths? {
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
        Thread {
            try { ensureReady(context) } catch (_: Exception) { }
        }.start()
    }

    fun isCompatibleWheel(filename: String): Boolean {
        val lower = filename.lowercase()
        if (listOf("cp312", "manylinux", "musllinux", "linux_aarch64", "android_28_x86_64").any(lower::contains)) return false
        return lower.endsWith("-py3-none-any.whl") ||
            Regex(".+-cp314-(cp314|abi3)-android_[0-9]+_arm64_v8a\\.whl").matches(lower)
    }

    private fun doEnsureReady(context: Context): PythonRuntimePaths? {

        val home = File(context.filesDir, "runtimes/python-$VERSION/prefix")
        val marker = File(home, ".daidai-python-ready")
        val nativeDir = AndroidLinuxRuntime.nativeLibraryDir(context).absolutePath
        val libDir = File(home, "lib")
        val compatLibDir = File(home, "compat-lib")

        // Force re-extraction if _ssl.so is missing or marker is old
        val sslSo = File(home, "lib/python3.14/lib-dynload/_ssl.cpython-314-aarch64-linux-android.so")
        if (!marker.exists() || !sslSo.isFile || marker.length() < 10) {
            if (marker.exists()) { marker.delete() }
            home.deleteRecursively()
            home.mkdirs()
            try {
                extractZipAsset(context, home)
                AndroidLinuxRuntime.copyVersionedLibraries(File(nativeDir), compatLibDir, VERSIONED_LIBS)
                // Bootstrap pip
                bootstrapPip(context, home, nativeDir, compatLibDir)
                marker.writeText("ready:$VERSION")
            } catch (e: Exception) {
                return null
            }
        }

        // Ensure compatLibDir exists even if marker was already set
        if (!compatLibDir.exists()) {
            AndroidLinuxRuntime.copyVersionedLibraries(File(nativeDir), compatLibDir, VERSIONED_LIBS)
        }

        // Use libpylauncher.so from nativeLibraryDir - it's a PIE executable with exec permission
        val launcherExe = File(nativeDir, "libpylauncher.so")
        if (!launcherExe.isFile) return null

        // Create wrapper script
        val wrapper = AndroidLinuxRuntime.writeShellWrapper(
            output = File(home, "bin/python3.14-wrapper.sh"),
            env = mapOf(
                "LD_LIBRARY_PATH" to "$compatLibDir:$libDir:$nativeDir:\$LD_LIBRARY_PATH",
                "PYTHONHOME" to home.absolutePath,
                "PYTHONPATH" to "$home/lib/python3.14:$home/lib/python3.14/lib-dynload:$home/lib/python3.14/site-packages:${DependencyStorage.pythonSitePackages(context.filesDir)}",
                "HOME" to home.absolutePath,
                "DAIDAI_RUNTIME_LANGUAGE" to "python",
            ),
            executable = launcherExe,
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

    private val VERSIONED_LIBS = mapOf(
        "libssl_v3.so" to listOf("libssl.so.3"),
        "libssl_python.so" to listOf("libssl.so.3"),
        "libcrypto_v3.so" to listOf("libcrypto.so.3"),
        "libcrypto_python.so" to listOf("libcrypto.so.3"),
        "libz.so" to listOf("libz.so.1"),
        "libsqlite3.so" to listOf("libsqlite3.so.0"),
        "libsqlite3_python.so" to listOf("libsqlite3.so.0"),
        "libffi.so" to listOf("libffi.so.8", "libffi.so.7"),
        "libexpat_termux.so" to listOf("libexpat.so.1"),
        "libbz2_termux.so" to listOf("libbz2.so.1.0", "libbz2.so.1"),
        "liblzma_termux.so" to listOf("liblzma.so.5"),
    )

    private fun readAssetManifest(context: Context): JSONObject {
        return context.assets.open(ASSET_MANIFEST).use { input ->
            JSONObject(input.bufferedReader().readText())
        }
    }

    private fun bootstrapPip(context: Context, home: File, nativeDir: String, compatLibDir: File) {
        val launcherExe = File(nativeDir, "libpylauncher.so")
        val libDir = File(home, "lib")
        val pipDir = File(home, "lib/python3.14/site-packages")
        pipDir.mkdirs()

        // Install pip by extracting the wheel directly
        val bootstrapScript = File(home, "bin/bootstrap_pip.py")
        bootstrapScript.writeText("""
import os, sys, zipfile, glob

home = os.environ.get('PYTHONHOME', '')
site_packages = os.path.join(home, 'lib', 'python3.14', 'site-packages')
bundled = os.path.join(home, 'lib', 'python3.14', 'ensurepip', '_bundled')

os.makedirs(site_packages, exist_ok=True)

# Install pip from local wheel (fast, no network)
wheels = glob.glob(os.path.join(bundled, 'pip*.whl'))
if wheels:
    print(f"Installing pip from {wheels[0]}")
    with zipfile.ZipFile(wheels[0]) as z:
        z.extractall(site_packages)
    sys.path.insert(0, site_packages)
    import pip
    print(f"pip {pip.__version__} installed")
else:
    print("No pip wheel found")
""".trimIndent())

        val wrapper = File(home, "bin/bootstrap-wrapper.sh")
        wrapper.writeText(
            "#!/system/bin/sh\n" +
            "export LD_LIBRARY_PATH=\"$compatLibDir:$libDir:$nativeDir:\$LD_LIBRARY_PATH\"\n" +
            "export PYTHONHOME=\"$home\"\n" +
            "export PYTHONPATH=\"$home/lib/python3.14:$home/lib/python3.14/lib-dynload:$home/lib/python3.14/site-packages\"\n" +
            "export HOME=\"$home\"\n" +
            "exec \"$launcherExe\" \"$bootstrapScript\"\n"
        )

        try {
            val pb = ProcessBuilder("/system/bin/sh", wrapper.absolutePath)
            pb.directory(home)
            pb.redirectErrorStream(true)
            val process = pb.start()
            process.inputStream.bufferedReader().readText()
            process.waitFor(30, java.util.concurrent.TimeUnit.SECONDS)
        } catch (_: Exception) { }
    }
}
