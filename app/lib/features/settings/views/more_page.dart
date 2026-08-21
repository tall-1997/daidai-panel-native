import 'package:flutter/material.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:package_info_plus/package_info_plus.dart';
import '../../../core/auth/auth_provider.dart';
import '../../../core/local_panel/method_channel_local_panel_host.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/services/app_update_service.dart';
import '../../../core/storage/secure_storage.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';

class MorePage extends ConsumerStatefulWidget {
  const MorePage({super.key});

  @override
  ConsumerState<MorePage> createState() => _MorePageState();
}

bool showsOperatorAutomation(String? role) =>
    role == 'operator' || role == 'admin';

bool showsPanelCapability(PanelCapability capability, {String? scope}) =>
    !PanelCapabilityRegistry.isUnsupported(capability, scope: scope);

bool enablesPanelCapability(PanelCapability capability, {String? scope}) =>
    !PanelCapabilityRegistry.isUnavailable(capability, scope: scope);

class _MorePageState extends ConsumerState<MorePage> {
  AppUpdateInfo? _updateInfo;
  bool _checking = false;
  String? _serverUrl;

  @override
  void initState() {
    super.initState();
    _loadServerUrl();
  }

  Future<void> _loadServerUrl() async {
    final url = await SecureStorage.getServerUrl();
    if (mounted) setState(() => _serverUrl = url);
  }

  Future<void> _openBrowserPanel() async {
    try {
      await MethodChannelLocalPanelHost().openBrowserPanel();
    } catch (_) {
      if (mounted) {
        AppGlassNotice.show(
          context,
          '无法打开本地 Web 面板',
          type: AppGlassNoticeType.error,
        );
      }
    }
  }

  String? _buildAvatarUrl(String? avatarPath) {
    if (avatarPath == null || avatarPath.isEmpty || _serverUrl == null) {
      return null;
    }
    if (avatarPath.startsWith('http')) return avatarPath;
    return '$_serverUrl$avatarPath';
  }

