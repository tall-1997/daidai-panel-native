package com.daidai.daidai_app.ui.screens.envs

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Button
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.repository.EnvsRepository
import com.daidai.daidai_app.data.repository.PanelEnvsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 环境变量编辑表单（Compose 原生，阶段 3-2）。
 *
 * 支持「新建」与「编辑」两种模式：新建传入 [EnvVar] 缺省空对象并调用
 * [onCreateFromValue]（缺省走 [EnvsViewModel.create]）；编辑传入现有 [env] 预填
 * 字段并调用 [onUpdateFromValue]（缺省走 [EnvsViewModel.update]）。
 *
 * 表单字段：名称 / 值 / 备注 / 是否加密。其中「是否加密」([EnvVar.secret]) 为
 * 纯 UI 掩码开关——开启后值输入框以密文占位显示、不落库（后端不持久化该字段）。
 *
 * **未接导航**，保存成功后经 [onSaved] 回调交由调用方关闭/跳转，本组件自包含可编译。
 */
@Composable
fun EnvFormScreen(
    modifier: Modifier = Modifier,
    env: EnvVar = EnvVar(),
    repository: EnvsRepository? = null,
    viewModel: EnvsViewModel? = null,
    title: String = if (env.id > 0L) "编辑环境变量" else "新建环境变量",
    onSaved: () -> Unit = {},
    onCancel: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            PanelEnvsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        EnvsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    var name by remember(env.id) { mutableStateOf(env.name) }
    var value by remember(env.id) { mutableStateOf(env.value) }
    var remark by remember(env.id) { mutableStateOf(env.remark) }
    var secret by remember(env.id) { mutableStateOf(env.secret) }
    var enabled by remember(env.id) { mutableStateOf(env.enabled) }
    var saving by remember { mutableStateOf(false) }

    val isEditing = env.id > 0L

    // 提交后等待真实结果：失败显示错误留在本页；成功回调 onSaved 关闭表单。
    LaunchedEffect(state.errorMessage) {
        if (saving && state.errorMessage != null) {
            saving = false
        }
    }
    LaunchedEffect(state.mutationSucceeded) {
        if (saving && state.mutationSucceeded) {
            saving = false
            screenViewModel.consumeMutation()
            onSaved()
        }
    }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .verticalScroll(rememberScrollState())
            .padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = title,
            style = androidx.compose.material3.MaterialTheme.typography.titleLarge,
            color = androidx.compose.material3.MaterialTheme.colorScheme.onSurface,
            fontWeight = FontWeight.Bold,
        )

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .clip(RoundedCornerShape(12.dp))
                .background(androidx.compose.material3.MaterialTheme.colorScheme.surface)
                .padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            OutlinedTextField(
                value = name,
                onValueChange = { name = it },
                label = { Text("变量名") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
            OutlinedTextField(
                value = value,
                onValueChange = { value = it },
                label = { Text(if (secret) "值（已加密）" else "值") },
                singleLine = true,
                visualTransformation = if (secret) {
                    androidx.compose.ui.text.input.PasswordVisualTransformation()
                } else {
                    androidx.compose.ui.text.input.VisualTransformation.None
                },
                modifier = Modifier.fillMaxWidth(),
            )
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "是否加密",
                        style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
                        color = androidx.compose.material3.MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = "开启后值以密文显示，不落库",
                        style = androidx.compose.material3.MaterialTheme.typography.bodySmall,
                        color = AppColors.slate500,
                    )
                }
                Switch(checked = secret, onCheckedChange = { secret = it })
            }
            OutlinedTextField(
                value = remark,
                onValueChange = { remark = it },
                label = { Text("备注") },
                singleLine = true,
                modifier = Modifier.fillMaxWidth(),
            )
        }

        if (isEditing) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = if (enabled) "已启用" else "已禁用",
                    style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
                    color = androidx.compose.material3.MaterialTheme.colorScheme.onSurface,
                    modifier = Modifier.weight(1f),
                )
                Switch(checked = enabled, onCheckedChange = { enabled = it })
            }
        }

        state.errorMessage?.takeIf { saving || it.isNotBlank() }?.let {
            Text(
                it,
                color = androidx.compose.material3.MaterialTheme.colorScheme.error,
                style = androidx.compose.material3.MaterialTheme.typography.bodyMedium,
            )
        }

        Button(
            onClick = {
                val trimmedName = name.trim()
                if (trimmedName.isEmpty()) return@Button
                saving = true
                if (isEditing) {
                    screenViewModel.update(
                        id = env.id,
                        name = trimmedName,
                        value = value,
                        remark = remark,
                        enabled = enabled,
                    )
                } else {
                    screenViewModel.create(name = trimmedName, value = value, remark = remark)
                }
            },
            enabled = !saving,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(if (isEditing) "保存" else "创建")
        }

        Button(
            onClick = onCancel,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text("取消")
        }
    }
}