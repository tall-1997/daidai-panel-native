import 'package:daidai_app/features/notifications/views/notification_list_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('notification list state retains and clears load errors', () {
    final failed = const NotificationListState().copyWith(error: '加载失败');

    expect(failed.error, '加载失败');
    expect(failed.copyWith(loading: true).error, '加载失败');
    expect(failed.copyWith(clearError: true).error, isNull);
  });

  test('unknown current enum value becomes a selectable current option', () {
    const options = [
      NotificationFieldChoice(value: 'known', label: '已知值'),
    ];

    final choices = notificationFieldChoices(options, 'future-value');

    expect(choices.map((choice) => choice.value), ['future-value', 'known']);
    expect(choices.first.label, 'future-value（当前值）');
    expect(notificationFieldChoices(options, 'known'), same(options));
  });

  group('NotificationTypeOption', () {
    test('parses list fields and generic options', () {
      final option = NotificationTypeOption.fromJson({
        'type': 'service',
        'name': 'Service',
        'fields': [
          {
            'name': 'api_token',
            'label': 'API Token',
            'type': 'password',
            'required': true,
          },
          {
            'key': 'region',
            'options': [
              'cn',
              {'value': 'us', 'label': 'US'},
            ],
          },
        ],
      });

      expect(option.hasFieldsSchema, isTrue);
      expect(option.fields, hasLength(2));
      expect(option.fields.first.key, 'api_token');
      expect(option.fields.first.isCredential, isTrue);
      expect(option.fields.first.required, isTrue);
      expect(option.fields.last.options.last.label, 'US');
    });

    test('parses map and JSON-schema-shaped fields', () {
      final option = NotificationTypeOption.fromJson({
        'type': 'service',
        'config_schema': {
          'required': ['endpoint'],
          'properties': {
            'endpoint': {'type': 'url', 'label': 'Endpoint'},
            'secret': {'type': 'string'},
          },
        },
      });

      expect(option.fields.map((field) => field.key), ['endpoint', 'secret']);
      expect(option.fields.first.required, isTrue);
      expect(option.fields.last.label, 'secret');
      expect(option.fields.last.isCredential, isTrue);
    });

    test('parses a JSON schema supplied directly as fields', () {
      final option = NotificationTypeOption.fromJson({
        'type': 'service',
        'fields': {
          'type': 'object',
          'required': ['token'],
          'properties': {
            'token': {'type': 'string'},
          },
        },
      });

      expect(option.fields, hasLength(1));
      expect(option.fields.single.key, 'token');
      expect(option.fields.single.required, isTrue);
    });

    test('keeps an explicitly empty schema authoritative', () {
      final option = NotificationTypeOption.fromJson({
        'type': 'service',
        'fields': <dynamic>[],
      });

      expect(option.hasFieldsSchema, isTrue);
      expect(option.fields, isEmpty);
    });

    test('safely ignores invalid field entries', () {
      final fields = parseNotificationFields([
        null,
        1,
        {},
        {'label': 'Missing key'},
        'valid',
      ]);

      expect(fields, hasLength(1));
      expect(fields.single.key, 'valid');
    });
  });

  group('notification config helpers', () {
    test('reads SMTP SSL aliases and tri-state values', () {
      expect(readSmtpSslMode({}), SmtpSslMode.auto);
      expect(readSmtpSslMode({'smtp_secure': true}), SmtpSslMode.on);
      expect(readSmtpSslMode({'use_ssl': 'off'}), SmtpSslMode.off);
      expect(readSmtpSslMode({'ssl': 'unknown'}), SmtpSslMode.auto);
    });

    test('stringifies every config value without dropping keys', () {
      final config = stringifyNotificationConfig({
        'unknown': 42,
        'enabled': true,
        'nullable': null,
        'nested': {'key': 'value'},
      });

      expect(
        config.keys,
        containsAll(['unknown', 'enabled', 'nullable', 'nested']),
      );
      expect(config['unknown'], '42');
      expect(config['enabled'], 'true');
      expect(config['nullable'], '');
      expect(config['nested'], '{"key":"value"}');
      expect(config.values.every((value) => value is String), isTrue);
    });

    test('merges edited fields into existing config', () {
      final config = mergeNotificationConfig(
        existingConfig: {
          'unknown': 42,
          'token': 'old',
          'smtp_secure': true,
        },
        fieldValues: {'token': 'new', 'empty': '', 'smtp_ssl': 'auto'},
        removeKeys: smtpSslAliases,
      );

      expect(config, {
        'unknown': '42',
        'token': 'new',
        'smtp_ssl': 'auto',
      });
    });

    test('preserves an existing credential when edit input is empty', () {
      final config = mergeNotificationConfig(
        existingConfig: {'token': 'old-secret', 'region': 'cn'},
        fieldValues: {'token': '', 'region': 'us'},
        preserveEmptyKeys: const ['token'],
      );

      expect(config, {'token': 'old-secret', 'region': 'us'});
    });
  });
}
