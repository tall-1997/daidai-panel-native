import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/cron_template.dart';
import '../../../shared/models/python_runtime_info.dart';
import '../../../shared/models/task.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';
import '../providers/task_provider.dart';

class TaskFormPrefill {
  final String name;
  final String command;
  final String? taskType;
  final String? cronExpression;

  const TaskFormPrefill({
    required this.name,
    required this.command,
    this.taskType,
    this.cronExpression,
  });
}

class TaskFormPage extends ConsumerStatefulWidget {
  final Task? task;
  final TaskFormPrefill? prefill;

  const TaskFormPage({super.key, this.task, this.prefill});

  @override
  ConsumerState<TaskFormPage> createState() => _TaskFormPageState();
}

enum _RandomDelayMode { inherit, disabled, custom }

class _TaskNotificationChannel {
  final int id;
  final String name;
  final String type;
  final bool enabled;

  const _TaskNotificationChannel({
    required this.id,
    required this.name,
    required this.type,
    required this.enabled,
  });

  factory _TaskNotificationChannel.fromJson(Map<String, dynamic> json) {
    return _TaskNotificationChannel(
      id: (json['id'] as num?)?.toInt() ?? 0,
      name: json['name']?.toString() ?? '',
      type: json['type']?.toString() ?? '',
      enabled: json['enabled'] == true,
    );
  }
}

class _TaskFormPageState extends ConsumerState<TaskFormPage> {
  final _formKey = GlobalKey<FormState>();
  late final TextEditingController _nameC;
  late final TextEditingController _commandC;
  late final TextEditingController _cronC;
  late final TextEditingController _timeoutC;
  late final TextEditingController _randomDelayC;
  late final TextEditingController _retriesC;
  late final TextEditingController _retryIntervalC;
  late final TextEditingController _successExitCodesC;
  late final TextEditingController _dependsOnC;
  late final TextEditingController _taskBeforeC;
  late final TextEditingController _taskAfterC;
  late final TextEditingController _stopScheduleC;
  late final TextEditingController _labelC;
  late final TextEditingController _groupC;

  bool _saving = false;
  bool _loadingChannels = false;
  bool _loadingPythonRuntimes = false;
  bool _loadingCronTemplates = false;
  bool _parsingCron = false;
  String _taskType = 'cron';
  bool _notifyOnFailure = true;
  bool _notifyOnSuccess = false;
  bool _notifyOnAbort = false;
  bool _allowMultipleInstances = false;
  String _pythonVersion = '3.12';
  String _pythonDefaultVersion = '3.12';
  int? _notificationChannelId;
  _RandomDelayMode _randomDelayMode = _RandomDelayMode.inherit;
  final List<String> _labels = [];
  List<_TaskNotificationChannel> _notificationChannels = const [];
  List<PythonRuntimeInfo> _pythonRuntimes = const [];
  List<CronTemplateGroup> _cronTemplateGroups = fallbackCronTemplateGroups;
  String _panelTimezoneLabel = '执行时区：由面板设置决定';
  String? _cronPreview;
  bool _showHooks = false;
  List<String> _knownGroups = const [];

  bool get isEditing => widget.task != null;

