package com.daidai.daidai_app.ui.screens.applock

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.detectTapGestures
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import com.daidai.daidai_app.ui.theme.AppColors

private const val MIN_PATTERN_POINTS = 4

private enum class UnlockMethod { Password, Pattern }

/**
 * 3×3 图案锁键盘（阶段 4-2，Compose 原生，参照 Flutter `pattern_pad.dart`）。
 *
 * 点位编号 1~9；点按时追加选中，再次点击已选中点位不动作。
 * 被选中的点位按选择顺序高亮并显示顺位数字，选中点之间以直线相连。
 * 顶部可显示标题/副标题，底部提供“清空”按钮。
 *
 * @param onPointsChanged 每次点位集合变化（含清空）都会回调。
 * @param onPatternComplete 当点位达到最少数量时回调完成（由上层负责校验）。
 */
@Composable
fun PatternPad(
    initialPoints: List<Int> = emptyList(),
    onPointsChanged: (List<Int>) -> Unit = {},
    onPatternComplete: (List<Int>) -> Unit = {},
    onClear: () -> Unit = {},
    title: String? = null,
    subtitle: String? = null,
    enabled: Boolean = true,
    modifier: Modifier = Modifier,
) {
    var points by remember { mutableStateOf(initialPoints) }
    Column(
        modifier = modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (title != null) {
            Text(
                title,
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.Bold,
                color = Color.White,
            )
        }
        if (subtitle != null) {
            Spacer(Modifier.height(4.dp))
            Text(
                subtitle,
                style = MaterialTheme.typography.bodySmall,
                color = AppColors.slate400,
            )
        }
        Spacer(Modifier.height(16.dp))

        Canvas(
            modifier = Modifier
                .size(300.dp)
                .background(AppColors.darkSurfaceMuted, RoundedCornerShape(24.dp))
                .padding(24.dp)
                .pointerInput(enabled) {
                    if (!enabled) return@pointerInput
                    detectTapGestures { tap ->
                        val cell = size.width / 3f
                        val col = (tap.x / cell).toInt().coerceIn(0, 2)
                        val row = (tap.y / cell).toInt().coerceIn(0, 2)
                        val point = row * 3 + col + 1
                        if (points.contains(point)) return@detectTapGestures
                        val next = points + point
                        points = next
                        onPointsChanged(next)
                        if (next.size >= MIN_PATTERN_POINTS) {
                            // 提交完整图案后清空本地点位，等待下一轮绘制（校验结果由上游决定）。
                            points = emptyList()
                            onPointsChanged(emptyList())
                            onPatternComplete(next)
                        }
                    }
                },
        ) {
            val cell = size.width / 3f
            val centers = List(9) { i ->
                Offset(
                    (i % 3) * cell + cell / 2f,
                    (i / 3) * cell + cell / 2f,
                )
            }
            // 已选中点之间连线
            if (points.size > 1) {
                val strokeWidth = 4.dp.toPx()
                for (idx in 0 until points.size - 1) {
                    val a = centers[points[idx] - 1]
                    val b = centers[points[idx + 1] - 1]
                    drawLine(
                        color = AppColors.primary,
                        start = a,
                        end = b,
                        strokeWidth = strokeWidth,
                    )
                }
            }
            // 圆点与顺位数字
            val dotRadius = 26.dp.toPx()
            centers.forEachIndexed { i, c ->
                val number = i + 1
                val selectedAt = points.indexOf(number)
                if (selectedAt >= 0) {
                    drawCircle(color = AppColors.primary, radius = dotRadius, center = c)
                    val textSize = 18.dp.toPx()
                    drawContext.canvas.nativeCanvas.applyCommonText(
                        text = (selectedAt + 1).toString(),
                        cx = c.x,
                        cy = c.y,
                        color = Color.White,
                        textSize = textSize,
                        bold = true,
                    )
                } else {
                    drawCircle(
                        color = AppColors.slate500,
                        radius = dotRadius,
                        center = c,
                        style = Stroke(width = 2.dp.toPx()),
                    )
                }
            }
        }

        Spacer(Modifier.height(12.dp))
        OutlinedButton(
            onClick = {
                points = emptyList()
                onPointsChanged(emptyList())
                onClear()
            },
            enabled = enabled && points.isNotEmpty(),
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text("清空")
        }
    }
}

/** 在 Canvas 原生层绘制居中的文本（选中点位顺位数字）。 */
private fun android.graphics.Canvas.applyCommonText(
    text: String,
    cx: Float,
    cy: Float,
    color: Color,
    textSize: Float,
    bold: Boolean,
) {
    val paint = android.graphics.Paint().apply {
        this.color = color.toArgb()
        this.textSize = textSize
        this.textAlign = android.graphics.Paint.Align.CENTER
        this.isAntiAlias = true
        if (bold) this.typeface = android.graphics.Typeface.DEFAULT_BOLD
    }
    val baseline = cy - ((paint.fontMetrics.ascent + paint.fontMetrics.descent) / 2f)
    drawText(text, cx, baseline, paint)
}

