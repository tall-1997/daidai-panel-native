package com.daidai.daidai_app.ui.screens.tasks

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.ui.theme.AppTheme

@Composable
fun TaskListScreen(
    viewModel: TasksViewModel = viewModel()
) {
    val uiState by viewModel.uiState
    val selectedTask by remember { derivedStateOf { uiState.selectedTask } }
    val taskStats by remember { derivedStateOf { uiState.taskStats } }

    LaunchedEffect(Unit) {
        viewModel.loadTasks()
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(AppTheme.colors.background)
            .padding(16.dp)
    ) {
        Text(
            text = "任务列表",
            fontSize = 24.sp,
            fontWeight = FontWeight.Bold,
            color = AppTheme.colors.onBackground
        )

        Spacer(modifier = Modifier.height(16.dp))

        if (uiState.isLoading) {
            CircularProgressIndicator()
        } else if (uiState.error != null) {
            Text(
                text = uiState.error ?: "加载失败",
                color = AppTheme.colors.error
            )
        } else {
            LazyColumn(
                verticalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                items(uiState.tasks) { task ->
                    TaskListItem(
                        task = task,
                        onClick = { viewModel.selectTask(task) },
                        statusColor = viewModel.getExecutionStatusColor(task.lastRunStatus),
                        statusText = viewModel.getExecutionStatusText(task.lastRunStatus)
                    )
                }
            }
        }

        selectedTask?.let { task ->
            Spacer(modifier = Modifier.height(16.dp))
            TaskStatsCard(
                task = task,
                stats = taskStats,
                isLoading = uiState.isStatsLoading,
                onClose = { viewModel.clearSelection() }
            )
        }
    }
}

@Composable
fun TaskListItem(
    task: Task,
    onClick: () -> Unit,
    statusColor: String,
    statusText: String
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable { onClick() },
        shape = RoundedCornerShape(8.dp)
    ) {
        Column(
            modifier = Modifier.padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = task.name ?: "未命名任务",
                    fontSize = 16.sp,
                    fontWeight = FontWeight.Medium
                )
                
                // 执行状态徽章
                Box(
                    modifier = Modifier
                        .background(
                            color = Color(android.graphics.Color.parseColor(statusColor)),
                            shape = RoundedCornerShape(4.dp)
                        )
                        .padding(horizontal = 8.dp, vertical = 4.dp)
                ) {
                    Text(
                        text = statusText,
                        fontSize = 12.sp,
                        color = Color.White
                    )
                }
            }

            Spacer(modifier = Modifier.height(8.dp))
            
            Text(
                text = "ID: ${task.id}",
                fontSize = 14.sp,
                color = AppTheme.colors.onSurfaceVariant
            )
            
            task.description?.let {
                Spacer(modifier = Modifier.height(4.dp))
                Text(
                    text = it,
                    fontSize = 14.sp,
                    color = AppTheme.colors.onSurfaceVariant,
                    maxLines = 2
                )
            }
        }
    }
}

@Composable
fun TaskStatsCard(
    task: Task,
    stats: com.daidai.daidai_app.data.model.TaskStats?,
    isLoading: Boolean,
    onClose: () -> Unit
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(8.dp)
    ) {
        Column(
            modifier = Modifier.padding(16.dp)
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Text(
                    text = "${task.name} - 执行统计",
                    fontSize = 18.sp,
                    fontWeight = FontWeight.Bold
                )
                
                TextButton(onClick = onClose) {
                    Text("关闭")
                }
            }

            Spacer(modifier = Modifier.height(16.dp))

            if (isLoading) {
                CircularProgressIndicator(modifier = Modifier.align(Alignment.CenterHorizontally))
            } else if (stats != null) {
                // 平均耗时
                StatRow(
                    label = "平均耗时",
                    value = "${stats.avgDuration ?: 0}秒"
                )

                // 成功率
                StatRow(
                    label = "成功率",
                    value = "${stats.successRate ?: 0}%"
                )

                // 失败次数
                StatRow(
                    label = "失败次数",
                    value = stats.failCount.toString()
                )

                // 超时次数
                StatRow(
                    label = "超时次数",
                    value = stats.timeoutCount.toString()
                )

                // 手动终止次数
                StatRow(
                    label = "手动终止次数",
                    value = stats.abortCount.toString()
                )

                // 总执行次数
                val totalRuns = (stats.successCount ?: 0) + stats.failCount + stats.timeoutCount + stats.abortCount
                StatRow(
                    label = "总执行次数",
                    value = totalRuns.toString()
                )
            } else {
                Text(
                    text = "暂无统计数据",
                    color = AppTheme.colors.onSurfaceVariant
                )
            }
        }
    }
}

@Composable
fun StatRow(label: String, value: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        horizontalArrangement = Arrangement.SpaceBetween
    ) {
        Text(
            text = label,
            fontSize = 14.sp,
            color = AppTheme.colors.onSurfaceVariant
        )
        Text(
            text = value,
            fontSize = 14.sp,
            fontWeight = FontWeight.Medium
        )
    }
}
