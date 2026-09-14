package com.daidai.daidai_app

import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.content.FileProvider
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest

/**
 * 阶段5-A1：从 MainActivity 抽离的纯 Kotlin 组件，封装 APK 安装与增量更新逻辑。
 * 不依赖任何 Flutter API，仅使用 Android 与系统 API，可在任意 Context 上使用。
 */
object ApkInstaller {

    /**
     * 安全安装 APK：
     *  - 仅接受 cacheDir/updates 目录内的 .apk 文件
     *  - 校验 APK 包名与当前应用一致
     *  - 校验归档版本号严格高于当前已安装版本
     * 通过 [FileProvider] 生成 content:// URI 后以系统安装器发起安装。
     * 任一步失败均抛异常，由调用方决定如何映射到错误码。
     */
    fun installApk(context: Context, path: String) {
        val apkFile = File(path)
        if (!apkFile.exists()) {
            throw Exception("APK file not found: $path")
        }
        val updateDir = File(context.cacheDir, "updates").canonicalFile
        val canonicalApk = apkFile.canonicalFile
        if (canonicalApk.parentFile != updateDir || !canonicalApk.name.endsWith(".apk")) {
            throw SecurityException("APK file is outside the update directory")
        }
        val packageManager = context.packageManager
        val packageName = context.packageName
        val archiveInfo = packageManager.getPackageArchiveInfo(canonicalApk.absolutePath, 0)
            ?: throw SecurityException("APK package information is invalid")
        if (archiveInfo.packageName != packageName) {
            throw SecurityException("APK package name does not match this application")
        }
        val archiveVersionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            archiveInfo.longVersionCode
        } else {
            @Suppress("DEPRECATION")
            archiveInfo.versionCode.toLong()
        }
        val currentInfo = packageManager.getPackageInfo(packageName, 0)
        val currentVersionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            currentInfo.longVersionCode
        } else {
            @Suppress("DEPRECATION")
            currentInfo.versionCode.toLong()
        }
        if (archiveVersionCode <= currentVersionCode) {
            throw SecurityException("APK version is not newer than the installed version")
        }

        val authority = "${context.packageName}.fileProvider"
        val apkUri = FileProvider.getUriForFile(context, authority, canonicalApk)

        val intent = Intent(Intent.ACTION_INSTALL_PACKAGE).apply {
            data = apkUri
            flags = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK
            putExtra(Intent.EXTRA_NOT_UNKNOWN_SOURCE, true)
            putExtra(Intent.EXTRA_RETURN_RESULT, true)
        }
        context.startActivity(intent)
    }

    /**
     * 已安装应用的信息：
     * { packageName, versionName, versionCode, size, md5, sha256 }。
     * md5/sha256 按整份 APK 文件计算。
     */
    fun installedApkInfo(context: Context): JSONObject {
        val sourceApk = File(context.applicationInfo.sourceDir)
        val packageManager = context.packageManager
        val packageName = context.packageName
        val packageInfo = packageManager.getPackageInfo(packageName, 0)
        val versionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
            packageInfo.longVersionCode
        } else {
            @Suppress("DEPRECATION")
            packageInfo.versionCode.toLong()
        }
        return JSONObject().apply {
            put("packageName", packageName)
            put("versionName", packageInfo.versionName)
            put("versionCode", versionCode)
            put("size", sourceApk.length())
            put("md5", digest(sourceApk, "MD5"))
            put("sha256", digest(sourceApk, "SHA-256"))
        }
    }

    /** 当前运行期 ABI。 */
    fun currentAbi(): String = AndroidLinuxRuntime.currentAbi()

    /**
     * 对当前安装的应用应用 bsdiff 增量补丁：
     *  - 补丁与输出必须位于 cacheDir/updates 目录内
     *  - 输出必须是以 .apk 结尾的文件名
     * 返回生成的输出文件。
     */
    fun applyPatch(context: Context, patchPath: String, outputName: String): File {
        val updateDir = File(context.cacheDir, "updates").apply { mkdirs() }.canonicalFile
        val patchFile = File(patchPath).canonicalFile
        if (patchFile.parentFile != updateDir || !patchFile.exists()) {
            throw SecurityException("Patch file is outside the update directory")
        }
        val outputFile = File(updateDir, outputName).canonicalFile
        if (outputFile.parentFile != updateDir || !outputFile.name.endsWith(".apk")) {
            throw SecurityException("Output file is invalid")
        }
        if (outputFile.exists()) outputFile.delete()
        val status = AndroidBsPatch.patch(
            File(context.applicationInfo.sourceDir),
            patchFile,
            outputFile,
        )
        if (status != 0 || !outputFile.exists()) {
            throw IllegalStateException("bspatch failed with status $status")
        }
        return outputFile
    }

    private fun digest(file: File, algorithm: String): String {
        val digest = MessageDigest.getInstance(algorithm)
        file.inputStream().buffered().use { input ->
            val buffer = ByteArray(1024 * 1024)
            while (true) {
                val read = input.read(buffer)
                if (read <= 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }
}
