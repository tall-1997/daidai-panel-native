import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

enum AppVisualStyle { pureFlat, liquidGlass }

class AppStyleSettings {
  final ThemeMode themeMode;
  final AppVisualStyle visualStyle;
  final String? backgroundImagePath;
  final double blurIntensity;

  const AppStyleSettings({
    this.themeMode = ThemeMode.system,
    this.visualStyle = AppVisualStyle.pureFlat,
    this.backgroundImagePath,
    this.blurIntensity = 0,
  });

  AppStyleSettings copyWith({
    ThemeMode? themeMode,
    AppVisualStyle? visualStyle,
    String? backgroundImagePath,
    bool clearBackgroundImage = false,
    double? blurIntensity,
  }) {
    return AppStyleSettings(
      themeMode: themeMode ?? this.themeMode,
      visualStyle: visualStyle ?? this.visualStyle,
      backgroundImagePath: clearBackgroundImage
          ? null
          : backgroundImagePath ?? this.backgroundImagePath,
      blurIntensity: blurIntensity ?? this.blurIntensity,
    );
  }
}

class AppStyleNotifier extends StateNotifier<AppStyleSettings> {
  late final Future<void> initialized;
  var _themeModeRevision = 0;
  var _visualStyleRevision = 0;
  var _backgroundImageRevision = 0;
  var _blurIntensityRevision = 0;

  AppStyleNotifier({
    AppStyleSettings initialState = const AppStyleSettings(),
    bool loadPersistedSettings = true,
  }) : super(initialState) {
    initialized = loadPersistedSettings ? _load() : Future.value();
  }

  Future<void> _load() async {
    final themeModeRevision = _themeModeRevision;
    final visualStyleRevision = _visualStyleRevision;
    final backgroundImageRevision = _backgroundImageRevision;
    final blurIntensityRevision = _blurIntensityRevision;
    try {
      final prefs = await SharedPreferences.getInstance();
      final themeIndex = prefs.getInt('theme_mode') ?? 0;
      final visualStyleName = prefs.getString('visual_style');
      final bgPath = prefs.getString('background_image_path');
      final blur = prefs.getDouble('blur_intensity') ?? 0;
      state = state.copyWith(
        themeMode: _themeModeRevision == themeModeRevision
            ? (themeIndex >= 0 && themeIndex < ThemeMode.values.length
                  ? ThemeMode.values[themeIndex]
                  : ThemeMode.system)
            : state.themeMode,
        visualStyle: _visualStyleRevision == visualStyleRevision
            ? AppVisualStyle.values.firstWhere(
                (style) => style.name == visualStyleName,
                orElse: () => AppVisualStyle.pureFlat,
              )
            : state.visualStyle,
        backgroundImagePath: _backgroundImageRevision == backgroundImageRevision
            ? bgPath
            : state.backgroundImagePath,
        clearBackgroundImage: _backgroundImageRevision == backgroundImageRevision && bgPath == null,
        blurIntensity: _blurIntensityRevision == blurIntensityRevision
            ? blur
            : state.blurIntensity,
      );
    } catch (_) {
      // Keep the current state so startup can continue with Pure Flat defaults.
    }
  }

  Future<void> setThemeMode(ThemeMode mode) async {
    _themeModeRevision++;
    state = state.copyWith(themeMode: mode);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setInt('theme_mode', mode.index);
  }

  Future<void> setVisualStyle(AppVisualStyle style) async {
    _visualStyleRevision++;
    state = state.copyWith(visualStyle: style);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('visual_style', style.name);
  }

  Future<void> setBackgroundImage(String? path) async {
    _backgroundImageRevision++;
    state = path != null
        ? state.copyWith(backgroundImagePath: path)
        : state.copyWith(clearBackgroundImage: true);
    final prefs = await SharedPreferences.getInstance();
    if (path != null) {
      await prefs.setString('background_image_path', path);
    } else {
      await prefs.remove('background_image_path');
    }
  }

  Future<void> setBlurIntensity(double value) async {
    _blurIntensityRevision++;
    state = state.copyWith(blurIntensity: value);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setDouble('blur_intensity', value);
  }

  Future<void> flushBlurIntensity() async {}
}

final appStyleProvider =
    StateNotifierProvider<AppStyleNotifier, AppStyleSettings>((ref) {
      return AppStyleNotifier();
    });

final themeProvider = Provider<ThemeMode>((ref) {
  return ref.watch(appStyleProvider).themeMode;
});
