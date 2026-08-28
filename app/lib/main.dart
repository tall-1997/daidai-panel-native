import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'app.dart';
import 'core/auth/auth_interceptor.dart';
import 'core/auth/auth_provider.dart';
import 'core/auth/auth_session_invalidation_coordinator.dart';
import 'core/network/app_user_agent.dart';
import 'core/network/dio_client.dart';
import 'core/services/local_notification_service.dart';
import 'core/services/app_update_service.dart';
import 'core/services/task_completion_observer.dart';
import 'core/storage/secure_storage.dart';
import 'core/router/app_router.dart';
import 'core/local_panel/managed_local_connection_monitor.dart';
import 'features/app_lock/providers/app_lock_provider.dart';

/// 全局 WidgetsBindingObserver 回调 - 处理后台返回时的应用锁和通知
@pragma('vm:entry-point')
void _appLifecycleObserver() {
  WidgetsBinding.instance.addObserver(_AppLifecycleHandler.instance);
}

class _AppLifecycleHandler extends WidgetsBindingObserver {
  _AppLifecycleHandler._();
  static final _AppLifecycleHandler instance = _AppLifecycleHandler._();

  DateTime? _pausedAt;
  ProviderContainer? _container;

  void attachContainer(ProviderContainer container) {
    _container = container;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.paused) {
      _pausedAt = DateTime.now();
      if (Platform.isAndroid) {
        ManagedLocalConnectionMonitor.instance.handleAppPaused();
      }
    } else if (state == AppLifecycleState.resumed) {
      if (Platform.isAndroid) {
        unawaited(ManagedLocalConnectionMonitor.instance.handleAppResumed());
      }
      AppUpdateService.clearInstallerCacheAfterReturn();
      if (_pausedAt != null) {
        final elapsed = DateTime.now().difference(_pausedAt!);
        if (elapsed >= const Duration(seconds: 1)) {
          // 应用从后台返回：如已启用应用锁则锁定
          _triggerAppLock();
        }
        _pausedAt = null;
      }
    }
  }

  void _triggerAppLock() {
    try {
      final container = _container;
      if (container == null) return;
      container.read(appLockProvider.notifier).lockIfEnabled();
    } catch (error, stackTrace) {
      debugPrint('Failed to trigger app lock: $error');
      debugPrintStack(stackTrace: stackTrace);
    }
  }
}

void main() async {
  WidgetsFlutterBinding.ensureInitialized();
  await AppUserAgent.initialize();
  await AppUpdateService.clearStaleUpdateCache();

  try {
    await LocalNotificationService().initialize();
  } catch (_) {}

  final container = ProviderContainer();
  _AppLifecycleHandler.instance.attachContainer(container);

  // 添加生命周期观察者用于后台通知和应用锁
  _appLifecycleObserver();

  // 恢复服务器地址
  final serverUrl = await SecureStorage.getServerUrl();
  if (serverUrl != null && serverUrl.isNotEmpty) {
    DioClient.instance.setBaseUrl(serverUrl);
  }
  // 注入认证拦截器
  AuthSessionInvalidationCoordinator.instance.onInvalidated = (epoch) async {
    container.read(authProvider.notifier).setUnauthenticated(authEpoch: epoch);
  };
  DioClient.instance.dio.interceptors.insert(
    1,
    AuthInterceptor(),
  );

  // 启动时先恢复本地可信登录态，7 天内避免重复触发登录接口和登录日志。
  await container.read(authProvider.notifier).restoreTrustedLocalSession();
  container.listen<AuthState>(authProvider, (_, next) {
    if (next.status == AuthStatus.authenticated) {
      TaskCompletionObserver.instance.start();
    } else {
      TaskCompletionObserver.instance.stop();
    }
  }, fireImmediately: true);

  runApp(
    UncontrolledProviderScope(
      container: container,
      child: const DaidaiApp(),
    ),
  );
  WidgetsBinding.instance.addPostFrameCallback((_) {
    LocalNotificationService().setNavigationHandler((route) {
      container.read(routerProvider).push(route);
    });
  });
}
