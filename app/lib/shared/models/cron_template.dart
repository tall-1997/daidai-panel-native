class CronTemplate {
  final String name;
  final String expression;
  final String description;

  const CronTemplate({
    required this.name,
    required this.expression,
    this.description = '',
  });

  factory CronTemplate.fromJson(Map<String, dynamic> json) {
    final expression = (json['expression'] ??
            json['cron_expression'] ??
            json['cron'] ??
            json['value'] ??
            '')
        .toString()
        .trim();
    final name = (json['name'] ??
            json['label'] ??
            json['title'] ??
            expression)
        .toString()
        .trim();
    return CronTemplate(
      name: name.isEmpty ? expression : name,
      expression: expression,
      description: json['description']?.toString().trim() ?? '',
    );
  }
}

class CronTemplateGroup {
  final String name;
  final List<CronTemplate> templates;

  const CronTemplateGroup({required this.name, required this.templates});
}

const fallbackCronTemplateGroups = [
  CronTemplateGroup(
    name: '常用',
    templates: [
      CronTemplate(name: '每小时', expression: '0 0 * * * *'),
      CronTemplate(name: '每天0点', expression: '0 0 0 * * *'),
      CronTemplate(name: '每天9点', expression: '0 0 9 * * *'),
    ],
  ),
];

List<CronTemplateGroup> parseCronTemplateGroups(dynamic raw) {
  final groups = <String, List<CronTemplate>>{};

  void addTemplate(dynamic value, String fallbackGroup) {
    if (value is! Map) return;
    final map = Map<String, dynamic>.from(value);
    final template = CronTemplate.fromJson(map);
    if (template.expression.isEmpty) return;
    final group = (map['group'] ??
            map['group_name'] ??
            map['category'] ??
            fallbackGroup)
        .toString()
        .trim();
    groups.putIfAbsent(group.isEmpty ? '常用' : group, () => []).add(template);
  }

  void addCollection(dynamic value, String fallbackGroup) {
    if (value is List) {
      for (final item in value) {
        if (item is Map) {
          final map = Map<String, dynamic>.from(item);
          final nested = map['templates'] ??
              map['items'] ??
              map['options'] ??
              map['groups'] ??
              map['categories'];
          if (nested is List || nested is Map) {
            final group = (map['group'] ??
                    map['group_name'] ??
                    map['category'] ??
                    map['name'] ??
                    map['label'] ??
                    fallbackGroup)
                .toString()
                .trim();
            addCollection(nested, group);
          } else {
            addTemplate(map, fallbackGroup);
          }
        }
      }
    } else if (value is Map) {
      final map = Map<String, dynamic>.from(value);
      final nested = map['templates'] ??
          map['items'] ??
          map['options'] ??
          map['groups'] ??
          map['categories'];
      if (nested is List || nested is Map) {
        final group = (map['group'] ??
                map['group_name'] ??
                map['category'] ??
                map['name'] ??
                map['label'] ??
                fallbackGroup)
            .toString()
            .trim();
        addCollection(nested, group);
      } else if (map.keys.any(
        (key) => const {
          'expression',
          'cron_expression',
          'cron',
          'value',
        }.contains(key),
      )) {
        addTemplate(map, fallbackGroup);
      } else {
        for (final entry in map.entries) {
          addCollection(entry.value, entry.key);
        }
      }
    }
  }

  addCollection(raw, '常用');

  return [
    for (final entry in groups.entries)
      CronTemplateGroup(name: entry.key, templates: entry.value),
  ];
}

String panelTimezoneLabel(dynamic raw) {
  const aliases = {'timezone', 'time_zone', 'tz'};

  String? readValue(dynamic value) {
    if (value is Map) {
      final map = Map<String, dynamic>.from(value);
      final key = (map['key'] ?? map['name'] ?? '').toString().toLowerCase();
      if (aliases.contains(key)) {
        final result = map['value'] ?? map['default_value'] ?? map['default'];
        if (result != null && result.toString().trim().isNotEmpty) {
          return result.toString().trim();
        }
      }
      for (final entry in map.entries) {
        if (aliases.contains(entry.key.toString().toLowerCase())) {
          final direct = entry.value;
          if (direct is Map) {
            final result = direct['value'] ??
                direct['default_value'] ??
                direct['default'];
            if (result != null && result.toString().trim().isNotEmpty) {
              return result.toString().trim();
            }
          } else if (direct != null && direct.toString().trim().isNotEmpty) {
            return direct.toString().trim();
          }
        }
      }
      for (final nested in map.values) {
        if (nested is Map || nested is List) {
          final result = readValue(nested);
          if (result != null) return result;
        }
      }
    } else if (value is List) {
      for (final item in value) {
        final result = readValue(item);
        if (result != null) return result;
      }
    }
    return null;
  }

  final timezone = readValue(raw);
  return timezone == null
      ? '执行时区：由面板设置决定'
      : '执行时区：$timezone';
}

String formatCronParseResult(dynamic data) {
  if (data is String && data.trim().isNotEmpty) {
    return data.trim();
  }
  if (data is List) {
    final values = data.take(5).map((item) => item.toString()).toList();
    return values.isEmpty ? '表达式有效' : '接下来执行：${values.join('、')}';
  }
  if (data is Map) {
    final map = Map<String, dynamic>.from(data);
    final lines = <String>[];
    final description = map['description']?.toString().trim();
    if (description != null && description.isNotEmpty) {
      lines.add(description);
    }
    final format = map['format']?.toString().trim();
    if (format != null && format.isNotEmpty) {
      lines.add(format);
    }
    final valid = map['is_valid'] ?? map['valid'];
    if (valid is bool) {
      lines.add(valid ? '表达式有效' : '表达式无效');
    }
    final next = map['next_run_times'] ??
        map['next_runs'] ??
        map['next_times'] ??
        map['next'] ??
        map['next_run'];
    if (next is List && next.isNotEmpty) {
      lines.add('接下来执行：${next.take(5).join('、')}');
    } else if (next != null && next.toString().trim().isNotEmpty) {
      lines.add('下次执行：$next');
    }
    return lines.isEmpty ? '表达式有效' : lines.join('\n');
  }
  return '表达式有效';
}
