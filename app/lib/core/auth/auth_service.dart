import 'package:dio/dio.dart';
import '../network/app_user_agent.dart';
import '../network/api_endpoints.dart';
import '../network/dio_client.dart';
import '../storage/secure_storage.dart';
import '../../shared/models/user.dart';
import 'token_refresh_coordinator.dart';

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
    try {
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
    } catch (_) {
      return false;
    }
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
  }) async {
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
    final Map<String, dynamic> map = result is Map<String, dynamic>
        ? result
        : {};

    if (map.containsKey('access_token')) {
      await SecureStorage.saveTokens(
        accessToken: map['access_token'] as String,
        refreshToken: map['refresh_token'] as String,
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

  Future<void> logout() async {
    TokenRefreshCoordinator.invalidate();
    try {
      await _dio.post(ApiEndpoints.logout);
    } finally {
      await SecureStorage.clearAuthSession();
    }
  }

  Future<User> getUser() async {
    final response = await _dio.get(ApiEndpoints.user);
    final data = _extractData(response.data);
    final userData = data is Map && data['user'] is Map
        ? Map<String, dynamic>.from(data['user'] as Map)
        : Map<String, dynamic>.from(data as Map);
    final user = User.fromJson(userData);
    await SecureStorage.saveUser(user);
    return user;
  }

  Future<void> changeUsername(String username) async {
    TokenRefreshCoordinator.invalidate();
    await _dio.put(ApiEndpoints.authUsername, data: {'username': username});
    await SecureStorage.clearAuthSession();
  }

  Future<void> changePassword(String oldPassword, String newPassword) async {
    TokenRefreshCoordinator.invalidate();
    await _dio.put(
      ApiEndpoints.authPassword,
      data: {'old_password': oldPassword, 'new_password': newPassword},
    );
    await SecureStorage.clearAuthSession();
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
    try {
      final dio = Dio(
        BaseOptions(
          baseUrl: serverUrl,
          connectTimeout: const Duration(seconds: 5),
          receiveTimeout: const Duration(seconds: 5),
          headers: AppUserAgent.defaultHeaders,
        ),
      );
      final response = await dio.get(ApiEndpoints.health);
      return response.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

}
