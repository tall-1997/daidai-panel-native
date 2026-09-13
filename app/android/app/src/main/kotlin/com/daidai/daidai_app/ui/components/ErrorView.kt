package com.daidai.daidai_app.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 通用的错误占位视图：错误图标 + 文案 + 可选重试按钮。
 *
 * @param message  向用户展示的错误信息。
 * @param onRetry  重试回调；非 null 时显示"重试"按钮。
 * @param modifier 外层修饰符；默认填满父容器并居中。
 */
@Composable
fun ErrorView(
    message: String,
    onRetry: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(
            imageVector = Icons.Filled.Warning,
            contentDescription = null,
            tint = AppColors.errorColor,
            modifier = Modifier.padding(bottom = 4.dp),
        )
        Text(
            text = message,
            style = MaterialTheme.typography.bodyMedium,
            color = AppColors.errorColor,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
        if (onRetry != null) {
            Button(onClick = onRetry) {
                Text("重试")
            }
        }
    }
}
