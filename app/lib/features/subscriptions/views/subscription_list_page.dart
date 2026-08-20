import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/network/sse_client.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/subscription.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/ansi_text.dart';
import '../../../shared/utils/log_background.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

String? validateSubscriptionAuth({
  required String subscriptionType,
  required String authType,
  required int? sshKeyId,
  required String authToken,
  bool hasExistingToken = false,
}) {
  if (subscriptionType != 'git-repo') return null;
  if (authType == 'ssh' && (sshKeyId == null || sshKeyId <= 0)) {
    return '请选择 SSH 密钥';
  }
  if (authType == 'token' && authToken.trim().isEmpty && !hasExistingToken) {
    return '请填写访问 Token';
  }
  return null;
}

// ── Provider ──

final subscriptionListProvider =
    StateNotifierProvider<SubscriptionListNotifier, SubscriptionListState>((
      ref,
    ) {
      return SubscriptionListNotifier();
    });

class SubscriptionListState {
  final List<Subscription> items;
  final bool loading;
  final int total;
  final String keyword;
  final String? error;

  const SubscriptionListState({
    this.items = const [],
    this.loading = false,
    this.total = 0,
    this.keyword = '',
    this.error,
  });

  SubscriptionListState copyWith({
    List<Subscription>? items,
    bool? loading,
    int? total,
    String? keyword,
    String? error,
  }) {
    return SubscriptionListState(
      items: items ?? this.items,
      loading: loading ?? this.loading,
      total: total ?? this.total,
      keyword: keyword ?? this.keyword,
      error: error,
    );
  }
}

class SubscriptionListNotifier extends StateNotifier<SubscriptionListState> {
  SubscriptionListNotifier() : super(const SubscriptionListState());
  int _loadRequestId = 0;
  int _page = 1;

  Future<void> load({bool refresh = true}) async {
    final requestId = ++_loadRequestId;
    if (refresh) _page = 1;
    state = state.copyWith(loading: true);
    try {
      final dio = DioClient.instance.dio;
      final params = <String, dynamic>{'page': _page, 'page_size': 100};
      if (state.keyword.isNotEmpty) params['keyword'] = state.keyword;
      final resp = await dio.get(
        ApiEndpoints.subscriptions,
        queryParameters: params,
      );
      final paginated = extractPaginated(resp.data);
      final items = paginated.items
          .map((e) => Subscription.fromJson(e))
          .toList();
      if (requestId != _loadRequestId) return;
      state = state.copyWith(
        items: refresh ? items : [...state.items, ...items],
        total: paginated.total,
        loading: false,
        error: null,
      );
    } catch (_) {
      if (requestId != _loadRequestId) return;
      if (!refresh && _page > 1) _page--;
      state = state.copyWith(loading: false, error: '加载订阅失败');
    }
  }

  Future<void> loadMore() async {
    if (state.loading || state.items.length >= state.total) return;
    _page++;
    await load(refresh: false);
  }

  void setKeyword(String keyword) {
    state = state.copyWith(keyword: keyword);
    load();
  }

  Future<void> toggle(int id, bool enabled) async {
    final dio = DioClient.instance.dio;
    if (enabled) {
      await dio.put(ApiEndpoints.subscriptionEnable(id));
    } else {
      await dio.put(ApiEndpoints.subscriptionDisable(id));
    }
    await load();
  }

  Future<void> pull(Subscription sub) async {
    final dio = DioClient.instance.dio;
    if (sub.type != sub.normalizedType) {
      await dio.put(
        ApiEndpoints.subscriptionById(sub.id),
        data: {'type': sub.normalizedType},
      );
    }
    await dio.put(ApiEndpoints.subscriptionPull(sub.id));
    await load();
  }

  Future<void> stopPull(int id) async {
    await DioClient.instance.dio.put(ApiEndpoints.subscriptionPullStop(id));
    await load();
  }

  Future<void> delete(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.subscriptionById(id));
    await load();
  }

  Future<void> create(Map<String, dynamic> data) async {
    await DioClient.instance.dio.post(ApiEndpoints.subscriptions, data: data);
    await load();
  }

  Future<void> update(int id, Map<String, dynamic> data) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.subscriptionById(id),
      data: data,
    );
    await load();
  }
}

// ── Page ──

class SubscriptionListPage extends ConsumerStatefulWidget {
  const SubscriptionListPage({super.key});

  @override
  ConsumerState<SubscriptionListPage> createState() =>
      _SubscriptionListPageState();
}

