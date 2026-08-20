import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

class _ApiScopeOption {
  final String value;
  final String label;
  final String description;

  const _ApiScopeOption(this.value, this.label, this.description);
}

const _apiScopeOptions = [
  _ApiScopeOption('tasks', '任务管理', '读取与操作定时任务'),
  _ApiScopeOption('scripts', '脚本管理', '访问脚本目录和执行入口'),
  _ApiScopeOption('envs', '环境变量', '读取和维护环境变量'),
  _ApiScopeOption('subscriptions', '订阅管理', '管理订阅仓库和文件'),
  _ApiScopeOption('logs', '日志查看', '读取执行日志和流式输出'),
  _ApiScopeOption('system', '系统信息', '读取系统信息和状态数据'),
];

Future<({List<Map<String, dynamic>> items, int total})> _loadOpenApiLogPage(
  int appId,
  int page,
) async {
  final resp = await DioClient.instance.dio.get(
    ApiEndpoints.openApiAppLogs(appId),
    queryParameters: {'page': page, 'page_size': 100},
  );
  return extractPaginated(resp.data);
}

class OpenApiPage extends ConsumerStatefulWidget {
  const OpenApiPage({super.key});

  @override
  ConsumerState<OpenApiPage> createState() => _OpenApiPageState();
}

class _OpenApiPageState extends ConsumerState<OpenApiPage> {
  List<Map<String, dynamic>> _apps = [];
  bool _loading = true;
  String? _loadError;

  String _scopeLabel(String value) {
    for (final option in _apiScopeOptions) {
      if (option.value == value) {
        return option.label;
      }
    }
    return value;
  }

  List<String> _parseScopes(String scopes) {
    return scopes
        .split(',')
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList();
  }

