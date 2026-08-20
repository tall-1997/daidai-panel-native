import 'dart:convert';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:shared_preferences/shared_preferences.dart';

class LocalNotificationService {
  static final LocalNotificationService _instance =
      LocalNotificationService._internal();
  factory LocalNotificationService() => _instance;
  LocalNotificationService._internal();

  final FlutterLocalNotificationsPlugin _plugin =
      FlutterLocalNotificationsPlugin();

  static const _prefsKeyTaskEnabled = 'local_notify_task_enabled';
  static const _prefsKeySystemEnabled = 'local_notify_system_enabled';
  static const _pendingPayloadKey = 'local_notify_pending_payload';
  void Function(String route)? _navigationHandler;
  String? _pendingPayload;

  /// 后台通知回调 - 当应用在后台运行且收到通知时由系统调用
  @pragma('vm:entry-point')
  static Future<void> _onDidReceiveBackgroundNotificationResponse(
      NotificationResponse response) async {
    final payload = response.payload;
    if (payload == null || payload.isEmpty) return;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_pendingPayloadKey, payload);
  }

  Future<void> initialize() async {
    const androidSettings =
        AndroidInitializationSettings('@mipmap/ic_launcher');
    const iosSettings = DarwinInitializationSettings(
      requestAlertPermission: false,
      requestBadgePermission: false,
      requestSoundPermission: false,
    );
    const settings = InitializationSettings(
      android: androidSettings,
      iOS: iosSettings,
    );
    await _plugin.initialize(
      settings,
      onDidReceiveNotificationResponse: _onNotificationTap,
      onDidReceiveBackgroundNotificationResponse:
          _onDidReceiveBackgroundNotificationResponse,
    );
    final launch = await _plugin.getNotificationAppLaunchDetails();
    _pendingPayload = launch?.didNotificationLaunchApp == true
        ? launch?.notificationResponse?.payload
        : null;
    final prefs = await SharedPreferences.getInstance();
    _pendingPayload ??= prefs.getString(_pendingPayloadKey);
  }

  void _onNotificationTap(NotificationResponse response) {
    final payload = response.payload;
    if (payload == null || payload.isEmpty) return;
    _dispatchPayload(payload);
  }

  void setNavigationHandler(void Function(String route) handler) {
    _navigationHandler = handler;
    final pending = _pendingPayload;
    _pendingPayload = null;
    if (pending != null) _dispatchPayload(pending);
  }

  void _dispatchPayload(String payload) {
    final route = notificationPayloadRoute(payload);
    if (route == null) return;
    final handler = _navigationHandler;
    if (handler == null) {
      _pendingPayload = payload;
    } else {
      handler(route);
      SharedPreferences.getInstance().then(
        (prefs) => prefs.remove(_pendingPayloadKey),
      );
    }
  }

  Future<bool> requestPermissions() async {
    final android = _plugin.resolvePlatformSpecificImplementation<
        AndroidFlutterLocalNotificationsPlugin>();
    if (android != null) {
      final granted = await android.requestNotificationsPermission();
      return granted ?? false;
    }
    final ios = _plugin.resolvePlatformSpecificImplementation<
        IOSFlutterLocalNotificationsPlugin>();
    if (ios != null) {
      final granted = await ios.requestPermissions(
        alert: true,
        badge: true,
        sound: true,
      );
      return granted ?? false;
    }
    return true;
  }

  Future<bool> areNotificationsEnabled() async {
    final android = _plugin.resolvePlatformSpecificImplementation<
        AndroidFlutterLocalNotificationsPlugin>();
    if (android != null) {
      return await android.areNotificationsEnabled() ?? true;
    }
    final ios = _plugin.resolvePlatformSpecificImplementation<
        IOSFlutterLocalNotificationsPlugin>();
    if (ios != null) {
      final permissions = await ios.checkPermissions();
      return permissions?.isEnabled ?? false;
    }
    return true;
  }

  Future<void> showTaskNotification({
    required int id,
    required String title,
    required String body,
    String? payload,
  }) async {
    const androidDetails = AndroidNotificationDetails(
      'task_channel',
      '任务通知',
      channelDescription: '任务执行结果通知',
      importance: Importance.high,
      priority: Priority.high,
      showWhen: true,
    );
    const iosDetails = DarwinNotificationDetails(
      presentAlert: true,
      presentBadge: true,
      presentSound: true,
    );
    const details = NotificationDetails(
      android: androidDetails,
      iOS: iosDetails,
    );
    await _plugin.show(id, title, body, details, payload: payload);
  }

  Future<void> showSystemNotification({
    required int id,
    required String title,
    required String body,
    String? payload,
  }) async {
    const androidDetails = AndroidNotificationDetails(
      'system_channel',
      '系统通知',
      channelDescription: '面板系统与安全通知',
      importance: Importance.defaultImportance,
      priority: Priority.defaultPriority,
      showWhen: true,
    );
    const iosDetails = DarwinNotificationDetails(
      presentAlert: true,
      presentBadge: true,
      presentSound: true,
    );
    const details = NotificationDetails(
      android: androidDetails,
      iOS: iosDetails,
    );
    await _plugin.show(id, title, body, details, payload: payload);
  }

  Future<bool> showUpdateNotification({
    required String version,
    String releaseNotes = '',
  }) async {
    if (!await getChannelEnabled(NotificationChannel.system)) return false;
    await showSystemNotification(
      id: updateNotificationId(version),
      title: '发现新版本 $version',
      body: updateNotificationBody(releaseNotes),
      payload: updateNotificationPayload(version),
    );
    return true;
  }

  Future<void> showTestNotification(NotificationChannel channel) async {
    final details = _channelDetails(channel);
    await _plugin.show(
      0,
      '测试通知',
      '这是一条来自呆呆面板的本地测试通知',
      details,
    );
  }

  NotificationDetails _channelDetails(NotificationChannel channel) {
    switch (channel) {
      case NotificationChannel.task:
        return const NotificationDetails(
          android: AndroidNotificationDetails(
            'task_channel',
            '任务通知',
            channelDescription: '任务执行结果通知',
            importance: Importance.high,
            priority: Priority.high,
            showWhen: true,
          ),
          iOS: DarwinNotificationDetails(
            presentAlert: true,
            presentBadge: true,
            presentSound: true,
          ),
        );
      case NotificationChannel.system:
        return const NotificationDetails(
          android: AndroidNotificationDetails(
            'system_channel',
            '系统通知',
            channelDescription: '面板系统与安全通知',
            importance: Importance.defaultImportance,
            priority: Priority.defaultPriority,
            showWhen: true,
          ),
          iOS: DarwinNotificationDetails(
            presentAlert: true,
            presentBadge: true,
            presentSound: true,
          ),
        );
    }
  }

  Future<bool> getChannelEnabled(NotificationChannel channel) async {
    final prefs = await SharedPreferences.getInstance();
    final key = channel == NotificationChannel.task
        ? _prefsKeyTaskEnabled
        : _prefsKeySystemEnabled;
    return prefs.getBool(key) ?? true;
  }

  Future<void> setChannelEnabled(
      NotificationChannel channel, bool enabled) async {
    final prefs = await SharedPreferences.getInstance();
    final key = channel == NotificationChannel.task
        ? _prefsKeyTaskEnabled
        : _prefsKeySystemEnabled;
    await prefs.setBool(key, enabled);
  }
}

