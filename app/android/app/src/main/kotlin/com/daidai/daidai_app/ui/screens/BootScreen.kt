package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 引导页（boot）：托管本地启动流程（阶段 1）。
 */
@Composable
fun BootScreen(
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
            "Boot Screen（引导页）",
            style = MaterialTheme.typography.headlineSmall,
            color = AppColors.primary,
        )
        Text(
            "目标模块：boot 流程 → 托管本地启动（阶段 1）",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        if (onContinue != null) {
            Button(onClick = onContinue) { Text("开始") }
        }
    }
}
