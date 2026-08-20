import 'package:daidai_app/core/network/sse_protocol.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('parseSseField', () {
    test('parses fields with and without an optional space', () {
      expect(parseSseField('data:value')?.value, 'value');
      expect(parseSseField('data: value')?.value, 'value');
      expect(parseSseField('event:done\r')?.value, 'done');
    });

    test('ignores empty lines and comments', () {
      expect(parseSseField(''), isNull);
      expect(parseSseField(': keep-alive'), isNull);
    });

    test('preserves additional colons in values', () {
      final field = parseSseField('data: https://example.test:5700');
      expect(field?.name, 'data');
      expect(field?.value, 'https://example.test:5700');
    });
  });

  group('SSE completion decisions', () {
    test('recognizes reconnect events', () {
      expect(isReconnectSseEvent('done', 'reconnect'), isTrue);
      expect(isTerminalSseEvent('done', 'reconnect'), isFalse);
    });

    test('recognizes business terminal events', () {
      for (final value in [
        'finished',
        'installed',
        'failed',
        'not_running',
        'closed',
        'timeout',
      ]) {
        expect(isTerminalSseEvent('done', value), isTrue);
        expect(isReconnectSseEvent('done', value), isFalse);
      }
    });

    test('keeps ordinary events open', () {
      expect(isTerminalSseEvent('message', 'finished'), isFalse);
      expect(isReconnectSseEvent(null, 'reconnect'), isFalse);
    });
  });

  group('SseDecoder', () {
    test('keeps state across chunks and exposes event ID metadata', () {
      final decoder = SseDecoder();

      expect(decoder.add('id: event-1\nevent: mes'), isEmpty);
      final events = decoder.add('sage\ndata: first\ndata: second\n\n');

      expect(events, hasLength(1));
      expect(events.single.event, 'message');
      expect(events.single.data, 'first\nsecond');
      expect(events.single.id, 'event-1');
      expect(events.single.hasExplicitId, isTrue);
      expect(events.single.lastEventId, 'event-1');
    });

    test('inherits the last event ID without marking it explicit', () {
      final decoder = SseDecoder(lastEventId: 'previous');
      final event = decoder.add('data: next\n\n').single;

      expect(event.id, isNull);
      expect(event.hasExplicitId, isFalse);
      expect(event.lastEventId, 'previous');
    });

    test('supports CRLF and an empty ID clears continuation', () {
      final decoder = SseDecoder(lastEventId: 'previous');
      final event = decoder.add('id:\r\ndata: next\r\n\r\n').single;

      expect(event.id, '');
      expect(event.hasExplicitId, isTrue);
      expect(event.lastEventId, '');
    });

    test('uses valid retry values and ignores invalid values', () {
      final decoder = SseDecoder();

      decoder.add('retry: 1500\nretry: invalid\ndata: value\n\n');

      expect(decoder.retryMilliseconds, 1500);
    });

    test('flushes a final event without a trailing blank line', () {
      final decoder = SseDecoder();
      decoder.add('event: done\ndata: finished');

      final event = decoder.close().single;
      expect(isTerminalSseEvent(event.event, event.data), isTrue);
    });
  });

  group('SSE reconnection', () {
    test('builds a resume header only for a non-empty event ID', () {
      expect(
        buildSseHeaders(lastEventId: 'event-7')['Last-Event-ID'],
        'event-7',
      );
      expect(buildSseHeaders(lastEventId: ''), isNot(contains('Last-Event-ID')));
    });

    test('applies bounded exponential backoff', () {
      const base = Duration(milliseconds: 500);

      expect(sseReconnectDelay(attempt: 0, baseDelay: base), base);
      expect(
        sseReconnectDelay(attempt: 3, baseDelay: base),
        const Duration(seconds: 4),
      );
      expect(
        sseReconnectDelay(attempt: 20, baseDelay: base),
        const Duration(seconds: 30),
      );
    });

    test('bounds explicit event ID duplicate suppression', () {
      final ids = SseEventIdCache(capacity: 2);

      expect(ids.add('one'), isTrue);
      expect(ids.add('one'), isFalse);
      expect(ids.add('two'), isTrue);
      expect(ids.add('three'), isTrue);
      expect(ids.length, 2);
      expect(ids.add('one'), isTrue);
    });
  });
}
