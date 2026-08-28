import 'package:dio/dio.dart';
import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../network/panel_capability_registry.dart';
import '../storage/secure_storage.dart';
import 'auth_token_snapshot.dart';
import 'auth_session_epoch.dart';
import 'auth_session_invalidation_coordinator.dart';
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

  final AuthSessionInvalidationCoordinator _sessionInvalidation;
  AuthInterceptor({
    void Function()? onAuthFailed,
    Future<void> Function(int epoch)? onAuthFailedForEpoch,
    AuthSessionInvalidationCoordinator? sessionInvalidation,
  }) : _sessionInvalidation =
           sessionInvalidation ??
           ((onAuthFailed != null || onAuthFailedForEpoch != null)
               ? AuthSessionInvalidationCoordinator(
                   onInvalidated: (epoch) async {
                     await onAuthFailedForEpoch?.call(epoch);
                     onAuthFailed?.call();
                   },
                 )
               : AuthSessionInvalidationCoordinator.instance);

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
      } catch (error) {
        if (isRefreshAuthFailure(error)) {
          await _clearSessionAndRejectPending(epoch: epoch);
        } else {
          err.requestOptions.extra['auth_refresh_transient_failure'] = true;
        }
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
    } catch (error) {
      if (isProtectedRequestAuthFailure(error)) {
        await _clearSessionAndRejectPending(epoch: epoch);
      }
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
    await _sessionInvalidation.invalidate(epoch);
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
