const int defaultMaxLogLines = 1000;
const int defaultMaxLogCharacters = 200000;

class LogReplayCursor {
  final List<String> _entries = [];
  final List<int> _prefixLengths = [];
  int _index = 0;
  bool _seekFullReplay = false;

  void reset(Iterable<String> entries, {bool seekFullReplay = false}) {
    _entries
      ..clear()
      ..addAll(entries);
    _prefixLengths
      ..clear()
      ..addAll(_buildPrefixLengths(_entries));
    _index = 0;
    _seekFullReplay = seekFullReplay && _entries.isNotEmpty;
  }

  List<String> consume(List<String> incomingEntries) {
    if (_index >= _entries.length) {
      reset(const []);
      return incomingEntries;
    }

    final result = <String>[];
    for (final entry in incomingEntries) {
      if (_seekFullReplay) {
        while (_index > 0 && entry != _entries[_index]) {
          _index = _prefixLengths[_index - 1];
        }
        if (entry == _entries[_index]) {
          _index++;
          if (_index == _entries.length) reset(const []);
        }
        continue;
      }
      if (_index < _entries.length && entry == _entries[_index]) {
        _index++;
        if (_index == _entries.length) reset(const []);
        continue;
      }

      reset(const []);
      result.add(entry);
    }
    return result;
  }
}

List<int> _buildPrefixLengths(List<String> entries) {
  final prefixLengths = List<int>.filled(entries.length, 0);
  var prefixLength = 0;
  for (var index = 1; index < entries.length; index++) {
    while (prefixLength > 0 && entries[index] != entries[prefixLength]) {
      prefixLength = prefixLengths[prefixLength - 1];
    }
    if (entries[index] == entries[prefixLength]) prefixLength++;
    prefixLengths[index] = prefixLength;
  }
  return prefixLengths;
}

void replaceBoundedLogEntries(
  List<String> target,
  Iterable<String> entries, {
  int maxLines = defaultMaxLogLines,
  int maxCharacters = defaultMaxLogCharacters,
}) {
  _validateLimits(maxLines, maxCharacters);
  target
    ..clear()
    ..addAll(_normalizeEntries(entries));
  trimBoundedLogEntries(
    target,
    maxLines: maxLines,
    maxCharacters: maxCharacters,
  );
}

void appendBoundedLogEntries(
  List<String> target,
  Iterable<String> entries, {
  int maxLines = defaultMaxLogLines,
  int maxCharacters = defaultMaxLogCharacters,
}) {
  _validateLimits(maxLines, maxCharacters);
  target.addAll(_normalizeEntries(entries));
  trimBoundedLogEntries(
    target,
    maxLines: maxLines,
    maxCharacters: maxCharacters,
  );
}

void trimBoundedLogEntries(
  List<String> entries, {
  int maxLines = defaultMaxLogLines,
  int maxCharacters = defaultMaxLogCharacters,
}) {
  _validateLimits(maxLines, maxCharacters);
  if (entries.isEmpty) return;

  final lastIndex = entries.length - 1;
  if (entries[lastIndex].length > maxCharacters) {
    var start = entries[lastIndex].length - maxCharacters;
    if (start > 0 && _isLowSurrogate(entries[lastIndex].codeUnitAt(start))) {
      start++;
    }
    entries[lastIndex] = entries[lastIndex].substring(start);
  }

  var retainedCharacters = 0;
  var firstRetainedIndex = entries.length;
  for (var index = entries.length - 1; index >= 0; index--) {
    final nextCharacters = retainedCharacters + entries[index].length;
    final nextLineCount = entries.length - index;
    if (nextLineCount > maxLines || nextCharacters > maxCharacters) break;
    retainedCharacters = nextCharacters;
    firstRetainedIndex = index;
  }

  if (firstRetainedIndex > 0) {
    entries.removeRange(0, firstRetainedIndex);
  }
}

Iterable<String> _normalizeEntries(Iterable<String> entries) sync* {
  for (final entry in entries) {
    yield* entry.replaceAll('\r\n', '\n').replaceAll('\r', '\n').split('\n');
  }
}

void _validateLimits(int maxLines, int maxCharacters) {
  if (maxLines <= 0) {
    throw ArgumentError.value(maxLines, 'maxLines', 'must be positive');
  }
  if (maxCharacters <= 0) {
    throw ArgumentError.value(
      maxCharacters,
      'maxCharacters',
      'must be positive',
    );
  }
}

bool _isLowSurrogate(int codeUnit) => codeUnit >= 0xDC00 && codeUnit <= 0xDFFF;
