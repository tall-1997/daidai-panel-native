import 'package:dio/dio.dart';

import 'dio_client.dart';

enum PanelCapabilityState {
  unknown,
  supported,
  unsupported,
  temporaryFailure,
}

enum PanelCapability {
  taskViews,
  panelSettings,
  systemVersion,
  pythonRuntimes,
  healthCheck,
  platformTokens,
  configScript,
  androidRuntime,
  installedPackages,
}

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
  static final Map<String, Map<PanelCapability, PanelCapabilityEntry>>
  _profiles = {};

  static String get currentScope => normalizeServerUrl(
    DioClient.instance.baseUrl,
  );

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

  static void clearScope(String serverUrl) {
    _profiles.remove(normalizeServerUrl(serverUrl));
  }

  static void reset() {
    _profiles.clear();
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
  }

  static String _scopeKey(String? scope) =>
      normalizeServerUrl(scope ?? currentScope);
}
