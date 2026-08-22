import 'package:daidai_app/features/dashboard/providers/dashboard_provider.dart';
import 'package:daidai_app/shared/models/cron_template.dart';
import 'package:daidai_app/shared/models/task.dart';
import 'package:daidai_app/shared/models/task_log.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final timestamp = DateTime.utc(2026);

  Task taskFrom(Map<String, dynamic> values) => Task.fromJson({
    'id': 1,
    'name': 'task',
    'command': 'run',
    'cron_expression': '* * * * *',
    'created_at': timestamp.toIso8601String(),
    'updated_at': timestamp.toIso8601String(),
    ...values,
  });

  group('Task contract', () {
    test('parses and serializes exit codes and abort notifications', () {
      final task = taskFrom({
        'success_exit_codes': '0,2',
        'notify_on_abort': true,
      });

      expect(task.successExitCodes, [0, 2]);
      expect(task.notifyOnAbort, isTrue);
      expect(task.toJson()['success_exit_codes'], '0,2');
      expect(task.toJson()['notify_on_abort'], isTrue);
    });

    test('parses success exit code form values', () {
      expect(Task.parseSuccessExitCodes('0, 2，2 255'), [0, 2, 255]);
      expect(Task.parseSuccessExitCodes('0,'), [0]);
      expect(Task.parseSuccessExitCodes('success'), isNull);
      expect(Task.parseSuccessExitCodes('-1'), isNull);
      expect(Task.parseSuccessExitCodes('256'), isNull);
    });

    test('formats aborted and unknown values without losing raw values', () {
      expect(taskFrom({'last_run_status': 2}).lastRunStatusText, '已终止');
      expect(taskFrom({'status': 7}).statusText, '未知状态（7）');
      expect(taskFrom({'task_type': 'event'}).taskTypeText, '未知类型（event）');
      expect(taskFrom({'last_run_status': 9}).lastRunStatusText, '未知结果（9）');
    });

    test('preserves ordered duplicate Cron expressions through serialization', () {
      final task = taskFrom({
        'cron_expression': 'legacy',
        'cron_expressions': ['0 0 * * *', '0 30 * * *', '0 0 * * *'],
      });

      expect(task.effectiveCronExpressions, [
        '0 0 * * *',
        '0 30 * * *',
        '0 0 * * *',
      ]);
      expect(task.toJson()['cron_expression'], '0 0 * * *\n0 30 * * *\n0 0 * * *');
      expect(task.toJson()['cron_expressions'], [
        '0 0 * * *',
        '0 30 * * *',
        '0 0 * * *',
      ]);
    });

    test('normalizes multiline Cron form input without deduplication', () {
      expect(Task.cronPayload('  a  \n\n b\r\na\n'), {
        'cron_expression': 'a\nb\na',
        'cron_expressions': ['a', 'b', 'a'],
      });
      expect(taskFrom({}).effectiveCronExpressions, ['* * * * *']);
    });
  });

  group('Cron template contract', () {
    test('parses grouped templates and compatible aliases', () {
      final groups = parseCronTemplateGroups({
        '基础': [
          {'label': '每小时', 'cron': '0 0 * * * *'},
        ],
        '高级': [
          {'title': '工作日', 'cron_expression': '0 0 9 * * 1-5'},
        ],
      });

      expect(groups.map((group) => group.name), ['基础', '高级']);
      expect(groups[0].templates.single.name, '每小时');
      expect(groups[0].templates.single.expression, '0 0 * * * *');
      expect(groups[1].templates.single.expression, '0 0 9 * * 1-5');
    });

    test('parses named group containers and flat group fields', () {
      final groups = parseCronTemplateGroups({
        'groups': [
          {
            'name': '推荐',
            'items': [
              {'name': '每天', 'value': '0 0 0 * * *'},
            ],
          },
          {'name': '每周', 'expression': '0 0 0 * * 0', 'category': '周期'},
        ],
      });

      expect(groups.map((group) => group.name), ['推荐', '周期']);
      expect(groups[0].templates.single.expression, '0 0 0 * * *');
    });

    test('formats explicit and fallback Panel timezone context', () {
      expect(
        panelTimezoneLabel({
          'timezone': {'value': 'Asia/Shanghai'},
        }),
        '执行时区：Asia/Shanghai',
      );
      expect(
        panelTimezoneLabel([
          {'key': 'time_zone', 'default_value': 'UTC'},
        ]),
        '执行时区：UTC',
      );
      expect(panelTimezoneLabel({}), '执行时区：由面板设置决定');
    });
  });

  group('TaskLog contract', () {
    TaskLog log({int? status, double? duration}) => TaskLog(
      id: 1,
      taskId: 1,
      status: status,
      duration: duration,
      startedAt: timestamp,
      createdAt: timestamp,
    );

    test('maps aborted and unknown statuses', () {
      expect(log(status: 3).isAborted, isTrue);
      expect(log(status: 3).statusText, '已终止');
      expect(log(status: 8).statusText, '未知状态（8）');
    });

    test('formats durations of at least one hour', () {
      expect(log(duration: 7325).durationText, '2h2m5s');
    });
  });

  test('DashboardData accepts aborted and abort log count keys', () {
    expect(
      const DashboardData(dashboard: {'aborted_logs': '4'}).todayAborted,
      4,
    );
    expect(
      const DashboardData(dashboard: {'abort_logs': 3}).todayAborted,
      3,
    );
    expect(const DashboardData(dashboard: {'aborted': 2}).todayAborted, 2);
    expect(const DashboardData(dashboard: {'abort': 1}).todayAborted, 1);
  });
}
