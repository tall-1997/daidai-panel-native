import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/notify_channel.dart';
import '../../../shared/widgets/app_card.dart';
import '../../../shared/utils/api_utils.dart';

final notificationListProvider =
    StateNotifierProvider<NotificationListNotifier, NotificationListState>((
      ref,
    ) {
      return NotificationListNotifier();
    });

class NotificationTypeOption {
  final String type;
  final String name;
  final List<NotificationFieldOption> fields;
  final bool hasFieldsSchema;

  const NotificationTypeOption({
    required this.type,
    required this.name,
    this.fields = const [],
    this.hasFieldsSchema = false,
  });

  factory NotificationTypeOption.fromJson(Map<String, dynamic> json) {
    final schema = json['schema'] ?? json['config_schema'];
    final fieldsSchema = json.containsKey('fields') ? json['fields'] : schema;
    final rawFields = fieldsSchema is Map
        ? fieldsSchema['fields'] ??
              fieldsSchema['properties'] ??
              (fieldsSchema['type'] == 'object' ? null : fieldsSchema)
        : fieldsSchema;
    final requiredSource = fieldsSchema is Map
        ? fieldsSchema['required']
        : schema is Map
        ? schema['required']
        : null;
    final requiredKeys = requiredSource is List
        ? requiredSource.map((value) => value.toString()).toSet()
        : const <String>{};
    final fields = parseNotificationFields(rawFields, requiredKeys: requiredKeys);

    return NotificationTypeOption(
      type: json['type']?.toString() ?? '',
      name: json['name']?.toString() ?? json['type']?.toString() ?? '',
      fields: fields,
      hasFieldsSchema: rawFields is List || rawFields is Map,
    );
  }
}

class NotificationFieldOption {
  final String key;
  final String label;
  final String type;
  final String hint;
  final bool required;
  final List<NotificationFieldChoice> options;

  const NotificationFieldOption({
    required this.key,
    required this.label,
    this.type = 'text',
    this.hint = '',
    this.required = false,
    this.options = const [],
  });

  bool get isCredential {
    final normalizedType = type.toLowerCase();
    final normalizedKey = key.toLowerCase();
    return normalizedType == 'password' ||
        normalizedType == 'secret' ||
        normalizedType == 'credential' ||
        normalizedKey.contains('password') ||
        normalizedKey.contains('passwd') ||
        normalizedKey.contains('secret') ||
        normalizedKey == 'key' ||
        normalizedKey.endsWith('_key') ||
        normalizedKey == 'token' ||
        normalizedKey.endsWith('_token') ||
        normalizedKey.contains('api_key') ||
        normalizedKey.contains('apikey');
  }
}

class NotificationFieldChoice {
  final String value;
  final String label;

  const NotificationFieldChoice({required this.value, required this.label});
}

List<NotificationFieldChoice> notificationFieldChoices(
  List<NotificationFieldChoice> options,
  String currentValue,
) {
  if (currentValue.isEmpty ||
      options.any((option) => option.value == currentValue)) {
    return options;
  }
  return [
    NotificationFieldChoice(
      value: currentValue,
      label: '$currentValue（当前值）',
    ),
    ...options,
  ];
}

List<NotificationFieldOption> parseNotificationFields(
  dynamic rawFields, {
  Set<String> requiredKeys = const {},
}) {
  final entries = <Map<String, dynamic>>[];
  if (rawFields is List) {
    for (final rawField in rawFields) {
      if (rawField is Map) {
        entries.add(Map<String, dynamic>.from(rawField));
      } else if (rawField is String && rawField.trim().isNotEmpty) {
        entries.add({'name': rawField});
      }
    }
  } else if (rawFields is Map) {
    for (final entry in rawFields.entries) {
      if (entry.value is Map) {
        entries.add({
          ...Map<String, dynamic>.from(entry.value as Map),
          'key': entry.key.toString(),
        });
      } else {
        entries.add({
          'key': entry.key.toString(),
          if (entry.value != null) 'label': entry.value.toString(),
        });
      }
    }
  }

  return entries.map((field) {
    final key = (field['key'] ?? field['name'])?.toString().trim() ?? '';
    if (key.isEmpty) return null;
    final label = field['label']?.toString().trim();
    final rawRequired = field['required'];
    return NotificationFieldOption(
      key: key,
      label: label == null || label.isEmpty ? key : label,
      type: (field['type'] ?? field['input'] ?? 'text').toString(),
      hint: (field['hint'] ?? field['placeholder'] ?? '').toString(),
      required: requiredKeys.contains(key) || _configBool(rawRequired),
      options: _parseFieldChoices(
        field['options'] ?? field['choices'] ?? field['enum'],
      ),
    );
  }).whereType<NotificationFieldOption>().toList();
}

