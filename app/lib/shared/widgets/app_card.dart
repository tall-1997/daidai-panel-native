import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:liquid_glass_easy/liquid_glass_easy.dart';

import '../../core/theme/app_theme.dart';
import '../../core/theme/theme_provider.dart';

class AppCard extends ConsumerWidget {
  final Widget child;
  final EdgeInsetsGeometry? padding;
  final EdgeInsetsGeometry? margin;
  final double borderRadius;
  final VoidCallback? onTap;
  final bool stableForScrolling;

  const AppCard({
    super.key,
    required this.child,
    this.padding,
    this.margin,
    this.borderRadius = 16,
    this.onTap,
    this.stableForScrolling = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    final performanceMode =
        stableForScrolling || Scrollable.maybeOf(context) != null;
    Widget card = isFlat
        ? Material(
            color: isLight ? Colors.white : AppColors.slate900,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(borderRadius),
              side: BorderSide(
                color: isLight ? AppColors.slate200 : AppColors.slate700,
              ),
            ),
            clipBehavior: Clip.antiAlias,
            child: InkWell(
              onTap: onTap,
              child: Padding(
                padding: padding ?? const EdgeInsets.all(16),
                child: child,
              ),
            ),
          )
        : LiquidGlassLens(
            style: appLiquidGlassStyle(
              isLight: isLight,
              borderRadius: borderRadius,
              performanceMode: performanceMode,
            ),
            child: Padding(
              padding: padding ?? const EdgeInsets.all(16),
              child: child,
            ),
          );

    if (!isFlat && onTap != null) {
      card = GestureDetector(onTap: onTap, child: card);
    }

    if (margin != null) {
      return Padding(padding: margin!, child: card);
    }
    return card;
  }
}

class AppListTile extends ConsumerWidget {
  final IconData icon;
  final String title;
  final VoidCallback onTap;
  final Widget? trailing;

  const AppListTile({
    super.key,
    required this.icon,
    required this.title,
    required this.onTap,
    this.trailing,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );

    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: isFlat
          ? Material(
              color: isLight ? Colors.white : AppColors.slate900,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(14),
                side: BorderSide(
                  color: isLight ? AppColors.slate200 : AppColors.slate700,
                ),
              ),
              clipBehavior: Clip.antiAlias,
              child: _buildTile(isLight),
            )
          : LiquidGlassLens(
              style: appLiquidGlassStyle(
                isLight: isLight,
                borderRadius: 14,
                performanceMode: true,
              ),
              child: Material(
                color: Colors.transparent,
                child: _buildTile(isLight),
              ),
            ),
    );
  }

  Widget _buildTile(bool isLight) => ListTile(
    leading: Icon(icon, size: 20),
    title: Text(title),
    trailing:
        trailing ??
        Icon(
          Icons.chevron_right,
          size: 18,
          color: isLight ? AppColors.slate400 : AppColors.slate600,
        ),
    onTap: onTap,
    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
  );
}

class AppGlassIconButton extends ConsumerWidget {
  final IconData icon;
  final VoidCallback? onTap;
  final String? tooltip;
  final Color accentColor;
  final double iconSize;

  const AppGlassIconButton({
    super.key,
    required this.icon,
    required this.onTap,
    this.tooltip,
    this.accentColor = AppColors.primary,
    this.iconSize = 20,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    final button = SizedBox(
      width: 44,
      height: 44,
      child: Center(
        child: SizedBox(
          width: 36,
          height: 36,
          child: isFlat
              ? Material(
                  color: isLight ? AppColors.slate100 : AppColors.slate800,
                  shape: const CircleBorder(),
                  child: InkWell(
                    customBorder: const CircleBorder(),
                    onTap: onTap,
                    child: Icon(icon, size: iconSize, color: accentColor),
                  ),
                )
              : LiquidGlassLens(
                  style: appLiquidGlassStyle(
                    isLight: isLight,
                    borderRadius: 18,
                    accentColor: accentColor,
                    selected: true,
                  ),
                  child: Material(
                    color: Colors.transparent,
                    shape: const CircleBorder(),
                    child: InkWell(
                      customBorder: const CircleBorder(),
                      onTap: onTap,
                      child: Icon(icon, size: iconSize, color: accentColor),
                    ),
                  ),
                ),
        ),
      ),
    );
    if (tooltip == null) {
      return button;
    }
    return Tooltip(message: tooltip!, child: button);
  }
}

class AppLiquidGlassSurface extends ConsumerWidget {
  final Widget child;
  final VoidCallback? onTap;
  final EdgeInsetsGeometry padding;
  final double borderRadius;
  final Color? accentColor;
  final bool selected;
  final bool performanceMode;

