import 'dart:io';
import 'dart:ui';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:liquid_glass_easy/liquid_glass_easy.dart';
import '../../core/theme/theme_provider.dart';
import '../../core/theme/app_theme.dart';

/// 页面级背景组件，为二级/三级页面提供背景图片和模糊效果
/// 主页面由 MainScaffold 的 LiquidGlassScaffold 处理，不需要此组件
class AppBackground extends ConsumerWidget {
  final Widget child;

  const AppBackground({super.key, required this.child});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(appStyleProvider);
    final hasBg = settings.backgroundImagePath != null &&
        settings.backgroundImagePath!.isNotEmpty;
    final isDark = Theme.of(context).brightness == Brightness.dark;

    final blur = settings.blurIntensity.clamp(0.0, 50.0);
    final baseColor = Theme.of(context).scaffoldBackgroundColor;
    final Widget background = hasBg
        ? Stack(
            fit: StackFit.expand,
            children: [
              ColoredBox(color: baseColor),
              Image.file(
                File(settings.backgroundImagePath!),
                fit: BoxFit.cover,
                errorBuilder: (_, _, _) => ColoredBox(
                  color: Theme.of(context).scaffoldBackgroundColor,
                ),
              ),
              if (blur > 0)
                BackdropFilter(
                  filter: ImageFilter.blur(sigmaX: blur, sigmaY: blur),
                  child: ColoredBox(
                    color: isDark
                        ? AppColors.darkPage.withAlpha(72)
                        : Colors.white.withAlpha(28),
                  ),
                ),
            ],
          )
        : ColoredBox(color: baseColor);

    return LiquidGlassView(
      pixelRatio: 0.7,
      realTimeCapture: false,
      useSync: true,
      backgroundWidget: background,
      child: Material(
        type: MaterialType.transparency,
        child: SizedBox.expand(child: child),
      ),
    );
  }
}