  Future<void> _checkUpdate({bool silent = false}) async {
    if (_checking) return;
    setState(() => _checking = true);
    try {
      final info = await AppUpdateService.checkUpdate(throwOnError: true);
      if (mounted) {
        setState(() {
          _updateInfo = info;
          _checking = false;
        });
        final availability = info == null ? null : classifyAppUpdate(info);
        if (availability == AppUpdateAvailability.updateAvailable && !silent) {
          AppUpdateService.showUpdateDialog(context, info!);
        } else if (availability == AppUpdateAvailability.installerMissing &&
            !silent) {
          AppGlassNotice.show(
            context,
            '发现新版本，但 Release 缺少可用的 Android 安装包',
            type: AppGlassNoticeType.error,
          );
        } else if (!silent) {
          AppGlassNotice.show(
            context,
            '当前已是最新版本',
            type: AppGlassNoticeType.info,
          );
        }
      }
    } catch (error) {
      if (mounted) {
        setState(() => _checking = false);
        if (!silent) {
          AppGlassNotice.show(
            context,
            error is DioException && error.response?.statusCode == 403
                ? 'GitHub API 请求受限，请稍后重试'
                : '检查更新失败，请检查网络后重试',
            type: AppGlassNoticeType.error,
          );
        }
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authProvider);
    final user = auth.user;
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;
    final capabilityScope = PanelCapabilityRegistry.currentScope;
    bool shows(PanelCapability capability) =>
        showsPanelCapability(capability, scope: capabilityScope);
    VoidCallback? gated(
      PanelCapability capability,
      VoidCallback action,
    ) => enablesPanelCapability(capability, scope: capabilityScope)
        ? action
        : null;

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: ListView(
        padding: EdgeInsets.only(
          top: MediaQuery.of(context).padding.top + 16,
          left: 16,
          right: 16,
          bottom: 110,
        ),
        children: [
          const Text(
            '设置',
            style: TextStyle(fontSize: 24, fontWeight: FontWeight.w700),
          ),
          const SizedBox(height: 20),

          // User Card
          if (user != null)
            AppCard(
              padding: const EdgeInsets.all(16),
              onTap: () => context.push('/profile'),
              child: _buildUserCardContent(user, isLight),
            ),
          const SizedBox(height: 24),

          // App Settings Section
          _SectionLabel('应用设置'),
          const SizedBox(height: 8),
          _SettingsItem(
            icon: Icons.dns_outlined,
            title: '服务器管理',
            isLight: isLight,
            onTap: () => context.push('/server-config?manage=1'),
          ),
          if (_serverUrl?.startsWith('http://127.0.0.1:') == true)
            _SettingsItem(
              icon: Icons.open_in_browser_outlined,
              title: '在设备浏览器打开本地面板',
              isLight: isLight,
              onTap: _openBrowserPanel,
            ),
          if (showsOperatorAutomation(user?.role)) ...[
            _SettingsItem(
              icon: Icons.key_outlined,
              title: '环境变量',
              isLight: isLight,
              onTap: () => context.go('/envs'),
            ),
            _SettingsItem(
              icon: Icons.tune_outlined,
              title: '环境变量高级工具',
              isLight: isLight,
              onTap: () => context.push('/env-tools'),
            ),
            if (shows(PanelCapability.scriptExecution))
              _SettingsItem(
                icon: Icons.code,
                title: '脚本管理',
                isLight: isLight,
                onTap: gated(
                  PanelCapability.scriptExecution,
                  () => context.push('/scripts'),
                ),
              ),
            if (shows(PanelCapability.subscriptionPull))
              _SettingsItem(
                icon: Icons.sync,
                title: '订阅管理',
                isLight: isLight,
                onTap: gated(
                  PanelCapability.subscriptionPull,
                  () => context.push('/subscriptions'),
                ),
              ),
          ],
          if (shows(PanelCapability.notificationDispatch))
            _SettingsItem(
              icon: Icons.notifications_none,
              title: '消息通知',
              isLight: isLight,
              onTap: gated(
                PanelCapability.notificationDispatch,
                () => context.push('/notifications'),
              ),
            ),
          _SettingsItem(
            icon: Icons.notifications_active_outlined,
            title: '本地通知',
            isLight: isLight,
            onTap: () => context.push('/local-notifications'),
          ),
          _SettingsItem(
            icon: Icons.lock_outline,
            title: '应用锁',
            isLight: isLight,
            onTap: () => context.push('/app-lock'),
          ),

          if (user != null && user.isAdmin) ...[
            const SizedBox(height: 24),
            _SectionLabel('系统管理'),
            const SizedBox(height: 8),
            if (shows(PanelCapability.dependencyMutation))
              _SettingsItem(
                icon: Icons.inventory_2_outlined,
                title: '依赖管理',
                isLight: isLight,
                onTap: gated(
                  PanelCapability.dependencyMutation,
                  () => context.push('/deps'),
                ),
              ),
            _SettingsItem(
              icon: Icons.people_outline,
              title: '用户管理',
              isLight: isLight,
              onTap: () => context.push('/users'),
            ),
            _SettingsItem(
              icon: Icons.security,
              title: '安全设置',
              isLight: isLight,
              onTap: () => context.push('/security'),
            ),
            _SettingsItem(
              icon: Icons.vpn_key_outlined,
              title: 'SSH 密钥',
              isLight: isLight,
              onTap: () => context.push('/ssh-keys'),
            ),
            if (shows(PanelCapability.platformTokens))
              _SettingsItem(
                icon: Icons.token_outlined,
                title: '平台令牌',
                isLight: isLight,
                onTap: () => context.push('/platform-tokens'),
              ),
            if (shows(PanelCapability.configScript))
              _SettingsItem(
                icon: Icons.terminal_outlined,
                title: '高级配置脚本',
                isLight: isLight,
                onTap: () => context.push('/config-script'),
              ),
            if (shows(PanelCapability.androidRuntime) &&
                shows(PanelCapability.runtimeMutation))
              _SettingsItem(
                icon: Icons.android_outlined,
                title: 'Android 运行时',
                isLight: isLight,
                onTap: gated(
                  PanelCapability.runtimeMutation,
                  () => context.push('/android-runtime'),
                ),
              ),
            if (shows(PanelCapability.installedPackages))
              _SettingsItem(
                icon: Icons.list_alt_outlined,
                title: '系统依赖清单',
                isLight: isLight,
                onTap: () => context.push('/installed-packages'),
              ),
            _SettingsItem(
              icon: Icons.settings,
              title: '系统设置',
              isLight: isLight,
              onTap: () => context.push('/system-settings'),
            ),
            if (shows(PanelCapability.healthCheck))
              _SettingsItem(
                icon: Icons.health_and_safety_outlined,
                title: '系统健康诊断',
                isLight: isLight,
                onTap: () => context.push('/health-check'),
              ),
            _SettingsItem(
              icon: Icons.palette_outlined,
              title: '面板设置',
              isLight: isLight,
              onTap: () => context.push('/panel-settings'),
            ),
            _SettingsItem(
              icon: Icons.article_outlined,
              title: '面板日志',
              isLight: isLight,
              onTap: () => context.push('/panel-log'),
            ),
            if (shows(PanelCapability.backupMutation))
              _SettingsItem(
                icon: Icons.backup_outlined,
                title: '备份与恢复',
                isLight: isLight,
                onTap: gated(
                  PanelCapability.backupMutation,
                  () => context.push('/backup'),
                ),
              ),
            _SettingsItem(
              icon: Icons.api,
              title: 'Open API',
              isLight: isLight,
              onTap: () => context.push('/open-api'),
            ),
          ],

          const SizedBox(height: 24),
          _SectionLabel('其他'),
          const SizedBox(height: 8),

          _SettingsItem(
            icon: Icons.palette_outlined,
            title: '主题设置',
            isLight: isLight,
            onTap: () => context.push('/theme-settings'),
          ),

          _SettingsItem(
            icon: Icons.volunteer_activism_outlined,
            title: '赞助名单',
            isLight: isLight,
            onTap: () => context.push('/sponsor'),
          ),
          _SettingsItem(
            icon: Icons.system_update_outlined,
            title: '检查更新',
            isLight: isLight,
            trailing: _checking
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(
                      strokeWidth: 2,
                      color: AppColors.primary,
                    ),
                  )
                : (_updateInfo?.hasUpdate == true
                      ? Container(
                          width: 8,
                          height: 8,
                          decoration: const BoxDecoration(
                            color: AppColors.red500,
                            shape: BoxShape.circle,
                          ),
                        )
                      : null),
            onTap: () => _checkUpdate(),
          ),
          const SizedBox(height: 6),
          _SettingsItem(
            icon: Icons.info_outline,
            title: '关于',
            isLight: isLight,
            onTap: () => _showAboutDialog(context),
          ),

          // Logout
          const SizedBox(height: 24),
          GestureDetector(
            onTap: () => _logout(context, ref),
            child: AppCard(
              padding: const EdgeInsets.symmetric(vertical: 14),
              child: Center(
                child: Text(
                  '退出登录',
                  style: TextStyle(
                    fontSize: 14,
                    fontWeight: FontWeight.w600,
                    color: AppColors.red500,
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildUserAvatar(dynamic user, double size) {
    final avatarFullUrl = _buildAvatarUrl(user.avatarUrl);
    if (avatarFullUrl != null) {
      return Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          border: Border.all(color: AppColors.primary.withAlpha(40), width: 2),
        ),
        child: ClipOval(
          child: Image.network(
            avatarFullUrl,
            width: size,
            height: size,
            fit: BoxFit.cover,
            headers: {
              'Authorization':
                  'Bearer ${DioClient.instance.dio.options.headers['Authorization']?.toString().replaceFirst('Bearer ', '') ?? ''}',
            },
            errorBuilder: (_, error, stackTrace) =>
                _buildFallbackAvatar(user, size),
          ),
        ),
      );
    }
    return _buildFallbackAvatar(user, size);
  }

  Widget _buildFallbackAvatar(dynamic user, double size) {
    final initial = user.username.isNotEmpty
        ? user.username.substring(0, 1).toUpperCase()
        : '?';
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(
        color: AppColors.primary.withAlpha(25),
        shape: BoxShape.circle,
      ),
      child: Center(
        child: Text(
          initial,
          style: TextStyle(
            fontSize: size * 0.38,
            fontWeight: FontWeight.w700,
            color: AppColors.primary,
          ),
        ),
      ),
    );
  }

  Widget _buildUserCardContent(dynamic user, bool isLight) {
    return Column(
      children: [
        Row(
          children: [
            _buildUserAvatar(user, 48),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    user.username,
                    style: const TextStyle(
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    user.role.toUpperCase(),
                    style: TextStyle(
                      fontSize: 12,
                      color: isLight ? AppColors.slate500 : AppColors.slate400,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
        if (_serverUrl != null) ...[
          const SizedBox(height: 10),
          Row(
            children: [
              Icon(
                Icons.link,
                size: 14,
                color: isLight ? AppColors.slate400 : AppColors.slate500,
              ),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  _serverUrl!
                      .replaceAll('http://', '')
                      .replaceAll('https://', ''),
                  style: TextStyle(
                    fontSize: 12,
                    color: isLight ? AppColors.slate500 : AppColors.slate400,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
        ],
      ],
    );
  }

  Future<void> _logout(BuildContext context, WidgetRef ref) async {
    final confirm = await showDialog<bool>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('退出登录'),
        content: const Text('确定要退出登录吗？'),
        actions: [
          AppLiquidGlassDialogActions(
            actions: [
              AppGlassDialogAction(
                label: '取消',
                onPressed: () => Navigator.pop(dialogCtx, false),
              ),
              AppGlassDialogAction(
                label: '退出',
                variant: AppLiquidGlassButtonVariant.danger,
                onPressed: () => Navigator.pop(dialogCtx, true),
              ),
            ],
          ),
        ],
      ),
    );
    if (confirm == true) {
      await ref.read(authProvider.notifier).logout();
      if (context.mounted) {
        context.go('/server-config?manual=1');
      }
    }
  }

  Future<void> _showAboutDialog(BuildContext context) async {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final packageInfoFuture = PackageInfo.fromPlatform();
    await showDialog<void>(
      context: context,
      builder: (dialogCtx) => AlertDialog(
        title: const Text('关于'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Container(
                    width: 42,
                    height: 42,
                    decoration: BoxDecoration(
                      color: AppColors.primary.withAlpha(20),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: const Icon(
                      Icons.dashboard_customize_outlined,
                      color: AppColors.primary,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        const Text(
                          '呆呆面板',
                          style: TextStyle(
                            fontSize: 16,
                            fontWeight: FontWeight.w700,
                          ),
                        ),
                        const SizedBox(height: 2),
                        FutureBuilder<PackageInfo>(
                          future: packageInfoFuture,
                          builder: (context, snapshot) {
                            final info = snapshot.data;
                            final versionLabel = info == null
                                ? '版本 -'
                                : '版本 ${info.version}${info.buildNumber.trim().isEmpty ? '' : '+${info.buildNumber}'}';
                            return Text(
                              versionLabel,
                              style: const TextStyle(fontSize: 12),
                            );
                          },
                        ),
                      ],
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Text(
                '轻量级定时任务管理平台',
                style: TextStyle(
                  fontSize: 13,
                  color: isLight ? AppColors.slate600 : AppColors.slate300,
                ),
              ),
              const SizedBox(height: 16),
              SizedBox(
                width: double.infinity,
                child: AppLiquidGlassSurface(
                  padding: const EdgeInsets.all(12),
                  borderRadius: 10,
                  performanceMode: true,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '仓库信息',
                        style: TextStyle(
                          fontSize: 13,
                          fontWeight: FontWeight.w700,
                          color: isLight
                              ? AppColors.slate700
                              : AppColors.slate200,
                        ),
                      ),
                      const SizedBox(height: 10),
                      _RepoInfoRow(
                        label: '当前仓库',
                        url: 'github.com/tall-1997/daidai-panel-native',
                        isLight: isLight,
                        icon: Icons.folder_outlined,
                      ),
                      const SizedBox(height: 8),
                      _RepoInfoRow(
                        label: '上游仓库',
                        url: 'github.com/linzixuanzz/Dumb-Panel-APP',
                        isLight: isLight,
                        icon: Icons.arrow_circle_up_outlined,
                      ),
                      const SizedBox(height: 8),
                      _RepoInfoRow(
                        label: '后端仓库',
                        url: 'github.com/linzixuanzz/daidai-panel',
                        isLight: isLight,
                        icon: Icons.dns_outlined,
                      ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 10),
              Text(
                '本应用基于上游项目二次开发，感谢开源社区贡献',
                style: TextStyle(
                  fontSize: 11,
                  color: isLight ? AppColors.slate500 : AppColors.slate500,
                ),
              ),
            ],
          ),
        ),
        actions: [
          SizedBox(
            width: double.infinity,
            height: 44,
            child: FilledButton(
              onPressed: () => Navigator.pop(dialogCtx),
              child: const Text('知道了'),
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionLabel extends StatelessWidget {
  final String text;
  const _SectionLabel(this.text);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(left: 2),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w600,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
          letterSpacing: 0.5,
        ),
      ),
    );
  }
}

class _SettingsItem extends ConsumerWidget {
  final IconData icon;
  final String title;
  final bool isLight;
  final VoidCallback? onTap;
  final Widget? trailing;

  const _SettingsItem({
    required this.icon,
    required this.title,
    required this.isLight,
    required this.onTap,
    this.trailing,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final enabled = onTap != null;
    final rowContent = Row(
      children: [
        Icon(
          icon,
          size: 20,
          color: enabled
              ? (isLight ? AppColors.slate500 : AppColors.slate400)
              : AppColors.slate400.withAlpha(100),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Text(
            title,
            style: TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w500,
              color: enabled ? null : AppColors.slate400,
            ),
          ),
        ),
        if (trailing != null) ...[trailing!, const SizedBox(width: 8)],
        Icon(
          Icons.chevron_right,
          size: 18,
          color: isLight ? AppColors.slate400 : AppColors.slate600,
        ),
      ],
    );

    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: AppLiquidGlassSurface(
        onTap: onTap,
        borderRadius: 16,
        performanceMode: true,
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
        child: rowContent,
      ),
    );
  }
}

class _RepoInfoRow extends StatelessWidget {
  final String label;
  final String url;
  final bool isLight;
  final IconData icon;

  const _RepoInfoRow({
    required this.label,
    required this.url,
    required this.isLight,
    required this.icon,
  });

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(
          icon,
          size: 15,
          color: isLight ? AppColors.slate400 : AppColors.slate500,
        ),
        const SizedBox(width: 8),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                label,
                style: TextStyle(
                  fontSize: 11,
                  fontWeight: FontWeight.w600,
                  color: isLight ? AppColors.slate500 : AppColors.slate400,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                url,
                style: TextStyle(
                  fontSize: 12,
                  fontWeight: FontWeight.w500,
                  fontFamily: 'monospace',
                  color: isLight ? AppColors.slate700 : AppColors.slate200,
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
