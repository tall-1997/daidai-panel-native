import 'dart:async';

import 'package:daidai_app/core/auth/auth_session_epoch.dart';
import 'package:daidai_app/core/auth/auth_session_invalidation_coordinator.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('concurrent failures advance and invalidate one epoch once', () async {
    final expectedEpoch = AuthSessionEpoch.current;
    final clearStarted = Completer<void>();
    final releaseClear = Completer<void>();
    final clearedEpochs = <int>[];
    final notifiedEpochs = <int>[];
    final coordinator = AuthSessionInvalidationCoordinator(
      clearSession: (epoch) async {
        clearedEpochs.add(epoch);
        clearStarted.complete();
        await releaseClear.future;
      },
      onInvalidated: (epoch) async => notifiedEpochs.add(epoch),
    );

    final first = coordinator.invalidate(expectedEpoch);
    await clearStarted.future;
    final second = coordinator.invalidate(expectedEpoch);
    expect(AuthSessionEpoch.current, expectedEpoch + 1);

    releaseClear.complete();
    expect(await Future.wait([first, second]), [
      expectedEpoch + 1,
      expectedEpoch + 1,
    ]);
    expect(clearedEpochs, [expectedEpoch + 1]);
    expect(notifiedEpochs, [expectedEpoch + 1]);
  });

  test('a stale failure cannot invalidate the current session', () async {
    final staleEpoch = AuthSessionEpoch.current;
    AuthSessionEpoch.advance();
    var clears = 0;
    final coordinator = AuthSessionInvalidationCoordinator(
      clearSession: (_) async => clears++,
    );

    expect(await coordinator.invalidate(staleEpoch), isNull);
    expect(clears, 0);
    expect(AuthSessionEpoch.current, staleEpoch + 1);
  });

  test('protected requests invalidate only on 401', () {
    for (final status in [400, 403]) {
      final options = RequestOptions(path: '/api/tasks');
      expect(
        isProtectedRequestAuthFailure(
          DioException.badResponse(
            statusCode: status,
            requestOptions: options,
            response: Response(requestOptions: options, statusCode: status),
          ),
        ),
        isFalse,
      );
    }
    final options = RequestOptions(path: '/api/tasks');
    expect(
      isProtectedRequestAuthFailure(
        DioException.badResponse(
          statusCode: 401,
          requestOptions: options,
          response: Response(requestOptions: options, statusCode: 401),
        ),
      ),
      isTrue,
    );
  });

  test('refresh failures use their own status classification', () {
    final options = RequestOptions(path: '/auth/refresh');
    expect(
      isRefreshAuthFailure(
        DioException(
          requestOptions: options,
          type: DioExceptionType.connectionError,
        ),
      ),
      isFalse,
    );
    for (final status in [400, 401, 403]) {
      expect(
        isRefreshAuthFailure(
          DioException.badResponse(
            statusCode: status,
            requestOptions: options,
            response: Response(requestOptions: options, statusCode: status),
          ),
        ),
        isTrue,
      );
    }
    expect(isRefreshAuthFailure(StateError('Missing refresh token')), isTrue);
    expect(
      isRefreshAuthFailure(
        StateError('Missing access_token in refresh response'),
      ),
      isTrue,
    );
    expect(
      isRefreshAuthFailure(
        StateError('Auth session changed during token refresh'),
      ),
      isFalse,
    );
  });
}
