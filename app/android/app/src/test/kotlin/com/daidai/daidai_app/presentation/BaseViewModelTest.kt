package com.daidai.daidai_app.presentation

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 覆盖 BaseViewModel 提供的便捷状态写入方法
 * （loading / success / error）以及默认初始态。
 */
class BaseViewModelTest {

    /** 通过 public 转发方法暴露 BaseViewModel 的 protected 状态写入 API，供测试驱动。 */
    private class DummyViewModel : BaseViewModel<String>() {
        fun setLoading() = loading()
        fun setSuccess(data: String) = success(data)
        fun setError(message: String) = error(message)
    }

    @Test
    fun `starts in Loading state`() {
        val vm = DummyViewModel()
        assertTrue(vm.uiState.value is UiState.Loading)
    }

    @Test
    fun `loading convenience switches to Loading`() {
        val vm = DummyViewModel()
        vm.setSuccess("data")
        assertTrue(vm.uiState.value is UiState.Success)

        vm.setLoading()
        assertTrue(vm.uiState.value is UiState.Loading)
    }

    @Test
    fun `success convenience carries data`() {
        val vm = DummyViewModel()
        vm.setSuccess("hello")
        assertEquals(UiState.Success("hello"), vm.uiState.value)
    }

    @Test
    fun `error convenience carries message`() {
        val vm = DummyViewModel()
        vm.setError("加载失败")
        assertEquals(UiState.Error("加载失败"), vm.uiState.value)
    }
}