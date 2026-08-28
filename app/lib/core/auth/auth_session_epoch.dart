class AuthSessionEpoch {
  AuthSessionEpoch._();

  static int _current = 0;
  static final Set<void Function()> _listeners = {};

  static int get current => _current;

  static bool isCurrent(int epoch) => epoch == _current;

  static int advance() {
    _current++;
    for (final listener in List<void Function()>.of(_listeners)) {
      listener();
    }
    return _current;
  }

  static void addListener(void Function() listener) => _listeners.add(listener);

  static void removeListener(void Function() listener) =>
      _listeners.remove(listener);

  static String scoped(String panelScope) => '$panelScope#auth=$_current';
}
