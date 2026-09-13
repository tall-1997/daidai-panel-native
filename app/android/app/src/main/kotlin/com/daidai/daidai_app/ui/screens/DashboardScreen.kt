package com.daidai.daidai_app.ui.screens

import androidx.compose.foundation.Canvas
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.List
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.daidai.daidai_app.ui.navigation.MoreEntryListScreen
import com.daidai.daidai_app.ui.navigation.NavSelections
import com.daidai.daidai_app.ui.navigation.Routes
import com.daidai.daidai_app.ui.screens.envs.EnvListScreen
import com.daidai.daidai_app.ui.screens.logs.LogListScreen
import com.daidai.daidai_app.ui.screens.tasks.TaskListScreen
import com.daidai.daidai_app.data.model.DailyStat
import com.daidai.daidai_app.data.model.DashboardStats
import com.daidai.daidai_app.data.model.SystemInfo
import com.daidai.daidai_app.data.repository.DashboardRepository
import com.daidai.daidai_app.ui.theme.AppColors

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
 * overview tab 展示真实仪表盘数据（CPU/内存/磁盘 + 任务统计 + 7 日趋势），
 * tasks/logs/envs 已接入真实模块列表（替换阶段 1 占位），more 复用
 * [MoreEntryListScreen] 提供 设置/安全/用户/系统 等模块入口。
 *
 * @param navTo 顶层导航回调：dashboard 内嵌列表页进入二级（表单/详情）或更模块入口时调用。
 */
@Composable
fun DashboardScreen(
    modifier: Modifier = Modifier,
    navTo: (String) -> Unit = {},
) {
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
                DashboardOverviewPage()
            }
            composable("tasks") {
                TaskListScreen(
                    onCreateTask = { NavSelections.task = null; navTo(Routes.TASKS_NEW) },
                    onEditTask = { NavSelections.task = it; navTo(Routes.TASKS_EDIT) },
                )
            }
            composable("logs") {
                LogListScreen()
            }
            composable("envs") {
                EnvListScreen(
                    onCreate = { NavSelections.env = null; navTo(Routes.ENVS_FORM) },
                    onEdit = { NavSelections.env = it; navTo(Routes.ENVS_FORM) },
                )
            }
            composable("more") { MoreEntryListScreen(onOpen = { route -> navTo(route) }) }
        }
    }
}

// ---------------------------------------------------------------------------
// 概览数据页：View 装配 + 状态渲染
// ---------------------------------------------------------------------------

/**
 * 仪表盘 overview 页。默认装配 [DashboardRepository]（经 AppServices 只读读取
 * 配置，见 data/repository/DashboardRepository.kt），并把 [DashboardUiState] 的
 * Loading / Success / Error 三态映射为界面。
 */
