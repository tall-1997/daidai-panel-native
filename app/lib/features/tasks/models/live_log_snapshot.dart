class LiveLogSnapshot {
  final List<String> logs;
  final bool done;
  final double? status;
  final int cursor;
  final int? logId;

  const LiveLogSnapshot({
    required this.logs,
    required this.done,
    required this.status,
    required this.cursor,
    required this.logId,
  });

  factory LiveLogSnapshot.fromMap(Map<dynamic, dynamic> data) {
    final rawLogs = data['logs'];
    final content = data['content']?.toString() ?? '';
    final logs = rawLogs is List
        ? rawLogs
              .map((item) => item.toString())
              .where((line) => line.isNotEmpty)
              .toList(growable: false)
        : content
              .replaceAll('\r\n', '\n')
              .split('\n')
              .where((line) => line.isNotEmpty)
              .toList(growable: false);
    return LiveLogSnapshot(
      logs: logs,
      done: data['done'] == true,
      status: _doubleOrNull(data['status']),
      cursor: _intOrNull(data['cursor']) ?? 0,
      logId: _intOrNull(data['log_id']),
    );
  }

  bool get isRunning => !done && status == 2;
  bool get shouldTrack => !done;
}

double? _doubleOrNull(dynamic value) => value is num
    ? value.toDouble()
    : double.tryParse(value?.toString() ?? '');

int? _intOrNull(dynamic value) => value is num
    ? value.toInt()
    : int.tryParse(value?.toString() ?? '');
