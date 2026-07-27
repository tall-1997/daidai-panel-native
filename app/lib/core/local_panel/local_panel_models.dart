enum LocalPanelPhase { stopped, starting, migrating, ready, degraded, failed }

class LocalPanelStatus {
  final LocalPanelPhase phase;
  final String baseUrl;
  final String instanceId;
  final String coreVersion;
  final int schemaVersion;
  final String failureStage;
  final String message;
  final bool foregroundServiceEnabled;
  final String localToken;

  const LocalPanelStatus({
    this.phase = LocalPanelPhase.stopped,
    this.baseUrl = '',
    this.instanceId = '',
    this.coreVersion = '',
    this.schemaVersion = 0,
    this.failureStage = '',
    this.message = '',
    this.foregroundServiceEnabled = false,
    this.localToken = '',
  });

  factory LocalPanelStatus.fromJson(Map<String, dynamic> json) {
    return LocalPanelStatus(
      phase: LocalPanelPhase.values.firstWhere(
        (phase) => phase.name == json['phase']?.toString(),
        orElse: () => LocalPanelPhase.failed,
      ),
      baseUrl: json['base_url']?.toString() ?? '',
      instanceId: json['instance_id']?.toString() ?? '',
      coreVersion: json['core_version']?.toString() ?? '',
      schemaVersion: _int(json['schema_version']),
      failureStage: json['failure_stage']?.toString() ?? '',
      message: json['message']?.toString() ?? '',
      foregroundServiceEnabled:
          json['foreground_service_enabled'] == true,
      localToken: json['local_token']?.toString() ?? '',
    );
  }
}

class LocalPanelCapabilities {
  final String instanceMode;
  final String platform;
  final String architecture;
  final String coreVersion;
  final int schemaVersion;
  final Map<String, bool> capabilities;
  final Map<String, dynamic> limits;

  const LocalPanelCapabilities({
    this.instanceMode = '',
    this.platform = '',
    this.architecture = '',
    this.coreVersion = '',
    this.schemaVersion = 0,
    this.capabilities = const {},
    this.limits = const {},
  });

  bool supports(String capability) => capabilities[capability] == true;

  factory LocalPanelCapabilities.fromJson(Map<String, dynamic> json) {
    final rawCapabilities = json['capabilities'];
    final rawLimits = json['limits'];
    return LocalPanelCapabilities(
      instanceMode: json['instance_mode']?.toString() ?? '',
      platform: json['platform']?.toString() ?? '',
      architecture: json['architecture']?.toString() ?? '',
      coreVersion: json['core_version']?.toString() ?? '',
      schemaVersion: _int(json['schema_version']),
      capabilities: rawCapabilities is Map
          ? rawCapabilities.map(
              (key, value) => MapEntry(key.toString(), value == true),
            )
          : const {},
      limits: rawLimits is Map
          ? Map<String, dynamic>.from(rawLimits)
          : const {},
    );
  }
}

enum RuntimeComponentState {
  unavailable,
  available,
  downloading,
  installing,
  installed,
  failed,
}

class RuntimeComponent {
  final String id;
  final String kind;
  final String version;
  final String architecture;
  final RuntimeComponentState state;
  final int downloadSize;
  final int installSize;
  final String sha256;
  final bool signatureVerified;
  final List<String> capabilities;
  final String nativePackageSupport;
  final int referencedByTasks;
  final String message;

  const RuntimeComponent({
    required this.id,
    required this.kind,
    required this.version,
    required this.architecture,
    this.state = RuntimeComponentState.unavailable,
    this.downloadSize = 0,
    this.installSize = 0,
    this.sha256 = '',
    this.signatureVerified = false,
    this.capabilities = const [],
    this.nativePackageSupport = 'unsupported',
    this.referencedByTasks = 0,
    this.message = '',
  });

  factory RuntimeComponent.fromJson(Map<String, dynamic> json) {
    final packageSupport = json['package_support'];
    return RuntimeComponent(
      id: json['id']?.toString() ?? '',
      kind: json['kind']?.toString() ?? '',
      version: json['version']?.toString() ?? '',
      architecture: json['architecture']?.toString() ?? '',
      state: RuntimeComponentState.values.firstWhere(
        (state) => state.name == json['state']?.toString(),
        orElse: () => RuntimeComponentState.unavailable,
      ),
      downloadSize: _int(json['download_size']),
      installSize: _int(json['install_size']),
      sha256: json['sha256']?.toString() ?? '',
      signatureVerified: json['signature_verified'] == true,
      capabilities: json['capabilities'] is List
          ? (json['capabilities'] as List).map((item) => '$item').toList()
          : const [],
      nativePackageSupport: packageSupport is Map
          ? packageSupport['native']?.toString() ?? 'unsupported'
          : 'unsupported',
      referencedByTasks: _int(json['referenced_by_tasks']),
      message: json['message']?.toString() ?? '',
    );
  }
}

int _int(dynamic value) {
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? 0;
}
