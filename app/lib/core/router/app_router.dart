import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../auth/auth_provider.dart';
import '../../features/login/views/app_boot_page.dart';
import '../../features/login/views/login_page.dart';
import '../../features/server_config/views/server_config_page.dart';
import '../../features/dashboard/views/dashboard_page.dart';
import '../../features/tasks/views/task_list_page.dart';
import '../../features/tasks/views/task_form_page.dart';
import '../../features/tasks/views/task_views_page.dart';
import '../../features/logs/views/log_list_page.dart';
import '../../features/logs/views/log_stream_page.dart';
import '../../features/envs/views/env_list_page.dart';
import '../../features/envs/views/env_tools_page.dart';
import '../../features/settings/views/more_page.dart';
import '../../features/settings/views/sponsor_page.dart';
import '../../features/subscriptions/views/subscription_list_page.dart';
import '../../features/scripts/views/script_list_page.dart';
import '../../features/notifications/views/notification_list_page.dart';
import '../../features/notifications/views/local_notification_settings_page.dart';
import '../../features/deps/views/dep_list_page.dart';
import '../../features/deps/views/android_runtime_page.dart';
import '../../features/deps/views/installed_packages_page.dart';
import '../../features/users/views/user_list_page.dart';
import '../../features/security/views/security_page.dart';
import '../../features/security/views/ssh_keys_page.dart';
import '../../features/security/views/platform_tokens_page.dart';
import '../../features/system/views/system_settings_page.dart';
import '../../features/system/views/panel_settings_page.dart';
import '../../features/system/views/panel_log_page.dart';
import '../../features/system/views/backup_page.dart';
import '../../features/system/views/health_check_page.dart';
import '../../features/system/views/config_script_page.dart';
import '../../features/openapi/views/open_api_page.dart';
import '../../features/app_lock/views/app_lock_settings_page.dart';
import '../../features/settings/views/theme_settings_page.dart';
import '../../features/profile/views/profile_page.dart';
import '../../shared/widgets/app_background.dart';
import '../../shared/widgets/main_scaffold.dart';
import '../../shared/models/task.dart';

final _rootNavigatorKey = GlobalKey<NavigatorState>();
final _shellNavigatorKey = GlobalKey<NavigatorState>();
String? _pendingProtectedLocation;

NoTransitionPage<void> _rootPage(Widget child) => NoTransitionPage<void>(
  child: AppBackground(child: child),
);

/// 将 auth status 变化转为 Listenable，供 GoRouter.refreshListenable 使用
class _AuthNotifierBridge extends ChangeNotifier {
  _AuthNotifierBridge(Ref ref) {
    ref.listen<AuthStatus>(
      authProvider.select((s) => s.status),
      (previous, next) => notifyListeners(),
    );
  }
}

final _authNotifierProvider = Provider<_AuthNotifierBridge>((ref) {
  return _AuthNotifierBridge(ref);
});

