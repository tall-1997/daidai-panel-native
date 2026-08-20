import 'dart:convert';
import 'dart:typed_data';

import 'package:daidai_app/shared/utils/import_payloads.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter_test/flutter_test.dart';

Uint8List _jsonBytes(Object? value) =>
    Uint8List.fromList(utf8.encode(jsonEncode(value)));

void main() {
  test('readPlatformFileBytes enforces the byte limit', () async {
    final file = PlatformFile(
      name: 'envs.json',
      size: 5,
      bytes: Uint8List.fromList([1, 2, 3, 4, 5]),
    );

    await expectLater(
      readPlatformFileBytes(file, maxBytes: 4),
      throwsA(
        isA<ImportPayloadException>().having(
          (error) => error.message,
          'message',
          contains('4 B'),
        ),
      ),
    );
  });

  group('parseTaskImportPayload', () {
    test('accepts a direct task array', () {
      final tasks = parseTaskImportPayload(
        _jsonBytes([
          {'name': 'daily'},
        ]),
      );

      expect(tasks, [
        {'name': 'daily'},
      ]);
    });

    test('accepts API and tasks wrappers', () {
      final tasks = parseTaskImportPayload(
        _jsonBytes({
          'data': {
            'tasks': [
              {'name': 'wrapped'},
            ],
          },
        }),
      );

      expect(tasks.single['name'], 'wrapped');
    });

    test('rejects non-object task entries', () {
      expect(
        () => parseTaskImportPayload(_jsonBytes(['invalid'])),
        throwsA(isA<ImportPayloadException>()),
      );
    });
  });

  group('parseEnvImportPayload', () {
    test('accepts direct and wrapped env arrays', () {
      final direct = parseEnvImportPayload(
        _jsonBytes([
          {'name': 'TOKEN', 'value': 'a'},
        ]),
      );
      final wrapped = parseEnvImportPayload(
        _jsonBytes({
          'data': {
            'envs': [
              {'name': 'TOKEN_2', 'remarks': 'secondary'},
            ],
          },
        }),
      );

      expect(direct.single['name'], 'TOKEN');
      expect(wrapped.single['name'], 'TOKEN_2');
    });

    test('rejects missing names', () {
      expect(
        () => parseEnvImportPayload(
          _jsonBytes([
            {'name': '  ', 'value': 'a'},
          ]),
        ),
        throwsA(
          isA<ImportPayloadException>().having(
            (error) => error.message,
            'message',
            contains('缺少有效名称'),
          ),
        ),
      );
    });

    test('rejects duplicate name and remarks identities', () {
      expect(
        () => parseEnvImportPayload(
          _jsonBytes([
            {'name': 'TOKEN', 'remarks': 'main', 'value': 'a'},
            {'name': ' TOKEN ', 'remarks': ' main ', 'value': 'b'},
          ]),
        ),
        throwsA(
          isA<ImportPayloadException>().having(
            (error) => error.message,
            'message',
            contains('重复环境变量'),
          ),
        ),
      );
    });
  });
}
