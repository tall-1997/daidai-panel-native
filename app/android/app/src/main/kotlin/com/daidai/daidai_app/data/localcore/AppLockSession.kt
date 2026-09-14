package com.daidai.daidai_app.data.localcore

import android.content.Context
import com.daidai.daidai_app.data.prefs.AppLockStore
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * 应用锁会话态（跨屏幕共享）：
 * - Activity 退后台（onPause）时若启用应用锁则置为锁定；
 * - 门禁验证通过后由 UI 调 [unlock] 复位。
 * 冷启动首帧默认未锁定，由 [lockIfEnabled] 在入口处按配置置锁。
 */
object AppLockSession {
    private val _locked = MutableStateFlow(false)
    val locked: StateFlow<Boolean> = _locked.asStateFlow()

    fun lockIfEnabled(context: Context) {
        if (AppLockStore.get(context.applicationContext).isEnabled()) {
            _locked.value = true
        }
    }

    fun unlock() {
        _locked.value = false
    }
}
