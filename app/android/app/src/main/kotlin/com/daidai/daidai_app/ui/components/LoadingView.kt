package com.daidai.daidai_app.ui.components

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier

/**
 * 通用的居中加载指示器。
 *
 * 使用 Material3 的 [CircularProgressIndicator]，着色跟随主题主色
 * （阶段 2-5 全部共用主色 #10B981）。
 *
 * @param modifier 外层修饰符；默认填满父容器并居中。
 */
@Composable
fun LoadingView(
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
    }
}
