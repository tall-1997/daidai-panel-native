import 'package:daidai_app/shared/utils/bounded_log_buffer.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'log update batcher coalesces entries for 32ms and flushes terminal updates',
    (tester) async {
      final flushed = <List<String>>[];
      final batcher = LogUpdateBatcher<String>(onFlush: flushed.add);

      batcher.addAll(['a', 'b']);
      await tester.pump(const Duration(milliseconds: 31));
      expect(flushed, isEmpty);
      await tester.pump(const Duration(milliseconds: 1));
      expect(flushed, [
        ['a', 'b'],
      ]);

      batcher.add('terminal');
      batcher.flush();
      expect(flushed.last, ['terminal']);
      batcher.dispose();
    },
  );
  group('bounded log entries', () {
    test('retains the newest lines within the line limit', () {
      final entries = <String>['one'];

      appendBoundedLogEntries(entries, ['two', 'three'], maxLines: 2);

      expect(entries, ['two', 'three']);
    });

    test('retains the newest complete lines within the character limit', () {
      final entries = <String>[];

      replaceBoundedLogEntries(
        entries,
        ['1234', '567', '89'],
        maxCharacters: 5,
      );

      expect(entries, ['567', '89']);
    });

    test('retains the tail of a single oversized line', () {
      final entries = <String>[];

      appendBoundedLogEntries(entries, ['123456'], maxCharacters: 4);

      expect(entries, ['3456']);
    });

    test('replace clears stale entries and applies both limits', () {
      final entries = <String>['stale'];

      replaceBoundedLogEntries(
        entries,
        ['a', 'bb', 'ccc'],
        maxLines: 2,
        maxCharacters: 4,
      );

      expect(entries, ['ccc']);
    });

    test('empty input leaves an empty buffer', () {
      final entries = <String>[];

      appendBoundedLogEntries(entries, const []);

      expect(entries, isEmpty);
    });

    test('splits multiline events before applying the line limit', () {
      final entries = <String>[];

      appendBoundedLogEntries(entries, ['one\ntwo\nthree'], maxLines: 2);

      expect(entries, ['two', 'three']);
    });

    test('rejects non-positive limits', () {
      expect(
        () => appendBoundedLogEntries(<String>[], ['line'], maxLines: 0),
        throwsArgumentError,
      );
      expect(
        () => appendBoundedLogEntries(
          <String>[],
          ['line'],
          maxCharacters: -1,
        ),
        throwsArgumentError,
      );
    });

    test('does not split a surrogate pair when truncating', () {
      final entries = <String>[];

      appendBoundedLogEntries(entries, ['a😀b'], maxCharacters: 2);

      expect(entries, ['b']);
    });
  });

  group('log replay cursor', () {
    test('drops the replayed prefix without shifting the history list', () {
      final cursor = LogReplayCursor()..reset(['one', 'two', 'three']);

      expect(cursor.consume(['one', 'two']), isEmpty);
      expect(cursor.consume(['three', 'four']), ['four']);
    });

    test('keeps entries after the replay diverges', () {
      final cursor = LogReplayCursor()..reset(['one', 'two']);

      expect(cursor.consume(['one', 'different', 'three']), [
        'different',
        'three',
      ]);
    });

    test('passes entries through after replay completes', () {
      final cursor = LogReplayCursor()..reset(['one']);

      expect(cursor.consume(['one']), isEmpty);
      expect(cursor.consume(['two']), ['two']);
    });

    test('finds retained tail within a full history replay', () {
      final cursor = LogReplayCursor()
        ..reset(['three', 'four'], seekFullReplay: true);

      expect(cursor.consume(['one', 'two', 'three']), isEmpty);
      expect(cursor.consume(['four', 'five']), ['five']);
    });

    test('handles overlapping prefixes in a full history replay', () {
      final cursor = LogReplayCursor()
        ..reset(['a', 'a', 'b'], seekFullReplay: true);

      expect(cursor.consume(['a', 'a', 'a', 'b', 'new']), ['new']);
    });
  });
}
