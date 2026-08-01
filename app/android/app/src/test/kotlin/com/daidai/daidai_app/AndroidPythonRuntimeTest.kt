package com.daidai.daidai_app

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class AndroidPythonRuntimeTest {
    @Test
    fun wheelCompatibilityAllowsPurePythonAndAndroidCp314() {
        assertTrue(AndroidPythonRuntime.isCompatibleWheel("requests-2.34.2-py3-none-any.whl"))
        assertTrue(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-cp314-android_28_arm64_v8a.whl"))
        assertTrue(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-abi3-android_35_arm64_v8a.whl"))
    }

    @Test
    fun wheelCompatibilityRejectsForeignPythonAndPlatforms() {
        assertFalse(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp312-cp312-android_28_arm64_v8a.whl"))
        assertFalse(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-cp314-manylinux_2_17_x86_64.whl"))
        assertFalse(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-cp314-linux_aarch64.whl"))
        assertFalse(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-cp314-musllinux_1_2_aarch64.whl"))
        assertFalse(AndroidPythonRuntime.isCompatibleWheel("native-1.0-cp314-cp314-android_28_x86_64.whl"))
    }
}
