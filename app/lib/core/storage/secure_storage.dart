import 'dart:convert';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../../shared/models/user.dart';

const Object _panelFieldUnset = Object();

enum PanelType { remote, managedLocal }

/// 面板配置信息
class PanelConfig {
  final String url;
  final String name;
  final String? username;
  final String? password;
  final bool rememberPassword;
  final bool autoLogin;
  final PanelType type;
  final String instanceId;

  const PanelConfig({
    required this.url,
    this.name = '',
    this.username,
    this.password,
    this.rememberPassword = false,
    this.autoLogin = false,
    this.type = PanelType.remote,
    this.instanceId = '',
  });

  Map<String, dynamic> toJson() => {
    'url': url,
    'name': name.isEmpty ? url : name,
    'username': username,
    'password': password,
    'rememberPassword': rememberPassword,
    'autoLogin': autoLogin,
    'type': type.name,
    'instanceId': instanceId,
  };

  factory PanelConfig.fromJson(Map<String, dynamic> json) => PanelConfig(
    url: json['url'] as String,
    name: json['name'] as String? ?? '',
    username: json['username'] as String?,
    password: json['password'] as String?,
    rememberPassword: json['rememberPassword'] as bool? ?? false,
    autoLogin: json['autoLogin'] as bool? ?? false,
    type: PanelType.values.firstWhere(
      (type) => type.name == json['type']?.toString(),
      orElse: () => PanelType.remote,
    ),
    instanceId: json['instanceId']?.toString() ?? '',
  );

  PanelConfig copyWith({
    String? url,
    String? name,
    Object? username = _panelFieldUnset,
    Object? password = _panelFieldUnset,
    bool? rememberPassword,
    bool? autoLogin,
    PanelType? type,
    String? instanceId,
  }) {
    return PanelConfig(
      url: url ?? this.url,
      name: name ?? this.name,
      username: identical(username, _panelFieldUnset)
          ? this.username
          : username as String?,
      password: identical(password, _panelFieldUnset)
          ? this.password
          : password as String?,
      rememberPassword: rememberPassword ?? this.rememberPassword,
      autoLogin: autoLogin ?? this.autoLogin,
      type: type ?? this.type,
      instanceId: instanceId ?? this.instanceId,
    );
  }

  PanelConfig sanitizedForStorage() => this;
}

class SecureStorage {
  static const _storage = FlutterSecureStorage();

  static const _accessTokenKey = 'access_token';
  static const _refreshTokenKey = 'refresh_token';
  static const _trustedLoginUntilKey = 'trusted_login_until';
  static const _trustedLoginServerUrlKey = 'trusted_login_server_url';
  static const _serverUrlKey = 'server_url';
  static const _serverListKey = 'server_list';
  static const _panelsKey = 'panels_config';
  static const _userKey = 'auth_user';
  static const _appLockConfigKey = 'app_lock_config';
  static const _prefsNamespaceKey = 'ui_state';

  // Token
  static Future<void> saveTokens({
    required String accessToken,
    required String refreshToken,
  }) async {
    final previousAccessToken = await getAccessToken();
    final previousRefreshToken = await getRefreshToken();
    try {
      await _storage.write(key: _accessTokenKey, value: accessToken);
      await _storage.write(key: _refreshTokenKey, value: refreshToken);
    } catch (error, stackTrace) {
      await _restoreValue(_accessTokenKey, previousAccessToken);
      await _restoreValue(_refreshTokenKey, previousRefreshToken);
      Error.throwWithStackTrace(error, stackTrace);
    }
  }

  static Future<String?> getAccessToken() =>
      _storage.read(key: _accessTokenKey);

  static Future<String?> getRefreshToken() =>
      _storage.read(key: _refreshTokenKey);

  static Future<void> saveAccessToken(String token) =>
      _storage.write(key: _accessTokenKey, value: token);

  static Future<void> clearTokens() async {
    await _storage.delete(key: _accessTokenKey);
    await _storage.delete(key: _refreshTokenKey);
  }

