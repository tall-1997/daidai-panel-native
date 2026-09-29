package com.daidai.daidai_app

import android.content.Context
import java.io.File
import java.util.zip.ZipInputStream

object AndroidNodeRuntime {
    private const val VERSION = "18.20.4"
    private const val ASSET_ARCHIVE = "node-runtime/18.20.4/node-runtime.zip"
    const val TYPESCRIPT_VERSION = "5.6.3"

    data class NodeRuntimePaths(
        val executable: String,
        val home: String,
        val modules: String,
    )

    private var cached: NodeRuntimePaths? = null
    @Volatile private var lastFailure: String = ""
    @Volatile private var moduleExtractAttempted = false

    fun guestNodeModulePath(includeWorkspace: Boolean = false): String {
        val paths = mutableListOf(
            "/host-files/runtimes/node-$VERSION/usr/lib/node_modules",
            "/host-files/deps/nodejs/node_modules",
            "/usr/local/lib/node_modules",
            "/usr/lib/node_modules",
        )
        if (includeWorkspace) paths += "/workspace"
        return paths.joinToString(":")
    }

    fun bundledModulesDir(context: Context): File = moduleHome(context)

    fun typescriptInstalled(context: Context): Boolean =
        typescriptReady(moduleHome(context)) ||
            File(context.filesDir, "deps/nodejs/node_modules/typescript/package.json").isFile

    fun ensureModules(context: Context): File? {
        val modules = moduleHome(context)
        if (typescriptReady(modules)) return modules
        synchronized(this) {
            if (typescriptReady(modules)) return modules
            if (moduleExtractAttempted) return null
            moduleExtractAttempted = true
            val home = modules.parentFile?.parentFile ?: return null
            return try {
                home.deleteRecursively()
                home.mkdirs()
                extractZipAsset(context, home)
                modules.takeIf { typescriptReady(it) }
            } catch (error: Exception) {
                lastFailure = error.message.orEmpty()
                null
            }
        }
    }

    fun ensureReady(context: Context): NodeRuntimePaths? {
        cached?.let { return it }
        synchronized(this) {
            cached?.let { return it }
            return runCatching { doEnsureReady(context) }
                .onFailure { lastFailure = it.message.orEmpty() }
                .getOrNull()?.also { cached = it; lastFailure = "" }
        }
    }

    fun preload(context: Context) {
        Thread { try { ensureReady(context) } catch (_: Exception) { } }.start()
    }

    private fun doEnsureReady(context: Context): NodeRuntimePaths? {
        val nativeDir = AndroidLinuxRuntime.nativeLibraryDir(context).absolutePath
        val launcherExe = File(nativeDir, "libnode_exec.so")
        if (!launcherExe.isFile) return null

        val modules = ensureModules(context) ?: return null
        val home = modules.parentFile?.parentFile ?: return null
        val marker = File(home, ".daidai-node-ready")
        if (!File(modules, "npm/bin/npm-cli.js").isFile) return null

        val compatLibDir = File(home, "compat-lib")
        AndroidLinuxRuntime.copyVersionedLibraries(File(nativeDir), compatLibDir, VERSIONED_LIBS)

        val wrapper = AndroidLinuxRuntime.writeShellWrapper(
            output = File(home, "bin/node-wrapper.sh"),
            env = mapOf(
                "LD_LIBRARY_PATH" to "$compatLibDir:$nativeDir:${home}/lib:\$LD_LIBRARY_PATH",
                "NODE_PATH" to "${modules.absolutePath}:\${NODE_PATH:-}",
                "HOME" to home.absolutePath,
                "DAIDAI_RUNTIME_LANGUAGE" to "node",
                "NPM_CONFIG_CACHE" to DependencyStorage.npmCache(context.filesDir).absolutePath,
            ),
            executable = launcherExe,
        )

        marker.writeText("ready:$VERSION")

        return NodeRuntimePaths(
            executable = "/system/bin/sh",
            home = home.absolutePath,
            modules = modules.absolutePath,
        ).also { wrapperPath = wrapper.absolutePath }
    }

    @Volatile var wrapperPath: String = ""

    private fun moduleHome(context: Context): File =
        File(context.filesDir, "runtimes/node-$VERSION/usr/lib/node_modules")

    private fun typescriptReady(modules: File): Boolean =
        File(modules, "typescript/package.json").isFile &&
            File(modules, "typescript/lib/typescript.js").isFile

    private val VERSIONED_LIBS = mapOf(
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

    private fun extractZipAsset(context: Context, dest: File) {
        dest.mkdirs()
        context.assets.open(ASSET_ARCHIVE).use { input ->
            ZipInputStream(input).use { zis ->
                var entry = zis.nextEntry
                while (entry != null) {
                    val outFile = AndroidLinuxRuntime.zipEntryFile(dest, entry.name)
                    if (outFile == null) {
                        entry = zis.nextEntry
                        continue
                    }
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

    fun failureReason(): String = lastFailure
}
