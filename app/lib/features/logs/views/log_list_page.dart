import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/task_log.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

final logListProvider = StateNotifierProvider<LogListNotifier, LogListState>((
  ref,
) {
  return LogListNotifier();
});

const _logStateUnset = Object();

class LogListState {
  final List<TaskLog> logs;
  final int total;
  final bool loading;
  final String? error;
  final String keyword;
  final String taskIdFilter;
  final int? statusFilter;
  const LogListState({
    this.logs = const [],
    this.total = 0,
    this.loading = false,
    this.error,
    this.keyword = '',
    this.taskIdFilter = '',
    this.statusFilter,
  });

  LogListState copyWith({
    List<TaskLog>? logs,
    int? total,
    bool? loading,
    Object? error = _logStateUnset,
    String? keyword,
    String? taskIdFilter,
    int? statusFilter,
    bool resetStatusFilter = false,
  }) {
    return LogListState(
      logs: logs ?? this.logs,
      total: total ?? this.total,
      loading: loading ?? this.loading,
      error: identical(error, _logStateUnset) ? this.error : error as String?,
      keyword: keyword ?? this.keyword,
      taskIdFilter: taskIdFilter ?? this.taskIdFilter,
      statusFilter: resetStatusFilter
          ? null
          : statusFilter ?? this.statusFilter,
    );
  }
}

class LogListNotifier extends StateNotifier<LogListState> {
  LogListNotifier() : super(const LogListState());
  int _page = 1;
  bool _loadInFlight = false;
  bool _refreshInFlight = false;
  bool _refreshQueued = false;
  bool _silentRefreshQueued = false;
  int _queryGeneration = 0;
  String? _scope;

  String _ensureScope() {
    final scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    if (_scope != scope) {
      _scope = scope;
      _queryGeneration++;
      _page = 1;
      _loadInFlight = false;
      _refreshInFlight = false;
      _refreshQueued = false;
      _silentRefreshQueued = false;
      state = const LogListState();
    }
    return scope;
  }

  Map<String, dynamic> _currentQueryParams({
    required int page,
    int pageSize = 20,
  }) {
    final params = <String, dynamic>{'page': page, 'page_size': pageSize};
    if (state.keyword.isNotEmpty) {
      params['keyword'] = state.keyword;
    }
    if (state.taskIdFilter.isNotEmpty) {
      params['task_id'] = state.taskIdFilter;
    }
    if (state.statusFilter != null) {
      params['status'] = state.statusFilter;
    }
    return params;
  }

