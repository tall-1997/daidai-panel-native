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

  group('loginChallengePayload', () {
    test('reads a nested two-factor challenge', () {
      final payload = loginChallengePayload({
        'error': '请输入两步验证码',
        'data': {'two_factor_required': true, 'code': 'two_factor_required'},
      });
      expect(payload, isNotNull);
      expect(payload!['two_factor_required'], isTrue);
      expect(payload['code'], 'two_factor_required');
    });

    test('ignores ordinary 4xx payloads', () {
      expect(loginChallengePayload({'error': '用户名或密码错误'}), isNull);
    });
  });
}
