import 'package:dio/dio.dart';
import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import 'token_refresh_coordinator.dart';

class AuthInterceptor extends Interceptor {
  bool _isRefreshing = false;
  final List<({RequestOptions options, ErrorInterceptorHandler handler})>
  _pendingRequests = [];

  final void Function()? onAuthFailed;

  AuthInterceptor({this.onAuthFailed});

  @override
  void onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) async {
    final token = await SecureStorage.getAccessToken();
    if (token != null && token.isNotEmpty) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    handler.next(options);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) async {
    if (err.response?.statusCode != 401) {
      handler.next(err);
      return;
    }

    final refreshToken = await SecureStorage.getRefreshToken();
    if (refreshToken == null || refreshToken.isEmpty) {
      await SecureStorage.clearAuthSession();
      onAuthFailed?.call();
      handler.next(err);
      return;
    }

    if (_isRefreshing) {
      _pendingRequests.add((options: err.requestOptions, handler: handler));
      return;
    }

    _isRefreshing = true;

    try {
      final newAccessToken = await TokenRefreshCoordinator.refresh();

      // 重发原始请求
      err.requestOptions.headers['Authorization'] = 'Bearer $newAccessToken';
      final retryResponse = await DioClient.instance.dio.fetch(
        err.requestOptions,
      );
      handler.resolve(retryResponse);

      // 重发所有排队中的请求
      for (final pending in _pendingRequests) {
        pending.options.headers['Authorization'] = 'Bearer $newAccessToken';
        try {
          final r = await DioClient.instance.dio.fetch(pending.options);
          pending.handler.resolve(r);
        } catch (e) {
          pending.handler.reject(
            DioException(requestOptions: pending.options, error: e),
          );
        }
      }
    } catch (_) {
      await SecureStorage.clearAuthSession();
      onAuthFailed?.call();
      handler.next(err);

      for (final pending in _pendingRequests) {
        pending.handler.reject(
          DioException(
            requestOptions: pending.options,
            error: 'Token refresh failed',
          ),
        );
      }
    } finally {
      _isRefreshing = false;
      _pendingRequests.clear();
    }
  }
}
