import 'dart:async';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/storage/secure_storage.dart';
import '../../../core/network/sse_client.dart';
import '../../../core/services/local_notification_service.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/task.dart';
import '../../../shared/utils/ansi_text.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/utils/log_background.dart';
import '../../../shared/utils/bounded_log_buffer.dart';
import '../../../shared/widgets/app_card.dart';
import '../../../shared/widgets/task_cron_list.dart';
import '../providers/task_provider.dart';
import '../providers/task_view_provider.dart';
import '../models/live_log_snapshot.dart';
import '../models/task_log_file.dart';

class TaskListPage extends ConsumerStatefulWidget {
  const TaskListPage({super.key});

  @override
  ConsumerState<TaskListPage> createState() => _TaskListPageState();
}

class _TaskStatusFilter {
  final String label;
  final String? value;

  const _TaskStatusFilter(this.label, this.value);
}

const _taskStatusFilters = [
  _TaskStatusFilter('全部', null),
  _TaskStatusFilter('运行中', '2'),
  _TaskStatusFilter('排队中', '0.5'),
  _TaskStatusFilter('已启用', '1'),
  _TaskStatusFilter('已禁用', '0'),
];

enum _TaskBatchAction { run, enable, disable, delete }

enum _TaskTransferAction { exportTasks, importTasks }

class _TaskListPageState extends ConsumerState<TaskListPage> {
  static const _collapsedGroupsStorageKey = 'tasks.collapsed_groups';
  static const _scrollOffsetStorageKey = 'tasks.scroll_offset';
  static const _selectedGroupStorageKey = 'tasks.selected_group';
  static const _groupOrderStorageKey = 'tasks.group_order';
  final _searchController = TextEditingController();
  final _scrollController = ScrollController();
  final Set<String> _collapsedGroups = <String>{};
  final Set<int> _selectedTaskIds = <int>{};
  final List<String> _knownGroups = <String>[];
  List<String> _groupOrder = <String>[];
  bool _groupReorderMode = false;
  bool _selectionMode = false;
  bool _taskSortMode = false;
  bool _taskOrderDirty = false;
  bool _taskTransferBusy = false;
  Timer? _debounce;
  Timer? _scrollSaveDebounce;
  bool _restoredScrollOffset = false;

  @override
  void initState() {
    super.initState();
    Future.microtask(() async {
      try {
        await ref.read(taskViewProvider.notifier).load();
      } catch (_) {
        // 任务视图属于增强能力，失败时仍加载核心任务列表。
      }
      await _restoreTaskUiState();
      if (!mounted) {
        return;
      }
      await ref.read(taskProvider.notifier).load(refresh: true);
    });
    _scrollController.addListener(_onScroll);
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent - 200) {
      ref.read(taskProvider.notifier).loadMore();
    }

