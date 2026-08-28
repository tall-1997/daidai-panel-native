import 'dart:async';
import 'dart:convert';
import 'dart:ui';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../core/storage/secure_storage.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/ansi_text.dart';
import '../../../shared/utils/log_background.dart';
import '../../../shared/utils/bounded_log_buffer.dart';
import '../../../shared/utils/time_utils.dart';
import '../../tasks/views/task_form_page.dart';
import '../../../shared/widgets/app_card.dart';
import '../models/script_run_log_data.dart';

final scriptProvider = StateNotifierProvider<ScriptNotifier, ScriptState>((
  ref,
) {
  return ScriptNotifier();
});

enum _ScriptAction { upload, createFile, createDirectory }

enum _ScriptEntryAction {
  open,
  addToTask,
  favorite,
  download,
  move,
  copy,
  rename,
  delete,
  versions,
  uploadHere,
  createFileHere,
  createDirectoryHere,
}

enum _ScriptViewerAction { format, versions, addToTask, debug }

const _stateUnset = Object();

class ScriptFile {
  final String name;
  final String path;
  final bool isDirectory;
  final List<ScriptFile> children;

  const ScriptFile({
    required this.name,
    required this.path,
    this.isDirectory = false,
    this.children = const [],
  });

  factory ScriptFile.fromJson(Map<String, dynamic> json) {
    final children =
        (json['children'] as List?)
            ?.whereType<Map>()
            .map(
              (entry) => ScriptFile.fromJson(Map<String, dynamic>.from(entry)),
            )
            .toList() ??
        [];
    return ScriptFile(
      name: json['title']?.toString() ?? json['name']?.toString() ?? '',
      path: json['key']?.toString() ?? json['path']?.toString() ?? '',
      isDirectory:
          json['type'] == 'directory' ||
          json['is_directory'] == true ||
          json['isLeaf'] == false,
      children: children,
    );
  }
}

class ScriptVersionRecord {
  final int id;
  final int version;
  final String message;
  final int contentLength;
  final DateTime? createdAt;

  const ScriptVersionRecord({
    required this.id,
    required this.version,
    required this.message,
    required this.contentLength,
    required this.createdAt,
  });

  factory ScriptVersionRecord.fromJson(Map<String, dynamic> json) {
    return ScriptVersionRecord(
      id: (json['id'] as num?)?.toInt() ?? 0,
      version: (json['version'] as num?)?.toInt() ?? 0,
      message: json['message']?.toString() ?? '',
      contentLength: (json['content_length'] as num?)?.toInt() ?? 0,
      createdAt: json['created_at'] is String
          ? DateTime.tryParse(json['created_at'].toString())
          : null,
    );
  }
}

class ScriptState {
  final List<ScriptFile> tree;
  final bool loading;
  final String keyword;
  final String? selectedPath;
  final String content;
  final bool isBinary;
  final bool loadingContent;
  final bool saving;
  final String? error;

  const ScriptState({
    this.tree = const [],
    this.loading = false,
    this.keyword = '',
    this.selectedPath,
    this.content = '',
    this.isBinary = false,
    this.loadingContent = false,
    this.saving = false,
    this.error,
  });

  ScriptState copyWith({
    List<ScriptFile>? tree,
    bool? loading,
    String? keyword,
    Object? selectedPath = _stateUnset,
    String? content,
    bool? isBinary,
    bool? loadingContent,
    bool? saving,
    String? error,
    bool clearError = false,
  }) {
    return ScriptState(
      tree: tree ?? this.tree,
      loading: loading ?? this.loading,
      keyword: keyword ?? this.keyword,
      selectedPath: identical(selectedPath, _stateUnset)
          ? this.selectedPath
          : selectedPath as String?,
      content: content ?? this.content,
      isBinary: isBinary ?? this.isBinary,
      loadingContent: loadingContent ?? this.loadingContent,
      saving: saving ?? this.saving,
      error: clearError ? null : error ?? this.error,
    );
  }
}

class ScriptNotifier extends StateNotifier<ScriptState> {
  ScriptNotifier({ScriptState initialState = const ScriptState()})
    : _sessionScope = AuthSessionEpoch.scoped(
        PanelCapabilityRegistry.currentScope,
      ),
      super(initialState) {
    AuthSessionEpoch.addListener(_synchronizeSessionScope);
    PanelCapabilityRegistry.addScopeListener(_synchronizeSessionScope);
  }

  int _contentRequestId = 0;
  String _sessionScope;

  void _synchronizeSessionScope() {
    final nextScope = AuthSessionEpoch.scoped(
      PanelCapabilityRegistry.currentScope,
    );
    if (nextScope == _sessionScope) return;
    _sessionScope = nextScope;
    _contentRequestId++;
    state = ScriptState(keyword: state.keyword);
  }

  @override
  void dispose() {
    AuthSessionEpoch.removeListener(_synchronizeSessionScope);
    PanelCapabilityRegistry.removeScopeListener(_synchronizeSessionScope);
    super.dispose();
  }

  void setKeyword(String keyword) {
    state = state.copyWith(keyword: keyword);
  }

