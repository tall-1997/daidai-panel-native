import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';
import 'env_list_page.dart';

class EnvToolsPage extends ConsumerStatefulWidget {
  const EnvToolsPage({super.key});

  @override
  ConsumerState<EnvToolsPage> createState() => _EnvToolsPageState();
}

class _EnvToolsPageState extends ConsumerState<EnvToolsPage> {
  final _ids = TextEditingController();
  final _name = TextEditingController();
  final _search = TextEditingController();
  final _replace = TextEditingController();
  String _output = '';
  bool _busy = false;

  List<int> get _parsedIds => _ids.text
      .split(',')
      .map((value) => int.tryParse(value.trim()))
      .whereType<int>()
      .toList();

  @override
  void dispose() {
    _ids.dispose();
    _name.dispose();
    _search.dispose();
    _replace.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action, String success) async {
    if (_busy || _parsedIds.isEmpty) return;
    setState(() => _busy = true);
    try {
      await action();
      await ref.read(envListProvider.notifier).load();
      if (mounted) AppGlassNotice.show(context, success);
    } catch (error) {
      if (mounted) {
        AppGlassNotice.show(
          context,
          extractErrorMessage(error, '操作失败'),
          type: AppGlassNoticeType.error,
        );
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _rename() => _run(() async {
    await DioClient.instance.dio.put(
      ApiEndpoints.envsBatchRename,
      data: {
        'ids': _parsedIds,
        if (_name.text.trim().isNotEmpty) 'name': _name.text.trim(),
        if (_name.text.trim().isEmpty) 'search': _search.text,
        if (_name.text.trim().isEmpty) 'replace': _replace.text,
      },
    );
  }, '环境变量已批量改名');

  Future<void> _pin(bool top) => _run(() async {
    for (final id in _parsedIds) {
      await DioClient.instance.dio.put(
        top ? ApiEndpoints.envMoveTop(id) : ApiEndpoints.envCancelTop(id),
      );
    }
  }, top ? '环境变量已置顶' : '环境变量已取消置顶');

  Future<void> _export() async {
    if (_busy || _parsedIds.isEmpty) return;
    setState(() => _busy = true);
    try {
      final response = await DioClient.instance.dio.post(
        ApiEndpoints.envsExportFiles,
        data: {'format': 'all', 'ids': _parsedIds},
      );
      if (mounted) {
        setState(() {
          _output = (response.data is Map
                  ? response.data['data']
                  : response.data)
              .toString();
        });
      }
    } catch (error) {
      if (mounted) AppGlassNotice.show(context, extractErrorMessage(error, '导出失败'));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: const Text('环境变量高级工具')),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          AppCard(
            child: Column(
              children: [
                TextField(controller: _ids, decoration: const InputDecoration(labelText: '变量 ID，逗号分隔')),
                TextField(controller: _name, decoration: const InputDecoration(labelText: '统一新名称')),
                TextField(controller: _search, decoration: const InputDecoration(labelText: '查找文本')),
                TextField(controller: _replace, decoration: const InputDecoration(labelText: '替换文本')),
                Wrap(spacing: 8, children: [
                  TextButton(onPressed: _busy ? null : _rename, child: const Text('批量改名')),
                  TextButton(onPressed: _busy ? null : () => _pin(true), child: const Text('置顶')),
                  TextButton(onPressed: _busy ? null : () => _pin(false), child: const Text('取消置顶')),
                  TextButton(onPressed: _busy ? null : _export, child: const Text('多格式导出')),
                ]),
              ],
            ),
          ),
          if (_output.isNotEmpty)
            AppCard(child: SelectableText(_output, style: const TextStyle(fontFamily: 'monospace'))),
        ],
      ),
    );
  }
}
