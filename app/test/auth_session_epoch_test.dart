import 'package:daidai_app/core/auth/auth_session_epoch.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('advance invalidates captured session epochs', () {
    final captured = AuthSessionEpoch.current;

    expect(AuthSessionEpoch.isCurrent(captured), isTrue);
    expect(AuthSessionEpoch.advance(), captured + 1);
    expect(AuthSessionEpoch.isCurrent(captured), isFalse);
  });
}
