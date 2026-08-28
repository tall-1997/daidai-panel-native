class SseField {
  final String name;
  final String value;

  const SseField(this.name, this.value);
}

class SseDecodedEvent {
  final String? event;
  final String data;
  final String? id;
  final bool hasExplicitId;
  final String? lastEventId;

  const SseDecodedEvent({
    required this.event,
    required this.data,
    required this.id,
    required this.hasExplicitId,
    required this.lastEventId,
  });
}

class SseProtocolException implements Exception {
  final String message;

  const SseProtocolException(this.message);

  @override
  String toString() => 'SseProtocolException: $message';
}

class SseDecoder {
  SseDecoder({
    String? lastEventId,
    this.maxBufferCharacters = 64 * 1024,
    this.maxEventCharacters = 256 * 1024,
  }) : _lastEventId = lastEventId;

  final int maxBufferCharacters;
  final int maxEventCharacters;

  String _buffer = '';
  String? _event;
  String? _explicitId;
  String? _lastEventId;
  final List<String> _dataLines = [];
  int _eventCharacters = 0;
  int _frameCharacters = 0;

  int? retryMilliseconds;
  String? get lastEventId => _lastEventId;

  List<SseDecodedEvent> add(String chunk) {
    _buffer += chunk;
    if (_buffer.length > maxBufferCharacters && !_buffer.contains('\n')) {
      throw const SseProtocolException('SSE line buffer exceeded its limit');
    }
    final lines = _buffer.split('\n');
    _buffer = lines.removeLast();
    if (_buffer.length > maxBufferCharacters) {
      throw const SseProtocolException('SSE line buffer exceeded its limit');
    }
    return _processLines(lines);
  }

  List<SseDecodedEvent> close() {
    final lines = <String>[];
    if (_buffer.isNotEmpty) lines.add(_buffer);
    _buffer = '';
    final events = _processLines(lines);
    final pending = _dispatch();
    if (pending != null) events.add(pending);
    return events;
  }

  List<SseDecodedEvent> _processLines(List<String> lines) {
    final events = <SseDecodedEvent>[];
    for (final rawLine in lines) {
      if (rawLine.length > maxBufferCharacters) {
        throw const SseProtocolException('SSE line exceeded its limit');
      }
      _frameCharacters += rawLine.length;
      if (_frameCharacters > maxEventCharacters) {
        throw const SseProtocolException('SSE frame exceeded its limit');
      }
      final line = rawLine.endsWith('\r')
          ? rawLine.substring(0, rawLine.length - 1)
          : rawLine;
      if (line.isEmpty) {
        final event = _dispatch();
        if (event != null) events.add(event);
        continue;
      }

      final field = parseSseField(line);
      switch (field?.name) {
        case 'event':
          _event = field!.value;
          break;
        case 'data':
          _eventCharacters += field!.value.length;
          if (_eventCharacters > maxEventCharacters) {
            throw const SseProtocolException('SSE event exceeded its limit');
          }
          _dataLines.add(field.value);
          break;
        case 'id':
          if (!field!.value.contains('\u0000')) {
            _explicitId = field.value;
            _lastEventId = field.value;
          }
          break;
        case 'retry':
          if (RegExp(r'^\d+$').hasMatch(field!.value)) {
            retryMilliseconds = int.tryParse(field.value);
          }
          break;
        default:
          break;
      }
    }
    return events;
  }

  SseDecodedEvent? _dispatch() {
    if (_dataLines.isEmpty) {
      _event = null;
      _explicitId = null;
      _eventCharacters = 0;
      _frameCharacters = 0;
      return null;
    }
    final event = SseDecodedEvent(
      event: _event,
      data: _dataLines.join('\n'),
      id: _explicitId,
      hasExplicitId: _explicitId != null,
      lastEventId: _lastEventId,
    );
    _event = null;
    _explicitId = null;
    _dataLines.clear();
    _eventCharacters = 0;
    _frameCharacters = 0;
    return event;
  }
}

class SseEventIdCache {
  SseEventIdCache({this.capacity = 256}) : assert(capacity > 0);

  final int capacity;
  final Set<String> _ids = {};
  final List<String> _order = [];

  int get length => _ids.length;

  bool add(String id) {
    if (!_ids.add(id)) return false;
    _order.add(id);
    if (_order.length > capacity) {
      _ids.remove(_order.removeAt(0));
    }
    return true;
  }

  void clear() {
    _ids.clear();
    _order.clear();
  }
}

SseField? parseSseField(String rawLine) {
  final line = rawLine.endsWith('\r')
      ? rawLine.substring(0, rawLine.length - 1)
      : rawLine;
  if (line.isEmpty || line.startsWith(':')) return null;

  final separator = line.indexOf(':');
  if (separator < 0) return SseField(line, '');

  var value = line.substring(separator + 1);
  if (value.startsWith(' ')) value = value.substring(1);
  return SseField(line.substring(0, separator), value);
}

bool isTerminalSseEvent(String? event, String data) {
  return event == 'done' && data.trim() != 'reconnect';
}

bool isReconnectSseEvent(String? event, String data) {
  return event == 'done' && data.trim() == 'reconnect';
}

Duration sseReconnectDelay({
  required int attempt,
  required Duration baseDelay,
  Duration maxDelay = const Duration(seconds: 30),
}) {
  final exponent = attempt < 0 ? 0 : (attempt > 30 ? 30 : attempt);
  final milliseconds = baseDelay.inMilliseconds * (1 << exponent);
  return Duration(
    milliseconds: milliseconds.clamp(0, maxDelay.inMilliseconds).toInt(),
  );
}

Duration boundedSseRetryDelay(
  int milliseconds, {
  Duration minDelay = const Duration(milliseconds: 500),
  Duration maxDelay = const Duration(seconds: 30),
}) => Duration(
  milliseconds: milliseconds
      .clamp(minDelay.inMilliseconds, maxDelay.inMilliseconds)
      .toInt(),
);

Map<String, String> buildSseHeaders({
  String? token,
  String? lastEventId,
}) {
  return {
    'Accept': 'text/event-stream',
    'Cache-Control': 'no-cache',
    if (token != null) 'Authorization': 'Bearer $token',
    if (lastEventId != null && lastEventId.isNotEmpty)
      'Last-Event-ID': lastEventId,
  };
}