  static Future<void> saveTrustedLoginSession({
    required String serverUrl,
    required DateTime expiresAt,
  }) async {
    // 保存当前面板的本地可信登录有效期，7 天内启动不再重复走登录接口。
    await _storage.write(
      key: _trustedLoginUntilKey,
      value: expiresAt.toUtc().toIso8601String(),
    );
    await _storage.write(key: _trustedLoginServerUrlKey, value: serverUrl);
  }

  static Future<DateTime?> getTrustedLoginUntil() async {
    final raw = await _storage.read(key: _trustedLoginUntilKey);
    if (raw == null || raw.isEmpty) {
      return null;
    }

    try {
      return DateTime.parse(raw);
    } catch (_) {
      return null;
    }
  }

  static Future<String?> getTrustedLoginServerUrl() =>
      _storage.read(key: _trustedLoginServerUrlKey);

  static Future<bool> hasValidTrustedLogin({required String serverUrl}) async {
    final trustedServerUrl = await getTrustedLoginServerUrl();
    if (trustedServerUrl == null || trustedServerUrl != serverUrl) {
      return false;
    }

    final trustedUntil = await getTrustedLoginUntil();
    if (trustedUntil == null) {
      return false;
    }

    return DateTime.now().toUtc().isBefore(trustedUntil.toUtc());
  }

  static Future<void> clearTrustedLoginSession() async {
    await _storage.delete(key: _trustedLoginUntilKey);
    await _storage.delete(key: _trustedLoginServerUrlKey);
  }

  static Future<void> saveUser(User user) =>
      _storage.write(key: _userKey, value: jsonEncode(user.toJson()));

  static Future<User?> getUser() async {
    final raw = await _storage.read(key: _userKey);
    if (raw == null || raw.isEmpty) {
      return null;
    }

    try {
      final data = jsonDecode(raw);
      if (data is Map<String, dynamic>) {
        return User.fromJson(data);
      }
      if (data is Map) {
        return User.fromJson(Map<String, dynamic>.from(data));
      }
    } catch (_) {}

    return null;
  }

  static Future<void> clearUser() => _storage.delete(key: _userKey);

  static Future<void> clearAuthSession() async {
    final previousValues = <String, String?>{
      _accessTokenKey: await getAccessToken(),
      _refreshTokenKey: await getRefreshToken(),
      _userKey: await _storage.read(key: _userKey),
      _trustedLoginUntilKey: await _storage.read(key: _trustedLoginUntilKey),
      _trustedLoginServerUrlKey: await _storage.read(
        key: _trustedLoginServerUrlKey,
      ),
    };
    try {
      await clearTokens();
      await clearUser();
      await clearTrustedLoginSession();
    } catch (error, stackTrace) {
      for (final entry in previousValues.entries) {
        await _restoreValue(entry.key, entry.value);
      }
      Error.throwWithStackTrace(error, stackTrace);
    }
  }

  static Future<void> _restoreValue(String key, String? value) => value == null
      ? _storage.delete(key: key)
      : _storage.write(key: key, value: value);

  static Future<void> saveAppLockConfig(Map<String, dynamic> config) =>
      _storage.write(key: _appLockConfigKey, value: jsonEncode(config));

  static Future<Map<String, dynamic>?> getAppLockConfig() async {
    final raw = await _storage.read(key: _appLockConfigKey);
    if (raw == null || raw.isEmpty) {
      return null;
    }

    try {
      final data = jsonDecode(raw);
      if (data is Map<String, dynamic>) {
        return data;
      }
      if (data is Map) {
        return Map<String, dynamic>.from(data);
      }
    } catch (_) {}
    return null;
  }

  static Future<void> clearAppLockConfig() =>
      _storage.delete(key: _appLockConfigKey);