  Future<void> loadTree() async {
    _synchronizeSessionScope();
    final sessionScope = _sessionScope;
    state = state.copyWith(loading: true, clearError: true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.scriptsTree);
      final data = extractData(resp.data);
      final tree = data is List
          ? data
                .whereType<Map>()
                .map(
                  (entry) =>
                      ScriptFile.fromJson(Map<String, dynamic>.from(entry)),
                )
                .toList()
          : <ScriptFile>[];
      if (sessionScope != _sessionScope) {
        return;
      }
      state = state.copyWith(tree: tree, loading: false, clearError: true);
    } catch (error) {
      if (sessionScope != _sessionScope) {
        return;
      }
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '脚本列表加载失败'),
      );
    }
  }

  Future<void> loadContent(String path) async {
    _synchronizeSessionScope();
    final requestId = ++_contentRequestId;
    final sessionScope = _sessionScope;
    state = state.copyWith(
      selectedPath: path,
      loadingContent: true,
      clearError: true,
    );
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.scriptsContent,
        queryParameters: {'path': path},
      );
      final data = extractData(resp.data);
      if (requestId != _contentRequestId ||
          state.selectedPath != path ||
          sessionScope != _sessionScope) {
        return;
      }
      if (data is Map) {
        final isBinary = data['binary'] == true || data['is_binary'] == true;
        state = state.copyWith(
          selectedPath: path,
          content: isBinary ? '' : (data['content']?.toString() ?? ''),
          isBinary: isBinary,
          loadingContent: false,
          clearError: true,
        );
        return;
      }
      state = state.copyWith(
        selectedPath: path,
        content: data?.toString() ?? '',
        isBinary: false,
        loadingContent: false,
        clearError: true,
      );
    } catch (_) {
      if (requestId != _contentRequestId ||
          state.selectedPath != path ||
          sessionScope != _sessionScope) {
        return;
      }
      state = state.copyWith(
        selectedPath: path,
        content: '',
        isBinary: false,
        loadingContent: false,
        error: '脚本内容加载失败',
      );
    }
  }

  Future<void> saveContent(
    String path,
    String content, {
    String message = '',
  }) async {
    state = state.copyWith(saving: true);
    try {
      await DioClient.instance.dio.put(
        ApiEndpoints.scriptsContent,
        data: {'path': path, 'content': content, 'message': message},
      );
      state = state.copyWith(content: content, isBinary: false, saving: false);
    } catch (_) {
      state = state.copyWith(saving: false);
      rethrow;
    }
  }

  Future<void> createFile(String path) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.scriptsContent,
      data: {'path': path, 'content': '', 'message': 'V1 初始版本'},
    );
    await loadTree();
  }

  Future<void> createDirectory(String path) async {
    await DioClient.instance.dio.post(
      ApiEndpoints.scriptsDirectory,
      data: {'path': path},
    );
    await loadTree();
  }

  Future<List<String>> uploadFiles(
    List<PlatformFile> files, {
    String dir = '',
  }) async {
    if (files.isEmpty) {
      throw StateError('未选择可上传的文件');
    }
    final uploadedPaths = <String>[];
    for (final file in files) {
      final multipart = await _toMultipartFile(file);
      if (multipart == null) continue;
      final formData = FormData();
      formData.files.add(MapEntry('file', multipart));
      formData.fields.add(
        MapEntry('filename_b64', base64Encode(utf8.encode(file.name))),
      );
      if (dir.trim().isNotEmpty) {
        formData.fields.add(MapEntry('dir', dir.trim()));
      }
      final resp = await DioClient.instance.dio.post(
        ApiEndpoints.scriptsUpload,
        data: formData,
        options: Options(contentType: 'multipart/form-data'),
      );
      final raw = resp.data;
      if (raw is Map && raw['paths'] is List) {
        uploadedPaths.addAll(
          (raw['paths'] as List).map((item) => item.toString()),
        );
      } else if (raw is Map && raw['path'] != null) {
        uploadedPaths.add(raw['path'].toString());
      } else {
        uploadedPaths.add(_joinScriptPath(dir, file.name));
      }
    }
    if (uploadedPaths.isEmpty) {
      throw StateError('未选择可上传的文件');
    }
    await loadTree();
    return uploadedPaths;
  }

  Future<String> renamePath(String oldPath, String newName) async {
    final resp = await DioClient.instance.dio.put(
      ApiEndpoints.scriptsRename,
      data: {'old_path': oldPath, 'new_name': newName},
    );
    await loadTree();
    final data = resp.data;
    final rawPath = data is Map && data['new_path'] != null
        ? data['new_path']
        : null;
    final newPath =
        rawPath?.toString() ??
        _joinScriptPath(_defaultScriptDirectory(oldPath), newName);

    final selected = state.selectedPath;
    if (selected == oldPath) {
      state = state.copyWith(selectedPath: newPath);
    } else if (selected != null && selected.startsWith('$oldPath/')) {
      state = state.copyWith(
        selectedPath: selected.replaceFirst(oldPath, newPath),
      );
    }
    return newPath;
  }

  Future<String> movePath(String sourcePath, {String targetDir = ''}) async {
    final response = await DioClient.instance.dio.put(
      ApiEndpoints.scriptsMove,
      data: {'source_path': sourcePath, 'target_dir': targetDir},
    );
    await loadTree();
    final data = response.data;
    final newPath = data is Map && data['new_path'] != null
        ? data['new_path'].toString()
        : _joinScriptPath(targetDir, sourcePath.split('/').last);

    final selected = state.selectedPath;
    if (selected == sourcePath) {
      state = state.copyWith(selectedPath: newPath);
    } else if (selected != null && selected.startsWith('$sourcePath/')) {
      state = state.copyWith(
        selectedPath: selected.replaceFirst(sourcePath, newPath),
      );
    }
    return newPath;
  }

  Future<String> copyPath(
    String sourcePath, {
    String targetDir = '',
    String newName = '',
  }) async {
    final response = await DioClient.instance.dio.post(
      ApiEndpoints.scriptsCopy,
      data: {
        'source_path': sourcePath,
        'target_dir': targetDir,
        'new_name': newName,
      },
    );
    await loadTree();
    final data = response.data;
    if (data is Map && data['new_path'] != null) {
      return data['new_path'].toString();
    }
    final finalName = newName.trim().isEmpty
        ? sourcePath.split('/').last
        : newName.trim();
    return _joinScriptPath(targetDir, finalName);
  }

  Future<void> deletePath(String path, {required bool isDirectory}) async {
    await DioClient.instance.dio.delete(
      ApiEndpoints.scripts,
      queryParameters: {
        'path': path,
        'type': isDirectory ? 'directory' : 'file',
      },
    );
    await loadTree();
    final selected = state.selectedPath;
    if (selected == path ||
        (isDirectory && (selected?.startsWith('$path/') ?? false))) {
      state = state.copyWith(selectedPath: null, content: '', isBinary: false);
    }
  }

  Future<List<ScriptVersionRecord>> listVersions(String path) async {
    final resp = await DioClient.instance.dio.get(
      ApiEndpoints.scriptsVersions,
      queryParameters: {'path': path},
    );
    final data = extractData(resp.data);
    if (data is! List) {
      return const [];
    }
    return data
        .whereType<Map>()
        .map(
          (item) =>
              ScriptVersionRecord.fromJson(Map<String, dynamic>.from(item)),
        )
        .toList();
  }

  Future<void> rollbackVersion(int versionId, String path) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.scriptVersionRollback(versionId),
    );
    await loadTree();
    await loadContent(path);
  }

  Future<String> formatContent(String path, String content) async {
    final language = _detectFormatterLanguage(path);
    if (language == null) {
      throw StateError('该文件类型不支持格式化');
    }
    final resp = await DioClient.instance.dio.post(
      ApiEndpoints.scriptsFormat,
      data: {'content': content, 'language': language},
    );
    final data = extractData(resp.data);
    final formatted = data is Map
        ? data['content']?.toString() ?? content
        : content;
    state = state.copyWith(content: formatted, isBinary: false);
    return formatted;
  }

  Future<MultipartFile?> _toMultipartFile(PlatformFile file) async {
    if (file.path != null && file.path!.isNotEmpty) {
      return MultipartFile.fromFile(file.path!, filename: file.name);
    }
    if (file.bytes != null) {
      return MultipartFile.fromBytes(file.bytes!, filename: file.name);
    }
    return null;
  }

  Uint8List? extractBytes(dynamic data) {
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
}

class ScriptListPage extends ConsumerStatefulWidget {
  const ScriptListPage({super.key});

  @override
  ConsumerState<ScriptListPage> createState() => _ScriptListPageState();
}

class _ScriptListPageState extends ConsumerState<ScriptListPage> {
  static const _favoriteScriptsStorageKey = 'scripts.favorite_paths';
  final _searchController = TextEditingController();
  final Set<String> _favoriteScriptPaths = <String>{};

  @override
  void initState() {
    super.initState();
    Future.microtask(() async {
      final favorites = await SecureStorage.getUiStateList(
        _favoriteScriptsStorageKey,
      );
      if (mounted) {
        setState(() {
          _favoriteScriptPaths
            ..clear()
            ..addAll(favorites);
        });
      }
      await ref.read(scriptProvider.notifier).loadTree();
    });
  }

  @override
  void dispose() {
    _searchController.dispose();
    super.dispose();
  }

  void _showMessage(
    String message, {
    AppGlassNoticeType type = AppGlassNoticeType.info,
  }) {
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(context, message, type: type);
  }

  String _extractScriptError(dynamic error, String fallback) =>
      extractScriptSaveErrorMessage(error, fallback);

  Future<void> _openScript(String path) async {
    await ref.read(scriptProvider.notifier).loadContent(path);
    if (!mounted) {
      return;
    }
    context.push('/scripts/view', extra: path);
  }

  Future<void> _persistFavoriteScripts() {
    return SecureStorage.saveUiStateList(
      _favoriteScriptsStorageKey,
      _favoriteScriptPaths.toList(),
    );
  }

  Future<void> _toggleFavoriteScript(ScriptFile file) async {
    setState(() {
      if (_favoriteScriptPaths.contains(file.path)) {
        _favoriteScriptPaths.remove(file.path);
      } else {
        _favoriteScriptPaths.add(file.path);
      }
    });
    await _persistFavoriteScripts();
    if (!mounted) {
      return;
    }
    _showMessage(
      _favoriteScriptPaths.contains(file.path) ? '已置顶脚本' : '已取消置顶脚本',
    );
  }

  List<ScriptFile> _filterTree(List<ScriptFile> nodes, String keyword) {
    final query = keyword.trim().toLowerCase();
    if (query.isEmpty) {
      return nodes;
    }

    List<ScriptFile> visit(List<ScriptFile> items) {
      final result = <ScriptFile>[];
      for (final item in items) {
        final children = visit(item.children);
        final matched =
            item.name.toLowerCase().contains(query) ||
            item.path.toLowerCase().contains(query);
        if (matched || children.isNotEmpty) {
          result.add(
            ScriptFile(
              name: item.name,
              path: item.path,
              isDirectory: item.isDirectory,
              children: children,
            ),
          );
        }
      }
      return result;
    }

    return visit(nodes);
  }

  TaskFormPrefill _taskPrefillFromScriptPath(String path) {
    final fileName = path.split('/').last;
    final taskName = fileName.replaceFirst(RegExp(r'\.[^/.]+$'), '');
    return TaskFormPrefill(name: taskName, command: 'task $path');
  }

  Future<void> _navigateToTaskWithScript(String path) async {
    if (!mounted) {
      return;
    }
    await context.push('/tasks/new', extra: _taskPrefillFromScriptPath(path));
  }

