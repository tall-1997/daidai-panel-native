package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 登录页（login）：用户名/密码输入、check-init 后初始化分支、POST login，
 * 以及 loading / error / success 状态。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认装配
 * [AppServices.loginRepository]（对接本地 fallback 或远程 Go，见 di/AppServices.kt）。
 * onContinue 保持与 AppNavHost 兼容：成功回调后跳转 dashboard。
 *
 * 约定接口见 [LoginRepository]。
 */
@Composable
fun LoginScreen(
    onContinue: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
    repository: LoginRepository? = null,
    viewModel: LoginViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(repository, context) {
        LoginViewModel(repository ?: AppServices.loginRepository(context))
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 32.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Spacer(Modifier.height(8.dp))
        Text("登录 Daidai 面板", style = MaterialTheme.typography.headlineMedium, color = AppColors.primary)
        Text(
            when (state.phase) {
                LoginUiState.Phase.CheckingInitialization -> "正在检查面板初始化状态…"
                LoginUiState.Phase.RequiresInitialization -> "首次使用，请输入需要设置为管理员凭据的用户名与密码。"
                LoginUiState.Phase.Ready -> "请输入管理员用户名与密码登录。"
                LoginUiState.Phase.Loading -> "正在登录…"
                LoginUiState.Phase.Success -> "登录成功，正在进入面板…"
                LoginUiState.Phase.Error -> "登录已就绪，请重试。"
            },
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Spacer(Modifier.height(8.dp))

        OutlinedTextField(
            value = state.username,
            onValueChange = screenViewModel::setUsername,
            modifier = Modifier.fillMaxWidth(),
            label = { Text("用户名") },
            singleLine = true,
            enabled = !isBusy(state.phase),
        )

        OutlinedTextField(
            value = state.password,
            onValueChange = screenViewModel::setPassword,
            modifier = Modifier.fillMaxWidth(),
            label = { Text("密码") },
            singleLine = true,
            enabled = !isBusy(state.phase),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            visualTransformation = PasswordVisualTransformation(),
        )

        state.errorMessage?.let {
            Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodyMedium)
        }

        Spacer(Modifier.height(8.dp))

        Button(
            onClick = { screenViewModel.submit(onContinue ?: {}) },
            modifier = Modifier.fillMaxWidth(),
            enabled = !isBusy(state.phase),
        ) {
            if (state.phase == LoginUiState.Phase.Loading) {
                CircularProgressIndicator(
                    strokeWidth = 2.dp,
                    modifier = Modifier.size(20.dp),
                )
                Spacer(Modifier.size(8.dp))
            }
            Text(buttonLabel(state.phase))
        }

        // 状态异常或失败时，允许重新检查初始化状态。
        if (state.errorMessage != null || state.phase == LoginUiState.Phase.Error) {
            TextButton(
                onClick = { screenViewModel.refreshInitState() },
                modifier = Modifier.align(Alignment.CenterHorizontally),
            ) {
                Text("重新检查")
            }
        }
    }
}

private fun isBusy(phase: LoginUiState.Phase): Boolean =
    phase == LoginUiState.Phase.CheckingInitialization || phase == LoginUiState.Phase.Loading

private fun buttonLabel(phase: LoginUiState.Phase): String = when (phase) {
    LoginUiState.Phase.CheckingInitialization -> "检查中…"
    LoginUiState.Phase.RequiresInitialization -> "初始化并登录"
    LoginUiState.Phase.Ready -> "登录"
    LoginUiState.Phase.Loading -> "登录中…"
    LoginUiState.Phase.Success -> "登录成功"
    LoginUiState.Phase.Error -> "重试登录"
}