List<NotificationFieldChoice> _parseFieldChoices(dynamic rawOptions) {
  if (rawOptions is Map) {
    return rawOptions.entries
        .map(
          (entry) => NotificationFieldChoice(
            value: entry.key.toString(),
            label: entry.value?.toString() ?? entry.key.toString(),
          ),
        )
        .toList();
  }
  if (rawOptions is! List) return const [];
  return rawOptions.map((option) {
    if (option is Map) {
      final value = option['value'] ?? option['key'] ?? option['name'];
      if (value == null) return null;
      return NotificationFieldChoice(
        value: value.toString(),
        label: (option['label'] ?? option['name'] ?? value).toString(),
      );
    }
    return NotificationFieldChoice(
      value: option.toString(),
      label: option.toString(),
    );
  }).whereType<NotificationFieldChoice>().toList();
}

const List<NotificationTypeOption> _fallbackTypes = [
  NotificationTypeOption(type: 'webhook', name: 'Webhook'),
  NotificationTypeOption(type: 'email', name: '邮件'),
  NotificationTypeOption(type: 'telegram', name: 'Telegram'),
  NotificationTypeOption(type: 'dingtalk', name: '钉钉'),
  NotificationTypeOption(type: 'wecom', name: '企业微信机器人'),
  NotificationTypeOption(type: 'wecom_app', name: '企业微信应用'),
  NotificationTypeOption(type: 'bark', name: 'Bark'),
  NotificationTypeOption(type: 'pushplus', name: 'PushPlus'),
  NotificationTypeOption(type: 'serverchan', name: 'Server酱'),
  NotificationTypeOption(type: 'feishu', name: '飞书'),
  NotificationTypeOption(type: 'gotify', name: 'Gotify'),
  NotificationTypeOption(type: 'pushdeer', name: 'PushDeer'),
  NotificationTypeOption(type: 'pushme', name: 'PushMe'),
  NotificationTypeOption(type: 'chanify', name: 'Chanify'),
  NotificationTypeOption(type: 'igot', name: 'iGot'),
  NotificationTypeOption(type: 'qmsg', name: 'Qmsg'),
  NotificationTypeOption(type: 'pushover', name: 'Pushover'),
  NotificationTypeOption(type: 'discord', name: 'Discord'),
  NotificationTypeOption(type: 'slack', name: 'Slack'),
  NotificationTypeOption(type: 'ntfy', name: 'ntfy'),
  NotificationTypeOption(type: 'wxpusher', name: 'WxPusher'),
  NotificationTypeOption(type: 'custom', name: '自定义'),
];

class NotificationListState {
  final List<NotifyChannel> items;
  final bool loading;
  final bool typesLoading;
  final List<NotificationTypeOption> types;
  final String? error;

  const NotificationListState({
    this.items = const [],
    this.loading = false,
    this.typesLoading = false,
    this.types = const [],
    this.error,
  });

  NotificationListState copyWith({
    List<NotifyChannel>? items,
    bool? loading,
    bool? typesLoading,
    List<NotificationTypeOption>? types,
    String? error,
    bool clearError = false,
  }) {
    return NotificationListState(
      items: items ?? this.items,
      loading: loading ?? this.loading,
      typesLoading: typesLoading ?? this.typesLoading,
      types: types ?? this.types,
      error: clearError ? null : error ?? this.error,
    );
  }
}

class NotificationListNotifier extends StateNotifier<NotificationListState> {
  NotificationListNotifier() : super(const NotificationListState());

  String? _scope;
  int _loadRequestId = 0;
  int _typesRequestId = 0;

  String _beginScope() {
    final scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    if (_scope != scope) {
      _scope = scope;
      _loadRequestId++;
      _typesRequestId++;
      state = const NotificationListState();
    }
    return scope;
  }

  Future<void> load() async {
    final scope = _beginScope();
    final requestId = ++_loadRequestId;
    state = state.copyWith(loading: true, clearError: true);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.notifications,
      );
      final paginated = extractPaginated(response.data);
      final items = paginated.items
          .map((e) => NotifyChannel.fromJson(e))
          .toList();
      if (requestId != _loadRequestId ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(items: items, loading: false, clearError: true);
    } catch (error) {
      if (requestId != _loadRequestId ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '通知渠道加载失败'),
      );
    }
  }

  Future<void> loadTypes() async {
    final scope = _beginScope();
    final requestId = ++_typesRequestId;
    state = state.copyWith(typesLoading: true);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.notificationTypes,
      );
      final typeData = extractData(response.data);
      final types = typeData is List
          ? typeData
                .whereType<Map>()
                .map(
                  (e) => NotificationTypeOption.fromJson(
                    Map<String, dynamic>.from(e),
                  ),
                )
                .where((option) => option.type.isNotEmpty)
                .toList()
          : <NotificationTypeOption>[];

      if (requestId != _typesRequestId ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(
        typesLoading: false,
        types: types.isNotEmpty ? types : _fallbackTypes,
      );
    } catch (_) {
      if (requestId != _typesRequestId ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(
        typesLoading: false,
        types: state.types.isNotEmpty ? state.types : _fallbackTypes,
      );
    }
  }

  Future<void> toggle(int id, bool enabled) async {
    final dio = DioClient.instance.dio;
    if (enabled) {
      await dio.put(ApiEndpoints.notificationEnable(id));
    } else {
      await dio.put(ApiEndpoints.notificationDisable(id));
    }
    await load();
  }

  Future<void> test(int id) async {
    await DioClient.instance.dio.post(ApiEndpoints.notificationTest(id));
  }

  Future<void> delete(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.notificationById(id));
    await load();
  }

  Future<void> create(Map<String, dynamic> data) async {
    await DioClient.instance.dio.post(ApiEndpoints.notifications, data: data);
    await load();
  }

  Future<void> update(int id, Map<String, dynamic> data) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.notificationById(id),
      data: data,
    );
    await load();
  }
}

