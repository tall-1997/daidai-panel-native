import 'dart:convert';
import 'dart:typed_data';

import 'package:file_picker/file_picker.dart';

const int maxTaskImportBytes = 10 * 1024 * 1024;
const int maxEnvImportBytes = 1024 * 1024;

class ImportPayloadException implements Exception {
  final String message;

  const ImportPayloadException(this.message);

  @override
  String toString() => message;
}

Future<Uint8List> readPlatformFileBytes(
  PlatformFile file, {
  required int maxBytes,
}) async {
  if (file.size > maxBytes) {
    throw ImportPayloadException('文件大小不能超过 ${formatImportSize(maxBytes)}');
  }

  final bytes = file.bytes;
  if (bytes != null) {
    return _checkSize(bytes, maxBytes);
  }

  final stream = file.readStream;
  if (stream != null) {
    return _readStream(stream, maxBytes);
  }

  throw const ImportPayloadException('无法读取所选文件');
}

List<Map<String, dynamic>> parseTaskImportPayload(Uint8List bytes) {
  final decoded = _decodeJson(bytes);
  return _extractObjectList(decoded, key: 'tasks', itemLabel: '任务');
}

List<Map<String, dynamic>> parseEnvImportPayload(Uint8List bytes) {
  final decoded = _decodeJson(bytes);
  final envs = _extractObjectList(decoded, key: 'envs', itemLabel: '环境变量');
  final identities = <String>{};

  for (var index = 0; index < envs.length; index++) {
    final env = envs[index];
    final rawName = env['name'];
    if (rawName is! String || rawName.trim().isEmpty) {
      throw ImportPayloadException('第 ${index + 1} 个环境变量缺少有效名称');
    }
    final remarks = env['remarks'];
    if (remarks != null && remarks is! String) {
      throw ImportPayloadException('第 ${index + 1} 个环境变量的备注必须是字符串');
    }

    final identity = jsonEncode([
      rawName.trim(),
      (remarks as String? ?? '').trim(),
    ]);
    if (!identities.add(identity)) {
      throw ImportPayloadException(
        '文件内存在重复环境变量：${rawName.trim()}（名称和备注相同）',
      );
    }
  }

  return envs;
}

String formatImportSize(int bytes) {
  if (bytes < 1024) {
    return '$bytes B';
  }
  if (bytes % (1024 * 1024) == 0) {
    return '${bytes ~/ (1024 * 1024)} MiB';
  }
  return '${(bytes / 1024).toStringAsFixed(0)} KiB';
}

dynamic _decodeJson(Uint8List bytes) {
  if (bytes.isEmpty) {
    throw const ImportPayloadException('导入文件为空');
  }
  try {
    var text = utf8.decode(bytes);
    if (text.startsWith('\ufeff')) {
      text = text.substring(1);
    }
    return jsonDecode(text);
  } on FormatException {
    throw const ImportPayloadException('导入文件不是有效的 UTF-8 JSON');
  }
}

List<Map<String, dynamic>> _extractObjectList(
  dynamic value, {
  required String key,
  required String itemLabel,
}) {
  dynamic candidate = value;
  for (var depth = 0; depth < 3 && candidate is Map; depth++) {
    if (candidate.containsKey(key)) {
      candidate = candidate[key];
      break;
    }
    if (candidate.containsKey('data')) {
      candidate = candidate['data'];
      continue;
    }
    break;
  }

  if (candidate is! List) {
    throw ImportPayloadException('JSON 中缺少 $key 数组');
  }
  if (candidate.isEmpty) {
    throw ImportPayloadException('$itemLabel数组为空');
  }

  final result = <Map<String, dynamic>>[];
  for (var index = 0; index < candidate.length; index++) {
    final item = candidate[index];
    if (item is! Map) {
      throw ImportPayloadException(
        '第 ${index + 1} 个$itemLabel必须是 JSON 对象',
      );
    }
    result.add(Map<String, dynamic>.from(item));
  }
  return result;
}

Future<Uint8List> _readStream(Stream<List<int>> stream, int maxBytes) async {
  final builder = BytesBuilder(copy: false);
  await for (final chunk in stream) {
    if (builder.length + chunk.length > maxBytes) {
      throw ImportPayloadException('文件大小不能超过 ${formatImportSize(maxBytes)}');
    }
    builder.add(chunk);
  }
  return builder.takeBytes();
}

Uint8List _checkSize(Uint8List bytes, int maxBytes) {
  if (bytes.length > maxBytes) {
    throw ImportPayloadException(
      '文件大小不能超过 ${formatImportSize(maxBytes)}',
    );
  }
  return bytes;
}
