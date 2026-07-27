import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

class AppStyleSettings {
  final ThemeMode themeMode;
  final String? backgroundImagePath;
  final double blurIntensity;

  const AppStyleSettings({
    this.themeMode = ThemeMode.system,
    this.backgroundImagePath,
    this.blurIntensity = 0,
  });

  AppStyleSettings copyWith({
    ThemeMode? themeMode,
    String? backgroundImagePath,
    double? blurIntensity,
  }) {
    return AppStyleSettings(
      themeMode: themeMode ?? this.themeMode,
      backgroundImagePath: backgroundImagePath ?? this.backgroundImagePath,
      blurIntensity: blurIntensity ?? this.blurIntensity,
    );
  }
}

class AppStyleNotifier extends StateNotifier<AppStyleSettings> {
  AppStyleNotifier() : super(const AppStyleSettings()) {
    _load();
  }

  Future<void> _load() async {
    final prefs = await SharedPreferences.getInstance();
    final themeIndex = prefs.getInt('theme_mode') ?? 0;
    final bgPath = prefs.getString('background_image_path');
    final blur = prefs.getDouble('blur_intensity') ?? 0;

    state = AppStyleSettings(
      themeMode: ThemeMode.values[themeIndex],
      backgroundImagePath: bgPath,
      blurIntensity: blur,
    );
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    state = state.copyWith(themeMode: mode);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setInt('theme_mode', mode.index);
  }

  Future<void> setBackgroundImage(String? path) async {
    state = state.copyWith(backgroundImagePath: path);
    final prefs = await SharedPreferences.getInstance();
    if (path != null) {
      await prefs.setString('background_image_path', path);
    } else {
      await prefs.remove('background_image_path');
    }
  }

  Future<void> setBlurIntensity(double value) async {
    state = state.copyWith(blurIntensity: value);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setDouble('blur_intensity', value);
  }
}

final appStyleProvider =
    StateNotifierProvider<AppStyleNotifier, AppStyleSettings>((ref) {
  return AppStyleNotifier();
});

// 兼容旧代码
final themeProvider = Provider<ThemeMode>((ref) {
  return ref.watch(appStyleProvider).themeMode;
});