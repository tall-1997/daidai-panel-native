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
