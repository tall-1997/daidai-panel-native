package com.daidai.daidai_app

import android.content.Context
import android.util.Log
import java.io.File
import org.json.JSONObject

object GoCoreBridge {
    private const val TAG = "GoCoreBridge"
    private const val STOP_TIMEOUT_MILLIS = 3_000L

    private fun isAndroidPlatformCompatible(): Boolean {
        return android.os.Build.VERSION.SDK_INT < 36 && AndroidLinuxRuntime.currentAbi() in setOf("armeabi-v7a", "arm64-v8a", "x86_64")
    }

    @Synchronized
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> {
        if (!isAndroidPlatformCompatible()) {
            Log.w(TAG, "Go core unsupported on Android SDK ${android.os.Build.VERSION.SDK_INT} (>=36)")
            return mapOf(
                "phase" to "failed",
                "base_url" to "",
                "instance_id" to "",
                "core_version" to "gomobile",
                "schema_version" to 0,
                "failure_stage" to "CORE_PLATFORM_INCOMPATIBLE",
                "go_core_error_type" to "AndroidSdkVersion",
                "go_core_root_error_type" to "SELinuxLinkRestriction",
                "message" to "Embedded Go core requires Android <=15 (SDK <36); use Kotlin fallback",
                "foreground_service_enabled" to false,
            )
        }

        val current = status(localToken)
        if (current["phase"] == "ready") return current

        val runtimeMetadata = AndroidRuntimeMetadataBridge.metadataOptions(context)
        val foregroundActive = LocalPanelHostService.isPersistentSchedulingEnabled(context)
        val hostStatus = AndroidSchedulerHostStatus.status(context, foregroundActive, recoveryTrigger = "app-start")
        val dataDir = File(context.filesDir, "local-panel").apply { mkdirs() }.canonicalPath
        val webDir = LocalWebAssets.ensureExtracted(context).canonicalPath
        val options = JSONObject()
            .put("dataDir", dataDir)
            .put("bindHost", "127.0.0.1")
            .put("port", 0)
            .put("localToken", localToken)
            .put("nativeLibraryDir", context.applicationInfo.nativeLibraryDir)
            .put("webDir", webDir)
            .put("androidKeystoreMasterKey", AndroidRuntimeSecretBridge.runtimeMasterKey(context))
            .put("runtimeManifestPath", runtimeMetadata.getValue("runtimeManifestPath"))
            .put("runtimeCompatibilityPath", runtimeMetadata.getValue("runtimeCompatibilityPath"))
            .put("runtimeSmokeEvidencePath", runtimeMetadata.getValue("runtimeSmokeEvidencePath"))
            .put("runtimeDependenciesPath", runtimeMetadata.getValue("runtimeDependenciesPath"))
            .put("platformCapabilities", AndroidSchedulerHostStatus.platformCapabilities(context))
            .put(
                "schedulerGuarantee",
                JSONObject()
                    .put("state", hostStatus["scheduler_guarantee_state"])
                    .put("reasonCode", hostStatus["scheduler_guarantee_reason"])
                    .put("intervention", hostStatus["scheduler_intervention"])
                    .put("source", "android-host"),
            )
            .toString()
        val raw = invokeString(GoCoreReflectionContract.START_CORE, arrayOf(String::class.java), arrayOf(options))
        return mapResultWithEndpoint(raw, localToken)
    }

    @Synchronized
    fun status(localToken: String): Map<String, Any> {
        val raw = invokeString(GoCoreReflectionContract.CORE_STATUS, emptyArray(), emptyArray())
        return mapResultWithEndpoint(raw, localToken)
    }

    @Synchronized
    fun restart(context: Context, localToken: String): Map<String, Any> {
        val stopped = stop(localToken)
        if (stopped["phase"] == "failed") return stopped
        return ensureStarted(context, localToken)
    }

    @Synchronized
    fun stop(localToken: String): Map<String, Any> {
        val raw = invokeString(
            GoCoreReflectionContract.STOP_CORE,
            arrayOf(java.lang.Long.TYPE),
            arrayOf(STOP_TIMEOUT_MILLIS),
        )
        return GoCoreResultMapper.toStatus(raw, localToken)
    }

    @Synchronized
    fun createBrowserUrl(): String = invokeString(
        GoCoreReflectionContract.CREATE_BROWSER_URL,
        emptyArray(),
        emptyArray(),
    ).also { value ->
        require(value.startsWith("http://127.0.0.1:")) { "Go core returned an invalid browser URL" }
    }

    private fun mapResultWithEndpoint(raw: String, localToken: String): Map<String, Any> {
        val result = runCatching { JSONObject(raw) }.getOrNull()
            ?: return GoCoreResultMapper.toStatus(raw, localToken)
        if (result.optBoolean("running")) {
            result.put("endpoint", invokeString(GoCoreReflectionContract.CORE_ENDPOINT, emptyArray(), emptyArray()))
        }
        return GoCoreResultMapper.toStatus(result.toString(), localToken)
    }

    private fun invokeString(
        methodName: String,
        parameterTypes: Array<Class<*>>,
        arguments: Array<out Any>,
    ): String = try {
        val coreClass = Class.forName(GoCoreReflectionContract.CLASS_NAME)
        coreClass.getMethod(methodName, *parameterTypes).invoke(null, *arguments) as String
    } catch (error: Throwable) {
        val root = if (error is java.lang.reflect.InvocationTargetException) error.targetException ?: error else error
        val code = when (error) {
            is ClassNotFoundException -> "CORE_CLASS_NOT_FOUND"
            is NoSuchMethodException -> "CORE_METHOD_NOT_FOUND"
            is UnsatisfiedLinkError -> "CORE_JNI_LOAD_FAILED"
            is java.lang.reflect.InvocationTargetException -> "CORE_INVOCATION_FAILED"
            else -> "CORE_UNAVAILABLE"
        }
        Log.e(TAG, "Go core invocation failed: method=$methodName code=$code type=${error.javaClass.simpleName}")
        JSONObject()
            .put("ok", false)
            .put("running", false)
            .put("status", "failed")
            .put("errorCode", code)
            .put("errorType", error.javaClass.simpleName)
            .put("rootErrorType", root.javaClass.simpleName)
            .toString()
    }
}
