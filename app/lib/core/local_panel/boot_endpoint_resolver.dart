import '../storage/secure_storage.dart';
import 'local_panel_models.dart';
import 'local_panel_session_resolver.dart';

enum BootEndpointDestination { dashboard, continueBoot, localRecovery }

class BootEndpointDecision {
  final BootEndpointDestination destination;
  final ManagedLocalPanelResolution? managedLocal;

  const BootEndpointDecision(this.destination, {this.managedLocal});
}

class BootEndpointResolver {
  static Future<BootEndpointDecision> resolve({
    required bool authenticated,
    required PanelConfig? panel,
    required Future<LocalPanelStatus> Function() ensureStarted,
  }) async {
    if (panel?.type != PanelType.managedLocal) {
      return BootEndpointDecision(
        authenticated
            ? BootEndpointDestination.dashboard
            : BootEndpointDestination.continueBoot,
      );
    }
    try {
      final status = await ensureStarted();
      final resolved = resolveManagedLocalPanel(status, existing: panel);
      return BootEndpointDecision(
        authenticated
            ? BootEndpointDestination.dashboard
            : BootEndpointDestination.continueBoot,
        managedLocal: resolved,
      );
    } catch (_) {
      return const BootEndpointDecision(BootEndpointDestination.localRecovery);
    }
  }
}
