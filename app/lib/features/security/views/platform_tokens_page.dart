import 'package:flutter/material.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_async_state.dart';
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
  bool _mutating = false;
  String? _error;
  int _loadGeneration = 0;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_loadGeneration;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final responses = await Future.wait([
        DioClient.instance.dio.get(ApiEndpoints.platforms),
        DioClient.instance.dio.get(
          ApiEndpoints.platformTokens,
          queryParameters: _platformId == null
              ? null
              : {'platform_id': _platformId},
        ),
      ]);
      PanelCapabilityRegistry.recordSupported(PanelCapability.platformTokens);
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _platforms = _list(responses[0].data);
        _tokens = _list(responses[1].data);
      });
    } catch (error) {
      PanelCapabilityRegistry.recordFailure(PanelCapability.platformTokens, error);
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _error = extractErrorMessage(error, '平台令牌加载失败');
      });
    } finally {
      if (mounted && generation == _loadGeneration) {
        setState(() => _loading = false);
      }
    }
  }

  List<Map<String, dynamic>> _list(dynamic raw) {
    final data = raw is Map ? raw['data'] : raw;
    return data is List ? data.whereType<Map>().map((e) => Map<String, dynamic>.from(e)).toList() : const [];
  }

  Future<void> _editToken([Map<String, dynamic>? token]) async {
    final name = TextEditingController(text: token?['name']?.toString());
    final value = TextEditingController();
    final remarks = TextEditingController(text: token?['remarks']?.toString());
    try {
      int? platformId = token?['platform_id'] is num
          ? (token!['platform_id'] as num).toInt()
          : _platformId ??
                (_platforms.firstOrNull?['id'] as num?)?.toInt();
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => StatefulBuilder(
          builder: (ctx, setDialogState) => AlertDialog(
            title: Text(token == null ? '新增平台令牌' : '编辑平台令牌'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                DropdownButtonFormField<int>(
                  initialValue: platformId,
                  items: _platforms
                      .map(
                        (platform) => DropdownMenuItem(
                          value: (platform['id'] as num).toInt(),
                          child: Text(
                            platform['label']?.toString() ??
                                platform['name'].toString(),
                          ),
                        ),
                      )
                      .toList(),
                  onChanged: token == null
                      ? (selected) => setDialogState(
                          () => platformId = selected,
                        )
                      : null,
                ),
                TextField(
                  controller: name,
                  decoration: const InputDecoration(labelText: '名称'),
                ),
                TextField(
                  controller: value,
                  obscureText: true,
                  decoration: InputDecoration(
                    labelText: token == null
                        ? 'Token'
                        : '新 Token（留空保持原值）',
                  ),
                ),
                TextField(
                  controller: remarks,
                  decoration: const InputDecoration(labelText: '备注'),
                ),
              ],
            ),
            actions: [
              AppLiquidGlassDialogActions(
                actions: [
                  AppGlassDialogAction(
                    label: '取消',
                    onPressed: () => Navigator.pop(ctx, false),
                  ),
                  AppGlassDialogAction(
                    label: '保存',
                    onPressed: () => Navigator.pop(ctx, true),
                  ),
                ],
              ),
            ],
          ),
        ),
      );
      if (ok != true ||
          !mounted ||
          platformId == null ||
          name.text.trim().isEmpty ||
          (token == null && value.text.isEmpty)) {
        return;
      }
      final data = {
        'name': name.text.trim(),
        if (token == null) 'platform_id': platformId,
        if (token == null || value.text.isNotEmpty) 'token': value.text,
        'remarks': remarks.text.trim(),
      };
      await _mutate(
        () => token == null
            ? DioClient.instance.dio.post(
                ApiEndpoints.platformTokens,
                data: data,
              )
            : DioClient.instance.dio.put(
                ApiEndpoints.platformTokenById(
                  (token['id'] as num).toInt(),
                ),
                data: data,
              ),
        token == null ? '新增平台令牌失败' : '编辑平台令牌失败',
      );
    } finally {
      name.dispose();
      value.dispose();
      remarks.dispose();
    }
  }

  Future<void> _mutate(
    Future<dynamic> Function() mutation,
    String fallback,
  ) async {
    if (!mounted || _mutating) return;
    setState(() => _mutating = true);
    try {
      try {
        await mutation();
      } catch (error) {
        if (mounted) {
          AppGlassNotice.show(
            context,
            extractErrorMessage(error, fallback),
            type: AppGlassNoticeType.error,
          );
        }
        return;
      }
      if (mounted) await _load();
    } finally {
      if (mounted) setState(() => _mutating = false);
    }
  }

  Future<void> _toggle(Map<String, dynamic> token) async {
    final id = (token['id'] as num).toInt();
    await _mutate(
      () => DioClient.instance.dio.put(
        token['enabled'] == true
            ? ApiEndpoints.platformTokenDisable(id)
            : ApiEndpoints.platformTokenEnable(id),
      ),
      token['enabled'] == true ? '停用平台令牌失败' : '启用平台令牌失败',
    );
  }

  Future<void> _remove(int id) async {
    await _mutate(
      () => DioClient.instance.dio.delete(
        ApiEndpoints.platformTokenById(id),
      ),
      '删除平台令牌失败',
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: const Text('平台令牌'),
        actions: [
          AppGlassIconButton(
            icon: Icons.add,
            onTap: _loading || _mutating ? null : () => _editToken(),
          ),
        ],
      ),
      body: AppAsyncState(
        loading: _loading,
        error: _error,
        empty: false,
        emptyText: '暂无平台令牌',
        onRetry: _load,
        child: Column(
          children: [
            SizedBox(
              height: 48,
              child: ListView(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.symmetric(horizontal: 20),
                children: [
                  AppLiquidGlassChoiceChip(
                    label: '全部',
                    selected: _platformId == null,
                    onSelected: _mutating
                        ? null
                        : (_) {
                            setState(() => _platformId = null);
                            _load();
                          },
                  ),
                  const SizedBox(width: 8),
                  ..._platforms.map(
                    (platform) => Padding(
                      padding: const EdgeInsets.only(right: 8),
                      child: AppLiquidGlassChoiceChip(
                        label:
                            platform['label']?.toString() ??
                            platform['name'].toString(),
                        selected:
                            _platformId ==
                            (platform['id'] as num).toInt(),
                        onSelected: _mutating
                            ? null
                            : (_) {
                                setState(
                                  () => _platformId =
                                      (platform['id'] as num).toInt(),
                                );
                                _load();
                              },
                      ),
                    ),
                  ),
                ],
              ),
            ),
            Expanded(
              child: _tokens.isEmpty
                  ? Center(
                      child: Text(
                        '暂无平台令牌',
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        ),
                      ),
                    )
                  : ListView.builder(
                      padding: const EdgeInsets.all(20),
                      itemCount: _tokens.length,
                      itemBuilder: (context, index) {
                        final token = _tokens[index];
                        return AppCard(
                          margin: const EdgeInsets.only(bottom: 8),
                          child: ListTile(
                            onTap: _mutating
                                ? null
                                : () => _editToken(token),
                            title: Text(token['name']?.toString() ?? ''),
                            subtitle: Text(
                              '${token['platform_name'] ?? ''} · ${token['remarks'] ?? ''}',
                            ),
                            leading: Switch(
                              value: token['enabled'] == true,
                              onChanged: _mutating
                                  ? null
                                  : (_) => _toggle(token),
                            ),
                            trailing: IconButton(
                              icon: const Icon(Icons.delete_outline),
                              onPressed: _mutating
                                  ? null
                                  : () => _remove(
                                      (token['id'] as num).toInt(),
                                    ),
                            ),
                          ),
                        );
                      },
                    ),
            ),
          ],
        ),
      ),
    );
  }
}
