package com.daidai.daidai_app

import android.util.Log
import androidx.work.Configuration
import io.flutter.app.FlutterApplication

class DaidaiApplication : FlutterApplication(), Configuration.Provider {
    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder()
            .setMinimumLoggingLevel(Log.INFO)
            .build()
}
