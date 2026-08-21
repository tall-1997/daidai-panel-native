import 'dart:convert';

class NotifyChannel {
  final int id;
  final String name;
  final String type;
  final Map<String, dynamic> config;
  final bool enabled;
  final String pushScope;
  final int todaySendCount;
  final DateTime? lastTestAt;
  final String lastTestStatus;
  final DateTime createdAt;
  final DateTime updatedAt;

  const NotifyChannel({
    required this.id,
    required this.name,
    this.type = '',
    this.config = const {},
    this.enabled = true,
    this.pushScope = 'default',
    this.todaySendCount = 0,
    this.lastTestAt,
    this.lastTestStatus = '',
    required this.createdAt,
    required this.updatedAt,
  });

  factory NotifyChannel.fromJson(Map<String, dynamic> json) {
    return NotifyChannel(
      id: _int(json['id']),
      name: json['name']?.toString() ?? '',
      type: json['type']?.toString() ?? '',
      config: _config(json['config']),
      enabled: _bool(json['enabled'], fallback: true),
      pushScope: json['push_scope']?.toString() ?? 'default',
      todaySendCount: _int(json['today_send_count']),
      lastTestAt: _date(json['last_test_at']),
      lastTestStatus: json['last_test_status']?.toString() ?? '',
      createdAt: _date(json['created_at']) ?? DateTime.now(),
      updatedAt: _date(json['updated_at']) ?? DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
    'name': name,
    'type': type,
    'config': jsonEncode(config),
    'push_scope': pushScope,
  };
}

int _int(dynamic v) => (v is num) ? v.toInt() : 0;
bool _bool(dynamic value, {required bool fallback}) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  switch (value?.toString().trim().toLowerCase()) {
    case 'true':
    case '1':
    case 'yes':
    case 'on':
      return true;
    case 'false':
    case '0':
    case 'no':
    case 'off':
      return false;
    default:
      return fallback;
  }
}
DateTime? _date(dynamic v) {
  if (v is String && v.isNotEmpty) return DateTime.tryParse(v);
  return null;
}

Map<String, dynamic> _config(dynamic value) {
  if (value is Map<String, dynamic>) {
    return value;
  }

  if (value is Map) {
    return value.map((key, value) => MapEntry(key.toString(), value));
  }

  if (value is String && value.trim().isNotEmpty) {
    try {
      final decoded = jsonDecode(value);
      if (decoded is Map<String, dynamic>) {
        return decoded;
      }
      if (decoded is Map) {
        return decoded.map((key, value) => MapEntry(key.toString(), value));
      }
    } catch (_) {}
  }

  return {};
}
