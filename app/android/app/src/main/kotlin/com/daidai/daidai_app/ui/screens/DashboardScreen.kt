package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController

/** 仪表盘壳内 5 个底部导航 tab 的静态描述（规划 §5.1：dashboard/tasks/logs/envs/more）。 */
private data class DashboardTab(
    val route: String,
    val label: String,
    val icon: ImageVector,
)

private val dashboardTabs = listOf(
    DashboardTab("overview", "仪表盘", Icons.Filled.Home),
    DashboardTab("tasks", "任务", Icons.Filled.List),
    DashboardTab("logs", "日志", Icons.Filled.Info),
    DashboardTab("envs", "环境", Icons.Filled.Settings),
    DashboardTab("more", "更多", Icons.Filled.MoreVert),
)

/**
 * 仪表盘（dashboard）：主面板壳，带 5 个底部导航 tab。
 * 阶段 1 接入托管本地后的概览/任务/环境数据；更多 tab 复用 [MoreScreen]。
 */
@Composable
fun DashboardScreen(modifier: Modifier = Modifier) {
    val navController = rememberNavController()
    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route

    Scaffold(
        modifier = modifier,
        bottomBar = {
            NavigationBar {
                dashboardTabs.forEach { tab ->
                    NavigationBarItem(
                        selected = currentRoute == tab.route,
                        onClick = {
                            navController.navigate(tab.route) {
                                popUpTo(navController.graph.findStartDestination().id) {
                                    saveState = true
                                }
                                launchSingleTop = true
                                restoreState = true
                            }
                        },
                        icon = { Icon(tab.icon, contentDescription = tab.label) },
                        label = { Text(tab.label) },
                    )
                }
            }
        },
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = "overview",
            modifier = Modifier.padding(innerPadding),
        ) {
            composable("overview") {
                PlaceholderPage("仪表盘概览", "任务 / 运行状态总览（阶段 1）")
            }
            composable("tasks") {
                PlaceholderPage("任务", "任务列表与调度（阶段 1）")
            }
            composable("logs") {
                PlaceholderPage("日志", "运行日志（阶段 1）")
            }
            composable("envs") {
                PlaceholderPage("环境", "运行时环境管理（阶段 1）")
            }
            composable("more") { MoreScreen() }
        }
    }
}