  const AppLiquidGlassSurface({
    super.key,
    required this.child,
    this.onTap,
    this.padding = EdgeInsets.zero,
    this.borderRadius = 16,
    this.accentColor,
    this.selected = false,
    this.performanceMode = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    if (isFlat) {
      final accent = accentColor ?? AppColors.primary;
      return Material(
        color: selected
            ? Color.alphaBlend(
                accent.withAlpha(isLight ? 20 : 32),
                isLight ? Colors.white : AppColors.slate900,
              )
            : (isLight ? Colors.white : AppColors.slate900),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(borderRadius),
          side: BorderSide(
            color: selected
                ? accent.withAlpha(isLight ? 120 : 160)
                : (isLight ? AppColors.slate200 : AppColors.slate700),
          ),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(padding: padding, child: child),
        ),
      );
    }
    return LiquidGlassLens(
      style: appLiquidGlassStyle(
        isLight: isLight,
        borderRadius: borderRadius,
        accentColor: accentColor,
        selected: selected,
        performanceMode: performanceMode,
      ),
      child: Material(
        color: Colors.transparent,
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(borderRadius),
          child: Padding(padding: padding, child: child),
        ),
      ),
    );
  }
}

class AppLiquidGlassInput extends StatelessWidget {
  final Widget child;
  final double borderRadius;

  const AppLiquidGlassInput({
    super.key,
    required this.child,
    this.borderRadius = 14,
  });

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppLiquidGlassSurface(
      borderRadius: borderRadius,
      performanceMode: true,
      child: Theme(
        data: theme.copyWith(
          inputDecorationTheme: theme.inputDecorationTheme.copyWith(
            filled: true,
            fillColor: Colors.transparent,
            border: InputBorder.none,
            enabledBorder: InputBorder.none,
            focusedBorder: InputBorder.none,
            errorBorder: InputBorder.none,
            focusedErrorBorder: InputBorder.none,
          ),
          iconButtonTheme: IconButtonThemeData(
            style: ButtonStyle(
              backgroundColor: const WidgetStatePropertyAll(Colors.transparent),
              foregroundColor: WidgetStatePropertyAll(
                theme.colorScheme.onSurfaceVariant,
              ),
              overlayColor: WidgetStatePropertyAll(
                AppColors.primary.withAlpha(18),
              ),
              side: const WidgetStatePropertyAll(BorderSide.none),
              shape: const WidgetStatePropertyAll(CircleBorder()),
            ),
          ),
        ),
        child: child,
      ),
    );
  }
}

class AppStyleSlider extends ConsumerWidget {
  final double value;
  final ValueChanged<double>? onChanged;
  final Color activeColor;
  final Color? inactiveColor;

  const AppStyleSlider({
    super.key,
    required this.value,
    required this.onChanged,
    this.activeColor = AppColors.primary,
    this.inactiveColor,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    if (isFlat) {
      return Slider(
        value: value,
        onChanged: onChanged,
        activeColor: activeColor,
        inactiveColor: inactiveColor,
      );
    }
    return LayoutBuilder(
      builder: (context, constraints) => LiquidGlassSlider(
        value: value,
        layout: LiquidGlassSliderLayout(width: constraints.maxWidth),
        activeColor: activeColor,
        inactiveColor: inactiveColor ?? Theme.of(context).colorScheme.outlineVariant,
        pixelRatio: 0.7,
        onChanged: onChanged ?? (_) {},
      ),
    );
  }
}

class AppLiquidGlassToggle extends ConsumerWidget {
  final bool value;
  final ValueChanged<bool>? onChanged;
  final Color activeColor;

  const AppLiquidGlassToggle({
    super.key,
    required this.value,
    required this.onChanged,
    this.activeColor = AppColors.primary,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final enabled = onChanged != null;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    if (isFlat) {
      return Switch(
        value: value,
        onChanged: onChanged,
        activeTrackColor: activeColor,
      );
    }
    return Opacity(
      opacity: enabled ? 1 : 0.45,
      child: IgnorePointer(
        ignoring: !enabled,
        child: LiquidGlassToggle(
          value: value,
          onChanged: onChanged ?? (_) {},
          activeColor: activeColor,
          inactiveColor: Theme.of(context).brightness == Brightness.light
              ? const Color(0x66708090)
              : const Color(0x88506678),
          pixelRatio: 0.8,
        ),
      ),
    );
  }
}

enum AppLiquidGlassButtonVariant { primary, secondary, danger, warning }

class AppLiquidGlassButton extends ConsumerWidget {
  final String label;
  final IconData? icon;
  final VoidCallback? onPressed;
  final double? width;
  final double height;
  final bool loading;
  final AppLiquidGlassButtonVariant variant;
  final bool performanceMode;

