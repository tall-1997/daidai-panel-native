import 'package:dio/dio.dart';
import '../network/app_user_agent.dart';
import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import '../../shared/models/user.dart';
import 'auth_session_epoch.dart';

class ServerHealthCheckResult {
  final bool reachable;
  final String? errorMessage;

  const ServerHealthCheckResult._({
    required this.reachable,
    this.errorMessage,
  });

  const ServerHealthCheckResult.success()
    : this._(reachable: true);

  const ServerHealthCheckResult.failure(String message)
    : this._(reachable: false, errorMessage: message);
}

/// 从响应中提取 data 字段，兼容 {code, data: {...}} 和直接 {...} 两种格式
dynamic _extractData(dynamic responseData) {
  if (responseData is Map<String, dynamic> &&
      responseData.containsKey('data')) {
    return responseData['data'];
  }
  return responseData;
}

class AuthService {
  final Dio _dio = DioClient.instance.dio;

  /// 返回 true 表示需要初始化，false 表示已初始化
  Future<bool> needsInitialization() async {
    final response = await _dio.get(ApiEndpoints.checkInit);
    final raw = response.data;
    if (raw is Map<String, dynamic>) {
      // 后端实际返回: {"need_init": false}
      if (raw.containsKey('need_init')) {
        return raw['need_init'] == true;
      }
      // 兼容: {data: {need_init: true}}
      if (raw['data'] is Map<String, dynamic>) {
        final data = raw['data'] as Map<String, dynamic>;
        if (data.containsKey('need_init')) {
          return data['need_init'] == true;
        }
        if (data.containsKey('initialized')) {
          return data['initialized'] == false;
        }
      }
    }
    return false;
  }

  Future<void> initAdmin(String username, String password) async {
    await _dio.post(
      ApiEndpoints.init,
      data: {'username': username, 'password': password},
    );
  }

  Future<Map<String, dynamic>> login({
    required String username,
    required String password,
    String? totpCode,
    Map<String, dynamic>? captcha,
    int? authEpoch,
  }) async {
    final epoch = authEpoch ?? AuthSessionEpoch.advance();
    final data = <String, dynamic>{'username': username, 'password': password};
    if (totpCode != null && totpCode.isNotEmpty) {
      data['totp_code'] = totpCode;
    }
    if (captcha != null && captcha.isNotEmpty) {
      data['captcha'] = captcha;
    }

    final response = await _dio.post(
      ApiEndpoints.login,
      data: data,
      options: Options(
        sendTimeout: const Duration(seconds: 12),
        receiveTimeout: const Duration(seconds: 12),
        validateStatus: (status) => status != null && status < 500,
      ),
    );

    // 登录接口只要返回 4xx，就先交给上层显示明确原因，避免后续误进入首页再变成“网络错误”。
    final statusCode = response.statusCode ?? 0;
    if (statusCode >= 400) {
      throw DioException.badResponse(
        statusCode: statusCode,
        requestOptions: response.requestOptions,
        response: response,
      );
    }

    final result = _extractData(response.data);
    final map = result is Map
        ? Map<String, dynamic>.from(result)
        : <String, dynamic>{};

    if (map.containsKey('access_token')) {
      final accessToken = map['access_token'];
      final refreshToken = map['refresh_token'];
      if (accessToken is! String || accessToken.isEmpty) {
        throw const FormatException('登录响应中的 access_token 无效');
      }
      if (refreshToken is! String || refreshToken.isEmpty) {
        throw const FormatException('登录响应缺少 refresh_token');
      }
      await SecureStorage.saveTokens(
        accessToken: accessToken,
        refreshToken: refreshToken,
        authEpoch: epoch,
      );
    }

    return map;
  }

  Future<Map<String, dynamic>> captchaConfig({String? username}) async {
    final response = await _dio.get(
      ApiEndpoints.captchaConfig,
      queryParameters: username != null && username.trim().isNotEmpty
          ? {'username': username.trim()}
          : null,
      options: Options(
        sendTimeout: const Duration(seconds: 12),
        receiveTimeout: const Duration(seconds: 12),
        validateStatus: (status) => status != null && status < 500,
      ),
    );
    final result = _extractData(response.data);
    if (result is Map<String, dynamic>) {
      return result;
    }
    if (result is Map) {
      return Map<String, dynamic>.from(result);
    }
    return <String, dynamic>{};
  }

  Future<void> logout({int? authEpoch}) async {
    final epoch = authEpoch ?? AuthSessionEpoch.advance();
    try {
      await _dio.post(ApiEndpoints.logout);
    } finally {
      await SecureStorage.clearAuthSession(authEpoch: epoch);
    }
  }

