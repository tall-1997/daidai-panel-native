package com.daidai.daidai_app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import com.daidai.daidai_app.data.localcore.AppLockSession
import com.daidai.daidai_app.ui.navigation.AppNavHost
import com.daidai.daidai_app.ui.theme.AppTheme

/**
 * Compose 原生入口 Activity（阶段 0）。
 *
 * 与现有 Flutter 入口 [MainActivity]（FlutterActivity）**并存**：
 *  - [MainActivity] 仍是 launcher / Flutter 入口，阶段 0 保留不动；
 *  - [NativeMainActivity] 声明为普通 Activity，不设 launcher，
 *    通过 `adb shell am start -n com.daidai.daidai_app/.NativeMainActivity` 显式验证。
 *
 * 阶段 1 将 Home 迁移到 Compose 后，可把 launcher 指向本 Activity。
 */
class NativeMainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        // 冷启动入口：启用应用锁时立即进入锁定态（门禁页由 AppNavHost 压栈）。
        AppLockSession.lockIfEnabled(this)
        setContent {
            AppTheme {
                AppNavHost()
            }
        }
    }

    override fun onPause() {
        super.onPause()
        // 对齐 Flutter 版：退到后台即重新锁定（下次回前台需通过门禁）。
        AppLockSession.lockIfEnabled(this)
    }
}
