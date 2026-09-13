package com.daidai.daidai_app.ui.screens

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier

/**
 * 「更多」页占位（more）。
 * 承载设置 / 关于 / 恢复诊断等入口。
 */
@Composable
fun MoreScreen(modifier: Modifier = Modifier) {
    PlaceholderPage(
        title = "More Screen（更多）",
        targetModule = "目标模块：设置 / 关于 / 恢复诊断",
        modifier = modifier,
    )
}