  Future<User> getUser({int? authEpoch}) async {
    final epoch = authEpoch ?? AuthSessionEpoch.current;
    final response = await _dio.get(ApiEndpoints.user);
    final data = _extractData(response.data);
    if (data is! Map) {
      throw const FormatException('用户信息响应格式错误');
    }
    final userData = data is Map && data['user'] is Map
        ? Map<String, dynamic>.from(data['user'] as Map)
        : Map<String, dynamic>.from(data);
    final user = User.fromJson(userData);
    await SecureStorage.saveUser(user, authEpoch: epoch);
    return user;
  }

  Future<void> changeUsername(String username, {int? authEpoch}) async {
    final epoch = authEpoch ?? AuthSessionEpoch.advance();
    await _dio.put(ApiEndpoints.authUsername, data: {'username': username});
    await SecureStorage.clearAuthSession(authEpoch: epoch);
  }

  Future<void> changePassword(
    String oldPassword,
    String newPassword, {
    int? authEpoch,
  }) async {
    final epoch = authEpoch ?? AuthSessionEpoch.advance();
    await _dio.put(
      ApiEndpoints.authPassword,
      data: {'old_password': oldPassword, 'new_password': newPassword},
    );
    await SecureStorage.clearAuthSession(authEpoch: epoch);
  }

  Future<void> uploadAvatar(MultipartFile avatar) async {
    await _dio.post(
      ApiEndpoints.authAvatar,
      data: FormData.fromMap({'avatar': avatar}),
    );
  }

  Future<void> deleteAvatar() async {
    await _dio.delete(ApiEndpoints.authAvatar);
  }

  Future<bool> checkHealth(String serverUrl) async {
    return (await checkHealthDetails(serverUrl)).reachable;
  }

  Future<ServerHealthCheckResult> checkHealthDetails(String serverUrl) async {
    final uri = Uri.tryParse(serverUrl);
    if (uri == null ||
        !uri.hasAuthority ||
        uri.host.isEmpty ||
        (uri.scheme != 'http' && uri.scheme != 'https')) {
      return const ServerHealthCheckResult.failure('服务器地址格式无效');
    }

    try {
      final dio = Dio(
        BaseOptions(
          baseUrl: serverUrl,
          connectTimeout: const Duration(seconds: 8),
          receiveTimeout: const Duration(seconds: 8),
          sendTimeout: const Duration(seconds: 8),
          validateStatus: (status) => status != null && status < 600,
          headers: AppUserAgent.defaultHeaders,
        ),
      );
      final response = await dio.get(
        ApiEndpoints.health,
        options: Options(followRedirects: false),
      );
      final responseData = response.data;
      final healthy = responseData is Map && responseData['status'] == 'ok';
      if (response.statusCode == 200 && healthy) {
        return const ServerHealthCheckResult.success();
      }
      if (response.statusCode == 200) {
        return const ServerHealthCheckResult.failure(
          '服务器健康检查返回了非面板响应，请确认域名反向代理指向 Daidai Panel',
        );
      }
      return ServerHealthCheckResult.failure(
        '服务器健康检查返回 HTTP ${response.statusCode ?? '未知状态'}',
      );
    } on DioException catch (error) {
      final detail = '${error.message ?? ''} ${error.error ?? ''}'.toLowerCase();
      if (detail.contains('certificate') ||
          detail.contains('hostname mismatch')) {
        return const ServerHealthCheckResult.failure(
          'HTTPS 证书校验失败，请检查证书是否有效、域名是否匹配以及证书链是否完整',
        );
      }
      if (detail.contains('handshake')) {
        return const ServerHealthCheckResult.failure(
          'TLS 握手失败，请检查协议、证书和反向代理 TLS 配置',
        );
      }
      if (error.type == DioExceptionType.connectionTimeout ||
          error.type == DioExceptionType.sendTimeout ||
          error.type == DioExceptionType.receiveTimeout) {
        return const ServerHealthCheckResult.failure(
          '连接服务器超时，请检查域名解析、反向代理和防火墙配置',
        );
      }
      if (detail.contains('failed host lookup') ||
          detail.contains('name or service not known')) {
        return const ServerHealthCheckResult.failure('域名解析失败，请检查域名和 DNS 配置');
      }
      if (error.response?.statusCode != null) {
        return ServerHealthCheckResult.failure(
          '服务器健康检查返回 HTTP ${error.response!.statusCode}',
        );
      }
      return const ServerHealthCheckResult.failure(
        '无法建立连接，请检查 HTTPS 反向代理、端口和网络配置',
      );
    } catch (_) {
      return const ServerHealthCheckResult.failure('服务器连接检查失败');
    }
  }

}