  Future<void> _maybePromptAddToTask(String path) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('加入任务'),
        content: Text('脚本「${path.split('/').last}」上传成功，是否直接添加到定时任务？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '稍后再说',
                onPressed: () => Navigator.pop(dialogContext, false),
                variant: AppLiquidGlassButtonVariant.secondary,
              ),
              AppGlassDialogAction(
                label: '立即添加',
                onPressed: () => Navigator.pop(dialogContext, true),
                variant: AppLiquidGlassButtonVariant.primary,
              ),
            ],
          ),
        ],
      ),
    );
    if (confirm == true) {
      await _navigateToTaskWithScript(path);
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(scriptProvider);
    final isLight = Theme.of(context).brightness == Brightness.light;

    final visibleTree = _sortScriptTree(_filterTree(state.tree, state.keyword));

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
                      '脚本管理',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  GestureDetector(
                    onTap: () => ref.read(scriptProvider.notifier).loadTree(),
                    child: const Padding(
                      padding: EdgeInsets.all(4),
                      child: Icon(Icons.refresh, size: 22),
                    ),
                  ),
                  const SizedBox(width: 8),
                  PopupMenuButton<_ScriptAction>(
                    onSelected: (action) => _handleAction(action, state),
                    itemBuilder: (context) => const [
                      PopupMenuItem(
                        value: _ScriptAction.upload,
                        child: ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(Icons.upload_file_outlined, size: 20),
                          title: Text('上传脚本'),
                        ),
                      ),
                      PopupMenuItem(
                        value: _ScriptAction.createFile,
                        child: ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(Icons.note_add_outlined, size: 20),
                          title: Text('新建脚本'),
                        ),
                      ),
                      PopupMenuItem(
                        value: _ScriptAction.createDirectory,
                        child: ListTile(
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(
                            Icons.create_new_folder_outlined,
                            size: 20,
                          ),
                          title: Text('新建文件夹'),
                        ),
                      ),
                    ],
                    child: const AppGlassIconButton(
                      icon: Icons.add,
                      tooltip: '新建或上传脚本',
                      onTap: null,
                    ),
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
                    hintText: '搜索脚本名称或路径...',
                    prefixIcon: const Icon(
                      Icons.search,
                      size: 18,
                      color: AppColors.slate400,
                    ),
                    suffixIcon: _searchController.text.trim().isEmpty
                        ? null
                        : IconButton(
                            onPressed: () {
                              _searchController.clear();
                              ref.read(scriptProvider.notifier).setKeyword('');
                              setState(() {});
                            },
                            icon: const Icon(Icons.clear, size: 18),
                          ),
                  ),
                  onChanged: (value) {
                    ref.read(scriptProvider.notifier).setKeyword(value);
                    setState(() {});
                  },
                ),
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: state.loading
                  ? const Center(
                      child: CircularProgressIndicator(
                        color: AppColors.primary,
                      ),
                    )
                  : state.error != null && state.tree.isEmpty
                  ? _buildLoadError(state.error!)
                  : visibleTree.isEmpty
                  ? _buildEmpty(state)
                  : RefreshIndicator(
                      color: AppColors.primary,
                      onRefresh: () =>
                          ref.read(scriptProvider.notifier).loadTree(),
                      child: ListView(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                        children: [
                          if (state.error != null)
                            _ScriptInlineLoadError(
                              message: state.error!,
                              onRetry: () =>
                                  ref.read(scriptProvider.notifier).loadTree(),
                            ),
                          ...visibleTree.map(
                            (file) => _FileTreeItem(
                              file: file,
                              isLight: isLight,
                              depth: 0,
                              onTap: (path) => _openScript(path),
                              onAction: (entry) =>
                                  _handleEntryAction(entry, state),
                            ),
                          ),
                        ],
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildEmpty(ScriptState state) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.folder_off,
            size: 56,
            color: AppColors.slate400.withAlpha(120),
          ),
          const SizedBox(height: 12),
          const Text('暂无脚本', style: TextStyle(color: AppColors.slate400)),
          const SizedBox(height: 16),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              OutlinedButton.icon(
                onPressed: () => _handleAction(_ScriptAction.upload, state),
                icon: const Icon(Icons.upload_file_outlined),
                label: const Text('上传脚本'),
              ),
              OutlinedButton.icon(
                onPressed: () => _handleAction(_ScriptAction.createFile, state),
                icon: const Icon(Icons.note_add_outlined),
                label: const Text('新建脚本'),
              ),
              OutlinedButton.icon(
                onPressed: () =>
                    _handleAction(_ScriptAction.createDirectory, state),
                icon: const Icon(Icons.create_new_folder_outlined),
                label: const Text('新建文件夹'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Future<void> _handleAction(_ScriptAction action, ScriptState state) async {
    switch (action) {
      case _ScriptAction.upload:
        await _pickAndUploadFiles(state);
        return;
      case _ScriptAction.createFile:
        await _showCreateFileDialog(state);
        return;
      case _ScriptAction.createDirectory:
        await _showCreateDirectoryDialog(state);
        return;
    }
  }

  Future<void> _handleEntryAction(ScriptFile file, ScriptState state) async {
    final action = await showModalBottomSheet<_ScriptEntryAction>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (sheetContext) {
        // 菜单项较多时限制底部菜单高度，并允许上下滚动，避免“删除”入口被小屏幕截掉。
        final maxSheetHeight = MediaQuery.sizeOf(sheetContext).height * 0.8;
        return SafeArea(
          child: ConstrainedBox(
            constraints: BoxConstraints(maxHeight: maxSheetHeight),
            child: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (!file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.open_in_new),
                      title: const Text('打开脚本'),
                      onTap: () =>
                          Navigator.pop(sheetContext, _ScriptEntryAction.open),
                    ),
                  if (!file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.playlist_add_outlined),
                      title: const Text('加入任务'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.addToTask,
                      ),
                    ),
                  if (!file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.download_outlined),
                      title: const Text('下载'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.download,
                      ),
                    ),
                  if (!file.isDirectory)
                    ListTile(
                      leading: Icon(
                        _favoriteScriptPaths.contains(file.path)
                            ? Icons.push_pin_outlined
                            : Icons.push_pin,
                      ),
                      title: Text(
                        _favoriteScriptPaths.contains(file.path)
                            ? '取消置顶'
                            : '置顶到前面',
                      ),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.favorite,
                      ),
                    ),
                  if (!file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.history_outlined),
                      title: const Text('版本历史'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.versions,
                      ),
                    ),
                  ListTile(
                    leading: const Icon(Icons.drive_file_move_outline),
                    title: Text(file.isDirectory ? '移动文件夹' : '移动文件'),
                    onTap: () =>
                        Navigator.pop(sheetContext, _ScriptEntryAction.move),
                  ),
                  ListTile(
                    leading: const Icon(Icons.copy_outlined),
                    title: Text(file.isDirectory ? '复制文件夹' : '复制文件'),
                    onTap: () =>
                        Navigator.pop(sheetContext, _ScriptEntryAction.copy),
                  ),
                  if (file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.upload_file_outlined),
                      title: const Text('上传到此处'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.uploadHere,
                      ),
                    ),
                  if (file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.note_add_outlined),
                      title: const Text('在此新建脚本'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.createFileHere,
                      ),
                    ),
                  if (file.isDirectory)
                    ListTile(
                      leading: const Icon(Icons.create_new_folder_outlined),
                      title: const Text('在此新建文件夹'),
                      onTap: () => Navigator.pop(
                        sheetContext,
                        _ScriptEntryAction.createDirectoryHere,
                      ),
                    ),
                  ListTile(
                    leading: const Icon(Icons.drive_file_rename_outline),
                    title: const Text('重命名'),
                    onTap: () =>
                        Navigator.pop(sheetContext, _ScriptEntryAction.rename),
                  ),
                  ListTile(
                    leading: const Icon(
                      Icons.delete_outline,
                      color: AppColors.red500,
                    ),
                    title: const Text(
                      '删除',
                      style: TextStyle(color: AppColors.red500),
                    ),
                    onTap: () =>
                        Navigator.pop(sheetContext, _ScriptEntryAction.delete),
                  ),
                ],
              ),
            ),
          ),
        );
      },
    );

    if (action == null) {
      return;
    }

    switch (action) {
      case _ScriptEntryAction.open:
        await _openScript(file.path);
        return;
      case _ScriptEntryAction.addToTask:
        await _navigateToTaskWithScript(file.path);
        return;
      case _ScriptEntryAction.favorite:
        await _toggleFavoriteScript(file);
        return;
      case _ScriptEntryAction.download:
        await _downloadScript(file);
        return;
      case _ScriptEntryAction.move:
        await _showMoveDialog(file, state);
        return;
      case _ScriptEntryAction.copy:
        await _showCopyDialog(file, state);
        return;
      case _ScriptEntryAction.rename:
        await _showRenameDialog(file);
        return;
      case _ScriptEntryAction.delete:
        await _confirmDelete(file);
        return;
      case _ScriptEntryAction.versions:
        await _showVersionSheet(file.path);
        return;
      case _ScriptEntryAction.uploadHere:
        await _pickAndUploadFiles(state, initialDir: file.path);
        return;
      case _ScriptEntryAction.createFileHere:
        await _showCreateFileDialog(state, initialParent: file.path);
        return;
      case _ScriptEntryAction.createDirectoryHere:
        await _showCreateDirectoryDialog(state, initialParent: file.path);
        return;
    }
  }

  List<ScriptFile> _sortScriptTree(List<ScriptFile> nodes) {
    final items = nodes
        .map(
          (node) => ScriptFile(
            name: node.name,
            path: node.path,
            isDirectory: node.isDirectory,
            children: _sortScriptTree(node.children),
          ),
        )
        .toList();

    items.sort((a, b) {
      if (a.isDirectory != b.isDirectory) {
        return a.isDirectory ? -1 : 1;
      }
      final aFavorite = _favoriteScriptPaths.contains(a.path);
      final bFavorite = _favoriteScriptPaths.contains(b.path);
      if (aFavorite != bFavorite) {
        return aFavorite ? -1 : 1;
      }
      return a.name.toLowerCase().compareTo(b.name.toLowerCase());
    });

    return items;
  }

  Future<void> _showRenameDialog(ScriptFile file) async {
    final controller = TextEditingController(text: file.name);
    final parent = _defaultScriptDirectory(file.path);
    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return AlertDialog(
          title: Text(file.isDirectory ? '重命名文件夹' : '重命名脚本'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (parent.isNotEmpty) ...[
                Text(
                  '所在目录：$parent',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.slate500,
                  ),
                ),
                const SizedBox(height: 12),
              ],
              TextField(
                controller: controller,
                decoration: const InputDecoration(labelText: '新名称'),
              ),
            ],
          ),
          actions: [
            AppLiquidGlassDialogActions(
              actions: [
                AppGlassDialogAction(
                  label: '取消',
                  onPressed: () => navigator.pop(),
                  variant: AppLiquidGlassButtonVariant.secondary,
                ),
                AppGlassDialogAction(
                  label: '保存',
                  variant: AppLiquidGlassButtonVariant.primary,
                  onPressed: () async {
                    final newName = controller.text.trim();
                    if (newName.isEmpty) {
                      AppGlassNotice.show(
                        context,
                        '名称不能为空',
                        type: AppGlassNoticeType.warning,
                      );
                      return;
                    }
                    try {
                      final newPath = await ref
                          .read(scriptProvider.notifier)
                          .renamePath(file.path, newName);
                      if (!mounted) {
                        return;
                      }
                      navigator.pop();
                      _showMessage(
                        '已重命名为 ${newPath.split('/').last}',
                        type: AppGlassNoticeType.success,
                      );
                    } catch (error) {
                      if (!mounted) {
                        return;
                      }
                      AppGlassNotice.show(
                        context,
                        _extractRequestError(error, '重命名失败'),
                        type: AppGlassNoticeType.error,
                      );
                    }
                  },
                ),
              ],
            ),
          ],
        );
      },
    );
  }

  Future<void> _confirmDelete(ScriptFile file) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(file.isDirectory ? '删除文件夹' : '删除脚本'),
        content: Text(
          file.isDirectory
              ? '确定要删除文件夹「${file.name}」吗？其中的所有内容都会一起删除。'
              : '确定要删除脚本「${file.name}」吗？',
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogContext, false),
                variant: AppLiquidGlassButtonVariant.secondary,
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
    if (confirm != true) {
      return;
    }
    try {
      await ref
          .read(scriptProvider.notifier)
          .deletePath(file.path, isDirectory: file.isDirectory);
      _showMessage(
        file.isDirectory ? '文件夹已删除' : '脚本已删除',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      _showMessage(
        _extractRequestError(error, '删除失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _downloadScript(ScriptFile file) async {
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.scriptsDownload(file.path),
        options: Options(responseType: ResponseType.bytes),
      );
      final bytes = ref
          .read(scriptProvider.notifier)
          .extractBytes(response.data);
      if (bytes == null || bytes.isEmpty) {
        throw StateError('下载内容为空');
      }

      final savedPath = await FilePicker.platform.saveFile(
        dialogTitle: '保存脚本文件',
        fileName: file.name,
        type: FileType.any,
        bytes: bytes,
      );
      if (savedPath == null) {
        _showMessage('已取消保存', type: AppGlassNoticeType.warning);
        return;
      }

      _showMessage('脚本已保存', type: AppGlassNoticeType.success);
    } on UnsupportedError {
      _showMessage('当前平台暂不支持直接保存文件', type: AppGlassNoticeType.warning);
    } catch (error) {
      _showMessage(
        _extractScriptError(error, '下载脚本失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _showMoveDialog(ScriptFile file, ScriptState state) async {
    final folders = _scriptFolders(
      state.tree,
    ).where((folder) => folder != file.path).toList();
    String targetDir = _defaultScriptDirectory(file.path);

    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: Text(file.isDirectory ? '移动文件夹' : '移动文件'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '当前路径：${file.path}',
                  style: const TextStyle(
                    fontSize: 12,
                    color: AppColors.slate500,
                  ),
                ),
                const SizedBox(height: 12),
                DropdownButtonFormField<String>(
                  initialValue: targetDir,
                  decoration: const InputDecoration(labelText: '目标目录'),
                  items: [
                    const DropdownMenuItem(value: '', child: Text('根目录')),
                    ...folders.map(
                      (folder) =>
                          DropdownMenuItem(value: folder, child: Text(folder)),
                    ),
                  ],
                  onChanged: (value) {
                    setDialogState(() => targetDir = value ?? '');
                  },
                ),
              ],
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => navigator.pop(),
                    variant: AppLiquidGlassButtonVariant.secondary,
                  ),
                  AppGlassDialogAction(
                    label: '移动',
                    variant: AppLiquidGlassButtonVariant.primary,
                    onPressed: () async {
                      try {
                        final newPath = await ref
                            .read(scriptProvider.notifier)
                            .movePath(file.path, targetDir: targetDir);
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        _showMessage(
                          '已移动到 ${newPath.split('/').last}',
                          type: AppGlassNoticeType.success,
                        );
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          this.context,
                          _extractScriptError(error, '移动失败'),
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
      },
    );
  }

  Future<void> _showCopyDialog(ScriptFile file, ScriptState state) async {
    final folders = _scriptFolders(state.tree);
    final nameController = TextEditingController(text: file.name);
    String targetDir = _defaultScriptDirectory(file.path);

    await showDialog<void>(
      context: context,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: Text(file.isDirectory ? '复制文件夹' : '复制文件'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    '来源：${file.path}',
                    style: const TextStyle(
                      fontSize: 12,
                      color: AppColors.slate500,
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: nameController,
                    decoration: InputDecoration(
                      labelText: file.isDirectory ? '新文件夹名' : '新文件名',
                    ),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    initialValue: targetDir,
                    decoration: const InputDecoration(labelText: '目标目录'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('根目录')),
                      ...folders.map(
                        (folder) => DropdownMenuItem(
                          value: folder,
                          child: Text(folder),
                        ),
                      ),
                    ],
                    onChanged: (value) {
                      setDialogState(() => targetDir = value ?? '');
                    },
                  ),
                ],
              ),
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => navigator.pop(),
                    variant: AppLiquidGlassButtonVariant.secondary,
                  ),
                  AppGlassDialogAction(
                    label: '复制',
                    variant: AppLiquidGlassButtonVariant.primary,
                    onPressed: () async {
                      final newName = nameController.text.trim();
                      if (newName.isEmpty) {
                        AppGlassNotice.show(
                          this.context,
                          '名称不能为空',
                          type: AppGlassNoticeType.warning,
                        );
                        return;
                      }
                      try {
                        final newPath = await ref
                            .read(scriptProvider.notifier)
                            .copyPath(
                              file.path,
                              targetDir: targetDir,
                              newName: newName,
                            );
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        _showMessage(
                          '已复制到 ${newPath.split('/').last}',
                          type: AppGlassNoticeType.success,
                        );
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          this.context,
                          _extractScriptError(error, '复制失败'),
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
      },
    );
  }

  Future<void> _showVersionSheet(String path) async {
    if (!mounted) {
      return;
    }
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => _ScriptVersionSheet(path: path),
    );
  }

  Future<void> _showCreateFileDialog(
    ScriptState state, {
    String? initialParent,
  }) async {
    final nameController = TextEditingController();
    final folders = _scriptFolders(state.tree);
    String parent =
        initialParent ?? _defaultScriptDirectory(state.selectedPath);

    await showDialog<void>(
      context: context,
      useRootNavigator: true,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: const Text('新建脚本'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  TextField(
                    controller: nameController,
                    decoration: const InputDecoration(
                      labelText: '文件名',
                      hintText: '例如 demo.py / test.js / run.sh',
                    ),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    initialValue: parent,
                    decoration: const InputDecoration(labelText: '保存目录'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('根目录')),
                      ...folders.map(
                        (folder) => DropdownMenuItem(
                          value: folder,
                          child: Text(folder),
                        ),
                      ),
                    ],
                    onChanged: (value) {
                      setDialogState(() => parent = value ?? '');
                    },
                  ),
                ],
              ),
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => navigator.pop(),
                    variant: AppLiquidGlassButtonVariant.secondary,
                  ),
                  AppGlassDialogAction(
                    label: '创建',
                    variant: AppLiquidGlassButtonVariant.primary,
                    onPressed: () async {
                      final fileName = nameController.text.trim();
                      if (fileName.isEmpty) {
                        AppGlassNotice.show(
                          this.context,
                          '文件名不能为空',
                          type: AppGlassNoticeType.warning,
                        );
                        return;
                      }
                      final fullPath = _joinScriptPath(parent, fileName);
                      try {
                        await ref
                            .read(scriptProvider.notifier)
                            .createFile(fullPath);
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        await _openScript(fullPath);
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          this.context,
                          _extractRequestError(error, '创建脚本失败'),
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
      },
    );
  }

  Future<void> _showCreateDirectoryDialog(
    ScriptState state, {
    String? initialParent,
  }) async {
    final nameController = TextEditingController();
    final folders = _scriptFolders(state.tree);
    String parent =
        initialParent ?? _defaultScriptDirectory(state.selectedPath);

    await showDialog<void>(
      context: context,
      useRootNavigator: true,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: const Text('新建文件夹'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  TextField(
                    controller: nameController,
                    decoration: const InputDecoration(labelText: '文件夹名称'),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    initialValue: parent,
                    decoration: const InputDecoration(labelText: '上级目录'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('根目录')),
                      ...folders.map(
                        (folder) => DropdownMenuItem(
                          value: folder,
                          child: Text(folder),
                        ),
                      ),
                    ],
                    onChanged: (value) {
                      setDialogState(() => parent = value ?? '');
                    },
                  ),
                ],
              ),
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => navigator.pop(),
                    variant: AppLiquidGlassButtonVariant.secondary,
                  ),
                  AppGlassDialogAction(
                    label: '创建',
                    variant: AppLiquidGlassButtonVariant.primary,
                    onPressed: () async {
                      final name = nameController.text.trim();
                      if (name.isEmpty) {
                        AppGlassNotice.show(
                          this.context,
                          '文件夹名称不能为空',
                          type: AppGlassNoticeType.warning,
                        );
                        return;
                      }
                      try {
                        await ref
                            .read(scriptProvider.notifier)
                            .createDirectory(_joinScriptPath(parent, name));
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        _showMessage(
                          '文件夹创建成功',
                          type: AppGlassNoticeType.success,
                        );
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          this.context,
                          _extractRequestError(error, '创建文件夹失败'),
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
      },
    );
  }

  Future<void> _pickAndUploadFiles(
    ScriptState state, {
    String initialDir = '',
  }) async {
    final result = await FilePicker.platform.pickFiles(
      allowMultiple: true,
      withData: true,
    );
    if (result == null || result.files.isEmpty || !mounted) {
      return;
    }
    await _showUploadDialog(state, result.files, initialDir: initialDir);
  }

  Future<void> _showUploadDialog(
    ScriptState state,
    List<PlatformFile> files, {
    String initialDir = '',
  }) async {
    final folders = _scriptFolders(state.tree);
    String targetDir = initialDir.isNotEmpty
        ? initialDir
        : _defaultScriptDirectory(state.selectedPath);

    await showDialog<void>(
      context: context,
      useRootNavigator: true,
      builder: (dialogContext) {
        final navigator = Navigator.of(dialogContext);
        return StatefulBuilder(
          builder: (context, setDialogState) => AlertDialog(
            title: const Text('上传脚本'),
            content: SingleChildScrollView(
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    '已选择 ${files.length} 个文件',
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 8),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 160),
                    child: AppLiquidGlassSurface(
                      borderRadius: 12,
                      performanceMode: true,
                      child: ListView.separated(
                        shrinkWrap: true,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 12,
                          vertical: 8,
                        ),
                        itemCount: files.length,
                        separatorBuilder: (context, index) =>
                            const Divider(height: 12, thickness: 0.6),
                        itemBuilder: (_, index) => Text(
                          files[index].name,
                          style: const TextStyle(fontSize: 12),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 12),
                  DropdownButtonFormField<String>(
                    initialValue: targetDir,
                    decoration: const InputDecoration(labelText: '上传目录'),
                    items: [
                      const DropdownMenuItem(value: '', child: Text('根目录')),
                      ...folders.map(
                        (folder) => DropdownMenuItem(
                          value: folder,
                          child: Text(folder),
                        ),
                      ),
                    ],
                    onChanged: (value) {
                      setDialogState(() => targetDir = value ?? '');
                    },
                  ),
                ],
              ),
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => navigator.pop(),
                    variant: AppLiquidGlassButtonVariant.secondary,
                  ),
                  AppGlassDialogAction(
                    label: '上传',
                    variant: AppLiquidGlassButtonVariant.primary,
                    onPressed: () async {
                      try {
                        final paths = await ref
                            .read(scriptProvider.notifier)
                            .uploadFiles(files, dir: targetDir);
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        _showMessage(
                          paths.length > 1
                              ? '成功上传 ${paths.length} 个文件'
                              : '上传成功',
                          type: AppGlassNoticeType.success,
                        );
                        if (paths.length == 1) {
                          await _openScript(paths.first);
                          if (!mounted) {
                            return;
                          }
                          await _maybePromptAddToTask(paths.first);
                        }
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          this.context,
                          _extractScriptError(error, '上传失败'),
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
      },
    );
  }

  Widget _buildLoadError(String message) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(
            Icons.cloud_off_outlined,
            size: 56,
            color: AppColors.slate400,
          ),
          const SizedBox(height: 12),
          Text(message, textAlign: TextAlign.center),
          const SizedBox(height: 12),
          TextButton(
            onPressed: () => ref.read(scriptProvider.notifier).loadTree(),
            child: const Text('重试'),
          ),
        ],
      ),
    );
  }
}