class NotificationListPage extends ConsumerStatefulWidget {
  const NotificationListPage({super.key});

  @override
  ConsumerState<NotificationListPage> createState() =>
      _NotificationListPageState();
}

class _NotificationListPageState extends ConsumerState<NotificationListPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(() {
      final notifier = ref.read(notificationListProvider.notifier);
      notifier.load();
      notifier.loadTypes();
    });
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(notificationListProvider);
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 12),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                children: [
                  GestureDetector(
                    onTap: () => context.pop(),
                    child: const Icon(Icons.arrow_back_ios, size: 20),
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      '通知渠道',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  Padding(
                    padding: const EdgeInsets.only(right: 4),
                    child: AppGlassIconButton(
                      icon: Icons.send_outlined,
                      iconSize: 18,
                      tooltip: '发送通知',
                      onTap: () => _showSendDialog(state.items),
                    ),
                  ),
                  AppGlassIconButton(
                    icon: Icons.add,
                    tooltip: '新建通知渠道',
                    onTap: () => _showChannelDialog(),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: () async {
                  final notifier = ref.read(notificationListProvider.notifier);
                  await Future.wait([notifier.load(), notifier.loadTypes()]);
                },
                child: state.loading && state.items.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: const [
                          SizedBox(height: 120),
                          Center(
                            child: CircularProgressIndicator(
                              color: AppColors.primary,
                            ),
                          ),
                        ],
                      )
                    : state.error != null && state.items.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.symmetric(horizontal: 32),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.cloud_off_outlined,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          Text(
                            state.error!,
                            textAlign: TextAlign.center,
                            style: const TextStyle(color: AppColors.slate400),
                          ),
                          const SizedBox(height: 16),
                          Center(
                            child: AppLiquidGlassButton(
                              label: '重试',
                              onPressed: () => ref
                                  .read(notificationListProvider.notifier)
                                  .load(),
                            ),
                          ),
                        ],
                      )
                    : state.items.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.notifications_off,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无通知渠道',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                        itemCount:
                            state.items.length + (state.error == null ? 0 : 1),
                        itemBuilder: (_, i) {
                          if (state.error != null && i == 0) {
                            return _InlineLoadError(
                              message: state.error!,
                              onRetry: () => ref
                                  .read(notificationListProvider.notifier)
                                  .load(),
                            );
                          }
                          final itemIndex = i - (state.error == null ? 0 : 1);
                          final channel = state.items[itemIndex];
                          return _ChannelCard(
                            channel: channel,
                            typeLabel: _typeName(state.types, channel.type),
                            isLight: isLight,
                            onEdit: () => _showChannelDialog(channel: channel),
                            onToggle: () => ref
                                .read(notificationListProvider.notifier)
                                .toggle(channel.id, !channel.enabled),
                            onTest: () => _doTest(channel),
                            onDelete: () => _confirmDelete(channel),
                          );
                        },
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _showSendDialog(List<NotifyChannel> channels) {
    final titleC = TextEditingController(text: '呆呆面板通知');
    final contentC = TextEditingController();
    final selectedIds = <int>{};
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) {
          final enabledChannels = channels.where((c) => c.enabled).toList();
          final navigator = Navigator.of(ctx);
          return Padding(
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Text(
                    '发送通知',
                    style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                  ),
                  const SizedBox(height: 16),
                  TextField(
                    controller: titleC,
                    decoration: const InputDecoration(labelText: '标题'),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: contentC,
                    minLines: 4,
                    maxLines: 8,
                    decoration: const InputDecoration(
                      labelText: '正文',
                      hintText: '输入要发送的通知内容',
                    ),
                  ),
                  const SizedBox(height: 12),
                  const Text(
                    '发送渠道（留空则发送到全部已启用渠道）',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                  ),
                  const SizedBox(height: 8),
                  if (enabledChannels.isEmpty)
                    const Text(
                      '暂无已启用渠道',
                      style: TextStyle(fontSize: 12, color: AppColors.slate400),
                    )
                  else
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: enabledChannels.map((channel) {
                        return AppLiquidGlassChoiceChip(
                          label: channel.name,
                          selected: selectedIds.contains(channel.id),
                          onSelected: (selected) {
                            setSheetState(() {
                              if (selected) {
                                selectedIds.add(channel.id);
                              } else {
                                selectedIds.remove(channel.id);
                              }
                            });
                          },
                        );
                      }).toList(),
                    ),
                  const SizedBox(height: 20),
                  SizedBox(
                    width: double.infinity,
                    child: AppLiquidGlassButton(
                      label: '发送',
                      icon: Icons.send_outlined,
                      height: 44,
                      performanceMode: true,
                      onPressed: () async {
                        final title = titleC.text.trim();
                        final content = contentC.text.trim();
                        if (title.isEmpty || content.isEmpty) {
                          AppGlassNotice.show(
                            context,
                            '请输入标题和正文',
                            type: AppGlassNoticeType.warning,
                          );
                          return;
                        }
                        try {
                          final data = <String, dynamic>{
                            'title': title,
                            'content': content,
                            if (selectedIds.isNotEmpty)
                              'channel_ids': selectedIds.toList(),
                          };
                          final resp = await DioClient.instance.dio.post(
                            ApiEndpoints.notificationSend,
                            data: data,
                          );
                          final message = resp.data is Map
                              ? resp.data['message']?.toString()
                              : null;
                          if (!mounted) return;
                          navigator.pop();
                          AppGlassNotice.show(
                            context,
                            message ?? '通知已发送',
                            type: AppGlassNoticeType.success,
                          );
      } catch (error) {
        if (!mounted) return;
        AppGlassNotice.show(
                            context,
                            _extractMessage(error, '通知发送失败'),
                            type: AppGlassNoticeType.error,
                          );
                        }
                      },
                    ),
                  ),
                  const SizedBox(height: 8),
                  SizedBox(
                    width: double.infinity,
                    child: AppLiquidGlassButton(
                      label: '取消',
                      height: 44,
                      variant: AppLiquidGlassButtonVariant.secondary,
                      performanceMode: true,
                      onPressed: () => navigator.pop(),
                    ),
                  ),
                ],
              ),
            ),
          );
        },
      ),
    );
  }

  Future<void> _doTest(NotifyChannel channel) async {
    try {
      await ref.read(notificationListProvider.notifier).test(channel.id);
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '测试通知已发送',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        _extractMessage(error, '测试发送失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _confirmDelete(NotifyChannel channel) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除通知渠道'),
        content: Text('确定要删除「${channel.name}」吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogContext, false),
              ),
              AppGlassDialogAction(
                label: '删除',
                onPressed: () => Navigator.pop(dialogContext, true),
                variant: AppLiquidGlassButtonVariant.danger,
              ),
            ],
          ),
        ],
      ),
    );
    if (confirm == true) {
      try {
        await ref.read(notificationListProvider.notifier).delete(channel.id);
      } catch (error) {
        if (!mounted) {
          return;
        }
        AppGlassNotice.show(
          context,
          _extractMessage(error, '删除失败'),
          type: AppGlassNoticeType.error,
        );
      }
    }
  }

  static const _channelFieldMap =
      <String, List<({String key, String label, String hint, bool obscure})>>{
        'webhook': [
          (
            key: 'url',
            label: 'Webhook URL',
            hint: 'https://example.com/webhook',
            obscure: false,
          ),
        ],
        'email': [
          (
            key: 'smtp_host',
            label: 'SMTP 主机',
            hint: 'smtp.qq.com',
            obscure: false,
          ),
          (key: 'smtp_port', label: 'SMTP 端口', hint: '465', obscure: false),
          (
            key: 'smtp_user',
            label: '邮箱账号',
            hint: 'user@example.com',
            obscure: false,
          ),
          (
            key: 'smtp_pass',
            label: '邮箱密码/授权码',
            hint: 'SMTP 授权码',
            obscure: true,
          ),
          (key: 'to', label: '收件人', hint: '多个收件人用逗号分隔', obscure: false),
        ],
        'telegram': [
          (
            key: 'token',
            label: 'Bot Token',
            hint: '从 @BotFather 获取',
            obscure: false,
          ),
          (key: 'chat_id', label: 'Chat ID', hint: '聊天/群组 ID', obscure: false),
          (
            key: 'api_host',
            label: 'API 地址 (可选)',
            hint: '留空使用官方',
            obscure: false,
          ),
        ],
        'dingtalk': [
          (
            key: 'webhook',
            label: 'Webhook URL',
            hint: 'https://oapi.dingtalk.com/robot/send?access_token=xxx',
            obscure: false,
          ),
          (
            key: 'secret',
            label: '加签秘钥 (可选)',
            hint: 'SEC 开头的秘钥',
            obscure: false,
          ),
        ],
        'wecom': [
          (
            key: 'webhook',
            label: 'Webhook URL',
            hint: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx',
            obscure: false,
          ),
        ],
        'wecom_app': [
          (key: 'corp_id', label: '企业 ID', hint: 'CorpID', obscure: false),
          (key: 'secret', label: '应用 Secret', hint: 'Secret', obscure: true),
          (key: 'agent_id', label: 'Agent ID', hint: 'AgentId', obscure: false),
          (
            key: 'to_user',
            label: '成员账号 (可选)',
            hint: '多个成员用 | 分隔，留空 @all',
            obscure: false,
          ),
        ],
        'bark': [
          (
            key: 'key',
            label: 'Device Key',
            hint: 'Bark App 中的 Key',
            obscure: false,
          ),
          (
            key: 'server',
            label: '服务器 (可选)',
            hint: '默认 https://api.day.app',
            obscure: false,
          ),
          (
            key: 'sound',
            label: '推送声音 (可选)',
            hint: '如 birdsong',
            obscure: false,
          ),
          (key: 'group', label: '推送分组 (可选)', hint: '消息分组名称', obscure: false),
        ],
        'pushplus': [
          (
            key: 'token',
            label: 'Token',
            hint: 'PushPlus 用户 Token',
            obscure: false,
          ),
          (
            key: 'topic',
            label: '群组编码 (可选)',
            hint: '一对多推送时的群组编码',
            obscure: false,
          ),
        ],
        'serverchan': [
          (key: 'key', label: 'SendKey', hint: 'SCT...', obscure: false),
        ],
        'feishu': [
          (
            key: 'webhook',
            label: 'Webhook URL',
            hint: 'https://open.feishu.cn/open-apis/bot/v2/hook/xxx',
            obscure: false,
          ),
          (key: 'secret', label: '加签秘钥 (可选)', hint: '签名校验秘钥', obscure: false),
        ],
        'gotify': [
          (
            key: 'server',
            label: '服务器地址',
            hint: 'https://gotify.example.com',
            obscure: false,
          ),
          (
            key: 'token',
            label: 'App Token',
            hint: 'Gotify 应用 Token',
            obscure: false,
          ),
        ],
        'pushdeer': [
          (
            key: 'key',
            label: 'PushKey',
            hint: 'PushDeer 的 PushKey',
            obscure: false,
          ),
          (
            key: 'server',
            label: '服务器 (可选)',
            hint: '默认 https://api2.pushdeer.com',
            obscure: false,
          ),
        ],
        'pushme': [
          (key: 'key', label: 'PushMe Key', hint: 'push_key', obscure: false),
        ],
        'chanify': [
          (
            key: 'token',
            label: 'Token',
            hint: 'Chanify 设备 Token',
            obscure: false,
          ),
        ],
        'igot': [
          (key: 'key', label: 'Key', hint: 'iGot 推送 Key', obscure: false),
        ],
        'qmsg': [
          (key: 'key', label: 'Qmsg Key', hint: 'Qmsg 酱的 Key', obscure: false),
          (key: 'qq', label: 'QQ 号/群号 (可选)', hint: '留空按默认配置发送', obscure: false),
        ],
        'pushover': [
          (
            key: 'token',
            label: 'API Token',
            hint: '应用 API Token',
            obscure: false,
          ),
          (key: 'user', label: 'User Key', hint: '用户 Key', obscure: false),
        ],
        'discord': [
          (
            key: 'webhook',
            label: 'Webhook URL',
            hint: 'https://discord.com/api/webhooks/...',
            obscure: false,
          ),
        ],
        'slack': [
          (
            key: 'webhook',
            label: 'Webhook URL',
            hint: 'https://hooks.slack.com/services/...',
            obscure: false,
          ),
        ],
        'ntfy': [
          (key: 'topic', label: 'Topic', hint: '订阅主题名称', obscure: false),
          (
            key: 'server',
            label: '服务器 (可选)',
            hint: '默认 https://ntfy.sh',
            obscure: false,
          ),
          (key: 'token', label: 'Token (可选)', hint: '访问令牌', obscure: false),
        ],
        'wxpusher': [
          (
            key: 'app_token',
            label: 'App Token',
            hint: 'WxPusher 的 appToken',
            obscure: false,
          ),
          (
            key: 'uids',
            label: 'UID 列表 (可选)',
            hint: '多个 UID 用逗号分隔',
            obscure: false,
          ),
          (
            key: 'topic_ids',
            label: 'Topic ID (可选)',
            hint: '多个 ID 用逗号分隔',
            obscure: false,
          ),
        ],
      };

  void _showChannelDialog({NotifyChannel? channel}) {
    final nameController = TextEditingController(text: channel?.name ?? '');
    final existingConfig = Map<String, dynamic>.from(channel?.config ?? {});
    final fieldControllers = <String, TextEditingController>{};
    final credentialVisibility = <String, bool>{};
    var smtpSsl = readSmtpSslMode(existingConfig);

    final loadedTypes = ref.read(notificationListProvider).types.isNotEmpty
        ? ref.read(notificationListProvider).types
        : _fallbackTypes;
    final availableTypes = <NotificationTypeOption>[
      if (channel != null &&
          loadedTypes.every((option) => option.type != channel.type))
        NotificationTypeOption(type: channel.type, name: channel.type),
      ...loadedTypes,
    ];
    String selectedType = channel?.type ?? availableTypes.first.type;

    void disposeFieldControllers() {
      for (final c in fieldControllers.values) {
        c.dispose();
      }
      fieldControllers.clear();
    }

    TextEditingController getFieldController(
      String key, {
      bool credential = false,
    }) {
      final keepsExistingConfig = channel != null && selectedType == channel.type;
      final activeConfig = keepsExistingConfig
          ? existingConfig
          : const <String, dynamic>{};
      return fieldControllers.putIfAbsent(
        key,
        () => TextEditingController(
          text: key == '__raw_json__'
              ? const JsonEncoder.withIndent('  ').convert(activeConfig)
              : credential && keepsExistingConfig
              ? ''
              : activeConfig[key]?.toString() ?? '',
        ),
      );
    }

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) {
        return StatefulBuilder(
          builder: (ctx, setSheetState) {
            final keepsExistingConfig =
                channel != null && selectedType == channel.type;
            final activeConfig = keepsExistingConfig
                ? existingConfig
                : const <String, dynamic>{};
            final selectedOption = availableTypes.firstWhere(
              (option) => option.type == selectedType,
            );
            final fallbackFields = _fallbackFieldsFor(selectedType);
            final fieldDefinitions = selectedOption.hasFieldsSchema
                ? selectedOption.fields
                : [
                    ...fallbackFields,
                    if (fallbackFields.isEmpty && keepsExistingConfig)
                      ...activeConfig.keys.map(
                        (key) => NotificationFieldOption(
                          key: key,
                          label: key,
                        ),
                      ),
                  ];
            final usesFieldForm =
                selectedOption.hasFieldsSchema || fieldDefinitions.isNotEmpty;
            final supportsSmtpSsl = selectedType == 'email' &&
                (!selectedOption.hasFieldsSchema ||
                    selectedOption.fields.any(
                      (field) => smtpSslAliases.contains(field.key),
                    ));
            final fields = fieldDefinitions
                .where(
                  (field) =>
                      selectedType != 'email' ||
                      !smtpSslAliases.contains(field.key),
                )
                .toList();
            return Padding(
              padding: EdgeInsets.fromLTRB(
                20,
                0,
                20,
                MediaQuery.of(ctx).viewInsets.bottom + 20,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      channel == null ? '新建通知渠道' : '编辑通知渠道',
                      style: Theme.of(ctx).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 16),
                    TextField(
                      controller: nameController,
                      decoration: const InputDecoration(
                        labelText: '渠道名称',
                        hintText: '如：我的Bark',
                      ),
                    ),
                    const SizedBox(height: 12),
                    DropdownButtonFormField<String>(
                      initialValue: selectedType,
                      decoration: const InputDecoration(labelText: '渠道类型'),
                      items: availableTypes
                          .map(
                            (item) => DropdownMenuItem(
                              value: item.type,
                              child: Text(item.name),
                            ),
                          )
                          .toList(),
                      onChanged: (value) {
                        if (value != null) {
                          setSheetState(() {
                            disposeFieldControllers();
                            selectedType = value;
                          });
                        }
                      },
                    ),
                    if (fields.isNotEmpty || supportsSmtpSsl) ...[
                      const SizedBox(height: 16),
                      const Divider(height: 1),
                      const SizedBox(height: 12),
                      ...fields.map(
                        (f) => Padding(
                          padding: const EdgeInsets.only(bottom: 12),
                          child: f.options.isNotEmpty
                              ? Builder(
                                  builder: (context) {
                                    final controller = getFieldController(
                                      f.key,
                                      credential: f.isCredential,
                                    );
                                    final choices = notificationFieldChoices(
                                      f.options,
                                      controller.text,
                                    );
                                    return DropdownButtonFormField<String>(
                                      initialValue: controller.text.isEmpty
                                          ? null
                                          : controller.text,
                                      decoration: InputDecoration(
                                        labelText: f.required
                                            ? '${f.label} *'
                                            : f.label,
                                        hintText: f.hint,
                                      ),
                                      items: choices
                                          .map(
                                            (option) => DropdownMenuItem(
                                              value: option.value,
                                              child: Text(option.label),
                                            ),
                                          )
                                          .toList(),
                                      onChanged: (value) =>
                                          controller.text = value ?? '',
                                    );
                                  },
                                )
                              : TextField(
                                  controller: getFieldController(
                                    f.key,
                                    credential: f.isCredential,
                                  ),
                                  obscureText:
                                      f.isCredential &&
                                      !(credentialVisibility[f.key] ?? false),
                                  decoration: InputDecoration(
                                    labelText: f.required
                                        ? '${f.label} *'
                                        : f.label,
                                    hintText: f.hint,
                                    suffixIcon: f.isCredential
                                        ? IconButton(
                                            onPressed: () {
                                              setSheetState(() {
                                                credentialVisibility[f.key] =
                                                    !(credentialVisibility[
                                                          f.key
                                                        ] ??
                                                        false);
                                              });
                                            },
                                            icon: Icon(
                                              credentialVisibility[f.key] ??
                                                      false
                                                  ? Icons.visibility_off_outlined
                                                  : Icons.visibility_outlined,
                                            ),
                                          )
                                        : null,
                                  ),
                                ),
                        ),
                      ),
                      if (supportsSmtpSsl)
                        DropdownButtonFormField<SmtpSslMode>(
                          initialValue: smtpSsl,
                          decoration: const InputDecoration(
                            labelText: 'SMTP SSL',
                            helperText: '自动判断，或明确开启/关闭',
                          ),
                          items: const [
                            DropdownMenuItem(
                              value: SmtpSslMode.auto,
                              child: Text('自动'),
                            ),
                            DropdownMenuItem(
                              value: SmtpSslMode.on,
                              child: Text('开启'),
                            ),
                            DropdownMenuItem(
                              value: SmtpSslMode.off,
                              child: Text('关闭'),
                            ),
                          ],
                          onChanged: (value) {
                            if (value != null) {
                              setSheetState(() => smtpSsl = value);
                            }
                          },
                        ),
                    ],
                    if (!usesFieldForm) ...[
                      const SizedBox(height: 12),
                      TextField(
                        controller: getFieldController('__raw_json__'),
                        minLines: 5,
                        maxLines: 10,
                        decoration: const InputDecoration(
                          labelText: '配置 JSON',
                          alignLabelWithHint: true,
                          hintText: '{"key": "value"}',
                        ),
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 13,
                        ),
                      ),
                    ],
                    const SizedBox(height: 20),
                    AppLiquidGlassButton(
                      label: channel == null ? '创建' : '保存',
                      width: double.infinity,
                      performanceMode: true,
                      onPressed: () async {
                        final name = nameController.text.trim();
                        if (name.isEmpty) {
                          AppGlassNotice.show(
                             context,
                            '名称不能为空',
                            type: AppGlassNoticeType.warning,
                          );
                          return;
                        }

                        Map<String, dynamic> configMap;
                        if (usesFieldForm) {
                          final missingRequired = fields.where(
                            (field) =>
                                field.required &&
                                getFieldController(
                                  field.key,
                                  credential: field.isCredential,
                                ).text.trim().isEmpty &&
                                !(field.isCredential &&
                                    keepsExistingConfig &&
                                    (activeConfig[field.key]
                                            ?.toString()
                                            .trim()
                                            .isNotEmpty ??
                                        false)),
                          );
                          if (missingRequired.isNotEmpty) {
                            AppGlassNotice.show(
                              context,
                              '请填写${missingRequired.first.label}',
                              type: AppGlassNoticeType.warning,
                            );
                            return;
                          }
                          final fieldValues = <String, dynamic>{};
                          for (final f in fields) {
                            fieldValues[f.key] = getFieldController(
                              f.key,
                              credential: f.isCredential,
                            ).text.trim();
                          }
                          if (supportsSmtpSsl) {
                            fieldValues['smtp_ssl'] = smtpSsl.name;
                          }
                          configMap = mergeNotificationConfig(
                            existingConfig: activeConfig,
                            fieldValues: fieldValues,
                            removeKeys: supportsSmtpSsl
                                ? smtpSslAliases
                                : const [],
                            preserveEmptyKeys: !keepsExistingConfig
                                ? const []
                                : fields
                                      .where((field) => field.isCredential)
                                      .map((field) => field.key),
                          );
                        } else {
                          final raw = getFieldController(
                            '__raw_json__',
                          ).text.trim();
                          final parsed = _parseConfig(raw.isEmpty ? '{}' : raw);
                          if (parsed == null) {
                            AppGlassNotice.show(
                               context,
                              '配置 JSON 格式错误',
                              type: AppGlassNoticeType.error,
                            );
                            return;
                          }
                          configMap = parsed;
                        }

                        configMap = stringifyNotificationConfig(configMap);

                        final payload = {
                          'name': name,
                          'type': selectedType,
                          'config': jsonEncode(configMap),
                        };

                        try {
                          if (channel == null) {
                            await ref
                                .read(notificationListProvider.notifier)
                                .create(payload);
                          } else {
                            await ref
                                .read(notificationListProvider.notifier)
                                .update(channel.id, payload);
                          }
                          if (!mounted || !ctx.mounted) return;
                          Navigator.of(ctx).pop();
                          AppGlassNotice.show(
                             context,
                            channel == null ? '创建成功' : '保存成功',
                            type: AppGlassNoticeType.success,
                          );
                        } catch (error) {
                          if (!mounted) return;
                          AppGlassNotice.show(
                             context,
                            _extractMessage(
                              error,
                              channel == null ? '创建失败' : '保存失败',
                            ),
                            type: AppGlassNoticeType.error,
                          );
                        }
                      },
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    ).then((_) {
      nameController.dispose();
      disposeFieldControllers();
    });
  }

  List<NotificationFieldOption> _fallbackFieldsFor(String type) {
    return (_channelFieldMap[type] ?? const [])
        .map(
          (field) => NotificationFieldOption(
            key: field.key,
            label: field.label,
            hint: field.hint,
            type: field.obscure ? 'password' : 'text',
          ),
        )
        .toList();
  }
}

class _InlineLoadError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _InlineLoadError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: AppCard(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
        child: Row(
          children: [
            const Icon(Icons.sync_problem_outlined, color: AppColors.amber500),
            const SizedBox(width: 10),
            Expanded(
              child: Text(message, style: const TextStyle(fontSize: 13)),
            ),
            TextButton(onPressed: onRetry, child: const Text('重试')),
          ],
        ),
      ),
    );
  }
}

