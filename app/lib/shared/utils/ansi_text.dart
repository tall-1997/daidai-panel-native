import 'package:flutter/material.dart';

import '../../core/theme/app_theme.dart';

class AnsiTextTheme {
  final Color foreground;
  final Color background;

  const AnsiTextTheme({required this.foreground, required this.background});
}

class AnsiTextParser {
  static final RegExp _ansiPattern = RegExp(r'\x1B\[([0-9;]*)m');

  static TextSpan buildTextSpan(
    String text, {
    required TextStyle baseStyle,
    required Brightness brightness,
  }) {
    final palette = _paletteForBrightness(
      brightness,
      defaultForeground: baseStyle.color,
    );
    final spans = <InlineSpan>[];
    var state = _AnsiStyleState.defaults(palette);
    var cursor = 0;

    for (final match in _ansiPattern.allMatches(text)) {
      if (match.start > cursor) {
        spans.add(
          TextSpan(
            text: text.substring(cursor, match.start),
            style: state.toTextStyle(baseStyle),
          ),
        );
      }

      final codes = _parseCodes(match.group(1));
      state = state.applyCodes(codes, palette);
      cursor = match.end;
    }

    if (cursor < text.length) {
      spans.add(
        TextSpan(
          text: text.substring(cursor),
          style: state.toTextStyle(baseStyle),
        ),
      );
    }

    if (spans.isEmpty) {
      spans.add(TextSpan(text: text, style: baseStyle));
    }

    return TextSpan(children: spans, style: baseStyle);
  }

  static List<int> _parseCodes(String? raw) {
    if (raw == null || raw.isEmpty) {
      return const [0];
    }
    return raw
        .split(';')
        .map((item) => int.tryParse(item) ?? 0)
        .toList(growable: false);
  }

  static _AnsiPalette _paletteForBrightness(
    Brightness brightness, {
    Color? defaultForeground,
  }) {
    if (brightness == Brightness.dark) {
      return _AnsiPalette(
        defaultForeground: defaultForeground ?? AppColors.slate50,
        defaultBackground: Colors.transparent,
        colors: const [
          AppColors.slate400,
          Color(0xFFF87171),
          AppColors.termGreen,
          Color(0xFFFBBF24),
          AppColors.termBlue,
          Color(0xFFC084FC),
          Color(0xFF22D3EE),
          Color(0xFFE5E7EB),
        ],
        brightColors: const [
          AppColors.slate200,
          Color(0xFFFCA5A5),
          Color(0xFF6EE7B7),
          Color(0xFFFCD34D),
          Color(0xFF93C5FD),
          Color(0xFFD8B4FE),
          Color(0xFF67E8F9),
          Color(0xFFFFFFFF),
        ],
      );
    }

    return _AnsiPalette(
      defaultForeground: defaultForeground ?? AppColors.slate700,
      defaultBackground: Colors.transparent,
      colors: const [
        Color(0xFF111827),
        Color(0xFFDC2626),
        Color(0xFF059669),
        Color(0xFFD97706),
        Color(0xFF2563EB),
        Color(0xFF7C3AED),
        Color(0xFF0F766E),
        Color(0xFFE5E7EB),
      ],
      brightColors: const [
        Color(0xFF6B7280),
        Color(0xFFEF4444),
        Color(0xFF10B981),
        Color(0xFFF59E0B),
        Color(0xFF3B82F6),
        Color(0xFF8B5CF6),
        Color(0xFF14B8A6),
        Color(0xFFF8FAFC),
      ],
    );
  }
}

class _AnsiPalette {
  final Color defaultForeground;
  final Color defaultBackground;
  final List<Color> colors;
  final List<Color> brightColors;

  const _AnsiPalette({
    required this.defaultForeground,
    required this.defaultBackground,
    required this.colors,
    required this.brightColors,
  });
}

class _AnsiStyleState {
  final Color foreground;
  final Color background;
  final bool bold;

  const _AnsiStyleState({
    required this.foreground,
    required this.background,
    required this.bold,
  });