class _ScriptInlineLoadError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _ScriptInlineLoadError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) => AppCard(
    margin: const EdgeInsets.only(bottom: 12),
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
    child: Row(
      children: [
        const Icon(Icons.sync_problem_outlined, color: AppColors.amber500),
        const SizedBox(width: 10),
        Expanded(child: Text(message, style: const TextStyle(fontSize: 13))),
        TextButton(onPressed: onRetry, child: const Text('重试')),
      ],
    ),
  );
}

class _FileTreeItem extends ConsumerStatefulWidget {
  final ScriptFile file;
  final bool isLight;
  final int depth;
  final ValueChanged<String> onTap;
  final ValueChanged<ScriptFile> onAction;

  const _FileTreeItem({
    required this.file,
    required this.isLight,
    required this.depth,
    required this.onTap,
    required this.onAction,
  });

  @override
  ConsumerState<_FileTreeItem> createState() => _FileTreeItemState();
}

class _FileTreeItemState extends ConsumerState<_FileTreeItem> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final file = widget.file;
    final indent = widget.depth * 16.0;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        GestureDetector(
          onLongPress: () => widget.onAction(file),
          onTap: () {
            if (file.isDirectory) {
              setState(() => _expanded = !_expanded);
            } else {
              widget.onTap(file.path);
            }
          },
          child: AppCard(
            stableForScrolling: true,
            margin: const EdgeInsets.only(bottom: 2),
            padding: EdgeInsets.only(
              left: 12 + indent,
              right: 8,
              top: 10,
              bottom: 10,
            ),
            child: Row(
              children: [
                Icon(
                  file.isDirectory
                      ? (_expanded ? Icons.folder_open : Icons.folder)
                      : Icons.description_outlined,
                  size: 18,
                  color: file.isDirectory
                      ? AppColors.amber500
                      : AppColors.slate400,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    file.name,
                    style: TextStyle(
                      fontSize: 13,
                      fontWeight: file.isDirectory
                          ? FontWeight.w600
                          : FontWeight.w400,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (file.isDirectory)
                  Icon(
                    _expanded ? Icons.expand_less : Icons.expand_more,
                    size: 18,
                    color: AppColors.slate400,
                  ),
                IconButton(
                  onPressed: () => widget.onAction(file),
                  icon: const Icon(Icons.more_vert, size: 18),
                  visualDensity: VisualDensity.compact,
                  color: AppColors.slate400,
                  splashRadius: 18,
                ),
              ],
            ),
          ),
        ),
        if (_expanded && file.isDirectory)
          ...file.children.map(
            (child) => _FileTreeItem(
              file: child,
              isLight: widget.isLight,
              depth: widget.depth + 1,
              onTap: widget.onTap,
              onAction: widget.onAction,
            ),
          ),
      ],
    );
  }
}

