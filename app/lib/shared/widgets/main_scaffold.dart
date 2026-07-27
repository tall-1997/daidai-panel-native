import 'dart:io';
import 'dart:ui' show ImageFilter;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:liquid_glass_easy/liquid_glass_easy.dart';

import '../../core/theme/app_theme.dart';
import '../../core/theme/theme_provider.dart';
import 'app_card.dart';

class MainScaffold extends ConsumerStatefulWidget {
  final Widget child;

  const MainScaffold({super.key, required this.child});

  @override
  ConsumerState<MainScaffold> createState() => _MainScaffoldState();
}

class _MainScaffoldState extends ConsumerState<MainScaffold> {
  DateTime? _lastExitAttemptAt;

  int _currentIndex(BuildContext context) {
    final location = GoRouterState.of(context).matchedLocation;
    if (location.startsWith('/dashboard')) return 0;
    if (location.startsWith('/tasks')) return 1;
    if (location.startsWith('/logs')) return 2;
    if (location.startsWith('/envs')) return 3;
    if (location.startsWith('/more')) return 4;
    return 0;
  }

  Future<void> _handleBackPress(bool didPop) async {
    if (didPop) return;
    final now = DateTime.now();
    if (_lastExitAttemptAt == null ||
        now.difference(_lastExitAttemptAt!) > const Duration(seconds: 5)) {
      _lastExitAttemptAt = now;
      AppGlassNotice.show(
        context,
        '5秒内再按一次返回键退出应用',
        type: AppGlassNoticeType.warning,
        duration: const Duration(seconds: 5),
      );
      return;
    }
    await SystemNavigator.pop();
  }

  void _onTabSelected(int index) {
    switch (index) {
      case 0:
        context.go('/dashboard');
      case 1:
        context.go('/tasks');
      case 2:
        context.go('/logs');
      case 3:
        context.go('/envs');
      case 4:
        context.go('/more');
    }
  }

  Widget _buildBottomBar(BuildContext context, int idx) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    final width = (MediaQuery.sizeOf(context).width - 32)
        .clamp(220.0, 380.0)
        .toDouble();

    return LiquidGlassBottomNavBar(
      items: const [
        LiquidGlassTabBarItem(icon: Icons.space_dashboard_outlined, selectedIcon: Icons.space_dashboard, label: '主页'),
        LiquidGlassTabBarItem(icon: Icons.schedule_outlined, selectedIcon: Icons.schedule, label: '任务'),
        LiquidGlassTabBarItem(icon: Icons.terminal_outlined, selectedIcon: Icons.terminal, label: '日志'),
        LiquidGlassTabBarItem(icon: Icons.key_outlined, selectedIcon: Icons.key, label: '变量'),
        LiquidGlassTabBarItem(icon: Icons.menu_outlined, selectedIcon: Icons.menu, label: '更多'),
      ],
      selectedIndex: idx,
      onChanged: _onTabSelected,
      width: width,
      height: 64,
      margin: const EdgeInsets.only(left: 16, right: 16, bottom: 10),
      itemPadding: 5,
      itemStyle: LiquidGlassNavItemStyle(
        selectedColor: AppColors.primary,
        unselectedColor: isLight ? AppColors.slate600 : AppColors.slate300,
        iconSize: 22,
        labelFontSize: 10,
      ),
      pillStyle: LiquidGlassNavPillStyle(
        mode: LiquidGlassPillMode.both,
        animated: true,
        color: AppColors.primary.withAlpha(isLight ? 28 : 42),
        glassStyle: LiquidGlassStyle(
          appearance: LiquidGlassAppearance(
            color: AppColors.primary.withAlpha(isLight ? 22 : 34),
            blur: const LiquidGlassBlur(sigmaX: 2, sigmaY: 2),
          ),
          refraction: const LiquidGlassRefraction(
            distortion: 0.06,
            distortionWidth: 12,
            chromaticAberration: 0.001,
          ),
        ),
      ),
      style: LiquidGlassStyle(
        shape: LiquidGlassShape.roundedRectangle(
          cornerRadius: 32,
          borderWidth: 1.2,
          lightIntensity: 1.1,
          lightDirection: 80,
          borderType: const OpticalBorder(
            borderSaturation: 1.2,
            ambientIntensity: 1,
            borderSolidity: 0.3,
          ),
        ),
        appearance: LiquidGlassAppearance(
          color: isLight ? const Color(0x2EFFFFFF) : const Color(0x52111C2D),
          blur: const LiquidGlassBlur(sigmaX: 3, sigmaY: 3),
          saturation: 1.08,
        ),
        refraction: const LiquidGlassRefraction(
          distortion: 0.07,
          distortionWidth: 28,
          chromaticAberration: 0.0015,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final idx = _currentIndex(context);
    final styleSettings = ref.watch(appStyleProvider);
    final bg = styleSettings.backgroundImagePath;
    final blur = styleSettings.blurIntensity.clamp(0.0, 50.0);
    final isDark = Theme.of(context).brightness == Brightness.dark;

    Widget backgroundWidget;
    if (bg != null) {
      final imageWidget = Image.file(
        File(bg),
        fit: BoxFit.cover,
        width: double.infinity,
        height: double.infinity,
        errorBuilder: (_, _, _) => Container(
          color: Theme.of(context).scaffoldBackgroundColor,
        ),
      );
      if (blur > 0) {
        backgroundWidget = SizedBox.expand(
          child: Stack(
            fit: StackFit.expand,
            children: [
              imageWidget,
              BackdropFilter(
                filter: ImageFilter.blur(sigmaX: blur, sigmaY: blur),
                child: ColoredBox(
                  color: isDark
                      ? AppColors.darkPage.withAlpha(72)
                      : Colors.white.withAlpha(28),
                ),
              ),
            ],
          ),
        );
      } else {
        backgroundWidget = imageWidget;
      }
    } else {
      backgroundWidget =
          Container(color: Theme.of(context).scaffoldBackgroundColor);
    }

    return PopScope<void>(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) => _handleBackPress(didPop),
      child: LiquidGlassScaffold(
        pixelRatio: 0.65,
        realTimeCapture: true,
        useSync: false,
        safeArea: true,
        body: LiquidGlassView(
          pixelRatio: 0.7,
          realTimeCapture: false,
          useSync: true,
          backgroundWidget: backgroundWidget,
          child: Material(
            type: MaterialType.transparency,
            child: SizedBox.expand(child: widget.child),
          ),
        ),
        bottomNavigationBar: _buildBottomBar(context, idx),
      ),
    );
  }
}
