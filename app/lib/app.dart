import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'core/local_panel/managed_local_connection_monitor.dart';
import 'core/router/app_router.dart';
import 'core/theme/app_theme.dart';
import 'core/theme/theme_provider.dart';
import 'features/app_lock/widgets/app_lock_gate.dart';

class DaidaiApp extends ConsumerStatefulWidget {
  const DaidaiApp({super.key});

  @override
  ConsumerState<DaidaiApp> createState() => _DaidaiAppState();
}

class _DaidaiAppState extends ConsumerState<DaidaiApp> {
  late final ThemeData _lightTheme = AppTheme.light();
  late final ThemeData _darkTheme = AppTheme.dark();

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (Platform.isAndroid) {
        unawaited(
          ManagedLocalConnectionMonitor.instance.initializeFromStorage(),
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final router = ref.watch(routerProvider);
    final themeMode = ref.watch(
      appStyleProvider.select((settings) => settings.themeMode),
    );

    return MaterialApp.router(
      title: '呆呆面板',
      debugShowCheckedModeBanner: false,
      theme: _lightTheme,
      darkTheme: _darkTheme,
      themeMode: themeMode,
      routerConfig: router,
      locale: const Locale('zh', 'CN'),
      scrollBehavior: const MaterialScrollBehavior().copyWith(
        overscroll: false,
      ),
      builder: (context, child) =>
          AppLockGate(child: child ?? const SizedBox.shrink()),
    );
  }
}
