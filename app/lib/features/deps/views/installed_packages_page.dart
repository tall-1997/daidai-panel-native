import 'package:flutter/material.dart';
import 'package:dio/dio.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

class InstalledPackagesPage extends StatefulWidget {
  const InstalledPackagesPage({super.key});

  @override
  State<InstalledPackagesPage> createState() => _InstalledPackagesPageState();
}

class _InstalledPackagesPageState extends State<InstalledPackagesPage> {
  List<Map<String, dynamic>> _pip = const [];
  Map<String, dynamic> _npm = const {};
  bool _loading = true;
  String? _error;
  String _exported = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final responses = await Future.wait([
        DioClient.instance.dio.get(ApiEndpoints.depsPip),
        DioClient.instance.dio.get(ApiEndpoints.depsNpm),
      ]);
      final pip = extractData(responses[0].data);
      final npm = extractData(responses[1].data);
      if (!mounted) return;
      setState(() {
        _pip = pip is List
            ? pip
                  .whereType<Map>()
                  .map((e) => Map<String, dynamic>.from(e))
                  .toList()
            : const [];
        _npm = npm is Map && npm['dependencies'] is Map
            ? Map<String, dynamic>.from(npm['dependencies'] as Map)
            : const {};
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = extractErrorMessage(error, '依赖清单加载失败');
      });
    }
  }

  Future<void> _export(String type) async {
    final response = await DioClient.instance.dio.get(
      ApiEndpoints.depsExport,
      queryParameters: {'type': type},
      options: Options(responseType: ResponseType.plain),
    );
    if (mounted) setState(() => _exported = response.data?.toString() ?? '');
  }

  Future<void> _batchReinstall() async {
    final controller = TextEditingController();
    final ok = await showDialog<bool>(context: context, builder: (ctx) => AlertDialog(
      title: const Text('顺序批量重装'),
      content: TextField(controller: controller, decoration: const InputDecoration(labelText: '依赖 ID，逗号分隔')),
      actions: [AppLiquidGlassDialogActions(actions: [AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)), AppGlassDialogAction(label: '提交', onPressed: () => Navigator.pop(ctx, true))])],
    ));
    if (ok == true) {
      final ids = controller.text.split(',').map((e) => int.tryParse(e.trim())).whereType<int>().toList();
      if (ids.isNotEmpty) await DioClient.instance.dio.post(ApiEndpoints.depsBatchReinstall, data: {'ids': ids});
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: const Text('系统依赖清单')),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
          ? Center(
              child: AppCard(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.cloud_off_outlined, size: 42),
                    const SizedBox(height: 10),
                    Text(_error!, textAlign: TextAlign.center),
                    const SizedBox(height: 12),
                    AppLiquidGlassButton(label: '重试', onPressed: _load),
                  ],
                ),
              ),
            )
          : ListView(
              padding: const EdgeInsets.all(20),
              children: [
                Wrap(spacing: 8, children: [AppLiquidGlassButton(label: '导出 Python', onPressed: () => _export('python'), height: 42), AppLiquidGlassButton(label: '导出 Node', onPressed: () => _export('nodejs'), height: 42), AppLiquidGlassButton(label: '顺序重装', onPressed: _batchReinstall, height: 42)]),
                if (_exported.isNotEmpty) AppCard(child: SelectableText(_exported, style: const TextStyle(fontFamily: 'monospace'))),
                const Text('Python', style: TextStyle(fontWeight: FontWeight.bold)),
                ..._pip.map((item) => AppCard(
                      margin: const EdgeInsets.only(bottom: 6),
                      child: Text('${item['name']} ${item['version']}'),
                    )),
                const Text('Node.js', style: TextStyle(fontWeight: FontWeight.bold)),
                ..._npm.entries.map((entry) {
                  final value = entry.value;
                  final version = value is Map ? value['version'] : '';
                  return AppCard(
                    margin: const EdgeInsets.only(bottom: 6),
                    child: Text('${entry.key} $version'),
                  );
                }),
              ],
            ),
    );
  }
}
