package com.daidai.daidai_app.ui.navigation

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.daidai.daidai_app.data.localcore.AppLockSession
import com.daidai.daidai_app.data.repository.PanelConnectionMode
import com.daidai.daidai_app.data.model.EnvVar
import com.daidai.daidai_app.data.model.OpenApiApp
import com.daidai.daidai_app.data.model.Subscription
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.di.AppServices
import com.daidai.daidai_app.ui.screens.BootScreen
import com.daidai.daidai_app.ui.screens.DashboardScreen
import com.daidai.daidai_app.ui.screens.LoginScreen
import com.daidai.daidai_app.ui.screens.ServerConfigScreen
import com.daidai.daidai_app.ui.screens.applock.AppLockGateScreen
import com.daidai.daidai_app.ui.screens.applock.AppLockSettingsScreen
import com.daidai.daidai_app.ui.screens.deps.DepInstallScreen
import com.daidai.daidai_app.ui.screens.deps.DepListScreen
import com.daidai.daidai_app.ui.screens.envs.EnvFormScreen
import com.daidai.daidai_app.ui.screens.envs.EnvListScreen
import com.daidai.daidai_app.ui.screens.logs.LogListScreen
import com.daidai.daidai_app.ui.screens.notifications.LocalNotificationSettingsScreen
import com.daidai.daidai_app.ui.screens.notifications.NotificationListScreen
import com.daidai.daidai_app.ui.screens.openapi.OpenApiCreateScreen
import com.daidai.daidai_app.ui.screens.openapi.OpenApiDetailScreen
import com.daidai.daidai_app.ui.screens.openapi.OpenApiListScreen
import com.daidai.daidai_app.ui.screens.profile.ProfileScreen
import com.daidai.daidai_app.ui.screens.scripts.ScriptListScreen
import com.daidai.daidai_app.ui.screens.scripts.ScriptViewScreen
import com.daidai.daidai_app.ui.screens.platformtokens.PlatformTokensScreen
import com.daidai.daidai_app.ui.screens.terminal.TerminalScreen
import com.daidai.daidai_app.ui.screens.system.ConfigScriptScreen
import com.daidai.daidai_app.ui.screens.system.PanelLogScreen
import com.daidai.daidai_app.ui.screens.settings.PanelSettingsScreen
import com.daidai.daidai_app.ui.screens.settings.AboutScreen
import com.daidai.daidai_app.ui.screens.security.SecurityScreen
import com.daidai.daidai_app.ui.screens.security.SshKeysScreen
import com.daidai.daidai_app.ui.screens.settings.SettingsEntry
import com.daidai.daidai_app.ui.screens.settings.SettingsScreen
import com.daidai.daidai_app.ui.screens.settings.SystemSettingsScreen
import com.daidai.daidai_app.ui.screens.settings.ThemeSettingsScreen
import com.daidai.daidai_app.ui.screens.subscriptions.SubscriptionDetailScreen
import com.daidai.daidai_app.ui.screens.subscriptions.SubscriptionListScreen
import com.daidai.daidai_app.ui.screens.system.BackupScreen
import com.daidai.daidai_app.ui.screens.system.HealthCheckScreen
import com.daidai.daidai_app.ui.screens.tasks.TaskFormScreen
import com.daidai.daidai_app.ui.screens.tasks.TaskListScreen
import com.daidai.daidai_app.ui.screens.users.UserListScreen
import com.daidai.daidai_app.ui.theme.ThemeController
import kotlinx.coroutines.launch

/** 顶层路由常量：引导链路 + 全部模块主入口 + 二级（表单/详情）路由。 */
object Routes {
    const val BOOT = "boot"
    const val SERVER_CONFIG = "server-config"
    const val LOGIN = "login"
    const val DASHBOARD = "dashboard"
    const val MORE = "more"

    // 主壳 / 顶层模块入口
    const val LOGS = "logs"
    const val NOTIFICATIONS = "notifications"
    const val OPENAPI = "openapi"
    const val TASKS = "tasks"
    const val ENVS = "envs"
    const val SCRIPTS = "scripts"
    const val DEPS = "deps"
    const val SUBSCRIPTIONS = "subscriptions"
    const val SECURITY = "security"
    const val SETTINGS = "settings"
    const val SETTINGS_THEME = "settings/theme"
    const val SETTINGS_SYSTEM = "settings/system"
    const val PROFILE = "profile"
    const val APPLOCK = "applock"
    const val APPLOCK_GATE = "applock/gate"
    const val USERS = "users"
    const val BACKUP = "backup"
    const val HEALTH_CHECK = "health-check"
    const val LOCAL_NOTIFICATIONS = "notifications/local"

