import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:logger/logger.dart';
import 'app_user_agent.dart';
import 'managed_local_session.dart';

final _logger = Logger(printer: PrettyPrinter(methodCount: 0));

class DioClient {
  static DioClient? _instance;
  final Dio dio;
  String _baseUrl = '';
  final ManagedLocalSession _managedLocalSession;

  DioClient._()
    : this._withDio(
        Dio(),
        managedLocalSession: defaultManagedLocalSession,
      );

  DioClient.forTesting(
    Dio dio, {
    ManagedLocalSession? managedLocalSession,
  }) : this._withDio(
         dio,
         enableLogging: false,
         managedLocalSession: managedLocalSession ?? ManagedLocalSession(),
       );

  DioClient._withDio(
    this.dio, {
    required ManagedLocalSession managedLocalSession,
    bool enableLogging = true,
  }) : _managedLocalSession = managedLocalSession {
    dio.options = BaseOptions(
      validateStatus: (status) =>
          status != null && status >= 200 && status < 300,
      connectTimeout: const Duration(seconds: 15),
      receiveTimeout: const Duration(seconds: 30),
      sendTimeout: const Duration(seconds: 15),
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        ...AppUserAgent.defaultHeaders,
      },
    );

    dio.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          applyManagedLocalHeaders(options);
          handler.next(options);
        },
      ),
    );

    if (enableLogging && kDebugMode) {
      dio.interceptors.add(
        LogInterceptor(
          request: false,
          requestHeader: false,
          requestBody: false,
          responseHeader: false,
          responseBody: false,
          logPrint: (obj) => _logger.d(obj),
        ),
      );
    }
  }

  static DioClient get instance {
    _instance ??= DioClient._();
    return _instance!;
  }

  String get baseUrl => _baseUrl;

  void setBaseUrl(String url) {
    clearManagedLocalSession();
    _baseUrl = url.endsWith('/') ? url.substring(0, url.length - 1) : url;
    dio.options.baseUrl = _baseUrl;
    dio.options.headers.addAll(AppUserAgent.defaultHeaders);
  }

  void clearManagedLocalSession() {
    _managedLocalSession.clear();
    dio.options.headers.remove('X-Daidai-Local-Token');
    dio.options.headers.remove('Origin');
  }

  void setManagedLocalSession(String baseUrl, String token) {
    final normalized = baseUrl.endsWith('/')
        ? baseUrl.substring(0, baseUrl.length - 1)
        : baseUrl;
    _managedLocalSession.set(normalized, token);
    _baseUrl = normalized;
    dio.options.baseUrl = normalized;
    dio.options.headers.addAll(AppUserAgent.defaultHeaders);
    dio.options.headers.addAll(
      _managedLocalSession.headersFor(Uri.parse(normalized)),
    );
  }

  Dio get rawDio {
    final raw = Dio(
      BaseOptions(
        baseUrl: _baseUrl,
        connectTimeout: const Duration(seconds: 10),
        receiveTimeout: const Duration(seconds: 10),
        validateStatus: (status) =>
            status != null && status >= 200 && status < 300,
        headers: {'Accept': 'application/json', ...AppUserAgent.defaultHeaders},
      ),
    );
    raw.interceptors.add(
      InterceptorsWrapper(
        onRequest: (options, handler) {
          applyManagedLocalHeaders(options);
          handler.next(options);
        },
      ),
    );
    raw.options.headers.addAll(
      _managedLocalSession.headersFor(Uri.parse(_baseUrl)),
    );
    return raw;
  }

  void applyManagedLocalHeaders(RequestOptions options) {
    options.headers.remove('X-Daidai-Local-Token');
    options.headers.remove('Origin');
    options.headers.addAll(_managedLocalSession.headersFor(options.uri));
  }
}
