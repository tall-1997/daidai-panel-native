import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
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
    super.dispose();
  }

  Future<void> _load() async {
    setState(() { _loading = true; _error = null; });
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.androidRuntimeStatus);
      if (!mounted) return;
      setState(() { _data = Map<String, dynamic>.from(response.data as Map); _loading = false; });
    } catch (error) {
      if (mounted) setState(() { _loading = false; _error = extractErrorMessage(error, '运行时状态加载失败'); });
    }
  }

  Future<void> _install(String name) async {
    if (_busy) return;
    setState(() { _busy = true; _logs.clear(); _error = null; });
    var failed = false;
    try {
      final response = await DioClient.instance.dio.post<ResponseBody>(
        ApiEndpoints.androidRuntimeInstall,
        data: {'name': name},
        options: Options(responseType: ResponseType.stream),
      );
      final lines = response.data!.stream
          .cast<List<int>>()
          .transform(utf8.decoder)
          .transform(const LineSplitter());
      final completer = Completer<void>();
      _installSubscription = lines.listen((line) {
        if (!line.startsWith('data: ')) return;
        final message = line.substring(6).replaceAll(r'\n', '\n');
        if (message.startsWith('❌')) failed = true;
        if (mounted) setState(() => _logs.add(message));
      }, onError: completer.completeError, onDone: completer.complete);
      await completer.future;
      if (failed) throw StateError('运行时安装失败，请查看日志');
    } catch (error) {
      if (mounted) setState(() => _error = extractErrorMessage(error, '运行时安装失败'));
    } finally {
      if (mounted) { setState(() => _busy = false); await _load(); }
    }
  }

  Future<void> _uninstall(String name) async {
    try {
      await DioClient.instance.dio.post(ApiEndpoints.androidRuntimeUninstall, data: {'name': name});
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