    // 二级路由（从列表进入表单 / 详情）
    const val TASKS_NEW = "tasks/new"
    const val TASKS_EDIT = "tasks/edit"
    const val ENVS_FORM = "envs/form"
    const val SCRIPTS_VIEW = "scripts/view"
    const val DEPS_INSTALL = "deps/install"
    const val SUBSCRIPTIONS_DETAIL = "subscriptions/detail"
    const val OPENAPI_CREATE = "openapi/create"
    const val OPENAPI_DETAIL = "openapi/detail"
    const val SECURITY_SSH_KEYS = "security/ssh-keys"
    const val PLATFORM_TOKENS = "platform-tokens"
    const val TERMINAL = "terminal"
    const val PANEL_LOG = "panel-log"
    const val CONFIG_SCRIPT = "config-script"
    const val PANEL_SETTINGS = "panel-settings"
    const val ABOUT = "about"
}

/**
 * 跨页面导航时传递的「编辑 / 查看」实体。列表页进入表单/详情前先写入对应字段再 navigate；
 * 目标路由读取后渲染，纯内存态、不持久化。
 */
object NavSelections {
    var task: Task? = null
    var env: EnvVar? = null
    var subscription: Subscription? = null
    var openApiApp: OpenApiApp? = null
}

/**
 * 应用顶层导航：boot → server-config → login → dashboard 的引导链路；
 * dashboard 为带 5 个底部 tab 的主面板壳，之后是各模块顶层入口与二级表单/详情路由。
 * 「更多」tab 复用 [MoreEntryListScreen] 进入 设置/安全/应用锁/用户/系统 等模块。
 */