final routerProvider = Provider<GoRouter>((ref) {
  final refreshNotifier = ref.watch(_authNotifierProvider);

  return GoRouter(
    navigatorKey: _rootNavigatorKey,
    initialLocation: '/boot',
    refreshListenable: refreshNotifier,
    redirect: (context, state) {
      final authState = ref.read(authProvider);
      final isAuth = authState.status == AuthStatus.authenticated;
      final isUnknown = authState.status == AuthStatus.unknown;
      final isBootRoute = state.matchedLocation == '/boot';
      final isLoginRoute = state.matchedLocation == '/login';
      final isServerConfig = state.matchedLocation == '/server-config';
      final manualServerConfig = state.uri.queryParameters['manual'] == '1';
      final manageServerConfig = state.uri.queryParameters['manage'] == '1';

      if (isBootRoute) {
        return null;
      }
      if (isUnknown) {
        if (!isLoginRoute && !isServerConfig) {
          _pendingProtectedLocation = state.uri.toString();
        }
        return '/boot';
      }
      if (isServerConfig) {
        if (isAuth && !manualServerConfig && !manageServerConfig) {
          return '/dashboard';
        }
        if (!isAuth &&
            !manualServerConfig &&
            !manageServerConfig &&
            authState.status == AuthStatus.unauthenticated) {
          return '/login';
        }
        return null;
      }
      if (!isAuth && !isLoginRoute) {
        _pendingProtectedLocation = state.uri.toString();
        return '/login';
      }
      if (isAuth && (isLoginRoute || state.matchedLocation == '/dashboard')) {
        final pending = _pendingProtectedLocation;
        if (pending != null && pending != state.uri.toString()) {
          _pendingProtectedLocation = null;
          return pending;
        }
        if (isLoginRoute) return '/dashboard';
      }
      if (isAuth) {
        final path = state.matchedLocation;
        const adminRoutes = <String>{
          '/deps','/users','/security','/ssh-keys','/system-settings',
          '/panel-settings','/panel-log','/backup','/open-api',
          '/platform-tokens',
          '/config-script',
          '/android-runtime',
          '/installed-packages',
        };
        const operatorRoutes = <String>{
          '/scripts','/subscriptions','/envs','/env-tools','/tasks/new',
          '/tasks/edit','/task-views',
        };
        final user = authState.user;
        if (adminRoutes.any((route) => path.startsWith(route)) &&
            user?.hasMinRole('admin') != true) {
          return '/more';
        }
        if (operatorRoutes.any((route) => path.startsWith(route)) &&
            user?.hasMinRole('operator') != true) {
          return '/more';
        }
      }
      return null;
    },
    routes: [
      GoRoute(
        path: '/boot',
        builder: (_, state) => const AppBootPage(),
      ),
      GoRoute(
        path: '/server-config',
        pageBuilder: (_, state) => NoTransitionPage(
          child: ServerConfigPage(
            manageMode: state.uri.queryParameters['manage'] == '1',
          ),
        ),
      ),
      GoRoute(
        path: '/login',
        builder: (_, state) => const LoginPage(),
      ),
      ShellRoute(
        navigatorKey: _shellNavigatorKey,
        builder: (_, state, child) => MainScaffold(child: child),
        routes: [
          GoRoute(
            path: '/dashboard',
            pageBuilder: (_, state) =>
                const NoTransitionPage(child: DashboardPage()),
          ),
          GoRoute(
            path: '/tasks',
            pageBuilder: (_, state) =>
                const NoTransitionPage(child: TaskListPage()),
          ),
          GoRoute(
            path: '/logs',
            pageBuilder: (_, state) =>
                const NoTransitionPage(child: LogListPage()),
          ),
          GoRoute(
            path: '/envs',
            pageBuilder: (_, state) =>
                const NoTransitionPage(child: EnvListPage()),
          ),
          GoRoute(
            path: '/more',
            pageBuilder: (_, state) =>
                const NoTransitionPage(child: MorePage()),
          ),
        ],
      ),
      GoRoute(
        path: '/task-views',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const TaskViewsPage()),
      ),
      GoRoute(
        path: '/profile',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const ProfilePage()),
      ),
      GoRoute(
        path:'/env-tools',parentNavigatorKey:_rootNavigatorKey,pageBuilder:(_,state)=>_rootPage(const EnvToolsPage()),
      ),
      GoRoute(
        path:'/installed-packages',parentNavigatorKey:_rootNavigatorKey,pageBuilder:(_,state)=>_rootPage(const InstalledPackagesPage()),
      ),
      GoRoute(
        path:'/android-runtime',parentNavigatorKey:_rootNavigatorKey,pageBuilder:(_,state)=>_rootPage(const AndroidRuntimePage()),
      ),
      GoRoute(
        path:'/config-script',parentNavigatorKey:_rootNavigatorKey,pageBuilder:(_,state)=>_rootPage(const ConfigScriptPage()),
      ),
      GoRoute(
        path: '/platform-tokens',parentNavigatorKey:_rootNavigatorKey,pageBuilder:(_,state)=>_rootPage(const PlatformTokensPage()),
      ),
      GoRoute(
        path: '/health-check',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const HealthCheckPage()),
      ),
      GoRoute(
        path: '/tasks/new',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          TaskFormPage(
            prefill: state.extra is TaskFormPrefill
                ? state.extra as TaskFormPrefill
                : null,
          ),
        ),
      ),
      GoRoute(
        path: '/tasks/edit',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) {
          final task = state.extra as Task?;
          return _rootPage(TaskFormPage(task: task));
        },
      ),
      GoRoute(
        path: '/tasks/:id/live-logs',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          TaskLiveLogPage(
            taskId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0,
            taskName: state.extra as String?,
          ),
        ),
      ),
      GoRoute(
        path: '/logs/:id/stream',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          LogStreamPage(logId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0),
        ),
      ),
      GoRoute(
        path: '/subscriptions',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const SubscriptionListPage()),
      ),
      GoRoute(
        path: '/subscriptions/:id/pull-stream',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          SubscriptionPullStreamPage(
            subscriptionId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0,
          ),
        ),
      ),
      GoRoute(
        path: '/subscriptions/:id/logs',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          SubscriptionLogsPage(
            subscriptionId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0,
            subscriptionName: state.extra as String?,
          ),
        ),
      ),
      GoRoute(
        path: '/scripts',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const ScriptListPage()),
      ),
      GoRoute(
        path: '/scripts/view',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) {
          final path = state.extra as String? ?? '';
          return _rootPage(ScriptViewPage(path: path));
        },
      ),
      GoRoute(
        path: '/notifications',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const NotificationListPage()),
      ),
      GoRoute(
        path: '/local-notifications',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          const LocalNotificationSettingsPage(),
        ),
      ),
      GoRoute(
        path: '/deps',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const DepListPage()),
      ),
      GoRoute(
        path: '/deps/:id/log-stream',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          DepLogStreamPage(depId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0),
        ),
      ),
      GoRoute(
        path: '/users',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const UserListPage()),
      ),
      GoRoute(
        path: '/security',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const SecurityPage()),
      ),
      GoRoute(
        path: '/app-lock',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const AppLockSettingsPage()),
      ),
      GoRoute(
        path: '/theme-settings',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const ThemeSettingsPage()),
      ),
      GoRoute(
        path: '/system-settings',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const SystemSettingsPage()),
      ),
      GoRoute(
        path: '/panel-settings',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const PanelSettingsPage()),
      ),
      GoRoute(
        path: '/panel-log',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const PanelLogPage()),
      ),
      GoRoute(
        path: '/backup',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const BackupPage()),
      ),
      GoRoute(
        path: '/open-api',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const OpenApiPage()),
      ),
      GoRoute(
        path: '/ssh-keys',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const SshKeysPage()),
      ),
      GoRoute(
        path: '/sponsor',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(const SponsorPage()),
      ),
      GoRoute(
        path: '/open-api/:id/logs',
        parentNavigatorKey: _rootNavigatorKey,
        pageBuilder: (_, state) => _rootPage(
          OpenApiLogsPage(
            appId: int.tryParse(state.pathParameters['id'] ?? '') ?? 0,
          ),
        ),
      ),
    ],
  );
});
