import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:file_picker/file_picker.dart';
import 'package:liquid_glass_easy/liquid_glass_easy.dart';

import '../../../core/theme/app_theme.dart';
import '../../../core/theme/theme_provider.dart';
import '../../../shared/widgets/app_card.dart';

class ThemeSettingsPage extends ConsumerWidget {
  const ThemeSettingsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(appStyleProvider);
    final isLight = Theme.of(context).brightness == Brightness.light;

    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: const Text('主题设置'),
        backgroundColor: Colors.transparent,
        elevation: 0,
      ),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: [
          _buildSectionTitle('主题模式', isLight),
          const SizedBox(height: 8),
          AppCard(
            padding: const EdgeInsets.all(6),
            child: _ThemeModeSelector(
              isLight: isLight,
              currentMode: settings.themeMode,
              onChanged: (mode) =>
                  ref.read(appStyleProvider.notifier).setThemeMode(mode),
            ),
          ),
          const SizedBox(height: 24),
          _buildSectionTitle('背景图片', isLight),
          const SizedBox(height: 8),
          AppCard(
            padding: const EdgeInsets.all(16),
            child: _BackgroundImagePicker(
              isLight: isLight,
              currentPath: settings.backgroundImagePath,
              onChanged: (path) =>
                  ref.read(appStyleProvider.notifier).setBackgroundImage(path),
            ),
          ),
          if (settings.backgroundImagePath != null) ...[
            const SizedBox(height: 24),
            _buildSectionTitle('模糊强度', isLight),
            const SizedBox(height: 8),
            AppCard(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: _BlurIntensitySlider(
                isLight: isLight,
                currentValue: settings.blurIntensity,
                onChanged: (value) =>
                    ref.read(appStyleProvider.notifier).setBlurIntensity(value),
                onChangeEnd: (_) =>
                    ref.read(appStyleProvider.notifier).flushBlurIntensity(),
              ),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildSectionTitle(String title, bool isLight) {
    return Padding(
      padding: const EdgeInsets.only(left: 4),
      child: Text(
        title,
        style: TextStyle(
          fontSize: 13,
          fontWeight: FontWeight.w600,
          color: isLight ? AppColors.slate500 : AppColors.slate400,
        ),
      ),
    );
  }
}

class _ThemeModeSelector extends ConsumerWidget {
  final bool isLight;
  final ThemeMode currentMode;
  final ValueChanged<ThemeMode> onChanged;

  const _ThemeModeSelector({
    required this.isLight,
    required this.currentMode,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Row(
      children: ThemeMode.values.map((mode) {
        final isSelected = mode == currentMode;
        return Expanded(
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 3),
            child: AppLiquidGlassSurface(
              onTap: () => onChanged(mode),
              borderRadius: 12,
              selected: isSelected,
              padding: const EdgeInsets.symmetric(vertical: 14),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    _modeIcon(mode),
                    size: 22,
                    color: isSelected
                        ? AppColors.primary
                        : (isLight ? AppColors.slate400 : AppColors.slate500),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    _modeLabel(mode),
                    style: TextStyle(
                      fontSize: 12,
                      fontWeight: isSelected
                          ? FontWeight.w600
                          : FontWeight.w400,
                      color: isSelected
                          ? AppColors.primary
                          : (isLight ? AppColors.slate500 : AppColors.slate400),
                    ),
                  ),
                ],
              ),
            ),
          ),
        );
      }).toList(),
    );
  }

  String _modeLabel(ThemeMode mode) {
    switch (mode) {
      case ThemeMode.light:
        return '浅色';
      case ThemeMode.dark:
        return '深色';
      case ThemeMode.system:
        return '跟随系统';
    }
  }

  IconData _modeIcon(ThemeMode mode) {
    switch (mode) {
      case ThemeMode.light:
        return Icons.light_mode;
      case ThemeMode.dark:
        return Icons.dark_mode;
      case ThemeMode.system:
        return Icons.settings_brightness;
    }
  }
}

class _BackgroundImagePicker extends StatelessWidget {
  final bool isLight;
  final String? currentPath;
  final ValueChanged<String?> onChanged;

  const _BackgroundImagePicker({
    required this.isLight,
    required this.currentPath,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        Row(
          children: [
            Icon(
              Icons.image_outlined,
              size: 20,
              color: isLight ? AppColors.slate500 : AppColors.slate400,
            ),
            const SizedBox(width: 12),
            Text(
              currentPath != null ? '已选择背景图片' : '未选择背景图片',
              style: TextStyle(
                fontSize: 14,
                color: isLight ? AppColors.slate700 : AppColors.slate300,
              ),
            ),
            const Spacer(),
            if (currentPath != null)
              GestureDetector(
                onTap: () => onChanged(null),
                child: Icon(
                  Icons.close,
                  size: 20,
                  color: isLight ? AppColors.slate400 : AppColors.slate500,
                ),
              ),
          ],
        ),
        const SizedBox(height: 12),
        if (currentPath != null)
          ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: Image.file(
              File(currentPath!),
              height: 80,
              width: double.infinity,
              fit: BoxFit.cover,
              errorBuilder: (_, _, _) => Container(
                height: 80,
                color: isLight ? AppColors.slate100 : AppColors.slate800,
                child: Icon(
                  Icons.broken_image,
                  color: isLight ? AppColors.slate400 : AppColors.slate500,
                ),
              ),
            ),
          ),
        const SizedBox(height: 8),
        SizedBox(
          width: double.infinity,
          child: OutlinedButton.icon(
            onPressed: () async {
              final result = await FilePicker.platform.pickFiles(
                type: FileType.image,
                allowMultiple: false,
              );
              if (result != null && result.files.isNotEmpty) {
                onChanged(result.files.single.path);
              }
            },
            icon: const Icon(Icons.add_photo_alternate, size: 18),
            label: Text(
              currentPath != null ? '更换图片' : '选择图片',
              style: const TextStyle(fontSize: 13),
            ),
          ),
        ),
      ],
    );
  }
}

class _BlurIntensitySlider extends StatelessWidget {
  final bool isLight;
  final double currentValue;
  final ValueChanged<double> onChanged;
  final ValueChanged<double> onChangeEnd;

  const _BlurIntensitySlider({
    required this.isLight,
    required this.currentValue,
    required this.onChanged,
    required this.onChangeEnd,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Icon(
          Icons.blur_on,
          size: 20,
          color: isLight ? AppColors.slate500 : AppColors.slate400,
        ),
        const SizedBox(width: 12),
        Expanded(
          child: LayoutBuilder(
            builder: (context, constraints) => LiquidGlassSlider(
              value: (currentValue / 20).clamp(0.0, 1.0),
              layout: LiquidGlassSliderLayout(width: constraints.maxWidth),
              activeColor: AppColors.primary,
              inactiveColor: isLight ? AppColors.slate200 : AppColors.slate700,
              pixelRatio: 0.8,
              onChanged: (value) => onChanged((value * 20).roundToDouble()),
              onChangeEnd: (value) => onChangeEnd((value * 20).roundToDouble()),
            ),
          ),
        ),
        SizedBox(
          width: 36,
          child: Text(
            currentValue.toStringAsFixed(0),
            textAlign: TextAlign.center,
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w500,
              color: isLight ? AppColors.slate600 : AppColors.slate300,
            ),
          ),
        ),
      ],
    );
  }
}