@Composable
fun AppNavHost(modifier: Modifier = Modifier) {
    val navController = rememberNavController()

    // 应用锁门禁：启用后（冷启动 / 退后台回前台）锁定态自动压栈到 gate 页。
    val appLocked by AppLockSession.locked.collectAsStateWithLifecycle()
    LaunchedEffect(appLocked) {
        if (appLocked && navController.currentDestination?.route != Routes.APPLOCK_GATE) {
            navController.navigate(Routes.APPLOCK_GATE) { launchSingleTop = true }
        }
    }

    NavHost(
        navController = navController,
        startDestination = Routes.BOOT,
        modifier = modifier,
    ) {
        composable(Routes.BOOT) {
            val context = LocalContext.current
            var destination by remember { mutableStateOf<String?>(null) }
            LaunchedEffect(Unit) {
                val config = AppServices.configRepository(context).getConfig()
                destination = when {
                    AppServices.probeExistingSession() -> Routes.DASHBOARD
                    config.mode == PanelConnectionMode.MANAGED_LOCAL || config.serverUrl.isNotBlank() -> Routes.LOGIN
                    else -> Routes.SERVER_CONFIG
                }
                if (destination == Routes.DASHBOARD) {
                    navController.navigate(Routes.DASHBOARD) {
                        popUpTo(Routes.BOOT) { inclusive = true }
                        launchSingleTop = true
                    }
                }
            }
            BootScreen(
                isResolving = destination == null,
                onContinue = { navController.navigate(destination ?: Routes.SERVER_CONFIG) { launchSingleTop = true } },
            )
        }
        composable(Routes.SERVER_CONFIG) {
            ServerConfigScreen(onContinue = { navController.navigate(Routes.LOGIN) { launchSingleTop = true } })
        }
        composable(Routes.LOGIN) {
            LoginScreen(onContinue = {
                navController.navigate(Routes.DASHBOARD) {
                    // 进入主面板后清空引导栈，避免返回键回到登录页。
                    popUpTo(Routes.BOOT) { inclusive = true }
                    launchSingleTop = true
                }
            })
        }
        composable(Routes.DASHBOARD) {
            DashboardScreen(navTo = { route -> navController.navigate(route) { launchSingleTop = true } })
        }
        composable(Routes.MORE) {
            MoreEntryListScreen(onOpen = { route -> navController.navigate(route) { launchSingleTop = true } })
        }

        // ---- 顶层模块入口（列表 / 只读页，自包含装配） ----
        composable(Routes.LOGS) {
            LogListScreen()
        }
        composable(Routes.NOTIFICATIONS) {
            NotificationListScreen()
        }
        composable(Routes.LOCAL_NOTIFICATIONS) {
            LocalNotificationSettingsScreen()
        }
        composable(Routes.OPENAPI) {
            OpenApiListScreen(
                onCreate = {
                    NavSelections.openApiApp = null
                    navController.navigate(Routes.OPENAPI_CREATE) { launchSingleTop = true }
                },
                onOpenDetail = {
                    NavSelections.openApiApp = it
                    navController.navigate(Routes.OPENAPI_DETAIL) { launchSingleTop = true }
                },
            )
        }
        composable(Routes.SETTINGS_THEME) {
            ThemeSettingsScreen(
                onThemeModeChange = { mode -> ThemeController.setMode(mode) },
            )
        }
        composable(Routes.SETTINGS_SYSTEM) {
            SystemSettingsScreen()
        }

        // ---- 模块主入口路由（自包含列表；表单/详情走二级路由） ----
        composable(Routes.SETTINGS) {
            SettingsScreen(
                onNavigate = { entry -> settingsRouteFor(entry)?.let { navController.navigate(it) } },
            )
        }
        composable(Routes.SECURITY) {
            SecurityScreen()
        }
        composable(Routes.SECURITY_SSH_KEYS) {
            SshKeysScreen()
        }
        composable(Routes.APPLOCK) {
            AppLockSettingsScreen()
        }
        composable(Routes.APPLOCK_GATE) {
            AppLockGateScreen(onUnlocked = {
                AppLockSession.unlock()
                navController.navigateUp()
            })
        }
        composable(Routes.PROFILE) {
            val context = LocalContext.current
            val scope = rememberCoroutineScope()
            ProfileScreen(
                onLogout = {
                    scope.launch {
                        AppServices.configRepository(context).clearTokens()
                        navController.navigate(Routes.LOGIN) {
                            popUpTo(0) { inclusive = true }
                            launchSingleTop = true
                        }
                    }
                },
            )
        }
        composable(Routes.USERS) {
            UserListScreen()
        }
        composable(Routes.BACKUP) {
            BackupScreen()
        }
        composable(Routes.PANEL_LOG) {
            PanelLogScreen()
        }
        composable(Routes.CONFIG_SCRIPT) {
            ConfigScriptScreen()
        }
        composable(Routes.PLATFORM_TOKENS) {
            PlatformTokensScreen()
        }
        composable(Routes.TERMINAL) {
            TerminalScreen()
        }
        composable(Routes.PANEL_SETTINGS) {
            PanelSettingsScreen()
        }
        composable(Routes.ABOUT) {
            AboutScreen()
        }
        composable(Routes.HEALTH_CHECK) {
            HealthCheckScreen()
        }

        // ---- 二级路由（列表 → 表单 / 详情） ----
        composable(Routes.TASKS) {
            TaskListScreen(
                onCreateTask = { NavSelections.task = null; navController.navigate(Routes.TASKS_NEW) },
                onEditTask = { NavSelections.task = it; navController.navigate(Routes.TASKS_EDIT) },
            )
        }
        composable(Routes.TASKS_NEW) {
            TaskFormScreen(editingTask = NavSelections.task, onSaved = { navController.popBackStack() })
        }
        composable(Routes.TASKS_EDIT) {
            TaskFormScreen(editingTask = NavSelections.task, onSaved = { navController.popBackStack() })
        }
        composable(Routes.ENVS) {
            EnvListScreen(
                onCreate = { NavSelections.env = null; navController.navigate(Routes.ENVS_FORM) },
                onEdit = { NavSelections.env = it; navController.navigate(Routes.ENVS_FORM) },
            )
        }
        composable(Routes.ENVS_FORM) {
            EnvFormScreen(
                env = NavSelections.env ?: EnvVar(),
                onSaved = { navController.popBackStack() },
                onCancel = { navController.popBackStack() },
            )
        }
        composable(Routes.SCRIPTS) {
            ScriptListScreen()
        }
        composable(Routes.SCRIPTS_VIEW) {
            ScriptViewScreen(path = NavSelections.task?.scriptPath ?: "", onBack = { navController.popBackStack() })
        }
        composable(Routes.DEPS) {
            DepListScreen(onNavigateToInstall = { navController.navigate(Routes.DEPS_INSTALL) })
        }
        composable(Routes.DEPS_INSTALL) {
            DepInstallScreen(onAfterSubmit = { navController.popBackStack() })
        }
        composable(Routes.SUBSCRIPTIONS) {
            SubscriptionListScreen()
        }
        composable(Routes.SUBSCRIPTIONS_DETAIL) {
            SubscriptionDetailScreen(
                subscription = NavSelections.subscription,
                onBack = { navController.popBackStack() },
                onSaved = { navController.popBackStack() },
            )
        }
        composable(Routes.OPENAPI_CREATE) {
            OpenApiCreateScreen(
                onBack = { navController.popBackStack() },
                onCreated = { navController.popBackStack() },
            )
        }
        composable(Routes.OPENAPI_DETAIL) {
            OpenApiDetailScreen(
                app = NavSelections.openApiApp ?: OpenApiApp(),
                onBack = { navController.popBackStack() },
            )
        }
    }
}

