import '../storage/secure_storage.dart';
import '../network/panel_capability_registry.dart';
import 'local_panel_models.dart';

class ManagedLocalPanelResolution {
  final PanelConfig panel;
  final String localToken;

  const ManagedLocalPanelResolution({
    required this.panel,
    required this.localToken,
  });
}

ManagedLocalPanelResolution resolveManagedLocalPanel(
  LocalPanelStatus status, {
  PanelConfig? existing,
}) {
  PanelCapabilityRegistry.recordManagedLocalStatus(status);
  return _resolveManagedLocalEndpoint(
    status,
    existing: existing,
    allowedPhases: const {LocalPanelPhase.ready},
  );
}

ManagedLocalPanelResolution resolveManagedLocalDiagnostic(
  LocalPanelStatus status, {
  PanelConfig? existing,
}) {
  PanelCapabilityRegistry.recordManagedLocalStatus(status);
  if (status.fallbackMode != 'diagnostic') {
    throw StateError('Managed local diagnostic endpoint is unavailable');
  }
  return _resolveManagedLocalEndpoint(
    status,
    existing: existing,
    allowedPhases: const {LocalPanelPhase.degraded},
  );
}

ManagedLocalPanelResolution _resolveManagedLocalEndpoint(
  LocalPanelStatus status, {
  required Set<LocalPanelPhase> allowedPhases,
  PanelConfig? existing,
}) {
  final endpoint = Uri.tryParse(status.baseUrl);
  if (!allowedPhases.contains(status.phase) ||
      status.localToken.isEmpty ||
      endpoint == null ||
      endpoint.scheme != 'http' ||
      endpoint.host != '127.0.0.1' ||
      !endpoint.hasPort ||
      (endpoint.path.isNotEmpty && endpoint.path != '/') ||
      endpoint.hasQuery ||
      endpoint.hasFragment) {
    throw StateError('Managed local endpoint is unavailable');
  }
  return ManagedLocalPanelResolution(
    panel: PanelConfig(
      url: status.baseUrl,
      name: existing?.name.isNotEmpty == true ? existing!.name : '本地面板',
      username: existing?.username,
      password: existing?.password,
      rememberPassword: existing?.rememberPassword ?? false,
      autoLogin: existing?.autoLogin ?? false,
      type: PanelType.managedLocal,
      instanceId: status.instanceId,
    ),
    localToken: status.localToken,
  );
}
