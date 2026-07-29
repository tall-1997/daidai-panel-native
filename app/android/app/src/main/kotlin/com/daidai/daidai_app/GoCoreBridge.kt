package com.daidai.daidai_app

import android.content.Context
import android.util.Log
import org.json.JSONObject

object GoCoreBridge {
    private const val TAG = "GoCoreBridge"
    private const val STOP_TIMEOUT_MILLIS = 3_000L

    @Synchronized
    fun ensureStarted(context: Context, localToken: String): Map<String, Any> {
        val current = status(localToken)
        if (current["phase"] == "ready") return current

        val runtimeMetadata = AndroidRuntimeMetadataBridge.metadataOptions(context)
        val foregroundActive = LocalPanelHostService.isPersistentSchedulingEnabled(context)
        val hostStatus = AndroidSchedulerHostStatus.status(context, foregroundActive, recoveryTrigger = "app-start")
        val options = JSONObject()
            .put("dataDir", context.filesDir.resolve("local-panel").absolutePath)
            .put("bindHost", "127.0.0.1")
            .put("port", 0)
            .put("localToken", localToken)
            .put("nativeLibraryDir", context.applicationInfo.nativeLibraryDir)
            .put("androidKeystoreMasterKey", AndroidRuntimeSecretBridge.runtimeMasterKey(context))
            .put("runtimeManifestPath", runtimeMetadata.getValue("runtimeManifestPath"))
            .put("runtimeCompatibilityPath", runtimeMetadata.getValue("runtimeCompatibilityPath"))
            .put("runtimeSmokeEvidencePath", runtimeMetadata.getValue("runtimeSmokeEvidencePath"))
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
            .toString()
    }
}