class _ChannelCard extends StatelessWidget {
  final NotifyChannel channel;
  final String typeLabel;
  final bool isLight;
  final VoidCallback onEdit;
  final VoidCallback onToggle;
  final VoidCallback onTest;
  final VoidCallback onDelete;

  const _ChannelCard({
    required this.channel,
    required this.typeLabel,
    required this.isLight,
    required this.onEdit,
    required this.onToggle,
    required this.onTest,
    required this.onDelete,
  });

  IconData _typeIcon() {
    switch (channel.type) {
      case 'email':
        return Icons.email_outlined;
      case 'telegram':
        return Icons.send;
      case 'dingtalk':
        return Icons.chat;
      case 'wecom':
      case 'wecom_app':
        return Icons.business;
      case 'bark':
        return Icons.phone_iphone;
      default:
        return Icons.webhook;
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: AppCard(
        stableForScrolling: true,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        child: Row(
        children: [
          Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(
              color: channel.enabled
                  ? AppColors.primary.withAlpha(25)
                  : AppColors.slate200.withAlpha(60),
              borderRadius: BorderRadius.circular(8),
            ),
            child: Icon(
              _typeIcon(),
              size: 18,
              color: channel.enabled ? AppColors.primary : AppColors.slate400,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  channel.name,
                  style: const TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
                const SizedBox(height: 4),
                Text(
                  typeLabel,
                  style: TextStyle(
                    fontSize: 12,
                    color: isLight ? AppColors.slate500 : AppColors.slate400,
                  ),
                ),
              ],
            ),
          ),
          GestureDetector(
            onTap: onTest,
            child: const Padding(
              padding: EdgeInsets.all(6),
              child: Icon(Icons.send, size: 16, color: AppColors.blue500),
            ),
          ),
          GestureDetector(
            onTap: onEdit,
            child: const Padding(
              padding: EdgeInsets.all(6),
              child: Icon(
                Icons.edit_outlined,
                size: 18,
                color: AppColors.blue500,
              ),
            ),
          ),
          GestureDetector(
            onTap: onToggle,
            child: Padding(
              padding: const EdgeInsets.all(6),
              child: Icon(
                channel.enabled ? Icons.toggle_on : Icons.toggle_off,
                size: 28,
                color: channel.enabled ? AppColors.primary : AppColors.slate400,
              ),
            ),
          ),
          GestureDetector(
            onTap: onDelete,
            child: const Padding(
              padding: EdgeInsets.all(6),
              child: Icon(
                Icons.delete_outline,
                size: 18,
                color: AppColors.red500,
              ),
            ),
          ),
        ],
        ),
      ),
    );
  }
}