  @override
  void initState() {
    super.initState();
    final task = widget.task;
    final prefill = widget.prefill;
    _nameC = TextEditingController(text: task?.name ?? prefill?.name ?? '');
    _commandC = TextEditingController(
      text: task?.command ?? prefill?.command ?? '',
    );
    _cronC = TextEditingController(
      text: task?.effectiveCronExpressions.isNotEmpty == true
          ? task!.effectiveCronExpressions.join('\n')
          : (prefill?.cronExpression ?? '0 0 * * *'),
    );
    _timeoutC = TextEditingController(text: '${task?.timeout ?? 0}');
    _randomDelayC = TextEditingController(
      text: '${task?.randomDelaySeconds ?? 60}',
    );
    _retriesC = TextEditingController(text: '${task?.maxRetries ?? 0}');
    _retryIntervalC = TextEditingController(
      text: '${task?.retryInterval ?? 60}',
    );
    _successExitCodesC = TextEditingController(
      text: (task?.successExitCodes ?? const [0]).join(','),
    );
    _dependsOnC = TextEditingController(
      text: task?.dependsOn?.toString() ?? '',
    );
    _taskBeforeC = TextEditingController(text: task?.taskBefore ?? '');
    _taskAfterC = TextEditingController(text: task?.taskAfter ?? '');
    _stopScheduleC = TextEditingController(text: task?.stopSchedule ?? '');
    _labelC = TextEditingController();
    _groupC = TextEditingController(text: task?.groupName ?? '');

    _taskType = task?.taskType ?? prefill?.taskType ?? 'cron';
    _pythonVersion = task?.pythonVersion ?? '3.12';
    _notifyOnFailure = task?.notifyOnFailure ?? true;
    _notifyOnSuccess = task?.notifyOnSuccess ?? false;
    _notifyOnAbort = task?.notifyOnAbort ?? false;
    _allowMultipleInstances = task?.allowMultipleInstances ?? false;
    _notificationChannelId = task?.notificationChannelId;
    _labels
      ..clear()
      ..addAll(task?.userLabelsForDisplay ?? const []);
    _randomDelayMode = _resolveRandomDelayMode(task?.randomDelaySeconds);
    _showHooks = _taskBeforeC.text.isNotEmpty || _taskAfterC.text.isNotEmpty || _stopScheduleC.text.isNotEmpty;

    Future.microtask(() async {
      await Future.wait([
        _loadNotificationChannels(),
        _loadKnownGroups(),
        _loadPythonRuntimes(),
        _loadCronTemplates(),
        _loadPanelTimezone(),
      ]);
    });
  }

  @override
  void dispose() {
    for (final c in [
      _nameC,
      _commandC,
      _cronC,
      _timeoutC,
      _randomDelayC,
      _retriesC,
      _retryIntervalC,
      _successExitCodesC,
      _dependsOnC,
      _taskBeforeC,
      _taskAfterC,
      _stopScheduleC,
      _labelC,
      _groupC,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  _RandomDelayMode _resolveRandomDelayMode(int? v) {
    if (v == null) return _RandomDelayMode.inherit;
    if (v <= 0) return _RandomDelayMode.disabled;
    return _RandomDelayMode.custom;
  }

  Future<void> _loadNotificationChannels() async {
    setState(() => _loadingChannels = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.notificationChannels,
      );
      final data = extractData(resp.data);
      final channels = data is List
          ? data
                .whereType<Map>()
                .map(
                  (m) => _TaskNotificationChannel.fromJson(
                    Map<String, dynamic>.from(m),
                  ),
                )
                .toList()
          : <_TaskNotificationChannel>[];
      if (!mounted) return;
      setState(() {
        _notificationChannels = channels;
        _loadingChannels = false;
      });
    } catch (_) {
      if (mounted) setState(() => _loadingChannels = false);
    }
  }

  Future<void> _loadKnownGroups() async {
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.tasks,
        queryParameters: {'all': 1},
      );
      final paginated = extractPaginated(response.data);
      final groups =
          paginated.items
              .map((item) => Task.fromJson(item).groupName?.trim() ?? '')
              .where((group) => group.isNotEmpty)
              .toSet()
              .toList()
            ..sort();
      if (!mounted) {
        return;
      }
      setState(() => _knownGroups = groups);
    } catch (_) {}
  }