class ScriptViewPage extends ConsumerStatefulWidget {
  final String path;

  const ScriptViewPage({super.key, required this.path});

  @override
  ConsumerState<ScriptViewPage> createState() => _ScriptViewPageState();
}

class _ScriptViewPageState extends ConsumerState<ScriptViewPage> {
  late final TextEditingController _contentController;
  late final FocusNode _contentFocusNode;
  final ScrollController _contentScrollController = ScrollController();
  bool _editing = false;
  bool _debugRunning = false;
  String _lastSearchQuery = '';
  Color? _editorBackgroundColor;
  bool _searchHighlightActive = false;

  @override
  void initState() {
    super.initState();
    _contentController = TextEditingController();
    _contentFocusNode = FocusNode();
    Future.microtask(() async {
      await ref.read(scriptProvider.notifier).loadContent(widget.path);
      await _loadEditorAppearance();
    });
  }

  @override
  void dispose() {
    _contentScrollController.dispose();
    _contentController.dispose();
    _contentFocusNode.dispose();
    super.dispose();
  }

  void _showMessage(
    String message, {
    AppGlassNoticeType type = AppGlassNoticeType.info,
  }) {
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(context, message, type: type);
  }

  String _extractScriptError(dynamic error, String fallback) =>
      extractScriptSaveErrorMessage(error, fallback);