  // Server URL
  static Future<void> saveServerUrl(String url) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_serverUrlKey, url);
  }

  static Future<String?> getServerUrl() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString(_serverUrlKey);
  }

  // Server List (legacy)
  static Future<void> saveServerList(List<String> servers) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_serverListKey, servers);
  }

  static Future<List<String>> getServerList() async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getStringList(_serverListKey) ?? [];
  }

  // Panels
  static Future<void> savePanels(List<PanelConfig> panels) async {
    final sanitized = panels.map((p) => p.sanitizedForStorage()).toList();
    final json = sanitized.map((p) => jsonEncode(p.toJson())).toList();
    await _storage.write(key: _panelsKey, value: jsonEncode(json));
  }

  static Future<List<PanelConfig>> getPanels() async {
    final raw = await _storage.read(key: _panelsKey);
    if (raw == null) {
      // 迁移旧数据
      final oldList = await getServerList();
      if (oldList.isNotEmpty) {
        final panels = oldList
            .map((url) => PanelConfig(url: url, name: url))
            .toList();
        await savePanels(panels);
        return panels;
      }
      return [];
    }
    try {
      final list = jsonDecode(raw) as List;
      final panels = parsePanelEntries(list);
      await savePanels(panels);
      return panels;
    } catch (_) {
      return [];
    }
  }

  static Future<void> activatePanel(PanelConfig panel) async {
    final previousUrl = await getServerUrl();
    final previousPanels = await getPanels();
    final updatedPanels = [...previousPanels];
    final sanitized = panel.sanitizedForStorage();
    final index = updatedPanels.indexWhere(
      (item) =>
          item.url == sanitized.url ||
          (item.type == PanelType.managedLocal &&
              sanitized.type == PanelType.managedLocal),
    );
    if (index >= 0) {
      updatedPanels[index] = sanitized;
    } else {
      updatedPanels.insert(0, sanitized);
    }
    try {
      await savePanels(updatedPanels);
      await saveServerUrl(sanitized.url);
    } catch (error, stackTrace) {
      await savePanels(previousPanels);
      final prefs = await SharedPreferences.getInstance();
      if (previousUrl == null) {
        await prefs.remove(_serverUrlKey);
      } else {
        await prefs.setString(_serverUrlKey, previousUrl);
      }
      Error.throwWithStackTrace(error, stackTrace);
    }
  }

  static Future<void> savePanel(PanelConfig panel) async {
    final panels = await getPanels();
    final sanitized = panel.sanitizedForStorage();
    final idx = panels.indexWhere((p) => p.url == sanitized.url);
    if (idx >= 0) {
      panels[idx] = sanitized;
    } else {
      panels.insert(0, sanitized);
    }
    await savePanels(panels);
  }

  static Future<void> removePanel(String url) async {
    final panels = await getPanels();
    panels.removeWhere((p) => p.url == url);
    await savePanels(panels);
  }

  static Future<PanelConfig?> getCurrentPanel() async {
    // 当前活跃面板由 server_url 决定，再回 panels 列表里取完整配置。
    final currentUrl = await getServerUrl();
    if (currentUrl == null || currentUrl.isEmpty) {
      return null;
    }

    final panels = await getPanels();
    for (final panel in panels) {
      if (panel.url == currentUrl) {
        return panel;
      }
    }

    return null;
  }

  static Future<void> writeValue(String key, String value) =>
      _storage.write(key: key, value: value);

  static Future<String?> readValue(String key) => _storage.read(key: key);

  static Future<void> saveUiState(String key, String value) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('${_prefsNamespaceKey}_$key', value);
  }

  static Future<String?> getUiState(String key) async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getString('${_prefsNamespaceKey}_$key');
  }

  static Future<void> removeUiState(String key) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove('${_prefsNamespaceKey}_$key');
  }

  static Future<void> saveUiStateList(String key, List<String> values) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList('${_prefsNamespaceKey}_$key', values);
  }

  static Future<List<String>> getUiStateList(String key) async {
    final prefs = await SharedPreferences.getInstance();
    return prefs.getStringList('${_prefsNamespaceKey}_$key') ?? const [];
  }
}

List<PanelConfig> parsePanelEntries(List<dynamic> entries) {
  final panels = <PanelConfig>[];
  for (final entry in entries) {
    try {
      final decoded = entry is String ? jsonDecode(entry) : entry;
      if (decoded is Map) {
        panels.add(
          PanelConfig.fromJson(
            Map<String, dynamic>.from(decoded),
          ).sanitizedForStorage(),
        );
      }
    } catch (_) {}
  }
  return panels;
}