  Future<void> load({bool refresh = false, int? targetPage}) async {
    final scope = _ensureScope();
    if (_loadInFlight || _refreshInFlight) {
      if (refresh) _refreshQueued = true;
      return;
    }

    _loadInFlight = true;
    final generation = _queryGeneration;
    final requestedPage = refresh ? 1 : targetPage ?? _page;
    state = state.copyWith(loading: true, error: null);
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.logs,
        queryParameters: _currentQueryParams(page: requestedPage),
      );
      final paginated = extractPaginated(response.data);
      final items = paginated.items.map((e) => TaskLog.fromJson(e)).toList();
      if (generation != _queryGeneration ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      _page = requestedPage;
      state = state.copyWith(
        logs: refresh ? items : [...state.logs, ...items],
        total: paginated.total,
        loading: false,
        error: null,
      );
    } catch (error) {
      if (generation != _queryGeneration ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '日志加载失败'),
      );
    } finally {
      if (generation == _queryGeneration &&
          scope == AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        _loadInFlight = false;
        await _runQueuedRefresh();
      }
    }
  }

  Future<void> loadMore() async {
    if (_loadInFlight ||
        _refreshInFlight ||
        state.logs.length >= state.total) {
      return;
    }
    await load(targetPage: _page + 1);
  }

  Future<void> refreshFirstPage() async {
    final scope = _ensureScope();
    if (_loadInFlight || _refreshInFlight) {
      _silentRefreshQueued = true;
      return;
    }
    _refreshInFlight = true;
    final generation = _queryGeneration;
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.logs,
        queryParameters: _currentQueryParams(page: 1),
      );
      final paginated = extractPaginated(response.data);
      final firstPage = paginated.items.map(TaskLog.fromJson).toList();
      if (generation != _queryGeneration ||
          scope != AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      final firstPageIds = firstPage.map((log) => log.id).toSet();
      final merged = [
        ...firstPage,
        ...state.logs
            .where((log) => !firstPageIds.contains(log.id)),
      ].take(paginated.total).toList();
      state = state.copyWith(logs: merged, total: paginated.total, error: null);
    } catch (_) {
      // 自动刷新失败时保留当前分页和滚动内容。
    } finally {
      if (generation == _queryGeneration &&
          scope == AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        _refreshInFlight = false;
        await _runQueuedRefresh();
      }
    }
  }

  Future<void> _runQueuedRefresh() async {
    if (_loadInFlight || _refreshInFlight) return;
    if (_refreshQueued) {
      _refreshQueued = false;
      _silentRefreshQueued = false;
      await load(refresh: true);
      return;
    }
    if (_silentRefreshQueued) {
      _silentRefreshQueued = false;
      await refreshFirstPage();
    }
  }

  void _invalidateQuery() {
    _queryGeneration++;
    _loadInFlight = false;
    _refreshInFlight = false;
    _refreshQueued = false;
    _silentRefreshQueued = false;
  }

  void setKeyword(String keyword) {
    _invalidateQuery();
    state = state.copyWith(keyword: keyword);
    load(refresh: true);
  }

  void setTaskIdFilter(String taskId) {
    _invalidateQuery();
    state = state.copyWith(taskIdFilter: taskId);
    load(refresh: true);
  }

  void setStatusFilter(int? status) {
    _invalidateQuery();
    state = state.copyWith(
      statusFilter: status,
      resetStatusFilter: status == null,
    );
    load(refresh: true);
  }

  Future<void> deleteLog(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.logById(id));
    await load(refresh: true);
  }

  Future<void> batchDelete(List<int> ids) async {
    await DioClient.instance.dio.post(
      ApiEndpoints.logsBatchDelete,
      data: {'ids': ids},
    );
    await load(refresh: true);
  }

  Future<int> deleteAllMatching() async {
    // 后端日志列表单页最多 100 条，这里按当前筛选条件分页取出所有日志 ID 后批量删除。
    // 上限 100 页（10000 条），防止运行中任务持续产生日志导致 total 持续增长、循环永不终止。
    const pageSize = 100;
    const maxPages = 100;
    final ids = <int>[];
    var page = 1;
    var total = 0;

    do {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.logs,
        queryParameters: _currentQueryParams(page: page, pageSize: pageSize),
      );
      final paginated = extractPaginated(response.data);
      total = paginated.total;
      final pageIds = paginated.items
          .map((entry) => TaskLog.fromJson(entry).id)
          .where((id) => id > 0)
          .toList();
      if (pageIds.isEmpty) {
        break;
      }
      ids.addAll(pageIds);
      page++;
    } while (ids.length < total && page <= maxPages);

    if (ids.isEmpty) {
      await load(refresh: true);
      return 0;
    }

    await DioClient.instance.dio.post(
      ApiEndpoints.logsBatchDelete,
      data: {'ids': ids},
    );
    await load(refresh: true);
    return ids.length;
  }

  Future<void> clean({int? days}) async {
    await DioClient.instance.dio.delete(
      ApiEndpoints.logsClean,
      queryParameters: days == null ? null : {'days': days},
    );
    await load(refresh: true);
  }
}

class LogListPage extends ConsumerStatefulWidget {
  const LogListPage({super.key});

  @override
  ConsumerState<LogListPage> createState() => _LogListPageState();
}