@Composable
private fun DashboardOverviewPage(
    viewModel: DashboardViewModel? = null,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember {
        DashboardViewModel(DashboardRepository(context))
    }
    val state by screenViewModel.uiState.collectAsStateWithLifecycle()

    when (val s = state) {
        DashboardUiState.Loading -> {
            Column(
                modifier = modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState())
                    .padding(20.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                CircularProgressIndicator(color = AppColors.primary)
                Spacer(Modifier.height(12.dp))
                Text("加载仪表盘数据…", color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        is DashboardUiState.Error -> {
            Column(
                modifier = modifier
                    .fillMaxSize()
                    .padding(20.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text("加载失败", style = MaterialTheme.typography.titleMedium, color = AppColors.errorColor)
                Spacer(Modifier.height(8.dp))
                Text(
                    s.message,
                    style = MaterialTheme.typography.bodyMedium,
                    color = AppColors.slate500,
                    textAlign = TextAlign.Center,
                )
                Spacer(Modifier.height(20.dp))
                Button(onClick = screenViewModel::refresh) {
                    Text("重试")
                }
            }
        }
        is DashboardUiState.Success -> {
            DashboardContent(
                systemInfo = s.systemInfo,
                stats = s.stats,
                onRefresh = screenViewModel::refresh,
                modifier = modifier,
            )
        }
    }
}

@Composable
private fun DashboardContent(
    systemInfo: SystemInfo,
    stats: DashboardStats,
    onRefresh: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 16.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        // 标题行
        DashboardHeader(systemInfo, stats, onRefresh)

        // 资源卡片（CPU / 内存 / 磁盘）
        Text("系统资源", style = MaterialTheme.typography.titleMedium, color = AppColors.primary)
        ResourceCards(systemInfo)

        // 任务统计卡片
        Text("任务运行", style = MaterialTheme.typography.titleMedium, color = AppColors.primary)
        TaskStatsCard(stats)

        // 7 日执行趋势
        if (stats.dailyStats.isNotEmpty()) {
            Text("近 7 天执行统计", style = MaterialTheme.typography.titleMedium, color = AppColors.primary)
            TrendSection(stats.dailyStats)
        }
    }
}

@Composable
private fun DashboardHeader(
    systemInfo: SystemInfo,
    stats: DashboardStats,
    onRefresh: () -> Unit,
) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Column {
            Text(
                "仪表盘概览",
                style = MaterialTheme.typography.headlineSmall,
                color = AppColors.primary,
            )
            Spacer(Modifier.height(2.dp))
            Text(
                buildString {
                    append(systemInfo.hostname.ifBlank { "未知主机" })
                    if (systemInfo.os.isNotBlank()) append(" · ${systemInfo.os}/${systemInfo.arch}")
                }.trimEnd('·'),
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate500,
            )
            Text(
                "运行时长 ${systemInfo.uptime}",
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                "共 ${stats.taskCount} 个任务",
                style = MaterialTheme.typography.labelMedium,
                color = AppColors.slate600,
                modifier = Modifier.padding(horizontal = 8.dp),
            )
            TextButton(onClick = onRefresh) {
                Text("刷新", color = AppColors.primary)
            }
        }
    }
}

// --- 资源卡片 ---------------------------------------------------------------

@Composable
private fun ResourceCards(systemInfo: SystemInfo) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        ResourceValueCard("CPU 使用率", formatPercent(systemInfo.cpuUsage), systemInfo.cpuUsage)
        ResourceValueCard(
            "内存",
            "${formatBytes(systemInfo.memoryUsed)} / ${formatBytes(systemInfo.memoryTotal)}",
            systemInfo.memoryUsage,
            subtitle = "剩余 ${formatBytes(systemInfo.memoryFree)}",
        )
        ResourceValueCard(
            "磁盘",
            "${formatBytes(systemInfo.diskUsed)} / ${formatBytes(systemInfo.diskTotal)}",
            systemInfo.diskUsage,
            subtitle = "可用 ${formatBytes(systemInfo.diskFree)}",
        )
    }
}

/** 带进度条的单项资源卡片。ratio ∈ [0,1]，超过 100% 时归一。 */
@Composable
private fun ResourceValueCard(
    label: String,
    valueText: String,
    ratio: Double,
    subtitle: String? = null,
) {
    DashboardCard {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column {
                Text(label, style = MaterialTheme.typography.labelMedium, color = AppColors.slate500)
                if (subtitle != null) {
                    Text(subtitle, style = MaterialTheme.typography.bodySmall, color = AppColors.slate400)
                }
            }
            Text(valueText, style = MaterialTheme.typography.titleMedium, color = AppColors.primary)
        }
        Spacer(Modifier.height(10.dp))
        ProgressTrack(ratios = listOf(ratio.coerceIn(0.0, 1.0).toFloat()))
    }
}

/** 多层进度轨道（用于叠加出多条对比横条）。 */
@Composable
private fun ProgressTrack(ratios: List<Float>) {
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        ratios.forEach { r ->
            Canvas(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(8.dp),
            ) {
                val tractWidth = size.width
                val color = progressColor(r)
                // 背景轨道
                drawLine(
                    color = AppColors.slate200,
                    start = Offset(0f, size.height / 2),
                    end = Offset(tractWidth, size.height / 2),
                    strokeWidth = size.height,
                    cap = StrokeCap.Round,
                )
                // 进度
                drawLine(
                    color = color,
                    start = Offset(0f, size.height / 2),
                    end = Offset(tractWidth.coerceAtLeast(0f) * r, size.height / 2),
                    strokeWidth = size.height,
                    cap = StrokeCap.Round,
                )
            }
        }
    }
}

// --- 任务统计卡片 -----------------------------------------------------------

@Composable
private fun TaskStatsCard(stats: DashboardStats) {
    DashboardCard {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            StatCell("总数", stats.taskCount.toString())
            StatCell("启用", stats.enabledTasks.toString())
            StatCell("运行中", stats.runningTasks.toString())
            StatCell("禁用", stats.disabledTasks.toString())
        }
        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp), color = AppColors.glassCardBorder)
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            StatCell("今日成功", stats.todaySuccess.toString(), color = AppColors.successColor)
            StatCell("今日失败", stats.todayFailed.toString(), color = AppColors.errorColor)
            StatCell("今日中止", stats.todayAborted.toString(), color = AppColors.amber500)
        }
    }
}

