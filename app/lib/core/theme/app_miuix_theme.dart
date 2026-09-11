import 'package:flutter/material.dart';
import 'package:flutter_miuix/miuix.dart';

import 'app_theme.dart';

/// 在 Material 主题之下再注入一层 MIUIX 主题（HyperOS 配色 + 文本样式）。
///
/// 明暗亮度取自上层 [Theme]（由 [MaterialApp.themeMode] 决定），
/// 配色在 MIUIX 默认色板基础上保留应用品牌主色，避免整体换色。
class AppMiuixTheme extends StatelessWidget {
  final Widget child;

  const AppMiuixTheme({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    final brightness = Theme.of(context).brightness;
    final lightColors = lightColorScheme().copy(
      primary: AppColors.primary,
      primaryVariant: AppColors.primaryDark,
    );
    final darkColors = darkColorScheme().copy(
      primary: AppColors.primary,
      primaryVariant: AppColors.primaryDark,
    );
    final data = MiuixThemeData.of(
      brightness,
      lightColors: lightColors,
      darkColors: darkColors,
    );
    return MiuixTheme(data: data, child: child);
  }
}
