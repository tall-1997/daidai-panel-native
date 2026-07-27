import 'package:flutter/material.dart';

import '../../core/network/api_endpoints.dart';
import '../../core/network/dio_client.dart';
import '../../core/theme/app_theme.dart';
import 'api_utils.dart';

class LogSurfaceTheme {
  final Color background;
  final Color foreground;
  final Color mutedForeground;
  final Brightness brightness;

  const LogSurfaceTheme({
    required this.background,
    required this.foreground,
    required this.mutedForeground,
    required this.brightness,
  });
}

Future<Color?> loadPanelLogBackgroundColor() async {
  try {
    final response = await DioClient.instance.dio.get(
      ApiEndpoints.panelSettings,
    );
    final data = extractData(response.data);
    if (data is! Map<String, dynamic>) {
      return null;
    }
    return parseColorSetting(data['log_background_color']?.toString());
  } catch (_) {
    return null;
  }
}

Color? parseColorSetting(String? raw) {
  final text = raw?.trim() ?? '';
  if (text.isEmpty) {
    return null;
  }

  if (text.startsWith('#')) {
    final hex = text.substring(1);
    if (hex.length == 6) {
      final value = int.tryParse(hex, radix: 16);
      if (value != null) {
        return Color(0xFF000000 | value);
      }
    }
    if (hex.length == 8) {
      final value = int.tryParse(hex, radix: 16);
      if (value != null) {
        return Color(value);
      }
    }
  }

  final rgb = RegExp(
    r'^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*([0-9]*\.?[0-9]+))?\s*\)$',
    caseSensitive: false,
  ).firstMatch(text);
  if (rgb != null) {
    final r = int.tryParse(rgb.group(1) ?? '');
    final g = int.tryParse(rgb.group(2) ?? '');
    final b = int.tryParse(rgb.group(3) ?? '');
    final alphaText = rgb.group(4);
    if (r != null && g != null && b != null) {
      final opacity = alphaText == null
          ? 1.0
          : (double.tryParse(alphaText) ?? 1.0).clamp(0.0, 1.0);
      return Color.fromRGBO(
        r.clamp(0, 255),
        g.clamp(0, 255),
        b.clamp(0, 255),
        opacity,
      );
    }
  }

  return null;
}

LogSurfaceTheme resolveLogSurfaceTheme(
  Color? configuredColor, {
  Brightness appBrightness = Brightness.light,
}) {
  final background = configuredColor ??
      (appBrightness == Brightness.dark
          ? AppColors.termBgDark
          : AppColors.termBg);
  final brightness = ThemeData.estimateBrightnessForColor(background);
  final isDark = brightness == Brightness.dark;

  return LogSurfaceTheme(
    background: background,
    foreground: isDark ? AppColors.slate50 : AppColors.slate900,
    mutedForeground: isDark ? AppColors.slate300 : AppColors.slate500,
    brightness: brightness,
  );
}
