import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../shared/models/user.dart';
import '../storage/secure_storage.dart';
import '../network/panel_capability_registry.dart';
import 'auth_service.dart';
import 'auth_token_snapshot.dart';
import 'auth_session_epoch.dart';
import 'auth_session_invalidation_coordinator.dart';

enum AuthStatus { unknown, unauthenticated, authenticated }

const Object _authFieldUnset = Object();

class AuthState {
  final AuthStatus status;
  final User? user;
  final bool needsInit;
  final String? error;

  const AuthState({
    this.status = AuthStatus.unknown,
    this.user,
    this.needsInit = false,
    this.error,
  });

  AuthState copyWith({
    AuthStatus? status,
    Object? user = _authFieldUnset,
    bool? needsInit,
    Object? error = _authFieldUnset,
  }) {
    return AuthState(
      status: status ?? this.status,
      user: identical(user, _authFieldUnset) ? this.user : user as User?,
      needsInit: needsInit ?? this.needsInit,
      error: identical(error, _authFieldUnset) ? this.error : error as String?,
    );
  }
}

class AuthNotifier extends StateNotifier<AuthState> {
  final AuthService _authService;

  AuthNotifier(this._authService) : super(const AuthState());

  void clearError() {
    state = state.copyWith(error: null);
  }

  String errorMessageFor(Object error) => _extractErrorMessage(error);

  Future<void> restoreTrustedLocalSession() async {
    final epoch = AuthSessionEpoch.current;
    // 启动时先恢复本地可信登录态，避免每次打开 APP 都重新打登录日志。
    final token = await SecureStorage.getAccessToken();
    final serverUrl = await SecureStorage.getServerUrl();
    if (token == null ||
        token.isEmpty ||
        serverUrl == null ||
        serverUrl.isEmpty) {
      if (!AuthSessionEpoch.isCurrent(epoch)) return;
      AuthTokenSnapshot.clear();
      state = const AuthState(status: AuthStatus.unauthenticated);
      return;
    }

    final trusted = await SecureStorage.hasValidTrustedLogin(
      serverUrl: serverUrl,
    );
    if (!trusted) {
      if (!AuthSessionEpoch.isCurrent(epoch)) return;
      AuthTokenSnapshot.clear();
      state = const AuthState(status: AuthStatus.unauthenticated);
      return;
    }

    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    AuthTokenSnapshot.setAccessToken(token);
    final user = await SecureStorage.getUser();
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    state = state.copyWith(
      status: AuthStatus.authenticated,
      user: user,
      error: null,
    );
  }

  Future<void> restoreSession() async {
    final epoch = AuthSessionEpoch.current;
    final token = await SecureStorage.getAccessToken();
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    AuthTokenSnapshot.setAccessToken(token);
    if (token == null || token.isEmpty) {
      state = const AuthState(status: AuthStatus.unauthenticated);
      return;
    }
    state = state.copyWith(status: AuthStatus.unknown, error: null);
  }

  Future<void> checkAuthStatus({bool verifyRemote = true}) async {
    final previousState = state;
    await restoreSession();
    if (!verifyRemote) {
      return;
    }

    final epoch = AuthSessionEpoch.current;
    final token = await SecureStorage.getAccessToken();
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    if (token == null || token.isEmpty) {
      state = const AuthState(status: AuthStatus.unauthenticated);
      return;
    }

    try {
      final user = await _authService.getUser(authEpoch: epoch);
      if (!AuthSessionEpoch.isCurrent(epoch)) return;
      state = state.copyWith(
        status: AuthStatus.authenticated,
        user: user,
        error: null,
      );

      final serverUrl = await SecureStorage.getServerUrl();
      if (serverUrl != null && serverUrl.isNotEmpty) {
        await SecureStorage.saveTrustedLoginSession(
          serverUrl: serverUrl,
          expiresAt: DateTime.now().toUtc().add(const Duration(days: 7)),
          authEpoch: epoch,
        );
      }
    } catch (error) {
      if (!AuthSessionEpoch.isCurrent(epoch)) return;
      if (isProtectedRequestAuthFailure(error)) {
        final invalidatedEpoch = await AuthSessionInvalidationCoordinator.instance
            .invalidate(epoch);
        if (invalidatedEpoch == null ||
            !AuthSessionEpoch.isCurrent(invalidatedEpoch)) {
          return;
        }
        state = const AuthState(
          status: AuthStatus.unauthenticated,
          error: '登录状态已失效，请重新登录',
        );
        return;
      }
      state = state.copyWith(
        status: previousState.status == AuthStatus.authenticated
            ? AuthStatus.authenticated
            : AuthStatus.unknown,
        user: previousState.user,
        error: '暂时无法验证登录状态，请检查网络连接',
      );
    }
  }

