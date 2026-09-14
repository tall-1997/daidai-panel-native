package com.daidai.daidai_app.ui.screens.deps

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
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
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.daidai.daidai_app.data.model.DepInstallRequest
import com.daidai.daidai_app.data.model.DepManager
import com.daidai.daidai_app.data.repository.DepsRepository
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.theme.AppColors

/**
 * 安装依赖表单（Compose 原生，阶段 3-4）。
 *
 * 提供包管理器选择（pip / npm）、包名、可选版本三项输入，提交后经
 * [DepsViewModel.install] 走 POST /api/deps 安装。覆盖提交中 / 错误提示。
 *
 * 依赖通过参数注入（repository / viewModel）；不传时默认用
 * [AppServices.configRepository] 读到的连接信息装配 [DepsRepository]。
 * **未接线导航**，本组件自包含可编译。
 *
 * @param onAfterSubmit 提交成功后由调用方接管（如回到列表并刷新）；默认空实现。
 */
@Composable
fun DepInstallScreen(
    modifier: Modifier = Modifier,
    repository: DepsRepository? = null,
    viewModel: DepsViewModel? = null,
    onAfterSubmit: () -> Unit = {},
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: androidx.lifecycle.viewmodel.compose.viewModel(initializer = {
        val resolved = repository ?: run {
            val config = AppServices.configRepository(context).config.value
            DepsRepository(
                baseUrl = config.serverUrl,
                accessToken = config.accessToken,
                localToken = config.localToken,
            )
        }
        DepsViewModel(resolved)
    })
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    var manager by remember { mutableStateOf(DepManager.Pip) }
    var packageName by remember { mutableStateOf("") }
    var version by remember { mutableStateOf("") }

    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 20.dp, vertical = 20.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = "安装依赖",
            style = MaterialTheme.typography.titleLarge,
            fontWeight = FontWeight.W700,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Text(
            text = "选择包管理器并填写包名（可选填版本），提交后由面板后台异步安装。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        Text("包管理器", style = MaterialTheme.typography.titleMedium)
        Row(verticalAlignment = Alignment.CenterVertically) {
            FilterChip(
                selected = manager == DepManager.Pip,
                onClick = { if (!state.submittingInstall) manager = DepManager.Pip },
                label = { Text(DepManager.Pip.label) },
            )
            Spacer(Modifier.width(12.dp))
            FilterChip(
                selected = manager == DepManager.Npm,
                onClick = { if (!state.submittingInstall) manager = DepManager.Npm },
                label = { Text(DepManager.Npm.label) },
            )
        }

        OutlinedTextField(
            value = packageName,
            onValueChange = { if (!state.submittingInstall) packageName = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("包名") },
            placeholder = { Text(if (manager == DepManager.Pip) "e.g. requests" else "e.g. lodash") },
            singleLine = true,
            enabled = !state.submittingInstall,
        )

        OutlinedTextField(
            value = version,
            onValueChange = { if (!state.submittingInstall) version = it },
            modifier = Modifier.fillMaxWidth(),
            label = { Text("版本（可选）") },
            placeholder = { Text(if (manager == DepManager.Pip) "e.g. 2.31.0" else "e.g. ^4.17.21") },
            singleLine = true,
            enabled = !state.submittingInstall,
        )

        val errorMessage = state.errorMessage
        if (errorMessage != null) {
            Text(
                text = errorMessage,
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.errorColor,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(AppColors.red50)
                    .padding(12.dp),
            )
        }

        val notice = state.notice
        if (notice != null) {
            Text(
                text = notice,
                style = MaterialTheme.typography.bodyMedium,
                color = AppColors.successColor,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(MaterialTheme.colorScheme.surfaceVariant)
                    .padding(12.dp),
            )
        }

        Button(
            onClick = {
                val ok = screenViewModel.install(
                    DepInstallRequest(
                        manager = manager,
                        packageName = packageName.trim(),
                        version = version.trim(),
                    ),
                )
                if (ok) {
                    packageName = ""
                    version = ""
                    onAfterSubmit()
                }
            },
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp),
            enabled = !state.submittingInstall && packageName.isNotBlank(),
            shape = RoundedCornerShape(14.dp),
        ) {
            if (state.submittingInstall) {
                CircularProgressIndicator(
                    modifier = Modifier
                        .width(22.dp)
                        .height(22.dp),
                    strokeWidth = 2.dp,
                    color = MaterialTheme.colorScheme.onPrimary,
                )
            } else {
                Text("提交安装", fontWeight = FontWeight.W600)
            }
        }
    }
}
