import '../utils/api_utils.dart';

class RawLogTicket {
  final String url;
  final String filename;
  final int size;
  final DateTime? expiresAt;
  final int expiresIn;
  final DateTime? receivedAt;

  const RawLogTicket({
    required this.url,
    required this.filename,
    this.size = 0,
    this.expiresAt,
    this.expiresIn = 0,
    this.receivedAt,
  });

  factory RawLogTicket.fromResponse(
    dynamic response, {
    DateTime? receivedAt,
  }) {
    final parsedAt = receivedAt ?? DateTime.now();
    final data = extractData(response);
    if (data is! Map) throw const FormatException('下载票据格式错误');
    final map = Map<String, dynamic>.from(data);
    final url = map['url']?.toString().trim() ?? '';
    final filename = map['filename']?.toString().trim() ?? '';
    final expiresIn = _tryInt(map['expires_in']);
    if (url.isEmpty || filename.isEmpty) {
      throw const FormatException('下载票据缺少 URL 或文件名');
    }
    return RawLogTicket(
      url: url,
      filename: filename,
      size: _toInt(map['size']),
      expiresAt: DateTime.tryParse(map['expires_at']?.toString() ?? ''),
      expiresIn: expiresIn ?? 0,
      receivedAt: expiresIn != null && expiresIn >= 0 ? parsedAt : null,
    );
  }

  DateTime? get expirationTime {
    final relativeExpiration = expiresIn >= 0 && receivedAt != null
        ? receivedAt!.add(Duration(seconds: expiresIn))
        : null;
    if (expiresAt == null) return relativeExpiration;
    if (relativeExpiration == null) return expiresAt;
    return expiresAt!.isBefore(relativeExpiration)
        ? expiresAt
        : relativeExpiration;
  }

  bool isExpired({DateTime? at, Duration safetyWindow = Duration.zero}) {
    final expiration = expirationTime;
    if (expiration == null) return false;
    return !(at ?? DateTime.now()).add(safetyWindow).isBefore(expiration);
  }
}

int _toInt(dynamic value) {
  return _tryInt(value) ?? 0;
}

int? _tryInt(dynamic value) {
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}
