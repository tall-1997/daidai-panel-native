import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

class HealthCheckPage extends StatefulWidget {
  const HealthCheckPage({super.key});

  @override
  State<HealthCheckPage> createState() => _HealthCheckPageState();
}

class _HealthCheckPageState extends State<HealthCheckPage> {
  List<Map<String, dynamic>> _items = const [];
  String? _checkedAt;
  String? _error;
  bool _loading = true;

  @override
  void initState() { super.initState(); _load(); }

  Future<void> _load({bool run = false}) async {
    setState(() { _loading = true; _error = null; });
    try {
      final response = run
          ? await DioClient.instance.dio.post(ApiEndpoints.systemHealthCheck)
          : await DioClient.instance.dio.get(ApiEndpoints.systemHealthCheck);
      final data = extractData(response.data);
      PanelCapabilityRegistry.recordSupported(PanelCapability.healthCheck);
      final map = data is Map ? Map<String, dynamic>.from(data) : <String, dynamic>{};
      if (!mounted) return;
      setState(() {
        _items = map['items'] is List
            ? (map['items'] as List).whereType<Map>().map((e) => Map<String, dynamic>.from(e)).toList()
            : const [];
        _checkedAt = map['last_checked_at']?.toString();
        _loading = false;
      });
    } catch (error) {
      PanelCapabilityRegistry.recordFailure(PanelCapability.healthCheck, error);
      if (mounted) setState(() { _loading = false; _error = extractErrorMessage(error, '健康检查加载失败'); });
    }
  }

  String _label(String name) => const {'database':'数据库','memory':'内存','scheduler':'调度器','network':'网络'}[name] ?? name;

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: Colors.transparent,
    appBar: AppBar(title: const Text('系统健康诊断'), leading: IconButton(onPressed: context.pop, icon: const Icon(Icons.arrow_back_ios))),
    body: _loading ? const Center(child: CircularProgressIndicator()) : _error != null
        ? Center(child: Column(mainAxisSize: MainAxisSize.min, children: [Text(_error!), const SizedBox(height: 12), AppLiquidGlassButton(label: '重试', onPressed: _load)]))
        : ListView(padding: const EdgeInsets.all(20), children: [
            if (_checkedAt != null) Text('上次检查：$_checkedAt', style: TextStyle(color: Theme.of(context).colorScheme.onSurfaceVariant)),
            const SizedBox(height: 12),
            if (_items.isEmpty) const AppCard(child: Center(child: Text('尚未执行健康检查'))),
            ..._items.map((item) {
              final status = item['status']?.toString() ?? 'unknown';
              final color = status == 'ok' ? AppColors.primary : status == 'warning' ? AppColors.amber500 : AppColors.red500;
              return AppCard(margin: const EdgeInsets.only(bottom: 10), child: Row(children: [Icon(status == 'ok' ? Icons.check_circle : Icons.warning_amber, color: color), const SizedBox(width: 12), Expanded(child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [Text(_label(item['name']?.toString() ?? ''), style: const TextStyle(fontWeight: FontWeight.w700)), if ((item['message']?.toString() ?? '').isNotEmpty) Text(item['message'].toString())]))]));
            }),
            const SizedBox(height: 12),
            AppLiquidGlassButton(label: '立即检查', icon: Icons.health_and_safety_outlined, onPressed: () => _load(run: true)),
          ]),
  );
}