String taskNotificationPayload(int taskId) =>
    jsonEncode({'type': 'task', 'id': taskId});

String logNotificationPayload(int logId) =>
    jsonEncode({'type': 'log', 'id': logId});

String updateNotificationPayload(String version) =>
    jsonEncode({'type': 'app_update', 'version': version});

int updateNotificationId(String version) {
  var hash = 17;
  for (final codeUnit in version.codeUnits) {
    hash = (hash * 31 + codeUnit) & 0x3fffffff;
  }
  return 1000000000 + hash;
}

String updateNotificationBody(String releaseNotes) {
  final normalized = releaseNotes.trim().replaceAll(RegExp(r'\s+'), ' ');
  if (normalized.isEmpty) return '点击查看并更新应用';
  return normalized.length <= 120
      ? normalized
      : '${normalized.substring(0, 117)}...';
}

String? notificationPayloadRoute(String payload) {
  try {
    final data = jsonDecode(payload);
    if (data is! Map) return null;
    if (data['type'] == 'app_update' &&
        data['version']?.toString().trim().isNotEmpty == true) {
      return '/more';
    }
    final id = data['id'];
    if (id is! num || id.toInt() <= 0) return null;
    return switch (data['type']) {
      'task' => '/tasks/${id.toInt()}/live-logs',
      'log' => '/logs/${id.toInt()}/stream',
      _ => null,
    };
  } catch (_) {
    return null;
  }
}

enum NotificationChannel { task, system }
