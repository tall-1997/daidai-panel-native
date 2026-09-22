String defaultSchemeForServerUrl(String rawUrl) {
  final trimmed = rawUrl.trim().toLowerCase();
  if (trimmed.isEmpty) return 'https';
  if (trimmed.startsWith('https://')) return 'https';
  if (trimmed.startsWith('http://')) return 'http';
  final hostPort = trimmed.split('/').first;
  final host = hostFromHostPort(hostPort);
  if (isLoopbackOrPrivateHost(host)) return 'http';
  return 'https';
}

String hostFromHostPort(String hostPort) {
  final value = hostPort.trim();
  if (value.startsWith('[')) {
    final end = value.indexOf(']');
    if (end > 1) return value.substring(1, end);
  }
  if (value.contains('::') || value.split(':').length > 2) {
    return value.split('%').first;
  }
  return value.split(':').first;
}

bool isLoopbackOrPrivateHost(String host) {
  final value = host.trim().toLowerCase();
  if (value.isEmpty) return false;
  if (value == 'localhost' ||
      value == '127.0.0.1' ||
      value == '0.0.0.0' ||
      value == '::1' ||
      value == '10.0.2.2' ||
      value == '10.0.3.2') {
    return true;
  }
  if (value.contains(':')) {
    return value.startsWith('fe80:') ||
        value.startsWith('fc') ||
        value.startsWith('fd');
  }
  final parts = value.split('.');
  if (parts.length != 4) return false;
  final first = int.tryParse(parts[0]);
  final second = int.tryParse(parts[1]);
  if (first == null || second == null) return false;
  if (first == 10 || first == 127 || (first == 192 && second == 168)) {
    return true;
  }
  if (first == 169 && second == 254) {
    return true;
  }
  if (first == 100 && second >= 64 && second <= 127) {
    return true;
  }
  return first == 172 && second >= 16 && second <= 31;
}
