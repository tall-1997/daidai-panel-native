import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../../../core/auth/auth_provider.dart';
import '../../../core/auth/auth_service.dart';
import '../../../core/local_panel/local_panel_models.dart';
import '../../../core/local_panel/method_channel_local_panel_host.dart';
import '../../../core/local_panel/local_panel_session_resolver.dart';
import '../../../core/local_panel/managed_local_connection_monitor.dart';
import '../../../core/storage/secure_storage.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/theme/app_theme.dart';
import '../../../shared/widgets/app_card.dart';
import '../../../shared/utils/api_utils.dart';
import '../../dashboard/providers/dashboard_provider.dart';
import '../widgets/geetest_captcha_dialog.dart';

class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({super.key});

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends ConsumerState<LoginPage> {
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  final _totpController = TextEditingController();
  final _serverUrlController = TextEditingController();
  final _formKey = GlobalKey<FormState>();

  bool _loading = false;
  bool _obscurePassword = true;
  bool _needsInit = false;
  bool _needsTotp = false;
  bool _rememberPassword = false;
  bool _autoLogin = false;
  bool _needsServerUrl = false;
  String? _error;
  String? _currentUrl;
  List<PanelConfig> _panels = [];
  PanelConfig? _selectedPanel;

  Future<Map<String, dynamic>?> _prepareCaptchaIfEnabled(
    String username,
  ) async {
    final config = await ref
        .read(authProvider.notifier)
        .captchaConfig(username: username);
    final enabled = config['enabled'] == true;
    if (!enabled) {
      return null;
    }

    final captchaId = config['captcha_id']?.toString().trim() ?? '';
    if (captchaId.isEmpty || config['configured'] == false) {
      throw _LoginFlowMessage(
        config['message']?.toString() ?? '验证码未配置完整，请先在 Web 端检查极验配置',
      );
    }

    if (!mounted) {
      throw const _LoginFlowMessage('登录页面已关闭');
    }
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      barrierDismissible: false,
      builder: (_) => GeeTestCaptchaDialog(captchaId: captchaId),
    );
    if (result == null) {
      throw _LoginFlowMessage('请先完成滑块验证');
    }
    return result;
  }

  @override
  void initState() {
    super.initState();
    _initCheck();
  }

  Future<void> _initCheck() async {
    final serverUrl = await SecureStorage.getServerUrl();
    if (serverUrl == null || serverUrl.isEmpty) {
      // 没有已保存的服务器地址，显示服务器地址输入框
      _needsServerUrl = true;
      _serverUrlController.text = '127.0.0.1:5700';
      _panels = await SecureStorage.getPanels();
      if (mounted) setState(() {});
      return;
    }
    _currentUrl = serverUrl;

    _panels = await SecureStorage.getPanels();
    _selectedPanel = _panels.where((p) => p.url == serverUrl).firstOrNull;
    var managedLocalHealthy = true;
    if (Platform.isAndroid && _selectedPanel?.type == PanelType.managedLocal) {
      try {
        final status = await MethodChannelLocalPanelHost().ensureStarted();
        final resolved = resolveManagedLocalPanel(
          status,
          existing: _selectedPanel,
        );
        _selectedPanel = resolved.panel;
        _currentUrl = resolved.panel.url;
        final adopted = await ManagedLocalConnectionMonitor.instance.adoptHealthy(
          resolved,
        );
        if (!adopted) throw StateError('Remote transition in progress');
      } catch (_) {
        managedLocalHealthy = false;
        _error = '本地面板正在恢复，请稍后重试';
        await ManagedLocalConnectionMonitor.instance.startManaged(
          _selectedPanel!,
          reconcileImmediately: false,
        );
      }
    } else {
      await ManagedLocalConnectionMonitor.instance.stopAndDrain(
        remoteCommit: () async => DioClient.instance.setBaseUrl(serverUrl),
      );
    }

    if (_selectedPanel != null) {
      _rememberPassword = _selectedPanel!.rememberPassword;
      _autoLogin =
          _selectedPanel!.rememberPassword && _selectedPanel!.autoLogin;
      if (_selectedPanel!.rememberPassword &&
          _selectedPanel!.username != null) {
        _usernameController.text = _selectedPanel!.username!;
        if (_selectedPanel!.password != null) {
          _passwordController.text = _selectedPanel!.password!;
        }
      }
    }

    if (mounted) setState(() {});

    if (managedLocalHealthy &&
        ref.read(authProvider).status == AuthStatus.authenticated) {
      if (mounted) {
        context.go('/dashboard');
      }
      return;
    }

    try {
      final auth = ref.read(authProvider.notifier);
      await auth.checkInit();
      final authState = ref.read(authProvider);
      if (mounted) setState(() => _needsInit = authState.needsInit);
    } catch (_) {}
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) return;

    setState(() {
      _loading = true;
      _error = null;
    });

    try {
      // 需要先配置服务器地址
      if (_needsServerUrl) {
        final rawUrl = _serverUrlController.text.trim();
        if (rawUrl.isEmpty) {
          setState(() {
            _error = '请输入服务器地址';
            _loading = false;
          });
          return;
        }

        var finalUrl = rawUrl;
        if (!finalUrl.toLowerCase().startsWith('http')) {
          finalUrl = '${_defaultSchemeForServerUrl(finalUrl)}://$finalUrl';
        }
        if (finalUrl.endsWith('/')) {
          finalUrl = finalUrl.substring(0, finalUrl.length - 1);
        }

        final authService = AuthService();
        final ok = await authService.checkHealth(finalUrl);
        if (!mounted) return;
        if (!ok) {
          setState(() {
            _error = '无法连接到服务器 ($finalUrl)，请检查地址是否正确、面板是否运行中。'
                '如果使用群晖/飞牛等 NAS 的 Nginx Proxy Manager 或公网域名反代，'
                '请升级面板到 v2.3.0 以上；升级前可在 config.yaml 的 cors.origins 中加入完整公网地址。';
            _loading = false;
          });
          return;
        }

        _currentUrl = finalUrl;
        await ManagedLocalConnectionMonitor.instance.stopAndDrain(
          remoteCommit: () async {
            DioClient.instance.setBaseUrl(finalUrl);
            await SecureStorage.activatePanel(
              PanelConfig(url: finalUrl, name: finalUrl),
            );
          },
        );
        _needsServerUrl = false;
      }

      final auth = ref.read(authProvider.notifier);
      final initializing = _needsInit;

      if (initializing) {
        try {
          await auth.initAdmin(
            _usernameController.text.trim(),
            _passwordController.text,
          );
        } catch (error) {
          if (!mounted) return;
          setState(() {
            _needsInit = true;
            _error = extractErrorMessage(error, '初始化失败，请检查输入后重试');
            _loading = false;
          });
          return;
        }
        if (!mounted) return;
        setState(() => _needsInit = false);
      }

      final captcha = initializing
          ? null
          : await _prepareCaptchaIfEnabled(_usernameController.text.trim());
      if (!mounted) {
        return;
      }

      final result = await auth.login(
        username: _usernameController.text.trim(),
        password: _passwordController.text,
        totpCode: _needsTotp ? _totpController.text.trim() : null,
        captcha: captcha,
      );
      if (!mounted) return;

      if (result['two_factor_required'] == true) {
        setState(() {
          _needsTotp = true;
          _error = result['error']?.toString() ?? '请输入两步验证码';
          _loading = false;
        });
        return;
      }

      if (result['captcha_required'] == true) {
        setState(() {
          _error = result['error']?.toString() ?? '验证码已失效，请重新完成滑块验证';
          _loading = false;
        });
        return;
      }

      // 保存偏好
      if (_currentUrl != null) {
        await SecureStorage.savePanel(
          PanelConfig(
            url: _currentUrl!,
            name: _selectedPanel?.name.isNotEmpty == true
                ? _selectedPanel!.name
                : _currentUrl!,
            username: _rememberPassword
                ? _usernameController.text.trim()
                : null,
            password: _rememberPassword ? _passwordController.text : null,
            rememberPassword: _rememberPassword,
            autoLogin: _rememberPassword && _autoLogin,
            type: _selectedPanel?.type ?? PanelType.remote,
            instanceId: _selectedPanel?.instanceId ?? '',
          ),
        );
      }

      try {
        ref.invalidate(dashboardProvider);
        await ref.read(dashboardProvider.notifier).load();
      } catch (_) {}

      if (mounted) {
        context.go('/dashboard');
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = e is _LoginFlowMessage
            ? e.message
            : ref.read(authProvider).error ?? '登录失败';
      });
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _switchPanel(PanelConfig panel) async {
    var selected = panel;
    if (Platform.isAndroid && panel.type == PanelType.managedLocal) {
      final status = await MethodChannelLocalPanelHost().ensureStarted();
      final resolved = resolveManagedLocalPanel(status, existing: panel);
      selected = resolved.panel;
      final adopted = await ManagedLocalConnectionMonitor.instance.adoptHealthy(
        resolved,
      );
      if (!adopted) throw StateError('Remote transition in progress');
    } else {
      final previousUrl = DioClient.instance.baseUrl;
      await ManagedLocalConnectionMonitor.instance.stopAndDrain(
        remoteCommit: () async {
          DioClient.instance.setBaseUrl(selected.url);
          try {
            await SecureStorage.activatePanel(selected);
          } catch (_) {
            DioClient.instance.setBaseUrl(previousUrl);
            rethrow;
          }
        },
      );
    }
    if (!mounted) return;
    setState(() {
      _selectedPanel = selected;
      _currentUrl = selected.url;
      _rememberPassword = selected.rememberPassword;
      _autoLogin = selected.rememberPassword && selected.autoLogin;
      if (selected.rememberPassword && selected.username != null) {
        _usernameController.text = selected.username!;
        _passwordController.text = selected.password ?? '';
      } else {
        _usernameController.clear();
        _passwordController.clear();
      }
    });
  }

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    _totpController.dispose();
    _serverUrlController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final isLight = theme.brightness == Brightness.light;

    return Scaffold(
      body: Container(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topCenter,
            end: Alignment.bottomCenter,
            colors: isLight
                ? [Colors.white, AppColors.slate50]
                : [AppColors.slate900, AppColors.slate950],
          ),
        ),
        child: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.symmetric(horizontal: 24),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 420),
                child: Form(
                  key: _formKey,
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      // Logo
                      Container(
                        width: 56,
                        height: 56,
                        decoration: BoxDecoration(
                          color: AppColors.primary.withAlpha(25),
                          borderRadius: BorderRadius.circular(16),
                        ),
                        child: ClipRRect(
                          borderRadius: BorderRadius.circular(12),
                          child: Image.asset(
                            'assets/icon.png',
                            width: 40,
                            height: 40,
                            fit: BoxFit.cover,
                          ),
                        ),
                      ),
                      const SizedBox(height: 20),

                      // Title
                      Text(
                        _needsInit ? '初始化面板' : '欢迎回来',
                        style: const TextStyle(
                          fontSize: 28,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 6),
                      Text(
                        _needsInit
                            ? '首次使用，请创建管理员账号。'
                            : '登录 Daidai Panel 管理您的服务器与任务。',
                        style: TextStyle(
                          fontSize: 13,
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                      const SizedBox(height: 32),

                      // Server URL input (when no saved server)
                      if (_needsServerUrl) ...[
                        _FieldLabel('服务器地址'),
                        const SizedBox(height: 6),
                        _IconInput(
                          icon: Icons.dns_outlined,
                          child: TextFormField(
                            controller: _serverUrlController,
                            decoration: const InputDecoration(
                              hintText: '127.0.0.1:5700',
                              border: InputBorder.none,
                              enabledBorder: InputBorder.none,
                              focusedBorder: InputBorder.none,
                              contentPadding: EdgeInsets.zero,
                              isDense: true,
                            ),
                            textAlignVertical: TextAlignVertical.center,
                            keyboardType: TextInputType.url,
                            textInputAction: TextInputAction.next,
                            validator: (v) => v == null || v.trim().isEmpty
                                ? '请输入服务器地址'
                                : null,
                          ),
                        ),
                        const SizedBox(height: 16),
                      ],

                      // Server selector
                      if (_panels.isNotEmpty && !_needsInit && !_needsServerUrl) ...[
                        _FieldLabel('服务器'),
                        const SizedBox(height: 6),
                        _IconInput(
                          icon: Icons.dns_outlined,
                          child: DropdownButtonHideUnderline(
                            child: DropdownButton<String>(
                              value: _currentUrl,
                              isExpanded: true,
                              isDense: false,
                              icon: Icon(
                                Icons.expand_more,
                                size: 20,
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                              style: TextStyle(
                                fontSize: 14,
                                color: theme.colorScheme.onSurface,
                              ),
                              items: _panels.map((p) {
                                return DropdownMenuItem(
                                  value: p.url,
                                  child: Text(
                                    '${p.name.isNotEmpty ? p.name : "面板"} (${_shortUrl(p.url)})',
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                );
                              }).toList(),
                              onChanged: (url) async {
                                final panel = _panels
                                    .where((p) => p.url == url)
                                    .first;
                                await _switchPanel(panel);
                              },
                            ),
                          ),
                        ),
                        const SizedBox(height: 16),
                      ],

                      // Username
                      _FieldLabel('用户名'),
                      const SizedBox(height: 6),
                      _IconInput(
                        icon: Icons.person_outline,
                        child: TextFormField(
                          controller: _usernameController,
                          decoration: const InputDecoration(
                            hintText: 'admin',
                            border: InputBorder.none,
                            enabledBorder: InputBorder.none,
                            focusedBorder: InputBorder.none,
                            contentPadding: EdgeInsets.zero,
                            isDense: true,
                          ),
                          textAlignVertical: TextAlignVertical.center,
                          textInputAction: TextInputAction.next,
                          validator: (v) =>
                              v == null || v.trim().isEmpty ? '请输入用户名' : null,
                        ),
                      ),
                      const SizedBox(height: 16),

                      // Password
                      _FieldLabel('密码'),
                      const SizedBox(height: 6),
                      _IconInput(
                        icon: Icons.lock_outline,
                        suffix: GestureDetector(
                          onTap: () => setState(
                            () => _obscurePassword = !_obscurePassword,
                          ),
                          child: Icon(
                            _obscurePassword
                                ? Icons.visibility_off_outlined
                                : Icons.visibility_outlined,
                            size: 20,
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                        ),
                        child: TextFormField(
                          controller: _passwordController,
                          decoration: const InputDecoration(
                            hintText: '••••••••',
                            border: InputBorder.none,
                            enabledBorder: InputBorder.none,
                            focusedBorder: InputBorder.none,
                            contentPadding: EdgeInsets.zero,
                            isDense: true,
                          ),
                          textAlignVertical: TextAlignVertical.center,
                          obscureText: _obscurePassword,
                          textInputAction: _needsTotp
                              ? TextInputAction.next
                              : TextInputAction.go,
                          onFieldSubmitted: _needsTotp
                              ? null
                              : (_) => _submit(),
                          validator: (v) =>
                              v == null || v.isEmpty ? '请输入密码' : null,
                        ),
                      ),

                      // TOTP
                      if (_needsTotp) ...[
                        const SizedBox(height: 16),
                        _FieldLabel('两步验证码'),
                        const SizedBox(height: 6),
                        _IconInput(
                          icon: Icons.security,
                          suffix: SizedBox(
                            width: 40,
                            child: Text(
                              '${_totpController.text.trim().length}/6',
                              textAlign: TextAlign.right,
                              style: TextStyle(
                                fontSize: 12,
                                color: theme.colorScheme.onSurfaceVariant,
                              ),
                            ),
                          ),
                          child: TextFormField(
                            controller: _totpController,
                            decoration: const InputDecoration(
                              hintText: '6位数字验证码',
                              border: InputBorder.none,
                              enabledBorder: InputBorder.none,
                              focusedBorder: InputBorder.none,
                              contentPadding: EdgeInsets.zero,
                              isDense: true,
                            ),
                            textAlignVertical: TextAlignVertical.center,
                            keyboardType: TextInputType.number,
                            maxLength: 6,
                            buildCounter:
                                (
                                  BuildContext context, {
                                  required int currentLength,
                                  required bool isFocused,
                                  required int? maxLength,
                                }) => null,
                            textInputAction: TextInputAction.go,
                            onChanged: (_) => setState(() {}),
                            onFieldSubmitted: (_) => _submit(),
                            validator: (v) {
                              if (v == null || v.trim().isEmpty) {
                                return '请输入验证码';
                              }
                              if (v.trim().length != 6) return '验证码为6位数字';
                              return null;
                            },
                          ),
                        ),
                      ],

                      // Remember + AutoLogin
                      const SizedBox(height: 8),
                      Wrap(
                        alignment: WrapAlignment.center,
                        spacing: 16,
                        runSpacing: 8,
                        children: [
                          _CompactCheck(
                            value: _rememberPassword,
                            label: '记住密码',
                            onChanged: (v) {
                              setState(() {
                                _rememberPassword = v;
                                if (!v) _autoLogin = false;
                              });
                            },
                          ),
                          const SizedBox(width: 16),
                          _CompactCheck(
                            value: _autoLogin,
                            label: '自动登录',
                            enabled: _rememberPassword,
                            onChanged: (v) => setState(() => _autoLogin = v),
                          ),
                        ],
                      ),

                      // Error
                      if (_error != null) ...[
                        const SizedBox(height: 12),
                        Container(
                          width: double.infinity,
                          padding: const EdgeInsets.symmetric(
                            horizontal: 12,
                            vertical: 10,
                          ),
                          decoration: BoxDecoration(
                            color: AppColors.red500.withAlpha(15),
                            borderRadius: BorderRadius.circular(10),
                            border: Border.all(
                              color: AppColors.red500.withAlpha(40),
                            ),
                          ),
                          child: Text(
                            _error!,
                            style: const TextStyle(
                              color: AppColors.red500,
                              fontSize: 13,
                            ),
                            textAlign: TextAlign.center,
                          ),
                        ),
                      ],

                      // Login Button
                      const SizedBox(height: 24),
                      AppLiquidGlassButton(
                        label: _needsInit ? '创建并登录' : '连接并登录',
                        onPressed: _loading ? null : _submit,
                        width: double.infinity,
                        height: 52,
                        loading: _loading,
                      ),

                      // Switch panel
                      const SizedBox(height: 16),
                      Center(
                        child: TextButton.icon(
                          onPressed: () =>
                              context.go('/server-config?manual=1'),
                          icon: Icon(
                            Icons.swap_horiz,
                            size: 18,
                            color: theme.colorScheme.onSurfaceVariant,
                          ),
                          label: Text(
                            '管理面板',
                            style: TextStyle(
                              color: theme.colorScheme.onSurfaceVariant,
                              fontSize: 13,
                            ),
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  String _shortUrl(String url) {
    return url.replaceAll('http://', '').replaceAll('https://', '');
  }

  String _defaultSchemeForServerUrl(String rawUrl) {
    final trimmed = rawUrl.trim().toLowerCase();
    if (trimmed == '::1' || trimmed.startsWith('[::1]')) {
      return 'http';
    }
    final host = rawUrl.split('/').first.split(':').first.trim().toLowerCase();
    if (host == 'localhost' || host == '127.0.0.1') {
      return 'http';
    }
    final parts = host.split('.');
    if (parts.length == 4) {
      final first = int.tryParse(parts[0]);
      final second = int.tryParse(parts[1]);
      if (first == 10 || (first == 192 && second == 168)) {
        return 'http';
      }
      if (first == 172 && second != null && second >= 16 && second <= 31) {
        return 'http';
      }
    }
    return 'https';
  }
}

class _LoginFlowMessage implements Exception {
  final String message;

  const _LoginFlowMessage(this.message);
}

class _FieldLabel extends StatelessWidget {
  final String text;
  const _FieldLabel(this.text);

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(left: 2),
      child: Text(
        text,
        style: TextStyle(
          fontSize: 12,
          fontWeight: FontWeight.w500,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

class _IconInput extends StatelessWidget {
  final IconData icon;
  final Widget child;
  final Widget? suffix;

  const _IconInput({required this.icon, required this.child, this.suffix});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      height: 52,
      child: AppLiquidGlassSurface(
        borderRadius: 14,
        padding: const EdgeInsets.symmetric(horizontal: 14),
        child: Row(
          children: [
            Icon(icon, size: 20, color: AppColors.slate400),
            const SizedBox(width: 12),
            Expanded(child: child),
            if (suffix != null) ...[const SizedBox(width: 8), suffix!],
          ],
        ),
        ),
    );
  }
}

class _CompactCheck extends StatelessWidget {
  final bool value;
  final String label;
  final bool enabled;
  final ValueChanged<bool> onChanged;

  const _CompactCheck({
    required this.value,
    required this.label,
    this.enabled = true,
    required this.onChanged,
  });

  @override
  Widget build(BuildContext context) {
    final isLight = Theme.of(context).brightness == Brightness.light;
    final color = enabled
        ? Theme.of(context).colorScheme.onSurface
        : Theme.of(context).disabledColor;

    return GestureDetector(
      onTap: enabled ? () => onChanged(!value) : null,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox(
            width: 28,
            height: 28,
            child: Checkbox(
              value: value,
              onChanged: enabled ? (v) => onChanged(v ?? false) : null,
              activeColor: AppColors.primary,
              checkColor: Colors.white,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(4),
              ),
              side: BorderSide(
                color: isLight ? AppColors.slate300 : AppColors.slate600,
                width: 1.5,
              ),
            ),
          ),
          const SizedBox(width: 8),
          Text(label, style: TextStyle(fontSize: 13, color: color)),
        ],
      ),
    );
  }
}
