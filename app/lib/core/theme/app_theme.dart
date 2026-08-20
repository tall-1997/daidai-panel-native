import 'package:flutter/material.dart';

import 'theme_provider.dart';

/// 设计系统色板 — 基于 Emerald + Slate
class AppColors {
  // Primary
  static const primary = Color(0xFF10B981); // Emerald-500
  static const primaryLight = Color(0xFFD1FAE5); // Emerald-100
  static const primaryDark = Color(0xFF059669); // Emerald-600

  // Slate 体系
  static const slate50 = Color(0xFFF8FAFC);
  static const slate100 = Color(0xFFF1F5F9);
  static const slate200 = Color(0xFFE2E8F0);
  static const slate300 = Color(0xFFCBD5E1);
  static const slate400 = Color(0xFF94A3B8);
  static const slate500 = Color(0xFF64748B);
  static const slate600 = Color(0xFF475569);
  static const slate700 = Color(0xFF334155);
  static const slate800 = Color(0xFF1E293B);
  static const slate900 = Color(0xFF0F172A);
  static const slate950 = Color(0xFF020617);

  // 液态玻璃色板
  static const glassBg = Color(0xFFF2F2F7);
  static const glassCard = Color(0xFFFFFFFF);
  static const glassCardBorder = Color(0xFFE5E5EA);
  static const glassDivider = Color(0xFFE5E5EA);
  static const lightPage = Color(0xFFF4F7F9);
  static const darkPage = Color(0xFF07111F);
  static const lightSurface = Color(0xE6FFFFFF);
  static const darkSurface = Color(0xD91A2638);
  static const lightSurfaceMuted = Color(0xCCF8FAFC);
  static const darkSurfaceMuted = Color(0xCC111C2D);
  static const lightBorder = Color(0x99FFFFFF);
  static const darkBorder = Color(0x6636475C);
  static const lightControl = Color(0x2EFFFFFF);
  static const darkControl = Color(0x52111C2D);
  static const lightControlPressed = Color(0x52FFFFFF);
  static const darkControlPressed = Color(0x70334459);
  static const miuixRed = Color(0xFFE5534B);
  static const miuixGreen = Color(0xFF30A14E);
  static const miuixBlue = Color(0xFF3B82F6);
  static const miuixPurple = Color(0xFF8B5CF6);
  static const miuixYellow = Color(0xFFD4A017);

  // 功能色
  static const blue500 = Color(0xFF3B82F6);
  static const blue600 = Color(0xFF2563EB);
  static const blue100 = Color(0xFFDBEAFE);
  static const purple500 = Color(0xFF8B5CF6);
  static const purple600 = Color(0xFF7C3AED);
  static const purple100 = Color(0xFFEDE9FE);
  static const red500 = Color(0xFFEF4444);
  static const red600 = Color(0xFFDC2626);
  static const red100 = Color(0xFFFEE2E2);
  static const red50 = Color(0xFFFEF2F2);
  static const amber500 = Color(0xFFF59E0B);

  // 日志终端
  static const termBg = Colors.white;
  static const termBgDark = Color(0xFF000000);
  static const termText = Color(0xFF0F172A); // slate-900
  static const termBlue = Color(0xFF60A5FA); // blue-400
  static const termGreen = Color(0xFF34D399); // emerald-400
  static const termRed = Color(0xFFF87171); // red-400
}

class AppTheme {
  static ThemeData light({
    AppVisualStyle visualStyle = AppVisualStyle.pureFlat,
  }) {
    final colorScheme = ColorScheme.fromSeed(
      seedColor: AppColors.primary,
      brightness: Brightness.light,
      primary: AppColors.primary,
      onPrimary: Colors.white,
      secondary: AppColors.blue500,
      surface: visualStyle == AppVisualStyle.pureFlat
          ? Colors.white
          : AppColors.lightSurface,
      onSurface: AppColors.slate900,
      onSurfaceVariant: AppColors.slate500,
      outline: AppColors.glassCardBorder,
      outlineVariant: AppColors.slate100,
      error: AppColors.red500,
      surfaceContainerHighest: AppColors.slate100,
    );
    return _buildTheme(colorScheme, visualStyle);
  }

