package com.daidai.daidai_app.ui.components

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * 可复用的卡片容器（封装 Material3 [Card]）：可选标题 + 任意子内容 + 可选点击。
 *
 * @param title   可选的卡片标题；为 null 时不渲染标题行。
 * @param modifier 外层修饰符，透传给底层的 [Card]。
 * @param onClick 点击回调；非 null 时卡片变为可点击卡片。
 * @param content 卡片主体内容（标题之下的内容区域）。
 */
@Composable
fun CardView(
    title: String? = null,
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    content: @Composable () -> Unit,
) {
    val body: @Composable () -> Unit = {
        Column(modifier = Modifier
            .fillMaxWidth()
            .padding(16.dp)) {
            if (title != null) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.padding(bottom = 8.dp),
                )
            }
            content()
        }
    }

    if (onClick != null) {
        Card(onClick = onClick, modifier = modifier) { body() }
    } else {
        Card(modifier = modifier) { body() }
    }
}