/**
 * 应用锁门禁页（阶段 4-2，Compose 原生，参照 Flutter `action_lock_gate.dart`）。
 *
 * 作为启动门禁：由集成阶段在导航外包裹现有界面，启用锁时本屏呈现锁定覆盖层。
 * 图案点数达 4 或密码输入正确后回调 [onUnlocked]；未配置任何验证方式时给出引导。
 * 生物识别为占位（[AppLockViewModel.BIOMETRIC_NOTICE]），未在可用方法中展开。
 *
 * @param onUnlocked 解锁成功回调（集成阶段据此放行进入主界面）。
 */
@Composable
fun AppLockGateScreen(
    onUnlocked: () -> Unit,
    viewModel: AppLockViewModel? = null,
) {
    val context = LocalContext.current
    val screenViewModel = viewModel ?: remember(context) { AppLockViewModel(context) }
    val state by screenViewModel.uiState.collectAsState()

    var method by remember {
        mutableStateOf(
            if (state.hasPassword && !state.hasPattern) UnlockMethod.Password
            else UnlockMethod.Pattern,
        )
    }
    var password by remember { mutableStateOf("") }
    var patternPoints by remember { mutableStateOf<List<Int>>(emptyList()) }

    // 数据就绪且已启用锁时进入锁定态。
    LaunchedEffect(Unit) {
        if (screenViewModel.uiState.value.enabled) {
            screenViewModel.lockIfEnabled()
        }
    }

    val backoffActive =
        state.lockedUntilEpochMs > 0L && System.currentTimeMillis() < state.lockedUntilEpochMs

    Surface(modifier = Modifier.fillMaxSize(), color = AppColors.darkPage) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text("🔒", fontSize = MaterialTheme.typography.displaySmall.fontSize)
            Spacer(Modifier.height(8.dp))
            Text(
                "呆呆面板已锁定",
                style = MaterialTheme.typography.titleLarge,
                color = Color.White,
                textAlign = TextAlign.Center,
            )

            if (state.hasPassword || state.hasPattern) {
                Spacer(Modifier.height(20.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    if (state.hasPattern) {
                        MethodChip(selected = method == UnlockMethod.Pattern, text = "图案") {
                            method = UnlockMethod.Pattern
                            patternPoints = emptyList()
                        }
                    }
                    if (state.hasPassword) {
                        MethodChip(selected = method == UnlockMethod.Password, text = "密码") {
                            method = UnlockMethod.Password
                            password = ""
                        }
                    }
                }
            }

            Spacer(Modifier.height(24.dp))

            state.notice?.let { message ->
                Text(
                    message,
                    color = AppColors.red500,
                    style = MaterialTheme.typography.bodyMedium,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.padding(bottom = 12.dp),
                )
            }

            when {
                method == UnlockMethod.Pattern && state.hasPattern -> {
                    PatternPad(
                        onPatternComplete = { points ->
                            // 校验通过即解锁；否则由 ViewModel 记录失败并清空点位于 UI 层处理，
                            // 这里通过重置本地 points 让用户重新绘制。
                            if (!backoffActive) {
                                if (screenViewModel.verifyPattern(points)) {
                                    patternPoints = emptyList()
                                    onUnlocked()
                                } else {
                                    patternPoints = emptyList()
                                }
                            }
                        },
                        onPointsChanged = { patternPoints = it },
                        title = "绘制解锁图案",
                        subtitle = "请按至少 4 个点位的顺序绘制",
                        enabled = !backoffActive,
                    )
                }
                method == UnlockMethod.Password && state.hasPassword -> {
                    OutlinedTextField(
                        value = password,
                        onValueChange = { password = it },
                        label = { Text("请输入密码") },
                        modifier = Modifier.fillMaxWidth(),
                        visualTransformation = PasswordVisualTransformation(),
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        singleLine = true,
                    )
                    Spacer(Modifier.height(16.dp))
                    Button(
                        onClick = {
                            if (!backoffActive && screenViewModel.verifyPassword(password)) {
                                onUnlocked()
                            } else if (!backoffActive) {
                                password = ""
                            }
                        },
                        enabled = !backoffActive,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text("解锁")
                    }
                }
                else -> {
                    Text(
                        "请先到“应用锁设置”配置图案或密码",
                        color = AppColors.slate400,
                        style = MaterialTheme.typography.bodyMedium,
                        textAlign = TextAlign.Center,
                    )
                }
            }
        }
    }
}

@Composable
private fun MethodChip(
    selected: Boolean,
    text: String,
    onClick: () -> Unit,
) {
    Surface(
        onClick = onClick,
        shape = CircleShape,
        color = if (selected) AppColors.primary else Color.Transparent,
        modifier = Modifier.border(1.dp, if (selected) AppColors.primary else AppColors.slate500, CircleShape),
    ) {
        Text(
            text,
            color = if (selected) Color.White else AppColors.slate200,
            modifier = Modifier.padding(horizontal = 20.dp, vertical = 8.dp),
        )
    }
}
