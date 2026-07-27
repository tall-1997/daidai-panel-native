import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

// ── Security Page (Tabbed) ──

class SecurityPage extends ConsumerStatefulWidget {
  const SecurityPage({super.key});

  @override
  ConsumerState<SecurityPage> createState() => _SecurityPageState();
}

class _SecurityPageState extends ConsumerState<SecurityPage>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 6, vsync: this);
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
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
                    onTap: () => context.pop(),
                    child: const Icon(Icons.arrow_back_ios, size: 20),
                  ),
                  const SizedBox(width: 8),
                  const Expanded(
                    child: Text(
                      '安全设置',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 12),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20),
              child: AppLiquidGlassSurface(
                borderRadius: 18,
                child: TabBar(
                  controller: _tabController,
                  isScrollable: true,
                  tabAlignment: TabAlignment.start,
                  labelColor: AppColors.primary,
                  unselectedLabelColor: isLight
                      ? AppColors.slate500
                      : AppColors.slate400,
                  indicatorColor: AppColors.primary,
                  dividerColor: Colors.transparent,
                  labelStyle: const TextStyle(
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                  labelPadding: const EdgeInsets.symmetric(horizontal: 14),
                  tabs: const [
                    Tab(text: '登录日志'),
                    Tab(text: '活跃会话'),
                    Tab(text: 'IP 白名单'),
                    Tab(text: '两步验证'),
                    Tab(text: '登录统计'),
                    Tab(text: '审计日志'),
                  ],
                ),
              ),
            ),
            Expanded(
              child: TabBarView(
                controller: _tabController,
                children: [
                  _LoginLogsTab(isLight: isLight),
                  _SessionsTab(isLight: isLight),
                  _IpWhitelistTab(isLight: isLight),
                  _TwoFaTab(isLight: isLight),
                  _LoginStatsTab(isLight: isLight),
                  _AuditLogsTab(isLight: isLight),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ── Login Logs Tab ──

class _LoginLogsTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _LoginLogsTab({required this.isLight});

  @override
  ConsumerState<_LoginLogsTab> createState() => _LoginLogsTabState();
}

class _LoginLogsTabState extends ConsumerState<_LoginLogsTab>
    with AutomaticKeepAliveClientMixin {
  List<Map<String, dynamic>> _logs = [];
  bool _loading = true;
  int _page = 1;
  int _total = 0;
  final TextEditingController _usernameController = TextEditingController();

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _usernameController.dispose();
    super.dispose();
  }

  Future<void> _load({bool refresh = true}) async {
    if (_loading && !refresh) return;
    if (refresh) _page = 1;
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.loginLogs,
        queryParameters: {
          'page': _page,
          'page_size': 100,
          if (_usernameController.text.trim().isNotEmpty)
            'username': _usernameController.text.trim(),
        },
      );
      final paginated = extractPaginated(resp.data);
      if (!mounted) return;
      setState(() {
        _logs = refresh ? paginated.items : [..._logs, ...paginated.items];
        _total = paginated.total;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  void _loadMore() {
    if (_loading || _logs.length >= _total) return;
    _page++;
    _load(refresh: false);
  }

  Future<void> _clearLogs() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('清理登录日志'),
        content: const Text('确定要清理全部登录日志吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx, false)),
          AppGlassDialogAction(label: '清理', onPressed: () => Navigator.pop(dialogCtx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirmed != true) {
      return;
    }
    try {
      await DioClient.instance.dio.delete(ApiEndpoints.loginLogs);
      await _load();
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '登录日志已清理',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '清理登录日志失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification.metrics.extentAfter < 240) _loadMore();
        return false;
      },
      child: RefreshIndicator(
      color: AppColors.primary,
      onRefresh: _load,
      child: _loading && _logs.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const [
                SizedBox(height: 120),
                Center(
                  child: CircularProgressIndicator(color: AppColors.primary),
                ),
              ],
            )
          : ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
              children: [
                Row(
                  children: [
                    Expanded(
                      child: TextField(
                        controller: _usernameController,
                        decoration: InputDecoration(
                          hintText: '按用户名筛选',
                          prefixIcon: const Icon(Icons.search, size: 18),
                          suffixIcon: _usernameController.text.isNotEmpty
                              ? IconButton(
                                  icon: const Icon(Icons.clear, size: 16),
                                  onPressed: () {
                                    _usernameController.clear();
                                    setState(() {});
                                    _load();
                                  },
                                )
                              : null,
                        ),
                        onChanged: (_) => setState(() {}),
                        onSubmitted: (_) => _load(),
                      ),
                    ),
                    const SizedBox(width: 10),
                    OutlinedButton.icon(
                      onPressed: _load,
                      icon: const Icon(Icons.refresh, size: 16),
                      label: const Text('刷新'),
                    ),
                  ],
                ),
                const SizedBox(height: 10),
                FilledButton.tonalIcon(
                  onPressed: _clearLogs,
                  icon: const Icon(Icons.delete_sweep_outlined, size: 16),
                  label: const Text('清理登录日志'),
                  style: FilledButton.styleFrom(
                    foregroundColor: AppColors.red500,
                  ),
                ),
                const SizedBox(height: 12),
                if (_logs.isEmpty)
                  const Padding(
                    padding: EdgeInsets.only(top: 80),
                    child: Center(
                      child: Text(
                        '暂无记录',
                        style: TextStyle(color: AppColors.slate400),
                      ),
                    ),
                  )
                else
                  ..._logs.map((log) {
                    final success = (log['status'] as num?)?.toInt() == 0;
                    final time = DateTime.tryParse(
                      log['created_at']?.toString() ?? '',
                    );
                    return Padding(
                      padding: const EdgeInsets.only(bottom: 8),
                      child: AppCard(
                        stableForScrolling: true,
                        padding: const EdgeInsets.symmetric(
                          horizontal: 14,
                          vertical: 12,
                        ),
                        child: Row(
                        children: [
                          Icon(
                            success ? Icons.check_circle : Icons.cancel,
                            size: 18,
                            color: success
                                ? AppColors.primary
                                : AppColors.red500,
                          ),
                          const SizedBox(width: 10),
                          Expanded(
                            child: Column(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Text(
                                  log['username']?.toString() ?? '',
                                  style: const TextStyle(
                                    fontSize: 13,
                                    fontWeight: FontWeight.w600,
                                  ),
                                ),
                                const SizedBox(height: 2),
                                Text(
                                  '${log['ip'] ?? ''} · ${log['message'] ?? ''}',
                                  style: TextStyle(
                                    fontSize: 11,
                                    color: widget.isLight
                                        ? AppColors.slate500
                                        : AppColors.slate400,
                                  ),
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                                const SizedBox(height: 2),
                                Text(
                                  log['client_name']?.toString() ?? '客户端未知',
                                  style: TextStyle(
                                    fontSize: 11,
                                    color: widget.isLight
                                        ? AppColors.slate500
                                        : AppColors.slate400,
                                  ),
                                  maxLines: 1,
                                  overflow: TextOverflow.ellipsis,
                                ),
                              ],
                            ),
                          ),
                          Text(
                            formatTimeCn(time),
                            style: TextStyle(
                              fontSize: 11,
                              color: widget.isLight
                                  ? AppColors.slate400
                                  : AppColors.slate500,
                            ),
                          ),
                        ],
                        ),
                      ),
                    );
                  }),
                if (_loading && _logs.isNotEmpty)
                  const Padding(
                    padding: EdgeInsets.all(16),
                    child: Center(
                      child: CircularProgressIndicator(color: AppColors.primary),
                    ),
                  ),
              ],
            ),
      ),
    );
  }
}

// ── Sessions Tab ──

class _SessionsTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _SessionsTab({required this.isLight});

  @override
  ConsumerState<_SessionsTab> createState() => _SessionsTabState();
}

