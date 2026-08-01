import 'dart:convert';

import 'package:daidai_app/core/local_panel/local_panel_models.dart';
import 'package:daidai_app/core/local_panel/local_panel_session_resolver.dart';
import 'package:daidai_app/core/network/managed_local_session.dart';
import 'package:daidai_app/core/network/dio_client.dart';
import 'package:daidai_app/core/network/sse_client.dart';
import 'package:daidai_app/core/storage/secure_storage.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('PanelConfig', () {
    test('legacy JSON defaults to remote', () {
      final panel = PanelConfig.fromJson(const {
        'url': 'https://panel.example.com',
      });

      expect(panel.type, PanelType.remote);
      expect(panel.instanceId, isEmpty);
    });

    test('managed local type and instance ID serialize without token', () {
      const panel = PanelConfig(
        url: 'http://127.0.0.1:12345',
        type: PanelType.managedLocal,
        instanceId: 'core-7',
      );

      final json = panel.toJson();
      expect(json['type'], 'managedLocal');
      expect(json['instanceId'], 'core-7');
      expect(json.containsKey('local_token'), isFalse);
      expect(jsonEncode(json), isNot(contains('local-token-value')));
    });

    test('bad entries are skipped independently', () {
      final panels = parsePanelEntries([
        jsonEncode({'url': 'https://one.example.com'}),
        '{bad-json',
        jsonEncode({'url': 'https://two.example.com'}),
      ]);

      expect(panels.map((panel) => panel.url), [
        'https://one.example.com',
        'https://two.example.com',
      ]);
    });
  });

  test('LocalPanelStatus keeps local token only in memory model', () {
    final status = LocalPanelStatus.fromJson(const {
      'phase': 'ready',
      'base_url': 'http://127.0.0.1:12345',
      'local_token': 'local-token-value',
    });

    expect(status.localToken, 'local-token-value');
    expect(status.toString(), isNot(contains('local-token-value')));
  });

  test('managed local headers use exact origin for Dio and SSE', () {
    final session = ManagedLocalSession();
    final client = DioClient.forTesting(Dio(), managedLocalSession: session);
    client.setManagedLocalSession(
      'http://127.0.0.1:12345',
      'local-token-value',
    );

    expect(client.dio.options.headers['X-Daidai-Local-Token'], 'local-token-value');
    expect(client.dio.options.headers['Origin'], 'http://127.0.0.1:12345');
    expect(client.rawDio.options.headers['X-Daidai-Local-Token'], 'local-token-value');
    expect(client.rawDio.options.headers['Origin'], 'http://127.0.0.1:12345');

    expect(
      session.headersFor(Uri.parse('http://127.0.0.1:12345/events'))[
          'X-Daidai-Local-Token'],
      'local-token-value',
    );
    expect(
      session.headersFor(Uri.parse('http://127.0.0.1:54321/events')),
      isEmpty,
    );
    final mainDifferentPort = RequestOptions(
      path: 'http://127.0.0.1:54321/events',
      headers: {'X-Daidai-Local-Token': 'stale', 'Origin': 'stale'},
    );
    client.applyManagedLocalHeaders(mainDifferentPort);
    expect(
      mainDifferentPort.headers.containsKey('X-Daidai-Local-Token'),
      isFalse,
    );
    final rawDifferentPort = RequestOptions(
      path: 'http://127.0.0.1:54321/events',
      headers: Map<String, dynamic>.from(client.rawDio.options.headers),
    );
    client.applyManagedLocalHeaders(rawDifferentPort);
    expect(
      rawDifferentPort.headers.containsKey('X-Daidai-Local-Token'),
      isFalse,
    );

    final sseHeaders = buildSseHeaders(
      Uri.parse('http://127.0.0.1:12345/events'),
      accessToken: null,
      managedLocalSession: session,
    );
    expect(sseHeaders['X-Daidai-Local-Token'], 'local-token-value');
    expect(sseHeaders['Origin'], 'http://127.0.0.1:12345');
    final otherSseHeaders = buildSseHeaders(
      Uri.parse('http://127.0.0.1:54321/events'),
      accessToken: null,
      managedLocalSession: session,
    );
    expect(otherSseHeaders.containsKey('X-Daidai-Local-Token'), isFalse);

    client.setBaseUrl('https://panel.example.com');

    expect(client.dio.options.headers.containsKey('X-Daidai-Local-Token'), isFalse);
    expect(client.dio.options.headers.containsKey('Origin'), isFalse);
    expect(client.rawDio.options.headers.containsKey('X-Daidai-Local-Token'), isFalse);
    expect(client.rawDio.options.headers.containsKey('Origin'), isFalse);
    expect(
      buildSseHeaders(
        Uri.parse('http://127.0.0.1:12345/events'),
        accessToken: null,
        managedLocalSession: session,
      ).containsKey('X-Daidai-Local-Token'),
      isFalse,
    );
  });

  test('managed local resolver updates dynamic endpoint only when ready', () {
    const existing = PanelConfig(
      url: 'http://127.0.0.1:11111',
      name: '本地面板',
      type: PanelType.managedLocal,
      instanceId: 'old',
    );
    final ready = LocalPanelStatus.fromJson(const {
      'phase': 'ready',
      'base_url': 'http://127.0.0.1:22222',
      'instance_id': 'new',
      'local_token': 'local-token-value',
    });

    final resolved = resolveManagedLocalPanel(ready, existing: existing);
    expect(resolved.panel.url, 'http://127.0.0.1:22222');
    expect(resolved.panel.instanceId, 'new');
    expect(resolved.localToken, 'local-token-value');
    expect(
      () => resolveManagedLocalPanel(const LocalPanelStatus(), existing: existing),
      throwsStateError,
    );
  });

  test('diagnostic resolver accepts only degraded local endpoint', () {
    final degraded = LocalPanelStatus.fromJson(const {
      'phase': 'degraded',
      'base_url': 'http://127.0.0.1:33333',
      'instance_id': 'kotlin-local-fallback',
      'local_token': 'local-token-value',
      'fallback_mode': 'diagnostic',
    });

    final resolved = resolveManagedLocalDiagnostic(degraded);
    expect(resolved.panel.url, 'http://127.0.0.1:33333');
    expect(resolved.localToken, 'local-token-value');
    expect(
      () => resolveManagedLocalPanel(degraded),
      throwsStateError,
    );
    expect(
      () => resolveManagedLocalDiagnostic(
        LocalPanelStatus.fromJson(const {
          'phase': 'degraded',
          'base_url': 'http://127.0.0.1:44444',
          'local_token': 'local-token-value',
        }),
      ),
      throwsStateError,
    );
  });
}
