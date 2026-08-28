import 'dart:async';

import '../network/dio_client.dart';
import '../network/request_readiness_barrier.dart';
import '../storage/secure_storage.dart';
import 'local_panel_host.dart';
import 'local_panel_models.dart';
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

typedef ManagedLocalHealthCheck = Future<void> Function(
  String baseUrl,
  String token,
);

class ManagedLocalCoreUnavailable implements Exception {
  const ManagedLocalCoreUnavailable([this.cause]);

  final Object? cause;

  @override
  String toString() => cause == null
      ? 'Core unavailable'
      : 'Core unavailable: $cause';
}

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
    this.healthCheck,
    this.interval = const Duration(seconds: 30),
  }) : _host = host,
       _scheduler = scheduler,
       _loadCurrentPanel = loadCurrentPanel,
       _activatePanel = activatePanel,
       _setManagedSession = setManagedSession,
       _clearManagedSession = clearManagedSession;

  static final ManagedLocalConnectionMonitor instance = _createInstance();

  static ManagedLocalConnectionMonitor _createInstance() {
    final monitor = ManagedLocalConnectionMonitor(
      host: MethodChannelLocalPanelHost(),
      scheduler: _timerScheduler,
      loadCurrentPanel: SecureStorage.getCurrentPanel,
      activatePanel: SecureStorage.activatePanel,
      setManagedSession: DioClient.instance.setManagedLocalSession,
      clearManagedSession: DioClient.instance.clearManagedLocalSession,
    );
    RequestReadinessBarrier.install(monitor.ensureReadyForRequest);
    return monitor;
  }

  final LocalPanelHost _host;
  final MonitorScheduler _scheduler;
  final Future<PanelConfig?> Function() _loadCurrentPanel;
  final Future<void> Function(PanelConfig panel) _activatePanel;
  final void Function(String baseUrl, String token) _setManagedSession;
  final void Function() _clearManagedSession;
  final ManagedLocalHealthCheck? healthCheck;
  final Duration interval;

  static const Duration _healthyInterval = Duration(minutes: 5);

  MonitorScheduleHandle? _schedule;
  PanelConfig? _panel;
  String? _localToken;
  Future<void>? _activeReconcile;
  int? _activeReconcileGeneration;
  Future<void>? _foregroundRecovery;
  Future<void>? _remoteTransitionFuture;
  final _writes = _AsyncSerialQueue();
  int _generation = 0;
  bool _remoteTransition = false;
  bool _isForeground = true;
  bool _healthy = false;

  bool get healthy => _healthy;

  Future<void> initializeFromStorage() async {
    if (_remoteTransition) return;
    final expectedGeneration = _generation;
    try {
      final panel = await _loadCurrentPanel();
      if (_remoteTransition || expectedGeneration != _generation) return;
      if (panel?.type == PanelType.managedLocal) {
        await startManaged(panel!, expectedGeneration: expectedGeneration);
      } else {
        await stopAndDrain(
          expectedGeneration: expectedGeneration,
          waitForForegroundRecovery: false,
        );
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
        (expectedGeneration != null && expectedGeneration != _generation)) {
      return;
    }
    final generation = ++_generation;
    _panel = panel;
    _localToken = localToken;
    _healthy = false;
    _replaceSchedule();
    if (reconcileImmediately) await _reconcile(generation);
  }

  Future<bool> adoptHealthy(ManagedLocalPanelResolution resolved) async {
    if (_remoteTransition) return false;
    final generation = ++_generation;
    return _writes.run(() async {
      if (generation != _generation) return false;
      await _activatePanel(resolved.panel);
      if (generation != _generation) return false;
      _panel = resolved.panel;
      _localToken = resolved.localToken;
      _setManagedSession(resolved.panel.url, resolved.localToken);
      _healthy = true;
      _replaceSchedule();
      return true;
    });
  }

  Future<void> reconcileNow() => _reconcile(_generation);

  Future<void> ensureReadyForRequest() async {
    while (true) {
      final transition = _remoteTransitionFuture;
      if (transition == null) break;
      await transition;
    }
    if (_panel == null || _panel?.type != PanelType.managedLocal) return;
    final recovery = _foregroundRecovery ?? _reconcile(_generation);
    await recovery;
    if (!_healthy) throw const ManagedLocalCoreUnavailable();
  }

  Future<void> handleAppResumed() {
    _isForeground = true;
    _replaceSchedule();
    final active = _foregroundRecovery;
    if (active != null) return active;
    late final Future<void> recovery;
    recovery = (_panel == null ? initializeFromStorage() : reconcileNow())
        .whenComplete(() {
          if (identical(_foregroundRecovery, recovery)) {
            _foregroundRecovery = null;
          }
        });
    _foregroundRecovery = recovery;
    return recovery;
  }

  void handleAppPaused() {
    _isForeground = false;
    _schedule?.cancel();
    _schedule = null;
  }

  Future<void> stopAndDrain({
    bool clearSession = true,
    int? expectedGeneration,
    Future<void> Function()? remoteCommit,
    bool waitForForegroundRecovery = true,
  }) {
    Future<void> operation() => _stopAndDrain(
      clearSession: clearSession,
      expectedGeneration: expectedGeneration,
      remoteCommit: remoteCommit,
      waitForForegroundRecovery: waitForForegroundRecovery,
    );
    if (remoteCommit == null) return operation();
    _remoteTransition = true;
    final previous = _remoteTransitionFuture ?? Future<void>.value();
    late final Future<void> transition;
    transition = previous
        .catchError((_) {})
        .then((_) => operation())
        .whenComplete(() {
          if (identical(_remoteTransitionFuture, transition)) {
            _remoteTransitionFuture = null;
            _remoteTransition = false;
          }
        });
    _remoteTransitionFuture = transition;
    return transition;
  }

  Future<void> _stopAndDrain({
    required bool clearSession,
    required int? expectedGeneration,
    required Future<void> Function()? remoteCommit,
    required bool waitForForegroundRecovery,
  }) async {
    if (expectedGeneration != null && expectedGeneration != _generation) return;
    final stopGeneration = ++_generation;
    _panel = null;
    _localToken = null;
    _healthy = false;
    _schedule?.cancel();
    _schedule = null;
    final active = _activeReconcile;
    if (active != null) await active;
    final foregroundRecovery = _foregroundRecovery;
    if (waitForForegroundRecovery &&
        foregroundRecovery != null &&
        !identical(foregroundRecovery, active)) {
      await foregroundRecovery;
    }
    await _writes.drain();
    if (stopGeneration != _generation) {
      throw StateError('Local panel transition was superseded');
    }
    if (clearSession) _clearManagedSession();
    final commit = remoteCommit;
    if (commit != null) await commit();
  }

  Future<void> _reconcile(int generation) async {
    if (_panel == null || generation != _generation) return;
    final active = _activeReconcile;
    if (active != null) {
      if (_activeReconcileGeneration == generation) return active;
      await active;
      if (_panel == null || generation != _generation) return;
      return _reconcile(generation);
    }
    late final Future<void> running;
    running = _runReconcile(generation).whenComplete(() {
      if (identical(_activeReconcile, running)) {
        _activeReconcile = null;
        _activeReconcileGeneration = null;
      }
    });
    _activeReconcile = running;
    _activeReconcileGeneration = generation;
    return running;
  }

  Future<void> _runReconcile(int generation) async {
    try {
      final status = await _host.ensureStarted();
      if (generation != _generation) return;
      if (status.phase != LocalPanelPhase.ready) {
        throw const ManagedLocalCoreUnavailable();
      }
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
        if (sessionChanged) _localToken = resolved.localToken;
        _setManagedSession(resolved.panel.url, resolved.localToken);
        await healthCheck?.call(resolved.panel.url, resolved.localToken);
        if (generation != _generation) return;
        _healthy = true;
        _replaceSchedule();
      });
    } catch (_) {
      if (generation != _generation) return;
      _healthy = false;
      _replaceSchedule();
      await _writes.run(() async {
        if (generation == _generation) _clearManagedSession();
      });
    }
  }

  void _replaceSchedule() {
    _schedule?.cancel();
    _schedule = _isForeground && _panel != null
        ? _scheduler(_healthy ? _healthyInterval : interval, reconcileNow)
        : null;
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