    _scrollSaveDebounce?.cancel();
    _scrollSaveDebounce = Timer(const Duration(milliseconds: 400), () {
      if (!_scrollController.hasClients) {
        return;
      }
      SecureStorage.saveUiState(
        _scrollOffsetStorageKey,
        _scrollController.offset.toStringAsFixed(2),
      );
    });
  }

  void _showMessage(String message) {
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(context, message);
  }

  Future<void> _handleTaskTransferAction(_TaskTransferAction? action) async {
    if (action == null || _taskTransferBusy) {
      return;
    }
    switch (action) {
      case _TaskTransferAction.exportTasks:
        await _exportTasks();
        break;
      case _TaskTransferAction.importTasks:
        await _importTasks();
        break;
    }
  }

  Future<void> _exportTasks() async {
    setState(() => _taskTransferBusy = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.tasksExport,
        options: Options(responseType: ResponseType.bytes),
      );
      final bytes = _extractBytes(resp.data);
      if (bytes == null || bytes.isEmpty) {
        throw StateError('导出内容为空');
      }

      final savedPath = await FilePicker.platform.saveFile(
        dialogTitle: '保存任务导出文件',
        fileName: 'daidai-tasks-export.json',
        type: FileType.custom,
        allowedExtensions: const ['json'],
        bytes: bytes,
      );
      if (savedPath == null) {
        _showMessage('已取消保存');
        return;
      }
      _showMessage('任务已导出');
    } on UnsupportedError {
      _showMessage('当前平台暂不支持直接保存文件');
    } catch (error) {
      await _showActionError(error, '导出任务失败');
    } finally {
      if (mounted) {
        setState(() => _taskTransferBusy = false);
      }
    }
  }

  Future<void> _importTasks() async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: false,
      withData: false,
      withReadStream: true,
      type: FileType.custom,
      allowedExtensions: const ['json'],
      dialogTitle: '选择任务导入文件',
    );
    if (result == null || result.files.isEmpty) {
      return;
    }

    final file = result.files.first;
    final multipart = await _toMultipartFile(file);
    if (multipart == null) {
      _showMessage('无法读取所选任务文件');
      return;
    }

    setState(() => _taskTransferBusy = true);
    try {
      final formData = FormData();
      formData.files.add(MapEntry('file', multipart));
      await DioClient.instance.dio.post(
        ApiEndpoints.tasksImport,
        data: formData,
        options: Options(contentType: 'multipart/form-data'),
      );
      await ref.read(taskProvider.notifier).load(refresh: true);
      _showMessage('任务导入成功');
    } catch (error) {
      await _showActionError(error, '导入任务失败');
    } finally {
      if (mounted) {
        setState(() => _taskTransferBusy = false);
      }
    }
  }

  Future<MultipartFile?> _toMultipartFile(PlatformFile file) async {
    if (file.path != null && file.path!.isNotEmpty) {
      return MultipartFile.fromFile(file.path!, filename: file.name);
    }
    if (file.readStream != null) {
      return MultipartFile.fromStream(
        () => file.readStream!,
        file.size,
        filename: file.name,
      );
    }
    if (file.bytes != null) {
      return MultipartFile.fromBytes(file.bytes!, filename: file.name);
    }
    return null;
  }

  Uint8List? _extractBytes(dynamic data) {
    if (data is Uint8List) {
      return data;
    }
    if (data is List<int>) {
      return Uint8List.fromList(data);
    }
    if (data is List) {
      final values = data.whereType<num>().map((item) => item.toInt()).toList();
      return Uint8List.fromList(values);
    }
    return null;
  }

  Future<void> _showActionError(dynamic error, String fallback) async {
    _showMessage(_extractTaskError(error, fallback));
  }

  bool _isAllTasksSelected(List<Task> tasks) =>
      tasks.isNotEmpty &&
      tasks.every((task) => _selectedTaskIds.contains(task.id));

  void _setSelectionMode(bool enabled) {
    setState(() {
      _selectionMode = enabled;
      if (!enabled) {
        _selectedTaskIds.clear();
      }
    });
  }

  void _toggleTaskSelection(int id) {
    setState(() {
      _selectionMode = true;
      if (_selectedTaskIds.contains(id)) {
        _selectedTaskIds.remove(id);
      } else {
        _selectedTaskIds.add(id);
      }
      if (_selectedTaskIds.isEmpty) {
        _selectionMode = false;
      }
    });
  }

  void _toggleSelectAllTasks(List<Task> tasks) {
    final visibleIds = tasks.map((task) => task.id).toSet();
    setState(() {
      if (visibleIds.isNotEmpty &&
          visibleIds.every((id) => _selectedTaskIds.contains(id))) {
        _selectedTaskIds.removeAll(visibleIds);
        if (_selectedTaskIds.isEmpty) {
          _selectionMode = false;
        }
      } else {
        _selectionMode = true;
        _selectedTaskIds.addAll(visibleIds);
      }
    });
  }

  Future<bool> _confirmBatchTaskDelete(int count) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('批量删除任务'),
        content: Text('确定要删除选中的 $count 个任务吗？此操作不可恢复。'),
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
    return confirmed == true;
  }

  Future<void> _performBatchTaskAction(_TaskBatchAction action) async {
    final ids = _selectedTaskIds.toList()..sort();
    if (ids.isEmpty) {
      return;
    }

    if (action == _TaskBatchAction.run && ids.length > 10) {
      _showMessage('批量运行最多选择 10 个任务');
      return;
    }

    if (action == _TaskBatchAction.delete) {
      final confirmed = await _confirmBatchTaskDelete(ids.length);
      if (!confirmed) {
        return;
      }
    }

    try {
      final notifier = ref.read(taskProvider.notifier);
      switch (action) {
        case _TaskBatchAction.run:
          await notifier.batchRun(ids);
          break;
        case _TaskBatchAction.enable:
          await notifier.batchEnable(ids);
          break;
        case _TaskBatchAction.disable:
          await notifier.batchDisable(ids);
          break;
        case _TaskBatchAction.delete:
          await notifier.batchDelete(ids);
          break;
      }

      if (!mounted) {
        return;
      }

      _setSelectionMode(false);
      final message = switch (action) {
        _TaskBatchAction.run => '已批量运行 ${ids.length} 个任务',
        _TaskBatchAction.enable => '已批量启用 ${ids.length} 个任务',
        _TaskBatchAction.disable => '已批量禁用 ${ids.length} 个任务',
        _TaskBatchAction.delete => '已批量删除 ${ids.length} 个任务',
      };
      _showMessage(message);
    } catch (error) {
      await _showActionError(error, '批量操作失败');
    }
  }

  Future<void> _finishTaskSortMode(List<Task> tasks) async {
    if (!_taskOrderDirty) {
      setState(() => _taskSortMode = false);
      return;
    }

    try {
      await ref.read(taskProvider.notifier).saveTaskOrder(tasks);
      if (!mounted) {
        return;
      }
      setState(() {
        _taskSortMode = false;
        _taskOrderDirty = false;
      });
      _showMessage('任务排序已保存');
    } catch (error) {
      await _showActionError(error, '保存任务排序失败');
    }
  }

  Future<void> _openLatestLog(Task task) async {
    if (task.isRunning) {
      _openLiveLog(task);
      return;
    }
    try {
      final latestLog = await ref
          .read(taskProvider.notifier)
          .fetchLatestLog(task.id);
      if (!mounted) {
        return;
      }
      if (latestLog == null) {
        _showMessage('当前任务暂无日志');
        return;
      }
      context.push('/logs/${latestLog.id}/stream');
    } catch (_) {
      _showMessage('打开日志失败');
    }
  }

  void _openLiveLog(Task task) {
    context.push('/tasks/${task.id}/live-logs', extra: task.name);
  }

  Future<void> _showTaskStats(Task task) async {
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.taskStats(task.id),
      );
      final data = extractData(response.data);
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text('${task.name} 统计'),
          content: _TaskInfoDialogContent(
            emptyText: '暂无统计数据',
            lines: _formatTaskInfoLines(data),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogContext),
              child: const Text('关闭'),
            ),
          ],
        ),
      );
    } catch (error) {
      await _showActionError(error, '加载任务统计失败');
    }
  }

  Future<void> _showTaskLogFiles(Task task) async {
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.taskLogFiles(task.id),
      );
      final data = extractData(response.data);
      final files = data is List
          ? data
                .whereType<Map>()
                .map(
                  (item) => TaskLogFile.fromJson(
                    Map<String, dynamic>.from(item),
                  ),
                )
                .toList()
          : <TaskLogFile>[];
      if (!mounted) return;
      final selectedLogId = await showDialog<int>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text('${task.name} 日志文件'),
          content: _TaskLogFileList(
            files: files,
            onOpen: (logId) => Navigator.pop(dialogContext, logId),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(dialogContext),
              child: const Text('关闭'),
            ),
          ],
        ),
      );
      if (!mounted || selectedLogId == null) return;
      context.push('/logs/$selectedLogId/stream');
    } catch (error) {
      await _showActionError(error, '加载任务日志文件失败');
    }
  }

  List<String> _formatTaskInfoLines(dynamic data) {
    if (data is List) {
      return data.map((item) => item.toString()).toList();
    }
    if (data is Map) {
      return data.entries
          .map((entry) => '${entry.key}: ${entry.value}')
          .toList();
    }
    final text = data?.toString().trim() ?? '';
    return text.isEmpty ? const [] : [text];
  }

  Future<void> _runTask(Task task) async {
    try {
      await ref.read(taskProvider.notifier).runTask(task.id);
      if (!mounted) {
        return;
      }
      _openLiveLog(task);
    } catch (error) {
      final message = _extractTaskError(error, '启动任务失败');
      if (!mounted) {
        return;
      }
      if (message.contains('运行中')) {
        _openLiveLog(task);
        return;
      }
      _showMessage(message);
    }
  }

  Future<void> _stopTask(Task task) async {
    try {
      await ref.read(taskProvider.notifier).stopTask(task.id);
      _showMessage('任务已停止');
    } catch (error) {
      await _showActionError(error, '停止任务失败');
    }
  }

  Future<void> _toggleTaskEnabled(Task task) async {
    try {
      if (task.isDisabled) {
        await ref.read(taskProvider.notifier).enableTask(task.id);
        _showMessage('任务已启用');
      } else {
        await ref.read(taskProvider.notifier).disableTask(task.id);
        _showMessage(task.isRunning ? '任务已设置为完成后禁用' : '任务已禁用');
      }
    } catch (error) {
      await _showActionError(error, '更新任务状态失败');
    }
  }

  Future<void> _copyTask(Task task) async {
    try {
      await ref.read(taskProvider.notifier).copyTask(task.id);
      _showMessage('任务已复制');
    } catch (error) {
      await _showActionError(error, '复制任务失败');
    }
  }

  Future<void> _togglePinned(Task task) async {
    try {
      if (task.isPinned) {
        await ref.read(taskProvider.notifier).unpinTask(task.id);
        _showMessage('已取消置顶');
      } else {
        await ref.read(taskProvider.notifier).pinTask(task.id);
        _showMessage('已置顶任务');
      }
    } catch (error) {
      await _showActionError(error, '更新置顶状态失败');
    }
  }

  void _onSearchChanged(String value) {
    setState(() {});
    if (_selectionMode) _setSelectionMode(false);
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 300), () {
      if (_scrollController.hasClients && _scrollController.offset > 0) {
        _scrollController.jumpTo(0);
      }
      ref.read(taskProvider.notifier).setKeyword(value);
    });
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _scrollSaveDebounce?.cancel();
    if (_scrollController.hasClients) {
      SecureStorage.saveUiState(
        _scrollOffsetStorageKey,
        _scrollController.offset.toStringAsFixed(2),
      );
    }
    _searchController.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  Future<void> _restoreTaskUiState() async {
    final collapsedRaw = await SecureStorage.getUiState(
      _collapsedGroupsStorageKey,
    );
    final selectedGroup = await SecureStorage.getUiState(
      _selectedGroupStorageKey,
    );
    final groups = <String>{};
    if (collapsedRaw != null && collapsedRaw.trim().isNotEmpty) {
      groups.addAll(
        collapsedRaw
            .split('\n')
            .map((item) => item.trim())
            .where((item) => item.isNotEmpty),
      );
    } else {
      groups.add('');
    }
    if (!mounted) {
      return;
    }
    final groupOrderRaw = await SecureStorage.getUiState(_groupOrderStorageKey);
    final savedGroupOrder = <String>[];
    if (groupOrderRaw != null && groupOrderRaw.trim().isNotEmpty) {
      savedGroupOrder.addAll(groupOrderRaw.split('\n').map((s) => s.trim()));
    }
    if (!mounted) return;
    setState(() {
      _collapsedGroups
        ..clear()
        ..addAll(groups);
      _groupOrder = savedGroupOrder;
    });
    if (selectedGroup != null) {
      ref
          .read(taskProvider.notifier)
          .setLabelFilter(selectedGroup.trim().isEmpty ? null : selectedGroup);
    }
  }

  Future<void> _persistCollapsedGroups() {
    return SecureStorage.saveUiState(
      _collapsedGroupsStorageKey,
      _collapsedGroups.join('\n'),
    );
  }

  Future<void> _persistGroupOrder() {
    return SecureStorage.saveUiState(
      _groupOrderStorageKey,
      _groupOrder.join('\n'),
    );
  }

  List<_TaskGroup> _sortGroupsByOrder(List<_TaskGroup> groups) {
    if (_groupOrder.isEmpty) return groups;
    final orderMap = <String, int>{};
    for (var i = 0; i < _groupOrder.length; i++) {
      orderMap[_groupOrder[i]] = i;
    }
    groups.sort((a, b) {
      final ai = orderMap[a.key] ?? 9999;
      final bi = orderMap[b.key] ?? 9999;
      if (ai != bi) return ai.compareTo(bi);
      return 0;
    });
    return groups;
  }

  Future<void> _restoreScrollOffsetIfNeeded() async {
    if (_restoredScrollOffset || !_scrollController.hasClients) {
      return;
    }
    final raw = await SecureStorage.getUiState(_scrollOffsetStorageKey);
    if (raw == null || raw.trim().isEmpty) {
      _restoredScrollOffset = true;
      return;
    }
    final offset = double.tryParse(raw);
    if (offset == null) {
      _restoredScrollOffset = true;
      return;
    }
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) {
        return;
      }
      final maxOffset = _scrollController.position.maxScrollExtent;
      _scrollController.jumpTo(offset.clamp(0, maxOffset));
      _restoredScrollOffset = true;
    });
  }

  void _collectKnownGroups(List<Task> tasks) {
    final groups =
        tasks
            .map((task) => task.groupName?.trim() ?? '')
            .where((group) => group.isNotEmpty)
            .toSet()
            .toList()
          ..sort();
    _knownGroups
      ..clear()
      ..addAll(groups);
  }

  Future<void> _showGroupPicker() async {
    final options = [..._knownGroups];
    final selected = await showModalBottomSheet<String>(
      context: context,
      showDragHandle: true,
      builder: (sheetContext) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(
            maxHeight: MediaQuery.sizeOf(sheetContext).height * 0.7,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const ListTile(
                title: Text('选择任务分组'),
                subtitle: Text('可筛选已有分组任务'),
              ),
              Flexible(
                child: ListView.builder(
                  shrinkWrap: true,
                  itemCount: options.length + 1,
                  itemBuilder: (context, index) {
                    if (index == 0) {
                      return ListTile(
                        leading: const Icon(Icons.layers_clear_outlined),
                        title: const Text('全部分组'),
                        onTap: () => Navigator.pop(sheetContext, ''),
                      );
                    }
                    final group = options[index - 1];
                    return ListTile(
                      leading: const Icon(Icons.label_outline),
                      title: Text(group),
                      trailing: ref.watch(taskProvider).labelFilter == group
                          ? const Icon(Icons.check, color: AppColors.primary)
                          : null,
                      onTap: () => Navigator.pop(sheetContext, group),
                    );
                  },
                ),
              ),
            ],
          ),
        ),
      ),
    );

    if (!mounted || selected == null) {
      return;
    }
    if (_selectionMode) _setSelectionMode(false);

    if (_scrollController.hasClients && _scrollController.offset > 0) {
      _scrollController.jumpTo(0);
    }
    ref
        .read(taskProvider.notifier)
        .setLabelFilter(selected.isEmpty ? null : selected);
    await SecureStorage.saveUiState(_selectedGroupStorageKey, selected);
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(taskProvider);
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    final compactLayout = MediaQuery.sizeOf(context).width < 380;
    final hasActiveFilters =
        state.keyword.trim().isNotEmpty ||
        state.statusFilter != null ||
        state.labelFilter != null;

    _collectKnownGroups(state.tasks);
    final groupedTasks = _sortGroupsByOrder(_groupTasks(state.tasks));
    final taskRows = <_TaskListRow>[];
    for (final group in groupedTasks) {
      taskRows.add(_TaskListRow.group(group));
      if (!_collapsedGroups.contains(group.key)) {
        taskRows.addAll(group.tasks.map(_TaskListRow.task));
      }
    }
    final selectedCount = _selectedTaskIds.length;
    final allSelected = _isAllTasksSelected(state.tasks);
    _restoreScrollOffsetIfNeeded();

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 12),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  const Expanded(
                    child: Text(
                      '定时任务',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  Row(
                    children: [
                      PopupMenuButton<int?>(
                        tooltip: '任务视图',
                        onSelected: (id) {
                          if (id == -1) {
                            context.push('/task-views');
                            return;
                          }
                          final views = ref.read(taskViewProvider).items;
                          final view = views
                              .where((item) => item.id == id)
                              .firstOrNull;
                          ref
                              .read(taskProvider.notifier)
                              .setView(view?.filters, view?.sortRules);
                        },
                        itemBuilder: (_) => [
                          const PopupMenuItem<int?>(
                            value: null,
                            child: Text('全部任务'),
                          ),
                          ...ref
                              .watch(taskViewProvider)
                              .items
                              .where((v) => !v.hidden)
                              .map(
                                (v) => PopupMenuItem<int?>(
                                  value: v.id,
                                  child: Text(v.name),
                                ),
                              ),
                          const PopupMenuDivider(),
                          const PopupMenuItem<int?>(
                            value: -1,
                            child: Text('管理视图'),
                          ),
                        ],
                        child: const _TaskGlassIconTarget(
                          icon: Icons.view_list_outlined,
                        ),
                      ),
                      if (!_taskSortMode)
                        _TaskHeaderChipButton(
                          label: _selectionMode ? '取消' : '批量',
                          icon: _selectionMode ? Icons.close : Icons.done_all,
                          isLight: isLight,
                          compact: compactLayout,
                          onTap: () => _setSelectionMode(!_selectionMode),
                        ),
                      if (!_selectionMode && !hasActiveFilters) ...[
                        const SizedBox(width: 8),
                        _TaskHeaderChipButton(
                          label: _taskSortMode ? '完成' : '排序',
                          icon: _taskSortMode ? Icons.check : Icons.swap_vert,
                          isLight: isLight,
                          compact: compactLayout,
                          onTap: () async {
                            if (_taskSortMode) {
                              await _finishTaskSortMode(state.tasks);
                            } else {
                              setState(() {
                                _taskSortMode = true;
                                _groupReorderMode = false;
                                _taskOrderDirty = false;
                              });
                            }
                          },
                        ),
                      ],
                      if (!_selectionMode && !_taskSortMode) ...[
                        const SizedBox(width: 8),
                        AppGlassIconButton(
                          icon: Icons.add,
                          tooltip: '新建任务',
                          onTap: () => context.push('/tasks/new'),
                        ),
                      ],
                    ],
                  ),
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
                    hintText: '搜索任务名称或命令...',
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
                              setState(() {});
                              ref.read(taskProvider.notifier).setKeyword('');
                            },
                          )
                        : null,
                  ),
                  style: const TextStyle(fontSize: 14),
                  onChanged: _onSearchChanged,
                ),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              height: 38,
              child: ClipRect(
                child: ListView.separated(
                  clipBehavior: Clip.hardEdge,
                  padding: const EdgeInsets.symmetric(horizontal: 20),
                  scrollDirection: Axis.horizontal,
                  itemCount: _taskStatusFilters.length,
                  separatorBuilder: (_, index) => const SizedBox(width: 8),
                  itemBuilder: (_, index) {
                    final filter = _taskStatusFilters[index];
                    final selected = state.statusFilter == filter.value;
                    return AppLiquidGlassChoiceChip(
                      label: filter.label,
                      selected: selected,
                      onSelected: (_) {
                        if (_selectionMode) _setSelectionMode(false);
                        if (_scrollController.hasClients &&
                            _scrollController.offset > 0) {
                          _scrollController.jumpTo(0);
                        }
                        ref
                            .read(taskProvider.notifier)
                            .setStatusFilter(filter.value);
                      },
                    );
                  },
                ),
              ),
            ),
            const SizedBox(height: 10),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                children: [
                  Text(
                    '共 ${state.total} 个任务',
                    style: TextStyle(
                      fontSize: 12,
                      color: theme.colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const Spacer(),
                  ConstrainedBox(
                    constraints: BoxConstraints(
                      maxWidth: compactLayout ? 96 : 132,
                    ),
                    child: AppLiquidGlassSurface(
                      onTap: _showGroupPicker,
                      borderRadius: 12,
                      performanceMode: true,
                      padding: const EdgeInsets.symmetric(
                        horizontal: 12,
                        vertical: 10,
                      ),
                      child: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          const Icon(Icons.label_outline, size: 16),
                          const SizedBox(width: 6),
                          Flexible(
                            child: Text(
                              state.labelFilter?.isNotEmpty == true
                                  ? state.labelFilter!
                                  : '全部分组',
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ),
                  PopupMenuButton<_TaskTransferAction>(
                    tooltip: '导入导出',
                    enabled: !_taskTransferBusy,
                    onSelected: _handleTaskTransferAction,
                    itemBuilder: (_) => const [
                      PopupMenuItem(
                        value: _TaskTransferAction.exportTasks,
                        child: ListTile(
                          leading: Icon(Icons.file_download_outlined),
                          title: Text('导出任务'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                      ),
                      PopupMenuItem(
                        value: _TaskTransferAction.importTasks,
                        child: ListTile(
                          leading: Icon(Icons.file_upload_outlined),
                          title: Text('导入任务'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                      ),
                    ],
                    child: _TaskGlassIconTarget(
                      icon: Icons.more_horiz,
                      loading: _taskTransferBusy,
                    ),
                  ),
                  if (state.statusFilter != null || state.labelFilter != null)
                    compactLayout
                        ? _TaskGlassIconTarget(
                            icon: Icons.filter_alt_off,
                            tooltip: '清除筛选',
                            onTap: () {
                              if (_selectionMode) _setSelectionMode(false);
                              if (_scrollController.hasClients &&
                                  _scrollController.offset > 0) {
                                _scrollController.jumpTo(0);
                              }
                              ref
                                  .read(taskProvider.notifier)
                                  .setStatusFilter(null);
                              ref
                                  .read(taskProvider.notifier)
                                  .setLabelFilter(null);
                              SecureStorage.saveUiState(
                                _selectedGroupStorageKey,
                                '',
                              );
                            },
                          )
                        : AppLiquidGlassSurface(
                            onTap: () {
                              if (_selectionMode) _setSelectionMode(false);
                              if (_scrollController.hasClients &&
                                  _scrollController.offset > 0) {
                                _scrollController.jumpTo(0);
                              }
                              ref
                                  .read(taskProvider.notifier)
                                  .setStatusFilter(null);
                              ref
                                  .read(taskProvider.notifier)
                                  .setLabelFilter(null);
                              SecureStorage.saveUiState(
                                _selectedGroupStorageKey,
                                '',
                              );
                            },
                            borderRadius: 12,
                            performanceMode: true,
                            padding: const EdgeInsets.symmetric(
                              horizontal: 12,
                              vertical: 10,
                            ),
                            child: const Text('清除筛选'),
                          ),
                ],
              ),
            ),
            if (_selectionMode) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: Row(
                    children: [
                      _TaskBatchActionButton(
                        label: allSelected ? '取消全选' : '全选',
                        icon: allSelected ? Icons.deselect : Icons.select_all,
                        color: AppColors.slate500,
                        isLight: isLight,
                        enabled: state.tasks.isNotEmpty,
                        onTap: () => _toggleSelectAllTasks(state.tasks),
                      ),
                      const SizedBox(width: 8),
                      _TaskBatchActionButton(
                        label: '批量运行',
                        icon: Icons.play_circle_outline,
                        color: AppColors.primary,
                        isLight: isLight,
                        enabled: selectedCount > 0,
                        onTap: () =>
                            _performBatchTaskAction(_TaskBatchAction.run),
                      ),
                      const SizedBox(width: 8),
                      _TaskBatchActionButton(
                        label: '批量启用',
                        icon: Icons.toggle_on_outlined,
                        color: AppColors.primary,
                        isLight: isLight,
                        enabled: selectedCount > 0,
                        onTap: () =>
                            _performBatchTaskAction(_TaskBatchAction.enable),
                      ),
                      const SizedBox(width: 8),
                      _TaskBatchActionButton(
                        label: '批量禁用',
                        icon: Icons.toggle_off_outlined,
                        color: AppColors.slate500,
                        isLight: isLight,
                        enabled: selectedCount > 0,
                        onTap: () =>
                            _performBatchTaskAction(_TaskBatchAction.disable),
                      ),
                      const SizedBox(width: 8),
                      _TaskBatchActionButton(
                        label: '批量删除',
                        icon: Icons.delete_outline,
                        color: AppColors.red500,
                        isLight: isLight,
                        enabled: selectedCount > 0,
                        onTap: () =>
                            _performBatchTaskAction(_TaskBatchAction.delete),
                      ),
                    ],
                  ),
                ),
              ),
            ],
            if (_taskSortMode) ...[
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
                child: AppLiquidGlassSurface(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 10,
                  ),
                  borderRadius: 10,
                  accentColor: AppColors.primary,
                  selected: true,
                  performanceMode: true,
                  child: const Row(
                    children: [
                      Icon(Icons.swap_vert, size: 16, color: AppColors.primary),
                      SizedBox(width: 8),
                      Expanded(
                        child: Text(
                          '长按拖拽调整当前任务列表顺序，点击「完成」保存',
                          style: TextStyle(
                            fontSize: 12,
                            color: AppColors.primary,
                            fontWeight: FontWeight.w500,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: () =>
                    ref.read(taskProvider.notifier).load(refresh: true),
                child: state.loading && state.tasks.isEmpty
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
                    : state.tasks.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [_buildEmpty()],
                      )
                    : _taskSortMode
                    ? _buildTaskReorderView(state.tasks)
                    : _groupReorderMode
                    ? _buildGroupReorderView(groupedTasks)
                    : ListView.builder(
                        controller: _scrollController,
                        clipBehavior: Clip.hardEdge,
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
                        itemCount: taskRows.length,
                        itemBuilder: (context, index) {
                          final row = taskRows[index];
                          if (row.group != null) {
                            return _buildTaskGroupHeader(row.group!);
                          }
                          return _buildTaskCard(row.task!, isLight);
                        },
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEmpty() {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.inbox_outlined,
            size: 56,
            color: AppColors.slate400.withAlpha(120),
          ),
          const SizedBox(height: 12),
          const Text(
            '暂无任务',
            style: TextStyle(color: AppColors.slate400, fontSize: 15),
          ),
        ],
      ),
    );
  }

  List<_TaskGroup> _groupTasks(List<Task> tasks) {
    final groups = <_TaskGroup>[];
    final map = <String, _TaskGroup>{};

    for (final task in tasks) {
      final groupName = task.groupName?.trim();
      final key = (groupName == null || groupName.isEmpty) ? '' : groupName;
      final title = key.isEmpty ? '未分组' : key;
      final entry = map.putIfAbsent(key, () {
        final created = _TaskGroup(key: key, title: title);
        groups.add(created);
        return created;
      });
      entry.tasks.add(task);
    }

    return groups;
  }

  Future<void> _renameGroup(String oldName, List<Task> tasks) async {
    final controller = TextEditingController(text: oldName);
    final newName = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('重命名分组'),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: '分组名称',
            hintText: '输入新的分组名称',
          ),
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(ctx),
              ),
              AppGlassDialogAction(
                label: '确定',
                onPressed: () => Navigator.pop(ctx, controller.text.trim()),
                variant: AppLiquidGlassButtonVariant.primary,
              ),
            ],
          ),
        ],
      ),
    );
    controller.dispose();
    if (newName == null || newName.isEmpty || newName == oldName) return;
    try {
      await ref
          .read(taskProvider.notifier)
          .batchUpdateGroupLabel(
            tasks: tasks,
            oldGroupName: oldName,
            newGroupName: newName,
          );
      if (mounted) {
        AppGlassNotice.show(
          context,
          '已将分组 "$oldName" 重命名为 "$newName"',
          type: AppGlassNoticeType.success,
        );
      }
    } catch (e) {
      if (mounted) {
        AppGlassNotice.show(context, '重命名分组失败', type: AppGlassNoticeType.error);
      }
    }
  }

  Future<void> _deleteGroup(String groupName, List<Task> tasks) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除分组'),
        content: Text('确定将 "$groupName" 分组中的 ${tasks.length} 个任务移回未分组？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(ctx, false),
              ),
              AppGlassDialogAction(
                label: '确定',
                onPressed: () => Navigator.pop(ctx, true),
                variant: AppLiquidGlassButtonVariant.danger,
              ),
            ],
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await ref
          .read(taskProvider.notifier)
          .batchUpdateGroupLabel(
            tasks: tasks,
            oldGroupName: groupName,
            newGroupName: null,
          );
      if (mounted) {
        AppGlassNotice.show(
          context,
          '已删除分组 "$groupName"',
          type: AppGlassNoticeType.success,
        );
      }
    } catch (e) {
      if (mounted) {
        AppGlassNotice.show(context, '删除分组失败', type: AppGlassNoticeType.error);
      }
    }
  }

  Future<void> _addTasksToGroup(
    String targetGroup,
    List<Task> ungroupedTasks,
  ) async {
    if (ungroupedTasks.isEmpty) {
      AppGlassNotice.show(
        context,
        '没有未分组的任务可添加',
        type: AppGlassNoticeType.info,
      );
      return;
    }
    final selected = <int>{};
    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: Text('添加任务到 "$targetGroup"'),
          content: SizedBox(
            width: double.maxFinite,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(ctx).height * 0.55,
              ),
              child: ListView.builder(
                shrinkWrap: true,
                itemCount: ungroupedTasks.length,
                itemBuilder: (ctx, i) {
                  final task = ungroupedTasks[i];
                  return CheckboxListTile(
                    value: selected.contains(task.id),
                    title: Text(
                      task.name,
                      style: const TextStyle(fontSize: 14),
                    ),
                    dense: true,
                    onChanged: (v) {
                      setDialogState(() {
                        if (v == true) {
                          selected.add(task.id);
                        } else {
                          selected.remove(task.id);
                        }
                      });
                    },
                  );
                },
              ),
            ),
          ),
          actions: [
            AppLiquidGlassDialogActions(
              actions: [
                AppGlassDialogAction(
                  label: '取消',
                  onPressed: () => Navigator.pop(ctx),
                ),
                AppGlassDialogAction(
                  label: '添加 (${selected.length})',
                  variant: AppLiquidGlassButtonVariant.primary,
                  onPressed: () async {
                    Navigator.pop(ctx);
                    if (selected.isEmpty) return;
                    final tasksToMove = ungroupedTasks
                        .where((t) => selected.contains(t.id))
                        .toList();
                    try {
                      await ref
                          .read(taskProvider.notifier)
                          .batchUpdateGroupLabel(
                            tasks: tasksToMove,
                            oldGroupName: null,
                            newGroupName: targetGroup,
                          );
                      if (mounted) {
                        AppGlassNotice.show(
                          context,
                          '已将 ${tasksToMove.length} 个任务添加到 "$targetGroup"',
                          type: AppGlassNoticeType.success,
                        );
                      }
                    } catch (e) {
                      if (mounted) {
                        AppGlassNotice.show(
                          context,
                          '添加任务到分组失败',
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
      ),
    );
  }

  Future<void> _showCreateGroupFromUngrouped(List<Task> ungroupedTasks) async {
    final nameController = TextEditingController();
    final selected = <int>{};
    await showDialog(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: const Text('新建分组'),
          content: SizedBox(
            width: double.maxFinite,
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(ctx).height * 0.6,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  TextField(
                    controller: nameController,
                    autofocus: true,
                    decoration: const InputDecoration(
                      labelText: '分组名称',
                      hintText: '输入新分组的名称',
                    ),
                  ),
                  const SizedBox(height: 16),
                  const Text(
                    '选择要加入的任务:',
                    style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                  ),
                  const SizedBox(height: 8),
                  Expanded(
                    child: ListView.builder(
                      itemCount: ungroupedTasks.length,
                      itemBuilder: (ctx, i) {
                        final task = ungroupedTasks[i];
                        return CheckboxListTile(
                          value: selected.contains(task.id),
                          title: Text(
                            task.name,
                            style: const TextStyle(fontSize: 14),
                          ),
                          dense: true,
                          onChanged: (v) {
                            setDialogState(() {
                              if (v == true) {
                                selected.add(task.id);
                              } else {
                                selected.remove(task.id);
                              }
                            });
                          },
                        );
                      },
                    ),
                  ),
                ],
              ),
            ),
          ),
          actions: [
            AppLiquidGlassDialogActions(
              actions: [
                AppGlassDialogAction(
                  label: '取消',
                  onPressed: () => Navigator.pop(ctx),
                ),
                AppGlassDialogAction(
                  label: '创建',
                  variant: AppLiquidGlassButtonVariant.primary,
                  onPressed: () async {
                    final groupName = nameController.text.trim();
                    Navigator.pop(ctx);
                    if (groupName.isEmpty || selected.isEmpty) return;
                    final tasksToMove = ungroupedTasks
                        .where((t) => selected.contains(t.id))
                        .toList();
                    try {
                      await ref
                          .read(taskProvider.notifier)
                          .batchUpdateGroupLabel(
                            tasks: tasksToMove,
                            oldGroupName: null,
                            newGroupName: groupName,
                          );
                      if (mounted) {
                        AppGlassNotice.show(
                          context,
                          '已创建分组 "$groupName" 并添加 ${tasksToMove.length} 个任务',
                          type: AppGlassNoticeType.success,
                        );
                      }
                    } catch (e) {
                      if (mounted) {
                        AppGlassNotice.show(
                          context,
                          '创建分组失败',
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
      ),
    );
    nameController.dispose();
  }

  Widget _buildGroupReorderView(List<_TaskGroup> groups) {
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
          child: Row(
            children: [
              const Icon(Icons.swap_vert, size: 18),
              const SizedBox(width: 8),
              const Expanded(
                child: Text(
                  '长按拖拽调整分组顺序',
                  style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
                ),
              ),
              TextButton(
                onPressed: () => setState(() => _groupReorderMode = false),
                child: const Text('完成'),
              ),
            ],
          ),
        ),
        Expanded(
          child: ReorderableListView.builder(
            clipBehavior: Clip.hardEdge,
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
            itemCount: groups.length,
            onReorder: (oldIndex, newIndex) {
              setState(() {
                final item = groups.removeAt(oldIndex);
                groups.insert(newIndex, item);
                _groupOrder = groups.map((g) => g.key).toList();
              });
              _persistGroupOrder();
            },
            itemBuilder: (ctx, i) {
              final group = groups[i];
              return AppCard(
                key: ValueKey(group.key),
                margin: const EdgeInsets.only(bottom: 8),
                stableForScrolling: true,
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 14,
                ),
                borderRadius: 14,
                child: Row(
                  children: [
                    const Icon(
                      Icons.drag_handle,
                      size: 20,
                      color: AppColors.slate400,
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Text(
                        group.title,
                        style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ),
                    Text(
                      '${group.tasks.length} 条',
                      style: const TextStyle(
                        fontSize: 12,
                        color: AppColors.slate400,
                      ),
                    ),
                  ],
                ),
              );
            },
          ),
        ),
      ],
    );
  }

  Widget _buildTaskReorderView(List<Task> tasks) {
    return ReorderableListView.builder(
      clipBehavior: Clip.hardEdge,
      padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
      itemCount: tasks.length,
      onReorder: (oldIndex, newIndex) {
        // 只先调整本地顺序，等用户点击“完成”后再统一保存到后端，避免拖一下就请求多次。
        ref.read(taskProvider.notifier).reorderLocalTasks(oldIndex, newIndex);
        setState(() => _taskOrderDirty = true);
      },
      itemBuilder: (context, index) {
        final task = tasks[index];
        return RepaintBoundary(
          key: ValueKey('task-sort-${task.id}'),
          child: AppCard(
            margin: const EdgeInsets.only(bottom: 8),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
            borderRadius: 14,
            stableForScrolling: true,
            child: Row(
              children: [
                const Icon(
                  Icons.drag_handle,
                  size: 20,
                  color: AppColors.slate400,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        task.name,
                        style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w700,
                        ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                      const SizedBox(height: 4),
                      Text(
                        task.groupName?.isNotEmpty == true
                            ? '分组：${task.groupName}'
                            : '未分组',
                        style: const TextStyle(
                          fontSize: 11,
                          color: AppColors.slate400,
                        ),
                      ),
                    ],
                  ),
                ),
                _MetaChip(label: task.statusText, active: !task.isDisabled),
              ],
            ),
          ),
        );
      },
    );
  }

  Widget _buildTaskGroupHeader(_TaskGroup group) {
    final collapsed = _collapsedGroups.contains(group.key);
    final enabledCount = group.tasks.where((task) => task.isEnabled).length;
    final runningCount = group.tasks.where((task) => task.isRunning).length;
    final isUngrouped = group.key.isEmpty;
    final currentState = ref.read(taskProvider);
    final canModifyGroup =
        currentState.keyword.trim().isEmpty &&
        currentState.statusFilter == null &&
        currentState.labelFilter == null;

    void requireUnfiltered(VoidCallback action) {
      if (!canModifyGroup) {
        _showMessage('请先清除筛选后再修改任务分组');
        return;
      }
      action();
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = constraints.maxWidth < 340;
        return Padding(
          padding: const EdgeInsets.only(bottom: 8),
          child: GestureDetector(
            onTap: () {
              setState(() {
                if (collapsed) {
                  _collapsedGroups.remove(group.key);
                } else {
                  _collapsedGroups.add(group.key);
                }
              });
              _persistCollapsedGroups();
            },
            onLongPress: () {
              final current = ref.read(taskProvider);
              if (current.keyword.trim().isNotEmpty ||
                  current.statusFilter != null ||
                  current.labelFilter != null) {
                _showMessage('请先清除筛选后再调整分组顺序');
                return;
              }
              HapticFeedback.mediumImpact();
              setState(() => _groupReorderMode = true);
            },
            child: AppLiquidGlassSurface(
              borderRadius: 14,
              performanceMode: true,
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              child: Row(
                children: [
                  Icon(
                    collapsed ? Icons.chevron_right : Icons.expand_more,
                    size: 20,
                    color: AppColors.slate400,
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      group.title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  Text(
                    '${group.tasks.length} 条',
                    style: TextStyle(
                      fontSize: 12,
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                  const SizedBox(width: 10),
                  if (!compact)
                    if (runningCount > 0)
                      _MetaChip(label: '$runningCount 运行中', active: true)
                    else
                      _MetaChip(
                        label: '$enabledCount 已启用',
                        active: enabledCount > 0,
                      ),
                  const SizedBox(width: 4),
                  _GroupPopupMenu(
                    isUngrouped: isUngrouped,
                    onRename: isUngrouped
                        ? null
                        : () => requireUnfiltered(
                            () => _renameGroup(group.key, group.tasks),
                          ),
                    onDelete: isUngrouped
                        ? null
                        : () => requireUnfiltered(
                            () => _deleteGroup(group.key, group.tasks),
                          ),
                    onAddTasks: () {
                      requireUnfiltered(() {
                        final allTasks = ref.read(taskProvider).tasks;
                        final ungrouped = allTasks
                            .where((t) => (t.groupName ?? '').isEmpty)
                            .toList();
                        final targetGroup = isUngrouped ? null : group.key;
                        if (targetGroup == null) {
                          _showCreateGroupFromUngrouped(ungrouped);
                        } else {
                          _addTasksToGroup(targetGroup, ungrouped);
                        }
                      });
                    },
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _buildTaskCard(Task task, bool isLight) {
    return _TaskCard(
      key: ValueKey('task-card-${task.id}'),
      task: task,
      isLight: isLight,
      selectionMode: _selectionMode,
      selected: _selectedTaskIds.contains(task.id),
      onTap: () =>
          _selectionMode ? _toggleTaskSelection(task.id) : _openLatestLog(task),
      onLongPress: () {
        HapticFeedback.mediumImpact();
        _toggleTaskSelection(task.id);
      },
      onSelectedChanged: () => _toggleTaskSelection(task.id),
      onRun: () => _runTask(task),
      onStop: () => _stopTask(task),
      onAction: (action) {
        switch (action) {
          case _TaskItemAction.toggleEnabled:
            _toggleTaskEnabled(task);
            return;
          case _TaskItemAction.togglePinned:
            _togglePinned(task);
            return;
          case _TaskItemAction.copy:
            _copyTask(task);
            return;
          case _TaskItemAction.stats:
            _showTaskStats(task);
            return;
          case _TaskItemAction.logFiles:
            _showTaskLogFiles(task);
            return;
          case _TaskItemAction.edit:
            context.push('/tasks/edit', extra: task);
            return;
          case _TaskItemAction.delete:
            _confirmDelete(task);
            return;
        }
      },
    );
  }

  Future<void> _confirmDelete(Task task) async {
    final scriptPath = _extractScriptPathFromCommand(task.command);
    var deleteScript = false;
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('删除任务'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('确定要删除「${task.name}」吗？'),
              if (scriptPath != null) ...[
                const SizedBox(height: 14),
                CheckboxListTile(
                  value: deleteScript,
                  contentPadding: EdgeInsets.zero,
                  dense: true,
                  controlAffinity: ListTileControlAffinity.leading,
                  title: const Text('同时删除关联脚本'),
                  subtitle: Text(scriptPath),
                  onChanged: (value) {
                    setDialogState(() => deleteScript = value ?? false);
                  },
                ),
              ],
            ],
          ),
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
      ),
    );
    if (confirm != true) {
      return;
    }
    try {
      await ref.read(taskProvider.notifier).deleteTask(task.id);
      if (deleteScript && scriptPath != null) {
        try {
          await DioClient.instance.dio.delete(
            ApiEndpoints.scripts,
            queryParameters: {'path': scriptPath, 'type': 'file'},
          );
          _showMessage('任务和关联脚本已删除');
        } catch (error) {
          _showMessage(
            '任务已删除，但脚本删除失败：${extractErrorMessage(error, '请稍后手动删除脚本')}',
          );
        }
        return;
      }
      _showMessage('任务已删除');
    } catch (error) {
      await _showActionError(error, '删除任务失败');
    }
  }
}

class _TaskInfoDialogContent extends StatelessWidget {
  final List<String> lines;
  final String emptyText;

  const _TaskInfoDialogContent({required this.lines, required this.emptyText});

  @override
  Widget build(BuildContext context) {
    if (lines.isEmpty) {
      return Text(emptyText);
    }
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 420, maxHeight: 360),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: lines
              .map(
                (line) => Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: SelectableText(
                    line,
                    style: const TextStyle(fontSize: 13),
                  ),
                ),
              )
              .toList(),
        ),
      ),
    );
  }
}

class _TaskLogFileList extends StatelessWidget {
  final List<TaskLogFile> files;
  final ValueChanged<int> onOpen;

  const _TaskLogFileList({required this.files, required this.onOpen});

  String _formatSize(int size) {
    if (size < 1024) return '$size B';
    if (size < 1024 * 1024) return '${(size / 1024).toStringAsFixed(1)} KB';
    return '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
  }

  @override
  Widget build(BuildContext context) {
    if (files.isEmpty) return const Text('暂无日志文件');
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 440, maxHeight: 420),
      child: ListView.separated(
        shrinkWrap: true,
        itemCount: files.length,
        separatorBuilder: (_, _) => const SizedBox(height: 8),
        itemBuilder: (context, index) {
          final file = files[index];
          final canOpen = file.logId != null;
          return AppLiquidGlassSurface(
            onTap: canOpen ? () => onOpen(file.logId!) : null,
            borderRadius: 12,
            performanceMode: true,
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
            child: Row(
              children: [
                Icon(
                  canOpen ? Icons.description_outlined : Icons.file_present,
                  color: canOpen ? AppColors.primary : AppColors.slate400,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        file.filename.isEmpty ? file.path : file.filename,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 3),
                      Text(
                        '${_formatSize(file.size)} · ${formatTimeCn(file.createdAt)}',
                        style: TextStyle(
                          fontSize: 11,
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                      if (file.contractError != null)
                        Text(
                          file.contractError!,
                          style: const TextStyle(
                            fontSize: 11,
                            color: AppColors.red500,
                          ),
                        ),
                    ],
                  ),
                ),
                if (canOpen)
                  const Icon(
                    Icons.chevron_right,
                    size: 20,
                    color: AppColors.slate400,
                  ),
              ],
            ),
          );
        },
      ),
    );
  }
}

enum _TaskItemAction {
  toggleEnabled,
  togglePinned,
  copy,
  stats,
  logFiles,
  edit,
  delete,
}

class _TaskCard extends StatelessWidget {
  final Task task;
  final bool isLight;
  final bool selectionMode;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onLongPress;
  final VoidCallback onSelectedChanged;
  final VoidCallback onRun;
  final VoidCallback onStop;
  final ValueChanged<_TaskItemAction> onAction;

  const _TaskCard({
    super.key,
    required this.task,
    required this.isLight,
    required this.selectionMode,
    required this.selected,
    required this.onTap,
    required this.onLongPress,
    required this.onSelectedChanged,
    required this.onRun,
    required this.onStop,
    required this.onAction,
  });

  Color _dotColor() {
    if (task.isRunning) {
      return AppColors.primary;
    }
    if (task.isQueued) {
      return AppColors.amber500;
    }
    if (task.lastRunStatus == 1) {
      return AppColors.red500;
    }
    if (task.isEnabled) {
      return AppColors.primary;
    }
    return AppColors.slate300;
  }

  String _statusLabel() {
    if (task.isRunning) {
      return '运行中';
    }
    if (task.isQueued) {
      return '排队中';
    }
    if (task.isEnabled) {
      return '已启用';
    }
    return '已禁用';
  }

  Color _statusBg() {
    if (task.isRunning) {
      return AppColors.primary.withAlpha(isLight ? 18 : 25);
    }
    if (task.isQueued) {
      return AppColors.amber500.withAlpha(isLight ? 18 : 25);
    }
    if (task.isEnabled) {
      return AppColors.blue500.withAlpha(isLight ? 20 : 25);
    }
    return AppColors.slate500.withAlpha(isLight ? 14 : 24);
  }

  Color _statusFg() {
    if (task.isRunning) {
      return isLight ? const Color(0xFF047857) : AppColors.primary;
    }
    if (task.isQueued) {
      return AppColors.amber500;
    }
    if (task.isEnabled) {
      return isLight ? AppColors.blue600 : AppColors.blue500;
    }
    return AppColors.slate500;
  }

  String _taskTypeLabel() {
    switch (task.taskType) {
      case 'manual':
        return '手动运行';
      case 'startup':
        return '开机运行';
      default:
        return '常规定时';
    }
  }

  List<String> _scheduleExpressions() {
    if (task.cronExpressions.isNotEmpty) {
      return task.cronExpressions;
    }
    if (task.cronExpression.trim().isNotEmpty) {
      return [task.cronExpression.trim()];
    }
    return const [];
  }

  String _bottomText() {
    if (task.isRunning) {
      return '点击查看实时日志';
    }
    if (task.lastRunStatus == 1 && task.lastRunAt != null) {
      return '上次失败：${formatTimeCn(task.lastRunAt, short: true)}';
    }
    if (task.nextRunAt != null) {
      return '下次运行：${formatTimeCn(task.nextRunAt, short: true)}';
    }
    if (task.taskType == 'manual') {
      return '手动触发';
    }
    if (task.taskType == 'startup') {
      return '面板启动时自动执行';
    }
    return '暂无计划';
  }

  @override
  Widget build(BuildContext context) {
    final dotColor = _dotColor();
    final labels = task.userLabelsForDisplay;
    final hasFailure = task.lastRunStatus == 1;
    final primaryColor = task.isRunning ? AppColors.red500 : AppColors.primary;

    return GestureDetector(
      onTap: onTap,
      onLongPress: onLongPress,
      child: Padding(
        padding: const EdgeInsets.only(bottom: 10),
        child: AppLiquidGlassSurface(
          borderRadius: 16,
          performanceMode: true,
          selected: selected || hasFailure,
          accentColor: selected
              ? AppColors.primary
              : hasFailure
              ? AppColors.red500
              : null,
          padding: const EdgeInsets.all(14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  if (selectionMode) ...[
                    SizedBox(
                      width: 24,
                      height: 24,
                      child: Checkbox(
                        value: selected,
                        onChanged: (_) => onSelectedChanged(),
                        activeColor: AppColors.primary,
                        visualDensity: VisualDensity.compact,
                        materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                    ),
                    const SizedBox(width: 8),
                  ],
                  Container(
                    width: 8,
                    height: 8,
                    decoration: BoxDecoration(
                      color: dotColor,
                      shape: BoxShape.circle,
                      boxShadow: task.isRunning || hasFailure
                          ? [
                              BoxShadow(
                                color: dotColor.withAlpha(140),
                                blurRadius: 8,
                              ),
                            ]
                          : null,
                    ),
                  ),
                  if (!selectionMode)
                    PopupMenuButton<_TaskItemAction>(
                      tooltip: '任务操作',
                      padding: EdgeInsets.zero,
                      onSelected: onAction,
                      itemBuilder: (_) => [
                        PopupMenuItem(
                          value: _TaskItemAction.toggleEnabled,
                          child: Text(task.isDisabled ? '启用' : '禁用'),
                        ),
                        PopupMenuItem(
                          value: _TaskItemAction.togglePinned,
                          child: Text(task.isPinned ? '取消置顶' : '置顶'),
                        ),
                        const PopupMenuItem(
                          value: _TaskItemAction.copy,
                          child: Text('复制任务'),
                        ),
                        const PopupMenuItem(
                          value: _TaskItemAction.stats,
                          child: Text('任务统计'),
                        ),
                        const PopupMenuItem(
                          value: _TaskItemAction.logFiles,
                          child: Text('日志文件'),
                        ),
                        const PopupMenuItem(
                          value: _TaskItemAction.edit,
                          child: Text('编辑任务'),
                        ),
                        const PopupMenuItem(
                          value: _TaskItemAction.delete,
                          child: Text(
                            '删除任务',
                            style: TextStyle(color: AppColors.red500),
                          ),
                        ),
                      ],
                      child: const _TaskGlassIconTarget(
                        icon: Icons.more_vert,
                        compact: true,
                      ),
                    ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(
                      task.name,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w700,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  if (task.isPinned)
                    const Padding(
                      padding: EdgeInsets.only(right: 6),
                      child: Icon(
                        Icons.push_pin,
                        size: 14,
                        color: AppColors.amber500,
                      ),
                    ),
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: 10,
                      vertical: 4,
                    ),
                    decoration: BoxDecoration(
                      color: _statusBg(),
                      borderRadius: BorderRadius.circular(999),
                      border: Border.all(
                        color: _statusFg().withAlpha(isLight ? 36 : 48),
                      ),
                    ),
                    child: Text(
                      _statusLabel(),
                      style: TextStyle(
                        fontSize: 11,
                        fontWeight: FontWeight.w700,
                        color: _statusFg(),
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              _TaskScheduleSummary(
                taskType: task.taskType,
                taskTypeLabel: _taskTypeLabel(),
                expressions: _scheduleExpressions(),
                isLight: isLight,
              ),
              if (labels.isNotEmpty) ...[
                const SizedBox(height: 8),
                _TaskSubscriptionSummary(labels: labels, isLight: isLight),
              ],
              const SizedBox(height: 10),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      _bottomText(),
                      style: TextStyle(
                        fontSize: 12,
                        color: hasFailure
                            ? AppColors.red500
                            : (isLight
                                  ? AppColors.slate400
                                  : AppColors.slate500),
                      ),
                    ),
                  ),
                  if (!selectionMode)
                    _TaskPrimaryActionButton(
                      label: task.isRunning ? '停止' : '运行',
                      icon: task.isRunning
                          ? Icons.stop_rounded
                          : Icons.play_arrow_rounded,
                      color: primaryColor,
                      onTap: task.isRunning ? onStop : onRun,
                    ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _TaskPrimaryActionButton extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color color;
  final VoidCallback onTap;

  const _TaskPrimaryActionButton({
    required this.label,
    required this.icon,
    required this.color,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return AppLiquidGlassSurface(
      onTap: onTap,
      borderRadius: 18,
      accentColor: color,
      selected: true,
      performanceMode: true,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: color),
          const SizedBox(width: 4),
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w700,
              color: color,
            ),
          ),
        ],
      ),
    );
  }
}

class _TaskScheduleSummary extends StatelessWidget {
  final String taskType;
  final String taskTypeLabel;
  final List<String> expressions;
  final bool isLight;

  const _TaskScheduleSummary({
    required this.taskType,
    required this.taskTypeLabel,
    required this.expressions,
    required this.isLight,
  });

  @override
  Widget build(BuildContext context) {
    final isCron = taskType == 'cron';
    final cleanExpressions = expressions
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList();
    final title = isCron
        ? (cleanExpressions.length > 1
              ? 'Cron 定时 · ${cleanExpressions.length} 条'
              : 'Cron 定时')
        : taskTypeLabel;
    final value = isCron
        ? (cleanExpressions.isEmpty ? '暂无定时规则' : cleanExpressions.first)
        : (taskType == 'manual' ? '手动触发运行' : '面板启动时自动执行');
    final icon = isCron
        ? Icons.schedule_rounded
        : taskType == 'manual'
        ? Icons.touch_app_outlined
        : Icons.power_settings_new_rounded;
    final color = isCron
        ? AppColors.primary
        : taskType == 'manual'
        ? AppColors.blue500
        : AppColors.amber500;

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2, vertical: 4),
      child: Row(
        children: [
          Container(
            width: 28,
            height: 28,
            decoration: BoxDecoration(
              color: color.withAlpha(isLight ? 22 : 36),
              borderRadius: BorderRadius.circular(9),
            ),
            child: Icon(icon, size: 16, color: color),
          ),
          const SizedBox(width: 9),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                    color: isLight ? AppColors.slate600 : AppColors.slate300,
                  ),
                ),
                const SizedBox(height: 4),
                Text(
                  value,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 13,
                    height: 1.45,
                    fontWeight: FontWeight.w600,
                    fontFamily: isCron ? 'monospace' : null,
                    color: isLight ? AppColors.slate800 : AppColors.slate100,
                  ),
                ),
              ],
            ),
          ),
          if (cleanExpressions.length > 1) ...[
            const SizedBox(width: 8),
            _TaskMiniCountChip(
              label: '+${cleanExpressions.length - 1}',
              isLight: isLight,
            ),
          ],
        ],
      ),
    );
  }
}

