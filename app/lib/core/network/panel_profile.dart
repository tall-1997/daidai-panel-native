enum PanelCapabilityState {
  unknown,
  supported,
  unsupported,
  disabled,
  temporaryFailure,
}

enum PanelCapability {
  taskViews('task_views'),
  panelSettings('panel_settings'),
  systemVersion('system_version'),
  pythonRuntimes('python_runtimes'),
  healthCheck('health_check'),
  platformTokens('platform_tokens'),
  configScript('config_script'),
  androidRuntime('android_runtime'),
  installedPackages('installed_packages'),
  taskExecution('task_execution'),
  scriptExecution('script_execution'),
  dependencyMutation('dependency_mutation'),
  subscriptionPull('subscription_pull'),
  systemUpdate('system_update'),
  systemRestart('system_restart'),
  backupMutation('backup_mutation'),
  runtimeMutation('runtime_mutation'),
  notificationDispatch('notification_dispatch');

  const PanelCapability(this.id);

  final String id;

  static PanelCapability? fromId(String id) {
    for (final capability in values) {
      if (capability.id == id) return capability;
    }
    return null;
  }
}

enum PanelProfileSource { managedLocal, endpointProbe, platformCapability }

class PanelCapabilityStatus {
  final PanelCapabilityState state;
  final String reasonCode;
  final String adapterId;

  const PanelCapabilityStatus({
    this.state = PanelCapabilityState.unknown,
    this.reasonCode = '',
    this.adapterId = '',
  });
}

class PanelProfile {
  final String instanceId;
  final String instanceMode;
  final String serverVersion;
  final String apiVersion;
  final int schemaVersion;
  final String capabilityRevision;
  final Map<PanelCapability, PanelCapabilityStatus> capabilities;
  final PanelProfileSource source;

  const PanelProfile({
    this.instanceId = '',
    this.instanceMode = '',
    this.serverVersion = '',
    this.apiVersion = '',
    this.schemaVersion = 0,
    this.capabilityRevision = '',
    this.capabilities = const {},
    this.source = PanelProfileSource.endpointProbe,
  });

  PanelCapabilityStatus statusFor(PanelCapability capability) =>
      capabilities[capability] ?? const PanelCapabilityStatus();
}

PanelCapabilityState parsePanelCapabilityState(Object? value) {
  switch (value?.toString().trim().toLowerCase()) {
    case 'enabled':
    case 'supported':
      return PanelCapabilityState.supported;
    case 'unsupported':
      return PanelCapabilityState.unsupported;
    case 'disabled':
      return PanelCapabilityState.disabled;
    case 'temporary_unavailable':
    case 'temporaryunavailable':
    case 'temporary_failure':
      return PanelCapabilityState.temporaryFailure;
    default:
      return PanelCapabilityState.unknown;
  }
}
