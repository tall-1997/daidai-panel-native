class ScriptRunLogData {
  final List<String> logs;
  final bool done;
  final String status;
  final int? exitCode;
  final String? error;
  final int logCount;

  const ScriptRunLogData({
    required this.logs,
    required this.done,
    required this.status,
    required this.exitCode,
    required this.error,
    required this.logCount,
  });

  factory ScriptRunLogData.fromMap(Map<dynamic, dynamic> data) {
    final rawLogs = data['logs'];
    final logs = rawLogs is List
        ? rawLogs.map((item) => item.toString()).toList(growable: false)
        : _splitContent(data['content']?.toString());
    final rawExitCode = data['exit_code'];
    return ScriptRunLogData(
      logs: logs,
      done: data['done'] == true,
      status: data['status']?.toString() ?? '',
      exitCode: rawExitCode is num
          ? rawExitCode.toInt()
          : int.tryParse(rawExitCode?.toString() ?? ''),
      error: _nonBlank(data['error']?.toString()),
      logCount: (data['log_count'] as num?)?.toInt() ?? logs.length,
    );
  }

  String get statusText {
    if (!done) return '运行中...';
    if (status == 'success' || exitCode == 0) return '执行成功';
    if (status == 'stopped') return '已停止${_exitSuffix()}';
    final detail = error == null ? '' : '：$error';
    return '执行失败${_exitSuffix()}$detail';
  }

  String _exitSuffix() => exitCode == null ? '' : '（exit code $exitCode）';

  static List<String> _splitContent(String? content) {
    if (content == null || content.isEmpty) return const [];
    return content.split(RegExp(r'\r?\n'));
  }

  static String? _nonBlank(String? value) {
    final trimmed = value?.trim();
    return trimmed == null || trimmed.isEmpty ? null : trimmed;
  }
}