  TaskFormPrefill _taskPrefill() {
    final fileName = widget.path.split('/').last;
    final taskName = fileName.replaceFirst(RegExp(r'\.[^/.]+$'), '');
    return TaskFormPrefill(name: taskName, command: 'task ${widget.path}');
  }

  Future<void> _save() async {
    try {
      await ref
          .read(scriptProvider.notifier)
          .saveContent(widget.path, _contentController.text);
      if (!mounted) {
        return;
      }
      setState(() => _editing = false);
      _showMessage('保存成功', type: AppGlassNoticeType.success);
    } catch (error) {
      _showMessage(
        _extractScriptError(error, '保存失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _format() async {
    try {
      final formatted = await ref
          .read(scriptProvider.notifier)
          .formatContent(widget.path, _contentController.text);
      if (!mounted) {
        return;
      }
      _contentController.text = formatted;
      if (!_editing) {
        setState(() => _editing = true);
      }
      _showMessage('格式化完成', type: AppGlassNoticeType.success);
    } catch (error) {
      final message = error is StateError
          ? error.message
          : _extractRequestError(error, '格式化失败');
      _showMessage(message, type: AppGlassNoticeType.error);
    }
  }

  Future<void> _showVersions() async {
    if (!mounted) {
      return;
    }
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (context) => _ScriptVersionSheet(path: widget.path),
    );
  }

  Future<void> _debugRun() async {
    if (_debugRunning) {
      return;
    }

    final hasUnsavedEdits =
        _editing && _contentController.text != ref.read(scriptProvider).content;

    if (_editing && hasUnsavedEdits) {
      // 有未保存的编辑内容，使用 runCode 直接执行
      await _runCode();
      return;
    }

    if (_editing) {
      await _save();
    }

    setState(() => _debugRunning = true);
    try {
      final resp = await DioClient.instance.dio.post(
        ApiEndpoints.scriptsRun,
        data: {'path': widget.path},
      );
      final raw = resp.data;
      String? runId;
      if (raw is Map && raw['run_id'] != null) {
        runId = raw['run_id'].toString();
      } else if (raw is Map &&
          raw['data'] is Map &&
          raw['data']['run_id'] != null) {
        runId = raw['data']['run_id'].toString();
      }
      if (runId == null || runId.isEmpty) {
        throw StateError('调试任务已启动，但未返回运行 ID');
      }

      if (!mounted) {
        return;
      }
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (context) =>
            _ScriptDebugRunSheet(path: widget.path, runId: runId!),
      );
    } catch (error) {
      _showMessage(
        _extractScriptError(error, '调试运行失败'),
        type: AppGlassNoticeType.error,
      );
    } finally {
      if (mounted) {
        setState(() => _debugRunning = false);
      }
    }
  }

  Future<void> _runCode() async {
    setState(() => _debugRunning = true);
    try {
      final resp = await DioClient.instance.dio.post(
        ApiEndpoints.scriptsRunCode,
        data: {
          'path': widget.path,
          'code': _contentController.text,
          'language': switch (widget.path.split('.').last.toLowerCase()) {
            'js' || 'mjs' || 'cjs' => 'javascript',
            'ts' => 'typescript',
            'sh' || 'bash' => 'shell',
            _ => 'python',
          },
        },
      );
      final raw = resp.data;
      String? runId;
      if (raw is Map && raw['run_id'] != null) {
        runId = raw['run_id'].toString();
      } else if (raw is Map &&
          raw['data'] is Map &&
          raw['data']['run_id'] != null) {
        runId = raw['data']['run_id'].toString();
      }
      if (runId == null || runId.isEmpty) {
        throw StateError('调试任务已启动，但未返回运行 ID');
      }

      if (!mounted) {
        return;
      }
      await showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (context) =>
            _ScriptDebugRunSheet(path: widget.path, runId: runId!),
      );
    } catch (error) {
      _showMessage(
        _extractScriptError(error, '调试运行失败'),
        type: AppGlassNoticeType.error,
      );
    } finally {
      if (mounted) {
        setState(() => _debugRunning = false);
      }
    }
  }

