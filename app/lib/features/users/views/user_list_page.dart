import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/auth/auth_provider.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/api_endpoints.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/utils/time_utils.dart';
import '../../../shared/widgets/app_card.dart';

// ── Provider ──

final userListProvider = StateNotifierProvider<UserListNotifier, UserListState>(
  (ref) {
    return UserListNotifier();
  },
);

class UserListItem {
  final int id;
  final String username;
  final String role;
  final bool enabled;
  final DateTime? lastLoginAt;
  final DateTime createdAt;

  const UserListItem({
    required this.id,
    required this.username,
    required this.role,
    this.enabled = true,
    this.lastLoginAt,
    required this.createdAt,
  });

  factory UserListItem.fromJson(Map<String, dynamic> json) {
    return UserListItem(
      id: (json['id'] as num?)?.toInt() ?? 0,
      username: json['username']?.toString() ?? '',
      role: json['role']?.toString() ?? 'viewer',
      enabled: json['enabled'] != false,
      lastLoginAt: json['last_login_at'] is String
          ? DateTime.tryParse(json['last_login_at'])
          : null,
      createdAt: json['created_at'] is String
          ? DateTime.tryParse(json['created_at']!) ?? DateTime.now()
          : DateTime.now(),
    );
  }

  bool get isAdmin => role == 'admin';

  String get roleLabel {
    switch (role) {
      case 'admin':
        return '管理员';
      case 'operator':
        return '操作员';
      case 'viewer':
        return '观察者';
      default:
        return role;
    }
  }
}

class UserListState {
  final List<UserListItem> items;
  final bool loading;
  final String? error;

  const UserListState({this.items = const [], this.loading = false, this.error});

  UserListState copyWith({
    List<UserListItem>? items,
    bool? loading,
    String? error,
    bool clearError = false,
  }) {
    return UserListState(
      items: items ?? this.items,
      loading: loading ?? this.loading,
      error: clearError ? null : error ?? this.error,
    );
  }
}

class UserListNotifier extends StateNotifier<UserListState> {
  UserListNotifier({UserListState initialState = const UserListState()})
    : _scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope),
      super(initialState) {
    AuthSessionEpoch.addListener(_synchronizeScope);
    PanelCapabilityRegistry.addScopeListener(_synchronizeScope);
  }
  String _scope;
  int _loadRequestId = 0;

  void _synchronizeScope() {
    final scope = AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope);
    if (_scope == scope) return;
    _scope = scope;
    _loadRequestId++;
    state = const UserListState();
  }

  @override
  void dispose() {
    AuthSessionEpoch.removeListener(_synchronizeScope);
    PanelCapabilityRegistry.removeScopeListener(_synchronizeScope);
    super.dispose();
  }

  Future<void> load() async {
    _synchronizeScope();
    final requestId = ++_loadRequestId;
    final scope = _scope;
    state = state.copyWith(loading: true, clearError: true);
    try {
      final resp = await DioClient.instance.dio.get(ApiEndpoints.users);
      final data = extractData(resp.data);
      List<UserListItem> items = [];
      if (data is List) {
        items = data
            .whereType<Map<String, dynamic>>()
            .map((e) => UserListItem.fromJson(e))
            .toList();
      }
      if (requestId != _loadRequestId || scope != _scope) return;
      state = state.copyWith(items: items, loading: false, clearError: true);
    } catch (error) {
      if (requestId != _loadRequestId || scope != _scope) return;
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '用户列表加载失败'),
      );
    }
  }

  Future<void> create(String username, String password, String role) async {
    await DioClient.instance.dio.post(
      ApiEndpoints.users,
      data: {'username': username, 'password': password, 'role': role},
    );
    await load();
  }

  Future<void> update(int id, {String? role, bool? enabled}) async {
    final data = <String, dynamic>{};
    if (role != null) data['role'] = role;
    if (enabled != null) data['enabled'] = enabled;
    await DioClient.instance.dio.put(ApiEndpoints.userById(id), data: data);
    await load();
  }

  Future<void> delete(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.userById(id));
    await load();
  }

  Future<void> resetPassword(int id, String password) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.userResetPassword(id),
      data: {'password': password},
    );
  }
}

// ── Page ──

class UserListPage extends ConsumerStatefulWidget {
  const UserListPage({super.key});

  @override
  ConsumerState<UserListPage> createState() => _UserListPageState();
}