class _TaskSubscriptionSummary extends ConsumerWidget {
  final List<String> labels;
  final bool isLight;

  const _TaskSubscriptionSummary({required this.labels, required this.isLight});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final visibleLabels = labels.take(3).toList();

    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 2, vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            height: 24,
            padding: const EdgeInsets.symmetric(horizontal: 8),
            decoration: BoxDecoration(
              color: AppColors.blue500.withAlpha(isLight ? 18 : 30),
              borderRadius: BorderRadius.circular(999),
            ),
            child: const Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.sync_rounded, size: 13, color: AppColors.blue500),
                SizedBox(width: 4),
                Text(
                  '订阅',
                  style: TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w800,
                    color: AppColors.blue500,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Wrap(
              spacing: 6,
              runSpacing: 6,
              children: [
                ...visibleLabels.map(
                  (label) =>
                      _TaskSubscriptionChip(label: label, isLight: isLight),
                ),
                if (labels.length > visibleLabels.length)
                  _TaskMiniCountChip(
                    label: '+${labels.length - visibleLabels.length}',
                    isLight: isLight,
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _TaskSubscriptionChip extends StatelessWidget {
  final String label;
  final bool isLight;

  const _TaskSubscriptionChip({required this.label, required this.isLight});

  @override
  Widget build(BuildContext context) {
    // 订阅标签只做轻量展示，不再做成大胶囊，避免任务卡片显得拥挤。
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
      decoration: BoxDecoration(
        color: AppColors.slate500.withAlpha(isLight ? 12 : 24),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: isLight ? AppColors.slate200 : AppColors.darkBorder,
        ),
      ),
      child: Text(
        label,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontSize: 11,
          fontWeight: FontWeight.w700,
          color: isLight ? AppColors.slate600 : AppColors.slate300,
        ),
      ),
    );
  }
}

class _TaskMiniCountChip extends StatelessWidget {
  final String label;
  final bool isLight;

  const _TaskMiniCountChip({required this.label, required this.isLight});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
      decoration: BoxDecoration(
        color: AppColors.slate500.withAlpha(isLight ? 12 : 24),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 10,
          fontWeight: FontWeight.w800,
          color: isLight ? AppColors.slate500 : AppColors.slate400,
        ),
      ),
    );
  }
}