String _typeName(List<NotificationTypeOption> types, String type) {
  for (final item in types) {
    if (item.type == type) {
      return item.name;
    }
  }
  return type;
}

Map<String, dynamic>? _parseConfig(String raw) {
  final text = raw.trim();
  if (text.isEmpty) {
    return <String, dynamic>{};
  }

  try {
    final decoded = jsonDecode(text);
    if (decoded is Map<String, dynamic>) {
      return decoded;
    }
    if (decoded is Map) {
      return decoded.map((key, value) => MapEntry(key.toString(), value));
    }
  } catch (_) {}

  return null;
}

enum SmtpSslMode { auto, on, off }

const smtpSslAliases = [
  'smtp_ssl',
  'smtp_secure',
  'smtp_use_ssl',
  'use_ssl',
  'ssl',
  'secure',
];

SmtpSslMode readSmtpSslMode(Map<String, dynamic> config) {
  dynamic value;
  for (final alias in smtpSslAliases) {
    if (config.containsKey(alias)) {
      value = config[alias];
      break;
    }
  }
  final text = value?.toString().trim().toLowerCase() ?? '';
  if (text.isEmpty || text == 'auto' || text == 'default') {
    return SmtpSslMode.auto;
  }
  if (const {'true', '1', 'yes', 'on', 'enabled'}.contains(text)) {
    return SmtpSslMode.on;
  }
  if (const {'false', '0', 'no', 'off', 'disabled'}.contains(text)) {
    return SmtpSslMode.off;
  }
  return SmtpSslMode.auto;
}