class _SessionsTabState extends ConsumerState<_SessionsTab>
    with AutomaticKeepAliveClientMixin {
  List<Map<String, dynamic>> _sessions = [];
  bool _loading = true;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.sessions);
      final data = extractData(resp.data);
      if (!mounted) return;
      setState(() {
        _sessions = (data is List)
            ? data.whereType<Map<String, dynamic>>().toList()
            : [];
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  Future<void> _revokeOthers() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('撤销其他会话'),
        content: const Text('确定要撤销当前账号的所有其他活跃会话吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx, false)),
          AppGlassDialogAction(label: '撤销', onPressed: () => Navigator.pop(dialogCtx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirmed != true) {
      return;
    }
    try {
      await DioClient.instance.dio.delete(ApiEndpoints.sessionsOthers);
      await _load();
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '其他会话已撤销',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '撤销其他会话失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _revokeSession(int id) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('撤销会话'),
        content: const Text('确定要撤销这个活跃会话吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx, false)),
          AppGlassDialogAction(label: '撤销', onPressed: () => Navigator.pop(dialogCtx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirmed != true) {
      return;
    }
    try {
      await DioClient.instance.dio.delete(ApiEndpoints.sessionById(id));
      await _load();
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        '会话已撤销',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '撤销会话失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    
    return RefreshIndicator(
      color: AppColors.primary,
      onRefresh: _load,
      child: _loading && _sessions.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const [
                SizedBox(height: 120),
                Center(
                  child: CircularProgressIndicator(color: AppColors.primary),
                ),
              ],
            )
          : _sessions.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(20, 8, 20, 4),
                  child: SizedBox(
                    width: double.infinity,
                    height: 36,
                    child: OutlinedButton.icon(
                      onPressed: _revokeOthers,
                      icon: const Icon(Icons.logout, size: 16),
                      label: const Text(
                        '撤销其他会话',
                        style: TextStyle(fontSize: 12),
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 80),
                const Center(
                  child: Text(
                    '暂无会话',
                    style: TextStyle(color: AppColors.slate400),
                  ),
                ),
              ],
            )
          : ListView.builder(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
              itemCount: _sessions.length + 1,
              itemBuilder: (_, i) {
                if (i == 0) {
                  return Padding(
                    padding: const EdgeInsets.only(bottom: 8),
                    child: SizedBox(
                      width: double.infinity,
                      height: 36,
                      child: OutlinedButton.icon(
                        onPressed: _revokeOthers,
                        icon: const Icon(Icons.logout, size: 16),
                        label: const Text(
                          '撤销其他会话',
                          style: TextStyle(fontSize: 12),
                        ),
                      ),
                    ),
                  );
                }
                final s = _sessions[i - 1];
                final expires = DateTime.tryParse(
                  s['expires_at']?.toString() ?? '',
                );
                return Padding(
                  padding: const EdgeInsets.only(bottom: 8),
                  child: AppCard(
                    stableForScrolling: true,
                    padding: const EdgeInsets.symmetric(
                      horizontal: 14,
                      vertical: 12,
                    ),
                    child: Row(
                    children: [
                      const Icon(
                        Icons.devices,
                        size: 18,
                        color: AppColors.primary,
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              '${s['username'] ?? ''} · ${s['ip'] ?? ''}',
                              style: const TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                            const SizedBox(height: 2),
                            Text(
                              expires != null
                                  ? '过期: ${formatTimeCn(expires)}'
                                  : '',
                              style: TextStyle(
                                fontSize: 11,
                                color: widget.isLight
                                    ? AppColors.slate500
                                    : AppColors.slate400,
                              ),
                            ),
                            const SizedBox(height: 2),
                            Text(
                              '客户端: ${s['client_name'] ?? s['client_type_label'] ?? '未知'}',
                              style: TextStyle(
                                fontSize: 11,
                                color: widget.isLight
                                    ? AppColors.slate500
                                    : AppColors.slate400,
                              ),
                            ),
                            if ((s['user_agent']?.toString() ?? '').isNotEmpty)
                              Text(
                                'UA: ${s['user_agent']}',
                                style: TextStyle(
                                  fontSize: 11,
                                  color: widget.isLight
                                      ? AppColors.slate500
                                      : AppColors.slate400,
                                ),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              ),
                          ],
                        ),
                      ),
                      GestureDetector(
                        onTap: () {
                          final id = (s['id'] as num?)?.toInt();
                          if (id != null) {
                            _revokeSession(id);
                          }
                        },
                        child: const Icon(
                          Icons.close,
                          size: 18,
                          color: AppColors.red500,
                        ),
                      ),
                    ],
                    ),
                  ),
                );
              },
            ),
    );
  }
}

// ── IP Whitelist Tab ──

class _IpWhitelistTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _IpWhitelistTab({required this.isLight});

  @override
  ConsumerState<_IpWhitelistTab> createState() => _IpWhitelistTabState();
}

class _IpWhitelistTabState extends ConsumerState<_IpWhitelistTab>
    with AutomaticKeepAliveClientMixin {
  List<Map<String, dynamic>> _items = [];
  bool _loading = true;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.ipWhitelist);
      final data = extractData(resp.data);
      if (!mounted) return;
      setState(() {
        _items = (data is List)
            ? data.whereType<Map<String, dynamic>>().toList()
            : [];
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  Future<void> _deleteItem(Map<String, dynamic> item) async {
    final id = (item['id'] as num?)?.toInt();
    if (id == null) {
      return;
    }

    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('删除白名单'),
        content: Text('确定删除 IP「${item['ip'] ?? ''}」吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx, false),
              ),
              AppGlassDialogAction(
                label: '删除',
                variant: AppLiquidGlassButtonVariant.danger,
                onPressed: () => Navigator.pop(dialogCtx, true),
              ),
            ],
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }

    try {
      await DioClient.instance.dio.delete(ApiEndpoints.ipWhitelistById(id));
      await _load();
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        'IP 白名单已删除',
        type: AppGlassNoticeType.success,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '删除 IP 白名单失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    return RefreshIndicator(
      color: AppColors.primary,
      onRefresh: _load,
      child: _loading
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const [
                SizedBox(height: 120),
                Center(
                  child: CircularProgressIndicator(color: AppColors.primary),
                ),
              ],
            )
          : _items.isEmpty
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
                        '添加 IP',
                        style: TextStyle(fontSize: 12),
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 80),
                const Center(
                  child: Text(
                    '暂无白名单',
                    style: TextStyle(color: AppColors.slate400),
                  ),
                ),
              ],
            )
          : ListView.builder(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
              itemCount: _items.length + 1,
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
                          '添加 IP',
                          style: TextStyle(fontSize: 12),
                        ),
                      ),
                    ),
                  );
                }
                final item = _items[i - 1];
                return AppCard(
                  stableForScrolling: true,
                  margin: const EdgeInsets.only(bottom: 8),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 12,
                  ),
                  borderRadius: 12,
                  child: Row(
                    children: [
                      const Icon(
                        Icons.shield_outlined,
                        size: 18,
                        color: AppColors.primary,
                      ),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              item['ip']?.toString() ?? '',
                              style: const TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w600,
                                fontFamily: 'monospace',
                              ),
                            ),
                            if ((item['remarks']?.toString() ?? '').isNotEmpty)
                              Text(
                                item['remarks'].toString(),
                                style: TextStyle(
                                  fontSize: 11,
                                  color: widget.isLight
                                      ? AppColors.slate500
                                      : AppColors.slate400,
                                ),
                              ),
                          ],
                        ),
                      ),
                      GestureDetector(
                        onTap: () => _deleteItem(item),
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
    );
  }

  void _showAddDialog() {
    final ipC = TextEditingController();
    final remarksC = TextEditingController();
    showDialog(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('添加 IP 白名单'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: ipC,
              decoration: const InputDecoration(labelText: 'IP 地址'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: remarksC,
              decoration: const InputDecoration(labelText: '备注 (可选)'),
            ),
          ],
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx),
              ),
              AppGlassDialogAction(
                label: '添加',
                variant: AppLiquidGlassButtonVariant.primary,
                onPressed: () async {
                  if (ipC.text.trim().isEmpty) return;
                  try {
                    await DioClient.instance.dio.post(
                      ApiEndpoints.ipWhitelist,
                      data: {
                        'ip': ipC.text.trim(),
                        'remarks': remarksC.text.trim(),
                      },
                    );
                    if (!mounted || !dialogCtx.mounted) {
                      return;
                    }
                    Navigator.of(dialogCtx).pop();
                    await _load();
                    if (!mounted) {
                      return;
                    }
                    AppGlassNotice.show(
                      context,
                      'IP 白名单已添加',
                      type: AppGlassNoticeType.success,
                    );
                  } catch (error) {
                    if (!mounted) {
                      return;
                    }
                    AppGlassNotice.show(
                      context,
                      extractErrorMessage(error, '添加 IP 白名单失败'),
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
}

// ── 2FA Tab ──

class _TwoFaTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _TwoFaTab({required this.isLight});

  @override
  ConsumerState<_TwoFaTab> createState() => _TwoFaTabState();
}

class _TwoFaTabState extends ConsumerState<_TwoFaTab>
    with AutomaticKeepAliveClientMixin {
  bool _enabled = false;
  bool _loading = true;
  String? _secret;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _loadStatus();
  }

  Future<void> _loadStatus() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.twoFaStatus);
      final data = extractData(resp.data);
      if (!mounted) return;
      setState(() {
        _enabled = data is Map && data['enabled'] == true;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);
    if (_loading) {
      return const Center(
        child: CircularProgressIndicator(color: AppColors.primary),
      );
    }

    return Padding(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          AppCard(
            padding: const EdgeInsets.all(16),
            borderRadius: 14,
            child: Row(
              children: [
                Icon(
                  Icons.security,
                  size: 24,
                  color: _enabled ? AppColors.primary : AppColors.slate400,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text(
                        '两步验证 (TOTP)',
                        style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 2),
                      Text(
                        _enabled ? '已启用' : '未启用',
                        style: TextStyle(
                          fontSize: 12,
                          color: _enabled
                              ? AppColors.primary
                              : AppColors.slate400,
                        ),
                      ),
                    ],
                  ),
                ),
                if (_enabled)
                  OutlinedButton(
                    onPressed: _disable2FA,
                    style: OutlinedButton.styleFrom(
                      foregroundColor: AppColors.red500,
                      side: const BorderSide(color: AppColors.red500),
                      minimumSize: const Size(0, 36),
                    ),
                    child: const Text('禁用', style: TextStyle(fontSize: 12)),
                  )
                else
                  FilledButton(
                    onPressed: _setup2FA,
                    style: FilledButton.styleFrom(
                      minimumSize: const Size(0, 36),
                    ),
                    child: const Text('设置', style: TextStyle(fontSize: 12)),
                  ),
              ],
            ),
          ),
          if (_secret != null) ...[
            const SizedBox(height: 16),
            AppCard(
              padding: const EdgeInsets.all(16),
              borderRadius: 14,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('请使用验证器扫描或输入密钥:', style: TextStyle(fontSize: 13)),
                  const SizedBox(height: 8),
                  SelectableText(
                    _secret!,
                    style: const TextStyle(
                      fontFamily: 'monospace',
                      fontSize: 14,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                  const SizedBox(height: 12),
                  _VerifyCodeInput(onVerify: (code) => _verify2FA(code)),
                ],
              ),
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _setup2FA() async {
    try {
      final resp = await DioClient.instance.dio.post(ApiEndpoints.twoFaSetup);
      final data = extractData(resp.data);
      if (!mounted) return;
      if (data is Map) {
        setState(() {
          _secret = data['secret']?.toString();
        });
      }
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '获取 2FA 密钥失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }

  Future<void> _verify2FA(String code) async {
    try {
      await DioClient.instance.dio.post(
        ApiEndpoints.twoFaVerify,
        data: {'code': code},
      );
      if (!mounted) return;
      setState(() {
        _enabled = true;
        _secret = null;
      });
      if (mounted) {
        AppGlassNotice.show(
          context,
          '2FA 已启用',
          type: AppGlassNoticeType.success,
        );
      }
    } catch (_) {
      if (mounted) {
        AppGlassNotice.show(
          context,
          '验证码错误',
          type: AppGlassNoticeType.error,
        );
      }
    }
  }

  Future<void> _disable2FA() async {
    final codeController = TextEditingController();
    final code = await showDialog<String>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('禁用两步验证'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('请输入当前验证器里的 6 位动态验证码后再禁用。'),
            const SizedBox(height: 12),
            TextField(
              controller: codeController,
              keyboardType: TextInputType.number,
              maxLength: 6,
              decoration: const InputDecoration(
                labelText: '动态验证码',
                hintText: '6位数字',
              ),
            ),
          ],
        ),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx),
              ),
              AppGlassDialogAction(
                label: '禁用',
                variant: AppLiquidGlassButtonVariant.danger,
                onPressed: () =>
                    Navigator.pop(dialogCtx, codeController.text.trim()),
              ),
            ],
          ),
        ],
      ),
    );
    codeController.dispose();
    if (code == null || code.length != 6) {
      return;
    }
    try {
      await DioClient.instance.dio.delete(
        ApiEndpoints.twoFa,
        data: {'code': code},
      );
      if (!mounted) {
        return;
      }
      setState(() => _enabled = false);
      AppGlassNotice.show(
        context,
        '2FA 已禁用',
        type: AppGlassNoticeType.warning,
      );
    } catch (error) {
      if (!mounted) {
        return;
      }
      AppGlassNotice.show(
        context,
        extractErrorMessage(error, '禁用 2FA 失败'),
        type: AppGlassNoticeType.error,
      );
    }
  }
}

