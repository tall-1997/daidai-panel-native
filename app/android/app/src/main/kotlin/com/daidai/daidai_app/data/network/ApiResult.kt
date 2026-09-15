package com.daidai.daidai_app.data.network

/** 网络请求结果封装，与 Flutter result 模式对齐。 */
sealed class ApiResult<out T> {
    data class Success<T>(val data: T) : ApiResult<T>()
    data class Error(val message: String?) : ApiResult<Nothing>()
    data class NetworkError(val message: String?) : ApiResult<Nothing>()
}
