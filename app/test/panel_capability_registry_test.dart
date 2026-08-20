import 'package:daidai_app/core/local_panel/local_panel_models.dart';
import 'package:daidai_app/core/network/panel_capability_registry.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUp(PanelCapabilityRegistry.reset);

  test('normalizes equivalent panel URLs', () {
    expect(
      PanelCapabilityRegistry.normalizeServerUrl(
        'HTTPS://Panel.Example.com/root/',
      ),
      'https://panel.example.com/root',
    );
  });

  test('isolates capability entries by panel URL', () {
    PanelCapabilityRegistry.recordSupported(
      PanelCapability.taskViews,
      scope: 'https://one.example.com',
    );

    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.taskViews,
        scope: 'https://one.example.com',
      ),
      PanelCapabilityState.supported,
    );
    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.taskViews,
        scope: 'https://two.example.com',
      ),
      PanelCapabilityState.unknown,
    );
  });

  test('tracks optional management capabilities independently', () {
    const scope = 'https://one.example.com';
    PanelCapabilityRegistry.recordSupported(
      PanelCapability.healthCheck,
      scope: scope,
    );

    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.healthCheck,
        scope: scope,
      ),
      PanelCapabilityState.supported,
    );
    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.configScript,
        scope: scope,
      ),
      PanelCapabilityState.unknown,
    );
  });

  test('registers managed local platform capability profile', () {
    final status = LocalPanelStatus.fromJson({
      'phase': 'ready',
      'base_url': 'http://127.0.0.1:43123',
      'instance_id': 'local-1',
      'core_version': '3.0.6',
      'schema_version': 12,
      'platform_capabilities':
          '{"version":1,"capabilities":{"task_execution":{"state":"enabled","reasonCode":"ANDROID_HOST"},"system_restart":{"state":"unsupported","reasonCode":"ANDROID_SANDBOX"}}}',
    });

    PanelCapabilityRegistry.recordManagedLocalStatus(status);

    expect(
      status.capabilityState('task_execution'),
      PanelCapabilityState.supported,
    );
    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.systemRestart,
        scope: status.baseUrl,
      ),
      PanelCapabilityState.unsupported,
    );
    expect(
      PanelCapabilityRegistry.profileFor(scope: status.baseUrl)?.instanceMode,
      'managed_local',
    );
  });

  test('records PLATFORM_CAPABILITY response and friendly message', () {
    final options = RequestOptions(
      path: '/api/v1/system/restart',
      baseUrl: 'http://127.0.0.1:43123',
    );
    final response = Response<dynamic>(
      requestOptions: options,
      statusCode: 409,
      data: {
        'errorCode': 'PLATFORM_CAPABILITY',
        'capability': 'system_restart',
        'state': 'disabled',
        'reasonCode': 'BACKGROUND_WINDOW',
      },
    );
    final error = DioException.badResponse(
      statusCode: 409,
      requestOptions: options,
      response: response,
    );

    expect(
      PanelCapabilityRegistry.recordPlatformCapabilityFailure(error),
      isTrue,
    );
    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.systemRestart,
        scope: options.baseUrl,
      ),
      PanelCapabilityState.disabled,
    );
    expect(
      (response.data as Map)['message'],
      '系统重启 当前不可用（BACKGROUND_WINDOW）',
    );
  });

  test('expires supported entries after twelve hours', () {
    var now = DateTime.utc(2026, 8, 9);
    PanelCapabilityRegistry.setClockForTesting(() => now);
    PanelCapabilityRegistry.recordSupported(
      PanelCapability.taskViews,
      scope: 'https://one.example.com',
    );

    now = now.add(const Duration(hours: 12));

    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.taskViews,
        scope: 'https://one.example.com',
      ),
      PanelCapabilityState.unknown,
    );
  });

  test('expires unsupported entries after thirty minutes', () {
    var now = DateTime.utc(2026, 8, 9);
    PanelCapabilityRegistry.setClockForTesting(() => now);
    final options = RequestOptions(path: '/api/tasks/views');
    final error = DioException.badResponse(
      statusCode: 404,
      requestOptions: options,
      response: Response(requestOptions: options, statusCode: 404),
    );
    PanelCapabilityRegistry.recordFailure(
      PanelCapability.taskViews,
      error,
      scope: 'https://one.example.com',
    );

    now = now.add(const Duration(minutes: 30));

    expect(
      PanelCapabilityRegistry.shouldProbe(
        PanelCapability.taskViews,
        scope: 'https://one.example.com',
      ),
      isTrue,
    );
  });

  test('expires temporary failures after thirty seconds', () {
    var now = DateTime.utc(2026, 8, 9);
    PanelCapabilityRegistry.setClockForTesting(() => now);
    final options = RequestOptions(path: '/api/tasks/views');
    final error = DioException.connectionTimeout(
      timeout: const Duration(seconds: 1),
      requestOptions: options,
    );
    PanelCapabilityRegistry.recordFailure(
      PanelCapability.taskViews,
      error,
      scope: 'https://one.example.com',
    );

    now = now.add(const Duration(seconds: 30));

    expect(
      PanelCapabilityRegistry.stateFor(
        PanelCapability.taskViews,
        scope: 'https://one.example.com',
      ),
      PanelCapabilityState.unknown,
    );
  });

  test('classifies missing endpoints as unsupported', () {
    for (final statusCode in [404, 405]) {
      final options = RequestOptions(path: '/api/tasks/views');
      final error = DioException.badResponse(
        statusCode: statusCode,
        requestOptions: options,
        response: Response(requestOptions: options, statusCode: statusCode),
      );

      expect(
        PanelCapabilityRegistry.classifyFailure(error),
        PanelCapabilityState.unsupported,
      );
    }
  });

  test('classifies server and transport errors as temporary failures', () {
    final options = RequestOptions(path: '/api/tasks/views');
    final serverError = DioException.badResponse(
      statusCode: 503,
      requestOptions: options,
      response: Response(requestOptions: options, statusCode: 503),
    );
    final timeout = DioException.connectionTimeout(
      timeout: const Duration(seconds: 1),
      requestOptions: options,
    );

    expect(
      PanelCapabilityRegistry.classifyFailure(serverError),
      PanelCapabilityState.temporaryFailure,
    );
    expect(
      PanelCapabilityRegistry.classifyFailure(timeout),
      PanelCapabilityState.temporaryFailure,
    );
  });
}
