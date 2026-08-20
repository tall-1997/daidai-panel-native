import 'package:dio/dio.dart';
import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../network/panel_capability_registry.dart';
import '../storage/secure_storage.dart';
import 'auth_token_snapshot.dart';
import 'auth_session_epoch.dart';
import 'token_refresh_coordinator.dart';

class AuthInterceptor extends Interceptor {
  static const _retryMarker = 'auth_retry_attempted';
  static const _requestScopeMarker = 'auth_request_scope';
  static const _sessionEpochMarker = 'auth_session_epoch';
  static const _publicAuthPaths = {
    ApiEndpoints.checkInit,
    ApiEndpoints.init,
    ApiEndpoints.login,
    ApiEndpoints.captchaConfig,
  };

  final Map<int, Future<void>> _authFailureInFlight = {};
  final Set<int> _failedAuthEpochs = {};
  final void Function()? onAuthFailed;
  final Future<void> Function(int epoch)? onAuthFailedForEpoch;

  AuthInterceptor({this.onAuthFailed, this.onAuthFailedForEpoch});

  static bool isPublicAuthPath(String path) {
    final normalizedPath = Uri.tryParse(path)?.path ?? path;
    return _publicAuthPaths.contains(normalizedPath);
  }

  @override
  void onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) {
    options.extra.putIfAbsent(
      _requestScopeMarker,
      () => PanelCapabilityRegistry.currentScope,
    );
    options.extra.putIfAbsent(
      _sessionEpochMarker,
      () => AuthSessionEpoch.current,
    );
    if (isPublicAuthPath(options.path)) {
      options.headers.removeWhere(
        (name, _) => name.toLowerCase() == 'authorization',
      );
      handler.next(options);
      return;
    }
    final token = AuthTokenSnapshot.accessToken;
    if (token != null && token.isNotEmpty) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    handler.next(options);
  }

  @override
  void onResponse(Response response, ResponseInterceptorHandler handler) {
    final options = response.requestOptions;
    if (options.extra[_requestScopeMarker] !=
            PanelCapabilityRegistry.currentScope ||
        !_isRequestEpochCurrent(options)) {
      handler.reject(
        DioException(
          requestOptions: options,
          response: response,
          type: DioExceptionType.cancel,
          error: 'Auth session changed while request was in flight',
        ),
      );
      return;
    }
    handler.next(response);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) async {
    if (err.requestOptions.extra[_requestScopeMarker] !=
            PanelCapabilityRegistry.currentScope ||
        !_isRequestEpochCurrent(err.requestOptions)) {
      handler.reject(
        DioException(
          requestOptions: err.requestOptions,
          response: err.response,
          type: DioExceptionType.cancel,
          error: 'Auth session changed while request was in flight',
        ),
      );
      return;
    }
    if (err.response?.statusCode != 401 ||
        isPublicAuthPath(err.requestOptions.path)) {
      handler.next(err);
      return;
    }

    if (err.requestOptions.extra[_retryMarker] == true) {
      await _clearSessionAndRejectPending(
        epoch: _requestEpoch(err.requestOptions),
      );
      handler.next(err);
      return;
    }

    final epoch = _requestEpoch(err.requestOptions);
    var currentRequestCompleted = false;

    try {
      final refreshToken = await SecureStorage.getRefreshToken();
      if (!AuthSessionEpoch.isCurrent(epoch)) {
        handler.reject(_sessionChangedException(err));
        currentRequestCompleted = true;
        return;
      }
      if (refreshToken == null || refreshToken.isEmpty) {
        await _clearSessionAndRejectPending(epoch: epoch);
        handler.next(err);
        currentRequestCompleted = true;
        return;
      }

      late final String newAccessToken;
      try {
        newAccessToken = await TokenRefreshCoordinator.refresh(epoch: epoch);
      } catch (_) {
        await _clearSessionAndRejectPending(epoch: epoch);
        handler.next(err);
        currentRequestCompleted = true;
        return;
      }

      if (err.requestOptions.extra[_requestScopeMarker] !=
              PanelCapabilityRegistry.currentScope ||
          !AuthSessionEpoch.isCurrent(epoch)) {
        handler.reject(_sessionChangedException(err));
        currentRequestCompleted = true;
        return;
      }

      err.requestOptions.extra[_retryMarker] = true;
      err.requestOptions.headers['Authorization'] = 'Bearer $newAccessToken';
      try {
        final retryResponse = await DioClient.instance.dio.fetch(
          err.requestOptions,
        );
        handler.resolve(retryResponse);
        currentRequestCompleted = true;
      } catch (retryError) {
        handler.reject(_retryException(err.requestOptions, retryError));
        currentRequestCompleted = true;
      }
    } catch (_) {
      await _clearSessionAndRejectPending(epoch: epoch);
      if (!currentRequestCompleted) {
        handler.next(err);
      }
    }
  }

  DioException _sessionChangedException(DioException original) => DioException(
    requestOptions: original.requestOptions,
    response: original.response,
    type: DioExceptionType.cancel,
    error: 'Auth session changed while request was in flight',
  );

  Future<void> _clearSessionAndRejectPending({
    required int epoch,
  }) async {
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    final existing = _authFailureInFlight[epoch];
    if (existing != null) {
      await existing;
      return;
    }
    if (!_failedAuthEpochs.add(epoch)) return;
    _failedAuthEpochs.removeWhere((failedEpoch) => failedEpoch < epoch - 2);
    final operation = _clearSessionAndRejectPendingOnce(epoch);
    _authFailureInFlight[epoch] = operation;
    try {
      await operation;
    } finally {
      if (identical(_authFailureInFlight[epoch], operation)) {
        _authFailureInFlight.remove(epoch);
      }
    }
  }

  Future<void> _clearSessionAndRejectPendingOnce(int epoch) async {
    try {
      await SecureStorage.clearAuthSession(authEpoch: epoch);
    } catch (_) {}
    if (!AuthSessionEpoch.isCurrent(epoch)) return;
    try {
      await onAuthFailedForEpoch?.call(epoch);
      onAuthFailed?.call();
    } catch (_) {}
  }

  int _requestEpoch(RequestOptions options) =>
      options.extra[_sessionEpochMarker] as int? ?? AuthSessionEpoch.current;

  bool _isRequestEpochCurrent(RequestOptions options) =>
      AuthSessionEpoch.isCurrent(_requestEpoch(options));

  DioException _retryException(RequestOptions options, Object error) {
    if (error is DioException) return error;
    return DioException(requestOptions: options, error: error);
  }
}