  String _joinScopes(List<String> scopes) => scopes.join(',');

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.openApiApps);
      final data = extractData(resp.data);
      if (!mounted) return;
      setState(() {
        _apps = (data is List)
            ? data.whereType<Map<String, dynamic>>().toList()
            : [];
        _loading = false;
        _loadError = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _loadError = extractErrorMessage(error, 'Open API 应用加载失败');
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    

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
                      'Open API',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  AppGlassIconButton(
                    icon: Icons.add,
                    tooltip: '新建 Open API 应用',
                    onTap: _showCreateDialog,
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: _load,
                child: _loading && _apps.isEmpty
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
                    : _apps.isEmpty && _loadError == null
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.api_outlined,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无 API 应用',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                        children: [
                          if (_loadError != null) ...[
                            AppCard(
                              child: Row(
                                children: [
                                  const Icon(
                                    Icons.cloud_off_outlined,
                                    color: AppColors.red500,
                                  ),
                                  const SizedBox(width: 12),
                                  Expanded(child: Text(_loadError!)),
                                  TextButton(
                                    onPressed: _loading ? null : _load,
                                    child: const Text('重试'),
                                  ),
                                ],
                              ),
                            ),
                            if (_apps.isNotEmpty) const SizedBox(height: 12),
                          ],
                          for (final app in _apps)
                            _buildAppCard(app: app, isLight: isLight),
                        ],
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  void _showCreateDialog() {
    final nameC = TextEditingController();
    final rateLimitC = TextEditingController(text: '100');
    final selectedScopes = <String>{};
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) {
          final navigator = Navigator.of(ctx);
          return Padding(
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  '创建 API 应用',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: nameC,
                  decoration: const InputDecoration(labelText: '应用名称'),
                ),
                const SizedBox(height: 12),
                const Text(
                  '权限范围',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: _apiScopeOptions.map((option) {
                    return AppLiquidGlassChoiceChip(
                      label: option.label,
                      selected: selectedScopes.contains(option.value),
                      onSelected: (selected) {
                        setSheetState(() {
                          if (selected) {
                            selectedScopes.add(option.value);
                          } else {
                            selectedScopes.remove(option.value);
                          }
                        });
                      },
                    );
                  }).toList(),
                ),
                const SizedBox(height: 8),
                Text(
                  '留空表示该应用创建成功，但没有任何接口访问权限。',
                  style: TextStyle(fontSize: 11, color: AppColors.slate400),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: rateLimitC,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: '速率限制（次/小时）',
                    hintText: '默认 100',
                  ),
                ),
                const SizedBox(height: 20),
                SizedBox(
                  width: double.infinity,
                  child: AppLiquidGlassButton(
                    label: '创建',
                    height: 44,
                    performanceMode: true,
                    onPressed: () async {
                      if (nameC.text.trim().isEmpty) {
                        return;
                      }
                      try {
                        final resp = await DioClient.instance.dio.post(
                          ApiEndpoints.openApiApps,
                          data: {
                            'name': nameC.text.trim(),
                            'scopes': _joinScopes(selectedScopes.toList()),
                            'rate_limit':
                                int.tryParse(rateLimitC.text.trim()) ?? 100,
                          },
                        );
                        if (!mounted || !ctx.mounted) {
                          return;
                        }
                        navigator.pop();
                        await _load();
                        if (!mounted) return;
                        final data = extractData(resp.data);
                        if (data is Map && data['app_secret'] != null) {
                          _showSecretDialog(
                            data['app_key']?.toString() ?? '',
                            data['app_secret'].toString(),
                          );
                        }
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          context,
                          extractErrorMessage(error, '创建 API 应用失败'),
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
                    onPressed: () => Navigator.of(ctx).pop(),
                  ),
                ),
              ],
            ),
          );
        },
      ),
    );
  }

  void _showSecretDialog(String appKey, String appSecret) {
    showDialog(
      context: context,
      barrierDismissible: false,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('应用密钥'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '请妥善保管以下密钥，关闭后无法再次查看：',
              style: TextStyle(fontSize: 12, color: AppColors.red500),
            ),
            const SizedBox(height: 12),
            _CopyableField(
              label: 'App Key',
              value: appKey,
              noticeContext: context,
            ),
            const SizedBox(height: 8),
            _CopyableField(
              label: 'App Secret',
              value: appSecret,
              noticeContext: context,
            ),
          ],
        ),
        actions: [
          SizedBox(
            width: double.infinity,
            height: 44,
            child: FilledButton(
              onPressed: () => Navigator.pop(dialogCtx),
              child: const Text('我已保存'),
            ),
          ),
        ],
      ),
    );
  }

  void _showTokenRequestDialog(String appKey) {
    final secretC = TextEditingController();
    showDialog(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('获取访问 Token'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text(
              '输入 App Secret 后获取 24 小时有效的 Open API 访问 Token。',
              style: TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 12),
            _CopyableField(
              label: 'App Key',
              value: appKey,
              noticeContext: context,
            ),
            const SizedBox(height: 12),
            TextField(
              controller: secretC,
              obscureText: true,
              decoration: const InputDecoration(labelText: 'App Secret'),
            ),
          ],
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx),
              ),
              AppGlassDialogAction(
                label: '获取',
                variant: AppLiquidGlassButtonVariant.primary,
                onPressed: () async {
                  final secret = secretC.text.trim();
                  if (secret.isEmpty) {
                    return;
                  }
                  try {
                    final resp = await DioClient.instance.dio.post(
                      ApiEndpoints.openApiToken,
                      data: {'app_key': appKey, 'app_secret': secret},
                    );
                    final data = extractData(resp.data);
                    if (!mounted || !dialogCtx.mounted) return;
                    Navigator.pop(dialogCtx);
                    if (data is Map) {
                      _showAccessTokenDialog(Map<String, dynamic>.from(data));
                    }
                  } catch (error) {
                    if (!mounted) return;
                    AppGlassNotice.show(
                      context,
                      extractErrorMessage(error, '获取访问 Token 失败'),
                      type: AppGlassNoticeType.error,
                    );
                  }
                },
              ),
            ],
          ),
        ],
      ),
    );
  }

  void _showAccessTokenDialog(Map<String, dynamic> data) {
    final token = data['access_token']?.toString() ?? '';
    final tokenType = data['token_type']?.toString() ?? 'Bearer';
    final expiresIn = data['expires_in']?.toString() ?? '86400';
    showDialog(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('访问 Token'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '类型：$tokenType · 有效期：$expiresIn 秒',
              style: const TextStyle(fontSize: 12, color: AppColors.slate500),
            ),
            const SizedBox(height: 12),
            _CopyableField(
              label: 'Access Token',
              value: token,
              noticeContext: context,
            ),
          ],
        ),
        actions: [
          SizedBox(
            width: double.infinity,
            height: 44,
            child: FilledButton(
              onPressed: () => Navigator.pop(dialogCtx),
              child: const Text('关闭'),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildAppCard({
    required Map<String, dynamic> app,
    required bool isLight,
  }) {
    final id = (app['id'] as num?)?.toInt() ?? 0;
    final name = app['name']?.toString() ?? '';
    final appKey = app['app_key']?.toString() ?? '';
    final enabled = app['enabled'] != false;
    final callCount = (app['call_count'] as num?)?.toInt() ?? 0;
    final scopes = app['scopes']?.toString() ?? '';
    final rateLimit = (app['rate_limit'] as num?)?.toInt() ?? 0;

    return AppCard(
      stableForScrolling: true,
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Text(
                          name,
                          style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(width: 6),
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 6,
                            vertical: 2,
                          ),
                          decoration: BoxDecoration(
                            color: enabled
                                ? AppColors.primary.withAlpha(25)
                                : AppColors.slate400.withAlpha(25),
                            borderRadius: BorderRadius.circular(4),
                          ),
                          child: Text(
                            enabled ? '启用' : '禁用',
                            style: TextStyle(
                              fontSize: 10,
                              fontWeight: FontWeight.w700,
                              color: enabled
                                  ? AppColors.primary
                                  : AppColors.slate400,
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 4),
                    GestureDetector(
                      onTap: () {
                        Clipboard.setData(ClipboardData(text: appKey));
                        AppGlassNotice.show(
                          context,
                          'App Key 已复制',
                          type: AppGlassNoticeType.success,
                        );
                      },
                      child: Row(
                        children: [
                          Flexible(
                            child: Text(
                              appKey,
                              style: TextStyle(
                                fontSize: 11,
                                fontFamily: 'monospace',
                                color: isLight
                                    ? AppColors.slate500
                                    : AppColors.slate400,
                              ),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                          const SizedBox(width: 4),
                          Icon(Icons.copy, size: 12, color: AppColors.slate400),
                        ],
                      ),
                    ),
                    const SizedBox(height: 6),
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: scopes.isEmpty
                          ? [
                              const Chip(
                                label: Text('未授权任何范围'),
                                visualDensity: VisualDensity.compact,
                              ),
                            ]
                          : _parseScopes(scopes)
                                .map(
                                  (scope) => Chip(
                                    label: Text(_scopeLabel(scope)),
                                    visualDensity: VisualDensity.compact,
                                  ),
                                )
                                .toList(),
                    ),
                  ],
                ),
              ),
              PopupMenuButton<String>(
                icon: Icon(
                  Icons.more_vert,
                  size: 18,
                  color: isLight ? AppColors.slate400 : AppColors.slate500,
                ),
                itemBuilder: (_) => [
                  const PopupMenuItem(value: 'edit', child: Text('编辑')),
                  const PopupMenuItem(
                    value: 'view_secret',
                    child: Text('查看密钥'),
                  ),
                  const PopupMenuItem(
                    value: 'token',
                    child: Text('获取 Token'),
                  ),
                  PopupMenuItem(
                    value: 'toggle',
                    child: Text(enabled ? '禁用' : '启用'),
                  ),
                  const PopupMenuItem(value: 'reset', child: Text('重置密钥')),
                  const PopupMenuItem(value: 'logs', child: Text('调用日志')),
                  const PopupMenuItem(
                    value: 'delete',
                    child: Text(
                      '删除',
                      style: TextStyle(color: AppColors.red500),
                    ),
                  ),
                ],
                onSelected: (v) async {
                  switch (v) {
                    case 'edit':
                      _showEditDialog(id, name, scopes, rateLimit);
                      break;
                    case 'view_secret':
                      _showViewSecretDialog(id, appKey);
                      break;
                    case 'token':
                      _showTokenRequestDialog(appKey);
                      break;
                    case 'toggle':
                      final confirm = await showDialog<bool>(
                        context: context,
                        builder: (d) => AlertDialog(
                          title: Text(enabled ? '禁用应用' : '启用应用'),
                          content: Text(
                            enabled
                                ? '确认禁用「$name」吗？禁用后该 App Key / App Secret 将立即失效。'
                                : '确认启用「$name」吗？',
                          ),
                          actions: [
                            AppLiquidGlassDialogActions(
                              actions: [
                                AppGlassDialogAction(
                                  label: '取消',
                                  onPressed: () => Navigator.pop(d, false),
                                ),
                                AppGlassDialogAction(
                                  label: enabled ? '禁用' : '启用',
                                  onPressed: () => Navigator.pop(d, true),
                                  variant: enabled
                                      ? AppLiquidGlassButtonVariant.danger
                                      : AppLiquidGlassButtonVariant.primary,
                                ),
                              ],
                            ),
                          ],
                        ),
                      );
                      if (confirm == true) {
                        try {
                          await DioClient.instance.dio.put(
                            enabled
                                ? ApiEndpoints.openApiAppDisable(id)
                                : ApiEndpoints.openApiAppEnable(id),
                          );
                          if (!mounted) {
                            return;
                          }
                          await _load();
                          if (!mounted) {
                            return;
                          }
                          AppGlassNotice.show(
                            context,
                            enabled ? '应用已禁用' : '应用已启用',
                            type: AppGlassNoticeType.success,
                          );
                        } catch (error) {
                          if (!mounted) {
                            return;
                          }
                          AppGlassNotice.show(
                            context,
                            extractErrorMessage(
                              error,
                              enabled ? '禁用应用失败' : '启用应用失败',
                            ),
                            type: AppGlassNoticeType.error,
                          );
                        }
                      }
                      break;
                    case 'reset':
                      try {
                        final resp = await DioClient.instance.dio.put(
                          ApiEndpoints.openApiAppResetSecret(id),
                        );
                        final data = extractData(resp.data);
                        if (!mounted) return;
                        if (data is Map && data['app_secret'] != null) {
                          _showSecretDialog(
                            appKey,
                            data['app_secret'].toString(),
                          );
                        }
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          context,
                          extractErrorMessage(error, '重置密钥失败'),
                          type: AppGlassNoticeType.error,
                        );
                      }
                      break;
                    case 'logs':
                      context.push('/open-api/$id/logs');
                      break;
                    case 'delete':
                      final confirm = await showDialog<bool>(
                        context: context,
                        builder: (d) => AlertDialog(
                          title: const Text('删除应用'),
                          content: Text('确定要删除「$name」吗？'),
                          actions: [
                            AppLiquidGlassDialogActions(
                              actions: [
                                AppGlassDialogAction(
                                  label: '取消',
                                  onPressed: () => Navigator.pop(d, false),
                                ),
                                AppGlassDialogAction(
                                  label: '删除',
                                  onPressed: () => Navigator.pop(d, true),
                                  variant: AppLiquidGlassButtonVariant.danger,
                                ),
                              ],
                            ),
                          ],
                        ),
                      );
                      if (confirm == true) {
                        try {
                          await DioClient.instance.dio.delete(
                            ApiEndpoints.openApiAppById(id),
                          );
                          if (!mounted) {
                            return;
                          }
                          await _load();
                          if (!mounted) {
                            return;
                          }
                          AppGlassNotice.show(
                            context,
                            '应用已删除',
                            type: AppGlassNoticeType.success,
                          );
                        } catch (error) {
                          if (!mounted) {
                            return;
                          }
                          AppGlassNotice.show(
                            context,
                            extractErrorMessage(error, '删除应用失败'),
                            type: AppGlassNoticeType.error,
                          );
                        }
                      }
                      break;
                  }
                },
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            '调用次数: $callCount · 速率限制: $rateLimit/小时',
            style: TextStyle(
              fontSize: 11,
              color: isLight ? AppColors.slate500 : AppColors.slate400,
            ),
          ),
        ],
      ),
    );
  }

  void _showEditDialog(int id, String name, String scopes, int rateLimit) {
    final nameC = TextEditingController(text: name);
    final rateLimitC = TextEditingController(text: rateLimit.toString());
    final selectedScopes = _parseScopes(scopes).toSet();

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) {
          final navigator = Navigator.of(ctx);
          return Padding(
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  '编辑应用',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: nameC,
                  decoration: const InputDecoration(labelText: '应用名称'),
                ),
                const SizedBox(height: 12),
                const Text(
                  '权限范围',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  children: _apiScopeOptions.map((option) {
                    return AppLiquidGlassChoiceChip(
                      label: option.label,
                      selected: selectedScopes.contains(option.value),
                      onSelected: (selected) {
                        setSheetState(() {
                          if (selected) {
                            selectedScopes.add(option.value);
                          } else {
                            selectedScopes.remove(option.value);
                          }
                        });
                      },
                    );
                  }).toList(),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: rateLimitC,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: '速率限制（次/小时）',
                    hintText: '0 表示不限制',
                  ),
                ),
                const SizedBox(height: 20),
                SizedBox(
                  width: double.infinity,
                  child: AppLiquidGlassButton(
                    label: '保存',
                    height: 44,
                    performanceMode: true,
                    onPressed: () async {
                      try {
                        await DioClient.instance.dio.put(
                          ApiEndpoints.openApiAppById(id),
                          data: {
                            'name': nameC.text.trim(),
                            'scopes': _joinScopes(selectedScopes.toList()),
                            'rate_limit':
                                int.tryParse(rateLimitC.text.trim()) ?? 0,
                          },
                        );
                        if (!mounted || !ctx.mounted) {
                          return;
                        }
                        navigator.pop();
                        await _load();
                        if (!mounted) return;
                        AppGlassNotice.show(
                          context,
                          '应用已保存',
                          type: AppGlassNoticeType.success,
                        );
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          context,
                          extractErrorMessage(error, '保存应用失败'),
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
                    onPressed: () => Navigator.of(ctx).pop(),
                  ),
                ),
              ],
            ),
          );
        },
      ),
    );
  }

  void _showViewSecretDialog(int id, String appKey) {
    final passwordC = TextEditingController();
    showDialog(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('查看密钥'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text(
              '需要输入当前登录用户的密码来查看 App Secret',
              style: TextStyle(fontSize: 13),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: passwordC,
              obscureText: true,
              decoration: const InputDecoration(labelText: '密码'),
            ),
          ],
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx),
              ),
              AppGlassDialogAction(
                label: '确认',
                variant: AppLiquidGlassButtonVariant.primary,
                onPressed: () async {
                  if (passwordC.text.isEmpty) return;
                  try {
                    final resp = await DioClient.instance.dio.post(
                      ApiEndpoints.openApiAppViewSecret(id),
                      data: {'password': passwordC.text},
                    );
                    final data = extractData(resp.data);
                    if (!mounted || !dialogCtx.mounted) {
                      return;
                    }
                    Navigator.of(dialogCtx).pop();
                    if (data is Map && data['app_secret'] != null) {
                      _showSecretDialog(
                        appKey,
                        data['app_secret'].toString(),
                      );
                    }
                  } catch (error) {
                    if (mounted) {
                      AppGlassNotice.show(
                        context,
                        extractErrorMessage(error, '查看密钥失败'),
                        type: AppGlassNoticeType.error,
                      );
                    }
                  }
                },
              ),
            ],
          ),
        ],
      ),
    );
  }

}

