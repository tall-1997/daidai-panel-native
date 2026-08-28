import 'package:dio/dio.dart';

import '../local_panel/local_panel_models.dart';
import 'panel_profile.dart';
export 'panel_profile.dart';

class PanelCapabilityEntry {
  final PanelCapabilityState state;
  final DateTime checkedAt;
  final DateTime expiresAt;

  const PanelCapabilityEntry({
    required this.state,
    required this.checkedAt,
    required this.expiresAt,
  });

  bool isExpiredAt(DateTime now) => !now.isBefore(expiresAt);
}

class PanelCapabilityRegistry {
  PanelCapabilityRegistry._();

  static const _supportedTtl = Duration(hours: 12);
  static const _unsupportedTtl = Duration(minutes: 30);
  static const _temporaryFailureTtl = Duration(seconds: 30);
  static DateTime Function() _now = DateTime.now;
  static String _currentScope = '';
  static final Set<void Function()> _scopeListeners = {};
  static final Map<String, Map<PanelCapability, PanelCapabilityEntry>>
  _profiles = {};
  static final Map<String, PanelProfile> _panelProfiles = {};

  static String get currentScope => _currentScope;

  static void setCurrentScope(String serverUrl) {
    final nextScope = normalizeServerUrl(serverUrl);
    if (nextScope == _currentScope) return;
    _currentScope = nextScope;
    for (final listener in List<void Function()>.of(_scopeListeners)) {
      listener();
    }
  }

  static void addScopeListener(void Function() listener) =>
      _scopeListeners.add(listener);

  static void removeScopeListener(void Function() listener) =>
      _scopeListeners.remove(listener);

  static String normalizeServerUrl(String value) {
    final trimmed = value.trim();
    if (trimmed.isEmpty) return '';
    final uri = Uri.tryParse(trimmed);
    if (uri == null || !uri.hasScheme) {
      return trimmed.endsWith('/')
          ? trimmed.substring(0, trimmed.length - 1)
          : trimmed;
    }
    final normalizedPath = uri.path == '/' || uri.path.isEmpty
        ? ''
        : uri.path.replaceFirst(RegExp(r'/+$'), '');
    return uri
        .replace(
          scheme: uri.scheme.toLowerCase(),
          host: uri.host.toLowerCase(),
          path: normalizedPath,
          query: null,
          fragment: null,
        )
        .toString();
  }

  static PanelCapabilityState stateFor(
    PanelCapability capability, {
    String? scope,
  }) {
    final entry = _profiles[_scopeKey(scope)]?[capability];
    if (entry == null || entry.isExpiredAt(_now())) {
      return PanelCapabilityState.unknown;
    }
    return entry.state;
  }

  static bool shouldProbe(
    PanelCapability capability, {
    String? scope,
  }) => stateFor(capability, scope: scope) == PanelCapabilityState.unknown;

  static bool isUnsupported(
    PanelCapability capability, {
    String? scope,
  }) =>
      stateFor(capability, scope: scope) == PanelCapabilityState.unsupported;

  static bool isUnavailable(
    PanelCapability capability, {
    String? scope,
  }) {
    final state = stateFor(capability, scope: scope);
    return state == PanelCapabilityState.disabled ||
        state == PanelCapabilityState.temporaryFailure;
  }

  static PanelProfile? profileFor({String? scope}) =>
      _panelProfiles[_scopeKey(scope)];

  static void recordManagedLocalStatus(LocalPanelStatus status) {
    final scope = normalizeServerUrl(status.baseUrl);
    if (scope.isEmpty) return;
    final capabilities = <PanelCapability, PanelCapabilityStatus>{};
    for (final entry in status.platformCapabilities.entries) {
      final capability = PanelCapability.fromId(entry.key);
      if (capability == null) continue;
      capabilities[capability] = entry.value;
      _record(scope, capability, entry.value.state, _supportedTtl);
    }
    _panelProfiles[scope] = PanelProfile(
      instanceId: status.instanceId,
      instanceMode: 'managed_local',
      serverVersion: status.coreVersion,
      schemaVersion: status.schemaVersion,
      capabilities: capabilities,
      source: PanelProfileSource.managedLocal,
    );
  }

  static void recordSupported(
    PanelCapability capability, {
    String? scope,
  }) {
    _record(
      _scopeKey(scope),
      capability,
      PanelCapabilityState.supported,
      _supportedTtl,
    );
  }

