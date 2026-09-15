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
import androidx.compose.material3.FilterChip
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
 * 可编辑字段：名称 / 地址(Git URL) / 目标路径 / 是否启用，以及规则字段：
 * 分支(branch) / 定时同步(schedule cron) / 白名单(whitelist) / 黑名单(blacklist) /
 * 依赖规则(depend_on) / 仓库子路径(sub_path) / 别名(alias) / 鉴权方式
 * （auth_type + 用户名 + SSH 密钥 ID 或访问令牌）。提交后经
 * [SubscriptionsRepository] 写入后端：
 *  - [subscription] 为 null 时为新建（POST /api/subscriptions）
 *  - [subscription] 非 null 时为更新（PUT /api/subscriptions/:id）
 *
 * 规则语义与 Go 后端 panel/server/service/subscription.go 对齐：`,` 或 `|`
 * 分隔、子串包含匹配；白名单空 = 全部命中；黑名单对白名单与依赖规则都生效；
 * 依赖规则（depend_on）参与检出并落盘但不建任务。
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
    var branch by remember { mutableStateOf(subscription?.branch ?: "") }
    var schedule by remember { mutableStateOf(subscription?.schedule ?: "") }
    var whitelist by remember { mutableStateOf(subscription?.whitelist ?: "") }
    var blacklist by remember { mutableStateOf(subscription?.blacklist ?: "") }
    var dependOn by remember { mutableStateOf(subscription?.dependOn ?: "") }
    var subPath by remember { mutableStateOf(subscription?.subPath ?: "") }
    var alias by remember { mutableStateOf(subscription?.alias ?: "") }
    var authType by remember { mutableStateOf(subscription?.authType ?: "") }
    var authUsername by remember { mutableStateOf(subscription?.authUsername ?: "") }
    var sshKeyId by remember { mutableStateOf(subscription?.sshKeyId) }
    var authToken by remember { mutableStateOf("") }
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

            OutlinedTextField(
                value = branch,
                onValueChange = { branch = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("分支") },
                placeholder = { Text("可选，例如：main") },
                supportingText = { Text("检出仓库分支；留空使用仓库默认分支") },
            )

            OutlinedTextField(
                value = schedule,
                onValueChange = { schedule = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("定时同步") },
                placeholder = { Text("可选，例如：0 */6 * * *") },
                supportingText = { Text("cron 表达式；留空表示不自动同步。标准 5 段格式") },
            )

            OutlinedTextField(
                value = whitelist,
                onValueChange = { whitelist = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("白名单") },
                placeholder = { Text("例如：scripts/utils, txt；`,` 或 `|` 分隔") },
                supportingText = { Text("命中规则的文件参与检出并建任务；留空 = 全部命中。子串包含匹配，非正则") },
            )

            OutlinedTextField(
                value = blacklist,
                onValueChange = { blacklist = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("黑名单") },
                placeholder = { Text("例如：node_modules, test") },
                supportingText = { Text("命中规则的文件被排除，对白名单与依赖规则都生效") },
            )

            OutlinedTextField(
                value = dependOn,
                onValueChange = { dependOn = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("依赖规则") },
                placeholder = { Text("例如：utils, sendNotify") },
                supportingText = { Text("命中规则的文件参与检出并落盘，但不会据此建定时任务") },
            )

            OutlinedTextField(
                value = subPath,
                onValueChange = { subPath = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("仓库子路径") },
                placeholder = { Text("可选，例如：scripts/") },
                supportingText = { Text("只检出仓库内的子目录（sub_path）") },
            )

            OutlinedTextField(
                value = alias,
                onValueChange = { alias = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("别名") },
                placeholder = { Text("可选") },
                supportingText = { Text("订阅显示别名") },
            )

            Text(
                text = "仓库鉴权",
                style = MaterialTheme.typography.titleSmall,
                color = AppColors.slate700,
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                androidx.compose.material3.FilterChip(
                    selected = authType.isEmpty(),
                    onClick = { authType = "" },
                    label = { Text("无") },
                )
                Spacer(Modifier.width(8.dp))
                androidx.compose.material3.FilterChip(
                    selected = authType == "token",
                    onClick = { authType = if (authType == "token") "" else "token" },
                    label = { Text("Token") },
                )
                Spacer(Modifier.width(8.dp))
                androidx.compose.material3.FilterChip(
                    selected = authType == "ssh",
                    onClick = { authType = if (authType == "ssh") "" else "ssh" },
                    label = { Text("SSH") },
                )
            }
            if (authType == "token") {
                OutlinedTextField(
                    value = authUsername,
                    onValueChange = { authUsername = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("令牌用户名") },
                    placeholder = { Text("可选") },
                )
                OutlinedTextField(
                    value = authToken,
                    onValueChange = { authToken = it },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("访问令牌") },
                    supportingText = {
                        if (subscription?.hasAuthToken == true && authToken.isBlank()) {
                            Text("已保存一个令牌；留空保持原值，填写则覆盖")
                        } else {
                            Text("保存时写入后端加密存储，不回读明文")
                        }
                    },
                    singleLine = false,
                    minLines = 2,
                )
            }
            if (authType == "ssh") {
                OutlinedTextField(
                    value = sshKeyId?.toString() ?: "",
                    onValueChange = { input ->
                        sshKeyId = input.trim().toLongOrNull()
                    },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("SSH 密钥 ID") },
                    placeholder = { Text("例如：1") },
                    supportingText = { Text("引用“安全 > SSH 密钥”中已录入密钥的 ID（ssh_key_id）；留空表示不使用") },
                )
            }

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
                    val trimmedAuthType = when (authType.trim().lowercase()) {
                        "token" -> "token"
                        "ssh" -> "ssh"
                        else -> ""
                    }
                    if (trimmedAuthType == "token" && authToken.trim().isEmpty() && subscription?.hasAuthToken != true) {
                        errorMessage = "选择 Token 鉴权时请填写访问令牌"
                        return@Button
                    }
                    val currentSshKeyId = sshKeyId
                    if (trimmedAuthType == "ssh" && (currentSshKeyId == null || currentSshKeyId <= 0L)) {
                        errorMessage = "选择 SSH 鉴权时请填写 SSH 密钥 ID"
                        return@Button
                    }
                    saving = true
                    errorMessage = null
                    val payload = SubscriptionWritePayload(
                        name = trimmedName,
                        url = trimmedUrl,
                        targetPath = targetPath.trim(),
                        enabled = enabled,
                        branch = branch.trim(),
                        schedule = schedule.trim(),
                        whitelist = whitelist.trim(),
                        blacklist = blacklist.trim(),
                        dependOn = dependOn.trim(),
                        subPath = subPath.trim(),
                        alias = alias.trim(),
                        authType = trimmedAuthType,
                        authUsername = authUsername.trim(),
                        authToken = authToken.trim(),
                        sshKeyId = sshKeyId,
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