class _LogListPageState extends ConsumerState<LogListPage>
    with WidgetsBindingObserver {
  final _scrollController = ScrollController();
  final _searchController = TextEditingController();
  Timer? _refreshTimer;
  Timer? _debounce;
  bool _selectionMode = false;
  bool _appResumed = true;
  final Set<int> _selectedIds = <int>{};

  @override
  void initState() {
    super.initState();
    _appResumed =
        WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;
    WidgetsBinding.instance.addObserver(this);
    Future.microtask(
      () => ref.read(logListProvider.notifier).load(refresh: true),
    );
    _scrollController.addListener(() {
      if (_scrollController.position.pixels >=
          _scrollController.position.maxScrollExtent - 200) {
        ref.read(logListProvider.notifier).loadMore();
      }
    });
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _debounce?.cancel();
    _refreshTimer?.cancel();
    _searchController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  void _syncAutoRefresh(LogListState state) {
    final hasRunning = state.logs.any((log) => log.isRunning);
    if (hasRunning && _appResumed) {
      _refreshTimer ??= Timer.periodic(const Duration(seconds: 5), (_) {
        ref.read(logListProvider.notifier).refreshFirstPage();
      });
    } else {
      _refreshTimer?.cancel();
      _refreshTimer = null;
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _appResumed = state == AppLifecycleState.resumed;
    if (!_appResumed) {
      _refreshTimer?.cancel();
      _refreshTimer = null;
      return;
    }

    final notifier = ref.read(logListProvider.notifier);
    notifier.refreshFirstPage();
    _syncAutoRefresh(ref.read(logListProvider));
  }

  void _resetScroll() {
    if (_scrollController.hasClients && _scrollController.offset > 0) {
      _scrollController.jumpTo(0);
    }
  }

  void _showMessage(String message) {
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(context, message);
  }

  String _extractError(Object error, String fallback) {
    return extractErrorMessage(error, fallback);
  }

  void _enterSelectionModeWith(int id) {
    setState(() {
      _selectionMode = true;
      _selectedIds
        ..clear()
        ..add(id);
    });
  }

  void _exitSelectionMode() {
    setState(() {
      _selectionMode = false;
      _selectedIds.clear();
    });
  }

  void _toggleSelection(int id) {
    setState(() {
      if (_selectedIds.contains(id)) {
        _selectedIds.remove(id);
        if (_selectedIds.isEmpty) _selectionMode = false;
      } else {
        _selectedIds.add(id);
      }
    });
  }

  void _toggleSelectAll(List<TaskLog> logs) {
    setState(() {
      if (_selectedIds.length == logs.length) {
        _selectedIds.clear();
      } else {
        _selectedIds
          ..clear()
          ..addAll(logs.map((l) => l.id));
      }
    });
  }

  Future<void> _batchDeleteSelected() async {
    if (_selectedIds.isEmpty) return;
    final count = _selectedIds.length;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('批量删除'),
        content: Text('确定要删除选中的 $count 条日志吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)),
          AppGlassDialogAction(label: '删除', onPressed: () => Navigator.pop(ctx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirmed != true) return;
    try {
      await ref
          .read(logListProvider.notifier)
          .batchDelete(_selectedIds.toList());
      if (!mounted) return;
      _exitSelectionMode();
      _showMessage('已删除 $count 条日志');
    } catch (e) {
      _showMessage(_extractError(e, '批量删除失败'));
    }
  }

  Future<void> _showCleanDialog() async {
    final days = await showDialog<int>(
      context: context,
      builder: (ctx) => SimpleDialog(
        title: const Text('清理旧日志'),
        children: [
          SimpleDialogOption(
            onPressed: () => Navigator.pop(ctx, 3),
            child: const Text('清理 3 天前的日志'),
          ),
          SimpleDialogOption(
            onPressed: () => Navigator.pop(ctx, 7),
            child: const Text('清理 7 天前的日志'),
          ),
          SimpleDialogOption(
            onPressed: () => Navigator.pop(ctx, 30),
            child: const Text('清理 30 天前的日志'),
          ),
          SimpleDialogOption(
            onPressed: () => Navigator.pop(ctx, 0),
            child: const Text('清理全部日志', style: TextStyle(color: Colors.red)),
          ),
        ],
      ),
    );
    if (days == null) return;

    if (days == 0) {
      if (!mounted) {
        return;
      }
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('清理全部日志'),
          content: const Text('确定要清理当前筛选条件下的全部日志吗？此操作不可恢复。'),
          actions: [AppLiquidGlassDialogActions(actions: [
            AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)),
            AppGlassDialogAction(label: '清理', onPressed: () => Navigator.pop(ctx, true), variant: AppLiquidGlassButtonVariant.danger),
          ])],
        ),
      );
      if (confirmed != true) return;
    }

    try {
      if (days == 0) {
        // 后端 clean 接口会自动套用默认保留天数，不能表达“全部清空”。
        // 所以这里改为读取当前筛选条件下全部日志 ID，再走批量删除接口。
        final count = await ref
            .read(logListProvider.notifier)
            .deleteAllMatching();
        if (!mounted) return;
        _exitSelectionMode();
        _showMessage(count == 0 ? '暂无可清理日志' : '已清理 $count 条日志');
        return;
      }

      await ref.read(logListProvider.notifier).clean(days: days);
      if (!mounted) return;
      _exitSelectionMode();
      _showMessage('已清理 $days 天前的日志');
    } catch (e) {
      _showMessage(_extractError(e, '清理失败'));
    }
  }

  Future<void> _handleDelete(TaskLog log) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('删除日志'),
        content: Text('确定要删除日志 #${log.id} 吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx, false)),
          AppGlassDialogAction(label: '删除', onPressed: () => Navigator.pop(dialogCtx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirmed != true) {
      return;
    }
    try {
      await ref.read(logListProvider.notifier).deleteLog(log.id);
      _showMessage('日志已删除');
    } catch (error) {
      _showMessage(_extractError(error, '删除失败'));
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen<LogListState>(logListProvider, (_, next) {
      _syncAutoRefresh(next);
    });
    final state = ref.watch(logListProvider);
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
                  Expanded(
                    child: _selectionMode
                        ? Text(
                            '已选 ${_selectedIds.length} 条',
                            style: const TextStyle(
                              fontSize: 18,
                              fontWeight: FontWeight.w700,
                            ),
                          )
                        : const Text(
                            '运行日志',
                            style: TextStyle(
                              fontSize: 24,
                              fontWeight: FontWeight.w700,
                            ),
                          ),
                  ),
                  if (_selectionMode) ...[
                    IconButton(
                      icon: Icon(
                        _selectedIds.length == state.logs.length
                            ? Icons.deselect
                            : Icons.select_all,
                        size: 20,
                      ),
                      onPressed: () => _toggleSelectAll(state.logs),
                      tooltip: _selectedIds.length == state.logs.length
                          ? '取消全选'
                          : '全选',
                    ),
                    IconButton(
                      icon: const Icon(
                        Icons.delete_outline,
                        size: 20,
                        color: AppColors.red500,
                      ),
                      onPressed: _batchDeleteSelected,
                      tooltip: '批量删除',
                    ),
                    IconButton(
                      icon: const Icon(Icons.close, size: 20),
                      onPressed: _exitSelectionMode,
                      tooltip: '取消',
                    ),
                  ] else ...[
                    IconButton(
                      icon: const Icon(
                        Icons.cleaning_services_outlined,
                        size: 20,
                      ),
                      onPressed: _showCleanDialog,
                      tooltip: '清理日志',
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(height: 16),

            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: AppLiquidGlassInput(
                child: TextField(
                controller: _searchController,
                decoration: InputDecoration(
                  hintText: '搜索任务名称...',
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
                            if (_selectionMode) _exitSelectionMode();
                            _searchController.clear();
                            setState(() {});
                            ref.read(logListProvider.notifier).setKeyword('');
                          },
                        )
                      : null,
                ),
                style: const TextStyle(fontSize: 14),
                onChanged: (value) {
                  setState(() {});
                  if (_selectionMode) _exitSelectionMode();
                  _debounce?.cancel();
                  _debounce = Timer(const Duration(milliseconds: 300), () {
                    _resetScroll();
                    ref.read(logListProvider.notifier).setKeyword(value);
                  });
                },
                ),
              ),
            ),
            const SizedBox(height: 12),

            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Align(
                alignment: Alignment.centerLeft,
                child: Wrap(
                  spacing: 10,
                  runSpacing: 10,
                  children: [
                    _StatusFilterChip(
                      label: '全部',
                      selected: state.statusFilter == null,
                      onTap: () {
                        if (_selectionMode) _exitSelectionMode();
                        _resetScroll();
                        ref
                            .read(logListProvider.notifier)
                            .setStatusFilter(null);
                      },
                    ),
                    _StatusFilterChip(
                      label: '成功',
                      selected: state.statusFilter == 0,
                      onTap: () {
                        if (_selectionMode) _exitSelectionMode();
                        _resetScroll();
                        ref.read(logListProvider.notifier).setStatusFilter(0);
                      },
                    ),
                    _StatusFilterChip(
                      label: '失败',
                      selected: state.statusFilter == 1,
                      onTap: () {
                        if (_selectionMode) _exitSelectionMode();
                        _resetScroll();
                        ref.read(logListProvider.notifier).setStatusFilter(1);
                      },
                      selectedColor: AppColors.red500,
                    ),
                    _StatusFilterChip(
                      label: '运行中',
                      selected: state.statusFilter == 2,
                      onTap: () {
                        if (_selectionMode) _exitSelectionMode();
                        _resetScroll();
                        ref.read(logListProvider.notifier).setStatusFilter(2);
                      },
                      selectedColor: AppColors.blue500,
                    ),
                    _StatusFilterChip(
                      label: '已终止',
                      selected: state.statusFilter == 3,
                      onTap: () {
                        if (_selectionMode) _exitSelectionMode();
                        _resetScroll();
                        ref.read(logListProvider.notifier).setStatusFilter(3);
                      },
                      selectedColor: AppColors.amber500,
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 16),

            if (state.error != null && state.logs.isNotEmpty) ...[
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                child: AppCard(
                  child: Row(
                    children: [
                      const Icon(Icons.error_outline, color: AppColors.red500),
                      const SizedBox(width: 10),
                      Expanded(child: Text(state.error!)),
                      TextButton(
                        onPressed: () => ref
                            .read(logListProvider.notifier)
                            .load(refresh: true),
                        child: const Text('重试'),
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 12),
            ],

            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: () =>
                    ref.read(logListProvider.notifier).load(refresh: true),
                child: state.loading && state.logs.isEmpty
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
                    : state.error != null && state.logs.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 80, 20, 110),
                        children: [
                          AppCard(
                            child: Column(
                              children: [
                                const Icon(Icons.cloud_off_outlined, size: 42),
                                const SizedBox(height: 10),
                                Text(state.error!, textAlign: TextAlign.center),
                                const SizedBox(height: 12),
                                AppLiquidGlassButton(
                                  label: '重试',
                                  onPressed: () => ref
                                      .read(logListProvider.notifier)
                                      .load(refresh: true),
                                ),
                              ],
                            ),
                          ),
                        ],
                      )
                    : state.logs.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.article_outlined,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无日志',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        controller: _scrollController,
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
                        itemCount: state.logs.length,
                        itemBuilder: (_, i) {
                          final log = state.logs[i];
                          return _LogItem(
                            log: log,
                            isLight: isLight,
                            selectionMode: _selectionMode,
                            selected: _selectedIds.contains(log.id),
                            onView: () {
                              if (_selectionMode) {
                                _toggleSelection(log.id);
                              } else {
                                context.push('/logs/${log.id}/stream');
                              }
                            },
                            onLongPress: () {
                              if (!_selectionMode) {
                                _enterSelectionModeWith(log.id);
                              }
                            },
                            onDelete: () => _handleDelete(log),
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
}

class _LogItem extends ConsumerWidget {
  final TaskLog log;
  final bool isLight;
  final VoidCallback onView;
  final VoidCallback onDelete;
  final VoidCallback? onLongPress;
  final bool selectionMode;
  final bool selected;

  const _LogItem({
    required this.log,
    required this.isLight,
    required this.onView,
    required this.onDelete,
    this.onLongPress,
    this.selectionMode = false,
    this.selected = false,
  });

  Color _statusColor() {
    if (log.isSuccess) return AppColors.primary;
    if (log.isFailed) return AppColors.red500;
    if (log.isAborted) return AppColors.amber500;
    return AppColors.blue500;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    
    final color = _statusColor();
    return GestureDetector(
      onLongPress: onLongPress,
      child: Padding(
        padding: const EdgeInsets.only(bottom: 12),
        child: AppLiquidGlassSurface(
          borderRadius: 18,
          accentColor: AppColors.primary,
          selected: selected,
          performanceMode: true,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
          child: Row(
          crossAxisAlignment: CrossAxisAlignment.center,
          children: [
            if (selectionMode) ...[
              SizedBox(
                width: 24,
                height: 24,
                child: Checkbox(
                  value: selected,
                  onChanged: (_) => onView(),
                  activeColor: AppColors.primary,
                  shape: RoundedRectangleBorder(
                    borderRadius: BorderRadius.circular(4),
                  ),
                ),
              ),
              const SizedBox(width: 10),
            ] else ...[
              Container(
                width: 12,
                height: 12,
                decoration: BoxDecoration(color: color, shape: BoxShape.circle),
              ),
            ],
            const SizedBox(width: 12),
            Expanded(
              child: InkWell(
                onTap: onView,
                borderRadius: BorderRadius.circular(12),
                child: Padding(
                  padding: const EdgeInsets.symmetric(vertical: 2),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(
                        log.taskName ?? '任务 #${log.taskId}',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w700,
                          color: Theme.of(context).colorScheme.onSurface,
                          height: 1.35,
                        ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const SizedBox(height: 6),
                      Text(
                        '运行时间 ${formatTimeCn(log.startedAt)} · ${log.durationText}',
                        style: TextStyle(
                          fontSize: 12,
                          fontWeight: FontWeight.w500,
                          color: isLight
                              ? AppColors.slate500
                              : AppColors.slate400,
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
            const SizedBox(width: 8),
            IconButton(
              tooltip: '删除日志',
              onPressed: onDelete,
              visualDensity: VisualDensity.compact,
              splashRadius: 20,
              icon: const Icon(Icons.delete_outline, size: 20),
              color: AppColors.red500,
            ),
          ],
          ),
        ),
      ),
    );
  }
}

class _StatusFilterChip extends StatelessWidget {
  final String label;
  final bool selected;
  final VoidCallback onTap;
  final Color selectedColor;

  const _StatusFilterChip({
    required this.label,
    required this.selected,
    required this.onTap,
    this.selectedColor = AppColors.primary,
  });

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final foreground = selected
        ? selectedColor
        : (isLight ? AppColors.slate600 : AppColors.slate300);
    return AppLiquidGlassSurface(
      onTap: onTap,
      borderRadius: 18,
      accentColor: selectedColor,
      selected: selected,
      performanceMode: true,
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 14,
          fontWeight: FontWeight.w700,
          color: foreground,
          ),
      ),
    );
  }
}