  static ThemeData dark({
    AppVisualStyle visualStyle = AppVisualStyle.pureFlat,
  }) {
    final colorScheme = ColorScheme.fromSeed(
      seedColor: AppColors.primary,
      brightness: Brightness.dark,
      primary: AppColors.primary,
      onPrimary: Colors.white,
      secondary: AppColors.blue500,
      surface: visualStyle == AppVisualStyle.pureFlat
          ? AppColors.slate900
          : AppColors.darkSurface,
      onSurface: AppColors.slate50,
      onSurfaceVariant: AppColors.slate400,
      outline: AppColors.slate800,
      outlineVariant: AppColors.slate800,
      error: AppColors.red500,
      surfaceContainerHighest: AppColors.slate900,
    );
    return _buildTheme(colorScheme, visualStyle);
  }

  static ThemeData _buildTheme(ColorScheme cs, AppVisualStyle visualStyle) {
    final isLight = cs.brightness == Brightness.light;
    final isFlat = visualStyle == AppVisualStyle.pureFlat;
    final cardColor = isFlat
        ? (isLight ? Colors.white : AppColors.slate900)
        : (isLight ? AppColors.lightSurface : AppColors.darkSurface);
    final borderColor = isFlat
        ? (isLight ? AppColors.slate200 : AppColors.slate700)
        : (isLight ? const Color(0x70FFFFFF) : AppColors.darkBorder);
    final scaffoldBg = isLight ? AppColors.lightPage : AppColors.darkPage;
    final controlColor = isFlat
        ? (isLight ? AppColors.slate100 : AppColors.slate800)
        : (isLight ? AppColors.lightControl : AppColors.darkControl);
    final pressedControlColor = isFlat
        ? (isLight ? AppColors.slate200 : AppColors.slate700)
        : (isLight
              ? AppColors.lightControlPressed
              : AppColors.darkControlPressed);
    final overlayGlassColor = isFlat
        ? cardColor
        : (isLight
              ? Colors.white.withAlpha(188)
              : AppColors.slate900.withAlpha(196));
    final modalSurfaceColor = isFlat
        ? cardColor
        : (isLight
              ? Colors.white.withAlpha(236)
              : AppColors.slate900.withAlpha(236));
    final modalBarrierColor = isLight
        ? AppColors.slate950.withAlpha(64)
        : Colors.black.withAlpha(96);

    Color resolveControlColor(Set<WidgetState> states) {
      if (states.contains(WidgetState.disabled)) {
        return isFlat
            ? Color.alphaBlend(
                controlColor.withAlpha(isLight ? 120 : 105),
                cardColor,
              )
            : controlColor.withAlpha(isLight ? 120 : 105);
      }
      if (states.contains(WidgetState.pressed) ||
          states.contains(WidgetState.hovered) ||
          states.contains(WidgetState.focused)) {
        return pressedControlColor;
      }
      return controlColor;
    }

    BorderSide resolveControlSide(Set<WidgetState> states) {
      final selected = states.contains(WidgetState.selected) ||
          states.contains(WidgetState.pressed) ||
          states.contains(WidgetState.focused);
      return BorderSide(
        color: selected
            ? AppColors.primary.withAlpha(isLight ? 120 : 150)
            : borderColor,
        width: selected ? 1.2 : 1,
      );
    }

    return ThemeData(
      useMaterial3: true,
      colorScheme: cs,
      scaffoldBackgroundColor: scaffoldBg,
      appBarTheme: AppBarTheme(
        centerTitle: false,
        elevation: 0,
        scrolledUnderElevation: 0,
        backgroundColor: isFlat
            ? scaffoldBg
            : scaffoldBg.withAlpha(isLight ? 224 : 230),
        foregroundColor: cs.onSurface,
        titleTextStyle: TextStyle(
          color: cs.onSurface,
          fontSize: 24,
          fontWeight: FontWeight.w700,
        ),
      ),
      cardTheme: CardThemeData(
        elevation: 0,
        color: cardColor,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(16),
          side: BorderSide(color: borderColor, width: 0.5),
        ),
        margin: const EdgeInsets.only(bottom: 10),
      ),
      inputDecorationTheme: InputDecorationTheme(
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: borderColor),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: BorderSide(color: borderColor),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(12),
          borderSide: const BorderSide(color: AppColors.primary, width: 1.5),
        ),
        filled: true,
        fillColor: controlColor,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: 16,
          vertical: 14,
        ),
        hintStyle: TextStyle(
          color: isLight ? AppColors.slate300 : AppColors.slate600,
          fontSize: 14,
        ),
        labelStyle: TextStyle(
          color: cs.onSurfaceVariant,
          fontSize: 12,
          fontWeight: FontWeight.w500,
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: ButtonStyle(
          backgroundColor: WidgetStateProperty.resolveWith(resolveControlColor),
          foregroundColor: const WidgetStatePropertyAll(AppColors.primary),
          overlayColor: WidgetStatePropertyAll(
            AppColors.primary.withAlpha(isLight ? 18 : 28),
          ),
          minimumSize: const WidgetStatePropertyAll(Size(0, 48)),
          padding: const WidgetStatePropertyAll(
            EdgeInsets.symmetric(horizontal: 18, vertical: 12),
          ),
          side: WidgetStateProperty.resolveWith(resolveControlSide),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
          ),
          elevation: const WidgetStatePropertyAll(0),
          textStyle: const WidgetStatePropertyAll(
            TextStyle(fontSize: 15, fontWeight: FontWeight.w700),
          ),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: ButtonStyle(
          backgroundColor: WidgetStateProperty.resolveWith(resolveControlColor),
          foregroundColor: WidgetStatePropertyAll(cs.onSurface),
          overlayColor: WidgetStatePropertyAll(
            AppColors.primary.withAlpha(isLight ? 16 : 24),
          ),
          minimumSize: const WidgetStatePropertyAll(Size(0, 48)),
          padding: const WidgetStatePropertyAll(
            EdgeInsets.symmetric(horizontal: 16, vertical: 12),
          ),
          side: WidgetStateProperty.resolveWith(resolveControlSide),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
          ),
          elevation: const WidgetStatePropertyAll(0),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: ButtonStyle(
          backgroundColor: WidgetStateProperty.resolveWith((states) {
            if (states.contains(WidgetState.pressed) ||
                states.contains(WidgetState.hovered)) {
              return pressedControlColor;
            }
            return isFlat
                ? Color.alphaBlend(
                    controlColor.withAlpha(isLight ? 120 : 105),
                    cardColor,
                  )
                : controlColor.withAlpha(isLight ? 120 : 105);
          }),
          foregroundColor: WidgetStatePropertyAll(cs.primary),
          overlayColor: WidgetStatePropertyAll(
            AppColors.primary.withAlpha(isLight ? 16 : 24),
          ),
          padding: const WidgetStatePropertyAll(
            EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          ),
          side: WidgetStatePropertyAll(
            BorderSide(color: borderColor, width: 0.8),
          ),
          shape: WidgetStatePropertyAll(
            RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
          ),
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: ButtonStyle(
          backgroundColor: WidgetStateProperty.resolveWith(resolveControlColor),
          foregroundColor: WidgetStatePropertyAll(cs.onSurfaceVariant),
          overlayColor: WidgetStatePropertyAll(
            AppColors.primary.withAlpha(isLight ? 18 : 28),
          ),
          minimumSize: const WidgetStatePropertyAll(Size(44, 44)),
          side: WidgetStateProperty.resolveWith(resolveControlSide),
          shape: const WidgetStatePropertyAll(CircleBorder()),
        ),
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.all(Colors.white),
        trackColor: WidgetStateProperty.resolveWith((states) {
          return states.contains(WidgetState.selected)
              ? AppColors.primary
              : isLight
              ? AppColors.slate300
              : AppColors.slate700;
        }),
        trackOutlineColor: WidgetStateProperty.resolveWith((states) {
          return states.contains(WidgetState.selected)
              ? Colors.transparent
              : isLight
              ? AppColors.slate400
              : AppColors.slate600;
        }),
      ),
      navigationBarTheme: NavigationBarThemeData(
        elevation: 0,
        height: 60,
        backgroundColor: isFlat ? cardColor : Colors.transparent,
        surfaceTintColor: Colors.transparent,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        indicatorColor: isFlat
            ? Color.alphaBlend(
                AppColors.primary.withAlpha(isLight ? 28 : 42),
                cardColor,
              )
            : Colors.transparent,
        iconTheme: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return const IconThemeData(color: AppColors.primary, size: 22);
          }
          return IconThemeData(color: cs.onSurfaceVariant, size: 22);
        }),
        labelTextStyle: WidgetStateProperty.resolveWith((states) {
          if (states.contains(WidgetState.selected)) {
            return const TextStyle(
              color: AppColors.primary,
              fontSize: 10,
              fontWeight: FontWeight.w600,
            );
          }
          return TextStyle(color: cs.onSurfaceVariant, fontSize: 10);
        }),
      ),
      dividerTheme: DividerThemeData(
        color: borderColor,
        thickness: 0.5,
        space: 0,
      ),
      chipTheme: ChipThemeData(
        backgroundColor: overlayGlassColor,
        selectedColor: pressedControlColor,
        disabledColor: isLight ? AppColors.slate100 : AppColors.slate800,
        side: BorderSide(color: borderColor),
        labelStyle: TextStyle(color: cs.onSurfaceVariant, fontSize: 12),
        secondaryLabelStyle: const TextStyle(
          color: AppColors.primary,
          fontSize: 12,
          fontWeight: FontWeight.w600,
        ),
        surfaceTintColor: Colors.transparent,
        padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 5),
        labelPadding: const EdgeInsets.symmetric(horizontal: 7),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: AppColors.primary,
        unselectedLabelColor: cs.onSurfaceVariant,
        indicatorColor: AppColors.primary,
        dividerColor: Colors.transparent,
        overlayColor: WidgetStatePropertyAll(
          AppColors.primary.withAlpha(isLight ? 14 : 24),
        ),
        labelStyle: const TextStyle(fontWeight: FontWeight.w700),
        unselectedLabelStyle: const TextStyle(fontWeight: FontWeight.w500),
      ),
      listTileTheme: ListTileThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
        ),
        contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 2),
      ),
      bottomSheetTheme: BottomSheetThemeData(
        backgroundColor: modalSurfaceColor,
        modalBarrierColor: modalBarrierColor,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        modalElevation: 0,
        shadowColor: Colors.transparent,
        showDragHandle: true,
        dragHandleColor: isLight
            ? AppColors.slate300
            : AppColors.slate500,
        dragHandleSize: const Size(36, 4),
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
        ),
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: overlayGlassColor,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        shadowColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(18),
          side: BorderSide(color: borderColor),
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: modalSurfaceColor,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        shadowColor: Colors.transparent,
        barrierColor: modalBarrierColor,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(24),
          side: BorderSide(color: borderColor),
        ),
        actionsPadding: const EdgeInsets.fromLTRB(24, 8, 24, 20),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: isFlat
            ? cardColor
            : (isLight
                  ? AppColors.lightControlPressed
                  : AppColors.darkControlPressed),
        contentTextStyle: TextStyle(
          color: cs.onSurface,
          fontSize: 14,
          fontWeight: FontWeight.w600,
        ),
        actionTextColor: AppColors.primary,
        elevation: 0,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(18),
          side: BorderSide(color: borderColor),
        ),
        behavior: SnackBarBehavior.floating,
        insetPadding: const EdgeInsets.fromLTRB(16, 8, 16, 92),
        showCloseIcon: true,
        closeIconColor: cs.onSurfaceVariant,
        dismissDirection: DismissDirection.horizontal,
      ),
    );
  }

  // 状态颜色
  static const Color successColor = AppColors.primary;
  static const Color errorColor = AppColors.red500;
  static const Color warningColor = AppColors.amber500;
  static const Color runningColor = AppColors.primary;
  static const Color disabledColor = AppColors.slate300;
}
