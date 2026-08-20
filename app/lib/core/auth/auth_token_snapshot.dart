class AuthTokenSnapshot {
  AuthTokenSnapshot._();

  static String? _accessToken;

  static String? get accessToken => _accessToken;

  static void setAccessToken(String? token) {
    _accessToken = token == null || token.isEmpty ? null : token;
  }

  static void clear() {
    _accessToken = null;
  }
}