// ── Login Stats Tab ──

class _LoginStatsTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _LoginStatsTab({required this.isLight});

  @override
  ConsumerState<_LoginStatsTab> createState() => _LoginStatsTabState();
}

class _LoginStatsTabState extends ConsumerState<_LoginStatsTab>
    with AutomaticKeepAliveClientMixin {
  Map<String, dynamic>? _stats;
  bool _loading = true;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.loginStats);
      final data = extractData(resp.data);
      if (!mounted) return;
      setState(() {
        _stats = data is Map<String, dynamic> ? data : null;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);

    if (_loading) {
      return ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        children: const [
          SizedBox(height: 120),
          Center(
            child: CircularProgressIndicator(color: AppColors.primary),
          ),
        ],
      );
    }

    if (_stats == null || _stats!.isEmpty) {
      return RefreshIndicator(
        color: AppColors.primary,
        onRefresh: _load,
        child: ListView(
          physics: const AlwaysScrollableScrollPhysics(),
          children: const [
            SizedBox(height: 80),
            Center(
              child: Text(
                '暂无统计数据',
                style: TextStyle(color: AppColors.slate400),
              ),
            ),
          ],
        ),
      );
    }

    final keys = _stats!.keys.toList()..sort();
    return RefreshIndicator(
      color: AppColors.primary,
      onRefresh: _load,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
        children: keys.map((key) {
          final value = _stats![key];
          return AppCard(
            stableForScrolling: true,
            margin: const EdgeInsets.only(bottom: 8),
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            borderRadius: 12,
            child: Row(
              children: [
                Expanded(
                  child: Text(
                    key,
                    style: const TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
                Text(
                  _formatStatValue(value),
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w700,
                    color: widget.isLight
                        ? AppColors.slate600
                        : AppColors.slate300,
                  ),
                ),
              ],
            ),
          );
        }).toList(),
      ),
    );
  }

  String _formatStatValue(dynamic value) {
    if (value is num) {
      if (value == value.toInt()) return value.toInt().toString();
      return value.toStringAsFixed(1);
    }
    return value?.toString() ?? '-';
  }
}