class _UserListPageState extends ConsumerState<UserListPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.read(userListProvider.notifier).load());
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(userListProvider);
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    
    final currentUsername = ref.watch(authProvider).user?.username;

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
                      '用户管理',
                      style: TextStyle(
                        fontSize: 24,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ),
                  AppGlassIconButton(
                    icon: Icons.add,
                    tooltip: '新建用户',
                    onTap: () => _showCreateDialog(),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),
            Expanded(
              child: RefreshIndicator(
                color: AppColors.primary,
                onRefresh: () => ref.read(userListProvider.notifier).load(),
                child: state.loading && state.items.isEmpty
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
                    : state.error != null && state.items.isEmpty
                    ? _UserLoadError(
                        message: state.error!,
                        onRetry: () =>
                            ref.read(userListProvider.notifier).load(),
                      )
                    : state.items.isEmpty
                    ? ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        children: [
                          const SizedBox(height: 100),
                          Icon(
                            Icons.people_outline,
                            size: 56,
                            color: AppColors.slate400.withAlpha(120),
                          ),
                          const SizedBox(height: 12),
                          const Center(
                            child: Text(
                              '暂无用户',
                              style: TextStyle(color: AppColors.slate400),
                            ),
                          ),
                        ],
                      )
                    : ListView.builder(
                        padding: const EdgeInsets.fromLTRB(20, 0, 20, 100),
                        itemCount:
                            state.items.length + (state.error == null ? 0 : 1),
                        itemBuilder: (_, i) {
                          if (state.error != null && i == 0) {
                            return _InlineUserLoadError(
                              message: state.error!,
                              onRetry: () =>
                                  ref.read(userListProvider.notifier).load(),
                            );
                          }
                          final index = i - (state.error == null ? 0 : 1);
                          return _UserCard(
                            user: state.items[index],
                            isLight: isLight,
                            currentUsername: currentUsername,
                            showResetPw: _showResetPasswordDialog,
                            showRolePicker: _showRolePicker,
                            showDelete: _confirmDelete,
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

  Future<void> _showRolePicker(UserListItem user) async {
    String role = user.role;
    final changed = await showDialog<String>(
      context: context,
      builder: (dialogCtx) => StatefulBuilder(
        builder: (context, setDialogState) => AlertDialog(
          title: Text('修改 ${user.username} 的角色'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('请选择新的用户角色'),
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: ['admin', 'operator', 'viewer']
                    .map(
                       (item) => AppLiquidGlassChoiceChip(
                         label: item == 'admin'
                               ? '管理员'
                               : item == 'operator'
                               ? '操作员'
                               : '观察者',
                        selected: role == item,
                        onSelected: (_) => setDialogState(() => role = item),
                      ),
                    )
                    .toList(),
              ),
            ],
          ),
          actions: [AppLiquidGlassDialogActions(actions: [
            AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx)),
            AppGlassDialogAction(label: '保存', onPressed: () => Navigator.pop(dialogCtx, role), variant: AppLiquidGlassButtonVariant.primary),
          ])],
        ),
      ),
    );

    if (changed == null || changed == user.role) {
      return;
    }

    await ref.read(userListProvider.notifier).update(user.id, role: changed);
    if (!mounted) {
      return;
    }
    AppGlassNotice.show(
      context,
      '角色更新成功',
      type: AppGlassNoticeType.success,
    );
  }

  void _showCreateDialog() {
    final usernameC = TextEditingController();
    final passwordC = TextEditingController();
    String role = 'operator';

    showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useRootNavigator: true,
      builder: (ctx) {
        final navigator = Navigator.of(ctx);
        return StatefulBuilder(
          builder: (ctx, setSheetState) => Padding(
            padding: EdgeInsets.fromLTRB(
              20,
              0,
              20,
              MediaQuery.of(ctx).viewInsets.bottom + 20,
            ),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const Text(
                  '新建用户',
                  style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold),
                ),
                const SizedBox(height: 16),
                TextField(
                  controller: usernameC,
                  decoration: const InputDecoration(labelText: '用户名'),
                ),
                const SizedBox(height: 12),
                TextField(
                  controller: passwordC,
                  obscureText: true,
                  decoration: const InputDecoration(labelText: '密码'),
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  runSpacing: 8,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    const Text('角色', style: TextStyle(fontSize: 13)),
                    ...['admin', 'operator', 'viewer'].map(
                      (r) => AppLiquidGlassChoiceChip(
                          label: r == 'admin'
                                ? '管理员'
                                : r == 'operator'
                                ? '操作员'
                                : '观察者',
                          selected: role == r,
                          onSelected: (_) => setSheetState(() => role = r),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 20),
                SizedBox(
                  height: 44,
                  child: AppLiquidGlassButton(
                    label: '创建',
                    height: 44,
                    performanceMode: true,
                    onPressed: () async {
                      if (usernameC.text.trim().isEmpty ||
                          passwordC.text.isEmpty) {
                        return;
                      }
                      try {
                        await ref
                            .read(userListProvider.notifier)
                            .create(
                              usernameC.text.trim(),
                              passwordC.text,
                              role,
                            );
                        if (!mounted) {
                          return;
                        }
                        navigator.pop();
                        AppGlassNotice.show(
                          context,
                          '用户已创建',
                          type: AppGlassNoticeType.success,
                        );
                      } catch (error) {
                        if (!mounted) {
                          return;
                        }
                        AppGlassNotice.show(
                          context,
                          extractErrorMessage(error, '创建用户失败'),
                          type: AppGlassNoticeType.error,
                        );
                      }
                    },
                  ),
                ),
                const SizedBox(height: 8),
                SizedBox(
                  height: 44,
                  child: AppLiquidGlassButton(
                    label: '取消',
                    onPressed: () => Navigator.of(ctx).pop(),
                    width: double.infinity,
                    height: 44,
                    variant: AppLiquidGlassButtonVariant.secondary,
                    performanceMode: true,
                  ),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  void _showResetPasswordDialog(UserListItem user) {
    final passwordC = TextEditingController();
    showDialog(
      context: context,
      builder: (dialogCtx) {
        return AlertDialog(
          title: Text('重置 ${user.username} 的密码'),
          content: TextField(
            controller: passwordC,
            obscureText: true,
            decoration: const InputDecoration(labelText: '新密码'),
          ),
          actions: [
            AppLiquidGlassDialogActions(
              actions: [
                AppGlassDialogAction(
                  label: '取消',
                  onPressed: () => Navigator.pop(dialogCtx),
                ),
                AppGlassDialogAction(
                  label: '确认',
                  variant: AppLiquidGlassButtonVariant.warning,
                  onPressed: () async {
                    if (passwordC.text.isEmpty) return;
                    try {
                      await ref
                          .read(userListProvider.notifier)
                          .resetPassword(user.id, passwordC.text);
                      if (!mounted || !dialogCtx.mounted) {
                        return;
                      }
                      Navigator.of(dialogCtx).pop();
                      AppGlassNotice.show(
                        context,
                        '密码已重置',
                        type: AppGlassNoticeType.success,
                      );
                    } catch (error) {
                      if (!mounted) {
                        return;
                      }
                      AppGlassNotice.show(
                        context,
                        extractErrorMessage(error, '重置密码失败'),
                        type: AppGlassNoticeType.error,
                      );
                    }
                  },
                ),
              ],
            ),
          ],
        );
      },
    );
  }

  Future<void> _confirmDelete(UserListItem user) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('删除用户'),
        content: Text('确定要删除「${user.username}」吗？'),
        actions: [AppLiquidGlassDialogActions(actions: [
          AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(dialogCtx, false)),
          AppGlassDialogAction(label: '删除', onPressed: () => Navigator.pop(dialogCtx, true), variant: AppLiquidGlassButtonVariant.danger),
        ])],
      ),
    );
    if (confirm == true) {
      try {
        await ref.read(userListProvider.notifier).delete(user.id);
        if (!mounted) {
          return;
        }
        AppGlassNotice.show(
          context,
          '用户已删除',
          type: AppGlassNoticeType.success,
        );
      } catch (error) {
        if (!mounted) {
          return;
        }
        AppGlassNotice.show(
          context,
          extractErrorMessage(error, '删除用户失败'),
          type: AppGlassNoticeType.error,
        );
      }
    }
  }
}

class _UserLoadError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _UserLoadError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) => ListView(
    physics: const AlwaysScrollableScrollPhysics(),
    padding: const EdgeInsets.symmetric(horizontal: 32),
    children: [
      const SizedBox(height: 100),
      const Icon(Icons.cloud_off_outlined, size: 56, color: AppColors.slate400),
      const SizedBox(height: 12),
      Text(message, textAlign: TextAlign.center),
      const SizedBox(height: 12),
      Center(child: TextButton(onPressed: onRetry, child: const Text('重试'))),
    ],
  );
}

