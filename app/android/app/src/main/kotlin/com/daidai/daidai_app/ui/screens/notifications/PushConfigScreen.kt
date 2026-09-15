package com.daidai.daidai_app.ui.screens.notifications

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
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.NotificationChannelType
import com.daidai.daidai_app.data.model.NotificationFieldSpec
import com.daidai.daidai_app.data.prefs.PushChannelPrefs
import com.daidai.daidai_app.data.repository.NotificationsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 推送配置页（服务端推送渠道配置 UI）。
 *
 * 本页与「本机通知设置」([LocalNotificationSettingsScreen]) 相互独立：
 * 本页配置的是【服务端推送渠道】（22 渠道：webhook/email/telegram/dingtalk/
 * wecom/wecom_app/bark/pushplus/serverchan/feishu/gotify/pushdeer/pushme/
 * chanify/igot/qmsg/pushover/discord/slack/ntfy/wxpusher/custom）的接入参数，
 * 以及任务结果/系统事件的推送开关与自定义模板；LocalNotification 管本机系统
 * 通知总开关。两者各自持久化、互不冲突。
 *
 * 布局：
 *  1. 全局开关：任务成功 / 任务失败 / 系统事件告警；
 *  2. 自定义推送模板（{{title}} / {{content}} 占位符）；
 *  3. 22 渠道配置卡片（点入后按 schema 渲染必填字段，草稿持久化在
 *     [PushChannelPrefs]）；每个渠道卡提供「发送测试」按钮 —— 发送依赖后端
 *     推送服务，本地 fallback 无法发送时在反馈中标明。
 */
@Composable
fun PushConfigScreen(
    modifier: Modifier = Modifier,
    prefs: PushChannelPrefs? = null,
    viewModel: PushConfigViewModel? = null,
    repository: NotificationsRepository? = null,
    onBack: () -> Unit = {},
) {
    val context = LocalContext.current
    val resolvedPrefs = prefs ?: remember { PushChannelPrefs.getInstance(context) }
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val repo = repository ?: defaultPushNotificationsRepository(context)
        PushConfigViewModel(resolvedPrefs, sendTestRequest = repo::sendTestForType)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TextButton(onClick = onBack) {
                Text("返回")
            }
            Spacer(Modifier.width(4.dp))
            Text(
                text = "服务端推送渠道配置",
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
                modifier = Modifier.weight(1f),
            )
        }
        Text(
            text = "配置面板向外部渠道（企业微信 / 钉钉 / Telegram 等 22 种）推送通知的接入参数。" +
                "与本机系统通知（通知列表页上方开关）相互独立，两者可同时开启。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        GlobalSwitchCard(
            title = "任务执行成功通知",
            subtitle = "任务执行成功时推送到已配置的渠道",
            checked = state.notifyOnSuccess,
            onToggle = screenViewModel::setNotifyOnSuccess,
        )
        GlobalSwitchCard(
            title = "任务执行失败通知",
            subtitle = "任务执行失败时推送到已配置的渠道",
            checked = state.notifyOnFailure,
            onToggle = screenViewModel::setNotifyOnFailure,
        )
        GlobalSwitchCard(
            title = "系统事件告警",
            subtitle = "资源告警、登录通知、更新结果等系统事件推送",
            checked = state.notifySystemEvents,
            onToggle = screenViewModel::setNotifySystemEvents,
        )

        CustomTemplateCard(
            template = state.customTemplate,
            onTemplateChange = screenViewModel::setCustomTemplate,
        )

        Text(
            text = "推送渠道（${state.channelTypes.size} 种）",
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            color = MaterialTheme.colorScheme.onSurface,
        )
        state.channelTypes.forEach { type ->
            ChannelConfigCard(
                type = type,
                draft = state.drafts[type.type].orEmpty(),
                sending = state.sendingTest,
                onDraftChange = { values -> screenViewModel.updateDraft(type.type, values) },
                onClear = { screenViewModel.clearDraft(type.type) },
                onSendTest = { config -> screenViewModel.sendTest(type.type, config) },
            )
        }

        state.testMessage?.let { msg ->
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(8.dp))
                    .background(
                        if (msg.startsWith("测试发送失败")) {
                            AppColors.errorColor.copy(alpha = 0.1f)
                        } else {
                            AppColors.primary.copy(alpha = 0.1f)
                        }
                    )
                    .padding(12.dp),
            ) {
                Text(
                    text = msg,
                    style = MaterialTheme.typography.bodySmall,
                    color = if (msg.startsWith("测试发送失败")) {
                        AppColors.errorColor
                    } else {
                        AppColors.primary
                    },
                )
            }
        }

        Spacer(Modifier.height(16.dp))
    }
}

/** 全局开关卡片：任务成功 / 任务失败 / 系统事件。 */
@Composable
private fun GlobalSwitchCard(
    title: String,
    subtitle: String,
    checked: Boolean,
    onToggle: (Boolean) -> Unit,
) {
    CardView {
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = title,
                    style = MaterialTheme.typography.titleMedium,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.size(2.dp))
                Text(
                    text = subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(Modifier.width(12.dp))
            Switch(checked = checked, onCheckedChange = onToggle)
        }
    }
}

