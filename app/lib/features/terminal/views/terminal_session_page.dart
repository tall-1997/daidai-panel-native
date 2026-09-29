import 'dart:async';
import 'dart:convert';
import 'package:flutter/material.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

class TerminalSessionPage extends StatefulWidget {
  const TerminalSessionPage({super.key});

  @override
  State<TerminalSessionPage> createState() => _TerminalSessionPageState();
}

class _TerminalSessionPageState extends State<TerminalSessionPage> {
  final _inputController = TextEditingController();
  final _outputController = ScrollController();
  Timer? _pollTimer;
  Map<String, dynamic>? _session;
  String _output = '';
  int _cursor = 0;
  int? _lastRows;
  int? _lastColumns;
  bool _loading = true;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _create();
  }

  Future<void> _create() async {
    final previousId = _session?['id']?.toString();
    _pollTimer?.cancel();
    if (previousId != null && previousId.isNotEmpty) {
      try {
        await DioClient.instance.dio.delete(
          ApiEndpoints.terminalSession(previousId),
        );
      } catch (_) {
        // 旧会话可能已由后端清理，不阻止创建新会话。
      }
    }
    setState(() {
      _loading = true;
      _busy = true;
      _error = null;
      _session = null;
      _cursor = 0;
      _output = '';
    });
    try {
      final response = await DioClient.instance.dio.post(
        ApiEndpoints.terminalSessions,
        data: const {'rows': 24, 'columns': 80},
      );
      final data = extractData(response.data);
      if (data is! Map) throw StateError('终端会话响应无效');
      if (!mounted) return;
      setState(() {
        _session = Map<String, dynamic>.from(data);
        _loading = false;
        _busy = false;
      });
      _schedulePoll();
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _busy = false;
        _error = extractErrorMessage(error, '终端不可用，请先准备 Linux rootfs 和 PTY 环境');
      });
    }
  }

  void _schedulePoll() {
    _pollTimer?.cancel();
    _pollTimer = Timer(const Duration(milliseconds: 180), _poll);
  }

  Future<void> _poll() async {
    final id = _session?['id']?.toString();
    if (!mounted || id == null || id.isEmpty) return;
    try {
      final response = await DioClient.instance.dio.get(
        ApiEndpoints.terminalSession(id),
        queryParameters: {'cursor': _cursor},
      );
      final data = extractData(response.data);
      if (data is Map && mounted) {
        final chunks = data['output'];
        if (chunks is List) {
          for (final raw in chunks.whereType<Map>()) {
            final cursor = int.tryParse(raw['cursor']?.toString() ?? '');
            if (cursor != null && cursor > _cursor) _cursor = cursor;
            final encoded = raw['data']?.toString() ?? '';
            if (encoded.isEmpty) continue;
            final bytes = base64.decode(encoded);
            _output += utf8.decode(bytes, allowMalformed: true);
            if (_output.length > 1000000) {
              _output = _output.substring(_output.length - 1000000);
            }
          }
        }
        setState(() {
          _session = Map<String, dynamic>.from(data);
        });
        WidgetsBinding.instance.addPostFrameCallback((_) {
          if (_outputController.hasClients) {
            _outputController.jumpTo(
              _outputController.position.maxScrollExtent,
            );
          }
        });
        if (data['status']?.toString() == 'running') _schedulePoll();
      }
    } catch (_) {
      if (mounted && _session?['status']?.toString() == 'running') {
        _schedulePoll();
      }
    }
  }

  Future<void> _sendInput(String value) async {
    final id = _session?['id']?.toString();
    if (id == null || value.isEmpty || _busy) return;
    setState(() => _busy = true);
    try {
      await DioClient.instance.dio.post(
        ApiEndpoints.terminalInput(id),
        data: {'data': value, 'encoding': 'utf8'},
      );
      _inputController.clear();
      _schedulePoll();
    } catch (error) {
      if (mounted) {
        setState(() => _error = extractErrorMessage(error, '发送终端输入失败'));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _stop() async {
    final id = _session?['id']?.toString();
    if (id == null) return;
    try {
      await DioClient.instance.dio.put(ApiEndpoints.terminalStop(id));
      await _poll();
    } catch (error) {
      if (mounted) {
        setState(() => _error = extractErrorMessage(error, '停止终端失败'));
      }
    }
  }

  Future<void> _resize(Size size) async {
    final id = _session?['id']?.toString();
    if (id == null) return;
    final columns = (size.width / 8).round().clamp(10, 400);
    final rows = (size.height / 18).round().clamp(2, 200);
    if (rows == _lastRows && columns == _lastColumns) return;
    _lastRows = rows;
    _lastColumns = columns;
    try {
      await DioClient.instance.dio.put(
        ApiEndpoints.terminalResize(id),
        data: {'rows': rows, 'columns': columns},
      );
    } catch (_) {}
  }

  @override
  void dispose() {
    _pollTimer?.cancel();
    final id = _session?['id']?.toString();
    if (id != null) {
      DioClient.instance.dio.delete(ApiEndpoints.terminalSession(id)).ignore();
    }
    _inputController.dispose();
    _outputController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final status =
        _session?['status']?.toString() ?? (_loading ? '连接中' : '未连接');
    final running = status == 'running';
    return Scaffold(
      appBar: AppBar(
        title: const Text('Android 交互终端'),
        actions: [
          IconButton(
            onPressed: _busy ? null : _create,
            icon: const Icon(Icons.refresh),
            tooltip: '新建会话',
          ),
          IconButton(
            onPressed: running ? _stop : null,
            icon: const Icon(Icons.stop_circle_outlined),
            tooltip: '停止',
          ),
        ],
      ),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Align(
              alignment: Alignment.centerLeft,
              child: Text('PTY 状态：$status'),
            ),
            if (_error != null) ...[
              const SizedBox(height: 8),
              AppCard(
                child: Text(_error!, style: const TextStyle(color: Colors.red)),
              ),
            ],
            const SizedBox(height: 8),
            Expanded(
              child: LayoutBuilder(
                builder: (context, constraints) {
                  WidgetsBinding.instance.addPostFrameCallback(
                    (_) => _resize(constraints.biggest),
                  );
                  return Container(
                    width: double.infinity,
                    decoration: BoxDecoration(
                      color: const Color(0xff101a18),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    padding: const EdgeInsets.all(12),
                    child: SingleChildScrollView(
                      controller: _outputController,
                      child: SelectableText(
                        _output.isEmpty ? '等待终端输出…' : _output,
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          color: Color(0xffd1fae5),
                          fontSize: 13,
                        ),
                      ),
                    ),
                  );
                },
              ),
            ),
            const SizedBox(height: 10),
            Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _inputController,
                    enabled: running && !_busy,
                    maxLines: 3,
                    minLines: 1,
                    onSubmitted: (value) => _sendInput('$value\n'),
                    decoration: const InputDecoration(hintText: '输入命令，回车发送'),
                  ),
                ),
                const SizedBox(width: 8),
                IconButton(
                  onPressed: running ? () => _sendInput('\u0003') : null,
                  icon: const Icon(Icons.cancel_outlined),
                  tooltip: 'Ctrl-C',
                ),
                IconButton(
                  onPressed: running
                      ? () => _sendInput('${_inputController.text}\n')
                      : null,
                  icon: const Icon(Icons.send),
                  tooltip: '发送',
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