class _InlineUserLoadError extends StatelessWidget {
  final String message;
  final VoidCallback onRetry;

  const _InlineUserLoadError({required this.message, required this.onRetry});

  @override
  Widget build(BuildContext context) => AppCard(
    margin: const EdgeInsets.only(bottom: 12),
    padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
    child: Row(
      children: [
        const Icon(Icons.sync_problem_outlined, color: AppColors.amber500),
        const SizedBox(width: 10),
        Expanded(child: Text(message, style: const TextStyle(fontSize: 13))),
        TextButton(onPressed: onRetry, child: const Text('重试')),
      ],
    ),
  );
}

class _UserCard extends ConsumerWidget {
  final UserListItem user;
  final bool isLight;
  final String? currentUsername;
  final void Function(UserListItem) showResetPw;
  final Future<void> Function(UserListItem) showRolePicker;
  final Future<void> Function(UserListItem) showDelete;

  const _UserCard({
    required this.user,
    required this.isLight,
    required this.currentUsername,
    required this.showResetPw,
    required this.showRolePicker,
    required this.showDelete,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    
    final roleColor = user.role == 'admin'
        ? AppColors.red500
        : user.role == 'operator'
        ? AppColors.amber500
        : AppColors.primary;
    final isSelf = currentUsername == user.username;

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: AppCard(
        stableForScrolling: true,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        child: Row(
        children: [
          Container(
            width: 40,
            height: 40,
            decoration: BoxDecoration(
              color: roleColor.withAlpha(25),
              shape: BoxShape.circle,
            ),
            child: Center(
              child: Text(
                user.username.substring(0, 1).toUpperCase(),
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                  color: roleColor,
                ),
              ),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text(
                      user.username,
                      style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                      ),
                    ),
                    const SizedBox(width: 6),
                    Container(
                      padding: const EdgeInsets.symmetric(
                        horizontal: 8,
                        vertical: 3,
                      ),
                      decoration: BoxDecoration(
                        color: roleColor.withAlpha(25),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text(
                        user.roleLabel,
                        style: TextStyle(
                          fontSize: 11,
                          fontWeight: FontWeight.w700,
                          color: roleColor,
                        ),
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                Text(
                  user.enabled ? '已启用' : '已禁用',
                  style: TextStyle(
                    fontSize: 12,
                    color: user.enabled
                        ? AppColors.primary
                        : AppColors.slate400,
                  ),
                ),
                const SizedBox(height: 6),
                Text(
                  '最后登录: ${formatTimeCn(user.lastLoginAt)}',
                  style: TextStyle(
                    fontSize: 12,
                    color: isLight ? AppColors.slate500 : AppColors.slate400,
                  ),
                ),
                Text(
                  '创建时间: ${formatTimeCn(user.createdAt)}',
                  style: TextStyle(
                    fontSize: 12,
                    color: isLight ? AppColors.slate500 : AppColors.slate400,
                  ),
                ),
              ],
            ),
          ),
          PopupMenuButton<String>(
            icon: Icon(
              Icons.more_vert,
              size: 18,
              color: isLight ? AppColors.slate400 : AppColors.slate500,
            ),
            itemBuilder: (_) => [
              if (!isSelf)
                const PopupMenuItem(value: 'role', child: Text('修改角色')),
              if (!isSelf)
                PopupMenuItem(
                  value: 'toggle',
                  child: Text(user.enabled ? '禁用' : '启用'),
                ),
              const PopupMenuItem(value: 'reset_pw', child: Text('重置密码')),
              if (!isSelf)
                const PopupMenuItem(
                  value: 'delete',
                  child: Text('删除', style: TextStyle(color: AppColors.red500)),
                ),
            ],
            onSelected: (v) async {
              switch (v) {
                case 'role':
                  await showRolePicker(user);
                  break;
                case 'toggle':
                  try {
                    await ref
                        .read(userListProvider.notifier)
                        .update(user.id, enabled: !user.enabled);
                    if (!context.mounted) {
                      return;
                    }
                    AppGlassNotice.show(
                      context,
                      user.enabled ? '用户已禁用' : '用户已启用',
                      type: AppGlassNoticeType.success,
                    );
                  } catch (error) {
                    if (!context.mounted) {
                      return;
                    }
                    AppGlassNotice.show(
                      context,
                      extractErrorMessage(
                        error,
                        user.enabled ? '禁用用户失败' : '启用用户失败',
                      ),
                      type: AppGlassNoticeType.error,
                    );
                  }
                  break;
                case 'reset_pw':
                  showResetPw(user);
                  break;
                case 'delete':
                  await showDelete(user);
                  break;
              }
            },
          ),
        ],
        ),
      ),
    );
  }
}