class _TaskGroup {
  final String key;
  final String title;
  final List<Task> tasks = <Task>[];

  _TaskGroup({required this.key, required this.title});
}

class _TaskListRow {
  final _TaskGroup? group;
  final Task? task;

  const _TaskListRow._({this.group, this.task});

  factory _TaskListRow.group(_TaskGroup group) => _TaskListRow._(group: group);

  factory _TaskListRow.task(Task task) => _TaskListRow._(task: task);
}

String? _extractScriptPathFromCommand(String command) {
  final trimmed = command.trim();
  if (trimmed.isEmpty) {
    return null;
  }

  final tokens = _splitCommandTokens(trimmed);
  if (tokens.isEmpty) {
    return null;
  }

  bool hasSupportedExtension(String value) {
    final lower = value.toLowerCase();
    return lower.endsWith('.py') ||
        lower.endsWith('.js') ||
        lower.endsWith('.ts') ||
        lower.endsWith('.sh') ||
        lower.endsWith('.go');
  }

  String? joinCandidate(List<String> items) {
    for (var count = items.length; count >= 1; count--) {
      final candidate = items.take(count).join(' ').trim();
      if (hasSupportedExtension(candidate)) {
        return candidate;
      }
    }
    return null;
  }

  switch (tokens.first) {
    case 'task':
    case 'desi':
      final rest = tokens.sublist(1);
      var idx = 0;
      while (idx < rest.length) {
        if (rest[idx] == '-m' && idx + 1 < rest.length) {
          idx += 2;
          continue;
        }
        if (rest[idx] == '-l') {
          idx += 1;
          continue;
        }
        break;
      }
      return joinCandidate(rest.sublist(idx));
    case 'python':
    case 'python3':
    case 'node':
    case 'ts-node':
    case 'bash':
    case 'go':
      if (tokens.length <= 1) {
        return null;
      }
      return joinCandidate(tokens.sublist(1));
    default:
      return null;
  }
}

