import 'package:daidai_app/core/network/panel_capability_registry.dart';
import 'package:daidai_app/features/settings/views/more_page.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  setUp(PanelCapabilityRegistry.reset);

  test('operator automation navigation follows route role policy', () {
    expect(showsOperatorAutomation('admin'), isTrue);
    expect(showsOperatorAutomation('operator'), isTrue);
    expect(showsOperatorAutomation('viewer'), isFalse);
    expect(showsOperatorAutomation(null), isFalse);
  });

  test('unknown remote capability stays visible and enabled for probing', () {
    const scope = 'https://legacy.example.com';

    expect(
      showsPanelCapability(PanelCapability.healthCheck, scope: scope),
      isTrue,
    );
    expect(
      enablesPanelCapability(PanelCapability.healthCheck, scope: scope),
      isTrue,
    );
  });

  test('known unsupported capability is hidden', () {
    const scope = 'https://modern.example.com';
    final options = RequestOptions(path: '/api/health', baseUrl: scope);
    PanelCapabilityRegistry.recordFailure(
      PanelCapability.healthCheck,
      DioException.badResponse(
        statusCode: 404,
        requestOptions: options,
        response: Response(requestOptions: options, statusCode: 404),
      ),
      scope: scope,
    );

    expect(
      showsPanelCapability(PanelCapability.healthCheck, scope: scope),
      isFalse,
    );
  });

  test('disabled mutation remains visible and disabled', () {
    const scope = 'http://127.0.0.1:43123';
    final options = RequestOptions(path: '/api/deps', baseUrl: scope);
    final response = Response<dynamic>(
      requestOptions: options,
      statusCode: 409,
      data: {
        'errorCode': 'PLATFORM_CAPABILITY',
        'capability': 'dependency_mutation',
        'state': 'disabled',
      },
    );
    PanelCapabilityRegistry.recordPlatformCapabilityFailure(
      DioException.badResponse(
        statusCode: 409,
        requestOptions: options,
        response: response,
      ),
    );

    expect(
      showsPanelCapability(PanelCapability.dependencyMutation, scope: scope),
      isTrue,
    );
    expect(
      enablesPanelCapability(PanelCapability.dependencyMutation, scope: scope),
      isFalse,
    );
  });
}