Map<String, dynamic> stringifyNotificationConfig(Map<String, dynamic> config) {
  return config.map((key, value) {
    final stringValue = value is Map || value is List
        ? jsonEncode(value)
        : value?.toString() ?? '';
    return MapEntry(key, stringValue);
  });
}

Map<String, dynamic> mergeNotificationConfig({
  required Map<String, dynamic> existingConfig,
  required Map<String, dynamic> fieldValues,
  Iterable<String> removeKeys = const [],
  Iterable<String> preserveEmptyKeys = const [],
}) {
  final merged = Map<String, dynamic>.from(existingConfig);
  final preserved = preserveEmptyKeys.toSet();
  for (final key in removeKeys) {
    merged.remove(key);
  }
  for (final entry in fieldValues.entries) {
    final value = entry.value?.toString().trim() ?? '';
    if (value.isEmpty) {
      if (!preserved.contains(entry.key)) {
        merged.remove(entry.key);
      }
    } else {
      merged[entry.key] = value;
    }
  }
  return stringifyNotificationConfig(merged);
}

bool _configBool(dynamic value) {
  if (value is bool) {
    return value;
  }
  final text = value?.toString().trim().toLowerCase() ?? '';
  return text == 'true' || text == '1' || text == 'yes' || text == 'on';
}

String _extractMessage(dynamic error, String fallback) {
  try {
    final data = (error as dynamic).response?.data;
    if (data is Map && data['error'] != null) {
      return data['error'].toString();
    }
    if (data is Map && data['message'] != null) {
      return data['message'].toString();
    }
  } catch (_) {}
  return fallback;
}