  Future<void> checkInit({bool rethrowErrors = false}) async {
    final epoch = AuthSessionEpoch.current;
    final scope = PanelCapabilityRegistry.currentScope;
    try {
      final needsInit = await _authService.needsInitialization();
      if (!AuthSessionEpoch.isCurrent(epoch) ||
          scope != PanelCapabilityRegistry.currentScope) {
        return;
      }
      state = state.copyWith(needsInit: needsInit);
    } catch (_) {
      if (!AuthSessionEpoch.isCurrent(epoch) ||
          scope != PanelCapabilityRegistry.currentScope) {
        return;
      }
      // 出错时默认不需要初始化，直接显示登录
      state = state.copyWith(needsInit: false);
      if (rethrowErrors) rethrow;
    }
  }

  Future<Map<String, dynamic>> login({
    required String username,
    required String password,
    String? totpCode,
    Map<String, dynamic>? captcha,
  }) async {
    final epoch = AuthSessionEpoch.advance();
    AuthTokenSnapshot.clear();
    state = const AuthState(status: AuthStatus.unauthenticated);
    await SecureStorage.clearAuthSession(authEpoch: epoch);
    if (!AuthSessionEpoch.isCurrent(epoch)) {
      return const <String, dynamic>{};
    }
    try {
      final result = await _authService.login(
        username: username,
        password: password,
        totpCode: totpCode,
        captcha: captcha,
        authEpoch: epoch,
      );
      if (!AuthSessionEpoch.isCurrent(epoch)) return result;

      if (result.containsKey('access_token')) {
        late final User user;
        if (result.containsKey('user') && result['user'] != null) {
          user = User.fromJson(
            Map<String, dynamic>.from(result['user'] as Map),
          );
          await SecureStorage.saveUser(user, authEpoch: epoch);
          if (!AuthSessionEpoch.isCurrent(epoch)) return result;
        } else {
          user = await _authService.getUser(authEpoch: epoch);
          if (!AuthSessionEpoch.isCurrent(epoch)) return result;
        }

        final serverUrl = await SecureStorage.getServerUrl();
        if (serverUrl != null && serverUrl.isNotEmpty) {
          await SecureStorage.saveTrustedLoginSession(
            serverUrl: serverUrl,
            expiresAt: DateTime.now().toUtc().add(const Duration(days: 7)),
            authEpoch: epoch,
          );
        }
        if (!AuthSessionEpoch.isCurrent(epoch)) return result;
        state = state.copyWith(
          status: AuthStatus.authenticated,
          user: user,
          error: null,
        );
      }
      return result;
    } catch (e) {
      if (!AuthSessionEpoch.isCurrent(epoch)) rethrow;
      await SecureStorage.clearAuthSession(authEpoch: epoch);
      if (!AuthSessionEpoch.isCurrent(epoch)) rethrow;
      final msg = _extractErrorMessage(e);
      state = AuthState(status: AuthStatus.unauthenticated, error: msg);
      rethrow;
    }
  }

  Future<void> initAdmin(String username, String password) async {
    await _authService.initAdmin(username, password);
    state = state.copyWith(needsInit: false);
  }

  Future<Map<String, dynamic>> captchaConfig({String? username}) {
    return _authService.captchaConfig(username: username);
  }

  Future<void> logout() async {
    final requestEpoch = AuthSessionEpoch.current;
    try {
      await _authService.logout(authEpoch: requestEpoch);
    } finally {
      if (AuthSessionEpoch.isCurrent(requestEpoch)) {
        AuthSessionEpoch.advance();
        AuthTokenSnapshot.clear();
        state = const AuthState(status: AuthStatus.unauthenticated);
      }
    }
  }

  Future<void> refreshUser() async {
    final epoch = AuthSessionEpoch.current;
    try {
      final user = await _authService.getUser(authEpoch: epoch);
      if (!AuthSessionEpoch.isCurrent(epoch)) return;
      state = state.copyWith(user: user);
    } catch (_) {}
  }

