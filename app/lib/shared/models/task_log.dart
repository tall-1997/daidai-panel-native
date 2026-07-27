class TaskLog {
  final int id;
  final int taskId;
  final String content;
  final int? status; // 0=成功 1=失败 2=运行中
  final double? duration;
  final String? logPath;
  final DateTime startedAt;
  final DateTime? endedAt;
  final DateTime createdAt;
  final String? taskName;

  const TaskLog({
    required this.id,
    required this.taskId,
    this.content = '',
    this.status,
    this.duration,
    this.logPath,
    required this.startedAt,
    this.endedAt,
    required this.createdAt,
    this.taskName,
  });

  bool get isSuccess => status == 0;
  bool get isFailed => status == 1;
  bool get isRunning => status == 2;

  String get statusText {
    switch (status) {
      case 0:
        return '成功';
      case 1:
        return '失败';
      case 2:
        return '运行中';
      default:
        return '未知';
    }
  }

  String get durationText {
    if (duration == null) return '-';
    if (duration! < 1) return '${(duration! * 1000).toStringAsFixed(0)}ms';
    if (duration! < 60) return '${duration!.toStringAsFixed(1)}s';
    final minutes = (duration! / 60).floor();
    final seconds = (duration! % 60).toStringAsFixed(0);
    return '${minutes}m${seconds}s';
  }

  factory TaskLog.fromJson(Map<String, dynamic> json) {
    return TaskLog(
      id: _int(json['id']),
      taskId: _int(json['task_id']),
      content: json['content']?.toString() ?? '',
      status: _intOrNull(json['status']),
      duration: (json['duration'] is num)
          ? (json['duration'] as num).toDouble()
          : null,
      logPath: json['log_path']?.toString(),
      startedAt: _date(json['started_at']) ?? DateTime.now(),
      endedAt: _date(json['ended_at']),
      createdAt: _date(json['created_at']) ?? DateTime.now(),
      taskName: json['task_name']?.toString(),
    );
  }
}

int _int(dynamic v) => (v is num) ? v.toInt() : 0;
int? _intOrNull(dynamic v) => (v is num) ? v.toInt() : null;
DateTime? _date(dynamic v) {
  if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
  return null;
}
