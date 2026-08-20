import 'dart:async';

import 'package:dio/dio.dart';

import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import 'auth_session_epoch.dart';

class TokenRefreshCoordinator {
  TokenRefreshCoordinator._();

  static final Map<int, Future<String>> _inFlight = {};

  static int invalidate() => AuthSessionEpoch.advance();

  static Future<String> refresh({int? epoch}) {
    final refreshEpoch = epoch ?? AuthSessionEpoch.current;
    if (!AuthSessionEpoch.isCurrent(refreshEpoch)) {
      return Future.error(
        StateError('Auth session changed during token refresh'),
      );
    }
    final existing = _inFlight[refreshEpoch];
    if (existing != null) return existing;

    late final Future<String> operation;
    operation = _refreshOnce(refreshEpoch).whenComplete(() {
      if (identical(_inFlight[refreshEpoch], operation)) {
        _inFlight.remove(refreshEpoch);
      }
    });
    _inFlight[refreshEpoch] = operation;
    return operation;
  }

  static Future<String> _refreshOnce(int epoch) async {
    _ensureCurrent(epoch);
    final baseUrl = DioClient.instance.baseUrl;
    final refreshToken = await SecureStorage.getRefreshToken();
    _ensureCurrent(epoch);
    if (refreshToken == null || refreshToken.isEmpty) {
      throw StateError('Missing refresh token');
    }

    final response = await DioClient.instance.rawDio.post(
      ApiEndpoints.refresh,
      options: Options(headers: {'Authorization': 'Bearer $refreshToken'}),
    );
    final accessToken = _readToken(response.data, 'access_token');
    final rotatedRefreshToken = _readOptionalToken(
      response.data,
      'refresh_token',
    );
    if (!AuthSessionEpoch.isCurrent(epoch) ||
        baseUrl != DioClient.instance.baseUrl) {
      throw StateError('Auth session changed during token refresh');
    }
    await SecureStorage.saveTokens(
      accessToken: accessToken,
      refreshToken: rotatedRefreshToken ?? refreshToken,
      authEpoch: epoch,
    );
    _ensureCurrent(epoch);
    return accessToken;
  }

  static void _ensureCurrent(int epoch) {
    if (!AuthSessionEpoch.isCurrent(epoch)) {
      throw StateError('Auth session changed during token refresh');
    }
  }

  static String _readToken(dynamic data, String key) {
    final token = _readOptionalToken(data, key);
    if (token == null) throw StateError('Missing $key in refresh response');
    return token;
  }

  static String? _readOptionalToken(dynamic data, String key) {
    if (data is! Map) return null;
    final direct = data[key]?.toString();
    if (direct != null && direct.isNotEmpty) return direct;
    final nested = data['data'];
    if (nested is Map) {
      final value = nested[key]?.toString();
      if (value != null && value.isNotEmpty) return value;
    }
    return null;
  }
}