  Future<void> changeUsername(String username) async {
    final requestEpoch = AuthSessionEpoch.current;
    await _authService.changeUsername(username, authEpoch: requestEpoch);
    if (!AuthSessionEpoch.isCurrent(requestEpoch)) return;
    AuthSessionEpoch.advance();
    state = const AuthState(status: AuthStatus.unauthenticated);
  }

  Future<void> changePassword(String oldPassword, String newPassword) async {
    final requestEpoch = AuthSessionEpoch.current;
    await _authService.changePassword(
      oldPassword,
      newPassword,
      authEpoch: requestEpoch,
    );
    if (!AuthSessionEpoch.isCurrent(requestEpoch)) return;
    AuthSessionEpoch.advance();
    state = const AuthState(status: AuthStatus.unauthenticated);
  }

  Future<void> uploadAvatar(MultipartFile avatar) async {
    final epoch = AuthSessionEpoch.current;
    await _authService.uploadAvatar(avatar);
    final user = await _authService.getUser(authEpoch: epoch);
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    state = state.copyWith(user: user);
  }

  Future<void> deleteAvatar() async {
    final epoch = AuthSessionEpoch.current;
    await _authService.deleteAvatar();
    final user = await _authService.getUser(authEpoch: epoch);
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    state = state.copyWith(user: user);
  }

  void setUnauthenticated({int? authEpoch}) {
    if (authEpoch != null && !AuthSessionEpoch.isCurrent(authEpoch)) return;
    state = const AuthState(status: AuthStatus.unauthenticated);
  }

  Future<void> expireSession({required int expectedEpoch}) async {
    if (!AuthSessionEpoch.isCurrent(expectedEpoch)) return;
    final epoch = AuthSessionEpoch.advance();
    await SecureStorage.clearAuthSession(authEpoch: epoch);
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    state = const AuthState(status: AuthStatus.unauthenticated);
  }

  String _extractErrorMessage(dynamic e) {
    if (e is DioException) {
      final data = e.response?.data;
      var backendMessage = '';
      if (data is Map) {
        backendMessage =
            (data['error'] ?? data['message'])?.toString().trim() ?? '';
      }

      // NAS / Nginx Proxy Manager 反代旧面板时，登录接口可能因为 CORS 来源端口不一致返回 403。
      if (e.response?.statusCode == 403) {
        final extra = backendMessage.isEmpty ? '' : '\n后端提示：$backendMessage';
        return '登录请求被面板拒绝（403）。如果你是在群晖、飞牛等 NAS 中使用 Nginx Proxy Manager 或公网域名反代访问，请优先升级面板到 v2.3.0 及以上；升级前可临时在 config.yaml 的 cors.origins 中加入完整公网地址，例如 https://域名:端口。$extra';
      }

      // 登录接口返回 4xx 时优先显示后端明确原因
      if (backendMessage.isNotEmpty) {
        return backendMessage;
      }

      final statusCode = e.response?.statusCode;
      if (statusCode != null && statusCode >= 400) {
        return '请求失败 (HTTP $statusCode)，请检查服务器配置';
      }

      final detail = '${e.message ?? ''} ${e.error ?? ''}'.toLowerCase();
      if (detail.contains('certificate') ||
          detail.contains('hostname mismatch')) {
        return 'HTTPS 证书校验失败，请检查证书有效期、域名匹配和证书链';
      }
      if (detail.contains('handshake')) {
        return 'TLS 握手失败，请检查协议、证书和反向代理 TLS 配置';
      }
      if (e.type == DioExceptionType.connectionTimeout ||
          e.type == DioExceptionType.sendTimeout ||
          e.type == DioExceptionType.receiveTimeout) {
        return '登录请求超时，请检查 HTTPS 反向代理和面板服务状态';
      }
      if (detail.contains('failed host lookup') ||
          detail.contains('name or service not known')) {
        return '域名解析失败，请检查域名和 DNS 配置';
      }
      if (e.type == DioExceptionType.connectionError) {
        return '无法连接服务器，请检查域名、端口和反向代理配置';
      }
    }

    if (e is Exception) {
      try {
        final dioError = e as dynamic;
        if (dioError.response?.data != null) {
          return dioError.response.data['message']?.toString() ?? '操作失败';
        }
      } catch (_) {}
    }
    return '网络错误，请检查连接';
  }
}

final authServiceProvider = Provider<AuthService>((ref) => AuthService());

final authProvider = StateNotifierProvider<AuthNotifier, AuthState>((ref) {
  return AuthNotifier(ref.read(authServiceProvider));
});
