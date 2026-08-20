class AuthSessionEpoch {
  AuthSessionEpoch._();

  static int _current = 0;

  static int get current => _current;

  static bool isCurrent(int epoch) => epoch == _current;

  static int advance() => ++_current;

  static String scoped(String panelScope) => '$panelScope#auth=$_current';
}