// ── Audit Logs Tab ──

class _AuditLogsTab extends ConsumerStatefulWidget {
  final bool isLight;
  const _AuditLogsTab({required this.isLight});

  @override
  ConsumerState<_AuditLogsTab> createState() => _AuditLogsTabState();
}

class _AuditLogsTabState extends ConsumerState<_AuditLogsTab>
    with AutomaticKeepAliveClientMixin {
  List<Map<String, dynamic>> _logs = [];
  bool _loading = true;
  int _page = 1;
  int _total = 0;

  @override
  bool get wantKeepAlive => true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load({bool refresh = true}) async {
    if (_loading && !refresh) return;
    if (refresh) _page = 1;
    setState(() => _loading = true);
    try {
      final resp = await DioClient.instance.dio.get(
        ApiEndpoints.auditLogs,
        queryParameters: {'page': _page, 'page_size': 100},
      );
      final paginated = extractPaginated(resp.data);
      if (!mounted) return;
      setState(() {
        _logs = refresh ? paginated.items : [..._logs, ...paginated.items];
        _total = paginated.total;
        _loading = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _loading = false);
    }
  }

  void _loadMore() {
    if (_loading || _logs.length >= _total) return;
    _page++;
    _load(refresh: false);
  }

  @override
  Widget build(BuildContext context) {
    super.build(context);

    return NotificationListener<ScrollNotification>(
      onNotification: (notification) {
        if (notification.metrics.extentAfter < 240) _loadMore();
        return false;
      },
      child: RefreshIndicator(
      color: AppColors.primary,
      onRefresh: _load,
      child: _loading
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const [
                SizedBox(height: 120),
                Center(
                  child: CircularProgressIndicator(color: AppColors.primary),
                ),
              ],
            )
          : _logs.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: const [
                SizedBox(height: 80),
                Center(
                  child: Text(
                    '暂无审计记录',
                    style: TextStyle(color: AppColors.slate400),
                  ),
                ),
              ],
            )
          : ListView.builder(
              physics: const AlwaysScrollableScrollPhysics(),
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 100),
              itemCount: _logs.length + (_logs.length < _total ? 1 : 0),
              itemBuilder: (_, i) {
                if (i == _logs.length) {
                  return const Padding(
                    padding: EdgeInsets.all(16),
                    child: Center(
                      child: CircularProgressIndicator(color: AppColors.primary),
                    ),
                  );
                }
                final log = _logs[i];
                final time = DateTime.tryParse(
                  log['created_at']?.toString() ?? '',
                );
                return AppCard(
                  stableForScrolling: true,
                  margin: const EdgeInsets.only(bottom: 8),
                  padding: const EdgeInsets.symmetric(
                    horizontal: 14,
                    vertical: 12,
                  ),
                  borderRadius: 12,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          const Icon(Icons.history,
                            size: 16,
                            color: AppColors.primary,
                          ),
                          const SizedBox(width: 8),
                          Expanded(
                            child: Text(
                              log['action']?.toString() ?? '',
                              style: const TextStyle(
                                fontSize: 13,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                          if (time != null)
                            Text(
                              formatTimeCn(time),
                              style: TextStyle(
                                fontSize: 11,
                                color: widget.isLight
                                    ? AppColors.slate400
                                    : AppColors.slate500,
                              ),
                            ),
                        ],
                      ),
                      if ((log['username']?.toString() ?? '').isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          '${log['username']} · ${log['ip'] ?? ''}',
                          style: TextStyle(
                            fontSize: 11,
                            color: widget.isLight
                                ? AppColors.slate500
                                : AppColors.slate400,
                          ),
                        ),
                      ],
                      if ((log['detail']?.toString() ?? '').isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          log['detail'].toString(),
                          style: TextStyle(
                            fontSize: 11,
                            color: widget.isLight
                                ? AppColors.slate600
                                : AppColors.slate300,
                          ),
                          maxLines: 3,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ],
                  ),
                );
              },
            ),
      ),
    );
  }
}

class _VerifyCodeInput extends StatefulWidget {
  final Future<void> Function(String) onVerify;
  const _VerifyCodeInput({required this.onVerify});

  @override
  State<_VerifyCodeInput> createState() => _VerifyCodeInputState();
}

class _VerifyCodeInputState extends State<_VerifyCodeInput> {
  final _codeC = TextEditingController();

  @override
  void dispose() {
    _codeC.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Expanded(
          child: TextField(
            controller: _codeC,
            decoration: const InputDecoration(
              labelText: '验证码',
              hintText: '6位数字',
            ),
            keyboardType: TextInputType.number,
            maxLength: 6,
          ),
        ),
        const SizedBox(width: 12),
        FilledButton(
          onPressed: () {
            if (_codeC.text.length == 6) widget.onVerify(_codeC.text);
          },
          child: const Text('验证'),
        ),
      ],
    );
  }
}
