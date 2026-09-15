package com.daidai.daidai_app.ui.screens.profile

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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.ProfileInfo
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.components.CardView
import com.daidai.daidai_app.ui.components.ErrorView
import com.daidai.daidai_app.ui.components.LoadingView
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 个人中心页（Compose 原生）。
 *
 * 展示当前登录用户的 头像 / 用户名 / 角色 / 账号状态 / 面板版本，并提供
 * 退出登录按钮。退出登录的清 token 逻辑由集成方通过 [onLogout] 回调接驳
 * （本组件不在此处清 token）。**未接线导航**，自包含可编译。
 *
 * 依赖通过参数注入（viewModel）；不传时默认用 [AppServices.configRepository]
 * 读到的服务器地址与令牌装配 [ProfileViewModel]，与 Users / OpenApi 约定一致。
 *
 * @param onLogout 点击“退出登录”时回调（清 token 等由集成方实现）。
 * @param onRetry  加载失败时点击“重试”的回调，默认重新拉取。
 */
@Composable
fun ProfileScreen(
    modifier: Modifier = Modifier,
    onLogout: () -> Unit = {},
    viewModel: ProfileViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val config = AppServices.configRepository(context).config.value
        ProfileViewModel(
            baseUrl = config.serverUrl,
            accessToken = config.accessToken,
            localToken = config.localToken,
        )
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()
    val busy by screenViewModel.busy.collectAsStateWithLifecycle()
    val notice by screenViewModel.notice.collectAsStateWithLifecycle()
    var edit by remember { mutableStateOf<String?>(null) }
    val resolver = context.applicationContext.contentResolver
    val picker = rememberLauncherForActivityResult(androidx.activity.result.contract.ActivityResultContracts.GetContent()) { uri ->
        uri?.let { screenViewModel.uploadAvatar { resolver.openInputStream(it) } }
    }
    edit?.let { action ->
        AccountValueDialog(
            title = if (action == "password") "修改密码" else "修改用户名",
            fields = if (action == "password") listOf("原密码" to true, "新密码（至少 6 位）" to true, "确认新密码" to true)
                else listOf("新用户名" to false),
            onDismiss = { edit = null },
            onConfirm = { values ->
                if (action == "password") screenViewModel.changePassword(values[0], values[1])
                else screenViewModel.changeUsername(values[0].trim())
                edit = null
            },
        )
    }

    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
    ) {
        when (val s = state) {
            is ProfileUiState.Loading -> LoadingView()
            is ProfileUiState.Success -> ProfileContent(
                profile = s.profile,
                canLogout = s.canLogout,
                onRetry = screenViewModel::refresh,
                onLogout = onLogout,
                actions = {
                    notice?.let { Text(it) }
                    Button(onClick = { edit = "username" }, enabled = !busy) { Text("修改用户名") }
                    Button(onClick = { edit = "password" }, enabled = !busy) { Text("修改密码") }
                    Button(onClick = { picker.launch("image/*") }, enabled = !busy) { Text("上传头像（最大 5MB）") }
                    OutlinedButton(onClick = screenViewModel::deleteAvatar, enabled = !busy) { Text("删除头像") }
                    if (busy) Text("正在处理…")
                },
            )
            is ProfileUiState.Error -> ErrorView(
                message = s.message,
                onRetry = screenViewModel::refresh,
            )
        }
    }
}

