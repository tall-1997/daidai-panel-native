package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 引导页（boot）：启动期会话探测（自动登录）期间的过渡屏。
 *
 * 探测逻辑在 AppNavHost 内完成：有有效会话直达 dashboard，否则按已保存的
 * 连接配置进入登录页 / 服务器配置页。探测期间展示加载态，并提供手动入口兜底。
 */
@Composable
fun BootScreen(
    isResolving: Boolean,
    onContinue: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            "呆呆面板",
            style = MaterialTheme.typography.headlineSmall,
            color = AppColors.primary,
        )
        if (isResolving) {
            CircularProgressIndicator(strokeWidth = 2.dp, modifier = Modifier.size(28.dp))
            Text(
                "正在恢复会话…",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        } else {
            Text(
                "尚未配置面板连接，开始初始化。",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (onContinue != null) {
                Button(onClick = onContinue) { Text("开始") }
            }
        }
    }
}
