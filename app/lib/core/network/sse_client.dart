import 'dart:async';
import 'dart:convert';
import 'package:http/http.dart' as http;
import 'app_user_agent.dart';
import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import '../auth/token_refresh_coordinator.dart';
import 'managed_local_session.dart';

Map<String, String> buildSseHeaders(
  Uri uri, {
  required String? accessToken,
  ManagedLocalSession? managedLocalSession,
}) {
  final headers = <String, String>{
    ...AppUserAgent.defaultHeaders,
    'Accept': 'text/event-stream',
    'Cache-Control': 'no-cache',
  };
  if (accessToken != null) headers['Authorization'] = 'Bearer $accessToken';
  headers.addAll(
    (managedLocalSession ?? defaultManagedLocalSession).headersFor(uri),
  );
  return headers;
}

class SseEvent {
  final String? event;
  final String data;
  SseEvent({this.event, required this.data});
}

class SseClient {
  http.Client? _client;
  StreamSubscription? _subscription;
  Timer? _reconnectTimer;
  bool _closed = false;
  int _generation = 0;

  Future<void> connect({
    required String path,
    required void Function(SseEvent event) onEvent,
    void Function()? onDone,
    void Function(dynamic error)? onError,
    void Function()? onReconnecting,
    bool autoReconnect = false,
  }) async {
    _closed = false;
    final generation = ++_generation;
    await _doConnect(
      path: path,
      onEvent: onEvent,
      onDone: onDone,
      onError: onError,
      onReconnecting: onReconnecting,
      autoReconnect: autoReconnect,
      authRefreshAttempts: 0,
      generation: generation,
    );
  }

  Future<void> _doConnect({
    required String path,
    required void Function(SseEvent event) onEvent,
    void Function()? onDone,
    void Function(dynamic error)? onError,
    void Function()? onReconnecting,
    bool autoReconnect = false,
    int authRefreshAttempts = 0,
    required int generation,
  }) async {
    if (_closed || generation != _generation) return;

    final baseUrl = DioClient.instance.baseUrl;
    final token = await SecureStorage.getAccessToken();
    if (_closed || generation != _generation) return;
    final url = Uri.parse('$baseUrl$path');

    final client = http.Client();
    final request = http.Request('GET', url);
    request.headers.addAll(buildSseHeaders(url, accessToken: token));

    try {
      final response = await client.send(request);
      if (_closed || generation != _generation) {
        client.close();
        return;
      }
      _client = client;

      if (response.statusCode == 401 && !_closed) {
        if (authRefreshAttempts >= 1) {
          _disposeConnection();
          onError?.call('认证刷新后仍无法建立连接，请重新登录');
          return;
        }
        final refreshed = await _refreshAccessToken();
        if (refreshed && !_closed) {
          _disposeConnection();
          await _doConnect(
            path: path,
            onEvent: onEvent,
            onDone: onDone,
            onError: onError,
            onReconnecting: onReconnecting,
            autoReconnect: autoReconnect,
            authRefreshAttempts: authRefreshAttempts + 1,
            generation: generation,
          );
          return;
        }
        _disposeConnection();
        onError?.call('认证失败，请重新登录');
        return;
      }

      if (response.statusCode < 200 || response.statusCode >= 300) {
        _disposeConnection();
        onError?.call('SSE 连接失败（HTTP ${response.statusCode}）');
        return;
      }

      String buffer = '';
      String? currentEvent;
      final dataLines = <String>[];
      var reconnectScheduled = false;

      void scheduleReconnect() {
        if (!autoReconnect ||
            _closed ||
            generation != _generation ||
            reconnectScheduled) return;
        reconnectScheduled = true;
        onReconnecting?.call();
        _disposeConnection();
        _reconnectTimer?.cancel();
        _reconnectTimer = Timer(const Duration(seconds: 1), () {
          _doConnect(
            path: path,
            onEvent: onEvent,
            onDone: onDone,
            onError: onError,
            onReconnecting: onReconnecting,
            autoReconnect: autoReconnect,
            authRefreshAttempts: 0,
            generation: generation,
          );
        });
      }

      void emitEvent() {
        if (dataLines.isEmpty) {
          currentEvent = null;
          return;
        }
        final data = dataLines.join('\n');
        final event = SseEvent(event: currentEvent, data: data);
        if (generation != _generation) {
          return;
        }
        onEvent(event);

        if (currentEvent == 'done' &&
            data == 'reconnect' &&
            autoReconnect &&
            !_closed) {
          scheduleReconnect();
        }

        currentEvent = null;
        dataLines.clear();
      }

      final subscription = response.stream
          .transform(utf8.decoder)
          .listen(
            (chunk) {
              if (generation != _generation) return;
              buffer += chunk;
              final lines = buffer.split('\n');
              buffer = lines.removeLast(); // 保留不完整的行

              for (final rawLine in lines) {
                final line = _normalizeSseLine(rawLine);
                if (line.startsWith('event: ')) {
                  currentEvent = line.substring(7).trim();
                } else if (line.startsWith('data: ')) {
                  dataLines.add(line.substring(6));
                } else if (line.isEmpty) {
                  emitEvent();
                }
              }
            },
            onDone: () {
              if (generation != _generation) return;
              if (buffer.isNotEmpty) {
                final line = _normalizeSseLine(buffer);
                if (line.startsWith('data: ')) {
                  dataLines.add(line.substring(6));
                } else if (line.startsWith('event: ')) {
                  currentEvent = line.substring(7).trim();
                }
                buffer = '';
              }
              emitEvent();
              if (_closed) return;
              if (autoReconnect) {
                scheduleReconnect();
              } else {
                _disposeConnection();
                onDone?.call();
              }
            },
            onError: (error) {
              if (_closed || generation != _generation) return;
              if (autoReconnect) {
                scheduleReconnect();
              } else {
                _disposeConnection();
                onError?.call(error);
              }
            },
            cancelOnError: true,
          );
      if (generation == _generation) {
        _subscription = subscription;
      } else {
        await subscription.cancel();
        client.close();
      }
    } catch (e) {
      client.close();
      if (_closed || generation != _generation) return;
      _disposeConnection();
      if (autoReconnect) {
        onReconnecting?.call();
        _reconnectTimer?.cancel();
        _reconnectTimer = Timer(const Duration(seconds: 2), () {
          _doConnect(
            path: path,
            onEvent: onEvent,
            onDone: onDone,
            onError: onError,
            onReconnecting: onReconnecting,
            autoReconnect: autoReconnect,
            authRefreshAttempts: 0,
            generation: generation,
          );
        });
      } else {
        onError?.call(e);
      }
    }
  }

  void _disposeConnection() {
    _subscription?.cancel();
    _subscription = null;
    _client?.close();
    _client = null;
  }

  String _normalizeSseLine(String line) {
    return line.endsWith('\r') ? line.substring(0, line.length - 1) : line;
  }

  Future<bool> _refreshAccessToken() async {
    try {
      await TokenRefreshCoordinator.refresh();
      return true;
    } catch (_) {
      await SecureStorage.clearAuthSession();
      return false;
    }
  }

  void close() {
    _closed = true;
    _generation++;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _disposeConnection();
  }

}
