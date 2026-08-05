import 'package:daidai_app/features/scripts/models/script_run_log_data.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('keeps raw log lines and parses failure status', () {
    final data = ScriptRunLogData.fromMap({
      'logs': ['\u001b[31m错误 原文\u001b[0m', '', 'ValueError: 坏值'],
      'done': true,
      'status': 'failed',
      'exit_code': 2,
      'error': 'ValueError: 坏值',
      'log_count': 3,
    });

    expect(data.logs, ['\u001b[31m错误 原文\u001b[0m', '', 'ValueError: 坏值']);
    expect(data.logCount, 3);
    expect(data.statusText, '执行失败（exit code 2）：ValueError: 坏值');
  });

  test('falls back to content only when logs is missing', () {
    final data = ScriptRunLogData.fromMap({
      'content': '第一行\n\n第三行',
      'done': false,
      'status': 'running',
    });

    expect(data.logs, ['第一行', '', '第三行']);
    expect(data.statusText, '运行中...');
  });
}
