import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../shared/utils/api_utils.dart';

String _formatBytes(dynamic bytes) {
  final b = _number(bytes);
  if (b == null) {
    return '-';
  }
  if (b < 1024) return '${b.toStringAsFixed(0)}B';
  if (b < 1024 * 1024) return '${(b / 1024).toStringAsFixed(1)}KB';
  if (b < 1024 * 1024 * 1024) {
    return '${(b / 1024 / 1024).toStringAsFixed(1)}MB';
  }
  return '${(b / 1024 / 1024 / 1024).toStringAsFixed(1)}GB';
}

bool _resourceUnavailable(dynamic total) {
  final value = _number(total);
  return value == null || value <= 0;
}

double? _number(dynamic value) {
  if (value is num) return value.toDouble();
  if (value is String) return double.tryParse(value.trim());
  return null;
}

int _integer(dynamic value) => _number(value)?.toInt() ?? 0;

class DashboardData {
  final Map<String, dynamic> system;
  final Map<String, dynamic> dashboard;
  final bool loading;
  final String? error;

  const DashboardData({
    this.system = const {},
    this.dashboard = const {},
    this.loading = false,
    this.error,
  });

  // 系统资源
  double get cpuUsage => _number(system['cpu_usage']) ?? 0;
  double get memoryUsage => _number(system['memory_usage']) ?? 0;
  double get diskUsage => _number(system['disk_usage']) ?? 0;
  bool get memoryUnavailable => _resourceUnavailable(system['memory_total']);
  String get memoryTotal => _formatBytes(system['memory_total']);
  String get memoryUsed => _formatBytes(system['memory_used']);
  String get diskTotal => _formatBytes(system['disk_total']);
  String get diskUsed => _formatBytes(system['disk_used']);
  String get uptime => system['uptime']?.toString() ?? '-';
  String get hostname => system['hostname']?.toString() ?? '-';
  String get os => system['os']?.toString() ?? '-';
  String get panelTitle => system['panel_title']?.toString() ?? '';
  String get panelVersion => system['panel_version']?.toString() ?? '';

  // 仪表盘数据 — 字段名匹配后端实际返回
  int get totalTasks => _integer(dashboard['task_count']);
  int get enabledTasks => _integer(dashboard['enabled_tasks']);
  int get runningTasks => _integer(dashboard['running_tasks']);
  int get disabledTasks => totalTasks - enabledTasks;
  int get todaySuccess => _integer(dashboard['success_logs']);
  int get todayFailed => _integer(dashboard['failed_logs']);
  int get todayAborted => _integer(
    dashboard['aborted_logs'] ??
        dashboard['abort_logs'] ??
        dashboard['aborted'] ??
        dashboard['abort'],
  );
  List<dynamic> get recentLogs => dashboard['recent_logs'] as List? ?? [];
  List<dynamic> get executionTrend => dashboard['daily_stats'] as List? ?? [];

  DashboardData copyWith({
    Map<String, dynamic>? system,
    Map<String, dynamic>? dashboard,
    bool? loading,
    String? error,
  }) {
    return DashboardData(
      system: system ?? this.system,
      dashboard: dashboard ?? this.dashboard,
      loading: loading ?? this.loading,
      error: error,
    );
  }
}

class DashboardNotifier extends StateNotifier<DashboardData> {
  DashboardNotifier() : super(const DashboardData());

  int _loadId = 0;
  String? _scope;

  Future<void> load() async {
    final loadId = ++_loadId;
    final capabilityScope = PanelCapabilityRegistry.currentScope;
    final sessionScope = AuthSessionEpoch.scoped(capabilityScope);
    if (_scope != sessionScope) {
      _scope = sessionScope;
      state = const DashboardData();
    }
    state = state.copyWith(loading: true, error: null);
    try {
      final dio = DioClient.instance.dio;
      final coreResults = await Future.wait([
        dio.get(ApiEndpoints.systemInfo),
        dio.get(ApiEndpoints.dashboard),
      ]);
      final sysData = extractData(coreResults[0].data);
      final dashData = extractData(coreResults[1].data);
      final sysMap = sysData is Map
          ? Map<String, dynamic>.from(sysData)
          : <String, dynamic>{};
      if (loadId != _loadId ||
          sessionScope !=
              AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(
        system: sysMap,
        dashboard: dashData is Map
            ? Map<String, dynamic>.from(dashData)
            : const {},
        loading: false,
      );
      unawaited(
        _loadEnhancements(loadId, sessionScope, capabilityScope, sysMap),
      );
    } catch (e) {
      if (loadId != _loadId ||
          sessionScope !=
              AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(loading: false, error: '加载失败');
    }
  }

  Future<void> _loadEnhancements(
    int loadId,
    String sessionScope,
    String capabilityScope,
    Map<String, dynamic> system,
  ) async {
    final results = await Future.wait([
      _loadEnhancement(
        PanelCapability.panelSettings,
        ApiEndpoints.panelSettings,
        capabilityScope,
      ),
      _loadEnhancement(
        PanelCapability.systemVersion,
        ApiEndpoints.systemVersion,
        capabilityScope,
      ),
    ]);
    if (loadId != _loadId ||
        sessionScope !=
            AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
      return;
    }
    final nextSystem = Map<String, dynamic>.from(system);
    final panelData = results[0];
    if (panelData is Map) {
      final title = panelData['panel_title']?.toString() ?? '';
      if (title.isNotEmpty) nextSystem['panel_title'] = title;
    }
    final versionData = results[1];
    if (versionData is Map) {
      final version = versionData['version']?.toString() ?? '';
      if (version.isNotEmpty) nextSystem['panel_version'] = version;
    }
    state = state.copyWith(system: nextSystem);
  }

  Future<dynamic> _loadEnhancement(
    PanelCapability capability,
    String endpoint,
    String scope,
  ) async {
    if (!PanelCapabilityRegistry.shouldProbe(capability, scope: scope) &&
        PanelCapabilityRegistry.isUnsupported(capability, scope: scope)) {
      return null;
    }
    try {
      final response = await DioClient.instance.dio.get(endpoint);
      PanelCapabilityRegistry.recordSupported(capability, scope: scope);
      return extractData(response.data);
    } catch (error) {
      PanelCapabilityRegistry.recordFailure(capability, error, scope: scope);
      return null;
    }
  }
}

final dashboardProvider =
    StateNotifierProvider<DashboardNotifier, DashboardData>((ref) {
      return DashboardNotifier();
    });
