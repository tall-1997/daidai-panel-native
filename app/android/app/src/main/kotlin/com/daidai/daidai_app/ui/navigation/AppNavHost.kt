package com.daidai.daidai_app.ui.navigation

import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.daidai.daidai_app.ui.screens.BootScreen
import com.daidai.daidai_app.ui.screens.DashboardScreen
import com.daidai.daidai_app.ui.screens.LoginScreen
import com.daidai.daidai_app.ui.screens.MoreScreen
import com.daidai.daidai_app.ui.screens.ServerConfigScreen
import com.daidai.daidai_app.ui.screens.logs.LogListScreen
import com.daidai.daidai_app.ui.screens.notifications.NotificationListScreen
import com.daidai.daidai_app.ui.screens.openapi.OpenApiListScreen

/** 顶层路由常量（boot / server-config / login / dashboard / more / logs / notifications / openapi）。 */
object Routes {
    const val BOOT = "boot"
    const val SERVER_CONFIG = "server-config"
    const val LOGIN = "login"
    const val DASHBOARD = "dashboard"
    const val MORE = "more"
    const val LOGS = "logs"
    const val NOTIFICATIONS = "notifications"
    const val OPENAPI = "openapi"
}

/**
 * 应用顶层导航：boot → server-config → login → dashboard 的引导链路，
 * dashboard 为带 5 个底部 tab 的主面板壳；more 作为独立顶层路由（全屏设置）。
 *
 * 仅依赖 Navigation Compose + Material3。
 */
@Composable
fun AppNavHost(modifier: Modifier = Modifier) {
    val navController = rememberNavController()

    NavHost(
        navController = navController,
        startDestination = Routes.BOOT,
        modifier = modifier,
    ) {
        composable(Routes.BOOT) {
            BootScreen(onContinue = { navController.navigate(Routes.SERVER_CONFIG) })
        }
        composable(Routes.SERVER_CONFIG) {
            ServerConfigScreen(onContinue = { navController.navigate(Routes.LOGIN) })
        }
        composable(Routes.LOGIN) {
            LoginScreen(onContinue = {
                navController.navigate(Routes.DASHBOARD) {
                    // 进入主面板后清空引导栈，避免返回键回到登录页。
                    popUpTo(Routes.BOOT) { inclusive = true }
                }
            })
        }
        composable(Routes.DASHBOARD) {
            DashboardScreen()
        }
        composable(Routes.MORE) {
            MoreScreen()
        }
        // 阶段 2 只读模块：独立顶层路由（日志 / 通知渠道 / Open API 应用），
        // 依赖各自注入/默认装配，不传参即可。logs 同时存在于 dashboard 内嵌
        // 壳中作为占位，此处为独立的全屏入口（供顶层跳转/后续独立使用）。
        composable(Routes.LOGS) {
            LogListScreen()
        }
        composable(Routes.NOTIFICATIONS) {
            NotificationListScreen()
        }
        composable(Routes.OPENAPI) {
            OpenApiListScreen()
        }
    }
}
