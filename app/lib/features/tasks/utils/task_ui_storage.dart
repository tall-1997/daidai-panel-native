import '../../../core/network/panel_capability_registry.dart';

String taskUiStorageKey(String key, String panelUrl) {
  final scope = PanelCapabilityRegistry.normalizeServerUrl(panelUrl);
  return '$key.${Uri.encodeComponent(scope)}';
}
