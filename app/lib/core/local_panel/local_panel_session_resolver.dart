import '../storage/secure_storage.dart';
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
  final endpoint = Uri.tryParse(status.baseUrl);
  if (status.phase != LocalPanelPhase.ready ||
      status.localToken.isEmpty ||
      endpoint == null ||
      endpoint.scheme != 'http' ||
      endpoint.host != '127.0.0.1' ||
      !endpoint.hasPort ||
      (endpoint.path.isNotEmpty && endpoint.path != '/') ||
      endpoint.hasQuery ||
      endpoint.hasFragment) {
    throw StateError('Managed local panel is unavailable');
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