  Future<void> _loadPythonRuntimes() async {
    // 新建任务时，默认 Python 版本必须跟随后端默认版本，而不是继续写死 3.12。
    setState(() => _loadingPythonRuntimes = true);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.depsPythonRuntimes,
      );
      final raw = response.data;
      final map = raw is Map<String, dynamic>
          ? raw
          : raw is Map
          ? Map<String, dynamic>.from(raw)
          : <String, dynamic>{};
      final runtimeData = map['data'];
      final runtimes = runtimeData is List
          ? runtimeData
                .whereType<Map>()
                .map(
                  (item) => PythonRuntimeInfo.fromJson(
                    Map<String, dynamic>.from(item),
                  ),
                )
                .where((item) => item.version.trim().isNotEmpty)
                .toList()
          : <PythonRuntimeInfo>[];
      final defaultVersion =
          map['default_version']?.toString().trim().isNotEmpty == true
          ? map['default_version'].toString().trim()
          : '3.12';
      if (!mounted) {
        return;
      }
      setState(() {
        _pythonRuntimes = runtimes;
        _pythonDefaultVersion = defaultVersion;
        // 编辑已有任务时保留原值；新建任务时直接跟随后端默认版本。
        if (!isEditing) {
          _pythonVersion = defaultVersion;
        }
        _loadingPythonRuntimes = false;
      });
    } catch (_) {
      if (mounted) {
        setState(() => _loadingPythonRuntimes = false);
      }
    }
  }

  Future<void> _loadCronTemplates() async {
    setState(() => _loadingCronTemplates = true);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.cronTemplates,
      );
      final data = extractData(response.data);
      final groups = parseCronTemplateGroups(data);
      if (!mounted) return;
      setState(() {
        _cronTemplateGroups = groups.isEmpty
            ? fallbackCronTemplateGroups
            : groups;
        _loadingCronTemplates = false;
      });
    } catch (_) {
      if (mounted) {
        setState(() {
          _cronTemplateGroups = fallbackCronTemplateGroups;
          _loadingCronTemplates = false;
        });
      }
    }
  }

  Future<void> _loadPanelTimezone() async {
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.configs);
      final label = panelTimezoneLabel(extractData(response.data));
      if (mounted) setState(() => _panelTimezoneLabel = label);
    } catch (_) {}
  }

  void _appendCronTemplate(CronTemplate template) {
    final current = Task.parseCronInput(_cronC.text);
    final isUntouchedDefault = !isEditing &&
        current.length == 1 &&
        current.single == '0 0 * * *';
    _cronC.text = isUntouchedDefault || current.isEmpty
        ? template.expression
        : [...current, template.expression].join('\n');
    setState(() => _cronPreview = null);
  }

  Future<void> _parseCronExpression() async {
    final expressions = Task.parseCronInput(_cronC.text);
    final inputSnapshot = _cronC.text;
    if (expressions.isEmpty) {
      setState(() => _cronPreview = '请输入 Cron 表达式后再解析');
      return;
    }
    setState(() {
      _parsingCron = true;
      _cronPreview = null;
    });
    try {
      final previews = <String>[];
      for (final expression in expressions) {
        final response = await DioClient.instance.dio.post(
          ApiEndpoints.cronParse,
          data: {'expression': expression},
        );
        previews.add(
          '$expression\n${_formatCronParseResult(extractData(response.data))}',
        );
      }
      if (!mounted) return;
      if (_cronC.text != inputSnapshot) {
        setState(() => _parsingCron = false);
        return;
      }
      setState(() {
        _cronPreview = previews.join('\n\n');
        _parsingCron = false;
      });
    } catch (error) {
      if (!mounted) return;
      if (_cronC.text != inputSnapshot) {
        setState(() => _parsingCron = false);
        return;
      }
      setState(() {
        _cronPreview = extractErrorMessage(error, 'Cron 表达式解析失败');
        _parsingCron = false;
      });
    }
  }

  String _formatCronParseResult(dynamic data) {
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
      final valid = map['valid'];
      if (valid is bool) {
        lines.add(valid ? '表达式有效' : '表达式无效');
      }
      final next = map['next_runs'] ??
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

  void _addLabel() {
    final label = _labelC.text.trim();
    if (label.isEmpty || _labels.contains(label)) {
      _labelC.clear();
      return;
    }
    setState(() {
      _labels.add(label);
      _labelC.clear();
    });
  }

  int _parseInt(TextEditingController c, int fb) =>
      int.tryParse(c.text.trim()) ?? fb;

  Future<void> _save() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    if (_labelC.text.trim().isNotEmpty) {
      _addLabel();
    }

    final randomDelay = switch (_randomDelayMode) {
      _RandomDelayMode.inherit => null,
      _RandomDelayMode.disabled => 0,
      _RandomDelayMode.custom => int.tryParse(_randomDelayC.text.trim()),
    };

    if (_randomDelayMode == _RandomDelayMode.custom &&
        (randomDelay == null || randomDelay <= 0)) {
      AppGlassNotice.show(
        context,
        '请输入大于 0 的随机延迟秒数',
        type: AppGlassNoticeType.warning,
      );
      return;
    }

    final normalizedLabels = <String>[
      ..._labels.where((label) => !Task.isGroupLabel(label)),
    ];
    final groupName = _groupC.text.trim();
    if (groupName.isNotEmpty) {
      normalizedLabels.add(Task.toGroupLabel(groupName));
    }

    final data = <String, dynamic>{
      'name': _nameC.text.trim(),
      'command': _commandC.text.trim(),
      ...Task.cronPayload(_taskType == 'cron' ? _cronC.text : ''),
      'task_type': _taskType,
      'python_version': _pythonVersion,
      'timeout': _parseInt(_timeoutC, 0),
      'random_delay_seconds': randomDelay,
      'max_retries': _parseInt(_retriesC, 0),
      'retry_interval': _parseInt(_retryIntervalC, 60),
      'success_exit_codes': Task.parseSuccessExitCodes(
        _successExitCodesC.text,
      )!.join(','),
      'notify_on_failure': _notifyOnFailure,
      'notify_on_success': _notifyOnSuccess,
      'notify_on_abort': _notifyOnAbort,
      'notification_channel_id': _notificationChannelId,
      'labels': normalizedLabels,
      'depends_on': int.tryParse(_dependsOnC.text.trim()),
      'task_before': _taskBeforeC.text.trim(),
      'task_after': _taskAfterC.text.trim(),
      'stop_schedule': _stopScheduleC.text.trim(),
      'allow_multiple_instances': _allowMultipleInstances,
    };

    setState(() => _saving = true);
    try {
      if (isEditing) {
        await DioClient.instance.dio.put(
          ApiEndpoints.taskById(widget.task!.id),
          data: data,
        );
      } else {
        await DioClient.instance.dio.post(ApiEndpoints.tasks, data: data);
      }
      await ref.read(taskProvider.notifier).load(refresh: true);
      if (mounted) context.pop();
    } catch (error) {
      if (mounted) {
        AppGlassNotice.show(
          context,
          extractErrorMessage(error, '保存失败'),
          type: AppGlassNoticeType.error,
        );
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  List<_TaskNotificationChannel> get _channelOptions {
    final list = [..._notificationChannels];
    final sid = _notificationChannelId;
    if (sid != null && list.every((c) => c.id != sid)) {
      list.insert(
        0,
        _TaskNotificationChannel(
          id: sid,
          name: widget.task?.notificationChannelName ?? '渠道 #$sid',
          type: 'unknown',
          enabled: widget.task?.notificationChannelEnabled ?? false,
        ),
      );
    }
    return list;
  }

  List<DropdownMenuItem<String>> get _pythonRuntimeItems {
    if (_pythonRuntimes.isEmpty) {
      final fallback = ['3.10', '3.11', '3.12'];
      if (_pythonVersion.trim().isNotEmpty &&
          !fallback.contains(_pythonVersion)) {
        fallback.insert(0, _pythonVersion);
      }
      return [
        for (final version in fallback)
          DropdownMenuItem(
            value: version,
            child: Text('Python $version'),
          ),
      ];
    }
    final items = _pythonRuntimes.map((runtime) {
      final suffix = runtime.version == _pythonDefaultVersion ? '（默认）' : '';
      final status = runtime.available ? '' : ' · 需安装';
      return DropdownMenuItem(
        value: runtime.version,
        child: Text('${runtime.label}$suffix$status'),
      );
    }).toList();
    if (_pythonVersion.trim().isNotEmpty &&
        items.every((item) => item.value != _pythonVersion)) {
      items.insert(
        0,
        DropdownMenuItem(
          value: _pythonVersion,
          child: Text('Python $_pythonVersion（当前任务，运行时不可用）'),
        ),
      );
    }
    return items;
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isNarrow = MediaQuery.of(context).size.width < 420;
    Widget section(String title, List<Widget> children) {
      return AppCard(
        stableForScrolling: true,
        margin: const EdgeInsets.only(bottom: 14),
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              title,
              style: theme.textTheme.titleSmall?.copyWith(
                fontWeight: FontWeight.w700,
              ),
            ),
            const SizedBox(height: 14),
            ...children,
          ],
        ),
      );
    }

    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: Text(isEditing ? '编辑任务' : '新建任务'),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 8),
            child: AppLiquidGlassButton(
              label: '保存',
              onPressed: _saving ? null : _save,
              width: 80,
              height: 38,
              loading: _saving,
              performanceMode: true,
            ),
          ),
        ],
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        child: Form(
          key: _formKey,
          child: Column(
            children: [
              // 基本信息
              section('基本信息', [
                TextFormField(
                  controller: _nameC,
                  decoration: const InputDecoration(labelText: '任务名称'),
                  validator: (v) =>
                      v == null || v.trim().isEmpty ? '请输入任务名称' : null,
                ),
                const SizedBox(height: 12),
                TextFormField(
                  controller: _commandC,
                  decoration: const InputDecoration(
                    labelText: '执行命令',
                    hintText: 'task demo.py',
                  ),
                  minLines: 2,
                  maxLines: 4,
                  validator: (v) =>
                      v == null || v.trim().isEmpty ? '请输入执行命令' : null,
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  initialValue: _taskType,
                  decoration: const InputDecoration(labelText: '任务类型'),
                  items: [
                    const DropdownMenuItem(value: 'cron', child: Text('常规定时')),
                    const DropdownMenuItem(value: 'manual', child: Text('手动运行')),
                    const DropdownMenuItem(value: 'startup', child: Text('开机运行')),
                    if (!const ['cron', 'manual', 'startup'].contains(_taskType))
                      DropdownMenuItem(
                        value: _taskType,
                        child: Text('未知类型（$_taskType）'),
                      ),
                  ],
                  onChanged: (v) {
                    if (v != null) setState(() => _taskType = v);
                  },
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  initialValue: _pythonVersion,
                  decoration: InputDecoration(
                    labelText: 'Python 版本',
                    helperText: '仅 Python 脚本使用；新建任务默认跟随面板默认 Python 版本。',
                    suffixIcon: _loadingPythonRuntimes
                        ? const Padding(
                            padding: EdgeInsets.all(14),
                            child: SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                          )
                        : null,
                  ),
                  items: _pythonRuntimeItems,
                  onChanged: (v) {
                    if (v != null) {
                      setState(() => _pythonVersion = v);
                    }
                  },
                ),
                if (_taskType == 'cron') ...[
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: _cronC,
                    maxLines: null,
                    minLines: 1,
                    onChanged: (_) => setState(() => _cronPreview = null),
                    decoration: const InputDecoration(
                      labelText: 'Cron 表达式（每行一条）',
                      hintText: '0 0 * * *\n0 30 * * *',
                    ),
                    validator: (v) =>
                        _taskType == 'cron' && (v == null || v.trim().isEmpty)
                        ? '请输入 Cron 表达式'
                        : null,
                  ),
                  const SizedBox(height: 8),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: Text(
                      _panelTimezoneLabel,
                      style: TextStyle(
                        fontSize: 12,
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                  const SizedBox(height: 8),
                  for (final group in _cronTemplateGroups) ...[
                    Align(
                      alignment: Alignment.centerLeft,
                      child: Text(
                        group.name,
                        style: theme.textTheme.labelMedium,
                      ),
                    ),
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: [
                        for (final template in group.templates)
                          AppLiquidGlassActionChip(
                            label: template.name,
                            onPressed: () => _appendCronTemplate(template),
                          ),
                      ],
                    ),
                    const SizedBox(height: 8),
                  ],
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: [
                      AppLiquidGlassActionChip(
                        icon: Icons.manage_search,
                        label: _loadingCronTemplates ? '模板加载中' : '解析预览',
                        onPressed: _parsingCron ? null : _parseCronExpression,
                      ),
                    ],
                  ),
                  if (_cronPreview != null) ...[
                    const SizedBox(height: 8),
                    SizedBox(
                      width: double.infinity,
                      child: AppLiquidGlassSurface(
                        padding: const EdgeInsets.all(10),
                        borderRadius: 10,
                        performanceMode: true,
                        child: Text(
                          _cronPreview!,
                          style: TextStyle(
                            fontSize: 12,
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                      ),
                    ),
                  ],
                ],
                if (_labels.isNotEmpty || true) ...[
                  const SizedBox(height: 12),
                  _buildGroupAutocomplete(
                    controller: _groupC,
                    options: _knownGroups,
                  ),
                  const SizedBox(height: 12),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: [
                      ..._labels.map(
                        (l) => AppLiquidGlassInputChip(
                          label: l,
                          onDeleted: () => setState(() => _labels.remove(l)),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 8),
                  SizedBox(
                    width: double.infinity,
                    child: TextField(
                      controller: _labelC,
                      decoration: InputDecoration(
                        hintText: '添加标签',
                        isDense: true,
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 10,
                          vertical: 10,
                        ),
                        suffixIcon: IconButton(
                          onPressed: _addLabel,
                          icon: const Icon(Icons.add, size: 16),
                          padding: EdgeInsets.zero,
                          constraints: const BoxConstraints(
                            minWidth: 36,
                            minHeight: 36,
                          ),
                        ),
                      ),
                      style: const TextStyle(fontSize: 13),
                      onSubmitted: (_) => _addLabel(),
                    ),
                  ),
                ],
              ]),

              // 执行策略
              section('执行策略', [
                if (isNarrow) ...[
                  TextFormField(
                    controller: _timeoutC,
                    decoration: const InputDecoration(
                      labelText: '超时(秒)',
                      helperText: '0 表示永不超时',
                    ),
                    keyboardType: TextInputType.number,
                  ),
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: _retriesC,
                    decoration: const InputDecoration(labelText: '重试次数'),
                    keyboardType: TextInputType.number,
                  ),
                ] else
                  Row(
                    children: [
                      Expanded(
                        child: TextFormField(
                          controller: _timeoutC,
                          decoration: const InputDecoration(
                            labelText: '超时(秒)',
                            helperText: '0 表示永不超时',
                          ),
                          keyboardType: TextInputType.number,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: TextFormField(
                          controller: _retriesC,
                          decoration: const InputDecoration(labelText: '重试次数'),
                          keyboardType: TextInputType.number,
                        ),
                      ),
                    ],
                  ),
                const SizedBox(height: 12),
                if (isNarrow) ...[
                  TextFormField(
                    controller: _retryIntervalC,
                    decoration: const InputDecoration(labelText: '重试间隔(秒)'),
                    keyboardType: TextInputType.number,
                  ),
                  const SizedBox(height: 12),
                  TextFormField(
                    controller: _dependsOnC,
                    decoration: const InputDecoration(labelText: '依赖任务ID'),
                    keyboardType: TextInputType.number,
                  ),
                ] else
                  Row(
                    children: [
                      Expanded(
                        child: TextFormField(
                          controller: _retryIntervalC,
                          decoration: const InputDecoration(
                            labelText: '重试间隔(秒)',
                          ),
                          keyboardType: TextInputType.number,
                        ),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: TextFormField(
                          controller: _dependsOnC,
                          decoration: const InputDecoration(
                            labelText: '依赖任务ID',
                          ),
                          keyboardType: TextInputType.number,
                        ),
                      ),
                    ],
                  ),
                const SizedBox(height: 14),
                Text(
                  '随机延迟',
                  style: theme.textTheme.bodySmall?.copyWith(
                    fontWeight: FontWeight.w600,
                  ),
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                     AppLiquidGlassChoiceChip(
                       label: '继承系统',
                      selected: _randomDelayMode == _RandomDelayMode.inherit,
                      onSelected: (_) => setState(
                        () => _randomDelayMode = _RandomDelayMode.inherit,
                      ),
                    ),
                     AppLiquidGlassChoiceChip(
                       label: '不延迟',
                      selected: _randomDelayMode == _RandomDelayMode.disabled,
                      onSelected: (_) => setState(
                        () => _randomDelayMode = _RandomDelayMode.disabled,
                      ),
                    ),
                     AppLiquidGlassChoiceChip(
                       label: '自定义',
                      selected: _randomDelayMode == _RandomDelayMode.custom,
                      onSelected: (_) => setState(
                        () => _randomDelayMode = _RandomDelayMode.custom,
                      ),
                    ),
                  ],
                ),
                if (_randomDelayMode == _RandomDelayMode.custom) ...[
                  const SizedBox(height: 10),
                  TextFormField(
                    controller: _randomDelayC,
                    decoration: const InputDecoration(labelText: '延迟秒数'),
                    keyboardType: TextInputType.number,
                  ),
                ],
                const SizedBox(height: 12),
                TextFormField(
                  controller: _successExitCodesC,
                  decoration: const InputDecoration(
                    labelText: '成功退出码',
                    helperText: '使用英文逗号分隔，例如 0,2',
                  ),
                  keyboardType: const TextInputType.numberWithOptions(
                    signed: true,
                  ),
                  validator: (value) {
                    if (Task.parseSuccessExitCodes(value ?? '') == null) {
                      return '请输入逗号分隔的整数退出码';
                    }
                    return null;
                  },
                ),
              ]),

              // 通知
              section('通知与并发', [
                Row(
                  children: [
                    const Expanded(
                      child: Text('失败时通知', style: TextStyle(fontSize: 14)),
                    ),
                    AppLiquidGlassToggle(
                      value: _notifyOnFailure,
                      onChanged: (v) => setState(() => _notifyOnFailure = v),
                    ),
                  ],
                ),
                Row(
                  children: [
                    const Expanded(
                      child: Text('终止时通知', style: TextStyle(fontSize: 14)),
                    ),
                    AppLiquidGlassToggle(
                      value: _notifyOnAbort,
                      onChanged: (v) => setState(() => _notifyOnAbort = v),
                    ),
                  ],
                ),
                Row(
                  children: [
                    const Expanded(
                      child: Text('成功时通知', style: TextStyle(fontSize: 14)),
                    ),
                    AppLiquidGlassToggle(
                      value: _notifyOnSuccess,
                      onChanged: (v) => setState(() => _notifyOnSuccess = v),
                    ),
                  ],
                ),
                Row(
                  children: [
                    const Expanded(
                      child: Text('允许多实例', style: TextStyle(fontSize: 14)),
                    ),
                    AppLiquidGlassToggle(
                      value: _allowMultipleInstances,
                      onChanged: (v) =>
                          setState(() => _allowMultipleInstances = v),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                DropdownButtonFormField<int?>(
                  initialValue: _notificationChannelId,
                  decoration: InputDecoration(
                    labelText: '通知渠道',
                    suffixIcon: _loadingChannels
                        ? const Padding(
                            padding: EdgeInsets.all(14),
                            child: SizedBox(
                              width: 14,
                              height: 14,
                              child: CircularProgressIndicator(strokeWidth: 2),
                            ),
                          )
                        : null,
                  ),
                  items: [
                    const DropdownMenuItem<int?>(
                      value: null,
                      child: Text('全部启用渠道'),
                    ),
                    if (_notificationChannelId != null &&
                        _channelOptions.every(
                          (channel) => channel.id != _notificationChannelId,
                        ))
                      DropdownMenuItem<int?>(
                        value: _notificationChannelId,
                        child: Text('渠道 #$_notificationChannelId（当前值）'),
                      ),
                    ..._channelOptions.map(
                      (c) => DropdownMenuItem<int?>(
                        value: c.id,
                        child: Text('${c.name} (${c.type})'),
                      ),
                    ),
                  ],
                  onChanged: (v) => setState(() => _notificationChannelId = v),
                ),
              ]),

              // 钩子脚本（折叠）
              AppCard(
                stableForScrolling: true,
                margin: const EdgeInsets.only(bottom: 14),
                padding: EdgeInsets.zero,
                child: Column(
                  children: [
                    InkWell(
                      onTap: () => setState(() => _showHooks = !_showHooks),
                      borderRadius: const BorderRadius.vertical(
                        top: Radius.circular(14),
                      ),
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Row(
                          children: [
                            Text(
                              '钩子脚本',
                              style: theme.textTheme.titleSmall?.copyWith(
                                fontWeight: FontWeight.w700,
                              ),
                            ),
                            if (_taskBeforeC.text.isNotEmpty ||
                                _taskAfterC.text.isNotEmpty ||
                                _stopScheduleC.text.isNotEmpty)
                              Container(
                                margin: const EdgeInsets.only(left: 8),
                                width: 6,
                                height: 6,
                                decoration: const BoxDecoration(
                                  color: AppColors.primary,
                                  shape: BoxShape.circle,
                                ),
                              ),
                            const Spacer(),
                            Icon(
                              _showHooks
                                  ? Icons.expand_less
                                  : Icons.expand_more,
                              size: 20,
                              color: AppColors.slate400,
                            ),
                          ],
                        ),
                      ),
                    ),
                    if (_showHooks)
                      Padding(
                        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                        child: Column(
                          children: [
                            TextFormField(
                              controller: _taskBeforeC,
                              decoration: const InputDecoration(
                                labelText: '前置脚本',
                                hintText: '每次执行任务前运行，可写 Shell 命令或脚本路径。',
                                helperText: '任务命令和参数会作为参数传入，适合准备环境、检查变量。',
                                alignLabelWithHint: true,
                              ),
                              minLines: 3,
                              maxLines: 5,
                            ),
                            const SizedBox(height: 12),
                            TextFormField(
                              controller: _taskAfterC,
                              decoration: const InputDecoration(
                                labelText: '后置脚本',
                                hintText: '任务结束后运行，可用于清理现场或发送额外通知。',
                                helperText: '同样会收到任务命令和参数，任务失败时也会执行。',
                                alignLabelWithHint: true,
                              ),
                              minLines: 3,
                              maxLines: 5,
                            ),
                            const SizedBox(height: 12),
                            TextFormField(
                              controller: _stopScheduleC,
                              decoration: const InputDecoration(
                                labelText: '定时停止 Cron',
                                hintText: '例如 0 8 * * *，留空表示不定时停止。',
                                helperText: '命中表达式时停止该任务当前运行实例。',
                              ),
                              maxLines: 2,
                            ),
                          ],
                        ),
                      ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildGroupAutocomplete({
    required TextEditingController controller,
    required List<String> options,
  }) {
    return Autocomplete<String>(
      optionsBuilder: (textEditingValue) {
        final keyword = textEditingValue.text.trim().toLowerCase();
        if (keyword.isEmpty) {
          return options;
        }
        return options.where((group) => group.toLowerCase().contains(keyword));
      },
      onSelected: (value) => controller.text = value,
      fieldViewBuilder:
          (context, textEditingController, focusNode, onSubmitted) {
            textEditingController.value = controller.value;
            textEditingController.addListener(() {
              controller.value = textEditingController.value;
            });
            return TextField(
              controller: textEditingController,
              focusNode: focusNode,
              decoration: const InputDecoration(
                labelText: '任务分组',
                hintText: '可选已有分组或直接输入',
              ),
              onSubmitted: (_) => onSubmitted(),
            );
          },
      optionsViewBuilder: (context, onSelected, autocompleteOptions) {
        final items = autocompleteOptions.toList(growable: false);
        if (items.isEmpty) {
          return const SizedBox.shrink();
        }
        return Align(
          alignment: Alignment.topLeft,
          child: AppLiquidGlassSurface(
            borderRadius: 12,
            performanceMode: true,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 220, maxWidth: 280),
              child: ListView.builder(
                padding: const EdgeInsets.symmetric(vertical: 8),
                shrinkWrap: true,
                itemCount: items.length,
                itemBuilder: (context, index) {
                  final group = items[index];
                  return ListTile(
                    dense: true,
                    title: Text(group),
                    onTap: () => onSelected(group),
                  );
                },
              ),
            ),
          ),
        );
      },
    );
  }
}