  Future<void> _loadEditorAppearance() async {
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.panelSettings);
      final data = extractData(resp.data);
      if (data is! Map || !mounted) {
        return;
      }
      final payload = Map<String, dynamic>.from(data);
      setState(() {
        _editorBackgroundColor = _parseColorSetting(
          payload['editor_background_color']?.toString(),
        );
      });
    } catch (_) {
      // 背景色配置加载失败时回退到页面默认配色
    }
  }

  Color? _parseColorSetting(String? raw) {
    final text = raw?.trim() ?? '';
    if (text.isEmpty) {
      return null;
    }

    if (text.startsWith('#')) {
      final hex = text.substring(1);
      if (hex.length == 6) {
        final value = int.tryParse(hex, radix: 16);
        if (value != null) {
          return Color(0xFF000000 | value);
        }
      }
      if (hex.length == 8) {
        final value = int.tryParse(hex, radix: 16);
        if (value != null) {
          return Color(value);
        }
      }
    }

    final rgb = RegExp(
      r'^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*([0-9]*\.?[0-9]+))?\s*\)$',
      caseSensitive: false,
    ).firstMatch(text);
    if (rgb != null) {
      final r = int.tryParse(rgb.group(1) ?? '');
      final g = int.tryParse(rgb.group(2) ?? '');
      final b = int.tryParse(rgb.group(3) ?? '');
      final alphaText = rgb.group(4);
      if (r != null && g != null && b != null) {
        final opacity = alphaText == null
            ? 1.0
            : (double.tryParse(alphaText) ?? 1.0).clamp(0.0, 1.0);
        return Color.fromRGBO(
          r.clamp(0, 255),
          g.clamp(0, 255),
          b.clamp(0, 255),
          opacity,
        );
      }
    }

    return null;
  }

  bool _useLightForeground(Color background) =>
      background.computeLuminance() < 0.45;

  void _scrollToMatch(int index) {
    void scroll() {
      if (!_contentScrollController.hasClients) {
        return;
      }
      final prefix = _contentController.text.substring(0, index);
      final lineCount = '\n'.allMatches(prefix).length;
      const lineHeight = 13 * 1.5;
      final rawOffset = (lineCount * lineHeight) - (lineHeight * 2);
      final target = rawOffset.clamp(
        0.0,
        _contentScrollController.position.maxScrollExtent,
      );
      _contentScrollController.animateTo(
        target,
        duration: const Duration(milliseconds: 220),
        curve: Curves.easeOut,
      );
    }

    WidgetsBinding.instance.addPostFrameCallback((_) => scroll());
  }

  bool _findInContent(String rawQuery, {required bool forward}) {
    final query = rawQuery.trim();
    if (query.isEmpty) {
      _showMessage('请输入要查找的内容', type: AppGlassNoticeType.warning);
      return false;
    }

    final content = _contentController.text;
    if (content.isEmpty) {
      _showMessage('当前脚本暂无可搜索内容', type: AppGlassNoticeType.warning);
      return false;
    }

    final normalizedContent = content.toLowerCase();
    final normalizedQuery = query.toLowerCase();
    final selection = _contentController.selection;
    final sameQuery = _lastSearchQuery == query;
    int index = -1;

    if (forward) {
      final start = sameQuery && selection.isValid ? selection.end : 0;
      index = normalizedContent.indexOf(normalizedQuery, start);
      if (index == -1 && start > 0) {
        index = normalizedContent.indexOf(normalizedQuery);
      }
    } else {
      final fallbackStart = normalizedContent.length - 1;
      final start = sameQuery && selection.isValid
          ? (selection.start - 1).clamp(0, fallbackStart)
          : fallbackStart;
      index = normalizedContent.lastIndexOf(normalizedQuery, start);
      if (index == -1 && start < fallbackStart) {
        index = normalizedContent.lastIndexOf(normalizedQuery);
      }
    }

    if (index == -1) {
      _showMessage('未找到“$query”', type: AppGlassNoticeType.warning);
      return false;
    }

    _lastSearchQuery = query;
    _contentController.selection = TextSelection(
      baseOffset: index,
      extentOffset: index + query.length,
    );
    _contentFocusNode.requestFocus();
    _scrollToMatch(index);
    setState(() => _searchHighlightActive = true);
    Future.delayed(const Duration(seconds: 2), () {
      if (mounted) {
        setState(() => _searchHighlightActive = false);
      }
    });
    final prefix = _contentController.text.substring(0, index);
    final lineNumber = '\n'.allMatches(prefix).length + 1;
    _showMessage('已定位到第 $lineNumber 行');
    return true;
  }

  Future<void> _showFindSheet() async {
    if (ref.read(scriptProvider).isBinary) {
      _showMessage('当前文件暂不支持查找', type: AppGlassNoticeType.warning);
      return;
    }

    final controller = TextEditingController(text: _lastSearchQuery);
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (sheetContext) {
        final bottomInset = MediaQuery.of(sheetContext).viewInsets.bottom;
        return Padding(
          padding: EdgeInsets.fromLTRB(20, 0, 20, bottomInset + 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const Text(
                '查找代码',
                style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: controller,
                autofocus: true,
                textInputAction: TextInputAction.search,
                decoration: const InputDecoration(
                  hintText: '输入关键字，例如 send / token / class',
                  prefixIcon: Icon(Icons.search),
                ),
                onSubmitted: (value) => _findInContent(value, forward: true),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton.icon(
                      onPressed: () =>
                          _findInContent(controller.text, forward: false),
                      icon: const Icon(Icons.keyboard_arrow_up, size: 18),
                      label: const Text('上一个'),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: FilledButton.icon(
                      onPressed: () =>
                          _findInContent(controller.text, forward: true),
                      icon: const Icon(Icons.keyboard_arrow_down, size: 18),
                      label: const Text('下一个'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        );
      },
    );
  }

  Future<void> _handleAction(_ScriptViewerAction action) async {
    switch (action) {
      case _ScriptViewerAction.format:
        await _format();
        return;
      case _ScriptViewerAction.versions:
        await _showVersions();
        return;
      case _ScriptViewerAction.addToTask:
        if (!mounted) {
          return;
        }
        await context.push('/tasks/new', extra: _taskPrefill());
        return;
      case _ScriptViewerAction.debug:
        await _debugRun();
        return;
    }
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(scriptProvider);
    if (!_editing && !state.isBinary) {
      _contentController.text = state.content;
    }
    final isDark = Theme.of(context).brightness == Brightness.dark;
    final editorBackground =
        _editorBackgroundColor ??
        (isDark ? const Color(0xFF1E1E1E) : Colors.white);
    final editorForeground = _useLightForeground(editorBackground)
        ? const Color(0xFFD4D4D4)
        : AppColors.slate900;

    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: Text(widget.path.split('/').last),
        actions: [
          if (!state.isBinary)
            IconButton(
              onPressed: _showFindSheet,
              icon: const Icon(Icons.search),
              tooltip: '查找代码',
            ),
          if (!state.isBinary)
            PopupMenuButton<_ScriptViewerAction>(
              onSelected: _handleAction,
              itemBuilder: (context) => const [
                PopupMenuItem(
                  value: _ScriptViewerAction.addToTask,
                  child: ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.playlist_add_outlined),
                    title: Text('加入任务'),
                  ),
                ),
                PopupMenuItem(
                  value: _ScriptViewerAction.format,
                  child: ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.auto_fix_high_outlined),
                    title: Text('格式化'),
                  ),
                ),
                PopupMenuItem(
                  value: _ScriptViewerAction.versions,
                  child: ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.history_outlined),
                    title: Text('版本历史'),
                  ),
                ),
                PopupMenuItem(
                  value: _ScriptViewerAction.debug,
                  child: ListTile(
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(Icons.play_circle_outline),
                    title: Text('调试运行'),
                  ),
                ),
              ],
            ),
          if (!state.isBinary)
            IconButton(
              onPressed: _debugRunning ? null : _debugRun,
              icon: _debugRunning
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.play_arrow_rounded),
              tooltip: '调试运行',
            ),
          if (!state.isBinary)
            IconButton(
              onPressed: () => setState(() => _editing = !_editing),
              icon: Icon(
                _editing ? Icons.visibility_outlined : Icons.edit_outlined,
              ),
            ),
          if (_editing && !state.isBinary)
            IconButton(
              onPressed: state.saving ? null : _save,
              icon: state.saving
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.save_outlined),
            ),
        ],
      ),
      body: state.loadingContent
          ? const Center(
              child: CircularProgressIndicator(color: AppColors.primary),
            )
          : state.isBinary
          ? const Center(
              child: Padding(
                padding: EdgeInsets.symmetric(horizontal: 28),
                child: Text(
                  '当前文件为二进制内容，App 暂不支持预览和编辑。',
                  textAlign: TextAlign.center,
                ),
              ),
            )
          : Column(
              children: [
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
                  child: Text(
                    widget.path,
                    style: TextStyle(
                      fontSize: 12,
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
                  ),
                ),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        color: editorBackground,
                        borderRadius: BorderRadius.circular(12),
                        border: Border.all(
                          color:
                              Theme.of(context).brightness == Brightness.light
                              ? AppColors.slate200
                              : AppColors.slate800,
                        ),
                      ),
                      child: TextSelectionTheme(
                        data: TextSelectionThemeData(
                          selectionColor: _searchHighlightActive
                              ? AppColors.amber500.withAlpha(120)
                              : AppColors.primary.withAlpha(60),
                        ),
                        child: TextField(
                          controller: _contentController,
                          focusNode: _contentFocusNode,
                          scrollController: _contentScrollController,
                          readOnly: !_editing,
                          expands: true,
                          maxLines: null,
                          style: TextStyle(
                            fontSize: 13,
                            fontFamily: 'monospace',
                            height: 1.5,
                            color: editorForeground,
                          ),
                          cursorColor: editorForeground,
                          selectionHeightStyle: BoxHeightStyle.max,
                          decoration: const InputDecoration(
                            border: InputBorder.none,
                            contentPadding: EdgeInsets.all(14),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ],
            ),
    );
  }
}

class _ScriptVersionSheet extends ConsumerStatefulWidget {
  final String path;

  const _ScriptVersionSheet({required this.path});

  @override
  ConsumerState<_ScriptVersionSheet> createState() =>
      _ScriptVersionSheetState();
}

class _ScriptVersionSheetState extends ConsumerState<_ScriptVersionSheet> {
  bool _loading = true;
  List<ScriptVersionRecord> _versions = const [];

  @override
  void initState() {
    super.initState();
    Future.microtask(_loadVersions);
  }

