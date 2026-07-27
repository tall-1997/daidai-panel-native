import 'package:flutter/material.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/widgets/app_card.dart';

class PlatformTokensPage extends StatefulWidget {
  const PlatformTokensPage({super.key});
  @override
  State<PlatformTokensPage> createState() => _PlatformTokensPageState();
}

class _PlatformTokensPageState extends State<PlatformTokensPage> {
  List<Map<String, dynamic>> _platforms = const [];
  List<Map<String, dynamic>> _tokens = const [];
  int? _platformId;
  bool _loading = true;

  @override
  void initState() { super.initState(); _load(); }

  Future<void> _load() async {
    final responses = await Future.wait([
      DioClient.instance.dio.get(ApiEndpoints.platforms),
      DioClient.instance.dio.get(ApiEndpoints.platformTokens, queryParameters: _platformId == null ? null : {'platform_id': _platformId}),
    ]);
    if (!mounted) return;
    setState(() {
      _platforms = _list(responses[0].data);
      _tokens = _list(responses[1].data);
      _loading = false;
    });
  }

  List<Map<String, dynamic>> _list(dynamic raw) {
    final data = raw is Map ? raw['data'] : raw;
    return data is List ? data.whereType<Map>().map((e) => Map<String, dynamic>.from(e)).toList() : const [];
  }

  Future<void> _editToken([Map<String, dynamic>? token]) async {
    final name = TextEditingController(text: token?['name']?.toString());
    final value = TextEditingController();
    final remarks = TextEditingController(text: token?['remarks']?.toString());
    int? platformId = token?['platform_id'] is num ? (token!['platform_id'] as num).toInt() : _platformId ?? (_platforms.firstOrNull?['id'] as num?)?.toInt();
    final ok = await showDialog<bool>(context: context, builder: (ctx) => StatefulBuilder(builder: (ctx, setDialogState) => AlertDialog(
      title: Text(token == null ? '新增平台令牌' : '编辑平台令牌'),
      content: Column(mainAxisSize: MainAxisSize.min, children: [
        DropdownButtonFormField<int>(initialValue: platformId, items: _platforms.map((p) => DropdownMenuItem(value: (p['id'] as num).toInt(), child: Text(p['label']?.toString() ?? p['name'].toString()))).toList(), onChanged: token == null ? (v) => setDialogState(() => platformId = v) : null),
        TextField(controller: name, decoration: const InputDecoration(labelText: '名称')),
        TextField(controller: value, obscureText: true, decoration: InputDecoration(labelText: token == null ? 'Token' : '新 Token（留空保持原值）')),
        TextField(controller: remarks, decoration: const InputDecoration(labelText: '备注')),
      ]),
      actions: [AppLiquidGlassDialogActions(actions: [AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)), AppGlassDialogAction(label: '保存', onPressed: () => Navigator.pop(ctx, true))])],
    )));
    if (ok != true || platformId == null || name.text.trim().isEmpty || (token == null && value.text.isEmpty)) return;
    if (token == null) {
      await DioClient.instance.dio.post(ApiEndpoints.platformTokens, data: {'platform_id': platformId, 'name': name.text.trim(), 'token': value.text, 'remarks': remarks.text.trim()});
    } else {
      await DioClient.instance.dio.put(ApiEndpoints.platformTokenById((token['id'] as num).toInt()), data: {'name': name.text.trim(), if (value.text.isNotEmpty) 'token': value.text, 'remarks': remarks.text.trim()});
    }
    await _load();
  }

  Future<void> _toggle(Map<String, dynamic> token) async { final id=(token['id'] as num).toInt(); await DioClient.instance.dio.put(token['enabled']==true?ApiEndpoints.platformTokenDisable(id):ApiEndpoints.platformTokenEnable(id)); await _load(); }
  Future<void> _remove(int id) async { await DioClient.instance.dio.delete(ApiEndpoints.platformTokenById(id)); await _load(); }

  @override
  Widget build(BuildContext context) => Scaffold(
    backgroundColor: Colors.transparent,
    appBar: AppBar(title: const Text('平台令牌'), actions: [AppGlassIconButton(icon: Icons.add, onTap: _loading ? null : () => _editToken())]),
    body: _loading ? const Center(child: CircularProgressIndicator()) : Column(children: [
      SizedBox(height: 48, child: ListView(scrollDirection: Axis.horizontal, padding: const EdgeInsets.symmetric(horizontal: 20), children: [AppLiquidGlassChoiceChip(label: '全部', selected: _platformId == null, onSelected: (_) { setState(() => _platformId = null); _load(); }), const SizedBox(width: 8), ..._platforms.map((p) => Padding(padding: const EdgeInsets.only(right: 8), child: AppLiquidGlassChoiceChip(label: p['label']?.toString() ?? p['name'].toString(), selected: _platformId == (p['id'] as num).toInt(), onSelected: (_) { setState(() => _platformId = (p['id'] as num).toInt()); _load(); })))])),
      Expanded(child: ListView(padding: const EdgeInsets.all(20), children: [for(final token in _tokens) AppCard(margin: const EdgeInsets.only(bottom: 8), child: ListTile(onTap: () => _editToken(token), title: Text(token['name']?.toString() ?? ''), subtitle: Text('${token['platform_name'] ?? ''} · ${token['remarks'] ?? ''}'), leading: Switch(value: token['enabled'] == true, onChanged: (_) => _toggle(token)), trailing: IconButton(icon: const Icon(Icons.delete_outline), onPressed: () => _remove((token['id'] as num).toInt()))))])),
    ]),
  );
}
