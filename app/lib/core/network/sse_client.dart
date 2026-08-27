import 'dart:async';
import 'dart:convert';

import 'package:flutter/widgets.dart';
import 'package:http/http.dart' as http;

import '../auth/auth_session_epoch.dart';
import '../auth/auth_token_snapshot.dart';
import '../auth/token_refresh_coordinator.dart';
import '../storage/secure_storage.dart';
import 'app_user_agent.dart';
import 'dio_client.dart';
import 'managed_local_session.dart';
import 'sse_protocol.dart';

Map<String, String> buildSseHeaders(
  Uri uri, {
  required String? accessToken,
  ManagedLocalSession? managedLocalSession,
  String? lastEventId,
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
  if (lastEventId != null && lastEventId.isNotEmpty) {
    headers['Last-Event-ID'] = lastEventId;
  }
  return headers;
}

class SseEvent {
  final String? event;
  final String data;
  final String? id;
  final bool hasExplicitId;
  final String? lastEventId;

  const SseEvent({
    this.event,
    required this.data,
    this.id,
    this.hasExplicitId = false,
    this.lastEventId,
  });
}

enum SseClientState {
  idle,
  connecting,
  open,
  reconnecting,
  paused,
  completed,
  closed,
}

class SseClient with WidgetsBindingObserver {
  static const _defaultRetryDelay = Duration(seconds: 1);
  static Future<void> Function(int epoch)? defaultOnAuthFailedForEpoch;

  final Future<void> Function(int epoch)? _onAuthFailedForEpoch;
  final http.Client Function() _clientFactory;
  final Future<void> Function(int epoch) _refreshToken;
  final Future<void> Function(int epoch) _clearAuthSession;

  SseClient({
    Future<void> Function(int epoch)? onAuthFailedForEpoch,
    http.Client Function()? clientFactory,
    Future<void> Function(int epoch)? refreshToken,
    Future<void> Function(int epoch)? clearAuthSession,
  }) : _onAuthFailedForEpoch = onAuthFailedForEpoch,
       _clientFactory = clientFactory ?? http.Client.new,
       _refreshToken =
           refreshToken ??
           ((epoch) async {
             await TokenRefreshCoordinator.refresh(epoch: epoch);
           }),
       _clearAuthSession =
           clearAuthSession ??
           ((epoch) => SecureStorage.clearAuthSession(authEpoch: epoch));

  http.Client? _client;
  StreamSubscription? _subscription;
  Timer? _reconnectTimer;
  bool _closed = false;
  bool _paused = false;
  int _generation = 0;
  int _reconnectAttempt = 0;
  Duration _retryDelay = _defaultRetryDelay;
  String? _lastEventId;
  final SseEventIdCache _seenEventIds = SseEventIdCache();
  _SseConnectionOptions? _options;
  bool _observingLifecycle = false;

  SseClientState _state = SseClientState.idle;
  SseClientState get state => _state;
  String? get lastEventId => _lastEventId;

  Future<void> connect({
    required String path,
    required void Function(SseEvent event) onEvent,
    void Function()? onDone,
    void Function(dynamic error)? onError,
    void Function()? onReconnecting,
    bool autoReconnect = false,
  }) async {
    if (!_observingLifecycle) {
      WidgetsBinding.instance.addObserver(this);
      _observingLifecycle = true;
    }
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _disposeConnection();
    _closed = false;
    _paused = false;
    _reconnectAttempt = 0;
    _retryDelay = _defaultRetryDelay;
    _lastEventId = null;
    _seenEventIds.clear();
    final epoch = AuthSessionEpoch.current;
    final baseUrl = DioClient.instance.baseUrl;
    _options = _SseConnectionOptions(
      path: path,
      epoch: epoch,
      baseUrl: baseUrl,
      onEvent: onEvent,
      onDone: onDone,
      onError: onError,
      onReconnecting: onReconnecting,
      autoReconnect: autoReconnect,
    );
    final options = _options!;
    final generation = ++_generation;
    _state = SseClientState.connecting;
    await _doConnect(
      options: options,
      authRefreshAttempts: 0,
      generation: generation,
    );
  }

  Future<void> _doConnect({
    required _SseConnectionOptions options,
    int authRefreshAttempts = 0,
    required int generation,
  }) async {
    if (!_isCurrent(options, generation)) return;

    final token = AuthTokenSnapshot.accessToken;
    if (!_isCurrent(options, generation)) return;
    final url = Uri.parse('${options.baseUrl}${options.path}');

    final client = _clientFactory();
    final request = http.Request('GET', url);
    final resumedFromEventId = _lastEventId;
    var resumeReplayWindow = resumedFromEventId?.isNotEmpty == true;
    request.headers.addAll(
      buildSseHeaders(
        url,
        accessToken: token,
        lastEventId: _lastEventId,
      ),
    );

    try {
      final response = await client.send(request);
      if (!_isCurrent(options, generation)) {
        client.close();
        return;
      }
      _client = client;

      if (response.statusCode == 401 && !_closed) {
        if (authRefreshAttempts >= 1) {
          _disposeConnection();
          _state = SseClientState.completed;
          await _handleAuthFailure(options, generation);
          if (_isCurrent(options, generation)) {
            options.onError?.call('认证刷新后仍无法建立连接，请重新登录');
          }
          return;
        }
        final refreshed = await _refreshAccessToken(options, generation);
        if (refreshed && _isCurrent(options, generation)) {
          _disposeConnection();
          await _doConnect(
            options: options,
            authRefreshAttempts: authRefreshAttempts + 1,
            generation: generation,
          );
          return;
        }
        _disposeConnection();
        _state = SseClientState.completed;
        if (_isCurrent(options, generation)) {
          options.onError?.call('认证失败，请重新登录');
        }
        return;
      }

      if (response.statusCode < 200 || response.statusCode >= 300) {
        _disposeConnection();
        final error = 'SSE 连接失败（HTTP ${response.statusCode}）';
        if (options.autoReconnect &&
            const {408, 425, 429, 500, 502, 503, 504}.contains(
              response.statusCode,
            )) {
          options.onError?.call(error);
          _scheduleReconnectAfterFailure(options, generation);
        } else {
          _state = SseClientState.completed;
          options.onError?.call(error);
        }
        return;
      }

      _state = SseClientState.open;
      final decoder = SseDecoder(lastEventId: _lastEventId);
      var reconnectScheduled = false;
      var terminalEventReceived = false;

      void scheduleReconnect() {
        if (!options.autoReconnect ||
            !_isCurrent(options, generation) ||
            reconnectScheduled) {
          return;
        }
        reconnectScheduled = true;
        final reconnectGeneration = ++_generation;
        _state = SseClientState.reconnecting;
        options.onReconnecting?.call();
        _disposeConnection();
        _reconnectTimer?.cancel();
        final delay = sseReconnectDelay(
          attempt: _reconnectAttempt++,
          baseDelay: _retryDelay,
        );
        _reconnectTimer = Timer(delay, () {
          if (!_isCurrent(options, reconnectGeneration)) return;
          _state = SseClientState.connecting;
          unawaited(
            _doConnect(
              options: options,
              authRefreshAttempts: 0,
              generation: reconnectGeneration,
            ),
          );
        });
      }

      void emitEvent(SseDecodedEvent decoded) {
        if (terminalEventReceived || !_isCurrent(options, generation)) return;
        _lastEventId = decoded.lastEventId;
        final isResumeReplay =
            resumeReplayWindow &&
            decoded.hasExplicitId &&
            decoded.id == resumedFromEventId;
        if (resumeReplayWindow && !isResumeReplay) {
          resumeReplayWindow = false;
        }
        final shouldDeliver =
            !isResumeReplay &&
            (!decoded.hasExplicitId ||
                decoded.id == null ||
                decoded.id!.isEmpty ||
                _seenEventIds.add(decoded.id!));

        _reconnectAttempt = 0;
        final event = SseEvent(
          event: decoded.event,
          data: decoded.data,
          id: decoded.id,
          hasExplicitId: decoded.hasExplicitId,
          lastEventId: decoded.lastEventId,
        );
        final terminal = isTerminalSseEvent(decoded.event, decoded.data);
        final reconnect = isReconnectSseEvent(decoded.event, decoded.data);
        if (terminal) {
          terminalEventReceived = true;
          _state = SseClientState.completed;
          _disposeConnection();
        }
        if (shouldDeliver) options.onEvent(event);

        if (!terminal && reconnect && options.autoReconnect) {
          scheduleReconnect();
        }
      }

      final subscription = response.stream
          .transform(utf8.decoder)
          .listen(
            (chunk) {
              if (!_isCurrent(options, generation)) {
                _disposeConnection();
                return;
              }
              final events = decoder.add(chunk);
              final retry = decoder.retryMilliseconds;
              if (retry != null) {
                _retryDelay = Duration(milliseconds: retry);
              }
              for (final event in events) {
                emitEvent(event);
                if (terminalEventReceived) break;
              }
            },
            onDone: () {
              if (!_isCurrent(options, generation)) return;
              final finalEvents = decoder.close();
              final retry = decoder.retryMilliseconds;
              if (retry != null) {
                _retryDelay = Duration(milliseconds: retry);
              }
              for (final event in finalEvents) {
                emitEvent(event);
                if (terminalEventReceived) break;
              }
              if (_closed) return;
              if (terminalEventReceived) {
                _disposeConnection();
              } else if (options.autoReconnect) {
                scheduleReconnect();
              } else {
                _disposeConnection();
                _state = SseClientState.completed;
                options.onDone?.call();
              }
            },
            onError: (error) {
              if (!_isCurrent(options, generation)) return;
              if (options.autoReconnect) {
                scheduleReconnect();
              } else {
                _disposeConnection();
                _state = SseClientState.completed;
                options.onError?.call(error);
              }
            },
            cancelOnError: true,
          );
      if (_isCurrent(options, generation) &&
          !terminalEventReceived &&
          !reconnectScheduled) {
        _subscription = subscription;
      } else {
        await subscription.cancel();
        client.close();
      }
    } catch (e) {
      client.close();
      if (!_isCurrent(options, generation)) return;
      _disposeConnection();
      if (options.autoReconnect) {
        _scheduleReconnectAfterFailure(options, generation);
      } else {
        _state = SseClientState.completed;
        options.onError?.call(e);
      }
    }
  }

  void _scheduleReconnectAfterFailure(
    _SseConnectionOptions options,
    int generation,
  ) {
    if (!_isCurrent(options, generation)) return;
    final reconnectGeneration = ++_generation;
    _state = SseClientState.reconnecting;
    options.onReconnecting?.call();
    final delay = sseReconnectDelay(
      attempt: _reconnectAttempt++,
      baseDelay: _retryDelay,
    );
    _reconnectTimer?.cancel();
    _reconnectTimer = Timer(delay, () {
      if (!_isCurrent(options, reconnectGeneration)) return;
      _state = SseClientState.connecting;
      unawaited(
        _doConnect(
          options: options,
          authRefreshAttempts: 0,
          generation: reconnectGeneration,
        ),
      );
    });
  }

  void _disposeConnection() {
    _subscription?.cancel();
    _subscription = null;
    _client?.close();
    _client = null;
  }

  bool _isCurrent(_SseConnectionOptions options, int generation) =>
      !_closed &&
      !_paused &&
      generation == _generation &&
      AuthSessionEpoch.isCurrent(options.epoch) &&
      DioClient.instance.baseUrl == options.baseUrl;

  Future<bool> _refreshAccessToken(
    _SseConnectionOptions options,
    int generation,
  ) async {
    try {
      await _refreshToken(options.epoch);
      return _isCurrent(options, generation);
    } catch (_) {
      await _handleAuthFailure(options, generation);
      return false;
    }
  }

  Future<void> _handleAuthFailure(
    _SseConnectionOptions options,
    int generation,
  ) async {
    if (!_isCurrent(options, generation)) return;
    try {
      await _clearAuthSession(options.epoch);
    } catch (_) {}
    if (!_isCurrent(options, generation)) return;
    try {
      await (_onAuthFailedForEpoch ?? defaultOnAuthFailedForEpoch)?.call(
        options.epoch,
      );
    } catch (_) {}
  }

  void close() {
    _closed = true;
    _paused = false;
    _generation++;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _disposeConnection();
    if (_observingLifecycle) {
      WidgetsBinding.instance.removeObserver(this);
      _observingLifecycle = false;
    }
    _state = SseClientState.closed;
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    switch (state) {
      case AppLifecycleState.resumed:
        unawaited(resume());
        return;
      case AppLifecycleState.inactive:
      case AppLifecycleState.hidden:
        // 短暂 inactive（如 iOS 下拉通知/控制中心）不主动断开，避免群涌式重连
        return;
      case AppLifecycleState.paused:
      case AppLifecycleState.detached:
        pause();
        return;
    }
  }

  void pause() {
    if (_closed ||
        _paused ||
        _options == null ||
        _state == SseClientState.completed) {
      return;
    }
    _paused = true;
    _generation++;
    _reconnectTimer?.cancel();
    _reconnectTimer = null;
    _disposeConnection();
    _state = SseClientState.paused;
  }

  Future<void> resume() async {
    final options = _options;
    if (_closed || !_paused || options == null) return;
    _paused = false;
    final generation = ++_generation;
    _state = SseClientState.connecting;
    await _doConnect(
      options: options,
      authRefreshAttempts: 0,
      generation: generation,
    );
  }
}

class _SseConnectionOptions {
  final String path;
  final int epoch;
  final String baseUrl;
  final void Function(SseEvent event) onEvent;
  final void Function()? onDone;
  final void Function(dynamic error)? onError;
  final void Function()? onReconnecting;
  final bool autoReconnect;

  const _SseConnectionOptions({
    required this.path,
    required this.epoch,
    required this.baseUrl,
    required this.onEvent,
    required this.onDone,
    required this.onError,
    required this.onReconnecting,
    required this.autoReconnect,
  });
}
