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
    setState(() { _loading = true; _error = null; });
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.androidRuntimeStatus);
      final data = extractData(response.data);
      PanelCapabilityRegistry.recordSupported(PanelCapability.androidRuntime);
      if (!mounted) return;
      setState(() {
        _data = data is Map ? Map<String, dynamic>.from(data) : const {};
        _loading = false;
      });
    } catch (error) {
      PanelCapabilityRegistry.recordFailure(PanelCapability.androidRuntime, error);
      if (mounted) setState(() { _loading = false; _error = extractErrorMessage(error, '运行时状态加载失败'); });
    }
  }

  Future<void> _install(String name) async {
    if (_busy) return;
    setState(() { _busy = true; _logs.clear(); _error = null; });
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
      _installSubscription = lines.listen((line) {
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
      }, onError: (Object error, StackTrace stackTrace) {
        if (!completer.isCompleted) completer.completeError(error, stackTrace);
      }, onDone: () {
        emitEvent();
        if (!completer.isCompleted) completer.complete();
      }, cancelOnError: true);
      await completer.future;
      if (terminalResult != 'installed' && terminalResult != 'finished') {
        throw StateError(
          terminalResult == null
              ? '运行时安装结果未确认，请查看日志'
              : '运行时安装失败（$terminalResult），请查看日志',
        );
      }
    } catch (error) {
      if (mounted) setState(() => _error = extractErrorMessage(error, '运行时安装失败'));
    } finally {
      _installSubscription = null;
      _installCompleter = null;
      if (mounted) { setState(() => _busy = false); await _load(); }
    }
  }

  Future<void> _uninstall(String name) async {
    try {
      await DioClient.instance.dio.post(ApiEndpoints.androidRuntimeUninstall, data: {'name': name});
      if (!mounted) return;
      await _load();
    } catch (error) {
      if (mounted) setState(() => _error = extractErrorMessage(error, '卸载失败'));
    }
  }

  @override
  Widget build(BuildContext context) {
    final supported = _data?['supported'] == true;
    final runtimes = _data?['runtimes'] is List ? _data!['runtimes'] as List : const [];
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
            if (!supported) const AppCard(child: Text('当前面板不支持 Android/Magisk 运行时管理')),
            for (final runtime in runtimes.whereType<Map>())
              AppCard(
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  title: Text(runtime['name'].toString()),
                  subtitle: Text('${runtime['version'] ?? '未安装'}\n${runtime['path'] ?? ''}'),
                  trailing: Wrap(children: [
                    IconButton(onPressed: _busy ? null : () => _install(runtime['name'].toString()), icon: const Icon(Icons.download)),
                    IconButton(onPressed: runtime['installed'] == true ? () => _uninstall(runtime['name'].toString()) : null, icon: const Icon(Icons.delete_outline)),
                  ]),
                ),
              ),
            if (_logs.isNotEmpty) AppCard(child: SelectableText(_logs.join('\n'), style: const TextStyle(fontFamily: 'monospace'))),
          ],
        ),
      ),
    );
  }
}