List<String> _splitCommandTokens(String command) {
  final tokens = <String>[];
  final buffer = StringBuffer();
  String? quote;

  for (final rune in command.runes) {
    final char = String.fromCharCode(rune);
    if (quote != null) {
      if (char == quote) {
        quote = null;
      } else {
        buffer.write(char);
      }
      continue;
    }

    if (char == '"' || char == "'") {
      quote = char;
      continue;
    }

    if (char.trim().isEmpty) {
      if (buffer.isNotEmpty) {
        tokens.add(buffer.toString());
        buffer.clear();
      }
      continue;
    }

    buffer.write(char);
  }

  if (buffer.isNotEmpty) {
    tokens.add(buffer.toString());
  }

  return tokens;
}

class _MetaChip extends ConsumerWidget {
  final String label;
  final bool active;

  const _MetaChip({required this.label, this.active = true});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;

    final accent = active ? AppColors.primary : AppColors.slate500;
    final background = accent.withAlpha(isLight ? 14 : 24);
    final foreground = active
        ? (isLight ? AppColors.slate700 : AppColors.slate300)
        : AppColors.slate400;

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: background,
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: isLight ? AppColors.slate200 : AppColors.darkBorder,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Text(
            label,
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w600,
              color: foreground,
            ),
          ),
        ],
      ),
    );
  }
}

