import 'package:flutter/material.dart';
import 'package:flutter_miuix/miuix.dart';

/// MIUIX 风格对话框容器。
///
/// 作为 Material [AlertDialog] 的替代品，保留 `title` / `content` / `actions`
/// 参数形态，在 MIUIX 主题下渲染圆角 32 的 HyperOS 卡片。
class AppDialog extends StatelessWidget {
  final Widget? title;
  final Widget? content;
  final List<Widget>? actions;
  final EdgeInsetsGeometry? contentPadding;
  final EdgeInsetsGeometry? titlePadding;
  final EdgeInsetsGeometry? actionsPadding;
  final EdgeInsetsGeometry? insetPadding;
  final double maxWidth;

  const AppDialog({
    super.key,
    this.title,
    this.content,
    this.actions,
    this.contentPadding,
    this.titlePadding,
    this.actionsPadding,
    this.insetPadding,
    this.maxWidth = 420,
  });

  @override
  Widget build(BuildContext context) {
    final miuix = MiuixTheme.of(context);
    final colors = miuix.colors;
    final textStyles = miuix.textStyles;

    final children = <Widget>[
      if (title != null)
        Padding(
          padding: titlePadding ?? const EdgeInsets.fromLTRB(24, 24, 24, 0),
          child: DefaultTextStyle.merge(
            style: textStyles.title3.copyWith(
              color: colors.onBackground,
              fontWeight: FontWeight.w600,
            ),
            child: title!,
          ),
        ),
      if (content != null)
        Padding(
          padding:
              contentPadding ?? const EdgeInsets.fromLTRB(24, 16, 24, 0),
          child: DefaultTextStyle.merge(
            style: textStyles.body1.copyWith(
              color: colors.onSurfaceSecondary,
            ),
            child: content!,
          ),
        ),
      if (actions != null && actions!.isNotEmpty)
        Padding(
          padding: actionsPadding ?? const EdgeInsets.fromLTRB(24, 24, 24, 24),
          child: actions!.length == 1
              ? actions!.first
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    for (var index = 0; index < actions!.length; index++) ...[
                      actions![index],
                      if (index < actions!.length - 1)
                        const SizedBox(height: 10),
                    ],
                  ],
                ),
        ),
    ];

    return Center(
      child: Padding(
        padding: insetPadding ?? const EdgeInsets.symmetric(horizontal: 24),
        child: ConstrainedBox(
          constraints: BoxConstraints(maxWidth: maxWidth),
          child: Material(
            color: colors.background,
            elevation: 0,
            clipBehavior: Clip.antiAlias,
            shape: RoundedRectangleBorder(
              borderRadius: BorderRadius.circular(32),
              side: BorderSide(color: colors.dividerLine),
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: children,
            ),
          ),
        ),
      ),
    );
  }
}

/// 以 MIUIX 过渡与遮罩显示 [AppDialog]。
///
/// 参数与 Material `showDialog` 对齐，返回 `Future<T?>`。
Future<T?> showAppDialog<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  bool barrierDismissible = true,
  Color? barrierColor,
  String? barrierLabel,
  bool useSafeArea = true,
  bool useRootNavigator = true,
  RouteSettings? routeSettings,
}) {
  final label =
      barrierLabel ?? MaterialLocalizations.of(context).modalBarrierDismissLabel;
  final dimColor = MiuixTheme.of(context).colors.windowDimming;
  return showGeneralDialog<T>(
    context: context,
    barrierDismissible: barrierDismissible,
    barrierLabel: label,
    barrierColor: barrierColor ?? dimColor,
    transitionDuration: const Duration(milliseconds: 220),
    useRootNavigator: useRootNavigator,
    routeSettings: routeSettings,
    pageBuilder: (dialogContext, _, _) {
      final child = builder(dialogContext);
      return useSafeArea ? SafeArea(child: child) : child;
    },
    transitionBuilder: (_, animation, _, child) {
      final curved = CurvedAnimation(
        parent: animation,
        curve: Curves.easeOutCubic,
        reverseCurve: Curves.easeInCubic,
      );
      return FadeTransition(
        opacity: curved,
        child: ScaleTransition(
          scale: Tween<double>(begin: 0.95, end: 1).animate(curved),
          child: child,
        ),
      );
    },
  );
}

/// 以 MIUIX 背景、圆角与拖动手柄显示底部抽屉。
///
/// 参数与 Material `showModalBottomSheet` 对齐，返回 `Future<T?>`。
Future<T?> showAppBottomSheet<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  bool isScrollControlled = false,
  bool showDragHandle = true,
  bool useSafeArea = false,
  bool isDismissible = true,
  bool enableDrag = true,
  bool useRootNavigator = false,
  Color? backgroundColor,
  Color? barrierColor,
  ShapeBorder? shape,
  BoxConstraints? constraints,
  double? elevation,
  Clip? clipBehavior,
}) {
  final colors = MiuixTheme.of(context).colors;
  return showModalBottomSheet<T>(
    context: context,
    isScrollControlled: isScrollControlled,
    showDragHandle: showDragHandle,
    useSafeArea: useSafeArea,
    isDismissible: isDismissible,
    enableDrag: enableDrag,
    useRootNavigator: useRootNavigator,
    backgroundColor: backgroundColor ?? colors.background,
    barrierColor: barrierColor ?? colors.windowDimming,
    elevation: elevation ?? 0,
    clipBehavior: clipBehavior,
    constraints: constraints,
    shape: shape ??
        const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(28)),
        ),
    builder: builder,
  );
}
