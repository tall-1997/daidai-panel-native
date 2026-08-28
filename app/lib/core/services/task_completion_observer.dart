import 'dart:async';

import 'package:flutter/widgets.dart';

import '../../shared/models/task.dart';
import '../../shared/utils/api_utils.dart';
import '../auth/auth_session_epoch.dart';
import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../network/panel_capability_registry.dart';
import 'local_notification_service.dart';

class TaskCompletionObserver with WidgetsBindingObserver {
  TaskCompletionObserver._();

  static final TaskCompletionObserver instance = TaskCompletionObserver._();

  static const _pollInterval = Duration(seconds: 15);
  final Map<int, _ObservedTask> _observed = {};
  Timer? _timer;
  bool _requestRunning = false;
  bool _active = false;
  bool _resumed = true;
  String? _scope;
  final TaskCompletionRequestGeneration _requests =
      TaskCompletionRequestGeneration();

  void start() {
    if (_active) return;
    _requests.invalidate();
    _active = true;
    final lifecycleState = WidgetsBinding.instance.lifecycleState;
    _resumed =
        lifecycleState == null || lifecycleState == AppLifecycleState.resumed;
    _scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    WidgetsBinding.instance.addObserver(this);
    if (_resumed) unawaited(_poll());
    _timer = Timer.periodic(_pollInterval, (_) => _poll());
  }

  void stop() {
    if (!_active) return;
    _requests.invalidate();
    _active = false;
    _timer?.cancel();
    _timer = null;
    _requestRunning = false;
    _scope = null;
    _observed.clear();
    WidgetsBinding.instance.removeObserver(this);
  }

  void markRunning(int id) {
    if (!_active || id <= 0) return;
    final scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    if (_scope != scope) {
      _scope = scope;
      _observed.clear();
    }
    _observed[id] = const _ObservedTask(wasRunning: true);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _resumed = state == AppLifecycleState.resumed;
    if (_active && _resumed) unawaited(_poll());
  }

  Future<void> _poll() async {
    if (!_active || !_resumed || _requestRunning) return;
    _requestRunning = true;
    final generation = _requests.begin();
    final epoch = AuthSessionEpoch.current;
    final scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.tasks,
        queryParameters: const {'all': 1},
      );
      if (!_isCurrentRequest(generation, epoch, scope)) return;
      if (_scope != scope) {
        _scope = scope;
        _observed.clear();
      }
      final tasks = extractPaginated(response.data).items.map(Task.fromJson);
      for (final task in tasks) {
        final previous = _observed[task.id];
        final running = task.isRunning || task.isQueued;
        _observed[task.id] = _ObservedTask(
          wasRunning: running,
        );
        if (shouldNotifyTaskCompletion(
          wasRunning: previous?.wasRunning,
          isRunning: task.isRunning,
          isQueued: task.isQueued,
        )) {
          await _notify(task, generation, epoch, scope);
          if (!_isCurrentRequest(generation, epoch, scope)) return;
        }
      }
    } catch (_) {
      // Polling is best effort and retries on the next bounded interval.
    } finally {
      if (_requests.isCurrent(generation)) _requestRunning = false;
    }
  }

  bool _isCurrentRequest(int generation, int epoch, String scope) =>
      _active &&
      _requests.isCurrent(generation) &&
      AuthSessionEpoch.isCurrent(epoch) &&
      scope == AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);

  Future<void> _notify(
    Task task,
    int generation,
    int epoch,
    String scope,
  ) async {
    final service = LocalNotificationService();
    final enabled = await service.getChannelEnabled(NotificationChannel.task);
    if (!_isCurrentRequest(generation, epoch, scope) || !enabled) return;
    final title = task.name.trim().isEmpty ? '任务 #${task.id}' : task.name;
    final result = switch (task.lastRunStatus) {
      0 => ('执行完成', '任务已成功执行完毕'),
      1 => ('执行失败', '任务执行失败'),
      2 => ('已终止', '任务执行已终止'),
      _ => ('执行结束', '任务已结束，请查看运行日志'),
    };
    await service.showTaskNotification(
      id: task.id,
      title: '$title ${result.$1}',
      body: result.$2,
      payload: taskNotificationPayload(task.id),
    );
    if (!_isCurrentRequest(generation, epoch, scope)) return;
  }
}

class TaskCompletionRequestGeneration {
  int _current = 0;

  int begin() => ++_current;

  void invalidate() => _current++;

  bool isCurrent(int generation) => generation == _current;
}

class _ObservedTask {
  final bool wasRunning;

  const _ObservedTask({required this.wasRunning});
}

bool shouldNotifyTaskCompletion({
  required bool? wasRunning,
  required bool isRunning,
  required bool isQueued,
}) => wasRunning == true && !isRunning && !isQueued;
