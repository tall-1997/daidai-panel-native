class Dependency {
  final int id;
  final String name;
  final String version;
  final String type;
  final String pythonVersion;
  final String status;
  final String? remark;
  final String? log;
  final String operationId;
  final int? exitCode;
  final String errorCode;
  final Map<String, dynamic> compatibilityDetails;
  final DateTime createdAt;
  final DateTime updatedAt;

  const Dependency({
    required this.id,
    required this.name,
    this.version = '',
    this.type = 'nodejs',
    this.pythonVersion = '',
    this.status = 'installed',
    this.remark,
    this.log,
    this.operationId = '',
    this.exitCode,
    this.errorCode = '',
    this.compatibilityDetails = const {},
    required this.createdAt,
    required this.updatedAt,
  });

  bool get isQueued => status == 'queued';
  bool get isInstalling => status == 'installing';
  bool get isRemoving => status == 'removing';
  bool get isInstalled => status == 'installed';
  bool get isFailed => const {
    'failed',
    'unsupported',
    'unavailable',
    'blocked',
  }.contains(status);
  bool get isCancelled => status == 'cancelled' || status == 'canceled';
  bool get isBusy => isInstalling || isRemoving || isQueued;

  String get statusText {
    switch (status) {
      case 'queued':
        return '排队中';
      case 'installing':
        return '安装中';
      case 'removing':
        return '卸载中';
      case 'failed':
        return '失败';
      case 'unsupported':
        return '不支持';
      case 'unavailable':
        return '不可用';
      case 'blocked':
        return '已阻止';
      case 'cancelled':
      case 'canceled':
        return '已取消';
      case 'installed':
        return '已安装';
      default:
        return '状态未知';
    }
  }

  factory Dependency.fromJson(Map<String, dynamic> json) {
    return Dependency(
      id: _int(json['id']),
      name: json['name']?.toString() ?? '',
      version: json['version']?.toString() ?? '',
      type: json['type']?.toString() ?? 'nodejs',
      pythonVersion: json['python_version']?.toString() ?? '',
      status: json['status']?.toString() ?? 'installed',
      remark: json['remark']?.toString(),
      log: json['log']?.toString(),
      operationId: json['operation_id']?.toString() ?? '',
      exitCode: _nullableInt(json['exit_code']),
      errorCode: json['error_code']?.toString() ?? '',
      compatibilityDetails: _map(json['compatibility_details']),
      createdAt: _date(json['created_at']) ?? DateTime.now(),
      updatedAt: _date(json['updated_at']) ?? DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'version': version,
    'type': type,
    'python_version': pythonVersion,
    'remark': remark,
    'status': status,
    'log': log,
    'operation_id': operationId,
    'exit_code': exitCode,
    'error_code': errorCode,
    'compatibility_details': compatibilityDetails,
  };
}

int _int(dynamic value) => _nullableInt(value) ?? 0;

int? _nullableInt(dynamic value) {
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}

Map<String, dynamic> _map(dynamic value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  return const {};
}

DateTime? _date(dynamic value) {
  if (value is String && value.isNotEmpty) {
    return DateTime.tryParse(value);
  }
  return null;
}