class _TaskHeaderChipButton extends ConsumerWidget {
  final String label;
  final IconData icon;
  final bool isLight;
  final bool compact;
  final VoidCallback onTap;

  const _TaskHeaderChipButton({
    required this.label,
    required this.icon,
    required this.isLight,
    this.compact = false,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AppLiquidGlassSurface(
      onTap: onTap,
      borderRadius: 16,
      performanceMode: true,
      padding: EdgeInsets.symmetric(horizontal: compact ? 8 : 12, vertical: 7),
      child: Row(
        children: [
          Icon(icon, size: 16, color: AppColors.slate400),
          if (!compact) ...[
            const SizedBox(width: 6),
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ],
      ),
    );
  }
}

class _TaskGlassIconTarget extends StatelessWidget {
  final IconData icon;
  final VoidCallback? onTap;
  final String? tooltip;
  final bool loading;
  final bool compact;

  const _TaskGlassIconTarget({
    required this.icon,
    this.onTap,
    this.tooltip,
    this.loading = false,
    this.compact = false,
  });

  @override
  Widget build(BuildContext context) {
    final target = SizedBox(
      width: 44,
      height: 44,
      child: Center(
        child: SizedBox(
          width: compact ? 32 : 36,
          height: compact ? 32 : 36,
          child: AppLiquidGlassSurface(
            onTap: onTap,
            borderRadius: 18,
            performanceMode: true,
            child: Center(
              child: loading
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(icon, size: 18, color: AppColors.slate400),
            ),
          ),
        ),
      ),
    );
    return tooltip == null ? target : Tooltip(message: tooltip!, child: target);
  }
}

class _TaskBatchActionButton extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color color;
  final bool isLight;
  final bool enabled;
  final VoidCallback onTap;

  const _TaskBatchActionButton({
    required this.label,
    required this.icon,
    required this.color,
    required this.isLight,
    required this.enabled,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    final foregroundColor = enabled ? color : AppColors.slate400;

    return AppLiquidGlassSurface(
      onTap: enabled ? onTap : null,
      borderRadius: 12,
      accentColor: color,
      selected: enabled,
      performanceMode: true,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: foregroundColor),
          const SizedBox(width: 6),
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              color: foregroundColor,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}

class TaskLiveLogPage extends ConsumerStatefulWidget {
  final int taskId;
  final String? taskName;

  const TaskLiveLogPage({super.key, required this.taskId, this.taskName});

  @override
  ConsumerState<TaskLiveLogPage> createState() => _TaskLiveLogPageState();
}

class TaskDetailSheet extends StatelessWidget {
  final Task task;

  const TaskDetailSheet({super.key, required this.task});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    final labels = task.labelsForDisplay;
    final scheduleExpressions = task.cronExpressions.isNotEmpty
        ? task.cronExpressions
        : (task.cronExpression.trim().isNotEmpty
              ? [task.cronExpression.trim()]
              : const <String>[]);

    Widget infoTile(String label, Widget child, {bool expand = false}) {
      return Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(vertical: 12),
        decoration: BoxDecoration(
          border: Border(
            bottom: BorderSide(
              color: isLight ? AppColors.slate100 : AppColors.slate800,
            ),
          ),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: TextStyle(
                fontSize: 12,
                fontWeight: FontWeight.w600,
                color: theme.colorScheme.onSurfaceVariant,
              ),
            ),
            const SizedBox(height: 8),
            if (expand) child else DefaultTextStyle.merge(child: child),
          ],
        ),
      );
    }

