import 'package:flutter/material.dart';
import 'package:flutter_miuix/miuix.dart';

import 'theme_provider.dart';

/// 当前视觉风格的唯一色板。
///
/// 页面、卡片、控件和强调色都从这里读取。MIUIX 用 HyperOS 官方色，
/// Liquid Glass 用翠绿玻璃色。页面不要自己判断风格再写死颜色。
@immutable
class AppVisualPalette extends ThemeExtension<AppVisualPalette> {
  const AppVisualPalette({
    required this.style,
    required this.accent,
    required this.accentStrong,
    required this.onAccent,
    required this.page,
    required this.card,
    required this.control,
    required this.controlPressed,
    required this.text,
    required this.textSecondary,
    required this.divider,
    required this.success,
    required this.disabledAccent,
  });

  final AppVisualStyle style;
  final Color accent;
  final Color accentStrong;
  final Color onAccent;
  final Color page;
  final Color card;
  final Color control;
  final Color controlPressed;
  final Color text;
  final Color textSecondary;
  final Color divider;
  final Color success;
  final Color disabledAccent;

  bool get isMiuix => style == AppVisualStyle.miuix;

  static AppVisualPalette of(BuildContext context) {
    final palette = Theme.of(context).extension<AppVisualPalette>();
    assert(palette != null, 'ThemeData 缺少 AppVisualPalette');
    return palette!;
  }

  /// HyperOS 官方色。浅色页灰、卡片白、强调蓝；深色页黑、卡片灰、强调蓝。
  factory AppVisualPalette.miuix(MiuixColors colors, Brightness brightness) {
    final accent = colors.primary;
    return AppVisualPalette(
      style: AppVisualStyle.miuix,
      accent: accent,
      accentStrong: brightness == Brightness.light
          ? Color.alphaBlend(const Color(0x24000000), accent)
          : accent,
      onAccent: colors.onPrimary,
      page: colors.surface,
      card: colors.surfaceContainer,
      control: colors.secondaryContainer,
      controlPressed: colors.surfaceContainerHigh,
      text: colors.onSurface,
      textSecondary: colors.onSurfaceContainerVariant,
      divider: colors.dividerLine,
      success: const Color(0xFF10B981),
      disabledAccent: colors.disabledPrimaryButton,
    );
  }

  /// 未指定强调色时跟随当前风格。显式传入的其他颜色保持不变。
  Color resolve(Color color) {
    if (color == const Color(0xFF10B981)) return accent;
    return color;
  }

  @override
  AppVisualPalette copyWith({
    AppVisualStyle? style,
    Color? accent,
    Color? accentStrong,
    Color? onAccent,
    Color? page,
    Color? card,
    Color? control,
    Color? controlPressed,
    Color? text,
    Color? textSecondary,
    Color? divider,
    Color? success,
    Color? disabledAccent,
  }) {
    return AppVisualPalette(
      style: style ?? this.style,
      accent: accent ?? this.accent,
      accentStrong: accentStrong ?? this.accentStrong,
      onAccent: onAccent ?? this.onAccent,
      page: page ?? this.page,
      card: card ?? this.card,
      control: control ?? this.control,
      controlPressed: controlPressed ?? this.controlPressed,
      text: text ?? this.text,
      textSecondary: textSecondary ?? this.textSecondary,
      divider: divider ?? this.divider,
      success: success ?? this.success,
      disabledAccent: disabledAccent ?? this.disabledAccent,
    );
  }

  @override
  AppVisualPalette lerp(ThemeExtension<AppVisualPalette>? other, double t) {
    if (other is! AppVisualPalette) return this;
    return AppVisualPalette(
      style: t < 0.5 ? style : other.style,
      accent: Color.lerp(accent, other.accent, t)!,
      accentStrong: Color.lerp(accentStrong, other.accentStrong, t)!,
      onAccent: Color.lerp(onAccent, other.onAccent, t)!,
      page: Color.lerp(page, other.page, t)!,
      card: Color.lerp(card, other.card, t)!,
      control: Color.lerp(control, other.control, t)!,
      controlPressed: Color.lerp(controlPressed, other.controlPressed, t)!,
      text: Color.lerp(text, other.text, t)!,
      textSecondary: Color.lerp(textSecondary, other.textSecondary, t)!,
      divider: Color.lerp(divider, other.divider, t)!,
      success: Color.lerp(success, other.success, t)!,
      disabledAccent: Color.lerp(disabledAccent, other.disabledAccent, t)!,
    );
  }
}
