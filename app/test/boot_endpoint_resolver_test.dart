import 'package:daidai_app/core/local_panel/boot_endpoint_resolver.dart';
import 'package:daidai_app/core/local_panel/local_panel_models.dart';
import 'package:daidai_app/core/storage/secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const managed = PanelConfig(
    url: 'http://127.0.0.1:11111',
    type: PanelType.managedLocal,
  );

  test('authenticated managed local failure cannot enter dashboard', () async {
    final decision = await BootEndpointResolver.resolve(
      authenticated: true,
      panel: managed,
      ensureStarted: () async => throw StateError('unavailable'),
    );

    expect(decision.destination, BootEndpointDestination.localRecovery);
    expect(decision.managedLocal, isNull);
  });

  test('authenticated managed local success resolves token before dashboard', () async {
    final decision = await BootEndpointResolver.resolve(
      authenticated: true,
      panel: managed,
      ensureStarted: () async => LocalPanelStatus.fromJson(const {
        'phase': 'ready',
        'base_url': 'http://127.0.0.1:22222',
        'instance_id': 'new',
        'local_token': 'memory-token',
      }),
    );

    expect(decision.destination, BootEndpointDestination.dashboard);
    expect(decision.managedLocal?.panel.url, 'http://127.0.0.1:22222');
    expect(decision.managedLocal?.localToken, 'memory-token');
  });

  test('authenticated managed local empty token cannot enter dashboard', () async {
    final decision = await BootEndpointResolver.resolve(
      authenticated: true,
      panel: managed,
      ensureStarted: () async => LocalPanelStatus.fromJson(const {
        'phase': 'ready',
        'base_url': 'http://127.0.0.1:22222',
        'local_token': '',
      }),
    );

    expect(decision.destination, BootEndpointDestination.localRecovery);
  });

  test('authenticated remote keeps dashboard shortcut', () async {
    var ensureCalls = 0;
    final decision = await BootEndpointResolver.resolve(
      authenticated: true,
      panel: const PanelConfig(url: 'https://remote.example.com'),
      ensureStarted: () async {
        ensureCalls++;
        return const LocalPanelStatus();
      },
    );

    expect(decision.destination, BootEndpointDestination.dashboard);
    expect(ensureCalls, 0);
  });
}
