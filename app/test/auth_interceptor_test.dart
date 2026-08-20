import 'dart:async';
import 'dart:typed_data';

import 'package:daidai_app/core/auth/auth_interceptor.dart';
import 'package:daidai_app/core/auth/auth_session_epoch.dart';
import 'package:daidai_app/core/auth/auth_token_snapshot.dart';
import 'package:daidai_app/core/auth/token_refresh_coordinator.dart';
import 'package:daidai_app/core/network/api_endpoints.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

class _UnauthorizedAdapter implements HttpClientAdapter {
  Map<String, dynamic>? receivedHeaders;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    receivedHeaders = Map<String, dynamic>.from(options.headers);
    return ResponseBody.fromString(
      '{"error":"invalid credentials"}',
      401,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}

class _SuccessAdapter implements HttpClientAdapter {
  Map<String, dynamic>? receivedHeaders;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    receivedHeaders = Map<String, dynamic>.from(options.headers);
    return ResponseBody.fromString('{}', 200);
  }

  @override
  void close({bool force = false}) {}
}

class _DelayedUnauthorizedAdapter implements HttpClientAdapter {
  final requestStarted = Completer<void>();
  final releaseResponse = Completer<void>();
  int requests = 0;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests++;
    requestStarted.complete();
    await releaseResponse.future;
    return ResponseBody.fromString('{}', 401);
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  setUp(AuthTokenSnapshot.clear);
  tearDown(AuthTokenSnapshot.clear);

  group('AuthInterceptor public authentication paths', () {
    test('recognizes login bootstrap endpoints', () {
      expect(AuthInterceptor.isPublicAuthPath(ApiEndpoints.checkInit), isTrue);
      expect(AuthInterceptor.isPublicAuthPath(ApiEndpoints.init), isTrue);
      expect(AuthInterceptor.isPublicAuthPath(ApiEndpoints.login), isTrue);
      expect(
        AuthInterceptor.isPublicAuthPath(ApiEndpoints.captchaConfig),
        isTrue,
      );
    });

    test('recognizes an absolute login URL', () {
      expect(
        AuthInterceptor.isPublicAuthPath(
          'https://panel.example.com${ApiEndpoints.login}',
        ),
        isTrue,
      );
    });

    test('keeps protected endpoints authenticated', () {
      expect(AuthInterceptor.isPublicAuthPath(ApiEndpoints.user), isFalse);
      expect(AuthInterceptor.isPublicAuthPath(ApiEndpoints.dashboard), isFalse);
    });

    test('reads protected request token from memory snapshot', () async {
      final adapter = _SuccessAdapter();
      final dio = Dio(BaseOptions(baseUrl: 'https://panel.example.com'))
        ..httpClientAdapter = adapter
        ..interceptors.add(AuthInterceptor());
      AuthTokenSnapshot.setAccessToken('memory-token');

      await dio.get(ApiEndpoints.dashboard);

      expect(adapter.receivedHeaders?['Authorization'], 'Bearer memory-token');
    });

    test('removes stale auth and forwards a login 401 directly', () async {
      var authFailedCalls = 0;
      final adapter = _UnauthorizedAdapter();
      final dio = Dio(BaseOptions(baseUrl: 'https://panel.example.com'))
        ..httpClientAdapter = adapter
        ..interceptors.add(
          AuthInterceptor(onAuthFailed: () => authFailedCalls++),
        );

      await expectLater(
        dio.post(
          ApiEndpoints.login,
          options: Options(headers: {'Authorization': 'Bearer stale-token'}),
        ),
        throwsA(
          isA<DioException>().having(
            (error) => error.response?.statusCode,
            'status code',
            401,
          ),
        ),
      );

      expect(adapter.receivedHeaders?['Authorization'], isNull);
      expect(authFailedCalls, 0);
    });

    test('does not replay or fail auth for a stale session epoch', () async {
      var authFailedCalls = 0;
      final adapter = _DelayedUnauthorizedAdapter();
      final dio = Dio(BaseOptions(baseUrl: 'https://panel.example.com'))
        ..httpClientAdapter = adapter
        ..interceptors.add(
          AuthInterceptor(onAuthFailed: () => authFailedCalls++),
        );

      final request = dio.get(ApiEndpoints.dashboard);
      await adapter.requestStarted.future;
      AuthSessionEpoch.advance();
      adapter.releaseResponse.complete();

      await expectLater(
        request,
        throwsA(
          isA<DioException>().having(
            (error) => error.type,
            'type',
            DioExceptionType.cancel,
          ),
        ),
      );
      expect(adapter.requests, 1);
      expect(authFailedCalls, 0);
    });

    test('rejects refresh immediately for a stale session epoch', () async {
      final staleEpoch = AuthSessionEpoch.current;
      AuthSessionEpoch.advance();

      await expectLater(
        TokenRefreshCoordinator.refresh(epoch: staleEpoch),
        throwsA(isA<StateError>()),
      );
    });
  });
}
