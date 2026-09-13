package com.daidai.daidai_app.ui.theme

import androidx.compose.ui.graphics.Color

/**
 * 设计系统色板 — 基于 Emerald + Slate。
 *
 * 迁移自 Flutter `app/lib/core/theme/app_theme.dart`（AppColors 第 6–68 行），
 * 与 Compose 原生工程共享同一套令牌。参见规划文档 §1.5 主题令牌清单。
 */
object AppColors {
    // Primary
    val primary = Color(0xFF10B981)      // Emerald-500
    val primaryLight = Color(0xFFD1FAE5) // Emerald-100
    val primaryDark = Color(0xFF059669)  // Emerald-600

    // Slate 体系（11 阶）
    val slate50 = Color(0xFFF8FAFC)
    val slate100 = Color(0xFFF1F5F9)
    val slate200 = Color(0xFFE2E8F0)
    val slate300 = Color(0xFFCBD5E1)
    val slate400 = Color(0xFF94A3B8)
    val slate500 = Color(0xFF64748B)
    val slate600 = Color(0xFF475569)
    val slate700 = Color(0xFF334155)
    val slate800 = Color(0xFF1E293B)
    val slate900 = Color(0xFF0F172A)
    val slate950 = Color(0xFF020617)

    // 液态玻璃色板（15 个）
    val glassBg = Color(0xFFF2F2F7)
    val glassCard = Color(0xFFFFFFFF)
    val glassCardBorder = Color(0xFFE5E5EA)
    val glassDivider = Color(0xFFE5E5EA)
    val lightPage = Color(0xFFF4F7F9)
    val darkPage = Color(0xFF07111F)
    val lightSurface = Color(0xE6FFFFFF)
    val darkSurface = Color(0xD91A2638)
    val lightSurfaceMuted = Color(0xCCF8FAFC)
    val darkSurfaceMuted = Color(0xCC111C2D)
    val lightBorder = Color(0x99FFFFFF)
    val darkBorder = Color(0x6636475C)
    val lightControl = Color(0x2EFFFFFF)
    val darkControl = Color(0x52111C2D)
    val lightControlPressed = Color(0x52FFFFFF)
    val darkControlPressed = Color(0x70334459)

    // MIUIX 色（5 个）
    val miuixRed = Color(0xFFE5534B)
    val miuixGreen = Color(0xFF30A14E)
    val miuixBlue = Color(0xFF3B82F6)
    val miuixPurple = Color(0xFF8B5CF6)
    val miuixYellow = Color(0xFFD4A017)

    // 功能色（11 个）
    val blue500 = Color(0xFF3B82F6)
    val blue600 = Color(0xFF2563EB)
    val blue100 = Color(0xFFDBEAFE)
    val purple500 = Color(0xFF8B5CF6)
    val purple600 = Color(0xFF7C3AED)
    val purple100 = Color(0xFFEDE9FE)
    val red500 = Color(0xFFEF4444)
    val red600 = Color(0xFFDC2626)
    val red100 = Color(0xFFFEE2E2)
    val red50 = Color(0xFFFEF2F2)
    val amber500 = Color(0xFFF59E0B)

    // 日志终端（5 个）
    val termBg = Color(0xFFFFFFFF)
    val termBgDark = Color(0xFF000000)
    val termText = Color(0xFF0F172A)     // slate-900
    val termBlue = Color(0xFF60A5FA)     // blue-400
    val termGreen = Color(0xFF34D399)    // emerald-400
    val termRed = Color(0xFFF87171)      // red-400

    // 状态色（5 个）
    val successColor = primary
    val errorColor = red500
    val warningColor = amber500
    val runningColor = primary
    val disabledColor = slate300
}
