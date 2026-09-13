package com.daidai.daidai_app.presentation

import androidx.lifecycle.ViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 通用的异步 UI 状态，供各页面在 加载中 / 成功 / 失败 三种形态间切换。
 *
 * - [Loading]：加载中（无数据）。
 * - [Success]：已加载出数据 [data]。
 * - [Error]：失败，携带面向用户的 [message]。
 */
sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>
    data class Success<T>(val data: T) : UiState<T>
    data class Error(val message: String) : UiState<Nothing>
}

/**
 * 共享 ViewModel 基类：封装一个泛型 [UiState] 的 StateFlow 状态槽，
 * 并提供 loading / success / error 便捷写入方法。风格与阶段 1 的
 * LoginViewModel 一致（MutableStateFlow + asStateFlow + update）。
 *
 * 用法示例：
 * ```
 * class FooViewModel(private val repo: FooRepository) : BaseViewModel<Foo>() {
 *     fun load() {
 *         viewModelScope.launch {
 *             loading()
 *             try {
 *                 success(repo.fetch())
 *             } catch (e: Exception) {
 *                 error(e.message ?: "加载失败")
 *             }
 *         }
 *     }
 * }
 * ```
 *
 * @param T 成功态承载的数据类型。
 */
abstract class BaseViewModel<T> : ViewModel() {

    private val _uiState = MutableStateFlow<UiState<T>>(UiState.Loading)

    /** 对外只读的 UI 状态流，UI 通过 collect 观察并渲染。 */
    val uiState: StateFlow<UiState<T>> = _uiState.asStateFlow()

    /** 无条件覆盖为指定状态。 */
    protected fun setState(next: UiState<T>) {
        _uiState.value = next
    }

    /** 切换到加载中状态。 */
    protected fun loading() {
        _uiState.value = UiState.Loading
    }

    /** 携带数据进入成功状态。 */
    protected fun success(data: T) {
        _uiState.value = UiState.Success(data)
    }

    /** 携带错误说明进入失败状态。 */
    protected fun error(message: String) {
        _uiState.value = UiState.Error(message)
    }
}
