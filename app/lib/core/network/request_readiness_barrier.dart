typedef RequestReadinessCallback = Future<void> Function();

const requestReadinessPreparedKey = 'managedLocalReadinessPrepared';

class RequestReadinessBarrier {
  RequestReadinessBarrier._();

  static RequestReadinessCallback? _callback;

  static void install(RequestReadinessCallback callback) {
    _callback = callback;
  }

  static Future<void> ensureReady() => _callback?.call() ?? Future<void>.value();
}
