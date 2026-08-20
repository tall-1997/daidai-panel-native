import 'dart:async';
import 'dart:convert';

import 'package:daidai_app/core/auth/auth_session_epoch.dart';
import 'package:daidai_app/core/network/dio_client.dart';
import 'package:daidai_app/core/network/sse_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

class _RequestClient extends http.BaseClient {
  _RequestClient(this.onSend);

  final Future<http.StreamedResponse> Function(http.BaseRequest request) onSend;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) =>
      onSend(request);
}

http.StreamedResponse _response(int statusCode, [String body = '']) =>
    http.StreamedResponse(
      Stream.value(utf8.encode(body)),
      statusCode,
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late String originalBaseUrl;

  setUp(() {
    originalBaseUrl = DioClient.instance.baseUrl;
    DioClient.instance.setBaseUrl('https://old.example.com');
  });

  tearDown(() {
    DioClient.instance.setBaseUrl(originalBaseUrl);
    SseClient.defaultOnAuthFailedForEpoch = null;
  });

  test('refresh uses the epoch captured by connect', () async {
    final epoch = AuthSessionEpoch.current;
    final refreshEpochs = <int>[];
    final requests = <Uri>[];
    final clients = <_RequestClient>[
      _RequestClient((request) async {
        requests.add(request.url);
        return _response(401);
      }),
      _RequestClient((request) async {
        requests.add(request.url);
        return _response(200, 'event: done\ndata: finished\n\n');
      }),
    ];
    final client = SseClient(
      clientFactory: () => clients.removeAt(0),
      refreshToken: (capturedEpoch) async {
        refreshEpochs.add(capturedEpoch);
      },
    );

    await client.connect(path: '/stream', onEvent: (_) {});
    await Future<void>.delayed(Duration.zero);

    expect(refreshEpochs, [epoch]);
    expect(requests, [
      Uri.parse('https://old.example.com/stream'),
      Uri.parse('https://old.example.com/stream'),
    ]);
    client.close();
  });

  test('stale epoch suppresses stream callbacks and reconnects', () async {
    final stream = StreamController<List<int>>();
    var eventCalls = 0;
    var doneCalls = 0;
    var reconnectCalls = 0;
    var clientCreations = 0;
    final client = SseClient(
      clientFactory: () {
        clientCreations++;
        return _RequestClient(
          (_) async => http.StreamedResponse(stream.stream, 200),
        );
      },
    );

    await client.connect(
      path: '/stream',
      autoReconnect: true,
      onEvent: (_) => eventCalls++,
      onDone: () => doneCalls++,
      onReconnecting: () => reconnectCalls++,
    );
    AuthSessionEpoch.advance();
    stream.add(utf8.encode('data: stale\n\n'));
    await stream.close();
    await Future<void>.delayed(const Duration(milliseconds: 10));

    expect(eventCalls, 0);
    expect(doneCalls, 0);
    expect(reconnectCalls, 0);
    expect(clientCreations, 1);
    client.close();
  });

  test('failed stale refresh cannot clear or reject a new session', () async {
    final epoch = AuthSessionEpoch.current;
    final refreshStarted = Completer<void>();
    final finishRefresh = Completer<void>();
    final clearedEpochs = <int>[];
    final failedEpochs = <int>[];
    final errors = <dynamic>[];
    final client = SseClient(
      clientFactory: () => _RequestClient((_) async => _response(401)),
      refreshToken: (capturedEpoch) async {
        expect(capturedEpoch, epoch);
        refreshStarted.complete();
        await finishRefresh.future;
      },
      clearAuthSession: (capturedEpoch) async {
        clearedEpochs.add(capturedEpoch);
      },
      onAuthFailedForEpoch: (epoch) async => failedEpochs.add(epoch),
    );

    final connection = client.connect(
      path: '/stream',
      onEvent: (_) {},
      onError: errors.add,
    );
    await refreshStarted.future;
    AuthSessionEpoch.advance();
    finishRefresh.completeError(StateError('refresh failed'));
    await connection;

    expect(clearedEpochs, isEmpty);
    expect(failedEpochs, isEmpty);
    expect(errors, isEmpty);
    client.close();
  });

  test('current auth failure clears and reports its epoch once', () async {
    final epoch = AuthSessionEpoch.current;
    final clearedEpochs = <int>[];
    final failedEpochs = <int>[];
    final client = SseClient(
      clientFactory: () => _RequestClient((_) async => _response(401)),
      refreshToken: (_) async => throw StateError('refresh failed'),
      clearAuthSession: (capturedEpoch) async {
        clearedEpochs.add(capturedEpoch);
      },
      onAuthFailedForEpoch: (epoch) async => failedEpochs.add(epoch),
    );

    await client.connect(path: '/stream', onEvent: (_) {});

    expect(clearedEpochs, [epoch]);
    expect(failedEpochs, [epoch]);
    client.close();
  });

  test('suppresses repeated explicit IDs within one stream', () async {
    final events = <SseEvent>[];
    final client = SseClient(
      clientFactory: () => _RequestClient(
        (_) async => _response(
          200,
          'id: event-1\ndata: first\n\n'
          'id: event-1\ndata: duplicate\n\n'
          'event: done\ndata: finished\n\n',
        ),
      ),
    );

    await client.connect(path: '/stream', onEvent: events.add);
    await Future<void>.delayed(Duration.zero);

    expect(events.map((event) => event.data), ['first', 'finished']);
    client.close();
  });

  test('suppresses the resume event replayed after reconnect', () async {
    final events = <SseEvent>[];
    final requests = <http.BaseRequest>[];
    final clients = <_RequestClient>[
      _RequestClient((request) async {
        requests.add(request);
        return _response(
          200,
          'retry: 0\nid: event-1\ndata: first\n\n'
          'event: done\ndata: reconnect\n\n',
        );
      }),
      _RequestClient((request) async {
        requests.add(request);
        return _response(
          200,
          'id: event-1\ndata: replay\n\n'
          'id: event-2\ndata: second\n\n'
          'event: done\ndata: finished\n\n',
        );
      }),
    ];
    final client = SseClient(clientFactory: () => clients.removeAt(0));

    await client.connect(
      path: '/stream',
      autoReconnect: true,
      onEvent: events.add,
    );
    await Future<void>.delayed(const Duration(milliseconds: 10));

    expect(requests, hasLength(2));
    expect(requests.last.headers['Last-Event-ID'], 'event-1');
    expect(events.map((event) => event.data), [
      'first',
      'reconnect',
      'second',
      'finished',
    ]);
    client.close();
  });

  test('delivers repeated events with an explicit empty ID', () async {
    final events = <SseEvent>[];
    final client = SseClient(
      clientFactory: () => _RequestClient(
        (_) async => _response(
          200,
          'id:\ndata: first\n\nid:\ndata: second\n\n'
          'event: done\ndata: finished\n\n',
        ),
      ),
    );

    await client.connect(path: '/stream', onEvent: events.add);
    await Future<void>.delayed(Duration.zero);

    expect(events.map((event) => event.data), ['first', 'second', 'finished']);
    client.close();
  });

  test('delivers an ID again after bounded cache eviction', () async {
    final events = <SseEvent>[];
    final body = StringBuffer('id: event-0\ndata: first\n\n');
    for (var index = 1; index <= 256; index++) {
      body.write('id: event-$index\ndata: value-$index\n\n');
    }
    body.write(
      'id: event-0\ndata: after-eviction\n\n'
      'event: done\ndata: finished\n\n',
    );
    final client = SseClient(
      clientFactory: () =>
          _RequestClient((_) async => _response(200, body.toString())),
    );

    await client.connect(path: '/stream', onEvent: events.add);
    await Future<void>.delayed(Duration.zero);

    expect(events.where((event) => event.id == 'event-0'), hasLength(2));
    expect(events[events.length - 2].data, 'after-eviction');
    client.close();
  });
}