  const AppLiquidGlassButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.width,
    this.height = 48,
    this.loading = false,
    this.variant = AppLiquidGlassButtonVariant.primary,
    this.performanceMode = false,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final isFlat = ref.watch(
      appStyleProvider.select(
        (settings) => settings.visualStyle == AppVisualStyle.pureFlat,
      ),
    );
    final color = switch (variant) {
      AppLiquidGlassButtonVariant.primary => AppColors.primary,
      AppLiquidGlassButtonVariant.secondary =>
        isLight ? AppColors.slate600 : AppColors.slate300,
      AppLiquidGlassButtonVariant.danger => AppColors.red500,
      AppLiquidGlassButtonVariant.warning => AppColors.amber500,
    };
    if (isFlat) {
      final foreground = onPressed == null ? AppColors.slate400 : color;
      return SizedBox(
        width: width,
        height: height,
        child: OutlinedButton.icon(
          onPressed: loading ? null : onPressed,
          style: OutlinedButton.styleFrom(
            foregroundColor: foreground,
            backgroundColor: isLight ? Colors.white : AppColors.slate900,
            side: BorderSide(color: foreground.withAlpha(110)),
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(height / 2),
            ),
          ),
          icon: loading
              ? SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: color,
                  ),
                )
              : icon == null
              ? const SizedBox.shrink()
              : Icon(icon, size: 18),
          label: Text(label),
        ),
      );
    }
    if (loading) {
      return SizedBox(
        width: width,
        height: height,
        child: AppLiquidGlassSurface(
          borderRadius: height / 2,
          accentColor: color,
          selected: true,
          performanceMode: performanceMode,
          child: Center(
            child: SizedBox(
              width: 18,
              height: 18,
              child: CircularProgressIndicator(strokeWidth: 2, color: color),
            ),
          ),
        ),
      );
    }
    return LiquidGlassButton(
      label: label,
      icon: icon,
      onPressed: onPressed,
      width: width,
      height: height,
      foregroundColor: onPressed == null ? AppColors.slate400 : color,
      style: appLiquidGlassStyle(
        isLight: isLight,
        borderRadius: height / 2,
        accentColor: color,
        selected: onPressed != null,
        performanceMode: performanceMode,
      ),
    );
  }
}

class AppLiquidGlassChoiceChip extends StatelessWidget {
  final String label;
  final bool selected;
  final ValueChanged<bool>? onSelected;
  final Color accentColor;
  final bool performanceMode;

  const AppLiquidGlassChoiceChip({
    super.key,
    required this.label,
    required this.selected,
    required this.onSelected,
    this.accentColor = AppColors.primary,
    this.performanceMode = true,
  });

