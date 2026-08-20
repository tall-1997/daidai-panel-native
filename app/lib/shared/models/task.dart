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
  final List<int> successExitCodes;
  final bool notifyOnFailure;
  final bool notifyOnSuccess;
  final bool notifyOnAbort;
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
    this.successExitCodes = const [0],
    this.notifyOnFailure = false,
    this.notifyOnSuccess = false,
    this.notifyOnAbort = false,
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
    if (isDisabled) return '已禁用';
    return '未知状态（${_numberText(status)}）';
  }

  String get taskTypeText {
    switch (taskType) {
      case 'cron':
        return '常规定时';
      case 'manual':
        return '手动运行';
      case 'startup':
        return '开机运行';
      default:
        return '未知类型（$taskType）';
    }
  }

  String get lastRunStatusText {
    switch (lastRunStatus) {
      case null:
        return '未运行';
      case 0:
        return '成功';
      case 1:
        return '失败';
      case 2:
        return '已终止';
      default:
        return '未知结果（$lastRunStatus）';
    }
  }

  List<String> get labelList => labels.isEmpty
      ? []
      : labels.split(',').where((l) => l.isNotEmpty).toList();

  List<String> get labelsForDisplay =>
      displayLabels.isNotEmpty ? displayLabels : labelList;

  List<String> get effectiveCronExpressions => cronExpressions.isNotEmpty
      ? List.unmodifiable(cronExpressions)
      : cronExpression.trim().isEmpty
      ? const []
      : [cronExpression.trim()];

  static List<String> parseCronInput(String value) => value
      .split(RegExp(r'\r?\n'))
      .map((expression) => expression.trim())
      .where((expression) => expression.isNotEmpty)
      .toList();

  static Map<String, dynamic> cronPayload(String value) {
    final expressions = parseCronInput(value);
    return {
      'cron_expression': expressions.isEmpty ? '' : expressions.first,
      'cron_expressions': expressions,
    };
  }

  static bool isGroupLabel(String label) =>
      label.trim().startsWith(groupLabelPrefix);

  static String toGroupLabel(String group) =>
      '$groupLabelPrefix${group.trim()}';

  static List<int>? parseSuccessExitCodes(String value) {
    final parts = value
        .split(RegExp(r'[,，\s]+'))
        .where((part) => part.isNotEmpty);
    final result = <int>[];
    final seen = <int>{};
    for (final part in parts) {
      final code = int.tryParse(part);
      if (code == null || code < 0 || code > 255) return null;
      if (seen.add(code)) result.add(code);
    }
    return result.isEmpty ? null : result;
  }

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
                .map((e) => e.toString().trim())
                .where((s) => s.isNotEmpty)
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
      successExitCodes: _intList(json['success_exit_codes'], fallback: const [0]),
      notifyOnFailure: json['notify_on_failure'] == true,
      notifyOnSuccess: json['notify_on_success'] == true,
      notifyOnAbort: json['notify_on_abort'] == true,
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
    'cron_expression': effectiveCronExpressions.isEmpty
        ? ''
        : effectiveCronExpressions.first,
    'cron_expressions': effectiveCronExpressions,
    'task_type': taskType,
    'python_version': pythonVersion,
    'labels': labels,
    'timeout': timeout,
    'random_delay_seconds': randomDelaySeconds,
    'max_retries': maxRetries,
    'retry_interval': retryInterval,
    'success_exit_codes': successExitCodes.join(','),
    'notify_on_failure': notifyOnFailure,
    'notify_on_success': notifyOnSuccess,
    'notify_on_abort': notifyOnAbort,
    'notification_channel_id': notificationChannelId,
    'depends_on': dependsOn,
    'sort_order': sortOrder,
    'task_before': taskBefore,
    'task_after': taskAfter,
    'allow_multiple_instances': allowMultipleInstances,
  };
}

int _int(dynamic v) => _intOrNull(v) ?? 0;
int? _intOrNull(dynamic v) => v is num
    ? v.toInt()
    : int.tryParse(v?.toString().trim() ?? '');
double _double(dynamic v) => _doubleOrNull(v) ?? 0.0;
double? _doubleOrNull(dynamic v) => v is num
    ? v.toDouble()
    : double.tryParse(v?.toString().trim() ?? '');
List<int> _intList(dynamic value, {List<int> fallback = const []}) {
  if (value is String) {
    return Task.parseSuccessExitCodes(value) ?? fallback;
  }
  if (value is! List) return fallback;
  final parsed = Task.parseSuccessExitCodes(value.join(','));
  return parsed ?? fallback;
}
String _numberText(num value) => value == value.toInt()
    ? value.toInt().toString()
    : value.toString();
DateTime? _date(dynamic v) {
  if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
  return null;
}
