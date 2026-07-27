class Task {
  static const String groupLabelPrefix = '分组:';

  final int id;
  final String name;
  final String command;
  final String cronExpression;
  final List<String> cronExpressions;
  final String taskType;
  final String pythonVersion;
  final double status;
  final String labels;
  final List<String> displayLabels;
  final DateTime? lastRunAt;
  final DateTime? nextRunAt;
  final int? lastRunStatus;
  final int timeout;
  final int? randomDelaySeconds;
  final int maxRetries;
  final int retryInterval;
  final bool notifyOnFailure;
  final bool notifyOnSuccess;
  final int? notificationChannelId;
  final int? dependsOn;
  final int sortOrder;
  final bool isPinned;
  final String? taskBefore;
  final String? taskAfter;
  final bool allowMultipleInstances;
  final String? notificationChannelName;
  final bool? notificationChannelEnabled;
  final double? lastRunningTime;
  final DateTime createdAt;
  final DateTime updatedAt;

  const Task({
    required this.id,
    required this.name,
    required this.command,
    required this.cronExpression,
    this.cronExpressions = const [],
    this.taskType = 'cron',
    this.pythonVersion = '3.12',
    required this.status,
    this.labels = '',
    this.displayLabels = const [],
    this.lastRunAt,
    this.nextRunAt,
    this.lastRunStatus,
    this.timeout = 0,
    this.randomDelaySeconds,
    this.maxRetries = 0,
    this.retryInterval = 0,
    this.notifyOnFailure = false,
    this.notifyOnSuccess = false,
    this.notificationChannelId,
    this.dependsOn,
    this.sortOrder = 0,
    this.isPinned = false,
    this.taskBefore,
    this.taskAfter,
    this.allowMultipleInstances = false,
    this.notificationChannelName,
    this.notificationChannelEnabled,
    this.lastRunningTime,
    required this.createdAt,
    required this.updatedAt,
  });

  bool get isDisabled => status == 0;
  bool get isQueued => status == 0.5;
  bool get isEnabled => status == 1;
  bool get isRunning => status == 2;

  String get statusText {
    if (isRunning) return '运行中';
    if (isQueued) return '排队中';
    if (isEnabled) return '已启用';
    return '已禁用';
  }

  List<String> get labelList => labels.isEmpty
      ? []
      : labels.split(',').where((l) => l.isNotEmpty).toList();

  List<String> get labelsForDisplay =>
      displayLabels.isNotEmpty ? displayLabels : labelList;

  static bool isGroupLabel(String label) =>
      label.trim().startsWith(groupLabelPrefix);

  static String toGroupLabel(String group) =>
      '$groupLabelPrefix${group.trim()}';

  String? get groupName {
    for (final label in labelList) {
      final trimmed = label.trim();
      if (isGroupLabel(trimmed)) {
        final group = trimmed.substring(groupLabelPrefix.length).trim();
        if (group.isNotEmpty) {
          return group;
        }
      }
    }
    return null;
  }

  List<String> get userLabelsForDisplay {
    final visible = labelsForDisplay
        .where((label) => !isGroupLabel(label))
        .toList();
    final group = groupName;
    if (group != null && group.isNotEmpty) {
      visible.remove(group);
    }
    return visible;
  }

  factory Task.fromJson(Map<String, dynamic> json) {
    return Task(
      id: _int(json['id']),
      name: json['name']?.toString() ?? '',
      command: json['command']?.toString() ?? '',
      cronExpression: json['cron_expression']?.toString() ?? '',
      cronExpressions: json['cron_expressions'] is List
          ? (json['cron_expressions'] as List)
                .map((e) => e.toString())
                .where((s) => s.trim().isNotEmpty)
                .toList()
          : const [],
      taskType: json['task_type']?.toString() ?? 'cron',
      pythonVersion: json['python_version']?.toString() ?? '3.12',
      status: _double(json['status']),
      labels: json['labels'] is List
          ? (json['labels'] as List).join(',')
          : json['labels']?.toString() ?? '',
      displayLabels: json['display_labels'] is List
          ? (json['display_labels'] as List)
                .map((e) => e.toString())
                .where((label) => label.trim().isNotEmpty)
                .toList()
          : const [],
      lastRunAt: _date(json['last_run_at']),
      nextRunAt: _date(json['next_run_at']),
      lastRunStatus: _intOrNull(json['last_run_status']),
      timeout: _int(json['timeout']),
      randomDelaySeconds: _intOrNull(json['random_delay_seconds']),
      maxRetries: _int(json['max_retries']),
      retryInterval: _int(json['retry_interval']),
      notifyOnFailure: json['notify_on_failure'] == true,
      notifyOnSuccess: json['notify_on_success'] == true,
      notificationChannelId: _intOrNull(json['notification_channel_id']),
      dependsOn: _intOrNull(json['depends_on']),
      sortOrder: _int(json['sort_order']),
      isPinned: json['is_pinned'] == true,
      taskBefore: json['task_before']?.toString(),
      taskAfter: json['task_after']?.toString(),
      allowMultipleInstances: json['allow_multiple_instances'] == true,
      notificationChannelName: json['notification_channel_name']?.toString(),
      notificationChannelEnabled: json['notification_channel_enabled'] as bool?,
      lastRunningTime: _doubleOrNull(json['last_running_time']),
      createdAt: _date(json['created_at']) ?? DateTime.now(),
      updatedAt: _date(json['updated_at']) ?? DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'command': command,
    'cron_expression': cronExpression,
    'task_type': taskType,
    'python_version': pythonVersion,
    'labels': labels,
    'timeout': timeout,
    'random_delay_seconds': randomDelaySeconds,
    'max_retries': maxRetries,
    'retry_interval': retryInterval,
    'notify_on_failure': notifyOnFailure,
    'notify_on_success': notifyOnSuccess,
    'notification_channel_id': notificationChannelId,
    'depends_on': dependsOn,
    'sort_order': sortOrder,
    'task_before': taskBefore,
    'task_after': taskAfter,
    'allow_multiple_instances': allowMultipleInstances,
  };
}

int _int(dynamic v) => (v is num) ? v.toInt() : 0;
int? _intOrNull(dynamic v) => (v is num) ? v.toInt() : null;
double _double(dynamic v) => (v is num) ? v.toDouble() : 0.0;
double? _doubleOrNull(dynamic v) => (v is num) ? v.toDouble() : null;
DateTime? _date(dynamic v) {
  if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
  return null;
}