    return SafeArea(
      child: SizedBox(
        height: MediaQuery.of(context).size.height * 0.78,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '任务详情',
                style: theme.textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                task.name,
                style: theme.textTheme.titleMedium?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 14),
              Expanded(
                child: SingleChildScrollView(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      infoTile(
                        '状态',
                        _MetaChip(
                          label: task.statusText,
                          active: !task.isDisabled,
                        ),
                      ),
                      infoTile(
                        '任务类型',
                        Text(
                          task.taskType == 'manual'
                              ? '手动运行'
                              : task.taskType == 'startup'
                              ? '开机运行'
                              : '常规定时',
                          style: const TextStyle(fontSize: 13),
                        ),
                      ),
                      infoTile(
                        '定时规则',
                        task.taskType == 'cron'
                            ? TaskCronList(expressions: scheduleExpressions)
                            : Text(
                                '不使用 Cron',
                                style: TextStyle(
                                  fontSize: 13,
                                  color: theme.colorScheme.onSurfaceVariant,
                                ),
                              ),
                        expand: true,
                      ),
                      infoTile(
                        '执行命令',
                        SelectableText(
                          task.command,
                          style: const TextStyle(
                            fontSize: 12,
                            height: 1.5,
                            fontFamily: 'monospace',
                          ),
                        ),
                        expand: true,
                      ),
                      infoTile(
                        '标签',
                        labels.isEmpty
                            ? Text(
                                '无',
                                style: TextStyle(
                                  fontSize: 13,
                                  color: theme.colorScheme.onSurfaceVariant,
                                ),
                              )
                            : Wrap(
                                spacing: 6,
                                runSpacing: 6,
                                children: labels
                                    .map((label) => _MetaChip(label: label))
                                    .toList(),
                              ),
                        expand: true,
                      ),
                      infoTile(
                        '上次运行',
                        Text(
                          task.lastRunAt == null
                              ? '-'
                              : formatTimeCn(task.lastRunAt),
                          style: const TextStyle(fontSize: 13),
                        ),
                      ),
                      infoTile(
                        '下次运行',
                        Text(
                          task.nextRunAt == null
                              ? '-'
                              : formatTimeCn(task.nextRunAt),
                          style: const TextStyle(fontSize: 13),
                        ),
                      ),
                      infoTile(
                        '上次结果',
                        Text(
                          task.lastRunStatus == null
                              ? '未运行'
                              : task.lastRunStatus == 0
                              ? '成功'
                              : '失败',
                          style: TextStyle(
                            fontSize: 13,
                            color: task.lastRunStatus == 1
                                ? AppColors.red500
                                : null,
                          ),
                        ),
                      ),
                      infoTile(
                        '最近耗时',
                        Text(
                          task.lastRunningTime == null
                              ? '-'
                              : '${task.lastRunningTime!.toStringAsFixed(2)}s',
                          style: const TextStyle(fontSize: 13),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _TaskLiveLogPageState extends ConsumerState<TaskLiveLogPage>
    with WidgetsBindingObserver {
  final ScrollController _scrollController = ScrollController();
  final _sseClient = SseClient();
  final _lines = <String>[];
  final _historyReplayBuffer = <String>[];
  late final LogUpdateBatcher<String> _logBatcher;
  bool _loading = true;
  bool _done = false;
  bool _autoScroll = true;
  String _statusText = '连接中...';
  Timer? _pollTimer;
  bool _pollRequestRunning = false;
  bool _usingSse = false;
  bool _appResumed = true;
  int _lifecycleGeneration = 0;
  int _cursor = 0;
  int? _logId;
  Color? _logBackgroundColor;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _logBatcher = LogUpdateBatcher<String>(onFlush: _flushLogLines);
    _loadAppearance();
    Future.microtask(_init);
  }

  @override
  void dispose() {
    _lifecycleGeneration++;
    WidgetsBinding.instance.removeObserver(this);
    _pollTimer?.cancel();
    _sseClient.close();
    _logBatcher.dispose();
    _scrollController.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _appResumed = state == AppLifecycleState.resumed;
    if (state == AppLifecycleState.resumed && !_done && !_usingSse) {
      _lifecycleGeneration++;
      _startPolling();
    } else if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.detached) {
      _lifecycleGeneration++;
      _pollTimer?.cancel();
      _pollTimer = null;
    }
  }

  Future<void> _init() async {
    final generation = _lifecycleGeneration;
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.taskLiveLogs(widget.taskId),
        queryParameters: {'cursor': _cursor},
      );
      final data = extractData(resp.data);
      if (!mounted || !_appResumed || generation != _lifecycleGeneration) {
        return;
      }
      if (data is Map<String, dynamic>) {
        _applyLiveSnapshot(data, initial: true);
        return;
      }
    } catch (_) {}

    if (!mounted || !_appResumed || generation != _lifecycleGeneration) return;
    setState(() {
      _loading = false;
      _done = false;
      _statusText = '等待日志...';
    });
    _startPolling();
  }

  Future<void> _loadAppearance() async {
    final color = await loadPanelLogBackgroundColor();
    if (!mounted) {
      return;
    }
    setState(() => _logBackgroundColor = color);
  }

  void _applyLiveSnapshot(Map<String, dynamic> data, {bool initial = false}) {
    final snapshot = LiveLogSnapshot.fromMap(data);
    final shouldKeepPolling = snapshot.shouldTrack;

    if (!mounted) {
      return;
    }

    setState(() {
      _loading = false;
      if (initial || (_logId != null && snapshot.logId != _logId)) {
        _lines.clear();
      }
      appendBoundedLogEntries(_lines, snapshot.logs);
      _cursor = snapshot.cursor;
      _logId = snapshot.logId ?? _logId;
      _done = snapshot.done && !shouldKeepPolling;
      _statusText = shouldKeepPolling
          ? '等待日志...'
          : _statusFromLiveTask(snapshot.status, done: snapshot.done);
    });

    if (_autoScroll && snapshot.logs.isNotEmpty) {
      _scrollToBottom();
    }

    if (snapshot.isRunning) {
      _pollTimer?.cancel();
      _connectSSE(widget.taskId);
      return;
    }

    if (shouldKeepPolling) {
      _startPolling();
      return;
    }

    _pollTimer?.cancel();
    _pollTimer = null;
    if (!initial && snapshot.done) {
      _sendTaskCompletionNotification(widget.taskId, 'finished');
    }
  }

  void _startPolling() {
    _usingSse = false;
    if (_pollTimer != null) {
      return;
    }
    final pollingGeneration = ++_lifecycleGeneration;
    _pollTimer = Timer.periodic(const Duration(seconds: 3), (_) async {
      if (_pollRequestRunning || !_appResumed ||
          pollingGeneration != _lifecycleGeneration) {
        return;
      }
      _pollRequestRunning = true;
      if (!mounted) {
        _pollTimer?.cancel();
        _pollTimer = null;
        _pollRequestRunning = false;
        return;
      }
      try {
        final resp = await DioClient.instance.dio.get(
          ApiEndpoints.taskLiveLogs(widget.taskId),
          queryParameters: {'cursor': _cursor},
        );
        final data = extractData(resp.data);
        if (!mounted || !_appResumed || _usingSse ||
            pollingGeneration != _lifecycleGeneration) {
          return;
        }
        if (data is Map<String, dynamic>) {
          _applyLiveSnapshot(data);
        }
      } catch (_) {
      } finally {
        _pollRequestRunning = false;
      }

    });
  }

  void _connectSSE(int taskId) {
    _lifecycleGeneration++;
    _usingSse = true;
    _sseClient.close();
    _pollTimer?.cancel();
    _pollTimer = null;
    _historyReplayBuffer
      ..clear()
      ..addAll(_lines);
    _sseClient.connect(
      path: '${ApiEndpoints.logStream(taskId)}?cursor=$_cursor',
      autoReconnect: true,
      onEvent: (event) {
        if (!mounted) return;
        if (event.event == 'done') {
          _logBatcher.flush();
          if (event.data == 'reconnect') {
            setState(() {
              _done = false;
              _statusText = '运行中';
            });
            _historyReplayBuffer
              ..clear()
              ..addAll(_lines);
            return;
          }
          setState(() {
            _done = event.data == 'finished';
            _statusText = _statusFromStreamDone(event.data);
          });
          _sseClient.close();
          _sendTaskCompletionNotification(widget.taskId, event.data);
          return;
        }
        final newLines = event.data.replaceAll('\r\n', '\n').split('\n');
        newLines.removeWhere((l) => l.isEmpty);
        if (newLines.isEmpty) return;
        final dedupedLines = _consumeReplayLines(newLines);
        if (dedupedLines.isEmpty) return;
        final eventCursor = int.tryParse(event.lastEventId ?? event.id ?? '');
        if (eventCursor != null && eventCursor > _cursor) _cursor = eventCursor;
        _logBatcher.addAll(dedupedLines);
      },
      onDone: () {
        _logBatcher.flush();
        if (!mounted) return;
        if (_done) return;
        setState(() => _statusText = '连接结束');
      },
      onError: (_) {
        _logBatcher.flush();
        if (!mounted) return;
        if (!_done) {
          _usingSse = false;
          setState(() => _statusText = '连接错误');
          _pollTimer?.cancel();
          _pollTimer = null;
          _startPolling();
        }
      },
    );
  }

  void _flushLogLines(List<String> lines) {
    if (!mounted || lines.isEmpty) return;
    setState(() {
      appendBoundedLogEntries(_lines, lines);
      _done = false;
      _statusText = '运行中';
    });
    if (_autoScroll) _scrollToBottom();
  }

  List<String> _consumeReplayLines(List<String> incomingLines) {
    if (_historyReplayBuffer.isEmpty) {
      return incomingLines;
    }

    final result = <String>[];
    for (final line in incomingLines) {
      if (_historyReplayBuffer.isNotEmpty &&
          line == _historyReplayBuffer.first) {
        _historyReplayBuffer.removeAt(0);
        continue;
      }

      _historyReplayBuffer.clear();
      result.add(line);
    }

    return result;
  }

  String _statusFromLiveTask(double? status, {required bool done}) {
    if (!done) {
      return status == 2 ? '运行中' : status == 0.5 ? '排队中' : '等待日志...';
    }
    switch (status) {
      case 0:
        return '执行成功';
      case 1:
        return '执行失败';
      case 2:
        return '运行中';
      case 3:
        return '已终止';
      default:
        return _lines.isEmpty ? '等待日志...' : '已完成';
    }
  }

  String _statusFromStreamDone(String value) {
    switch (value) {
      case 'finished':
        return '已完成';
      case 'timeout':
        return '等待日志...';
      case 'reconnect':
        return '运行中';
      default:
        return value;
    }
  }

  void _sendTaskCompletionNotification(int taskId, String data) async {
    if (data == 'reconnect') return;
    final enabled = await LocalNotificationService().getChannelEnabled(
      NotificationChannel.task,
    );
    if (!enabled) return;
    final title = widget.taskName?.trim().isNotEmpty == true
        ? widget.taskName!
        : '任务 #$taskId';
    if (data == 'finished') {
      LocalNotificationService().showTaskNotification(
        id: taskId,
        title: '$title 执行完成',
        body: '任务已成功执行完毕',
        payload: taskNotificationPayload(taskId),
      );
    } else {
      LocalNotificationService().showTaskNotification(
        id: taskId,
        title: '$title 执行结束',
        body: '任务状态: $data',
        payload: taskNotificationPayload(taskId),
      );
    }
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 100),
          curve: Curves.easeOut,
        );
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final title = (widget.taskName?.trim().isNotEmpty ?? false)
        ? '${widget.taskName} 运行日志'
        : '运行日志';
    final logTheme = resolveLogSurfaceTheme(_logBackgroundColor);
    final chipBackground = logTheme.foreground.withAlpha(
      logTheme.brightness == Brightness.dark ? 24 : 14,
    );

    return Scaffold(
      backgroundColor: logTheme.background,
      appBar: AppBar(
        title: Text(title),
        backgroundColor: logTheme.background,
        foregroundColor: logTheme.foreground,
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 4),
            child: Chip(
              backgroundColor: chipBackground,
              side: BorderSide(color: logTheme.foreground.withAlpha(32)),
              surfaceTintColor: Colors.transparent,
              label: Text(
                _statusText,
                style: TextStyle(fontSize: 11, color: logTheme.foreground),
              ),
              avatar: _done
                  ? Icon(Icons.check, size: 14, color: logTheme.foreground)
                  : SizedBox(
                      width: 12,
                      height: 12,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: logTheme.foreground,
                      ),
                    ),
              visualDensity: VisualDensity.compact,
            ),
          ),
          if (_lines.isNotEmpty)
            IconButton(
              icon: Icon(Icons.copy, color: logTheme.foreground),
              tooltip: '复制全部',
              onPressed: () {
                Clipboard.setData(ClipboardData(text: _lines.join('\n')));
                AppGlassNotice.show(
                  context,
                  '日志已复制到剪贴板',
                  type: AppGlassNoticeType.success,
                );
              },
            ),
          IconButton(
            icon: Icon(
              _autoScroll ? Icons.vertical_align_bottom : Icons.pause,
              color: _autoScroll ? AppColors.primary : logTheme.mutedForeground,
            ),
            tooltip: _autoScroll ? '自动滚动: 开' : '自动滚动: 关',
            onPressed: () {
              setState(() => _autoScroll = !_autoScroll);
              if (_autoScroll) _scrollToBottom();
            },
          ),
        ],
      ),
      body: Container(
        color: logTheme.background,
        child: _loading && _lines.isEmpty
            ? const Center(
                child: CircularProgressIndicator(color: AppColors.primary),
              )
            : _lines.isEmpty
            ? Center(
                child: Text(
                  _done ? '无日志内容' : '等待日志输出...',
                  style: TextStyle(color: logTheme.mutedForeground),
                ),
              )
            : Theme(
                data: Theme.of(context).copyWith(
                  textSelectionTheme: TextSelectionThemeData(
                    selectionColor: AppColors.primary.withAlpha(80),
                    selectionHandleColor: AppColors.primary,
                  ),
                ),
                child: Scrollbar(
                  controller: _scrollController,
                  child: SingleChildScrollView(
                    controller: _scrollController,
                    padding: const EdgeInsets.all(12),
                    child: SelectableText.rich(
                      AnsiTextParser.buildTextSpan(
                        _lines.join('\n'),
                        baseStyle: TextStyle(
                          color: logTheme.foreground,
                          fontFamily: 'monospace',
                          fontSize: 12,
                          height: 1.6,
                        ),
                        brightness: logTheme.brightness,
                      ),
                      contextMenuBuilder: (context, editableTextState) {
                        return AdaptiveTextSelectionToolbar.editableText(
                          editableTextState: editableTextState,
                        );
                      },
                    ),
                  ),
                ),
              ),
      ),
    );
  }
}