class _CopyableField extends StatelessWidget {
  final String label;
  final String value;
  final BuildContext noticeContext;
  const _CopyableField({
    required this.label,
    required this.value,
    required this.noticeContext,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: const TextStyle(fontSize: 11, color: AppColors.slate400),
        ),
        const SizedBox(height: 4),
        AppLiquidGlassSurface(
          borderRadius: 8,
          padding: const EdgeInsets.all(10),
          onTap: () {
            Clipboard.setData(ClipboardData(text: value));
            AppGlassNotice.show(
              noticeContext,
              '已复制 $label',
              type: AppGlassNoticeType.success,
            );
          },
          child: SizedBox(
            width: double.infinity,
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    value,
                    style: const TextStyle(
                      fontSize: 12,
                      fontFamily: 'monospace',
                    ),
                  ),
                ),
                const Icon(Icons.copy, size: 14, color: AppColors.slate400),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

// ── Standalone logs page (for route /open-api/:id/logs) ──

class OpenApiLogsPage extends ConsumerStatefulWidget {
  final int appId;
  const OpenApiLogsPage({super.key, required this.appId});

  @override
  ConsumerState<OpenApiLogsPage> createState() => _OpenApiLogsPageState();
}

class _OpenApiLogsPageState extends ConsumerState<OpenApiLogsPage> {
  List<Map<String, dynamic>> _logs = [];
  bool _loading = true;
  bool _requestInFlight = false;
  int _page = 1;
  int _total = 0;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load({bool refresh = true}) async {
    if (_requestInFlight) return;
    final targetPage = refresh ? 1 : _page + 1;
    _requestInFlight = true;
    setState(() => _loading = true);
    try {
      final result = await _loadOpenApiLogPage(widget.appId, targetPage);
      if (!mounted) return;
      setState(() {
        _logs = refresh ? result.items : [..._logs, ...result.items];
        _total = result.total;
        _page = targetPage;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    } finally {
      _requestInFlight = false;
    }
  }

  void _loadMore() {
    if (_loading || _logs.length >= _total) return;
    _load(refresh: false);
  }

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;

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
                      'API 调用日志',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            Expanded(
              child: NotificationListener<ScrollNotification>(
                onNotification: (notification) {
                  if (notification.metrics.extentAfter < 240) _loadMore();
                  return false;
                },
                child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: _load,
                child: _loading && _logs.isEmpty
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
                    : _logs.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: const [
                          SizedBox(height: 100),
                          Center(
                            child: Text(
                              '暂无日志',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                         itemCount:
                             _logs.length + (_logs.length < _total ? 1 : 0),
                         itemBuilder: (_, i) {
                           if (i == _logs.length) {
                             return const Padding(
                               padding: EdgeInsets.all(16),
                               child: Center(
                                 child: CircularProgressIndicator(
                                   color: AppColors.primary,
                                 ),
                               ),
                             );
                           }
                          final log = _logs[i];
                          final status = (log['status'] as num?)?.toInt() ?? 0;
                          final ok = status >= 200 && status < 300;
                          final time = DateTime.tryParse(
                            log['created_at']?.toString() ?? '',
                          );
                          final duration =
                              (log['duration'] as num?)?.toDouble() ?? 0;
                          return AppCard(
                            margin: const EdgeInsets.only(bottom: 8),
                            padding: const EdgeInsets.symmetric(
                              horizontal: 14,
                              vertical: 12,
                            ),
                            borderRadius: 12,
                            stableForScrolling: true,
                            child: Row(
                              children: [
                                Container(
                                  padding: const EdgeInsets.symmetric(
                                    horizontal: 5,
                                    vertical: 2,
                                  ),
                                  decoration: BoxDecoration(
                                    color: ok
                                        ? AppColors.primary.withAlpha(25)
                                        : AppColors.red500.withAlpha(25),
                                    borderRadius: BorderRadius.circular(4),
                                  ),
                                  child: Text(
                                    '$status',
                                    style: TextStyle(
                                      fontSize: 10,
                                      fontWeight: FontWeight.w700,
                                      fontFamily: 'monospace',
                                      color: ok
                                          ? AppColors.primary
                                          : AppColors.red500,
                                    ),
                                  ),
                                ),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        '${log['method'] ?? ''} ${log['endpoint'] ?? ''}',
                                        style: const TextStyle(
                                          fontSize: 12,
                                          fontFamily: 'monospace',
                                          fontWeight: FontWeight.w500,
                                        ),
                                        maxLines: 1,
                                        overflow: TextOverflow.ellipsis,
                                      ),
                                      Text(
                                        '${log['ip'] ?? ''} · ${duration.toStringAsFixed(1)}ms',
                                        style: TextStyle(
                                          fontSize: 10,
                                          color: isLight
                                              ? AppColors.slate500
                                              : AppColors.slate400,
                                        ),
                                      ),
                                    ],
                                  ),
                                ),
                                Text(
                                  time != null ? formatTimeCn(time) : '',
                                  style: TextStyle(
                                    fontSize: 10,
                                    color: isLight
                                        ? AppColors.slate400
                                        : AppColors.slate500,
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
