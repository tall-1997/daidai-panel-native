class SystemConfigOption {
  final String value;
  final String label;

  const SystemConfigOption({required this.value, required this.label});

  factory SystemConfigOption.fromJson(dynamic raw) {
    if (raw is Map) {
      final value = raw['value'] ?? raw['key'] ?? raw['id'] ?? '';
      final label = raw['label'] ?? raw['name'] ?? value;
      return SystemConfigOption(value: value.toString(), label: label.toString());
    }
    return SystemConfigOption(
      value: raw?.toString() ?? '',
      label: raw?.toString() ?? '',
    );
  }
}

class SystemConfigSchema {
  final String key;
  final String label;
  final String group;
  final String valueType;
  final String value;
  final bool hasValue;
  final String defaultValue;
  final String description;
  final bool readonly;
  final bool registered;
  final List<SystemConfigOption> options;

  const SystemConfigSchema({
    required this.key,
    required this.label,
    required this.group,
    required this.valueType,
    required this.value,
    required this.hasValue,
    required this.defaultValue,
    required this.description,
    required this.readonly,
    required this.registered,
    required this.options,
  });

  String get effectiveValue {
    if (hasValue) return value;
    if (defaultValue.isNotEmpty) return defaultValue;
    if (key == 'max_log_content_size' || key == 'log_max_size') {
      return '102400000';
    }
    return '';
  }

  bool get isBool => valueType == 'bool' || valueType == 'boolean';
  bool get isInt =>
      valueType == 'int' || valueType == 'integer' || valueType == 'number';
  bool get isEnum => valueType == 'enum' || options.isNotEmpty;
  bool get isCredential {
    final normalized = key.toLowerCase();
    return normalized.contains('password') ||
        normalized.contains('secret') ||
        normalized == 'token' ||
        normalized.endsWith('_token') ||
        normalized == 'key' ||
        normalized.endsWith('_key');
  }

  factory SystemConfigSchema.fromJson(String mapKey, Map<String, dynamic> raw) {
    final key = (raw['key'] ?? raw['name'] ?? mapKey).toString();
    final defaultValue = _stringValue(raw['default_value'] ?? raw['default']);
    return SystemConfigSchema(
      key: key,
      label: (raw['label'] ?? raw['title'] ?? key).toString(),
      group: (raw['group'] ?? '其他').toString().trim().isEmpty
          ? '其他'
          : (raw['group'] ?? '其他').toString(),
      valueType: (raw['value_type'] ?? raw['type'] ?? 'string')
          .toString()
          .toLowerCase(),
      value: _stringValue(raw['value']),
      hasValue: raw.containsKey('value'),
      defaultValue: defaultValue,
      description: (raw['description'] ?? '').toString(),
      readonly: _boolValue(raw['readonly'] ?? raw['read_only']),
      registered: raw.containsKey('registered')
          ? _boolValue(raw['registered'])
          : true,
      options: (raw['options'] is List ? raw['options'] as List : const [])
          .map(SystemConfigOption.fromJson)
          .where((option) => option.value.isNotEmpty)
          .toList(),
    );
  }
}

List<SystemConfigSchema> parseSystemConfigSchemas(dynamic raw) {
  final schemas = <SystemConfigSchema>[];
  if (raw is Map) {
    for (final entry in raw.entries) {
      if (entry.value is Map) {
        schemas.add(SystemConfigSchema.fromJson(
          entry.key.toString(),
          Map<String, dynamic>.from(entry.value as Map),
        ));
      }
    }
  } else if (raw is List) {
    for (final item in raw.whereType<Map>()) {
      final map = Map<String, dynamic>.from(item);
      schemas.add(
        SystemConfigSchema.fromJson(map['key']?.toString() ?? '', map),
      );
    }
  }
  return schemas;
}

Map<String, String> changedSystemConfigValues(
  Map<String, String> initial,
  Map<String, String> current,
) {
  return {
    for (final entry in current.entries)
      if (initial[entry.key] != entry.value) entry.key: entry.value,
  };
}

String _stringValue(dynamic value) {
  if (value == null) return '';
  if (value is bool) return value ? 'true' : 'false';
  return value.toString();
}

bool _boolValue(dynamic value) {
  if (value is bool) return value;
  if (value is num) return value != 0;
  return const {
    'true',
    '1',
    'yes',
    'on',
  }.contains(value?.toString().toLowerCase());
}
