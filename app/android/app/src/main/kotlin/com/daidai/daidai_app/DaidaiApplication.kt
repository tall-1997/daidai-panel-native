package com.daidai.daidai_app

import android.app.Application
import android.util.Log
import androidx.work.Configuration
import com.daidai.daidai_app.di.AppServices

class DaidaiApplication : Application(), Configuration.Provider {
    override fun onCreate() {
        super.onCreate()
        // 阶段 1 装配：初始化本地面板控制台与配置存储，供 Login/ServerConfig 默认实现使用。
        AppServices.initialize(this)
    }

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder()
            .setMinimumLoggingLevel(Log.INFO)
            .build()
}