/** 把设置总页的 [SettingsEntry] 映射到顶层路由；未接线的入口返回 null（停留原页）。 */
private fun settingsRouteFor(entry: SettingsEntry): String? = when (entry) {
    SettingsEntry.THEME -> Routes.SETTINGS_THEME
    SettingsEntry.SYSTEM_SETTINGS -> Routes.SETTINGS_SYSTEM
    SettingsEntry.LOGS -> Routes.LOGS
    SettingsEntry.PANEL_SETTINGS -> Routes.PANEL_SETTINGS
    SettingsEntry.ABOUT -> Routes.ABOUT
    else -> null
}

/**
 * 「更多」入口列表：把阶段 1 的 [MoreScreen] 占位替换为各模块入口卡片，
 * 点击经 [onOpen] 交给调用方（顶层 NavHost 或 dashboard「更多」tab）跳转。
 */
@Composable
fun MoreEntryListScreen(onOpen: (String) -> Unit, modifier: Modifier = Modifier) {
    val themeMode by ThemeController.mode.collectAsStateWithLifecycle()

    val entries = listOf(
        Triple(Routes.PROFILE, "个人中心", "账号资料与退出登录"),
        Triple(Routes.SETTINGS, "设置", "应用与面板的各项设置入口"),
        Triple(Routes.SECURITY, "安全", "登录日志、在线会话与审计"),
        Triple(Routes.SECURITY_SSH_KEYS, "SSH 密钥", "订阅拉取与部署用的私钥管理"),
        Triple(Routes.APPLOCK, "应用锁", "密码 / 图案 / 生物识别锁"),
        Triple(Routes.USERS, "用户", "面板本地用户与权限管理"),
        Triple(Routes.SETTINGS_SYSTEM, "系统设置", "时区、语言、代理与更新"),
        Triple(Routes.BACKUP, "备份", "备份列表与新建 / 删除"),
        Triple(Routes.HEALTH_CHECK, "健康检查", "面板运行健康与自检"),
        Triple(Routes.LOGS, "日志", "运行日志"),
        Triple(Routes.NOTIFICATIONS, "通知渠道", "Open API 通知配置"),
        Triple(Routes.LOCAL_NOTIFICATIONS, "本机通知", "本机通知开关与设置"),
        Triple(Routes.OPENAPI, "Open API", "Open API 应用管理"),
        Triple(Routes.SCRIPTS, "脚本", "脚本文件树与运行"),
        Triple(Routes.DEPS, "依赖", "运行依赖管理"),
        Triple(Routes.SUBSCRIPTIONS, "订阅", "配置订阅源管理"),
    )

    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            text = "更多",
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.primary,
        )
        Text(
            text = "设置 / 安全 / 用户 / 系统 等模块入口。当前主题：${themeMode.label}",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.size(4.dp))
        entries.forEach { (route, title, subtitle) ->
            MoreEntryRow(title = title, subtitle = subtitle, onClick = { onOpen(route) })
        }
    }
}

/** 单条「更多」入口行，右侧带「>」指示箭头。 */
@Composable
private fun MoreEntryRow(
    title: String,
    subtitle: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(horizontal = 4.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Icon(
            imageVector = Icons.Filled.KeyboardArrowRight,
            contentDescription = "进入",
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.size(20.dp),
        )
    }
}
