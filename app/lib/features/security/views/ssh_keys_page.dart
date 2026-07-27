import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

class SshKeysPage extends ConsumerStatefulWidget {
  const SshKeysPage({super.key});

  @override
  ConsumerState<SshKeysPage> createState() => _SshKeysPageState();
}

class _SshKeysPageState extends ConsumerState<SshKeysPage> {
  List<Map<String, dynamic>> _keys = [];
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.sshKeys);
      final data = extractData(resp.data);
      setState(() {
        _keys = (data is List)
            ? data.whereType<Map<String, dynamic>>().toList()
            : [];
        _loading = false;
      });
    } catch (_) {
      setState(() => _loading = false);
    }
  }

  Future<void> _deleteKey(Map<String, dynamic> key) async {
    final id = (key['id'] as num?)?.toInt();
    if (id == null) return;

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除 SSH 密钥'),
        content: Text('确定要删除密钥「${key['name'] ?? ''}」吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(ctx, false),
              ),
              AppGlassDialogAction(
                label: '删除',
                variant: AppLiquidGlassButtonVariant.danger,
                onPressed: () => Navigator.pop(ctx, true),
              ),
            ],
          ),
        ],
      ),
    );
    if (confirmed != true) return;

    try {
      await DioClient.instance.dio.delete(ApiEndpoints.sshKeyById(id));
      await _load();
      if (!mounted) return;
      AppGlassNotice.show(
        context,
        'SSH 密钥已删除',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) return;
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '删除 SSH 密钥失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  void _showAddDialog() {
    final nameC = TextEditingController();
    final privateKeyC = TextEditingController();
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('添加 SSH 密钥'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: nameC,
                decoration: const InputDecoration(labelText: '密钥名称'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: privateKeyC,
                maxLines: 6,
                decoration: const InputDecoration(
                  labelText: '私钥内容',
                  hintText: '粘贴 SSH 私钥',
                  alignLabelWithHint: true,
                ),
              ),
            ],
          ),
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(ctx),
              ),
              AppGlassDialogAction(
                label: '添加',
                variant: AppLiquidGlassButtonVariant.primary,
                onPressed: () async {
                  if (nameC.text.trim().isEmpty) return;
                  if (privateKeyC.text.trim().isEmpty) return;
                  try {
                    await DioClient.instance.dio.post(
                      ApiEndpoints.sshKeys,
                      data: {
                        'name': nameC.text.trim(),
                        'private_key': privateKeyC.text,
                      },
                    );
                    if (!mounted || !ctx.mounted) return;
                    Navigator.of(ctx).pop();
                    await _load();
                    if (!mounted) return;
                    AppGlassNotice.show(
                      context,
                      'SSH 密钥已添加',
                      type: AppGlassNoticeType.success,
                    );
                  } catch (error) {
                    if (!mounted) return;
                    AppGlassNotice.show(
                      context,
                      extractErrorMessage(error, '添加 SSH 密钥失败'),
                      type: AppGlassNoticeType.error,
                    );
                  }
                },
              ),
            ],
          ),
        ],
      ),
    );
  }

  void _showViewDialog(Map<String, dynamic> key) {
    final publicKey = key['public_key']?.toString() ?? '';
    final fingerprint = key['fingerprint']?.toString() ?? '';
    showDialog(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(key['name']?.toString() ?? 'SSH 密钥'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (fingerprint.isNotEmpty) ...[
                const Text('指纹:', style: TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 4),
                SelectableText(
                  fingerprint,
                  style: const TextStyle(fontFamily: 'monospace', fontSize: 11),
                ),
                const SizedBox(height: 12),
              ],
              if (publicKey.isNotEmpty) ...[
                Row(
                  children: [
                    const Text('公钥:', style: TextStyle(fontWeight: FontWeight.w600)),
                    const Spacer(),
                    GestureDetector(
                      onTap: () {
                        Clipboard.setData(ClipboardData(text: publicKey));
                        AppGlassNotice.show(
                          ctx,
                          '公钥已复制',
                          type: AppGlassNoticeType.success,
                        );
                      },
                      child: const Icon(Icons.copy, size: 16, color: AppColors.primary),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                SizedBox(
                  width: double.infinity,
                  child: AppLiquidGlassSurface(
                    padding: const EdgeInsets.all(8),
                    borderRadius: 6,
                    performanceMode: true,
                    child: SelectableText(
                    publicKey,
                    style: const TextStyle(fontFamily: 'monospace', fontSize: 10),
                    ),
                  ),
                ),
              ],
            ],
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx),
            child: const Text('关闭'),
          ),
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: Padding(
        padding: EdgeInsets.only(top: MediaQuery.of(context).padding.top + 12),
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: Row(
                children: [
                  GestureDetector(
                    onTap: () => Navigator.of(context).pop(),
                    child: const Icon(Icons.arrow_back_ios, size: 20),
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      'SSH 密钥',
                      style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
                    ),
                  ),
                  IconButton(
                    onPressed: _showAddDialog,
                    icon: const Icon(Icons.add, size: 22),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: _load,
                child: _loading
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: const [
                          SizedBox(height: 120),
                          Center(
                            child: CircularProgressIndicator(
                              color: AppColors.primary,
                            ),
                          ),
                        ],
                      )
                    : _keys.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          Padding(
                            padding: const EdgeInsets.fromLTRB(20, 8, 20, 4),
                            child: SizedBox(
                              width: double.infinity,
                              height: 36,
                              child: FilledButton.icon(
                                onPressed: _showAddDialog,
                                icon: const Icon(Icons.add, size: 16),
                                label: const Text(
                                  '添加 SSH 密钥',
                                  style: TextStyle(fontSize: 12),
                                ),
                              ),
                            ),
                          ),
                          const SizedBox(height: 80),
                          const Center(
                            child: Text(
                              '暂无 SSH 密钥',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
                        itemCount: _keys.length + 1,
                        itemBuilder: (_, i) {
                          if (i == 0) {
                            return Padding(
                              padding: const EdgeInsets.only(bottom: 8),
                              child: SizedBox(
                                width: double.infinity,
                                height: 36,
                                child: FilledButton.icon(
                                  onPressed: _showAddDialog,
                                  icon: const Icon(Icons.add, size: 16),
                                  label: const Text(
                                    '添加 SSH 密钥',
                                    style: TextStyle(fontSize: 12),
                                  ),
                                ),
                              ),
                            );
                          }
                          final key = _keys[i - 1];
                          final createdAt = DateTime.tryParse(
                            key['created_at']?.toString() ?? '',
                          );
                          return AppCard(
                            stableForScrolling: true,
                            margin: const EdgeInsets.only(bottom: 8),
                            padding: const EdgeInsets.symmetric(
                              horizontal: 14,
                              vertical: 12,
                            ),
                            child: Row(
                              children: [
                                const Icon(
                                  Icons.vpn_key_outlined,
                                  size: 18,
                                  color: AppColors.primary,
                                ),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        key['name']?.toString() ?? '',
                                        style: const TextStyle(
                                          fontSize: 13,
                                          fontWeight: FontWeight.w600,
                                        ),
                                      ),
                                      if ((key['fingerprint']?.toString() ?? '')
                                          .isNotEmpty)
                                        Text(
                                          key['fingerprint'].toString(),
                                          style: TextStyle(
                                            fontSize: 11,
                                            fontFamily: 'monospace',
                                            color: isLight
                                                ? AppColors.slate500
                                                : AppColors.slate400,
                                          ),
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                      if (createdAt != null)
                                        Text(
                                          '创建于: ${formatTimeCn(createdAt)}',
                                          style: TextStyle(
                                            fontSize: 11,
                                            color: isLight
                                                ? AppColors.slate400
                                                : AppColors.slate500,
                                          ),
                                        ),
                                    ],
                                  ),
                                ),
                                GestureDetector(
                                  onTap: () => _showViewDialog(key),
                                  child: const Icon(
                                    Icons.visibility_outlined,
                                    size: 18,
                                    color: AppColors.primary,
                                  ),
                                ),
                                const SizedBox(width: 12),
                                GestureDetector(
                                  onTap: () => _deleteKey(key),
                                  child: const Icon(
                                    Icons.delete_outline,
                                    size: 18,
                                    color: AppColors.red500,
                                  ),
                                ),
                              ],
                            ),
                          );
                        },
                      ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
