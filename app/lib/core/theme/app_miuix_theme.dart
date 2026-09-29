import 'package:flutter/material.dart';
import 'package:flutter_miuix/miuix.dart';

import 'app_visual_palette.dart';

/// 让 MIUIX 控件和 Material 主题读同一套色板。
///
/// MIUIX 用官方 HyperOS 色。Liquid Glass 只把强调色换成翠绿，
/// 避免控件树里出现两套主色。
class AppMiuixTheme extends StatelessWidget {
  final Widget child;

  const AppMiuixTheme({super.key, required this.child});

  @override
  Widget build(BuildContext context) {
    final palette = AppVisualPalette.of(context);
    final brightness = Theme.of(context).brightness;
    if (palette.isMiuix) {
      return MiuixTheme(
        data: MiuixThemeData.of(brightness),
        child: child,
      );
    }
    final official = brightness == Brightness.dark
        ? darkColorScheme()
        : lightColorScheme();
    final glass = official.copy(
      primary: palette.accent,
      primaryVariant: palette.accentStrong,
    );
    return MiuixTheme(
      data: MiuixThemeData.of(
        brightness,
        lightColors: brightness == Brightness.dark ? null : glass,
        darkColors: brightness == Brightness.dark ? glass : null,
      ),
      child: child,
    );
  }
}
