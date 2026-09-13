package com.daidai.daidai_app.data.remote

interface PanelApi {
    suspend fun health(): HealthResponse
    suspend fun checkInit(): CheckInitResponse
    suspend fun init(request: AuthInitRequest): AuthResponse
    suspend fun login(request: AuthLoginRequest): AuthResponse
}