  @override
  Widget build(BuildContext context) {
    return AppLiquidGlassSurface(
      onTap: onSelected == null ? null : () => onSelected!(!selected),
      borderRadius: 16,
      accentColor: accentColor,
      selected: selected,
      performanceMode: performanceMode,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 12,
          fontWeight: selected ? FontWeight.w700 : FontWeight.w500,
          color: selected
              ? accentColor
              : Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

class AppLiquidGlassActionChip extends StatelessWidget {
  final String label;
  final VoidCallback? onPressed;
  final IconData? icon;
  final Color accentColor;

  const AppLiquidGlassActionChip({
    super.key,
    required this.label,
    required this.onPressed,
    this.icon,
    this.accentColor = AppColors.primary,
  });

  @override
  Widget build(BuildContext context) {
    return AppLiquidGlassSurface(
      onTap: onPressed,
      borderRadius: 16,
      accentColor: accentColor,
      selected: false,
      performanceMode: true,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 7),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 15, color: accentColor),
            const SizedBox(width: 5),
          ],
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: Theme.of(context).colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class AppLiquidGlassInputChip extends StatelessWidget {
  final String label;
  final VoidCallback? onDeleted;
  final Color accentColor;

  const AppLiquidGlassInputChip({
    super.key,
    required this.label,
    required this.onDeleted,
    this.accentColor = AppColors.primary,
  });

  @override
  Widget build(BuildContext context) {
    return AppLiquidGlassSurface(
      borderRadius: 16,
      accentColor: accentColor,
      performanceMode: true,
      padding: const EdgeInsets.fromLTRB(10, 6, 6, 6),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            label,
            style: const TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
          ),
          if (onDeleted != null) ...[
            const SizedBox(width: 4),
            GestureDetector(
              onTap: onDeleted,
              behavior: HitTestBehavior.opaque,
              child: const Padding(
                padding: EdgeInsets.all(3),
                child: Icon(Icons.close, size: 14),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class AppGlassDialogAction {
  final String label;
  final VoidCallback? onPressed;
  final AppLiquidGlassButtonVariant variant;
  final IconData? icon;
  final bool loading;

  const AppGlassDialogAction({
    required this.label,
    required this.onPressed,
    this.variant = AppLiquidGlassButtonVariant.secondary,
    this.icon,
    this.loading = false,
  });
}

class AppLiquidGlassDialogActions extends StatelessWidget {
  final List<AppGlassDialogAction> actions;
  final double height;

  const AppLiquidGlassDialogActions({
    super.key,
    required this.actions,
    this.height = 44,
  }) : assert(actions.length > 0 && actions.length <= 3);

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final vertical = constraints.maxWidth < 280 && actions.length > 1;
        final buttons = actions
            .map(
              (action) => AppLiquidGlassButton(
                label: action.label,
                icon: action.icon,
                onPressed: action.onPressed,
                height: height,
                loading: action.loading,
                variant: action.variant,
                performanceMode: true,
              ),
            )
            .toList();
        if (vertical) {
          return Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (var index = 0; index < buttons.length; index++) ...[
                SizedBox(width: double.infinity, child: buttons[index]),
                if (index < buttons.length - 1) const SizedBox(height: 8),
              ],
            ],
          );
        }
        return Row(
          children: [
            for (var index = 0; index < buttons.length; index++) ...[
              Expanded(child: buttons[index]),
              if (index < buttons.length - 1) const SizedBox(width: 10),
            ],
          ],
        );
      },
    );
  }
}

enum AppGlassNoticeType { info, success, warning, error }

class AppGlassNotice {
  static void show(
    BuildContext context,
    String message, {
    AppGlassNoticeType type = AppGlassNoticeType.info,
    Duration duration = const Duration(seconds: 3),
  }) {
    final messenger = ScaffoldMessenger.of(context);
    final color = switch (type) {
      AppGlassNoticeType.info => AppColors.blue500,
      AppGlassNoticeType.success => AppColors.primary,
      AppGlassNoticeType.warning => AppColors.amber500,
      AppGlassNoticeType.error => AppColors.red500,
    };
    final icon = switch (type) {
      AppGlassNoticeType.info => Icons.info_outline,
      AppGlassNoticeType.success => Icons.check_circle_outline,
      AppGlassNoticeType.warning => Icons.warning_amber_rounded,
      AppGlassNoticeType.error => Icons.error_outline,
    };

    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          duration: duration,
          content: Row(
            children: [
              Icon(icon, size: 19, color: color),
              const SizedBox(width: 10),
              Expanded(child: Text(message)),
            ],
          ),
        ),
      );
  }
}

LiquidGlassStyle appLiquidGlassStyle({
  required bool isLight,
  double borderRadius = 16,
  Color? accentColor,
  bool selected = false,
  bool performanceMode = false,
}) {
  final accent = accentColor ?? AppColors.primary;
  final tint = selected
      ? Color.alphaBlend(
          accent.withAlpha(isLight ? 18 : 28),
          isLight ? const Color(0x32FFFFFF) : const Color(0x66111C2D),
        )
      : (isLight ? const Color(0x2EFFFFFF) : const Color(0x5C111C2D));
  return LiquidGlassStyle(
    shape: LiquidGlassShape.roundedRectangle(
      cornerRadius: borderRadius,
      borderWidth: selected ? 1.2 : 1,
      borderColor: selected
          ? accent.withAlpha(isLight ? 110 : 145)
          : (isLight ? const Color(0x70FFFFFF) : AppColors.darkBorder),
      lightIntensity: performanceMode ? 0.65 : 1.05,
      lightDirection: 80,
      borderType: OpticalBorder(
        borderSaturation: selected ? 1.35 : 1.05,
        ambientIntensity: performanceMode ? 0.65 : 0.9,
        borderSolidity: selected ? 0.35 : 0.2,
      ),
    ),
    appearance: LiquidGlassAppearance(
      color: tint,
      blur: LiquidGlassBlur(
        sigmaX: performanceMode ? 1.5 : 3,
        sigmaY: performanceMode ? 1.5 : 3,
      ),
      saturation: isLight ? 1.02 : 1.08,
    ),
    refraction: LiquidGlassRefraction(
      distortion: performanceMode ? 0.025 : 0.065,
      distortionWidth: performanceMode ? 10 : 24,
      chromaticAberration: performanceMode ? 0 : 0.001,
    ),
  );
}

Color glassCardColor({
  required bool isLight,
  Color? lightColor,
  Color? darkColor,
}) {
  return isLight
      ? (lightColor ?? AppColors.lightSurface)
      : (darkColor ?? AppColors.darkSurface);
}

Color glassFillColor({required bool isLight}) {
  return isLight ? AppColors.lightSurfaceMuted : AppColors.darkSurfaceMuted;
}
