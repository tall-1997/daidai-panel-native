package com.daidai.daidai_app

import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.core.content.FileProvider
import androidx.core.content.ContextCompat
import io.flutter.embedding.android.FlutterActivity
import io.flutter.embedding.engine.FlutterEngine
import io.flutter.plugin.common.MethodChannel
import io.flutter.plugin.common.EventChannel
import com.yzq.bsdiff.BsDiffTool
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.Executors
import org.json.JSONArray
import org.json.JSONObject

class MainActivity : FlutterActivity() {
    private val ROOT_CHANNEL = "com.daidai.app/root"
    private val INSTALL_CHANNEL = "com.daidai.panel/app_install"
    private val LOCAL_HOST_CHANNEL = "com.daidai.panel/local_host"
    private val LOCAL_HOST_EVENTS = "com.daidai.panel/local_host/events"
    private val updateExecutor = Executors.newSingleThreadExecutor()
    private val localPanelClient by lazy { LocalPanelServiceClient(applicationContext) }
    @Volatile
    private var activityDestroyed = false
    private var localHostEventSink: EventChannel.EventSink? = null

    private var isRootChecked = false
    private var isRootAvailable = false

    override fun configureFlutterEngine(flutterEngine: FlutterEngine) {
        super.configureFlutterEngine(flutterEngine)

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, ROOT_CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "isRooted" -> {
                    result.success(isRooted())
                }
                "executeAsRoot" -> {
                    val command = call.argument<String>("command")
                    if (command != null) {
                        val output = executeAsRoot(command)
                        if (output.isSuccess) {
                            result.success(output.getOrNull())
                        } else {
                            result.error("ROOT_ERROR", output.exceptionOrNull()?.message, null)
                        }
                    } else {
                        result.error("INVALID_ARGS", "Command is required", null)
                    }
                }
                "readFileAsRoot" -> {
                    val path = call.argument<String>("path")
                    if (path != null) {
                        val content = readFileAsRoot(path)
                        if (content.isSuccess) {
                            result.success(content.getOrNull())
                        } else {
                            result.error("ROOT_ERROR", content.exceptionOrNull()?.message, null)
                        }
                    } else {
                        result.error("INVALID_ARGS", "Path is required", null)
                    }
                }
                "listDirectoryAsRoot" -> {
                    val path = call.argument<String>("path")
                    if (path != null) {
                        val entries = listDirectoryAsRoot(path)
                        if (entries.isSuccess) {
                            result.success(entries.getOrNull())
                        } else {
                            result.error("ROOT_ERROR", entries.exceptionOrNull()?.message, null)
                        }
                    } else {
                        result.error("INVALID_ARGS", "Path is required", null)
                    }
                }
                else -> {
                    result.notImplemented()
                }
            }
        }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, INSTALL_CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "installApk" -> {
                    val path = call.argument<String>("path")
                    if (path != null) {
                        try {
                            installApk(path)
                            result.success(null)
                        } catch (e: Exception) {
                            result.error("INSTALL_ERROR", e.message, null)
                        }
                    } else {
                        result.error("INVALID_ARGS", "Path is required", null)
                    }
                }
                "getInstalledApkInfo" -> {
                    updateExecutor.execute {
                        try {
                            val sourceApk = File(applicationInfo.sourceDir)
                            val packageInfo = packageManager.getPackageInfo(packageName, 0)
                            val versionCode = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) {
                                packageInfo.longVersionCode
                            } else {
                                @Suppress("DEPRECATION")
                                packageInfo.versionCode.toLong()
                            }
                            val info = mapOf(
                                "packageName" to packageName,
                                "versionName" to packageInfo.versionName,
                                "versionCode" to versionCode,
                                "size" to sourceApk.length(),
                                "md5" to digest(sourceApk, "MD5"),
                                "sha256" to digest(sourceApk, "SHA-256")
                            )
                            runOnUiThread { result.success(info) }
                        } catch (e: Exception) {
                            runOnUiThread { result.error("APK_INFO_ERROR", e.message, null) }
                        }
                    }
                }
                "applyPatch" -> {
                    val patchPath = call.argument<String>("patchPath")
                    val outputName = call.argument<String>("outputName")
                    if (patchPath.isNullOrBlank() || outputName.isNullOrBlank()) {
                        result.error("INVALID_ARGS", "Patch path and output name are required", null)
                        return@setMethodCallHandler
                    }
                    updateExecutor.execute {
                        try {
                            val updateDir = File(cacheDir, "updates").apply { mkdirs() }.canonicalFile
                            val patchFile = File(patchPath).canonicalFile
                            if (patchFile.parentFile != updateDir || !patchFile.exists()) {
                                throw SecurityException("Patch file is outside the update directory")
                            }
                            val outputFile = File(updateDir, outputName).canonicalFile
                            if (outputFile.parentFile != updateDir || !outputFile.name.endsWith(".apk")) {
                                throw SecurityException("Output file is invalid")
                            }
                            if (outputFile.exists()) outputFile.delete()
                            val status = BsDiffTool.patch(
                                applicationInfo.sourceDir,
                                patchFile.absolutePath,
                                outputFile.absolutePath
                            )
                            if (status != 0 || !outputFile.exists()) {
                                throw IllegalStateException("bspatch failed with status $status")
                            }
                            runOnUiThread { result.success(mapOf("path" to outputFile.absolutePath)) }
                        } catch (e: Exception) {
                            runOnUiThread { result.error("PATCH_ERROR", e.message, null) }
                        }
                    }
                }
                else -> {
                    result.notImplemented()
                }
            }
        }

        MethodChannel(flutterEngine.dartExecutor.binaryMessenger, LOCAL_HOST_CHANNEL).setMethodCallHandler { call, result ->
            when (call.method) {
                "ensureStarted" -> invokeLocalPanel(result, localPanelClient::ensureStarted)
                "getStatus" -> invokeLocalPanel(result, localPanelClient::status)
                "restart" -> invokeLocalPanel(result, localPanelClient::restart, emitAfter = true)
                "stop" -> {
                    invokeLocalPanel(result, localPanelClient::stop, emitAfter = true)
                }
                "setPersistentSchedulingEnabled" -> {
                    val enabled = call.argument<Boolean>("enabled") == true
                    if (enabled) {
                        ContextCompat.startForegroundService(
                            this,
                            Intent(this, LocalPanelHostService::class.java).apply {
                                action = LocalPanelHostService.ACTION_ENABLE_PERSISTENT
                            },
                        )
                    }
                    invokeLocalPanel(
                        result,
                        { callback ->
                            localPanelClient.setPersistentSchedulingEnabled(enabled, callback)
                        },
                        emitAfter = true,
                    )
                }
                else -> result.notImplemented()
            }
        }

        EventChannel(flutterEngine.dartExecutor.binaryMessenger, LOCAL_HOST_EVENTS).setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, events: EventChannel.EventSink?) {
                    localHostEventSink = events
                    emitLocalHostStatus()
                }

                override fun onCancel(arguments: Any?) {
                    localHostEventSink = null
                }
            }
        )
    }

    override fun onDestroy() {
        activityDestroyed = true
        localPanelClient.close()
        updateExecutor.shutdownNow()
        super.onDestroy()
    }

    private fun invokeLocalPanel(
        channelResult: MethodChannel.Result,
        operation: ((Result<String>) -> Unit) -> Unit,
        emitAfter: Boolean = false,
    ) {
        operation { callResult ->
            runOnUiThread {
                if (activityDestroyed) return@runOnUiThread
                callResult.fold(
                    onSuccess = { raw ->
                        val status = localHostStatus(raw)
                        channelResult.success(status)
                        if (emitAfter) localHostEventSink?.success(status)
                    },
                    onFailure = {
                        channelResult.success(localHostFailure())
                    },
                )
            }
        }
    }

    private fun localHostStatus(raw: String): Map<String, Any> =
        jsonObjectToMap(runCatching { JSONObject(raw) }.getOrElse { JSONObject() })

    private fun localHostFailure(): Map<String, Any> = mapOf(
        "phase" to "failed",
        "base_url" to "",
        "instance_id" to "",
        "core_version" to "gomobile",
        "schema_version" to 0,
        "failure_stage" to "binder",
        "message" to "Embedded core unavailable",
        "foreground_service_enabled" to false,
    )

    private fun jsonObjectToMap(value: JSONObject): Map<String, Any?> =
        value.keys().asSequence().associateWith { key ->
            when (val item = value.opt(key)) {
                JSONObject.NULL, null -> null
                is JSONObject -> jsonObjectToMap(item)
                is JSONArray -> jsonArrayToList(item)
                else -> item
            }
        }

    private fun jsonArrayToList(value: JSONArray): List<Any?> =
        (0 until value.length()).map { index ->
            when (val item = value.opt(index)) {
                JSONObject.NULL, null -> null
                is JSONObject -> jsonObjectToMap(item)
                is JSONArray -> jsonArrayToList(item)
                else -> item
            }
        }

    private fun emitLocalHostStatus() {
        localPanelClient.status { callResult ->
            runOnUiThread {
                if (activityDestroyed) return@runOnUiThread
                localHostEventSink?.success(
                    callResult.fold(
                        onSuccess = ::localHostStatus,
                        onFailure = { localHostFailure() },
                    )
                )
            }
        }
    }

    private fun installApk(path: String) {
        val apkFile = File(path)
        if (!apkFile.exists()) {
            throw Exception("APK file not found: $path")
        }
        val updateDir = File(cacheDir, "updates").canonicalFile
        val canonicalApk = apkFile.canonicalFile
        if (canonicalApk.parentFile != updateDir || !canonicalApk.name.endsWith(".apk")) {
            throw SecurityException("APK file is outside the update directory")
        }
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

        val authority = "${applicationContext.packageName}.fileProvider"
        val apkUri = FileProvider.getUriForFile(this, authority, canonicalApk)

        val intent = Intent(Intent.ACTION_INSTALL_PACKAGE).apply {
            data = apkUri
            flags = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK
            putExtra(Intent.EXTRA_NOT_UNKNOWN_SOURCE, true)
            putExtra(Intent.EXTRA_RETURN_RESULT, true)
        }
        startActivity(intent)
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

    private fun isRooted(): Boolean {
        if (isRootChecked) return isRootAvailable

        isRootChecked = true
        isRootAvailable = checkRootAccess()
        return isRootAvailable
    }

    private fun checkRootAccess(): Boolean {
        return try {
            val suPaths = listOf(
                "/system/bin/su",
                "/system/xbin/su",
                "/sbin/su",
                "/data/local/xbin/su",
                "/data/local/bin/su",
                "/system/sd/xbin/su",
                "/system/bin/failsafe/su",
                "/data/local/su",
                "/su/bin/su",
                "/system/app/Superuser.apk",
                "/system/app/SuperSU.apk",
                "/system/app/SuperSU/SuperSU.apk"
            )

            val suExists = suPaths.any { File(it).exists() }
            if (suExists) {
                try {
                    val process = Runtime.getRuntime().exec(arrayOf("su", "-c", "id"))
                    val result = process.inputStream.bufferedReader().readText()
                    process.waitFor()
                    result.contains("uid=0")
                } catch (e: Exception) {
                    suExists
                }
            } else {
                false
            }
        } catch (e: Exception) {
            false
        }
    }

    private fun executeAsRoot(command: String): Result<String> {
        return try {
            val process = Runtime.getRuntime().exec(arrayOf("su", "-c", command))
            val output = process.inputStream.bufferedReader().readText()
            val error = process.errorStream.bufferedReader().readText()
            val exitCode = process.waitFor()

            if (exitCode == 0) {
                Result.success(output.trim())
            } else {
                Result.failure(Exception("Root command failed: $error"))
            }
        } catch (e: Exception) {
            Result.failure(e)
        }
    }

    private fun readFileAsRoot(path: String): Result<String> {
        return executeAsRoot("cat $path")
    }

    private fun listDirectoryAsRoot(path: String): Result<List<String>> {
        return executeAsRoot("ls -la $path").map { output ->
            output.lines().filter { it.isNotBlank() }
        }
    }
}
