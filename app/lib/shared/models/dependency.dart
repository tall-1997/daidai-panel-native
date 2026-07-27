class Dependency {
  final int id;
  final String name;
  final String version;
  final String type;
  final String pythonVersion;
  final String status;
  final String? remark;
  final String? log;
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
    required this.createdAt,
    required this.updatedAt,
  });

  bool get isQueued => status == 'queued';
  bool get isInstalling => status == 'installing';
  bool get isRemoving => status == 'removing';
  bool get isInstalled => status == 'installed';
  bool get isFailed => status == 'failed';
  bool get isCancelled => status == 'cancelled';
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
      case 'cancelled':
        return '已取消';
      default:
        return '已安装';
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
  };
}

int _int(dynamic value) => (value is num) ? value.toInt() : 0;

DateTime? _date(dynamic value) {
  if (value is String && value.isNotEmpty) {
    return DateTime.tryParse(value);
  }
  return null;
}
