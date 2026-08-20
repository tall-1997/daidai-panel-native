class Subscription {
  final int id;
  final String name;
  final String type;
  final String url;
  final String branch;
  final String? subPath;
  final String schedule;
  final String whitelist;
  final String blacklist;
  final bool autoAddTask;
  final bool autoDelTask;
  final bool enabled;
  final double status;
  final DateTime? lastPullAt;
  final String saveDir;
  final int? sshKeyId;
  final String authType;
  final String authUsername;
  final String authToken;
  final bool hasAuthToken;
  final String alias;
  final String dependOn;
  final String hookScript;
  final bool? forceOverwrite;
  final DateTime createdAt;
  final DateTime updatedAt;

  const Subscription({
    required this.id,
    required this.name,
    this.type = 'public-repo',
    this.url = '',
    this.branch = '',
    this.subPath,
    this.schedule = '',
    this.whitelist = '',
    this.blacklist = '',
    this.autoAddTask = false,
    this.autoDelTask = false,
    this.enabled = true,
    this.status = 0,
    this.lastPullAt,
    this.saveDir = '',
    this.sshKeyId,
    this.authType = '',
    this.authUsername = '',
    this.authToken = '',
    this.hasAuthToken = false,
    this.alias = '',
    this.dependOn = '',
    this.hookScript = '',
    this.forceOverwrite,
    required this.createdAt,
    required this.updatedAt,
  });

  bool get isRunning => status == 2;
  bool get isPulling => status == 2;
  String get normalizedType {
    switch (type) {
      case 'file':
        return 'single-file';
      case 'public-repo':
      case 'private-repo':
        return 'git-repo';
      case '':
        return 'git-repo';
      default:
        return type;
    }
  }

  bool get isSingleFile => normalizedType == 'single-file';
  bool get isGitRepo => normalizedType == 'git-repo';
  String get typeLabel {
    if (isSingleFile) return '单文件';
    if (isGitRepo) return 'Git 仓库';
    return normalizedType;
  }

  String get statusText {
    if (isRunning) return '拉取中';
    if (enabled) return '已启用';
    return '已禁用';
  }

  factory Subscription.fromJson(Map<String, dynamic> json) {
    final sshKeyId = _intOrNull(json['ssh_key_id']);
    final hasAuthToken = json['has_auth_token'] == true;
    final rawAuthType = json['auth_type']?.toString().trim() ?? '';
    final authType = rawAuthType.isNotEmpty
        ? rawAuthType
        : hasAuthToken
        ? 'token'
        : sshKeyId != null
        ? 'ssh'
        : '';
    return Subscription(
      id: _int(json['id']),
      name: json['name']?.toString() ?? '',
      type: json['type']?.toString() ?? 'public-repo',
      url: json['url']?.toString() ?? '',
      branch: json['branch']?.toString() ?? '',
      subPath: json['sub_path']?.toString(),
      schedule: json['schedule']?.toString() ?? '',
      whitelist: json['whitelist']?.toString() ?? '',
      blacklist: json['blacklist']?.toString() ?? '',
      autoAddTask: _boolOrNull(json['auto_add_task']) ?? false,
      autoDelTask: _boolOrNull(json['auto_del_task']) ?? false,
      enabled: _boolOrNull(json['enabled']) ?? true,
      status: _double(json['status']),
      lastPullAt: _date(json['last_pull_at']),
      saveDir: json['save_dir']?.toString() ?? '',
      sshKeyId: sshKeyId,
      authType: authType,
      authUsername: json['auth_username']?.toString() ?? '',
      authToken: json['auth_token']?.toString() ?? '',
      hasAuthToken: hasAuthToken,
      alias: json['alias']?.toString() ?? '',
      dependOn: json['depend_on']?.toString() ?? '',
      hookScript: json['hook_script']?.toString() ?? '',
      forceOverwrite: _boolOrNull(json['force_overwrite']),
      createdAt: _date(json['created_at']) ?? DateTime.now(),
      updatedAt: _date(json['updated_at']) ?? DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'type': normalizedType,
    'url': url,
    'branch': branch,
    'sub_path': subPath ?? '',
    'schedule': schedule,
    'whitelist': whitelist,
    'blacklist': blacklist,
    'auto_add_task': autoAddTask,
    'auto_del_task': autoDelTask,
    'save_dir': saveDir,
    'ssh_key_id': authType == 'ssh' ? sshKeyId : null,
    'auth_type': authType,
    'auth_username': authType == 'token' ? authUsername : '',
    if (authType == 'token' && authToken.isNotEmpty) 'auth_token': authToken,
    'alias': alias,
    'depend_on': dependOn,
    'hook_script': hookScript,
    'force_overwrite': forceOverwrite ?? true,
  };
}

int _int(dynamic v) => _intOrNull(v) ?? 0;
int? _intOrNull(dynamic v) => v is num
    ? v.toInt()
    : int.tryParse(v?.toString().trim() ?? '');
double _double(dynamic v) => v is num
    ? v.toDouble()
    : double.tryParse(v?.toString().trim() ?? '') ?? 0.0;
bool? _boolOrNull(dynamic value) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  if (value is String) {
    final normalized = value.trim().toLowerCase();
    if (normalized == 'true' || normalized == '1') return true;
    if (normalized == 'false' || normalized == '0') return false;
  }
  return null;
}

DateTime? _date(dynamic v) {
  if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
  return null;
}