String _extractTaskError(dynamic error, String fallback) =>
    extractErrorMessage(error, fallback);

class _GroupPopupMenu extends StatelessWidget {
  final bool isUngrouped;
  final VoidCallback? onRename;
  final VoidCallback? onDelete;
  final VoidCallback onAddTasks;

  const _GroupPopupMenu({
    required this.isUngrouped,
    this.onRename,
    this.onDelete,
    required this.onAddTasks,
  });

  @override
  Widget build(BuildContext context) {
    return PopupMenuButton<String>(
      padding: EdgeInsets.zero,
      constraints: const BoxConstraints(),
      child: const _TaskGlassIconTarget(icon: Icons.more_vert, compact: true),
      itemBuilder: (ctx) => [
        if (!isUngrouped && onRename != null)
          const PopupMenuItem(value: 'rename', child: Text('重命名分组')),
        if (!isUngrouped && onDelete != null)
          const PopupMenuItem(value: 'delete', child: Text('删除分组')),
        PopupMenuItem(value: 'add', child: Text(isUngrouped ? '新建分组' : '添加任务')),
      ],
      onSelected: (value) {
        switch (value) {
          case 'rename':
            onRename?.call();
          case 'delete':
            onDelete?.call();
          case 'add':
            onAddTasks();
        }
      },
    );
  }
}
