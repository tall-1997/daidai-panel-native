import 'package:daidai_app/core/services/raw_log_download_service.dart';
import 'package:daidai_app/shared/models/raw_log_ticket.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('parses a direct raw log ticket response', () {
    final ticket = RawLogTicket.fromResponse({
      'url': '/api/logs/12/raw?ticket=test',
      'filename': 'task-12-raw.log',
      'size': 2048,
      'expires_at': '2026-08-08T12:00:00Z',
      'expires_in': 120,
    });
    expect(ticket.url, contains('/api/logs/12/raw'));
    expect(ticket.filename, 'task-12-raw.log');
    expect(ticket.size, 2048);
    expect(ticket.expiresIn, 120);
    expect(ticket.expiresAt, isNotNull);
  });

  test('parses a wrapped ticket and numeric strings', () {
    final ticket = RawLogTicket.fromResponse({
      'data': {
        'url': '/raw?ticket=test',
        'filename': 'raw.log',
        'size': '42',
        'expires_in': '60',
      },
    });
    expect(ticket.size, 42);
    expect(ticket.expiresIn, 60);
  });

  test('rejects an incomplete ticket', () {
    expect(
      () => RawLogTicket.fromResponse({'url': '/raw'}),
      throwsFormatException,
    );
  });

  test('uses the earliest absolute or relative expiration', () {
    final receivedAt = DateTime.utc(2026, 8, 8, 12);
    final ticket = RawLogTicket.fromResponse(
      {
        'url': '/raw?ticket=test',
        'filename': 'raw.log',
        'expires_at': '2026-08-08T12:02:00Z',
        'expires_in': 60,
      },
      receivedAt: receivedAt,
    );

    expect(ticket.expirationTime, receivedAt.add(const Duration(minutes: 1)));
    expect(
      ticket.isExpired(at: receivedAt.add(const Duration(seconds: 59))),
      isFalse,
    );
    expect(
      ticket.isExpired(at: receivedAt.add(const Duration(minutes: 1))),
      isTrue,
    );
  });

  test('supports a safety window when checking expiration', () {
    final receivedAt = DateTime.utc(2026, 8, 8, 12);
    final ticket = RawLogTicket.fromResponse(
      {
        'url': '/raw?ticket=test',
        'filename': 'raw.log',
        'expires_in': 30,
      },
      receivedAt: receivedAt,
    );

    expect(
      ticket.isExpired(
        at: receivedAt.add(const Duration(seconds: 26)),
        safetyWindow: const Duration(seconds: 5),
      ),
      isTrue,
    );
  });

  test('treats an explicit zero expires_in as expired', () {
    final receivedAt = DateTime.utc(2026, 8, 8, 12);
    final ticket = RawLogTicket.fromResponse(
      {
        'url': '/raw?ticket=test',
        'filename': 'raw.log',
        'expires_in': 0,
      },
      receivedAt: receivedAt,
    );

    expect(ticket.isExpired(at: receivedAt), isTrue);
  });

  test('refreshes only ticket-related download statuses', () {
    expect([401, 403, 404, 410].every(shouldRefreshRawLogTicket), isTrue);
    expect([null, 400, 408, 429, 500].any(shouldRefreshRawLogTicket), isFalse);
  });
}
