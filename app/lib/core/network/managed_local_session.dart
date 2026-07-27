class ManagedLocalSession {
  Uri? _origin;
  String? _token;

  Uri? get origin => _origin;

  void set(String baseUrl, String token) {
    final uri = Uri.tryParse(baseUrl);
    if (uri == null ||
        uri.scheme != 'http' ||
        uri.host != '127.0.0.1' ||
        !uri.hasPort ||
        (uri.path.isNotEmpty && uri.path != '/') ||
        uri.hasQuery ||
        uri.hasFragment ||
        token.isEmpty) {
      throw ArgumentError('Managed local session requires a loopback origin');
    }
    _origin = uri;
    _token = token;
  }

  void clear() {
    _origin = null;
    _token = null;
  }

  Map<String, String> headersFor(Uri requestUri) {
    final origin = _origin;
    final token = _token;
    if (origin == null || token == null || !_sameOrigin(origin, requestUri)) {
      return const {};
    }
    return {'X-Daidai-Local-Token': token, 'Origin': _originText(origin)};
  }

  bool _sameOrigin(Uri expected, Uri actual) =>
      expected.scheme == actual.scheme &&
      expected.host == actual.host &&
      expected.port == actual.port;

  String _originText(Uri uri) => '${uri.scheme}://${uri.host}:${uri.port}';
}

final defaultManagedLocalSession = ManagedLocalSession();
