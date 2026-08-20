import 'package:daidai_app/core/auth/auth_service.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('AuthService server health check', () {
    test('rejects an HTTPS URL without a host immediately', () async {
      final result = await AuthService().checkHealthDetails('https:///panel');

      expect(result.reachable, isFalse);
      expect(result.errorMessage, '服务器地址格式无效');
    });

    test('rejects unsupported URL schemes immediately', () async {
      final result = await AuthService().checkHealthDetails('ftp://example.com');

      expect(result.reachable, isFalse);
      expect(result.errorMessage, '服务器地址格式无效');
    });
  });
}
