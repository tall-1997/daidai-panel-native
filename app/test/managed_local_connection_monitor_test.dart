import 'dart:async';

import 'package:daidai_app/core/local_panel/local_panel_host.dart';
import 'package:daidai_app/core/local_panel/local_panel_models.dart';
import 'package:daidai_app/core/local_panel/managed_local_connection_monitor.dart';
import 'package:daidai_app/core/network/dio_client.dart';
import 'package:daidai_app/core/network/managed_local_session.dart';
import 'package:daidai_app/core/storage/secure_storage.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const oldPanel = PanelConfig(
    url: 'http://127.0.0.1:11111',
    name: '本地面板',
    type: PanelType.managedLocal,
    instanceId: 'old',
  );

  LocalPanelStatus ready(String endpoint, String token, String instanceId) =>
      LocalPanelStatus.fromJson({
        'phase': 'ready',
        'base_url': endpoint,
        'local_token': token,
        'instance_id': instanceId,
      });

  test('reconciles changed endpoint into Dio session and panel storage', () async {
    final host = FakeLocalPanelHost([
      ready('http://127.0.0.1:22222', 'new-token', 'new'),
    ]);
    final scheduler = ManualMonitorScheduler();
    final session = ManagedLocalSession()..set(oldPanel.url, 'old-token');
    final dio = DioClient.forTesting(Dio(), managedLocalSession: session)
      ..setManagedLocalSession(oldPanel.url, 'old-token');
    var stored = oldPanel;
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () async => stored,
      activatePanel: (panel) async => stored = panel,
      setManagedSession: dio.setManagedLocalSession,
      clearManagedSession: dio.clearManagedLocalSession,
    );

    await monitor.startManaged(oldPanel);

    expect(dio.baseUrl, 'http://127.0.0.1:22222');
    expect(stored.url, 'http://127.0.0.1:22222');
    expect(stored.instanceId, 'new');
    expect(
      session.headersFor(Uri.parse('http://127.0.0.1:22222/api'))[
          'X-Daidai-Local-Token'],
      'new-token',
    );
  });

  test('remote switch cancels timer and prevents further host calls', () async {
    final host = FakeLocalPanelHost([
      ready(oldPanel.url, 'old-token', 'old'),
    ]);
    final scheduler = ManualMonitorScheduler();
    var clears = 0;
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () async => oldPanel,
      activatePanel: (_) async {},
      setManagedSession: (_, _) {},
      clearManagedSession: () => clears++,
    );
    await monitor.startManaged(oldPanel, reconcileImmediately: false);

    await monitor.stopAndDrain();
    await scheduler.fire();

    expect(host.ensureCalls, 0);
    expect(scheduler.cancelled, isTrue);
    expect(clears, 1);
  });

  test('failure keeps healthy session and retries on next scheduled tick', () async {
    final host = FakeLocalPanelHost([
      StateError('temporarily unavailable'),
      ready('http://127.0.0.1:33333', 'recovered-token', 'recovered'),
    ]);
    final scheduler = ManualMonitorScheduler();
    final session = ManagedLocalSession()..set(oldPanel.url, 'old-token');
    final dio = DioClient.forTesting(Dio(), managedLocalSession: session)
      ..setManagedLocalSession(oldPanel.url, 'old-token');
    var stored = oldPanel;
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () async => stored,
      activatePanel: (panel) async => stored = panel,
      setManagedSession: dio.setManagedLocalSession,
      clearManagedSession: dio.clearManagedLocalSession,
    );

    await monitor.startManaged(oldPanel);
    expect(dio.baseUrl, oldPanel.url);
    expect(stored, same(oldPanel));

    await scheduler.fire();

    expect(dio.baseUrl, 'http://127.0.0.1:33333');
    expect(stored.instanceId, 'recovered');
    expect(host.ensureCalls, 2);
  });

  test('token rotation refreshes memory without rewriting unchanged panel', () async {
    final host = FakeLocalPanelHost([
      ready(oldPanel.url, 'rotated-token', oldPanel.instanceId),
    ]);
    final scheduler = ManualMonitorScheduler();
    final session = ManagedLocalSession()..set(oldPanel.url, 'old-token');
    var activations = 0;
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () async => oldPanel,
      activatePanel: (_) async => activations++,
      setManagedSession: session.set,
      clearManagedSession: session.clear,
    );

    await monitor.startManaged(oldPanel, localToken: 'old-token');

    expect(activations, 0);
    expect(
      session.headersFor(Uri.parse('${oldPanel.url}/api'))[
          'X-Daidai-Local-Token'],
      'rotated-token',
    );
  });

  test('late storage read cannot restart local after remote stop', () async {
    final storageResult = Completer<PanelConfig?>();
    final host = FakeLocalPanelHost([
      ready(oldPanel.url, 'old-token', oldPanel.instanceId),
    ]);
    final scheduler = ManualMonitorScheduler();
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () => storageResult.future,
      activatePanel: (_) async {},
      setManagedSession: (_, _) {},
      clearManagedSession: () {},
    );

    final initialization = monitor.initializeFromStorage();
    await monitor.stopAndDrain();
    storageResult.complete(oldPanel);
    await initialization;
    await scheduler.fire();

    expect(host.ensureCalls, 0);
    expect(scheduler.callback, isNull);
  });

  test('remote transition waits for in-flight local activation', () async {
    final activationEntered = Completer<void>();
    final releaseActivation = Completer<void>();
    final remoteCommitEntered = Completer<void>();
    final releaseRemoteCommit = Completer<void>();
    final host = FakeLocalPanelHost([
      ready('http://127.0.0.1:22222', 'new-token', 'new'),
    ]);
    final scheduler = ManualMonitorScheduler();
    PanelConfig stored = oldPanel;
    final monitor = ManagedLocalConnectionMonitor(
      host: host,
      scheduler: scheduler.schedule,
      loadCurrentPanel: () async => stored,
      activatePanel: (panel) async {
        activationEntered.complete();
        await releaseActivation.future;
        stored = panel;
      },
      setManagedSession: (_, _) {},
      clearManagedSession: () {},
    );

    final reconciliation = monitor.startManaged(oldPanel);
    await activationEntered.future;
    const remote = PanelConfig(url: 'https://remote.example.com');
    final stopped = monitor.stopAndDrain(
      remoteCommit: () async {
        remoteCommitEntered.complete();
        await releaseRemoteCommit.future;
        stored = remote;
      },
    );
    var stopCompleted = false;
    unawaited(stopped.then((_) => stopCompleted = true));
    await Future<void>.delayed(Duration.zero);
    expect(stopCompleted, isFalse);

    releaseActivation.complete();
    await reconciliation;
    await remoteCommitEntered.future;
    await monitor.initializeFromStorage();
    expect(host.ensureCalls, 1);
    releaseRemoteCommit.complete();
    await stopped;

    expect(stored.type, PanelType.remote);
    expect(stored.url, remote.url);
  });
}

