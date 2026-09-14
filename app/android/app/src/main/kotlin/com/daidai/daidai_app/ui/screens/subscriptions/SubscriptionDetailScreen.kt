package com.daidai.daidai_app.ui.screens.subscriptions

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.Subscription
import com.daidai.daidai_app.data.model.SubscriptionWritePayload
import com.daidai.daidai_app.data.repository.SubscriptionsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 订阅详情 / 新建编辑表单（Compose 原生，阶段 3-5）。
 *
 * 可编辑关键字段：名称 / 地址(Git URL) / 目标路径 / 是否启用。提交后经
 * [SubscriptionsRepository] 写入后端：
 *  - [subscription] 为 null 时为新建（POST /api/subscriptions）
 *  - [subscription] 非 null 时为更新（PUT /api/subscriptions/:id）
 *
 * 不接导航：与列表页共享同一个 [SubscriptionsViewModel]（保持单一数据源），
 * 保存成功后经 [onSaved] 回调交回列表页；顶部 [onBack] 用于返回。
 */
@Composable
fun SubscriptionDetailScreen(
    modifier: Modifier = Modifier,
    subscription: Subscription? = null,
    repository: SubscriptionsRepository? = null,
    viewModel: SubscriptionsViewModel? = null,
    onBack: () -> Unit = {},
    onSaved: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: SubscriptionsRepository(context)
        SubscriptionsViewModel(resolved)
    })

    // 表单初始值：编辑态来自订阅对象，新建态为空。
    var name by remember { mutableStateOf(subscription?.name ?: "") }
    var url by remember { mutableStateOf(subscription?.url ?: "") }
    var targetPath by remember { mutableStateOf(subscription?.targetPath ?: "") }
    var enabled by remember { mutableStateOf(subscription?.enabled ?: true) }
    var saving by remember { mutableStateOf(false) }
    var errorMessage by remember { mutableStateOf<String?>(null) }

    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    // 提交后监听结果：失败显示内联错误；成功回调 onSaved（交回列表页）。
    LaunchedEffect(state.errorMessage) {
        if (saving && state.errorMessage != null) {
            errorMessage = state.errorMessage
            saving = false
        }
    }
    LaunchedEffect(state.mutationSucceeded) {
        if (saving && state.mutationSucceeded) {
            saving = false
            screenViewModel.clearError()
            onSaved()
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        // 顶部栏：返回 + 标题
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 8.dp, vertical = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconButton(onClick = onBack, enabled = !saving, modifier = Modifier.width(40.dp)) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回", tint = AppColors.slate600)
            }
            Text(
                text = if (subscription == null) "新建订阅" else "订阅详情",
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
            )
        }

        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 20.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("订阅名称") },
                placeholder = { Text("例如：我的订阅仓库") },
                supportingText = { Text("用于在列表中识别的订阅名称") },
            )

            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("仓库地址") },
                placeholder = { Text("例如：https://example.com/repo.git") },
                supportingText = { Text("Git 仓库地址，订阅的更新来源") },
            )

            OutlinedTextField(
                value = targetPath,
                onValueChange = { targetPath = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("目标路径") },
                placeholder = { Text("可选，例如：/data/subscriptions/my-sub") },
                supportingText = { Text("更新内容落地到面板的目录（save_dir）") },
            )

            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = "启用订阅",
                    style = MaterialTheme.typography.bodyMedium,
                    color = AppColors.slate700,
                    modifier = Modifier.weight(1f),
                )
                Switch(
                    checked = enabled,
                    onCheckedChange = { enabled = it },
                    colors = SwitchDefaults.colors(
                        checkedTrackColor = AppColors.primary,
                    ),
                )
            }

            errorMessage?.let {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(AppColors.errorColor.copy(alpha = 0.12f))
                        .padding(horizontal = 12.dp, vertical = 8.dp),
                ) {
                    Text(
                        text = it,
                        style = MaterialTheme.typography.bodySmall,
                        color = AppColors.errorColor,
                    )
                }
            }

            Button(
                onClick = {
                    val trimmedName = name.trim()
                    val trimmedUrl = url.trim()
                    if (trimmedName.isEmpty()) {
                        errorMessage = "请输入订阅名称"
                        return@Button
                    }
                    if (trimmedUrl.isEmpty()) {
                        errorMessage = "请输入仓库地址"
                        return@Button
                    }
                    saving = true
                    errorMessage = null
                    val payload = SubscriptionWritePayload(
                        name = trimmedName,
                        url = trimmedUrl,
                        targetPath = targetPath.trim(),
                        enabled = enabled,
                    )
                    if (subscription == null) {
                        screenViewModel.create(payload)
                    } else {
                        screenViewModel.update(subscription.id, payload)
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                enabled = !saving,
            ) {
                if (saving) {
                    CircularProgressIndicator(
                        modifier = Modifier.width(16.dp).height(16.dp),
                        color = AppColors.primary,
                    )
                    Spacer(Modifier.width(8.dp))
                    Text("保存中…")
                } else {
                    Text(if (subscription == null) "创建订阅" else "保存修改")
                }
            }
        }
    }
}
