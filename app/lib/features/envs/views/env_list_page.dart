import 'dart:async';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/models/env_var.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

final envListProvider = StateNotifierProvider<EnvListNotifier, EnvListState>((
  ref,
) {
  return EnvListNotifier();
});

const _selectedGroupUnset = Object();

enum _EnvBatchAction { enable, disable, delete }

enum _EnvTransferAction { exportAll, importEnvs }

class EnvListState {
  final List<EnvVar> envs;
  final int total;
  final bool loading;
  final List<String> groups;
  final List<String> selectedGroups;
  final String keyword;
  final String? error;

  const EnvListState({
    this.envs = const [],
    this.total = 0,
    this.loading = false,
    this.groups = const [],
    this.selectedGroups = const [],
    this.keyword = '',
    this.error,
  });

  EnvListState copyWith({
    List<EnvVar>? envs,
    int? total,
    bool? loading,
    List<String>? groups,
    Object? selectedGroups = _selectedGroupUnset,
    String? keyword,
    String? error,
  }) {
    return EnvListState(
      envs: envs ?? this.envs,
      total: total ?? this.total,
      loading: loading ?? this.loading,
      groups: groups ?? this.groups,
      selectedGroups: identical(selectedGroups, _selectedGroupUnset)
          ? this.selectedGroups
          : selectedGroups as List<String>,
      keyword: keyword ?? this.keyword,
      error: error,
    );
  }
}

class EnvListNotifier extends StateNotifier<EnvListState> {
  EnvListNotifier() : super(const EnvListState());
  int _loadRequestId = 0;

  Future<void> load() async {
    final requestId = ++_loadRequestId;
    state = state.copyWith(loading: true, error: null);
    try {
      final dio = DioClient.instance.dio;
      // The panel backend caps page_size at 100. Requesting a larger value
      // silently falls back to 20, which previously made the app stop after 40 rows.
      const pageSize = 100;
      final params = <String, dynamic>{'page': 1, 'page_size': pageSize};
      if (state.selectedGroups.isNotEmpty) {
        params['groups'] = state.selectedGroups.join(',');
      }
      if (state.keyword.isNotEmpty) {
        params['keyword'] = state.keyword;
      }

      final firstPageFuture = dio.get(
        ApiEndpoints.envs,
        queryParameters: params,
      );
      final groupsFuture = dio.get(ApiEndpoints.envsGroups);
      final results = await Future.wait([firstPageFuture, groupsFuture]);

      final paginated = extractPaginated(results[0].data);
      final allItems = <Map<String, dynamic>>[...paginated.items];
      var page = 2;
      while (allItems.length < paginated.total) {
        final nextResponse = await dio.get(
          ApiEndpoints.envs,
          queryParameters: {...params, 'page': page},
        );
        final nextPage = extractPaginated(nextResponse.data);
        if (nextPage.items.isEmpty) {
          break;
        }
        allItems.addAll(nextPage.items);
        page++;
      }

      final items = allItems.map((e) => EnvVar.fromJson(e)).toList();
      final groupsRaw = results[1].data;
      List groupsList;
      if (groupsRaw is List) {
        groupsList = groupsRaw;
      } else if (groupsRaw is Map && groupsRaw['data'] is List) {
        groupsList = groupsRaw['data'] as List;
      } else {
        groupsList = [];
      }
      final groups = groupsList.map((e) => e.toString()).toList();
      if (requestId != _loadRequestId) return;
      state = state.copyWith(
        envs: items,
        total: paginated.total > items.length ? paginated.total : items.length,
        loading: false,
        groups: groups,
      );
    } catch (error) {
      if (requestId != _loadRequestId) return;
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '加载环境变量失败'),
      );
    }
  }

  void setGroups(List<String> groups) {
    state = state.copyWith(selectedGroups: groups);
    load();
  }

  void setKeyword(String keyword) {
    state = state.copyWith(keyword: keyword);
    load();
  }

  Future<void> toggle(int id, bool enabled) async {
    final dio = DioClient.instance.dio;
    if (enabled) {
      await dio.put(ApiEndpoints.envEnable(id));
    } else {
      await dio.put(ApiEndpoints.envDisable(id));
    }
    await load();
  }

  Future<void> delete(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.envById(id));
    await load();
  }

  Future<void> batchDelete(List<int> ids) async {
    await DioClient.instance.dio.delete(
      ApiEndpoints.envsBatchDelete,
      data: {'ids': ids},
    );
    await load();
  }

  Future<void> batchEnable(List<int> ids) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envsBatchEnable,
      data: {'ids': ids},
    );
    await load();
  }

  Future<void> batchDisable(List<int> ids) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envsBatchDisable,
      data: {'ids': ids},
    );
    await load();
  }

  Future<void> batchSetGroup(List<int> ids, List<String> groups) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envsBatchGroup,
      data: {'ids': ids, 'groups': groups},
    );
    await load();
  }

  Future<void> create(
    String name,
    String value, {
    String remarks = '',
    List<String> groups = const [],
  }) async {
    await DioClient.instance.dio.post(
      ApiEndpoints.envs,
      data: {
        'name': name,
        'value': value,
        'remarks': remarks,
        'group': groups.join(','),
        'groups': groups,
      },
    );
    await load();
  }

  Future<void> update(
    int id,
    String name,
    String value, {
    String remarks = '',
    List<String> groups = const [],
  }) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envById(id),
      data: {
        'name': name,
        'value': value,
        'remarks': remarks,
        'group': groups.join(','),
        'groups': groups,
      },
    );
    await load();
  }

  Future<void> sortEnvs(int sourceId, int? targetId) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envsSort,
      data: {'source_id': sourceId, 'target_id': targetId},
    );
    await load();
  }

  void reorderLocal(int oldIndex, int newIndex) {
    final items = List<EnvVar>.from(state.envs);
    final item = items.removeAt(oldIndex);
    items.insert(newIndex, item);
    state = state.copyWith(envs: items);
  }
}

