import 'package:daidai_app/features/tasks/models/live_log_snapshot.dart';
import 'package:daidai_app/features/tasks/models/task_log_file.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('live snapshot preserves incremental cursor and queued tracking', () {
    final queued = LiveLogSnapshot.fromMap({
      'logs': <String>[],
      'content': '',
      'done': false,
      'status': 0.5,
      'cursor': 12,
      'log_id': 8,
    });
    expect(queued.shouldTrack, isTrue);
    expect(queued.isRunning, isFalse);
    expect(queued.cursor, 12);
    expect(queued.logId, 8);

    final terminal = LiveLogSnapshot.fromMap({
      'content': 'final line\n',
      'done': true,
      'status': 0,
      'cursor': 23,
      'log_id': 9,
    });
    expect(terminal.logs, ['final line']);
    expect(terminal.shouldTrack, isFalse);
  });

  test('task log file requires direct log id contract', () {
    final valid = TaskLogFile.fromJson({
      'filename': 'run.log',
      'path': 'task_1/run.log',
      'log_id': 7,
      'size': 12,
      'created_at': '2026-08-21T10:00:00Z',
    });
    expect(valid.logId, 7);
    expect(valid.contractError, isNull);

    final invalid = TaskLogFile.fromJson({'name': 'legacy.log', 'id': 3});
    expect(invalid.logId, isNull);
    expect(invalid.contractError, contains('filename'));
    expect(invalid.contractError, contains('path'));
    expect(invalid.contractError, contains('log_id'));
  });
}
