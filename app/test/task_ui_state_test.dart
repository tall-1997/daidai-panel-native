import 'package:daidai_app/features/tasks/providers/task_view_provider.dart';
import 'package:daidai_app/features/tasks/utils/task_ui_storage.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('task UI storage keys', () {
    test('normalizes equivalent panel URLs', () {
      expect(
        taskUiStorageKey('tasks.search', 'HTTPS://Panel.Example.com/root/'),
        taskUiStorageKey('tasks.search', 'https://panel.example.com/root'),
      );
    });

    test('isolates different panels', () {
      expect(
        taskUiStorageKey('tasks.search', 'https://one.example.com'),
        isNot(taskUiStorageKey('tasks.search', 'https://two.example.com')),
      );
    });
  });

  group('task view responses', () {
    final view = {
      'id': 7,
      'name': 'Running',
      'filters': '[]',
      'sort_rules': '[]',
      'hidden': false,
      'sort_order': 1,
    };

    test('parses a direct list response', () {
      final items = parseTaskViewsResponse([view]);

      expect(items, hasLength(1));
      expect(items.single.id, 7);
    });

    test('parses a wrapped data response', () {
      final items = parseTaskViewsResponse({'data': [view]});

      expect(items, hasLength(1));
      expect(items.single.name, 'Running');
    });
  });
}
