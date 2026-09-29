import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/network/sse_protocol.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/bounded_log_buffer.dart';
import '../../../shared/widgets/app_async_state.dart';
import '../../../shared/widgets/app_card.dart';

class AndroidRuntimePage extends StatefulWidget {
  const AndroidRuntimePage({super.key});

  @override
  State<AndroidRuntimePage> createState() => _AndroidRuntimePageState();
}

class _AndroidRuntimePageState extends State<AndroidRuntimePage> {
  Map<String, dynamic>? _data;
  final List<String> _logs = [];
  StreamSubscription<String>? _installSubscription;
  Completer<void>? _installCompleter;
  bool _loading = true;
  bool _busy = false;
  String? _error;
  Map<String, dynamic>? _rootfs;
  Map<String, dynamic>? _downloadStatus;
  String _distribution = 'ubuntu';
  String _sourceId = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _installSubscription?.cancel();
    final completer = _installCompleter;
    if (completer != null && !completer.isCompleted) completer.complete();
    super.dispose();
  }

  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.androidRuntimeStatus,
      );
      final data = extractData(response.data);
      final rootfsData = data is Map ? data['rootfs'] : null;
      PanelCapabilityRegistry.recordSupported(PanelCapability.androidRuntime);
      if (!mounted) return;
      setState(() {
        _data = data is Map ? Map<String, dynamic>.from(data) : const {};
        final rootfs = rootfsData is Map
            ? Map<String, dynamic>.from(rootfsData)
            : <String, dynamic>{};
        final selected = rootfs['selected_distribution']?.toString();
        final image = rootfs['image'];
        final selectedSource = image is Map
            ? image['selected_source']?.toString()
            : null;
        _rootfs = rootfs.isEmpty ? null : rootfs;
        _distribution = selected?.isNotEmpty == true ? selected! : 'ubuntu';
        _sourceId = selectedSource ?? '';
        _loading = false;
      });
    } catch (error) {
      PanelCapabilityRegistry.recordFailure(
        PanelCapability.androidRuntime,
        error,
      );
      if (mounted) {
        setState(() {
          _loading = false;
          _error = extractErrorMessage(error, '运行时状态加载失败');
        });
      }
    }
  }

  Future<void> _install(String name) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _logs.clear();
      _error = null;
    });
    String? terminalResult;
    String? currentEvent;
    final dataLines = <String>[];

    void emitEvent() {
      if (dataLines.isEmpty) {
        currentEvent = null;
        return;
      }
      final message = dataLines.join('\n').replaceAll(r'\n', '\n');
      if (currentEvent == 'done') {
        terminalResult = message.trim().toLowerCase();
      }
      if (mounted) {
        setState(() => appendBoundedLogEntries(_logs, [message]));
      }
      currentEvent = null;
      dataLines.clear();
    }

    try {
      final response = await DioClient.instance.dio.post<ResponseBody>(
        ApiEndpoints.androidRuntimeInstall,
        data: {'name': name},
        options: Options(responseType: ResponseType.stream),
      );
      if (!mounted) {
        final abandonedSubscription = response.data?.stream.listen((_) {});
        await abandonedSubscription?.cancel();
        return;
      }
      final lines = response.data!.stream
          .cast<List<int>>()
          .transform(utf8.decoder)
          .transform(const LineSplitter());
      final completer = Completer<void>();
      _installCompleter = completer;
      _installSubscription = lines.listen(
        (line) {
          if (line.isEmpty || line == '\r') {
            emitEvent();
            return;
          }
          final field = parseSseField(line);
          if (field?.name == 'event') {
            currentEvent = field!.value.trim();
          } else if (field?.name == 'data') {
            dataLines.add(field!.value);
          }
        },
        onError: (Object error, StackTrace stackTrace) {
          if (!completer.isCompleted) {
            completer.completeError(error, stackTrace);
          }
        },
        onDone: () {
          emitEvent();
          if (!completer.isCompleted) completer.complete();
        },
        cancelOnError: true,
      );
      await completer.future;
      if (terminalResult != 'installed' && terminalResult != 'finished') {
        throw StateError(
          terminalResult == null
              ? '运行时安装结果未确认，请查看日志'
              : '运行时安装失败（$terminalResult），请查看日志',
        );
      }
    } catch (error) {
      if (mounted) {
        setState(() => _error = extractErrorMessage(error, '运行时安装失败'));
      }
    } finally {
      _installSubscription = null;
      _installCompleter = null;
      if (mounted) {
        setState(() => _busy = false);
        await _load();
      }
    }
  }

  Future<void> _selectRootfs(String distribution, String sourceId) async {
    try {
      await DioClient.instance.dio.post(
        ApiEndpoints.androidRootfsDistribution,
        queryParameters: {'distribution': distribution},
      );
      await DioClient.instance.dio.post(
        ApiEndpoints.androidRootfsSource,
        queryParameters: {'distribution': distribution, 'source_id': sourceId},
      );
      await _load();
    } catch (error) {
      if (mounted) {
        setState(() => _error = extractErrorMessage(error, '保存 rootfs 配置失败'));
      }
    }
  }

  Future<void> _downloadRootfs() async {
    if (_busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await DioClient.instance.dio.post(
        ApiEndpoints.androidRootfsDownload,
        queryParameters: {'distribution': _distribution},
      );
      for (var i = 0; i < 360 && mounted; i++) {
        await Future<void>.delayed(const Duration(seconds: 1));
        final response = await DioClient.instance.dio.get(
          ApiEndpoints.androidRootfsDownloadStatus,
          queryParameters: {'distribution': _distribution},
        );
        final status = extractData(response.data);
        if (status is Map) {
          setState(() => _downloadStatus = Map<String, dynamic>.from(status));
          if (status['running'] != true) break;
        }
      }
      await _load();
    } catch (error) {
      if (mounted) {
        setState(() => _error = extractErrorMessage(error, 'rootfs 下载失败'));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _buildRootfsCard() {
    final rootfs = _rootfs;
    if (rootfs == null) return const SizedBox.shrink();
    final image = rootfs['image'] is Map
        ? Map<String, dynamic>.from(rootfs['image'] as Map)
        : const <String, dynamic>{};
    final sources = image['sources'] is List
        ? (image['sources'] as List)
              .whereType<Map>()
              .map((source) => Map<String, dynamic>.from(source))
              .toList()
        : const <Map<String, dynamic>>[];
    final installed = rootfs['installed'] == true;
    final running = _downloadStatus?['running'] == true;
    return AppCard(
      margin: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text(
            'Linux rootfs / PTY 环境',
            style: TextStyle(fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 6),
          Text(
            installed
                ? '已就绪：${rootfs['distribution'] ?? _distribution}'
                : '未就绪：终端和依赖安装需要先准备 rootfs',
          ),
          const SizedBox(height: 10),
          DropdownButtonFormField<String>(
            initialValue: _distribution,
            decoration: const InputDecoration(labelText: '发行版', isDense: true),
            items: const [
              DropdownMenuItem(value: 'ubuntu', child: Text('Ubuntu')),
              DropdownMenuItem(value: 'alpine', child: Text('Alpine')),
            ],
            onChanged: _busy
                ? null
                : (value) {
                    if (value != null) {
                      setState(() => _distribution = value);
                    }
                  },
          ),
          const SizedBox(height: 8),
          DropdownButtonFormField<String>(
            initialValue: sources.any((source) => source['id'] == _sourceId)
                ? _sourceId
                : null,
            decoration: const InputDecoration(labelText: '镜像源', isDense: true),
            items: sources
                .map(
                  (source) => DropdownMenuItem<String>(
                    value: source['id']?.toString(),
                    child: Text(
                      source['display_name']?.toString() ??
                          source['id'].toString(),
                    ),
                  ),
                )
                .toList(),
            onChanged: _busy
                ? null
                : (value) {
                    if (value != null) {
                      setState(() => _sourceId = value);
                      _selectRootfs(_distribution, value);
                    }
                  },
          ),
          const SizedBox(height: 10),
          Row(
            children: [
              Expanded(
                child: Text(
                  _downloadStatus?['message']?.toString() ??
                      '选择发行版和镜像源后下载 rootfs',
                  style: const TextStyle(fontSize: 12),
                ),
              ),
              FilledButton.icon(
                onPressed: _busy || installed ? null : _downloadRootfs,
                icon: const Icon(Icons.download),
                label: Text(running ? '下载中' : '下载 rootfs'),
              ),
            ],
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final supported = _data?['supported'] == true;
    final runtimes = _data?['runtimes'] is List
        ? _data!['runtimes'] as List
        : const [];
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: const Text('Android 运行时')),
      body: AppAsyncState(
        loading: _loading,
        error: _error,
        empty: false,
        emptyText: '',
        onRetry: _load,
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            _buildRootfsCard(),
            if (!supported)
              const AppCard(child: Text('当前面板不支持 Android/Magisk 运行时管理')),
            for (final runtime in runtimes.whereType<Map>())
              AppCard(
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  title: Text(runtime['name'].toString()),
                  subtitle: Text(
                    '${runtime['version'] ?? '未安装'}\n${runtime['path'] ?? ''}',
                  ),
                  trailing: _rootfs != null
                      ? (runtime['installed'] == true
                            ? const Icon(
                                Icons.check_circle,
                                color: Colors.green,
                              )
                            : const SizedBox.shrink())
                      : Wrap(
                          children: [
                            IconButton(
                              onPressed: _busy
                                  ? null
                                  : () => _install(runtime['name'].toString()),
                              icon: const AppIcon(Icons.download),
                            ),
                          ],
                        ),
                ),
              ),
            if (_logs.isNotEmpty)
              AppCard(
                child: SelectableText(
                  _logs.join('\n'),
                  style: const TextStyle(fontFamily: 'monospace'),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
