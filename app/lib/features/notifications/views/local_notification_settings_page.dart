import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../../../core/services/local_notification_service.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';

class LocalNotificationSettingsPage extends ConsumerStatefulWidget {
  const LocalNotificationSettingsPage({super.key});

  @override
  ConsumerState<LocalNotificationSettingsPage> createState() =>
      _LocalNotificationSettingsPageState();
}

class _LocalNotificationSettingsPageState
    extends ConsumerState<LocalNotificationSettingsPage> {
  final _service = LocalNotificationService();
  bool _taskEnabled = true;
  bool _systemEnabled = true;
  bool _loading = true;
  bool _permissionGranted = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final task = await _service.getChannelEnabled(NotificationChannel.task);
    final system =
        await _service.getChannelEnabled(NotificationChannel.system);
    final granted = await _checkPermission();
    if (!mounted) return;
    setState(() {
      _taskEnabled = task;
      _systemEnabled = system;
      _permissionGranted = granted;
      _loading = false;
    });
  }

  Future<void> _toggle(NotificationChannel channel, bool value) async {
    setState(() {
      if (channel == NotificationChannel.task) {
        _taskEnabled = value;
      } else {
        _systemEnabled = value;
      }
    });
    await _service.setChannelEnabled(channel, value);
  }

  Future<void> _test(NotificationChannel channel) async {
    await _service.showTestNotification(channel);
  }

  Future<void> _requestPermission() async {
    final granted = await _service.requestPermissions();
    if (!mounted) return;
    setState(() {
      _permissionGranted = granted;
    });
    AppGlassNotice.show(
      context,
      granted ? '通知权限已开启' : '通知权限未开启，请在系统设置中打开',
      type: granted
          ? AppGlassNoticeType.success
          : AppGlassNoticeType.warning,
    );
  }

  Future<bool> _checkPermission() async {
    return await _service.areNotificationsEnabled();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;

    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(
        title: const Text('本地通知'),
      ),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          AppCard(
            padding: const EdgeInsets.all(16),
            child: _buildPermissionCard(isLight),
          ),
          const SizedBox(height: 16),
          Text(
            '通知渠道',
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w600,
              color: isLight ? AppColors.slate500 : AppColors.slate400,
              letterSpacing: 0.5,
            ),
          ),
          const SizedBox(height: 10),
          if (_loading)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(32),
                child: CircularProgressIndicator(color: AppColors.primary),
              ),
            )
          else ...[
            _ChannelTile(
              icon: Icons.schedule,
              iconColor: AppColors.blue500,
               iconBg: AppColors.blue500.withAlpha(20),
              iconBgDark: AppColors.blue500.withAlpha(25),
              title: '任务通知',
              subtitle: '任务执行完成或失败时推送本地通知',
              enabled: _taskEnabled,
              isLight: isLight,
              onToggle: (v) => _toggle(NotificationChannel.task, v),
              onTest: () => _test(NotificationChannel.task),
            ),
            const SizedBox(height: 8),
            _ChannelTile(
              icon: Icons.info_outline,
              iconColor: AppColors.amber500,
              iconBg: AppColors.amber500.withAlpha(25),
              iconBgDark: AppColors.amber500.withAlpha(20),
              title: '系统通知',
              subtitle: '面板系统事件和安全相关本地通知',
              enabled: _systemEnabled,
              isLight: isLight,
              onToggle: (v) => _toggle(NotificationChannel.system, v),
              onTest: () => _test(NotificationChannel.system),
            ),
          ],
        ],
      ),
    );
  }

  Widget _buildPermissionCard(bool isLight) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          '通知权限',
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: isLight ? AppColors.slate500 : AppColors.slate400,
          ),
        ),
        const SizedBox(height: 12),
        Row(
          children: [
            Icon(
              _permissionGranted ? Icons.check_circle : Icons.cancel,
              size: 20,
              color: _permissionGranted ? AppColors.primary : AppColors.red500,
            ),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                _permissionGranted ? '通知权限已授予' : '通知权限未授予',
                style: TextStyle(
                  fontSize: 14,
                  color: _permissionGranted
                      ? (isLight ? AppColors.slate700 : AppColors.slate200)
                      : AppColors.red500,
                ),
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        SizedBox(
          width: double.infinity,
          height: 44,
          child: AppLiquidGlassButton(
            label: '请求通知权限',
            icon: Icons.security,
            onPressed: _requestPermission,
            width: double.infinity,
            height: 44,
            performanceMode: true,
          ),
        ),
      ],
    );
  }
}

class _ChannelTile extends ConsumerWidget {
  final IconData icon;
  final Color iconColor;
  final Color iconBg;
  final Color iconBgDark;
  final String title;
  final String subtitle;
  final bool enabled;
  final bool isLight;
  final ValueChanged<bool> onToggle;
  final VoidCallback onTest;

  const _ChannelTile({
    required this.icon,
    required this.iconColor,
    required this.iconBg,
    required this.iconBgDark,
    required this.title,
    required this.subtitle,
    required this.enabled,
    required this.isLight,
    required this.onToggle,
    required this.onTest,
  });

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    

    final content = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Container(
              width: 40,
              height: 40,
              decoration: BoxDecoration(
                color: isLight ? iconBg : iconBgDark,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Icon(icon, size: 20, color: iconColor),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title,
                      style: const TextStyle(
                          fontSize: 15, fontWeight: FontWeight.w600)),
                  const SizedBox(height: 2),
                  Text(subtitle,
                      style: TextStyle(
                          fontSize: 12,
                          color: isLight
                              ? AppColors.slate500
                              : AppColors.slate400)),
                ],
              ),
            ),
            AppLiquidGlassToggle(
              value: enabled,
              activeColor: AppColors.primary,
              onChanged: onToggle,
            ),
          ],
        ),
        const SizedBox(height: 14),
        SizedBox(
          width: double.infinity,
          height: 40,
          child: AppLiquidGlassButton(
            label: '发送测试通知',
            icon: Icons.send,
            onPressed: onTest,
            width: double.infinity,
            height: 40,
            variant: AppLiquidGlassButtonVariant.secondary,
            performanceMode: true,
          ),
        ),
      ],
    );

    return AppCard(padding: const EdgeInsets.all(16), child: content);
  }
}