@Composable
private fun ProfileContent(
    profile: ProfileInfo,
    canLogout: Boolean,
    onRetry: () -> Unit,
    onLogout: () -> Unit,
    actions: @Composable () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "个人中心",
            style = MaterialTheme.typography.headlineSmall,
            fontWeight = FontWeight.W700,
            color = AppColors.slate700,
        )

        // 头像 / 用户名 / 角色 信息卡片
        CardView(modifier = Modifier.fillMaxWidth()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                AvatarCircle(username = profile.username)
                Spacer(modifier = Modifier.width(16.dp))
                Column {
                    Text(
                        text = profile.username.ifBlank { "未登录" },
                        style = MaterialTheme.typography.titleLarge,
                        fontWeight = FontWeight.W600,
                        color = AppColors.slate900,
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                    Row(
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RoleChip(text = profile.roleText)
                        StatusDot(enabled = profile.enabled)
                    }
                }
            }
        }

        // 账号信息卡片
        CardView(title = "账号信息", modifier = Modifier.fillMaxWidth()) {
            InfoRow(label = "用户名", value = profile.username.ifBlank { "-" })
            InfoRow(label = "角色", value = profile.roleText)
            InfoRow(label = "账号状态", value = if (profile.enabled) "正常" else "停用")
            InfoRow(label = "面板版本", value = profile.version.ifBlank { "-" })
        }

        actions()
        // 退出登录
        if (canLogout) {
            Button(
                onClick = onLogout,
                modifier = Modifier.fillMaxWidth(),
                colors = ButtonDefaults.buttonColors(
                    containerColor = AppColors.errorColor,
                    contentColor = Color.White,
                ),
            ) {
                Text("退出登录", fontWeight = FontWeight.W600)
            }
        } else {
            OutlinedButton(
                onClick = onRetry,
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("刷新")
            }
        }
    }
}

/** 用户头像占位：取用户名首字符渲染为圆形头像（未联后端头像 CDN）。 */
@Composable
private fun AvatarCircle(username: String, modifier: Modifier = Modifier) {
    val initial = username.trim().firstOrNull()?.uppercase()?.take(1) ?: "?"
    Box(
        modifier = modifier
            .size(64.dp)
            .clip(CircleShape)
            .background(AppColors.primary),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = initial,
            fontSize = 26.sp,
            fontWeight = FontWeight.W700,
            color = Color.White,
        )
    }
}

/** 角色徽标（底色随角色变化）。 */
@Composable
private fun RoleChip(text: String) {
    val (bg, fg) = roleColor(text)
    Text(
        text = text,
        color = fg,
        fontSize = 12.sp,
        fontWeight = FontWeight.W600,
        modifier = Modifier
            .clip(CircleShape)
            .background(bg)
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

private fun roleColor(text: String): Pair<Color, Color> = when (text.lowercase()) {
    "管理员", "admin" -> AppColors.primary to Color.White
    "访客", "viewer" -> AppColors.slate200 to AppColors.slate700
    "操作员" -> AppColors.termBlue to Color.White
    else -> AppColors.amber500 to Color.White
}

/** 账号状态小圆点。 */
@Composable
private fun StatusDot(enabled: Boolean) {
    Box(
        modifier = Modifier
            .size(8.dp)
            .clip(CircleShape)
            .background(if (enabled) AppColors.successColor else AppColors.errorColor),
    )
}

/** 信息行：label + 值（底部分隔线）。 */
@Composable
private fun InfoRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = AppColors.slate500,
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.W500,
            color = AppColors.slate800,
        )
    }
}


@Composable
private fun AccountValueDialog(
    title: String,
    fields: List<Pair<String, Boolean>>,
    onDismiss: () -> Unit,
    onConfirm: (List<String>) -> Unit,
) {
    val values = remember { androidx.compose.runtime.mutableStateListOf(*fields.map { "" }.toTypedArray()) }
    androidx.compose.material3.AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                fields.forEachIndexed { index, (label, secret) ->
                    androidx.compose.material3.OutlinedTextField(
                        value = values[index],
                        onValueChange = { values[index] = it },
                        label = { Text(label) },
                        singleLine = true,
                        visualTransformation = if (secret) androidx.compose.ui.text.input.PasswordVisualTransformation()
                        else androidx.compose.ui.text.input.VisualTransformation.None,
                    )
                }
            }
        },
        confirmButton = {
            androidx.compose.material3.TextButton(
                onClick = { onConfirm(values.toList()) },
                enabled = values.all { it.isNotBlank() } &&
                    (values.size != 3 || (values[1].length >= 6 && values[1] == values[2])),
            ) { Text("确定") }
        },
        dismissButton = { androidx.compose.material3.TextButton(onClick = onDismiss) { Text("取消") } },
    )
}