class EnvListPage extends ConsumerStatefulWidget {
  const EnvListPage({super.key});

  @override
  ConsumerState<EnvListPage> createState() => _EnvListPageState();
}

class _EnvListPageState extends ConsumerState<EnvListPage> {
  final _searchController = TextEditingController();
  final Set<int> _selectedIds = <int>{};
  Timer? _debounce;

  bool _selectionMode = false;
  bool _sortMode = false;
  bool _transferBusy = false;
  final List<({int sourceId, int? targetId})> _pendingSortMoves = [];

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
                labelText: '分组',
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

  List<String> _normalizeGroups(Iterable<String> values) {
    final seen = <String>{};
    final result = <String>[];
    for (final raw in values) {
      for (final group in raw.split(',')) {
        final trimmed = group.trim();
        if (trimmed.isEmpty || seen.contains(trimmed)) {
          continue;
        }
        seen.add(trimmed);
        result.add(trimmed);
      }
    }
    return result;
  }

  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.read(envListProvider.notifier).load());
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _searchController.dispose();
    super.dispose();
  }

  void _showMessage(String message) {
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(context, message);
  }

  Future<void> _handleEnvTransferAction(_EnvTransferAction? action) async {
    if (action == null || _transferBusy) {
      return;
    }
    switch (action) {
      case _EnvTransferAction.exportAll:
        await _exportEnvs();
        break;
      case _EnvTransferAction.importEnvs:
        await _importEnvs();
        break;
    }
  }

  Future<void> _exportEnvs() async {
    setState(() => _transferBusy = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.envsExportAll,
        options: Options(responseType: ResponseType.bytes),
      );
      final bytes = _extractBytes(resp.data);
      if (bytes == null || bytes.isEmpty) {
        throw StateError('导出内容为空');
      }

      final savedPath = await FilePicker.platform.saveFile(
        dialogTitle: '保存环境变量导出文件',
        fileName: 'daidai-envs-export.json',
        type: FileType.custom,
        allowedExtensions: const ['json'],
        bytes: bytes,
      );
      if (savedPath == null) {
        _showMessage('已取消保存');
        return;
      }
      _showMessage('环境变量已导出');
    } on UnsupportedError {
      _showMessage('当前平台暂不支持直接保存文件');
    } catch (error) {
      _showMessage(extractErrorMessage(error, '导出环境变量失败'));
    } finally {
      if (mounted) {
        setState(() => _transferBusy = false);
      }
    }
  }

  Future<void> _importEnvs() async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: false,
      withData: false,
      withReadStream: true,
      type: FileType.custom,
      allowedExtensions: const ['json'],
      dialogTitle: '选择环境变量导入文件',
    );
    if (result == null || result.files.isEmpty) {
      return;
    }

    final file = result.files.first;
    final multipart = await _toMultipartFile(file);
    if (multipart == null) {
      _showMessage('无法读取所选环境变量文件');
      return;
    }

    setState(() => _transferBusy = true);
    try {
      final formData = FormData();
      formData.files.add(MapEntry('file', multipart));
      await DioClient.instance.dio.post(
        ApiEndpoints.envsImport,
        data: formData,
        options: Options(contentType: 'multipart/form-data'),
      );
      await ref.read(envListProvider.notifier).load();
      _showMessage('环境变量导入成功');
    } catch (error) {
      _showMessage(extractErrorMessage(error, '导入环境变量失败'));
    } finally {
      if (mounted) {
        setState(() => _transferBusy = false);
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

  bool _isSelected(int id) => _selectedIds.contains(id);

  bool _isAllSelected(List<EnvVar> envs) =>
      envs.isNotEmpty && envs.every((env) => _selectedIds.contains(env.id));

  void _setSelectionMode(bool enabled) {
    setState(() {
      _selectionMode = enabled;
      if (!enabled) {
        _selectedIds.clear();
      }
    });
  }

  void _toggleSelection(int id) {
    setState(() {
      _selectionMode = true;
      if (_selectedIds.contains(id)) {
        _selectedIds.remove(id);
      } else {
        _selectedIds.add(id);
      }
      if (_selectedIds.isEmpty) {
        _selectionMode = false;
      }
    });
  }

  void _toggleSelectAll(List<EnvVar> envs) {
    final visibleIds = envs.map((env) => env.id).toSet();
    setState(() {
      if (visibleIds.isNotEmpty &&
          visibleIds.every((id) => _selectedIds.contains(id))) {
        _selectedIds.removeAll(visibleIds);
        if (_selectedIds.isEmpty) {
          _selectionMode = false;
        }
      } else {
        _selectionMode = true;
        _selectedIds.addAll(visibleIds);
      }
    });
  }

  Future<bool> _confirmBatchDelete(int count) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('批量删除'),
        content: Text('确定删除选中的 $count 个环境变量吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.of(dialogCtx).pop(false),
              ),
              AppGlassDialogAction(
                label: '删除',
                onPressed: () => Navigator.of(dialogCtx).pop(true),
                variant: AppLiquidGlassButtonVariant.danger,
              ),
            ],
          ),
        ],
      ),
    );

    return confirmed == true;
  }

  Future<void> _performBatchAction(_EnvBatchAction action) async {
    final ids = _selectedIds.toList()..sort();
    if (ids.isEmpty) {
      return;
    }

    if (action == _EnvBatchAction.delete) {
      final confirmed = await _confirmBatchDelete(ids.length);
      if (!confirmed) {
        return;
      }
    }

    try {
      final notifier = ref.read(envListProvider.notifier);
      switch (action) {
        case _EnvBatchAction.enable:
          await notifier.batchEnable(ids);
          break;
        case _EnvBatchAction.disable:
          await notifier.batchDisable(ids);
          break;
        case _EnvBatchAction.delete:
          await notifier.batchDelete(ids);
          break;
      }

      if (!mounted) {
        return;
      }

      _setSelectionMode(false);
      final message = switch (action) {
        _EnvBatchAction.enable => '已批量启用 ${ids.length} 个环境变量',
        _EnvBatchAction.disable => '已批量禁用 ${ids.length} 个环境变量',
        _EnvBatchAction.delete => '已批量删除 ${ids.length} 个环境变量',
      };
      AppGlassNotice.show(context, message, type: AppGlassNoticeType.success);
    } catch (_) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '批量操作失败，请稍后重试',
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _performBatchGroup(List<String> groups) async {
    final ids = _selectedIds.toList()..sort();
    if (ids.isEmpty) {
      return;
    }

    try {
      await ref.read(envListProvider.notifier).batchSetGroup(ids, groups);
      if (!mounted) {
        return;
      }

      _setSelectionMode(false);
      final message = groups.isEmpty
          ? '已清空 ${ids.length} 个环境变量的分组'
          : '已将 ${ids.length} 个环境变量分组到“${groups.join(' / ')}”';
      AppGlassNotice.show(context, message, type: AppGlassNoticeType.success);
    } catch (_) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '批量分组失败，请稍后重试',
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _showBatchGroupDialog(List<String> groups) async {
    if (_selectedIds.isEmpty) {
      return;
    }

    final controller = TextEditingController();
    final selectedGroups = <String>{};
    final result = await showDialog<List<String>>(
      context: context,
      builder: (dialogCtx) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: const Text('批量分组'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('将已选择的 ${_selectedIds.length} 个环境变量设置到同一分组。'),
                const SizedBox(height: 12),
                TextField(
                  controller: controller,
                  decoration: const InputDecoration(
                    labelText: '分组名称',
                    hintText: '输入多个分组，逗号分隔',
                  ),
                  onChanged: (_) => setDialogState(() {}),
                ),
                if (groups.isNotEmpty) ...[
                  const SizedBox(height: 14),
                  const Text(
                    '已有分组',
                    style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600),
                  ),
                  const SizedBox(height: 8),
                  Wrap(
                    spacing: 8,
                    runSpacing: 8,
                    children: groups
                        .map(
                          (group) => AppLiquidGlassActionChip(
                            label: group,
                            onPressed: () {
                              if (selectedGroups.contains(group)) {
                                selectedGroups.remove(group);
                              } else {
                                selectedGroups.add(group);
                              }
                              final merged = _normalizeGroups([
                                controller.text,
                                ...selectedGroups,
                              ]);
                              controller.text = merged.join(', ');
                              controller.selection = TextSelection.fromPosition(
                                TextPosition(offset: controller.text.length),
                              );
                              setDialogState(() {});
                            },
                          ),
                        )
                        .toList(),
                  ),
                ],
              ],
            ),
          ),
          actions: [
            AppLiquidGlassDialogActions(
              actions: [
                AppGlassDialogAction(
                  label: '取消',
                  onPressed: () => Navigator.of(dialogCtx).pop(),
                ),
                AppGlassDialogAction(
                  label: '清空分组',
                  onPressed: () => Navigator.of(dialogCtx).pop(const []),
                  variant: AppLiquidGlassButtonVariant.warning,
                ),
                AppGlassDialogAction(
                  label: '确认',
                  onPressed: () => Navigator.of(
                    dialogCtx,
                  ).pop(_normalizeGroups([controller.text, ...selectedGroups])),
                  variant: AppLiquidGlassButtonVariant.primary,
                ),
              ],
            ),
          ],
        ),
      ),
    );

    if (!mounted || result == null) {
      return;
    }

    await _performBatchGroup(result);
  }

  Future<void> _refresh() async {
    if (_selectionMode) {
      _setSelectionMode(false);
    }
    await ref.read(envListProvider.notifier).load();
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(envListProvider);
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;

    final selectedCount = _selectedIds.length;
    final allSelected = _isAllSelected(state.envs);

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 12),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: LayoutBuilder(
                builder: (context, constraints) {
                  final actions = Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      if (!_sortMode)
                        _HeaderChipButton(
                          label: _selectionMode ? '取消' : '批量',
                          icon: _selectionMode ? Icons.close : Icons.done_all,
                          isLight: isLight,
                          onTap: () => _setSelectionMode(!_selectionMode),
                        ),
                      if (!_selectionMode) ...[
                        const SizedBox(width: 8),
                        _HeaderChipButton(
                          label: _sortMode ? '完成' : '排序',
                          icon: _sortMode ? Icons.check : Icons.swap_vert,
                          isLight: isLight,
                          onTap: () async {
                            if (_sortMode) {
                              try {
                                for (final move in _pendingSortMoves) {
                                  await ref
                                      .read(envListProvider.notifier)
                                      .sortEnvs(move.sourceId, move.targetId);
                                }
                                if (context.mounted) {
                                  AppGlassNotice.show(
                                    context,
                                    '排序已保存',
                                    type: AppGlassNoticeType.success,
                                  );
                                }
                              } catch (error) {
                                if (context.mounted) {
                                  AppGlassNotice.show(
                                    context,
                                    extractErrorMessage(error, '保存排序失败'),
                                    type: AppGlassNoticeType.error,
                                  );
                                }
                              }
                            }
                            if (!mounted) return;
                            setState(() {
                              _sortMode = !_sortMode;
                              _pendingSortMoves.clear();
                            });
                          },
                        ),
                      ],
                      if (!_selectionMode && !_sortMode) ...[
                        const SizedBox(width: 8),
                        AppGlassIconButton(
                          icon: Icons.add,
                          tooltip: '新建变量',
                          onTap: () => _showCreateDialog(),
                        ),
                      ],
                    ],
                  );
                  final title = const Text(
                    '环境变量',
                    style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
                  );
                  if (constraints.maxWidth < 360) {
                    return Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        title,
                        const SizedBox(height: 10),
                        Align(alignment: Alignment.centerRight, child: actions),
                      ],
                    );
                  }
                  return Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [title, actions],
                  );
                },
              ),
            ),
            const SizedBox(height: 16),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                children: [
                  Expanded(
                    child: SizedBox(
                      height: 44,
                      child: AppLiquidGlassInput(
                        child: TextField(
                          controller: _searchController,
                          decoration: InputDecoration(
                            hintText: '搜索变量...',
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
                                      if (_selectionMode) {
                                        _setSelectionMode(false);
                                      }
                                      ref
                                          .read(envListProvider.notifier)
                                          .setKeyword('');
                                    },
                                  )
                                : null,
                          ),
                          style: const TextStyle(fontSize: 14),
                          onChanged: (v) {
                            setState(() {});
                            if (_selectionMode) {
                              _setSelectionMode(false);
                            }
                            _debounce?.cancel();
                            _debounce = Timer(
                              const Duration(milliseconds: 300),
                              () {
                                ref
                                    .read(envListProvider.notifier)
                                    .setKeyword(v);
                              },
                            );
                          },
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  PopupMenuButton<String>(
                    tooltip: '筛选分组',
                    onSelected: (value) {
                      if (_selectionMode) {
                        _setSelectionMode(false);
                      }
                      if (value == '__all__') {
                        ref.read(envListProvider.notifier).setGroups(const []);
                        return;
                      }
                      final current = [...state.selectedGroups];
                      if (current.contains(value)) {
                        current.remove(value);
                      } else {
                        current.add(value);
                      }
                      ref
                          .read(envListProvider.notifier)
                          .setGroups(_normalizeGroups(current));
                    },
                    itemBuilder: (_) => [
                      CheckedPopupMenuItem<String>(
                        value: '__all__',
                        checked: state.selectedGroups.isEmpty,
                        child: const Text('全部'),
                      ),
                      ...state.groups.map(
                        (g) => CheckedPopupMenuItem<String>(
                          value: g,
                          checked: state.selectedGroups.contains(g),
                          child: Text(g),
                        ),
                      ),
                    ],
                    child: ConstrainedBox(
                      constraints: const BoxConstraints(maxWidth: 140),
                      child: AppLiquidGlassSurface(
                        borderRadius: 12,
                        performanceMode: true,
                        padding: const EdgeInsets.symmetric(horizontal: 12),
                        child: SizedBox(
                          height: 44,
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              const Icon(
                                Icons.label_outline,
                                size: 18,
                                color: AppColors.slate400,
                              ),
                              const SizedBox(width: 6),
                              Flexible(
                                child: Text(
                                  state.selectedGroups.isEmpty
                                      ? '全部'
                                      : state.selectedGroups.join(' / '),
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(
                                    fontSize: 13,
                                    color: theme.colorScheme.onSurface,
                                  ),
                                ),
                              ),
                              const SizedBox(width: 4),
                              const Icon(
                                Icons.expand_more,
                                size: 18,
                                color: AppColors.slate400,
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 8),
                  PopupMenuButton<_EnvTransferAction>(
                    tooltip: '导入导出',
                    enabled: !_transferBusy,
                    onSelected: _handleEnvTransferAction,
                    itemBuilder: (_) => const [
                      PopupMenuItem(
                        value: _EnvTransferAction.exportAll,
                        child: ListTile(
                          leading: Icon(Icons.file_download_outlined),
                          title: Text('导出全部变量'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                      ),
                      PopupMenuItem(
                        value: _EnvTransferAction.importEnvs,
                        child: ListTile(
                          leading: Icon(Icons.file_upload_outlined),
                          title: Text('导入变量'),
                          dense: true,
                          contentPadding: EdgeInsets.zero,
                        ),
                      ),
                    ],
                    child: _transferBusy
                        ? AppLiquidGlassSurface(
                            borderRadius: 12,
                            performanceMode: true,
                            child: const SizedBox(
                              width: 44,
                              height: 44,
                              child: Center(
                                child: SizedBox(
                                  width: 18,
                                  height: 18,
                                  child: CircularProgressIndicator(
                                    strokeWidth: 2,
                                  ),
                                ),
                              ),
                            ),
                          )
                        : const AppGlassIconButton(
                            icon: Icons.more_horiz,
                            tooltip: '导入导出',
                            onTap: null,
                            accentColor: AppColors.slate400,
                          ),
                  ),
                ],
              ),
            ),
            if (_selectionMode) ...[
              const SizedBox(height: 12),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                child: SizedBox(
                  width: double.infinity,
                  child: AppCard(
                    padding: const EdgeInsets.all(12),
                    borderRadius: 14,
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Text(
                              '已选择 $selectedCount 项',
                              style: const TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                            const Spacer(),
                            TextButton(
                              onPressed: () => _toggleSelectAll(state.envs),
                              child: Text(allSelected ? '取消全选' : '全选'),
                            ),
                          ],
                        ),
                        Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          children: [
                            _BatchActionButton(
                              label: '批量分组',
                              icon: Icons.label_outline,
                              color: AppColors.blue500,
                              isLight: isLight,
                              enabled: selectedCount > 0,
                              onTap: () => _showBatchGroupDialog(state.groups),
                            ),
                            _BatchActionButton(
                              label: '批量启用',
                              icon: Icons.play_circle_outline,
                              color: AppColors.primary,
                              isLight: isLight,
                              enabled: selectedCount > 0,
                              onTap: () =>
                                  _performBatchAction(_EnvBatchAction.enable),
                            ),
                            _BatchActionButton(
                              label: '批量禁用',
                              icon: Icons.pause_circle_outline,
                              color: AppColors.slate500,
                              isLight: isLight,
                              enabled: selectedCount > 0,
                              onTap: () =>
                                  _performBatchAction(_EnvBatchAction.disable),
                            ),
                            _BatchActionButton(
                              label: '批量删除',
                              icon: Icons.delete_outline,
                              color: AppColors.red500,
                              isLight: isLight,
                              enabled: selectedCount > 0,
                              onTap: () =>
                                  _performBatchAction(_EnvBatchAction.delete),
                            ),
                          ],
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ],
            if (_sortMode) ...[
              const SizedBox(height: 8),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20),
                child: Container(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 10,
                  ),
                  decoration: BoxDecoration(
                    color: isLight
                        ? AppColors.primary.withAlpha(12)
                        : AppColors.primary.withAlpha(20),
                    borderRadius: BorderRadius.circular(10),
                    border: Border.all(color: AppColors.primary.withAlpha(40)),
                  ),
                  child: Row(
                    children: [
                      const Icon(
                        Icons.swap_vert,
                        size: 16,
                        color: AppColors.primary,
                      ),
                      const SizedBox(width: 8),
                      const Expanded(
                        child: Text(
                          '长按拖拽调整顺序，点击「完成」保存',
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
            const SizedBox(height: 12),
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: _refresh,
                child: state.loading && state.envs.isEmpty
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
                    : state.error != null && state.envs.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.symmetric(horizontal: 20),
                        children: [
                          const SizedBox(height: 100),
                          const Icon(
                            Icons.error_outline,
                            size: 56,
                            color: AppColors.red500,
                          ),
                          const SizedBox(height: 12),
                          Center(
                            child: Text(
                              state.error!,
                              textAlign: TextAlign.center,
                              style: const TextStyle(color: AppColors.red500),
                            ),
                          ),
                          const SizedBox(height: 12),
                          Center(
                            child: OutlinedButton.icon(
                              onPressed: _refresh,
                              icon: const Icon(Icons.refresh, size: 16),
                              label: const Text('重试'),
                            ),
                          ),
                        ],
                      )
                    : state.envs.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.key_off,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无环境变量',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : _sortMode
                    ? ReorderableListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
                        itemCount: state.envs.length,
                        onReorderItem: (oldIndex, newIndex) {
                          final current = List<EnvVar>.from(state.envs);
                          if (current.isEmpty || oldIndex >= current.length) {
                            return;
                          }
                          final sourceEnv = current[oldIndex];
                          int? targetId;
                          if (newIndex > 0 && newIndex - 1 < current.length) {
                            final targetSourceIndex = newIndex > oldIndex
                                ? newIndex
                                : newIndex - 1;
                            if (targetSourceIndex >= 0 &&
                                targetSourceIndex < current.length) {
                              targetId = current[targetSourceIndex].id;
                            }
                          }
                          ref
                              .read(envListProvider.notifier)
                              .reorderLocal(oldIndex, newIndex);
                          setState(() {
                            _pendingSortMoves.add((
                              sourceId: sourceEnv.id,
                              targetId: targetId,
                            ));
                          });
                        },
                        itemBuilder: (_, i) {
                          final env = state.envs[i];
                          return AppCard(
                            key: ValueKey(env.id),
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
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        env.name,
                                        style: const TextStyle(
                                          fontSize: 14,
                                          fontWeight: FontWeight.w600,
                                        ),
                                      ),
                                      if (env.remarks.isNotEmpty)
                                        Text(
                                          env.remarks,
                                          style: TextStyle(
                                            fontSize: 12,
                                            color: AppColors.slate400,
                                          ),
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                    ],
                                  ),
                                ),
                                Container(
                                  width: 8,
                                  height: 8,
                                  decoration: BoxDecoration(
                                    color: env.enabled
                                        ? AppColors.primary
                                        : AppColors.slate300,
                                    shape: BoxShape.circle,
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 110),
                        itemCount: state.envs.length,
                        itemBuilder: (_, i) {
                          final env = state.envs[i];
                          return _EnvCard(
                            env: env,
                            isLight: isLight,
                            selectionMode: _selectionMode,
                            selected: _isSelected(env.id),
                            onTap: () {
                              if (_selectionMode) {
                                _toggleSelection(env.id);
                              } else {
                                _showDetailSheet(env);
                              }
                            },
                            onLongPress: () {
                              if (!_selectionMode) {
                                HapticFeedback.mediumImpact();
                                setState(() => _sortMode = true);
                              }
                            },
                            onSelectedChanged: () => _toggleSelection(env.id),
                            onCopy: () {
                              Clipboard.setData(ClipboardData(text: env.value));
                              AppGlassNotice.show(
                                context,
                                '已复制值',
                                type: AppGlassNoticeType.info,
                              );
                            },
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

  void _showDetailSheet(EnvVar env) {
    final nameC = TextEditingController(text: env.name);
    final valueC = TextEditingController(text: env.value);
    final remarksC = TextEditingController(text: env.remarks);
    final groupC = TextEditingController(text: env.groups.join(', '));
    final groups = [...ref.read(envListProvider).groups];
    var valueEditorOpen = false;

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) {
          final theme = Theme.of(ctx);
          final navigator = Navigator.of(ctx);
          final sheetRoute = ModalRoute.of(ctx);
          if (valueEditorOpen) {
            return _EnvValueSheetEditor(
              title: '编辑变量值',
              controller: valueC,
              onDone: () => setSheetState(() => valueEditorOpen = false),
            );
          }
          return AnimatedPadding(
            duration: const Duration(milliseconds: 180),
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.of(ctx).size.height * 0.82,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        Container(
                          padding: const EdgeInsets.symmetric(
                            horizontal: 10,
                            vertical: 5,
                          ),
                          decoration: BoxDecoration(
                            color:
                                (env.enabled
                                        ? AppColors.primary
                                        : AppColors.slate400)
                                    .withAlpha(18),
                            borderRadius: BorderRadius.circular(999),
                          ),
                          child: Text(
                            env.enabled ? '当前已启用' : '当前已禁用',
                            style: TextStyle(
                              fontSize: 11,
                              fontWeight: FontWeight.w700,
                              color: env.enabled
                                  ? AppColors.primary
                                  : AppColors.slate500,
                            ),
                          ),
                        ),
                        const Spacer(),
                        OutlinedButton.icon(
                          onPressed: () async {
                            await ref
                                .read(envListProvider.notifier)
                                .toggle(env.id, !env.enabled);
                            if (!mounted || sheetRoute?.isCurrent != true) {
                              return;
                            }
                            navigator.pop();
                            AppGlassNotice.show(
                              context,
                              env.enabled
                                  ? '已禁用 ${env.name}'
                                  : '已启用 ${env.name}',
                              type: AppGlassNoticeType.success,
                            );
                          },
                          icon: Icon(
                            env.enabled
                                ? Icons.pause_circle_outline
                                : Icons.play_arrow,
                            size: 16,
                          ),
                          label: Text(env.enabled ? '禁用' : '启用'),
                          style: OutlinedButton.styleFrom(
                            foregroundColor: env.enabled
                                ? AppColors.slate600
                                : AppColors.primary,
                            side: BorderSide(
                              color: env.enabled
                                  ? AppColors.slate300
                                  : AppColors.primary,
                            ),
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 12),
                    Text(
                      env.name,
                      style: theme.textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 16),
                    TextField(
                      controller: nameC,
                      decoration: const InputDecoration(labelText: '变量名'),
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: valueC,
                      decoration: InputDecoration(
                        labelText: '值',
                        suffixIcon: IconButton(
                          // 变量值较长时切到弹窗内的大输入区，不再新开页面，避免回填丢失。
                          icon: const Icon(Icons.open_in_full, size: 18),
                          tooltip: '放大编辑变量值',
                          onPressed: () =>
                              setSheetState(() => valueEditorOpen = true),
                        ),
                      ),
                      maxLines: 4,
                      minLines: 2,
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: remarksC,
                      decoration: const InputDecoration(labelText: '备注'),
                    ),
                    const SizedBox(height: 12),
                    _buildGroupAutocomplete(
                      controller: groupC,
                      options: groups,
                    ),
                    const SizedBox(height: 20),
                    Row(
                      children: [
                        Expanded(
                          child: OutlinedButton.icon(
                            onPressed: () => Navigator.of(ctx).pop(),
                            icon: const Icon(Icons.close, size: 16),
                            label: const Text('关闭'),
                            style: OutlinedButton.styleFrom(
                              minimumSize: const Size(0, 44),
                            ),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: OutlinedButton.icon(
                            onPressed: () {
                              Clipboard.setData(
                                ClipboardData(text: valueC.text),
                              );
                              AppGlassNotice.show(
                                context,
                                '已复制值',
                                type: AppGlassNoticeType.info,
                              );
                            },
                            icon: const Icon(Icons.copy, size: 16),
                            label: const Text('复制'),
                            style: OutlinedButton.styleFrom(
                              foregroundColor: AppColors.blue500,
                              side: const BorderSide(color: AppColors.blue500),
                              minimumSize: const Size(0, 44),
                            ),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: FilledButton.icon(
                            onPressed: () async {
                              final navigator = Navigator.of(ctx);
                              final sheetRoute = ModalRoute.of(ctx);
                              try {
                                await ref
                                    .read(envListProvider.notifier)
                                    .update(
                                      env.id,
                                      nameC.text.trim(),
                                      valueC.text,
                                      remarks: remarksC.text.trim(),
                                      groups: _normalizeGroups([groupC.text]),
                                    );
                                if (!mounted || sheetRoute?.isCurrent != true) {
                                  return;
                                }
                                navigator.pop();
                                AppGlassNotice.show(
                                  context,
                                  '已保存',
                                  type: AppGlassNoticeType.success,
                                );
                              } catch (error) {
                                if (!mounted) {
                                  return;
                                }
                                AppGlassNotice.show(
                                  context,
                                  extractErrorMessage(error, '保存环境变量失败'),
                                  type: AppGlassNoticeType.error,
                                );
                              }
                            },
                            icon: const Icon(Icons.save, size: 16),
                            label: const Text('保存'),
                            style: FilledButton.styleFrom(
                              minimumSize: const Size(0, 44),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    ).then((_) {
      nameC.dispose();
      valueC.dispose();
      remarksC.dispose();
      groupC.dispose();
    });
  }

  void _showCreateDialog() {
    final nameC = TextEditingController();
    final valueC = TextEditingController();
    final remarksC = TextEditingController();
    final groupC = TextEditingController();
    final groups = [...ref.read(envListProvider).groups];
    var valueEditorOpen = false;

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheetState) {
          final navigator = Navigator.of(ctx);
          final sheetRoute = ModalRoute.of(ctx);
          if (valueEditorOpen) {
            return _EnvValueSheetEditor(
              title: '新建变量值',
              controller: valueC,
              onDone: () => setSheetState(() => valueEditorOpen = false),
            );
          }
          return AnimatedPadding(
            duration: const Duration(milliseconds: 180),
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: ConstrainedBox(
              constraints: BoxConstraints(
                maxHeight: MediaQuery.of(ctx).size.height * 0.82,
              ),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      '新建环境变量',
                      style: Theme.of(ctx).textTheme.titleMedium?.copyWith(
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                    const SizedBox(height: 16),
                    TextField(
                      controller: nameC,
                      decoration: const InputDecoration(
                        labelText: '变量名',
                        hintText: '如 MY_TOKEN',
                      ),
                      textInputAction: TextInputAction.next,
                    ),
                    const SizedBox(height: 12),
                    TextField(
                      controller: valueC,
                      decoration: InputDecoration(
                        labelText: '值',
                        suffixIcon: IconButton(
                          // 新建变量时也用同一个控制器放大编辑，完成后原表单立即保留输入。
                          icon: const Icon(Icons.open_in_full, size: 18),
                          tooltip: '放大编辑变量值',
                          onPressed: () =>
                              setSheetState(() => valueEditorOpen = true),
                        ),
                      ),
                      maxLines: 3,
                      minLines: 1,
                      textInputAction: TextInputAction.next,
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: TextField(
                            controller: remarksC,
                            decoration: const InputDecoration(labelText: '备注'),
                            textInputAction: TextInputAction.next,
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: _buildGroupAutocomplete(
                            controller: groupC,
                            options: groups,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 20),
                    FilledButton(
                      onPressed: () async {
                        if (nameC.text.trim().isEmpty) return;
                        try {
                          await ref
                              .read(envListProvider.notifier)
                              .create(
                                nameC.text.trim(),
                                valueC.text,
                                remarks: remarksC.text.trim(),
                                groups: _normalizeGroups([groupC.text]),
                              );
                          if (!mounted || sheetRoute?.isCurrent != true) {
                            return;
                          }
                          navigator.pop();
                          AppGlassNotice.show(
                            context,
                            '环境变量已创建',
                            type: AppGlassNoticeType.success,
                          );
                        } catch (error) {
                          if (!mounted) {
                            return;
                          }
                          AppGlassNotice.show(
                            context,
                            extractErrorMessage(error, '创建环境变量失败'),
                            type: AppGlassNoticeType.error,
                          );
                        }
                      },
                      style: FilledButton.styleFrom(
                        minimumSize: const Size(0, 48),
                      ),
                      child: const Text('创建'),
                    ),
                  ],
                ),
              ),
            ),
          );
        },
      ),
    ).then((_) {
      nameC.dispose();
      valueC.dispose();
      remarksC.dispose();
      groupC.dispose();
    });
  }
}

class _EnvValueSheetEditor extends ConsumerWidget {
  final String title;
  final TextEditingController controller;
  final VoidCallback onDone;

  const _EnvValueSheetEditor({
    required this.title,
    required this.controller,
    required this.onDone,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final screenHeight = MediaQuery.of(context).size.height;
    final keyboardHeight = MediaQuery.of(context).viewInsets.bottom;
    final editorHeight = (screenHeight - keyboardHeight - 72)
        .clamp(420.0, screenHeight * 0.88)
        .toDouble();

    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) {
          return;
        }
        // 系统返回键只退出大输入区，不直接关闭整个新建/编辑弹窗。
        onDone();
      },
      child: SafeArea(
        child: Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, keyboardHeight + 16),
          child: SizedBox(
            height: editorHeight,
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Container(
                      width: 36,
                      height: 36,
                      decoration: BoxDecoration(
                        color: AppColors.primary.withAlpha(isLight ? 20 : 34),
                        borderRadius: BorderRadius.circular(12),
                      ),
                      child: const Icon(
                        Icons.open_in_full,
                        size: 18,
                        color: AppColors.primary,
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text(
                        title,
                        style: const TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w800,
                        ),
                      ),
                    ),
                    IconButton(
                      tooltip: '回到表单',
                      onPressed: onDone,
                      icon: const Icon(Icons.close),
                    ),
                  ],
                ),
                const SizedBox(height: 8),
                const Text(
                  '这里直接编辑原表单里的值，点完成后会回到新建/编辑窗口，不会丢输入。',
                  style: TextStyle(fontSize: 12, color: AppColors.slate500),
                ),
                const SizedBox(height: 12),
                Expanded(
                  child: TextField(
                    controller: controller,
                    autofocus: true,
                    expands: true,
                    maxLines: null,
                    minLines: null,
                    keyboardType: TextInputType.multiline,
                    textAlignVertical: TextAlignVertical.top,
                    decoration: InputDecoration(
                      hintText: '在这里编辑完整变量值',
                      alignLabelWithHint: true,
                      filled: true,
                      fillColor: glassFillColor(isLight: isLight),
                      border: OutlineInputBorder(
                        borderRadius: BorderRadius.circular(14),
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton.icon(
                        onPressed: () {
                          controller.clear();
                        },
                        icon: const Icon(Icons.cleaning_services, size: 16),
                        label: const Text('清空'),
                        style: OutlinedButton.styleFrom(
                          foregroundColor: AppColors.slate500,
                          minimumSize: const Size(0, 44),
                        ),
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      flex: 2,
                      child: FilledButton.icon(
                        // 不再新开路由，直接收起大输入区，因此原表单控制器会立即保留当前文本。
                        onPressed: onDone,
                        icon: const Icon(Icons.check, size: 18),
                        label: const Text('完成，回到表单'),
                        style: FilledButton.styleFrom(
                          minimumSize: const Size(0, 44),
                        ),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _HeaderChipButton extends ConsumerWidget {
  final String label;
  final IconData icon;
  final bool isLight;
  final VoidCallback onTap;

  const _HeaderChipButton({
    required this.label,
    required this.icon,
    required this.isLight,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return AppLiquidGlassSurface(
      onTap: onTap,
      borderRadius: 16,
      performanceMode: true,
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 7),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 16, color: AppColors.slate400),
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
      ),
    );
  }
}

class _BatchActionButton extends StatelessWidget {
  final String label;
  final IconData icon;
  final Color color;
  final bool isLight;
  final bool enabled;
  final VoidCallback onTap;

  const _BatchActionButton({
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

    return Material(
      color: enabled ? color.withAlpha(16) : Colors.transparent,
      borderRadius: BorderRadius.circular(12),
      child: InkWell(
        onTap: enabled ? onTap : null,
        borderRadius: BorderRadius.circular(12),
        child: Padding(
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
        ),
      ),
    );
  }
}

class _EnvCard extends StatefulWidget {
  final EnvVar env;
  final bool isLight;
  final bool selectionMode;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onLongPress;
  final VoidCallback onSelectedChanged;
  final VoidCallback onCopy;

  const _EnvCard({
    required this.env,
    required this.isLight,
    required this.selectionMode,
    required this.selected,
    required this.onTap,
    required this.onLongPress,
    required this.onSelectedChanged,
    required this.onCopy,
  });

  @override
  State<_EnvCard> createState() => _EnvCardState();
}

class _EnvCardState extends State<_EnvCard> {
  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () {
        if (widget.selectionMode) {
          widget.onSelectedChanged();
        } else {
          widget.onTap();
        }
      },
      onLongPress: widget.onLongPress,
      child: ClipRRect(
        borderRadius: BorderRadius.circular(16),
        child: AppCard(
          stableForScrolling: true,
          margin: const EdgeInsets.only(bottom: 12),
          borderRadius: 16,
          padding: const EdgeInsets.fromLTRB(14, 14, 14, 18),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  if (widget.selectionMode) ...[
                    SizedBox(
                      width: 24,
                      height: 24,
                      child: Checkbox(
                        value: widget.selected,
                        onChanged: (_) => widget.onSelectedChanged(),
                        activeColor: AppColors.primary,
                        visualDensity: VisualDensity.compact,
                        materialTapTargetSize: MaterialTapTargetSize.shrinkWrap,
                      ),
                    ),
                    const SizedBox(width: 8),
                  ],
                  Container(
                    width: 6,
                    height: 6,
                    decoration: BoxDecoration(
                      color: widget.env.enabled
                          ? AppColors.primary
                          : AppColors.slate300,
                      shape: BoxShape.circle,
                    ),
                  ),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      widget.env.name,
                      style: TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w700,
                        color: widget.isLight
                            ? AppColors.blue600
                            : AppColors.blue500,
                      ),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                  if (!widget.selectionMode) ...[
                    _MiniBtn(icon: Icons.copy_outlined, onTap: widget.onCopy),
                    const SizedBox(width: 4),
                    _MiniBtn(icon: Icons.open_in_new, onTap: widget.onTap),
                  ],
                ],
              ),
              Padding(
                padding: EdgeInsets.only(left: widget.selectionMode ? 32 : 14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const SizedBox(height: 8),
                    Text(
                      widget.env.value,
                      style: TextStyle(
                        fontSize: 13,
                        fontFamily: 'monospace',
                        height: 1.55,
                        color: widget.isLight
                            ? AppColors.slate600
                            : AppColors.slate400,
                      ),
                      maxLines: 6,
                      overflow: TextOverflow.ellipsis,
                    ),
                    if (widget.env.remarks.isNotEmpty) ...[
                      const SizedBox(height: 10),
                      Text(
                        widget.env.remarks,
                        style: TextStyle(
                          fontSize: 12,
                          height: 1.4,
                          color: widget.isLight
                              ? AppColors.slate400
                              : AppColors.slate500,
                          fontStyle: FontStyle.italic,
                        ),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _MiniBtn extends StatelessWidget {
  final IconData icon;
  final VoidCallback onTap;

  const _MiniBtn({required this.icon, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Theme.of(context).colorScheme.surface.withAlpha(36),
      borderRadius: BorderRadius.circular(8),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(8),
        child: SizedBox(
          width: 30,
          height: 30,
          child: Icon(icon, size: 14, color: AppColors.slate400),
        ),
      ),
    );
  }
}