/** 自定义推送模板卡片。 */
@Composable
private fun CustomTemplateCard(
    template: String,
    onTemplateChange: (String) -> Unit,
) {
    CardView(title = "自定义推送模板") {
        Text(
            text = "使用 {{title}} 和 {{content}} 作为占位符，留空使用渠道默认模板。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.size(8.dp))
        OutlinedTextField(
            value = template,
            onValueChange = onTemplateChange,
            modifier = Modifier.fillMaxWidth(),
            placeholder = { Text("例如：任务「{{title}}」执行结果：{{content}}") },
            minLines = 2,
            maxLines = 4,
        )
    }
}

/**
 * 单个渠道配置卡片。
 *
 * 按 schema 渲染该渠道的必填字段（输入框 / 密码框 / 文本域 / 下拉），
 * 草稿实时持久化；提供「发送测试」按钮（依赖后端推送服务）。
 */
@Composable
private fun ChannelConfigCard(
    type: NotificationChannelType,
    draft: Map<String, String>,
    sending: Boolean,
    onDraftChange: (Map<String, String>) -> Unit,
    onClear: () -> Unit,
    onSendTest: (Map<String, String>) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }

    CardView(
        title = type.name,
        onClick = { expanded = !expanded },
    ) {
        if (!expanded) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "类型：${type.type}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = if (draft.isNotEmpty()) "已配置" else "未配置",
                    style = MaterialTheme.typography.labelMedium,
                    color = if (draft.isNotEmpty()) AppColors.successColor else AppColors.slate500,
                )
            }
        } else {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = "类型：${type.type}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                if (type.fields.isEmpty()) {
                    Text(
                        text = "该渠道无需配置参数。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                } else {
                    type.fields.forEach { field ->
                        ChannelFieldInput(
                            field = field,
                            value = draft[field.key].orEmpty(),
                            onValueChange = { newValue ->
                                onDraftChange(mapOf(field.key to newValue))
                            },
                        )
                    }
                }
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Button(
                        onClick = { onSendTest(draft) },
                        enabled = !sending,
                    ) {
                        if (sending) {
                            CircularProgressIndicator(
                                modifier = Modifier.size(16.dp),
                                strokeWidth = 2.dp,
                            )
                        } else {
                            Text("发送测试")
                        }
                    }
                    OutlinedButton(onClick = onClear) {
                        Text("清空草稿")
                    }
                }
                Text(
                    text = "发送测试依赖后端推送服务；本地 fallback 发送能力有限时会在反馈中标明。",
                    style = MaterialTheme.typography.bodySmall,
                    color = AppColors.slate500,
                )
            }
        }
    }
}

/**
 * 按 schema 渲染单个配置字段：input / password / textarea / select。
 *
 * select 使用 ExposedDropdownMenuBox 展示选项；textarea 使用多行文本域。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ChannelFieldInput(
    field: NotificationFieldSpec,
    value: String,
    onValueChange: (String) -> Unit,
) {
    when (field.widget) {
        "select" -> SelectField(field = field, value = value, onValueChange = onValueChange)
        "textarea" -> OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier.fillMaxWidth(),
            label = { Text(field.label) },
            placeholder = { Text(field.placeholder) },
            minLines = 2,
            maxLines = 5,
        )
        else -> OutlinedTextField(
            value = value,
            onValueChange = onValueChange,
            modifier = Modifier.fillMaxWidth(),
            label = { Text(field.label) },
            placeholder = { Text(field.placeholder) },
            singleLine = true,
        )
    }
}

/** select 下拉字段。 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SelectField(
    field: NotificationFieldSpec,
    value: String,
    onValueChange: (String) -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val currentLabel = field.options.firstOrNull { it.value == value }?.label ?: value

    ExposedDropdownMenuBox(
        expanded = expanded,
        onExpandedChange = { expanded = it },
    ) {
        OutlinedTextField(
            value = if (value.isBlank()) "" else currentLabel,
            onValueChange = {},
            readOnly = true,
            modifier = Modifier
                .fillMaxWidth()
                .menuAnchor(),
            label = { Text(field.label) },
            placeholder = { Text(field.placeholder) },
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = expanded) },
        )
        ExposedDropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
        ) {
            // 空值选项：清空（服务端留空走默认行为）。
            DropdownMenuItem(
                text = { Text(if (field.default.isBlank()) "（留空，使用默认）" else "（留空，默认：${field.default}）") },
                onClick = {
                    onValueChange("")
                    expanded = false
                },
            )
            field.options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.label) },
                    onClick = {
                        onValueChange(option.value)
                        expanded = false
                    },
                )
            }
        }
    }
}

/** 默认装配：从既有配置存储读取 serverUrl + accessToken 构造仓库。 */
private fun defaultPushNotificationsRepository(context: android.content.Context): NotificationsRepository {
    val config = AppServices.configRepository(context).config.value
    return NotificationsRepository(
        baseUrl = config.serverUrl,
        accessToken = config.accessToken,
    )
}