  static PanelCapabilityState recordFailure(
    PanelCapability capability,
    Object error, {
    String? scope,
  }) {
    final state = classifyFailure(error);
    _record(
      _scopeKey(scope),
      capability,
      state,
      state == PanelCapabilityState.unsupported
          ? _unsupportedTtl
          : _temporaryFailureTtl,
    );
    return state;
  }

  static PanelCapabilityState classifyFailure(Object error) {
    if (error is DioException) {
      final status = error.response?.statusCode;
      if (status == 404 || status == 405) {
        return PanelCapabilityState.unsupported;
      }
    }
    return PanelCapabilityState.temporaryFailure;
  }

  static bool recordPlatformCapabilityFailure(
    DioException error, {
    String? scope,
  }) {
    final data = error.response?.data;
    if (error.response?.statusCode != 409 || data is! Map) return false;
    if (data['errorCode']?.toString() != 'PLATFORM_CAPABILITY') return false;
    final capability = PanelCapability.fromId(
      data['capability']?.toString() ?? '',
    );
    if (capability == null) return false;
    final parsedState = parsePanelCapabilityState(data['state']);
    final state = parsedState == PanelCapabilityState.unknown
        ? PanelCapabilityState.disabled
        : parsedState;
    final requestScope = error.requestOptions.baseUrl.trim();
    final scopeKey = _scopeKey(
      scope ?? (requestScope.isEmpty ? currentScope : requestScope),
    );
    final current = _panelProfiles[scopeKey];
    _record(scopeKey, capability, state, _unsupportedTtl);
    final status = PanelCapabilityStatus(
      state: state,
      reasonCode: data['reasonCode']?.toString() ?? '',
    );
    _panelProfiles[scopeKey] = PanelProfile(
      instanceId: current?.instanceId ?? '',
      instanceMode: current?.instanceMode ?? '',
      serverVersion: current?.serverVersion ?? '',
      apiVersion: current?.apiVersion ?? '',
      schemaVersion: current?.schemaVersion ?? 0,
      capabilityRevision: current?.capabilityRevision ?? '',
      capabilities: {...?current?.capabilities, capability: status},
      source: PanelProfileSource.platformCapability,
    );
    error.response?.data = {
      ...Map<Object?, Object?>.from(data),
      'message': platformCapabilityMessage(capability, status),
    };
    return true;
  }

  static String platformCapabilityMessage(
    PanelCapability capability,
    PanelCapabilityStatus status,
  ) {
    const labels = <PanelCapability, String>{
      PanelCapability.taskExecution: '任务执行',
      PanelCapability.scriptExecution: '脚本执行',
      PanelCapability.dependencyMutation: '依赖变更',
      PanelCapability.subscriptionPull: '订阅拉取',
      PanelCapability.systemUpdate: '系统更新',
      PanelCapability.systemRestart: '系统重启',
      PanelCapability.backupMutation: '备份变更',
      PanelCapability.runtimeMutation: '运行时变更',
      PanelCapability.notificationDispatch: '通知发送',
    };
    final label = labels[capability] ?? '当前操作';
    final reason = status.reasonCode.trim();
    return reason.isEmpty ? '$label 当前不可用' : '$label 当前不可用（$reason）';
  }

  static void clearScope(String serverUrl) {
    final scope = normalizeServerUrl(serverUrl);
    _profiles.remove(scope);
    _panelProfiles.remove(scope);
  }

  static void reset() {
    _profiles.clear();
    _panelProfiles.clear();
    setCurrentScope('');
    _now = DateTime.now;
  }

  static void setClockForTesting(DateTime Function() clock) {
    _now = clock;
  }

  static void _record(
    String scope,
    PanelCapability capability,
    PanelCapabilityState state,
    Duration ttl,
  ) {
    if (scope.isEmpty) return;
    final checkedAt = _now();
    (_profiles[scope] ??= {})[capability] = PanelCapabilityEntry(
      state: state,
      checkedAt: checkedAt,
      expiresAt: checkedAt.add(ttl),
    );
    final current = _panelProfiles[scope];
    _panelProfiles[scope] = PanelProfile(
      instanceId: current?.instanceId ?? '',
      instanceMode: current?.instanceMode ?? '',
      serverVersion: current?.serverVersion ?? '',
      apiVersion: current?.apiVersion ?? '',
      schemaVersion: current?.schemaVersion ?? 0,
      capabilityRevision: current?.capabilityRevision ?? '',
      capabilities: {
        ...?current?.capabilities,
        capability: PanelCapabilityStatus(state: state),
      },
      source: current?.source ?? PanelProfileSource.endpointProbe,
    );
  }

  static String _scopeKey(String? scope) =>
      normalizeServerUrl(scope ?? currentScope);
}
