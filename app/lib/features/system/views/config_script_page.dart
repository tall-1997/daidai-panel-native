import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_async_state.dart';
import '../../../shared/widgets/app_card.dart';

class ConfigScriptPage extends StatefulWidget {
  const ConfigScriptPage({super.key});

  @override
  State<ConfigScriptPage> createState() => _ConfigScriptPageState();
}

class _ConfigScriptPageState extends State<ConfigScriptPage> {
  final _controller = TextEditingController();
  String _saved = '';
  bool _loading = true;
  bool _saving = false;
  String? _error;

  bool get _dirty => _controller.text != _saved;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onChanged);
    _load();
  }

  void _onChanged() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    _controller.removeListener(_onChanged);
    _controller.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    if (_dirty) {
      final discard = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('放弃未保存修改'),
          content: const Text('刷新会覆盖当前编辑内容，是否继续？'),
          actions: [
            AppLiquidGlassDialogActions(actions: [
              AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)),
              AppGlassDialogAction(label: '继续', onPressed: () => Navigator.pop(ctx, true)),
            ]),
          ],
        ),
      );
      if (discard != true || !mounted) return;
    }
    setState(() { _loading = true; _error = null; });
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.configScript);
      final data = response.data as Map;
      if (!mounted) return;
      _saved = data['content']?.toString() ?? '';
      _controller.text = _saved;
      setState(() => _loading = false);
    } catch (error) {
      if (mounted) setState(() { _loading = false; _error = extractErrorMessage(error, '配置脚本加载失败'); });
    }
  }

  Future<void> _save() async {
    if (_saving || !_dirty) return;
    setState(() => _saving = true);
    try {
      await DioClient.instance.dio.put(
        ApiEndpoints.configScript,
        data: {'content': _controller.text},
      );
      if (!mounted) return;
      setState(() => _saved = _controller.text);
      AppGlassNotice.show(context, '配置文件已保存');
    } catch (error) {
      if (mounted) AppGlassNotice.show(context, extractErrorMessage(error, '保存失败'));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: const Text('高级配置脚本'),
        actions: [
          IconButton(onPressed: () => Clipboard.setData(ClipboardData(text: _controller.text)), icon: const Icon(Icons.copy)),
          IconButton(onPressed: _load, icon: const Icon(Icons.refresh)),
        ],
      ),
      body: AppAsyncState(
        loading: _loading,
        error: _error,
        empty: false,
        emptyText: '',
        onRetry: _load,
        child: Padding(
          padding: const EdgeInsets.all(20),
          child: Column(children: [
            Expanded(child: AppCard(child: TextField(controller: _controller, maxLines: null, expands: true, style: const TextStyle(fontFamily: 'monospace'), decoration: const InputDecoration(border: InputBorder.none)))),
            const SizedBox(height: 12),
            AppLiquidGlassButton(label: _saving ? '保存中' : _dirty ? '保存配置' : '已保存', onPressed: _saving || !_dirty ? null : _save),
          ]),
        ),
      ),
    );
  }
}