  Future<void> _loadVersions() async {
    setState(() => _loading = true);
    try {
      final versions = await ref
          .read(scriptProvider.notifier)
          .listVersions(widget.path);
      if (!mounted) {
        return;
      }
      setState(() {
        _versions = versions;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) {
        return;
      }
      setState(() => _loading = false);
    }
  }

  Future<void> _rollback(ScriptVersionRecord version) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('回滚脚本'),
        content: Text('确定要回滚到 v${version.version} 吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogContext, false),
                variant: AppLiquidGlassButtonVariant.secondary,
              ),
              AppGlassDialogAction(
                label: '回滚',
                onPressed: () => Navigator.pop(dialogContext, true),
                variant: AppLiquidGlassButtonVariant.warning,
              ),
            ],
          ),
        ],
      ),
    );
    if (confirm != true) {
      return;
    }
    try {
      await ref
          .read(scriptProvider.notifier)
          .rollbackVersion(version.id, widget.path);
      if (!mounted) {
        return;
      }
      Navigator.pop(context);
      AppGlassNotice.show(
        context,
        '已回滚到 v${version.version}',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        _extractRequestError(error, '回滚失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return SafeArea(
      child: SizedBox(
        height: MediaQuery.of(context).size.height * 0.72,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '版本历史',
                style: theme.textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                widget.path,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 16),
              Expanded(
                child: _loading
                    ? const Center(
                        child: CircularProgressIndicator(
                          color: AppColors.primary,
                        ),
                      )
                    : _versions.isEmpty
                    ? const Center(
                        child: Text(
                          '暂无版本历史',
                          style: TextStyle(color: AppColors.slate400),
                        ),
                      )
                    : ListView.separated(
                        itemCount: _versions.length,
                        separatorBuilder: (_, index) =>
                            const SizedBox(height: 10),
                        itemBuilder: (_, index) {
                          final version = _versions[index];
                          final message = version.message.trim().isEmpty
                              ? 'v${version.version}'
                              : version.message;
                          return AppCard(
                            stableForScrolling: true,
                            borderRadius: 14,
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
                                        color: AppColors.primary.withAlpha(18),
                                        borderRadius: BorderRadius.circular(
                                          999,
                                        ),
                                      ),
                                      child: Text(
                                        'v${version.version}',
                                        style: const TextStyle(
                                          color: AppColors.primary,
                                          fontWeight: FontWeight.w700,
                                        ),
                                      ),
                                    ),
                                    const SizedBox(width: 10),
                                    Expanded(
                                      child: Text(
                                        message,
                                        style: const TextStyle(
                                          fontWeight: FontWeight.w600,
                                        ),
                                      ),
                                    ),
                                  ],
                                ),
                                const SizedBox(height: 8),
                                Text(
                                  version.createdAt != null
                                      ? formatTimeCn(version.createdAt)
                                      : '未知时间',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: theme.colorScheme.onSurfaceVariant,
                                  ),
                                ),
                                const SizedBox(height: 4),
                                Text(
                                  '内容长度：${version.contentLength} 字符',
                                  style: TextStyle(
                                    fontSize: 12,
                                    color: theme.colorScheme.onSurfaceVariant,
                                  ),
                                ),
                                const SizedBox(height: 12),
                                Align(
                                  alignment: Alignment.centerRight,
                                  child: OutlinedButton.icon(
                                    onPressed: () => _rollback(version),
                                    icon: const Icon(Icons.restore, size: 18),
                                    label: const Text('回滚到此版本'),
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

List<String> _scriptFolders(List<ScriptFile> tree) {
  final folders = <String>{};

  void visit(ScriptFile file) {
    if (file.isDirectory && file.path.isNotEmpty) {
      folders.add(file.path);
      for (final child in file.children) {
        visit(child);
      }
    }
  }

  for (final file in tree) {
    visit(file);
  }

  final values = folders.toList()..sort();
  return values;
}

String _defaultScriptDirectory(String? selectedPath) {
  final path = (selectedPath ?? '').trim();
  if (path.isEmpty) {
    return '';
  }
  final index = path.lastIndexOf('/');
  if (index <= 0) {
    return '';
  }
  return path.substring(0, index);
}

String _joinScriptPath(String dir, String name) {
  final folder = dir.trim();
  final leaf = name.trim();
  if (folder.isEmpty) {
    return leaf;
  }
  return '$folder/$leaf';
}

String? _detectFormatterLanguage(String path) {
  final ext = path.split('.').last.toLowerCase();
  switch (ext) {
    case 'py':
      return 'python';
    case 'sh':
    case 'bash':
      return 'shell';
    case 'go':
      return 'go';
    case 'json':
      return 'json';
    default:
      return null;
  }
}

String _extractRequestError(dynamic error, String fallback) =>
    extractScriptSaveErrorMessage(error, fallback);

class _ScriptDebugRunSheet extends StatefulWidget {
  final String path;
  final String runId;

  const _ScriptDebugRunSheet({required this.path, required this.runId});

  @override
  State<_ScriptDebugRunSheet> createState() => _ScriptDebugRunSheetState();
}

class _ScriptDebugRunSheetState extends State<_ScriptDebugRunSheet>
    with WidgetsBindingObserver {
  final ScrollController _scrollController = ScrollController();
  final List<String> _logs = [];
  bool _loading = true;
  bool _done = false;
  bool _autoScroll = true;
  String _statusText = '启动中...';
  String? _errorSummary;
  Timer? _pollTimer;
  bool _pollRequestRunning = false;
  bool _appResumed = true;
  int _cursor = 0;
  Color? _logBackgroundColor;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    Future.microtask(() async {
      final color = await loadPanelLogBackgroundColor();
      if (mounted) {
        setState(() => _logBackgroundColor = color);
      }
    });
    _startPolling();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _pollTimer?.cancel();
    _scrollController.dispose();
    super.dispose();
  }

  void _startPolling() {
    _pollTimer?.cancel();
    _loadLogs();
    _pollTimer = Timer.periodic(const Duration(seconds: 1), (_) => _loadLogs());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _appResumed = state == AppLifecycleState.resumed;
    if (_appResumed && !_done) {
      _startPolling();
    } else if (state == AppLifecycleState.paused ||
        state == AppLifecycleState.hidden ||
        state == AppLifecycleState.detached) {
      _pollTimer?.cancel();
      _pollTimer = null;
    }
  }

  Future<void> _loadLogs() async {
    if (_pollRequestRunning || !_appResumed) return;
    _pollRequestRunning = true;
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.scriptsRunLogs(widget.runId),
        queryParameters: {'cursor': _cursor},
      );
      final data = extractData(resp.data);
      if (data is! Map) {
        return;
      }

      final run = ScriptRunLogData.fromMap(data);

      if (!mounted || !_appResumed) {
        return;
      }

      setState(() {
        appendBoundedLogEntries(_logs, run.logs);
        _cursor = run.cursor;
        _done = run.done;
        _loading = false;
        _errorSummary = run.error;
        _statusText = run.statusText;
      });

      if (_autoScroll) {
        _scrollToBottom();
      }

      if (run.done) {
        _pollTimer?.cancel();
      }
    } catch (error) {
      if (!mounted || !_appResumed) {
        return;
      }
      setState(() {
        _loading = false;
        _statusText = extractScriptSaveErrorMessage(error, '日志读取失败');
      });
    } finally {
      _pollRequestRunning = false;
    }
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scrollController.hasClients) {
        _scrollController.animateTo(
          _scrollController.position.maxScrollExtent,
          duration: const Duration(milliseconds: 120),
          curve: Curves.easeOut,
        );
      }
    });
  }

  Future<void> _stopRun() async {
    try {
      await DioClient.instance.dio.put(
        ApiEndpoints.scriptsRunStop(widget.runId),
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _done = true;
        _statusText = '已停止';
      });
      _pollTimer?.cancel();
      await _loadLogs();
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractScriptSaveErrorMessage(error, '停止调试失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _clearRun() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('清除运行记录'),
        content: const Text('确定要清除此运行记录吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(ctx, false),
                variant: AppLiquidGlassButtonVariant.secondary,
              ),
              AppGlassDialogAction(
                label: '清除',
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
      await DioClient.instance.dio.delete(
        ApiEndpoints.scriptsRunClear(widget.runId),
      );
      if (!mounted) return;
      AppGlassNotice.show(context, '运行记录已清除', type: AppGlassNoticeType.success);
      Navigator.of(context).pop();
    } catch (error) {
      if (!mounted) return;
      AppGlassNotice.show(
        context,
        extractScriptSaveErrorMessage(error, '清除运行记录失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final logTheme = resolveLogSurfaceTheme(_logBackgroundColor);

    return SafeArea(
      child: SizedBox(
        height: MediaQuery.of(context).size.height * 0.8,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(20, 0, 20, 20),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '脚本调试',
                style: Theme.of(
                  context,
                ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 4),
              Text(
                widget.path,
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
              ),
              const SizedBox(height: 12),
              if (_errorSummary != null) ...[
                Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.errorContainer,
                    border: Border.all(
                      color: Theme.of(context).colorScheme.error,
                      width: 1.5,
                    ),
                    borderRadius: BorderRadius.circular(10),
                  ),
                  child: SelectableText(
                    '错误详情\n${_errorSummary!}',
                    style: TextStyle(
                      color: Theme.of(context).colorScheme.onErrorContainer,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                ),
                const SizedBox(height: 8),
              ],
              Row(
                children: [
                  Expanded(
                    child: Text(
                      _statusText,
                      style: const TextStyle(
                        fontSize: 13,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                  ),
                  IconButton(
                    onPressed: _logs.isEmpty
                        ? null
                        : () async {
                            await Clipboard.setData(
                              ClipboardData(text: _logs.join('\n')),
                            );
                            if (!mounted) return;
                            AppGlassNotice.show(
                              this.context,
                              '已复制调试日志',
                              type: AppGlassNoticeType.success,
                            );
                          },
                    icon: const Icon(Icons.copy_all_outlined),
                  ),
                  IconButton(
                    onPressed: () {
                      setState(() => _autoScroll = !_autoScroll);
                      if (_autoScroll) {
                        _scrollToBottom();
                      }
                    },
                    icon: Icon(
                      _autoScroll
                          ? Icons.vertical_align_bottom
                          : Icons.pause_circle_outline,
                    ),
                  ),
                  if (!_done)
                    IconButton(
                      onPressed: _stopRun,
                      icon: const Icon(Icons.stop_circle_outlined),
                    ),
                  if (_done)
                    IconButton(
                      onPressed: _clearRun,
                      icon: const Icon(Icons.delete_outline),
                      tooltip: '清除运行记录',
                    ),
                ],
              ),
              Expanded(
                child: Container(
                  width: double.infinity,
                  padding: const EdgeInsets.all(14),
                  decoration: BoxDecoration(
                    color: logTheme.background,
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: _loading
                      ? const Center(
                          child: CircularProgressIndicator(
                            color: AppColors.primary,
                          ),
                        )
                      : _logs.isEmpty
                      ? Center(
                          child: Text(
                            '等待调试输出...',
                            style: TextStyle(color: logTheme.mutedForeground),
                          ),
                        )
                      : Scrollbar(
                          controller: _scrollController,
                          child: SingleChildScrollView(
                            controller: _scrollController,
                            child: SelectionArea(
                              child: RichText(
                                text: AnsiTextParser.buildTextSpan(
                                  _logs.join('\n'),
                                  baseStyle: TextStyle(
                                    color: logTheme.foreground,
                                    fontFamily: 'monospace',
                                    fontSize: 12,
                                    height: 1.55,
                                  ),
                                  brightness: logTheme.brightness,
                                ),
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
    );
  }
}