class FakeLocalPanelHost implements LocalPanelHost {
  FakeLocalPanelHost(this.results);

  final List<Object> results;
  int ensureCalls = 0;

  @override
  Future<LocalPanelStatus> ensureStarted() async {
    final result = results[ensureCalls++];
    if (result is Error) throw result;
    if (result is Exception) throw result;
    return result as LocalPanelStatus;
  }

  @override
  Future<LocalPanelStatus> getStatus() => ensureStarted();

  @override
  Future<LocalPanelStatus> restart() => ensureStarted();

  @override
  Future<LocalPanelStatus> setPersistentSchedulingEnabled(bool enabled) =>
      ensureStarted();

  @override
  Future<LocalPanelStatus> stop() => ensureStarted();

  @override
  Stream<LocalPanelStatus> watchStatus() => const Stream.empty();
}

class ManualMonitorScheduler {
  Future<void> Function()? callback;
  bool cancelled = false;

  MonitorScheduleHandle schedule(
    Duration interval,
    Future<void> Function() callback,
  ) {
    cancelled = false;
    this.callback = callback;
    return CallbackMonitorScheduleHandle(() {
      cancelled = true;
      this.callback = null;
    });
  }

  Future<void> fire() async {
    if (!cancelled) await callback?.call();
  }
}
