import 'package:dio/dio.dart';

import '../storage/secure_storage.dart';
import 'auth_session_epoch.dart';

typedef AuthSessionClearer = Future<void> Function(int epoch);
typedef AuthSessionInvalidated = Future<void> Function(int epoch);

bool isProtectedRequestAuthFailure(Object error) {
  if (error is DioException) {
    if (error.requestOptions.extra['auth_refresh_transient_failure'] == true) {
      return false;
    }
    return error.response?.statusCode == 401;
  }
  return false;
}

bool isRefreshAuthFailure(Object error) {
  if (error is DioException) {
    return const {400, 401, 403}.contains(error.response?.statusCode);
  }
  if (error is! StateError) return false;
  final message = error.message.toString().toLowerCase();
  return message.contains('missing') && message.contains('refresh');
}

class AuthSessionInvalidationCoordinator {
  AuthSessionInvalidationCoordinator({
    AuthSessionClearer? clearSession,
    AuthSessionInvalidated? onInvalidated,
  }) : _clearSession =
           clearSession ??
           ((epoch) => SecureStorage.clearAuthSession(authEpoch: epoch)),
       onInvalidated = onInvalidated;

  static final instance = AuthSessionInvalidationCoordinator();

  final AuthSessionClearer _clearSession;
  AuthSessionInvalidated? onInvalidated;
  final Map<int, Future<int?>> _inFlight = {};

  Future<int?> invalidate(int expectedEpoch) {
    final existing = _inFlight[expectedEpoch];
    if (existing != null) return existing;
    if (!AuthSessionEpoch.isCurrent(expectedEpoch)) return Future.value(null);

    final invalidatedEpoch = AuthSessionEpoch.advance();
    final operation = _invalidateOnce(invalidatedEpoch);
    _inFlight[expectedEpoch] = operation;
    operation.whenComplete(() {
      if (identical(_inFlight[expectedEpoch], operation)) {
        _inFlight.remove(expectedEpoch);
      }
    });
    return operation;
  }

  Future<int?> _invalidateOnce(int invalidatedEpoch) async {
    try {
      await _clearSession(invalidatedEpoch);
    } catch (_) {}
    if (!AuthSessionEpoch.isCurrent(invalidatedEpoch)) return null;
    try {
      await onInvalidated?.call(invalidatedEpoch);
    } catch (_) {}
    return invalidatedEpoch;
  }
}
