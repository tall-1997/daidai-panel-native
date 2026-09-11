import 'package:flutter/material.dart';
import 'package:flutter_miuix/miuix.dart';

/// MIUIX 感知的图标组件。
///
/// 参数与 Material [Icon] 完全一致，便于全局替换；仅在未显式指定 `color` 时，
/// 优先取最近 MIUIX 容器（`MiuixCard` / `MiuixButton` 等）传递的内容色
/// [MiuixContentColor]，否则回退到 [IconTheme]，保持原有主题与明暗适配。
class AppIcon extends StatelessWidget {
  final IconData? icon;
  final double? size;
  final double? fill;
  final double? weight;
  final double? grade;
  final double? opticalSize;
  final Color? color;
  final List<Shadow>? shadows;
  final String? semanticLabel;
  final TextDirection? textDirection;
  final bool? applyTextScaling;

  const AppIcon(
    this.icon, {
    super.key,
    this.size,
    this.fill,
    this.weight,
    this.grade,
    this.opticalSize,
    this.color,
    this.shadows,
    this.semanticLabel,
    this.textDirection,
    this.applyTextScaling,
  });

  @override
  Widget build(BuildContext context) {
    final miuixColor = context
        .dependOnInheritedWidgetOfExactType<MiuixContentColor>()
        ?.color;
    return Icon(
      icon,
      size: size,
      fill: fill,
      weight: weight,
      grade: grade,
      opticalSize: opticalSize,
      color: color ?? miuixColor ?? IconTheme.of(context).color,
      shadows: shadows,
      semanticLabel: semanticLabel,
      textDirection: textDirection,
      applyTextScaling: applyTextScaling,
    );
  }
}
