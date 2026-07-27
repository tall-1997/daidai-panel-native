import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/auth/auth_provider.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/utils/api_utils.dart';
import '../../../shared/widgets/app_card.dart';

class ProfilePage extends ConsumerStatefulWidget {
  const ProfilePage({super.key});

  @override
  ConsumerState<ProfilePage> createState() => _ProfilePageState();
}

class _ProfilePageState extends ConsumerState<ProfilePage> {
  final _username = TextEditingController();
  final _oldPassword = TextEditingController();
  final _newPassword = TextEditingController();
  final _confirmPassword = TextEditingController();
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _username.text = ref.read(authProvider).user?.username ?? '';
  }

  @override
  void dispose() {
    _username.dispose();
    _oldPassword.dispose();
    _newPassword.dispose();
    _confirmPassword.dispose();
    super.dispose();
  }

  String? _avatarUrl(String? path) {
    if (path == null || path.isEmpty) return null;
    return path.startsWith('http') ? path : '${DioClient.instance.baseUrl}$path';
  }

  Future<void> _run(Future<void> Function() action, String success) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await action();
      if (!mounted) return;
      AppGlassNotice.show(context, success, type: AppGlassNoticeType.success);
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

  Future<void> _pickAvatar() async {
    final result = await FilePicker.platform.pickFiles(
      type: FileType.custom,
      allowedExtensions: const ['jpg', 'jpeg', 'png', 'gif', 'webp'],
      withData: true,
    );
    final file = result?.files.single;
    if (file == null) return;
    if (file.size > 2 * 1024 * 1024) {
      if (mounted) AppGlassNotice.show(context, '头像文件不能超过 2MB');
      return;
    }
    final multipart = file.path != null
        ? await MultipartFile.fromFile(file.path!, filename: file.name)
        : MultipartFile.fromBytes(file.bytes!, filename: file.name);
    await _run(
      () => ref.read(authProvider.notifier).uploadAvatar(multipart),
      '头像已更新',
    );
  }

  Future<void> _changeUsername() async {
    final value = _username.text.trim();
    if (!RegExp(r'^[\p{L}\p{N}_]{1,32}$', unicode: true).hasMatch(value)) {
      AppGlassNotice.show(context, '用户名需 1-32 位，支持中文、字母、数字和下划线');
      return;
    }
    await _run(
      () => ref.read(authProvider.notifier).changeUsername(value),
      '用户名已修改，请重新登录',
    );
    if (mounted && ref.read(authProvider).status == AuthStatus.unauthenticated) {
      context.go('/login?manual=1');
    }
  }

  Future<void> _changePassword() async {
    if (_newPassword.text.length < 6 || _newPassword.text.length > 128) {
      AppGlassNotice.show(context, '新密码长度需 6-128 位');
      return;
    }
    if (_newPassword.text != _confirmPassword.text) {
      AppGlassNotice.show(context, '两次输入的新密码不一致');
      return;
    }
    await _run(
      () => ref
          .read(authProvider.notifier)
          .changePassword(_oldPassword.text, _newPassword.text),
      '密码已修改，请重新登录',
    );
    if (mounted && ref.read(authProvider).status == AuthStatus.unauthenticated) {
      context.go('/login?manual=1');
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authProvider).user;
    final avatarUrl = _avatarUrl(user?.avatarUrl);
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: const Text('个人资料')),
      body: ListView(
        padding: const EdgeInsets.fromLTRB(20, 16, 20, 40),
        children: [
          AppCard(
            child: Column(
              children: [
                CircleAvatar(
                  radius: 42,
                  backgroundImage: avatarUrl == null ? null : NetworkImage(avatarUrl),
                  child: user?.avatarUrl == null
                      ? const Icon(Icons.person, size: 38)
                      : null,
                ),
                const SizedBox(height: 12),
                Wrap(
                  spacing: 8,
                  children: [
                    AppLiquidGlassButton(label: '更换头像', onPressed: _busy ? null : _pickAvatar, height: 42),
                    AppLiquidGlassButton(
                      label: '删除头像',
                      onPressed: _busy ? null : () => _run(() => ref.read(authProvider.notifier).deleteAvatar(), '头像已删除'),
                      height: 42,
                      variant: AppLiquidGlassButtonVariant.danger,
                    ),
                  ],
                ),
              ],
            ),
          ),
          const SizedBox(height: 14),
          AppCard(
            child: Column(
              children: [
                TextField(controller: _username, decoration: const InputDecoration(labelText: '用户名')),
                const SizedBox(height: 12),
                AppLiquidGlassButton(label: '修改用户名', onPressed: _busy ? null : _changeUsername),
              ],
            ),
          ),
          const SizedBox(height: 14),
          AppCard(
            child: Column(
              children: [
                TextField(controller: _oldPassword, obscureText: true, decoration: const InputDecoration(labelText: '当前密码')),
                const SizedBox(height: 10),
                TextField(controller: _newPassword, obscureText: true, decoration: const InputDecoration(labelText: '新密码')),
                const SizedBox(height: 10),
                TextField(controller: _confirmPassword, obscureText: true, decoration: const InputDecoration(labelText: '确认新密码')),
                const SizedBox(height: 12),
                AppLiquidGlassButton(label: '修改密码', onPressed: _busy ? null : _changePassword),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
