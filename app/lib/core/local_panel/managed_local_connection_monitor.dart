import 'dart:async';

import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import 'local_panel_host.dart';
import 'local_panel_session_resolver.dart';
import 'method_channel_local_panel_host.dart';

abstract interface class MonitorScheduleHandle {
  void cancel();
}

class CallbackMonitorScheduleHandle implements MonitorScheduleHandle {
  CallbackMonitorScheduleHandle(this._cancel);

  final void Function() _cancel;

  @override
  void cancel() => _cancel();
}

typedef MonitorScheduler = MonitorScheduleHandle Function(
  Duration interval,
  Future<void> Function() callback,
);

class _AsyncSerialQueue {
  Future<void> _tail = Future<void>.value();

  Future<T> run<T>(Future<T> Function() operation) {
    final completer = Completer<T>();
    _tail = _tail.then((_) async {
      try {
        completer.complete(await operation());
      } catch (error, stackTrace) {
        completer.completeError(error, stackTrace);
      }
    });
    return completer.future;
  }

  Future<void> drain() => _tail;
}

class ManagedLocalConnectionMonitor {
  ManagedLocalConnectionMonitor({
    required LocalPanelHost host,
    required MonitorScheduler scheduler,
    required Future<PanelConfig?> Function() loadCurrentPanel,
    required Future<void> Function(PanelConfig panel) activatePanel,
    required void Function(String baseUrl, String token) setManagedSession,
    required void Function() clearManagedSession,
    this.interval = const Duration(seconds: 30),
  }) : _host = host,
       _scheduler = scheduler,
       _loadCurrentPanel = loadCurrentPanel,
       _activatePanel = activatePanel,
       _setManagedSession = setManagedSession,
       _clearManagedSession = clearManagedSession;

  static final ManagedLocalConnectionMonitor instance =
      ManagedLocalConnectionMonitor(
        host: MethodChannelLocalPanelHost(),
        scheduler: _timerScheduler,
        loadCurrentPanel: SecureStorage.getCurrentPanel,
        activatePanel: SecureStorage.activatePanel,
        setManagedSession: DioClient.instance.setManagedLocalSession,
        clearManagedSession: DioClient.instance.clearManagedLocalSession,
      );

  final LocalPanelHost _host;
  final MonitorScheduler _scheduler;
  final Future<PanelConfig?> Function() _loadCurrentPanel;
  final Future<void> Function(PanelConfig panel) _activatePanel;
  final void Function(String baseUrl, String token) _setManagedSession;
  final void Function() _clearManagedSession;
  final Duration interval;

  MonitorScheduleHandle? _schedule;
  PanelConfig? _panel;
  String? _localToken;
  Future<void>? _activeReconcile;
  final _writes = _AsyncSerialQueue();
  int _generation = 0;
  bool _remoteTransition = false;

  Future<void> initializeFromStorage() async {
    if (_remoteTransition) return;
    final expectedGeneration = _generation;
    try {
      final panel = await _loadCurrentPanel();
      if (_remoteTransition || expectedGeneration != _generation) return;
      if (panel?.type == PanelType.managedLocal) {
        await startManaged(panel!, expectedGeneration: expectedGeneration);
      } else {
        await stopAndDrain(expectedGeneration: expectedGeneration);
      }
    } catch (_) {
      // A resumed lifecycle event will retry initialization/reconciliation.
    }
  }

  Future<void> startManaged(
    PanelConfig panel, {
    String? localToken,
    bool reconcileImmediately = true,
    int? expectedGeneration,
  }) async {
    if (_remoteTransition ||
        (expectedGeneration != null && expectedGeneration != _generation)) return;
    final generation = ++_generation;
    _panel = panel;
    _localToken = localToken;
    _schedule?.cancel();
    _schedule = _scheduler(interval, reconcileNow);
    if (reconcileImmediately) await _reconcile(generation);
  }

  Future<bool> adoptHealthy(ManagedLocalPanelResolution resolved) async {
    if (_remoteTransition) return false;
    final generation = ++_generation;
    _schedule?.cancel();
    _schedule = _scheduler(interval, reconcileNow);
    return _writes.run(() async {
      if (generation != _generation) return false;
      await _activatePanel(resolved.panel);
      if (generation != _generation) return false;
      _panel = resolved.panel;
      _localToken = resolved.localToken;
      _setManagedSession(resolved.panel.url, resolved.localToken);
      return true;
    });
  }

  Future<void> reconcileNow() => _reconcile(_generation);

  Future<void> handleAppResumed() async {
    if (_panel == null) {
      await initializeFromStorage();
    } else {
      await reconcileNow();
    }
  }

  Future<void> stopAndDrain({
    bool clearSession = true,
    int? expectedGeneration,
    Future<void> Function()? remoteCommit,
  }) async {
    if (expectedGeneration != null && expectedGeneration != _generation) return;
    final stopGeneration = ++_generation;
    _remoteTransition = remoteCommit != null;
    _panel = null;
    _localToken = null;
    _schedule?.cancel();
    _schedule = null;
    final active = _activeReconcile;
    if (active != null) await active;
    await _writes.drain();
    try {
      if (stopGeneration != _generation) return;
      if (clearSession) _clearManagedSession();
      final commit = remoteCommit;
      if (commit != null) await commit();
    } finally {
      if (stopGeneration == _generation) _remoteTransition = false;
    }
  }

  Future<void> _reconcile(int generation) async {
    if (_panel == null || generation != _generation) return;
    final active = _activeReconcile;
    if (active != null) {
      await active;
      if (_panel != null && generation == _generation) {
        return _reconcile(generation);
      }
      return;
    }
    late final Future<void> running;
    running = _runReconcile(generation).whenComplete(() {
      if (identical(_activeReconcile, running)) _activeReconcile = null;
    });
    _activeReconcile = running;
    return running;
  }

  Future<void> _runReconcile(int generation) async {
    try {
      final status = await _host.ensureStarted();
      if (generation != _generation) return;
      final stored = await _loadCurrentPanel();
      if (generation != _generation) return;
      final existing = stored?.type == PanelType.managedLocal ? stored : _panel;
      final resolved = resolveManagedLocalPanel(status, existing: existing);
      await _writes.run(() async {
        if (generation != _generation) return;
        final current = _panel;
        final panelChanged = current == null ||
            current.url != resolved.panel.url ||
            current.instanceId != resolved.panel.instanceId;
        final sessionChanged = panelChanged || _localToken != resolved.localToken;
        if (panelChanged) await _activatePanel(resolved.panel);
        if (generation != _generation) return;
        _panel = resolved.panel;
        if (sessionChanged) {
          _localToken = resolved.localToken;
          _setManagedSession(resolved.panel.url, resolved.localToken);
        }
      });
    } catch (_) {
      // Keep the last healthy in-memory session and retry on the next tick.
    }
  }

  static MonitorScheduleHandle _timerScheduler(
    Duration interval,
    Future<void> Function() callback,
  ) {
    final timer = Timer.periodic(interval, (_) {
      unawaited(callback());
    });
    return CallbackMonitorScheduleHandle(timer.cancel);
  }
}