@Composable
private fun StatCell(label: String, value: String, color: Color = AppColors.slate800) {
    Column(horizontalAlignment = Alignment.CenterHorizontally) {
        Text(value, style = MaterialTheme.typography.titleMedium, color = color)
        Spacer(Modifier.height(2.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = AppColors.slate500)
    }
}

// --- 7 日趋势（Canvas 折线） ------------------------------------------------

@Composable
private fun TrendSection(daily: List<DailyStat>) {
    val success = daily.map { it.success }.maxOrNull()?.toFloat() ?: 0f
    val failed = daily.map { it.failed }.maxOrNull()?.toFloat() ?: 0f
    val aborted = daily.map { it.aborted }.maxOrNull()?.toFloat() ?: 0f
    val maxY = listOf(success, failed, aborted).maxOrNull()?.takeIf { it > 0 } ?: 1f

    DashboardCard {
        Canvas(modifier = Modifier.fillMaxWidth().height(180.dp)) {
            val padLeft = 6.dp.toPx()
            val padRight = 16.dp.toPx()
            val padTop = 8.dp.toPx()
            val padBottom = 22.dp.toPx()
            val chartW = size.width - padLeft - padRight
            val chartH = size.height - padTop - padBottom
            val n = daily.size.coerceAtLeast(2)

            fun xAt(i: Int): Float = padLeft + (chartW * i / (n - 1).toFloat())
            fun yAt(v: Long): Float = padTop + chartH - (chartH * (v.toFloat() / maxY))

            fun linePath(values: List<Long>): Path = Path().apply {
                values.forEachIndexed { i, v ->
                    val x = xAt(i)
                    val y = yAt(v)
                    if (i == 0) moveTo(x, y) else lineTo(x, y)
                }
            }

            // 网格线
            val gridRows = 4
            for (i in 0..gridRows) {
                val y = padTop + chartH * (i / gridRows.toFloat())
                drawLine(
                    color = AppColors.slate100,
                    start = Offset(padLeft, y),
                    end = Offset(padLeft + chartW, y),
                    strokeWidth = 1.dp.toPx(),
                )
            }

            // 三条折线
            drawTrendLine(linePath(daily.map { it.success }), AppColors.successColor)
            drawTrendLine(linePath(daily.map { it.failed }), AppColors.errorColor)
            drawTrendLine(linePath(daily.map { it.aborted }), AppColors.amber500)

            // 日期标签
            daily.forEachIndexed { i, d ->
                drawContext.canvas.nativeCanvas.apply {
                    val paint = android.graphics.Paint().apply {
                        color = android.graphics.Color.parseColor("#64748B")
                        textSize = 10.sp.toPx()
                        textAlign = android.graphics.Paint.Align.CENTER
                    }
                    drawText(d.date, xAt(i), size.height - 6.dp.toPx(), paint)
                }
            }
        }
        Spacer(Modifier.height(4.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            TrendLegend(AppColors.successColor, "成功")
            TrendLegend(AppColors.errorColor, "失败")
            TrendLegend(AppColors.amber500, "中止")
        }
    }
}

private fun androidx.compose.ui.graphics.drawscope.DrawScope.drawTrendLine(path: Path, color: Color) {
    drawPath(
        path = path,
        color = color,
        style = Stroke(width = 2.5.dp.toPx(), cap = StrokeCap.Round),
    )
}

@Composable
private fun TrendLegend(color: Color, label: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Canvas(modifier = Modifier.width(10.dp).height(10.dp)) {
            drawCircle(color = color, radius = size.minDimension / 2)
        }
        Spacer(Modifier.width(4.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = AppColors.slate500)
    }
}

// --- 通用卡片与工具函数 -------------------------------------------------------

@Composable
private fun DashboardCard(content: @Composable () -> Unit) {
    Surface(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(12.dp),
        color = AppColors.glassCard,
        shadowElevation = 2.dp,
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            content()
        }
    }
}

private fun formatPercent(value: Double): String = "${(value * 100).let { if (it < 10) "%.1f".format(it) else "%.0f".format(it) }}%"

private fun formatBytes(bytes: Long): String {
    var b = bytes
    if (b < 0) b = 0
    return when {
        b < 1024 -> "${b}B"
        b < 1024 * 1024 -> "%.1fKB".format(b / 1024.0)
        b < 1024 * 1024 * 1024 -> "%.1fMB".format(b / 1024.0 / 1024.0)
        else -> "%.1fGB".format(b / 1024.0 / 1024.0 / 1024.0)
    }
}

private fun progressColor(ratio: Float): Color = when {
    ratio >= 0.9f -> AppColors.errorColor
    ratio >= 0.7f -> AppColors.amber500
    else -> AppColors.primary
}
