import 'package:daidai_app/shared/models/subscription.dart';
import 'package:daidai_app/features/subscriptions/views/subscription_list_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('Subscription token authentication', () {
    test('parses token fields', () {
      final subscription = Subscription.fromJson({
        'id': 1,
        'name': 'private repository',
        'auth_type': 'token',
        'auth_username': 'git-user',
        'auth_token': 'secret-token',
        'has_auth_token': true,
        'created_at': '2026-08-08T00:00:00Z',
        'updated_at': '2026-08-08T00:00:00Z',
      });

      expect(subscription.authType, 'token');
      expect(subscription.authUsername, 'git-user');
      expect(subscription.authToken, 'secret-token');
      expect(subscription.hasAuthToken, isTrue);
    });

    test('serializes token fields', () {
      final subscription = Subscription(
        id: 1,
        name: 'private repository',
        authType: 'token',
        authUsername: 'git-user',
        authToken: 'secret-token',
        hasAuthToken: true,
        createdAt: DateTime.utc(2026, 8, 8),
        updatedAt: DateTime.utc(2026, 8, 8),
      );
      final json = subscription.toJson();

      expect(json, containsPair('auth_type', 'token'));
      expect(json, containsPair('auth_username', 'git-user'));
      expect(json, containsPair('auth_token', 'secret-token'));
      expect(json.containsKey('has_auth_token'), isFalse);
      expect(json['ssh_key_id'], isNull);
    });

    test('omits token values outside token authentication', () {
      final subscription = Subscription(
        id: 1,
        name: 'public repository',
        authUsername: 'stale-user',
        authToken: 'stale-token',
        sshKeyId: 7,
        createdAt: DateTime.utc(2026, 8, 8),
        updatedAt: DateTime.utc(2026, 8, 8),
      );
      final json = subscription.toJson();

      expect(json['ssh_key_id'], isNull);
      expect(json['auth_username'], isEmpty);
      expect(json.containsKey('auth_token'), isFalse);
    });

    test('validates SSH and Token authentication requirements', () {
      expect(
        validateSubscriptionAuth(
          subscriptionType: 'git-repo',
          authType: 'ssh',
          sshKeyId: null,
          authToken: '',
        ),
        isNotNull,
      );
      expect(
        validateSubscriptionAuth(
          subscriptionType: 'git-repo',
          authType: 'token',
          sshKeyId: null,
          authToken: '',
          hasExistingToken: true,
        ),
        isNull,
      );
      expect(
        validateSubscriptionAuth(
          subscriptionType: 'single-file',
          authType: 'token',
          sshKeyId: null,
          authToken: '',
        ),
        isNull,
      );
    });

    test('infers legacy authentication from stored credential metadata', () {
      final sshSubscription = Subscription.fromJson({
        'id': 1,
        'name': 'legacy ssh',
        'ssh_key_id': 7,
        'created_at': '2026-08-08T00:00:00Z',
        'updated_at': '2026-08-08T00:00:00Z',
      });
      final tokenSubscription = Subscription.fromJson({
        'id': 2,
        'name': 'legacy token',
        'has_auth_token': true,
        'created_at': '2026-08-08T00:00:00Z',
        'updated_at': '2026-08-08T00:00:00Z',
      });

      expect(sshSubscription.authType, 'ssh');
      expect(sshSubscription.toJson()['ssh_key_id'], 7);
      expect(tokenSubscription.authType, 'token');
    });
  });
}