class _SubscriptionListPageState extends ConsumerState<SubscriptionListPage> {
  final _searchController = TextEditingController();
  Timer? _debounce;

  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.read(subscriptionListProvider.notifier).load());
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _searchController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(subscriptionListProvider);
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 12),
        child: Column(
          children: [
            // Header
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
                      '订阅管理',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  AppGlassIconButton(
                    icon: Icons.add,
                    tooltip: '新建订阅',
                    onTap: () => _showCreateDialog(),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),

            // Search
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: SizedBox(
                height: 44,
                child: AppLiquidGlassInput(
                  child: TextField(
                  controller: _searchController,
                  decoration: InputDecoration(
                    hintText: '搜索订阅...',
                    prefixIcon: const Icon(
                      Icons.search,
                      size: 18,
                      color: AppColors.slate400,
                    ),
                    isDense: true,
                    suffixIcon: _searchController.text.isNotEmpty
                        ? IconButton(
                            icon: const Icon(
                              Icons.clear,
                              size: 16,
                              color: AppColors.slate400,
                            ),
                            onPressed: () {
                              _searchController.clear();
                              ref
                                  .read(subscriptionListProvider.notifier)
                                  .setKeyword('');
                            },
                          )
                        : null,
                  ),
                  style: const TextStyle(fontSize: 14),
                  onChanged: (v) {
                    setState(() {});
                    _debounce?.cancel();
                    _debounce = Timer(const Duration(milliseconds: 300), () {
                      ref.read(subscriptionListProvider.notifier).setKeyword(v);
                    });
                  },
                  ),
                ),
              ),
            ),
            const SizedBox(height: 12),

            // List
            Expanded(
              child: NotificationListener<ScrollNotification>(
                onNotification: (notification) {
                  if (notification.metrics.extentAfter < 240) {
                    ref.read(subscriptionListProvider.notifier).loadMore();
                  }
                  return false;
                },
                child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: () =>
                    ref.read(subscriptionListProvider.notifier).load(),
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
                    : state.items.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.sync_disabled,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无订阅',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                        itemCount:
                            state.items.length +
                            (state.items.length < state.total ? 1 : 0),
                        itemBuilder: (_, i) {
                          if (i == state.items.length) {
                            return const Padding(
                              padding: EdgeInsets.all(16),
                              child: Center(
                                child: CircularProgressIndicator(
                                  color: AppColors.primary,
                                ),
                              ),
                            );
                          }
                          final sub = state.items[i];
                          return _SubCard(
                            sub: sub,
                            isLight: isLight,
                            onPull: () => _doPull(sub),
                            onStopPull: () => _doStopPull(sub),
                            onLogs: () => _openLogs(sub),
                            onToggle: () => ref
                                .read(subscriptionListProvider.notifier)
                                .toggle(sub.id, !sub.enabled),
                            onDelete: () => _confirmDelete(sub),
                            onEdit: () => _showEditDialog(sub),
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

  Future<void> _doPull(Subscription sub) async {
    try {
      await ref.read(subscriptionListProvider.notifier).pull(sub);
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '已触发拉取',
        type: AppGlassNoticeType.info,
      );
      context.push('/subscriptions/${sub.id}/pull-stream');
    } catch (error) {
      final message = _extractRequestErrorMessage(error, '拉取失败');
      if (!mounted) {
        return;
      }
      if (message.contains('拉取中')) {
        AppGlassNotice.show(
          context,
          '该订阅已在拉取中',
          type: AppGlassNoticeType.warning,
        );
        context.push('/subscriptions/${sub.id}/pull-stream');
        return;
      }
      AppGlassNotice.show(context, message, type: AppGlassNoticeType.error);
    }
  }

  Future<void> _doStopPull(Subscription sub) async {
    try {
      await ref.read(subscriptionListProvider.notifier).stopPull(sub.id);
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '已停止拉取',
        type: AppGlassNoticeType.info,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        _extractRequestErrorMessage(error, '停止拉取失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  void _openLogs(Subscription sub) {
    context.push('/subscriptions/${sub.id}/logs', extra: sub.name);
  }

  Future<void> _confirmDelete(Subscription sub) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('删除订阅'),
        content: Text('确定要删除「${sub.name}」吗？'),
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
        await ref.read(subscriptionListProvider.notifier).delete(sub.id);
        if (!mounted) {
          return;
        }
        AppGlassNotice.show(
          context,
          '订阅已删除',
          type: AppGlassNoticeType.success,
        );
      } catch (error) {
        if (!mounted) {
          return;
        }
        AppGlassNotice.show(
          context,
          _extractRequestErrorMessage(error, '删除订阅失败'),
          type: AppGlassNoticeType.error,
        );
      }
    }
  }

  void _showCreateDialog() async {
    final nameC = TextEditingController();
    final urlC = TextEditingController();
    final branchC = TextEditingController();
    final subPathC = TextEditingController();
    final scheduleC = TextEditingController();
    final saveDirC = TextEditingController();
    final aliasC = TextEditingController();
    final whitelistC = TextEditingController();
    final blacklistC = TextEditingController();
    final dependOnC = TextEditingController();
    final hookScriptC = TextEditingController();
    String selectedType = 'git-repo';
    bool forceOverwrite = true;
    int? selectedSshKeyId;
    List<Map<String, dynamic>> sshKeys = [];

    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.sshKeys);
      final data = extractData(resp.data);
      if (data is List) {
        sshKeys = data.whereType<Map<String, dynamic>>().toList();
      }
      } catch (_) {}
      if (!mounted) return;

      showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) {
        final navigator = Navigator.of(ctx);
        return StatefulBuilder(
          builder: (ctx, setSheetState) {
            final theme = Theme.of(ctx);
            final bottomInset = MediaQuery.of(ctx).viewInsets.bottom;
            return SizedBox(
              height: MediaQuery.of(ctx).size.height * 0.75,
              child: Padding(
                padding: EdgeInsets.only(bottom: bottomInset),
                child: Column(
                  children: [
                    // 固定标题
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
                      child: Align(
                        alignment: Alignment.centerLeft,
                        child: Text(
                          '新建订阅',
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                    ),
                    // 可滚动表单
                    Expanded(
                      child: SingleChildScrollView(
                        padding: const EdgeInsets.symmetric(horizontal: 20),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            TextField(
                              controller: nameC,
                              decoration: const InputDecoration(
                                labelText: '名称',
                              ),
                            ),
                            const SizedBox(height: 12),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              crossAxisAlignment: WrapCrossAlignment.center,
                              children: [
                                const Text('类型', style: TextStyle(fontSize: 13)),
                                ...['git-repo', 'single-file'].map((t) {
                                  final label = t == 'git-repo'
                                      ? 'Git 仓库'
                                      : '单文件';
                                  return AppLiquidGlassChoiceChip(
                                      label: label,
                                      selected: selectedType == t,
                                      onSelected: (_) =>
                                          setSheetState(() => selectedType = t),
                                  );
                                }),
                              ],
                            ),
                            const SizedBox(height: 12),
                            if (selectedType == 'git-repo' && sshKeys.isNotEmpty) ...[
                              DropdownButtonFormField<int?>(
                                initialValue: selectedSshKeyId,
                                decoration: const InputDecoration(
                                  labelText: 'SSH 密钥 (可选)',
                                ),
                                isExpanded: true,
                                items: [
                                  const DropdownMenuItem<int?>(
                                    value: null,
                                    child: Text('不使用 SSH 密钥'),
                                  ),
                                  ...sshKeys.map((k) => DropdownMenuItem<int?>(
                                    value: (k['id'] as num?)?.toInt(),
                                    child: Text(
                                      k['name']?.toString() ?? '',
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  )),
                                ],
                                onChanged: (v) =>
                                    setSheetState(() => selectedSshKeyId = v),
                              ),
                              const SizedBox(height: 12),
                            ],
                            TextField(
                              controller: urlC,
                              decoration: InputDecoration(
                                labelText: selectedType == 'single-file'
                                    ? '文件 URL'
                                    : '仓库地址',
                              ),
                            ),
                            const SizedBox(height: 12),
                            if (selectedType != 'single-file') ...[
                              TextField(
                                controller: branchC,
                                decoration: const InputDecoration(
                                  labelText: '分支',
                                  hintText: '默认分支 (留空使用默认)',
                                ),
                              ),
                              const SizedBox(height: 12),
                              TextField(
                                controller: subPathC,
                                decoration: const InputDecoration(
                                  labelText: '指定子目录',
                                  hintText: '逗号分隔多个，留空拉取全部',
                                ),
                              ),
                              const SizedBox(height: 12),
                            ],
                            TextField(
                              controller: scheduleC,
                              decoration: const InputDecoration(
                                labelText: '定时拉取',
                                hintText: 'cron 表达式 (留空不自动拉取)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: saveDirC,
                              decoration: const InputDecoration(
                                labelText: '保存目录',
                                hintText: '保存到 scripts 下的子目录',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: aliasC,
                              decoration: const InputDecoration(
                                labelText: '别名',
                                hintText: '目录/文件别名',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: whitelistC,
                              decoration: const InputDecoration(
                                labelText: '白名单',
                                hintText: '文件名/路径白名单 (逗号分隔)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: blacklistC,
                              decoration: const InputDecoration(
                                labelText: '黑名单',
                                hintText: '文件名/路径黑名单 (逗号分隔)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: dependOnC,
                              decoration: const InputDecoration(
                                labelText: '依赖说明',
                                hintText: '订阅依赖、过滤说明或迁移信息',
                              ),
                            ),
                            if (selectedType != 'single-file') ...[
                              const SizedBox(height: 16),
                              Row(
                                children: [
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment:
                                          CrossAxisAlignment.start,
                                      children: [
                                        const Text(
                                          '覆盖本地修改',
                                          style: TextStyle(
                                            fontSize: 13,
                                            fontWeight: FontWeight.w600,
                                          ),
                                        ),
                                        const SizedBox(height: 2),
                                        Text(
                                          forceOverwrite
                                              ? '拉取时覆盖本地修改'
                                              : '拉取时保留本地修改的文件',
                                          style: const TextStyle(
                                            fontSize: 11,
                                            color: Colors.grey,
                                          ),
                                        ),
                                      ],
                                    ),
                                  ),
                                  AppLiquidGlassToggle(
                                    value: forceOverwrite,
                                    onChanged: (v) =>
                                        setSheetState(() => forceOverwrite = v),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 12),
                              TextField(
                                controller: hookScriptC,
                                maxLines: 3,
                                decoration: const InputDecoration(
                                  labelText: '拉取后钩子',
                                  hintText: '拉取成功后执行的 Shell 命令',
                                  alignLabelWithHint: true,
                                ),
                              ),
                              const SizedBox(height: 8),
                              const _HookScriptHint(),
                            ],
                            const SizedBox(height: 16),
                          ],
                        ),
                      ),
                    ),
                    // 固定底部按钮
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
                      child: Column(
                        children: [
                          SizedBox(
                            width: double.infinity,
                            child: AppLiquidGlassButton(
                              label: '创建',
                              height: 44,
                              performanceMode: true,
                              onPressed: () async {
                                if (nameC.text.trim().isEmpty) return;
                                try {
                                  await ref
                                      .read(subscriptionListProvider.notifier)
                                      .create({
                                        'name': nameC.text.trim(),
                                        'type': selectedType,
                                        'url': urlC.text.trim(),
                                        'branch': branchC.text.trim(),
                                        'sub_path': subPathC.text.trim(),
                                        'schedule': scheduleC.text.trim(),
                                        'save_dir': saveDirC.text.trim(),
                                        'alias': aliasC.text.trim(),
                                        'whitelist': whitelistC.text.trim(),
                                        'blacklist': blacklistC.text.trim(),
                                        'depend_on': dependOnC.text.trim(),
                                        'hook_script': hookScriptC.text.trim(),
                                        'force_overwrite': forceOverwrite,
                                        'ssh_key_id': selectedSshKeyId,
                                      });
                                    if (!mounted) {
                                      return;
                                    }
                                    navigator.pop();
                                    AppGlassNotice.show(
                                      context,
                                      '订阅已创建',
                                      type: AppGlassNoticeType.success,
                                    );
                                } catch (error) {
                                  if (!mounted) {
                                    return;
                                  }
                                  AppGlassNotice.show(
                                    context,
                                    _extractRequestErrorMessage(
                                      error,
                                      '创建订阅失败',
                                    ),
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
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    );
  }

  void _showEditDialog(Subscription sub) async {
    final nameC = TextEditingController(text: sub.name);
    final urlC = TextEditingController(text: sub.url);
    final branchC = TextEditingController(text: sub.branch);
    final subPathC = TextEditingController(text: sub.subPath ?? '');
    final scheduleC = TextEditingController(text: sub.schedule);
    final saveDirC = TextEditingController(text: sub.saveDir);
    final aliasC = TextEditingController(text: sub.alias);
    final whitelistC = TextEditingController(text: sub.whitelist);
    final blacklistC = TextEditingController(text: sub.blacklist);
    final dependOnC = TextEditingController(text: sub.dependOn);
    final hookScriptC = TextEditingController(text: sub.hookScript);
    String selectedType = sub.normalizedType;
    bool forceOverwrite = sub.forceOverwrite ?? true;
    int? selectedSshKeyId = sub.sshKeyId;
    List<Map<String, dynamic>> sshKeys = [];

    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.sshKeys);
      final data = extractData(resp.data);
      if (data is List) {
        sshKeys = data.whereType<Map<String, dynamic>>().toList();
      }
      } catch (_) {}
      if (!mounted) return;

      showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) {
        final navigator = Navigator.of(ctx);
        return StatefulBuilder(
          builder: (ctx, setSheetState) {
            final theme = Theme.of(ctx);
            final bottomInset = MediaQuery.of(ctx).viewInsets.bottom;
            return SizedBox(
              height: MediaQuery.of(ctx).size.height * 0.75,
              child: Padding(
                padding: EdgeInsets.only(bottom: bottomInset),
                child: Column(
                  children: [
                    // 固定标题
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
                      child: Align(
                        alignment: Alignment.centerLeft,
                        child: Text(
                          '编辑订阅',
                          style: theme.textTheme.titleMedium?.copyWith(
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                    ),
                    // 可滚动表单
                    Expanded(
                      child: SingleChildScrollView(
                        padding: const EdgeInsets.symmetric(horizontal: 20),
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.stretch,
                          children: [
                            TextField(
                              controller: nameC,
                              decoration: const InputDecoration(
                                labelText: '名称',
                              ),
                            ),
                            const SizedBox(height: 12),
                            Wrap(
                              spacing: 8,
                              runSpacing: 8,
                              crossAxisAlignment: WrapCrossAlignment.center,
                              children: [
                                const Text('类型', style: TextStyle(fontSize: 13)),
                                ...['git-repo', 'single-file'].map((t) {
                                  final label = t == 'git-repo'
                                      ? 'Git 仓库'
                                      : '单文件';
                                  return AppLiquidGlassChoiceChip(
                                      label: label,
                                      selected: selectedType == t,
                                      onSelected: (_) =>
                                          setSheetState(() => selectedType = t),
                                  );
                                  }),
                              ],
                            ),
                            const SizedBox(height: 12),
                            if (selectedType == 'git-repo' && sshKeys.isNotEmpty) ...[
                              DropdownButtonFormField<int?>(
                                initialValue: selectedSshKeyId,
                                decoration: const InputDecoration(
                                  labelText: 'SSH 密钥 (可选)',
                                ),
                                isExpanded: true,
                                items: [
                                  const DropdownMenuItem<int?>(
                                    value: null,
                                    child: Text('不使用 SSH 密钥'),
                                  ),
                                  ...sshKeys.map((k) => DropdownMenuItem<int?>(
                                    value: (k['id'] as num?)?.toInt(),
                                    child: Text(
                                      k['name']?.toString() ?? '',
                                      overflow: TextOverflow.ellipsis,
                                    ),
                                  )),
                                ],
                                onChanged: (v) =>
                                    setSheetState(() => selectedSshKeyId = v),
                              ),
                              const SizedBox(height: 12),
                            ],
                            TextField(
                              controller: urlC,
                              decoration: InputDecoration(
                                labelText: selectedType == 'single-file'
                                    ? '文件 URL'
                                    : '仓库地址',
                              ),
                            ),
                            const SizedBox(height: 12),
                            if (selectedType != 'single-file') ...[
                              TextField(
                                controller: branchC,
                                decoration: const InputDecoration(
                                  labelText: '分支',
                                  hintText: '默认分支 (留空使用默认)',
                                ),
                              ),
                              const SizedBox(height: 12),
                              TextField(
                                controller: subPathC,
                                decoration: const InputDecoration(
                                  labelText: '指定子目录',
                                  hintText: '逗号分隔多个，留空拉取全部',
                                ),
                              ),
                              const SizedBox(height: 12),
                            ],
                            TextField(
                              controller: scheduleC,
                              decoration: const InputDecoration(
                                labelText: '定时拉取',
                                hintText: 'cron 表达式 (留空不自动拉取)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: saveDirC,
                              decoration: const InputDecoration(
                                labelText: '保存目录',
                                hintText: '保存到 scripts 下的子目录',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: aliasC,
                              decoration: const InputDecoration(
                                labelText: '别名',
                                hintText: '目录/文件别名',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: whitelistC,
                              decoration: const InputDecoration(
                                labelText: '白名单',
                                hintText: '文件名/路径白名单 (逗号分隔)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: blacklistC,
                              decoration: const InputDecoration(
                                labelText: '黑名单',
                                hintText: '文件名/路径黑名单 (逗号分隔)',
                              ),
                            ),
                            const SizedBox(height: 12),
                            TextField(
                              controller: dependOnC,
                              decoration: const InputDecoration(
                                labelText: '依赖说明',
                                hintText: '订阅依赖、过滤说明或迁移信息',
                              ),
                            ),
                            if (selectedType != 'single-file') ...[
                              const SizedBox(height: 16),
                              Row(
                                children: [
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment:
                                          CrossAxisAlignment.start,
                                      children: [
                                        const Text(
                                          '覆盖本地修改',
                                          style: TextStyle(
                                            fontSize: 13,
                                            fontWeight: FontWeight.w600,
                                          ),
                                        ),
                                        const SizedBox(height: 2),
                                        Text(
                                          forceOverwrite
                                              ? '拉取时覆盖本地修改'
                                              : '拉取时保留本地修改的文件',
                                          style: const TextStyle(
                                            fontSize: 11,
                                            color: Colors.grey,
                                          ),
                                        ),
                                      ],
                                    ),
                                  ),
                                  AppLiquidGlassToggle(
                                    value: forceOverwrite,
                                    onChanged: (v) =>
                                        setSheetState(() => forceOverwrite = v),
                                  ),
                                ],
                              ),
                              const SizedBox(height: 12),
                              TextField(
                                controller: hookScriptC,
                                maxLines: 3,
                                decoration: const InputDecoration(
                                  labelText: '拉取后钩子',
                                  hintText: '拉取成功后执行的 Shell 命令',
                                  alignLabelWithHint: true,
                                ),
                              ),
                              const SizedBox(height: 8),
                              const _HookScriptHint(),
                            ],
                            const SizedBox(height: 16),
                          ],
                        ),
                      ),
                    ),
                    // 固定底部按钮
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
                      child: Row(
                        children: [
                          Expanded(
                            child: AppLiquidGlassButton(
                              label: '关闭',
                              height: 44,
                              variant: AppLiquidGlassButtonVariant.secondary,
                              performanceMode: true,
                              onPressed: () => Navigator.of(ctx).pop(),
                            ),
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: AppLiquidGlassButton(
                              label: '保存',
                              height: 44,
                              performanceMode: true,
                              onPressed: () async {
                                try {
                                  await ref
                                      .read(subscriptionListProvider.notifier)
                                      .update(sub.id, {
                                        'name': nameC.text.trim(),
                                        'type': selectedType,
                                        'url': urlC.text.trim(),
                                        'branch': branchC.text.trim(),
                                        'sub_path': subPathC.text.trim(),
                                        'schedule': scheduleC.text.trim(),
                                        'save_dir': saveDirC.text.trim(),
                                        'alias': aliasC.text.trim(),
                                        'whitelist': whitelistC.text.trim(),
                                        'blacklist': blacklistC.text.trim(),
                                        'depend_on': dependOnC.text.trim(),
                                        'hook_script': hookScriptC.text.trim(),
                                        'force_overwrite': forceOverwrite,
                                        'ssh_key_id': selectedSshKeyId,
                                      });
                                    if (!mounted) {
                                      return;
                                    }
                                    navigator.pop();
                                    AppGlassNotice.show(
                                      context,
                                      '订阅已保存',
                                      type: AppGlassNoticeType.success,
                                    );
                                } catch (error) {
                                  if (!mounted) {
                                    return;
                                  }
                                  AppGlassNotice.show(
                                    context,
                                    _extractRequestErrorMessage(
                                      error,
                                      '保存订阅失败',
                                    ),
                                    type: AppGlassNoticeType.error,
                                  );
                                }
                              },
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
        );
      },
    );
  }
}

// ── Card ──

class _SubCard extends ConsumerWidget {
  final Subscription sub;
  final bool isLight;
  final VoidCallback onPull;
  final VoidCallback onStopPull;
  final VoidCallback onLogs;
  final VoidCallback onToggle;
  final VoidCallback onDelete;
  final VoidCallback onEdit;

  const _SubCard({
    required this.sub,
    required this.isLight,
    required this.onPull,
    required this.onStopPull,
    required this.onLogs,
    required this.onToggle,
    required this.onDelete,
    required this.onEdit,
  });

  Color _statusBg() {
    if (sub.isPulling) {
      return AppColors.blue500.withAlpha(isLight ? 20 : 25);
    }
    if (sub.enabled) {
      return AppColors.primary.withAlpha(isLight ? 18 : 25);
    }
    return AppColors.slate500.withAlpha(isLight ? 14 : 24);
  }

  Color _statusFg() {
    if (sub.isPulling) {
      return isLight ? AppColors.blue600 : AppColors.blue500;
    }
    if (sub.enabled) {
      return isLight ? AppColors.primaryDark : AppColors.primary;
    }
    return AppColors.slate500;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    
    return AppCard(
      onTap: onEdit,
      stableForScrolling: true,
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      child: Column(
          children: [
            // Top row: name + status
            Row(
              children: [
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: sub.enabled ? AppColors.primary : AppColors.slate300,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    sub.name,
                    style: const TextStyle(
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                const SizedBox(width: 8),
                Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 8,
                    vertical: 3,
                  ),
                  decoration: BoxDecoration(
                    color: _statusBg(),
                    borderRadius: BorderRadius.circular(4),
                  ),
                  child: Text(
                    sub.statusText,
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w700,
                      color: _statusFg(),
                    ),
                  ),
                ),
              ],
            ),
            // URL
            Padding(
              padding: const EdgeInsets.only(top: 10),
              child: Row(
                children: [
                  Text(
                    '仓库：',
                    style: TextStyle(
                      fontSize: 12,
                      color: isLight ? AppColors.slate500 : AppColors.slate400,
                    ),
                  ),
                  Expanded(
                    child: Text(
                      sub.url.isNotEmpty ? sub.url : sub.typeLabel,
                      style: TextStyle(
                        fontSize: 12,
                        color: isLight
                            ? AppColors.slate500
                            : AppColors.slate400,
                        fontFamily: 'monospace',
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 10),

            // Bottom: last pull + actions
            Container(
              padding: const EdgeInsets.only(top: 10),
              decoration: BoxDecoration(
                border: Border(
                  top: BorderSide(
                    color: isLight
                        ? AppColors.slate100
                        : AppColors.slate800.withAlpha(120),
                  ),
                ),
              ),
              child: Row(
                children: [
                  Text(
                    sub.lastPullAt != null
                        ? '上次拉取：${formatTimeCn(sub.lastPullAt, short: true)}'
                        : '尚未拉取',
                    style: TextStyle(
                      fontSize: 12,
                      color: isLight ? AppColors.slate500 : AppColors.slate400,
                    ),
                  ),
                  const Spacer(),
                  _SmallIconBtn(
                    icon: sub.isPulling ? Icons.stop : Icons.sync,
                    onTap: sub.isPulling ? onStopPull : onPull,
                    color: sub.isPulling ? AppColors.red500 : AppColors.primary,
                  ),
                  const SizedBox(width: 4),
                  _SmallIconBtn(
                    icon: Icons.receipt_long_outlined,
                    onTap: onLogs,
                    color: AppColors.blue500,
                  ),
                  const SizedBox(width: 4),
                  _SmallIconBtn(
                    icon: sub.enabled ? Icons.pause : Icons.play_arrow,
                    onTap: onToggle,
                  ),
                  const SizedBox(width: 4),
                  _SmallIconBtn(
                    icon: Icons.delete_outline,
                    onTap: onDelete,
                    color: AppColors.red500,
                  ),
                ],
              ),
            ),
          ],
      ),
    );
  }
}

class _SmallIconBtn extends StatelessWidget {
  final IconData icon;
  final VoidCallback onTap;
  final Color? color;

  const _SmallIconBtn({required this.icon, required this.onTap, this.color});

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.all(6),
        child: Icon(icon, size: 18, color: color ?? AppColors.slate400),
      ),
    );
  }
}

class _HookScriptHint extends StatelessWidget {
  const _HookScriptHint();

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: AppColors.blue500.withAlpha(12),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: AppColors.blue500.withAlpha(30)),
      ),
      child: Text(
        '钩子会在订阅拉取成功后执行，适合安装依赖、移动文件或触发通知；这里填写的是 Shell 命令，留空则不执行。',
        style: TextStyle(
          fontSize: 12,
          height: 1.5,
          color: isLight ? AppColors.slate600 : AppColors.slate300,
        ),
      ),
    );
  }
}

class SubscriptionLogsPage extends ConsumerStatefulWidget {
  final int subscriptionId;
  final String? subscriptionName;

  const SubscriptionLogsPage({
    super.key,
    required this.subscriptionId,
    this.subscriptionName,
  });

  @override
  ConsumerState<SubscriptionLogsPage> createState() =>
      _SubscriptionLogsPageState();
}

class _SubscriptionLogsPageState extends ConsumerState<SubscriptionLogsPage> {
  static const int _pageSize = 20;

  List<Map<String, dynamic>> _logs = [];
  bool _loading = true;
  int _page = 1;
  int _total = 0;
  Color? _logBackgroundColor;

  @override
  void initState() {
    super.initState();
    Future.microtask(() async {
      _logBackgroundColor = await loadPanelLogBackgroundColor();
      await _load();
    });
  }

  Future<void> _load({int? page}) async {
    final targetPage = page ?? _page;
    setState(() => _loading = true);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.subscriptionLogs(widget.subscriptionId),
        queryParameters: {'page': targetPage, 'page_size': _pageSize},
      );
      final paginated = extractPaginated(response.data);
      if (!mounted) {
        return;
      }
      setState(() {
        _logs = paginated.items;
        _total = paginated.total;
        _page = targetPage;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() => _loading = false);
    }
  }

  void _showLogDetail(Map<String, dynamic> log) {
    final logTheme = resolveLogSurfaceTheme(_logBackgroundColor);
    final borderColor = logTheme.brightness == Brightness.dark
        ? AppColors.slate700
        : AppColors.slate200;
    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => SafeArea(
        child: SizedBox(
          height: MediaQuery.of(context).size.height * 0.75,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  '日志详情',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
                ),
                const SizedBox(height: 12),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.all(14),
                    decoration: BoxDecoration(
                      color: logTheme.background,
                      borderRadius: BorderRadius.circular(12),
                      border: Border.all(color: borderColor),
                    ),
                    child: SingleChildScrollView(
                      child: SelectionArea(
                        child: RichText(
                          text: AnsiTextParser.buildTextSpan(
                            log['content']?.toString().trim().isNotEmpty == true
                                ? log['content'].toString()
                                : '(无日志内容)',
                            baseStyle: TextStyle(
                              color: logTheme.foreground,
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.6,
                            ),
                            brightness: logTheme.brightness,
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final totalPages = ((_total + _pageSize - 1) ~/ _pageSize).clamp(
      1,
      1 << 20,
    );
    final title = (widget.subscriptionName?.trim().isNotEmpty ?? false)
        ? '${widget.subscriptionName} 拉取日志'
        : '拉取日志';

    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: Text(title)),
      body: Column(
        children: [
          Expanded(
            child: RefreshIndicator(
              color: AppColors.primary,
              onRefresh: () => _load(page: _page),
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
                      children: const [
                        SizedBox(height: 100),
                        Center(
                          child: Text(
                            '暂无拉取日志',
                            style: TextStyle(color: AppColors.slate400),
                          ),
                        ),
                      ],
                    )
                  : ListView.builder(
                      padding: const EdgeInsets.fromLTRB(20, 16, 20, 20),
                      itemCount: _logs.length,
                      itemBuilder: (_, index) {
                        final log = _logs[index];
                        final success = (log['status'] as num?)?.toInt() == 0;
                        final time = DateTime.tryParse(
                          log['created_at']?.toString() ?? '',
                        );
                        final preview = _subscriptionLogPreview(log);

                        return Padding(
                          padding: const EdgeInsets.only(bottom: 10),
                          child: AppLiquidGlassSurface(
                            onTap: () => _showLogDetail(log),
                            borderRadius: 14,
                            performanceMode: true,
                            padding: const EdgeInsets.all(14),
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Row(
                                  children: [
                                    Container(
                                      padding: const EdgeInsets.symmetric(
                                        horizontal: 8,
                                        vertical: 4,
                                      ),
                                      decoration: BoxDecoration(
                                        color: success
                                            ? AppColors.primary.withAlpha(20)
                                            : AppColors.red500.withAlpha(15),
                                        borderRadius: BorderRadius.circular(
                                          999,
                                        ),
                                      ),
                                      child: Text(
                                        success ? '成功' : '失败',
                                        style: TextStyle(
                                          fontSize: 11,
                                          fontWeight: FontWeight.w700,
                                          color: success
                                              ? AppColors.primary
                                              : AppColors.red500,
                                        ),
                                      ),
                                    ),
                                    const Spacer(),
                                    Text(
                                      '${(log['duration'] as num?)?.toStringAsFixed(1) ?? '0.0'}s',
                                      style: TextStyle(
                                        fontSize: 12,
                                        color: isLight
                                            ? AppColors.slate500
                                            : AppColors.slate400,
                                      ),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 10),
                                Text(
                                  preview,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis,
                                  style: const TextStyle(
                                    fontSize: 13,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                                const SizedBox(height: 8),
                                Text(
                                  time != null ? formatTimeCn(time) : '',
                                  style: TextStyle(
                                    fontSize: 11,
                                    color: isLight
                                        ? AppColors.slate400
                                        : AppColors.slate500,
                                  ),
                                ),
                              ],
                            ),
                          ),
                        );
                      },
                    ),
            ),
          ),
          if (_total > _pageSize)
            SafeArea(
              top: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
                child: Row(
                  children: [
                    Expanded(
                      child: OutlinedButton(
                        onPressed: _page > 1
                            ? () => _load(page: _page - 1)
                            : null,
                        child: const Text('上一页'),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Text(
                      '第 $_page / $totalPages 页',
                      style: TextStyle(
                        fontSize: 12,
                        color: isLight
                            ? AppColors.slate500
                            : AppColors.slate400,
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: FilledButton(
                        onPressed: _page < totalPages
                            ? () => _load(page: _page + 1)
                            : null,
                        child: const Text('下一页'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
        ],
      ),
    );
  }
}

// ── Pull Stream Page ──

class SubscriptionPullStreamPage extends ConsumerStatefulWidget {
  final int subscriptionId;
  const SubscriptionPullStreamPage({super.key, required this.subscriptionId});

  @override
  ConsumerState<SubscriptionPullStreamPage> createState() =>
      _SubscriptionPullStreamPageState();
}

class _SubscriptionPullStreamPageState
    extends ConsumerState<SubscriptionPullStreamPage> {
  final _sseClient = SseClient();
  final _logs = <String>[];
  final _scrollController = ScrollController();
  bool _done = false;
  String? _statusMessage;
  Color? _logBackgroundColor;

  @override
  void initState() {
    super.initState();
    Future.microtask(() async {
      final color = await loadPanelLogBackgroundColor();
      if (mounted) {
        setState(() => _logBackgroundColor = color);
      }
    });
    _connectStream();
  }

  void _connectStream() {
    _sseClient.close();
    _sseClient.connect(
      path: ApiEndpoints.subscriptionPullStream(widget.subscriptionId),
      autoReconnect: true,
      onEvent: (event) {
        if (!mounted) return;
        setState(() {
          if (event.event == 'done' &&
              event.data == 'not_running' &&
              _logs.isEmpty) {
            _statusMessage = '当前没有正在运行的拉取任务';
          } else {
            _logs.add(event.data);
          }
          if (event.event == 'done' && event.data != 'reconnect') {
            _done = true;
          }
        });
        Future.delayed(const Duration(milliseconds: 50), () {
          if (_scrollController.hasClients) {
            _scrollController.jumpTo(
              _scrollController.position.maxScrollExtent,
            );
          }
        });
      },
      onDone: () {
        if (mounted) setState(() => _done = true);
      },
      onError: (_) {
        if (mounted) {
          setState(() {
            _done = true;
            _statusMessage ??= '拉取日志连接已断开';
          });
        }
      },
    );
  }

  @override
  void dispose() {
    _sseClient.close();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final logTheme = resolveLogSurfaceTheme(_logBackgroundColor);
    final doneBannerBackground = logTheme.brightness == Brightness.dark
        ? AppColors.slate800
        : AppColors.slate100;

    return Scaffold(
      backgroundColor: logTheme.background,
      appBar: AppBar(
        title: const Text('拉取日志'),
        backgroundColor: logTheme.background,
        foregroundColor: logTheme.foreground,
      ),
      body: Container(
        color: logTheme.background,
        child: Column(
          children: [
            Expanded(
              child: _logs.isEmpty && _statusMessage != null
                  ? Center(
                      child: Text(
                        _statusMessage!,
                        style: TextStyle(color: logTheme.mutedForeground),
                      ),
                    )
                  : ListView.builder(
                      controller: _scrollController,
                      padding: const EdgeInsets.all(12),
                      itemCount: _logs.length,
                      itemBuilder: (_, i) => SelectionArea(
                        child: RichText(
                          text: AnsiTextParser.buildTextSpan(
                            _logs[i],
                            baseStyle: TextStyle(
                              color: logTheme.foreground,
                              fontFamily: 'monospace',
                              fontSize: 12,
                              height: 1.6,
                            ),
                            brightness: logTheme.brightness,
                          ),
                        ),
                      ),
                    ),
            ),
            if (_done)
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(12),
                color: doneBannerBackground,
                child: Text(
                  '拉取完成',
                  textAlign: TextAlign.center,
                  style: TextStyle(
                    color: logTheme.brightness == Brightness.dark
                        ? logTheme.foreground
                        : AppColors.primary,
                    fontSize: 13,
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}

String _extractRequestErrorMessage(dynamic error, String fallback) =>
    extractErrorMessage(error, fallback);

String _subscriptionLogPreview(Map<String, dynamic> log) {
  final content = log['content']?.toString() ?? '';
  for (final line in content.split('\n')) {
    final trimmed = line.trim();
    if (trimmed.isNotEmpty) {
      return trimmed;
    }
  }
  return '(无日志内容)';
}
