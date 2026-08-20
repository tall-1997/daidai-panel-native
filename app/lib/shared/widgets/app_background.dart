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
    final isFlat = settings.visualStyle == AppVisualStyle.pureFlat;
    final baseColor = Theme.of(context).scaffoldBackgroundColor;
    final mediaQuery = MediaQuery.of(context);
    final cacheWidth =
        (mediaQuery.size.width * mediaQuery.devicePixelRatio).round();
    final cacheHeight =
        (mediaQuery.size.height * mediaQuery.devicePixelRatio).round();
    final image = hasBg
        ? Image(
            image: ResizeImage(
              FileImage(File(settings.backgroundImagePath!)),
              width: cacheWidth,
              height: cacheHeight,
              policy: ResizeImagePolicy.fit,
            ),
            fit: BoxFit.cover,
            errorBuilder: (_, _, _) => ColoredBox(color: baseColor),
          )
        : null;
    final Widget background = hasBg
        ? Stack(
            fit: StackFit.expand,
            children: [
              ColoredBox(color: baseColor),
              if (isFlat || blur == 0)
                image!
              else
                ImageFiltered(
                  imageFilter: ImageFilter.blur(sigmaX: blur, sigmaY: blur),
                  child: image!,
                ),
              if (!isFlat && blur > 0)
                ColoredBox(
                  color: isDark
                      ? AppColors.darkPage.withAlpha(72)
                      : Colors.white.withAlpha(28),
                ),
            ],
          )
        : ColoredBox(color: baseColor);

    if (isFlat) {
      return Stack(
        fit: StackFit.expand,
        children: [
          background,
          Material(
            type: MaterialType.transparency,
            child: SizedBox.expand(child: child),
          ),
        ],
      );
    }

    return LiquidGlassView(
      pixelRatio: 0.7,
      realTimeCapture: false,
      useSync: false,
      backgroundWidget: background,
      child: Material(
        type: MaterialType.transparency,
        child: SizedBox.expand(child: child),
      ),
    );
  }
}