  factory _AnsiStyleState.defaults(_AnsiPalette palette) {
    return _AnsiStyleState(
      foreground: palette.defaultForeground,
      background: palette.defaultBackground,
      bold: false,
    );
  }

  _AnsiStyleState applyCodes(List<int> codes, _AnsiPalette palette) {
    var nextForeground = foreground;
    var nextBackground = background;
    var nextBold = bold;

    for (var i = 0; i < codes.length; i++) {
      final code = codes[i];
      switch (code) {
        case 0:
          nextForeground = palette.defaultForeground;
          nextBackground = palette.defaultBackground;
          nextBold = false;
          break;
        case 1:
          nextBold = true;
          break;
        case 22:
          nextBold = false;
          break;
        case 39:
          nextForeground = palette.defaultForeground;
          break;
        case 49:
          nextBackground = palette.defaultBackground;
          break;
        default:
          if (code >= 30 && code <= 37) {
            nextForeground = palette.colors[code - 30];
          } else if (code >= 90 && code <= 97) {
            nextForeground = palette.brightColors[code - 90];
          } else if (code >= 40 && code <= 47) {
            nextBackground = palette.colors[code - 40];
          } else if (code >= 100 && code <= 107) {
            nextBackground = palette.brightColors[code - 100];
          } else if (code == 38 || code == 48) {
            final isForeground = code == 38;
            final parsed = _parseExtendedColor(codes, i, palette);
            if (parsed.color != null) {
              if (isForeground) {
                nextForeground = parsed.color!;
              } else {
                nextBackground = parsed.color!;
              }
            }
            i = parsed.nextIndex;
          }
      }
    }

    return _AnsiStyleState(
      foreground: nextForeground,
      background: nextBackground,
      bold: nextBold,
    );
  }

  TextStyle toTextStyle(TextStyle baseStyle) {
    return baseStyle.copyWith(
      color: foreground,
      backgroundColor: background == Colors.transparent ? null : background,
      fontWeight: bold ? FontWeight.w700 : baseStyle.fontWeight,
    );
  }

  _ExtendedColorResult _parseExtendedColor(
    List<int> codes,
    int index,
    _AnsiPalette palette,
  ) {
    if (index + 1 >= codes.length) {
      return _ExtendedColorResult(null, index);
    }

    final mode = codes[index + 1];
    if (mode == 5) {
      if (index + 2 >= codes.length) {
        return _ExtendedColorResult(null, index + 1);
      }
      return _ExtendedColorResult(
        _indexedColor(codes[index + 2], palette),
        index + 2,
      );
    }

    if (mode == 2) {
      if (index + 4 >= codes.length) {
        return _ExtendedColorResult(null, codes.length - 1);
      }
      return _ExtendedColorResult(
        Color.fromARGB(
          0xFF,
          codes[index + 2].clamp(0, 255),
          codes[index + 3].clamp(0, 255),
          codes[index + 4].clamp(0, 255),
        ),
        index + 4,
      );
    }

    return _ExtendedColorResult(null, index + 1);
  }

  Color _indexedColor(int index, _AnsiPalette palette) {
    if (index < 0) {
      return palette.defaultForeground;
    }
    if (index < 8) {
      return palette.colors[index];
    }
    if (index < 16) {
      return palette.brightColors[index - 8];
    }
    if (index >= 232 && index <= 255) {
      final level = ((index - 232) * 10) + 8;
      return Color.fromARGB(0xFF, level, level, level);
    }
    if (index >= 16 && index <= 231) {
      final normalized = index - 16;
      final red = normalized ~/ 36;
      final green = (normalized % 36) ~/ 6;
      final blue = normalized % 6;
      int component(int value) => value == 0 ? 0 : 55 + value * 40;
      return Color.fromARGB(
        0xFF,
        component(red),
        component(green),
        component(blue),
      );
    }
    return palette.defaultForeground;
  }
}

class _ExtendedColorResult {
  final Color? color;
  final int nextIndex;

  const _ExtendedColorResult(this.color, this.nextIndex);
}
