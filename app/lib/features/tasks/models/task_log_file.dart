class TaskLogFile {
  final String filename;
  final String path;
  final int size;
  final DateTime? createdAt;
  final int? logId;
  final String? contractError;

  const TaskLogFile({
    required this.filename,
    required this.path,
    required this.size,
    required this.createdAt,
    required this.logId,
    required this.contractError,
  });

  factory TaskLogFile.fromJson(Map<String, dynamic> json) {
    final filename = json['filename']?.toString().trim() ?? '';
    final path = json['path']?.toString().trim() ?? '';
    final logId = _positiveInt(json['log_id']);
    final missing = <String>[
      if (filename.isEmpty) 'filename',
      if (path.isEmpty) 'path',
      if (logId == null) 'log_id',
    ];
    return TaskLogFile(
      filename: filename,
      path: path,
      size: _int(json['size']),
      createdAt: DateTime.tryParse(json['created_at']?.toString() ?? ''),
      logId: logId,
      contractError: missing.isEmpty ? null : '日志契约错误：缺少 ${missing.join(', ')}',
    );
  }
}

int _int(dynamic value) => value is num
    ? value.toInt()
    : int.tryParse(value?.toString() ?? '') ?? 0;

int? _positiveInt(dynamic value) {
  final parsed = _int(value);
  return parsed > 0 ? parsed : null;
}
