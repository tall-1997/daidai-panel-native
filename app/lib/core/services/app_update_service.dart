import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../network/app_user_agent.dart';
import '../theme/app_theme.dart';
import 'android_update_manifest.dart';

const _kGitHubRepo = 'tall-1997/daidai-panel-native';
const _kGitHubDownloadHost = 'github.com';
const _kGitHubReleaseHost = 'objects.githubusercontent.com';
const _kGitHubAssetHost = 'githubusercontent.com';
const _kGitHubMirrorHost = 'gh.301.ee';
const _kGitHubMirrorPrefix = 'https://$_kGitHubMirrorHost/';
const _kAndroidUpdateManifestUrl =
    'https://github.com/$_kGitHubRepo/releases/latest/download/android-update.json';

bool _isTrustedDownloadUrl(String rawUrl) {
  final uri = Uri.tryParse(rawUrl);
  if (uri == null || uri.scheme != 'https') {
    return false;
  }
  final host = uri.host.toLowerCase();
  return host == _kGitHubDownloadHost ||
      host == _kGitHubReleaseHost ||
      host == _kGitHubMirrorHost ||
      host.endsWith('.$_kGitHubAssetHost');
}

String _applyGitHubMirror(String url) {
  final uri = Uri.tryParse(url);
  if (uri == null) return url;
  final host = uri.host.toLowerCase();
  if (host == _kGitHubDownloadHost || host.endsWith('.$_kGitHubAssetHost')) {
    return '$_kGitHubMirrorPrefix$url';
  }
  return url;
}

int? _toInt(dynamic value) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}

class AppUpdateInfo {
  final String latestVersion;
  final String currentVersion;
  final String releaseNotes;
  final String downloadUrl;
  final String assetName;
  final int assetSize;
  final String assetDigest;
  final String releasePageUrl;
  final bool hasUpdate;
  final DateTime? publishedAt;
  final AndroidUpdateManifest? androidManifest;

  const AppUpdateInfo({
    required this.latestVersion,
    required this.currentVersion,
    required this.releaseNotes,
    required this.downloadUrl,
    required this.assetName,
    required this.assetSize,
    required this.assetDigest,
    this.releasePageUrl = '',
    required this.hasUpdate,
    this.publishedAt,
    this.androidManifest,
  });
}

enum AppUpdateAvailability { upToDate, updateAvailable, installerMissing }

AppUpdateAvailability classifyAppUpdate(AppUpdateInfo info) {
  if (!info.hasUpdate) return AppUpdateAvailability.upToDate;
  if (info.downloadUrl.isEmpty || info.assetName.isEmpty) {
    return AppUpdateAvailability.installerMissing;
  }
  return AppUpdateAvailability.updateAvailable;
}

bool shouldRunAutomaticUpdateCheck(
  DateTime? lastCheck,
  DateTime now, {
  Duration interval = const Duration(hours: 24),
}) => lastCheck == null || now.difference(lastCheck) >= interval;

bool shouldShowAutomaticUpdateReminder({
  required String version,
  required String? lastVersion,
  required DateTime? lastReminder,
  required DateTime now,
}) => lastVersion != version ||
    lastReminder == null ||
    now.difference(lastReminder) >= const Duration(hours: 24);

@visibleForTesting
Future<int> clearUpdateArtifactDirectory(Directory directory) async {
  if (!await directory.exists()) return 0;

  var releasedBytes = 0;
  await for (final entity in directory.list(followLinks: false)) {
    try {
      releasedBytes += await _updateArtifactSize(entity);
    } on FileSystemException {
      // Continue deleting even when a file size cannot be read.
    }
    try {
      await entity.delete(recursive: true);
    } on FileSystemException {
      // Cache cleanup is best effort; a locked installer can be retried later.
    }
  }
  return releasedBytes;
}

Future<int> _updateArtifactSize(FileSystemEntity entity) async {
  if (entity is File) return entity.length();
  if (entity is! Directory) return 0;

  var size = 0;
  await for (final child in entity.list(followLinks: false)) {
    size += await _updateArtifactSize(child);
  }
  return size;
}

class AppUpdateService {
  AppUpdateService._();

  static final _dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 10),
    receiveTimeout: const Duration(seconds: 10),
    validateStatus: (status) => status != null && status < 400,
  ));

  static const _platform = MethodChannel('com.daidai.panel/app_install');
  static const _autoCheckAtKey = 'app_update_auto_check_at';
  static const _reminderAtKey = 'app_update_reminder_at';
  static const _reminderVersionKey = 'app_update_reminder_version';
  static bool _updateInProgress = false;
  static bool _installerOpened = false;

  static Future<int> clearStaleUpdateCache() async {
    if (_updateInProgress) return 0;
    try {
      final root = await getTemporaryDirectory();
      return clearUpdateArtifactDirectory(
        Directory('${root.path}/updates'),
      );
    } catch (_) {
      return 0;
    }
  }

  static Future<void> clearInstallerCacheAfterReturn() async {
    if (!_installerOpened || _updateInProgress) return;
    _installerOpened = false;
    await clearUpdateCache();
  }

  static Future<int> clearUpdateCache() async {
    if (_updateInProgress) return 0;
    try {
      final root = await getTemporaryDirectory();
      return clearUpdateArtifactDirectory(Directory('${root.path}/updates'));
    } catch (_) {
      return 0;
    }
  }

  static Future<bool> beginAutomaticCheck() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_autoCheckAtKey);
    final lastCheck = raw == null ? null : DateTime.tryParse(raw);
    final now = DateTime.now().toUtc();
    if (!shouldRunAutomaticUpdateCheck(lastCheck, now)) return false;
    return true;
  }

  static Future<void> completeAutomaticCheck() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _autoCheckAtKey,
      DateTime.now().toUtc().toIso8601String(),
    );
  }

  static Future<bool> shouldShowAutomaticReminder(String version) async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_reminderAtKey);
    final now = DateTime.now().toUtc();
    return shouldShowAutomaticUpdateReminder(
      version: version,
      lastVersion: prefs.getString(_reminderVersionKey),
      lastReminder: raw == null ? null : DateTime.tryParse(raw),
      now: now,
    );
  }

  static Future<void> completeAutomaticReminder(String version) async {
    final prefs = await SharedPreferences.getInstance();
    final now = DateTime.now().toUtc();
    await prefs.setString(_reminderVersionKey, version);
    await prefs.setString(_reminderAtKey, now.toIso8601String());
  }

  static Future<bool> claimAutomaticReminder(String version) async {
    if (!await shouldShowAutomaticReminder(version)) return false;
    await completeAutomaticReminder(version);
    return true;
  }

  /// Check GitHub Releases for new version.
  static Future<AppUpdateInfo?> checkUpdate({bool throwOnError = false}) async {
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android) {
      final manifestInfo = await _checkAndroidManifest();
      if (manifestInfo != null) return manifestInfo;
    }
    try {
      final resp = await _dio.get(
        'https://api.github.com/repos/$_kGitHubRepo/releases/latest',
        options: Options(headers: {'Accept': 'application/vnd.github.v3+json'}),
      );
      final data = resp.data;
      if (data is! Map<String, dynamic>) {
        throw const FormatException('GitHub Release 响应格式无效');
      }

      final rawTagName = data['tag_name']?.toString() ?? '';
      final tagName = rawTagName.replaceFirst(RegExp(r'^[vV]'), '');
      final body = data['body']?.toString() ?? '';
      final assets = data['assets'];
      final publishedAtStr = data['published_at']?.toString();
      DateTime? publishedAt;
      if (publishedAtStr != null) {
        publishedAt = DateTime.tryParse(publishedAtStr);
        if (publishedAt != null) {
          publishedAt = publishedAt.toLocal();
        }
      }

      String apkUrl = '';
      String assetName = '';
      int assetSize = 0;
      String assetDigest = '';
      if (assets is List) {
        for (final asset in assets) {
          final name = asset['name']?.toString() ?? '';
          if (name.endsWith('.apk')) {
            final rawUrl = asset['browser_download_url']?.toString() ?? '';
            if (_isTrustedDownloadUrl(rawUrl)) {
              final digest = asset['digest']?.toString() ?? '';
              if (!digest.toLowerCase().startsWith('sha256:') ||
                  digest.length <= 'sha256:'.length) {
                continue;
              }
              apkUrl = rawUrl;
              assetName = name;
              assetSize = _toInt(asset['size']) ?? 0;
              assetDigest = digest;
              break;
            }
          }
        }
      }

      final currentVersion = AppUserAgent.versionLabel;
      final hasUpdate = tagName.isNotEmpty && _isNewer(tagName, currentVersion);

      return AppUpdateInfo(
        latestVersion: tagName,
        currentVersion: currentVersion,
        releaseNotes: body,
        downloadUrl: apkUrl,
        assetName: assetName,
        assetSize: assetSize,
        assetDigest: assetDigest,
        releasePageUrl: data['html_url']?.toString() ?? '',
        hasUpdate: hasUpdate,
        publishedAt: publishedAt,
      );
    } catch (_) {
      if (throwOnError) rethrow;
      return null;
    }
  }

  static Future<AppUpdateInfo?> _checkAndroidManifest() async {
    try {
      final response = await _dio.get(
        _kAndroidUpdateManifestUrl,
        options: Options(headers: AppUserAgent.defaultHeaders),
      );
      final raw = response.data;
      if (raw is! Map) return null;
      final manifest = AndroidUpdateManifest.fromJson(
        Map<String, dynamic>.from(raw),
      );
      if (manifest.schemaVersion != 1 ||
          manifest.packageName != 'com.daidai.daidai_app' ||
          !_isTrustedDownloadUrl(manifest.full.url) ||
          manifest.full.md5.isEmpty ||
          manifest.full.sha256.isEmpty) {
        return null;
      }
      final currentVersion = AppUserAgent.versionLabel;
      return AppUpdateInfo(
        latestVersion: manifest.version,
        currentVersion: currentVersion,
        releaseNotes: manifest.releaseNotes,
        downloadUrl: manifest.full.url,
        assetName: manifest.full.name,
        assetSize: manifest.full.size,
        assetDigest: 'sha256:${manifest.full.sha256}',
        releasePageUrl:
            'https://github.com/$_kGitHubRepo/releases/tag/v${manifest.version}',
        hasUpdate: _isNewer(manifest.version, currentVersion),
        androidManifest: manifest,
      );
    } catch (_) {
      return null;
    }
  }

  /// Compare semantic versions: returns true if remote > local.
  static bool _isNewer(String remote, String local) {
    final rParts = remote.split('+');
    final lParts = local.split('+');
    final rVer = rParts[0].split('.').map((e) => int.tryParse(e) ?? 0).toList();
    final lVer = lParts[0].split('.').map((e) => int.tryParse(e) ?? 0).toList();
    while (rVer.length < 3) {
      rVer.add(0);
    }
    while (lVer.length < 3) {
      lVer.add(0);
    }
    for (int i = 0; i < 3; i++) {
      if (rVer[i] > lVer[i]) return true;
      if (rVer[i] < lVer[i]) return false;
    }
    final rBuild = rParts.length > 1 ? (int.tryParse(rParts[1]) ?? 0) : 0;
    final lBuild = lParts.length > 1 ? (int.tryParse(lParts[1]) ?? 0) : 0;
    if (rBuild > lBuild) return true;
    if (rBuild < lBuild) return false;
    return false;
  }

  static Future<void> downloadUpdateAndInstall(
    AppUpdateInfo info,
    ValueChanged<double> onProgress,
    VoidCallback onDone,
    ValueChanged<String> onError,
  ) async {
    if (_updateInProgress) {
      onError('已有更新任务正在进行中');
      return;
    }
    _updateInProgress = true;
    try {
      await _clearUpdateCacheDuringUpdate();
      final manifest = info.androidManifest;
      if (Platform.isAndroid && manifest != null) {
        try {
          final rawInfo = await _platform.invokeMapMethod<String, dynamic>(
            'getInstalledApkInfo',
          );
          final installed = rawInfo ?? const <String, dynamic>{};
          final currentVersion = installed['versionName']?.toString() ?? '';
          final currentVersionCode = _toInt(installed['versionCode']) ?? 0;
          final currentMd5 = installed['md5']?.toString().toLowerCase() ?? '';
          final currentSha256 =
              installed['sha256']?.toString().toLowerCase() ?? '';
          AndroidUpdatePatch? selectedPatch;
          for (final patch in manifest.patches) {
            final versionMatches = patch.fromVersion == currentVersion;
            final codeMatches = patch.fromVersionCode <= 0 ||
                patch.fromVersionCode == currentVersionCode;
            final md5Matches = patch.oldApkMd5.isEmpty ||
                patch.oldApkMd5 == currentMd5;
            final shaMatches = patch.oldApkSha256 == currentSha256;
            if (versionMatches && codeMatches && md5Matches && shaMatches) {
              selectedPatch = patch;
              break;
            }
          }
          if (selectedPatch != null &&
              selectedPatch.size > 0 &&
              selectedPatch.size < manifest.full.size) {
            final patchFile = await _downloadArtifact(
              selectedPatch,
              onProgress: (progress) => onProgress(progress * 0.8),
            );
            final outputName = 'daidai-v${manifest.version}-patched.apk';
            final result = await _platform.invokeMapMethod<String, dynamic>(
              'applyPatch',
              {'patchPath': patchFile.path, 'outputName': outputName},
            );
            final outputPath = result?['path']?.toString() ?? '';
            final output = File(outputPath);
            if (!await _matchesAsset(output, manifest.full)) {
              throw StateError('差分合并后的 APK 校验失败');
            }
            if (await patchFile.exists()) await patchFile.delete();
            onProgress(1.0);
            await _platform.invokeMethod('installApk', {'path': output.path});
            _installerOpened = true;
            onDone();
            return;
          }
        } catch (_) {
          // 基线、下载或合并失败时使用完整 APK，保证更新仍可完成。
          await _clearUpdateCacheDuringUpdate();
        }
      }
      await downloadAndInstall(
        info.downloadUrl,
        info.assetName,
        onProgress,
        onDone,
        onError,
        expectedSize: info.assetSize,
        expectedDigest: info.assetDigest,
        expectedMd5: manifest?.full.md5 ?? '',
      );
    } finally {
      _updateInProgress = false;
    }
  }

  static Future<void> _clearUpdateCacheDuringUpdate() async {
    final root = await getTemporaryDirectory();
    await clearUpdateArtifactDirectory(Directory('${root.path}/updates'));
  }

  static Future<File> _downloadArtifact(
    AndroidUpdateAsset asset, {
    required ValueChanged<double> onProgress,
  }) async {
    if (!_isTrustedDownloadUrl(asset.url)) {
      throw const FormatException('差分更新地址不可信');
    }
    final root = await getTemporaryDirectory();
    final dir = Directory('${root.path}/updates');
    await dir.create(recursive: true);
    final safeName = asset.name.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
    final file = File('${dir.path}/$safeName');
    final temp = File('${file.path}.download');
    if (await file.exists() && await _matchesAsset(file, asset)) {
      onProgress(1.0);
      return file;
    }
    if (await temp.exists()) await temp.delete();
    await _dio.download(
      _applyGitHubMirror(asset.url),
      temp.path,
      onReceiveProgress: (received, total) {
        if (total > 0) onProgress(received / total);
      },
      options: Options(receiveTimeout: const Duration(minutes: 10)),
    );
    if (!await _matchesAsset(temp, asset)) {
      if (await temp.exists()) await temp.delete();
      throw StateError('差分包校验失败');
    }
    if (await file.exists()) await file.delete();
    return temp.rename(file.path);
  }

  static Future<bool> _matchesAsset(
    File file,
    AndroidUpdateAsset asset,
  ) async {
    if (!await file.exists()) return false;
    if (asset.size > 0 && await file.length() != asset.size) return false;
    if (!await _matchesHash(file, asset.md5, md5)) return false;
    return _matchesHash(file, asset.sha256, sha256);
  }

  static Future<bool> _matchesHash(
    File file,
    String expected,
    Hash algorithm,
  ) async {
    final normalized = expected.trim().toLowerCase();
    if (normalized.isEmpty) return false;
    final actual = await algorithm.bind(file.openRead()).first;
    return actual.toString().toLowerCase() == normalized;
  }

  /// Download APK and install it.
  /// Uses GitHub mirror for acceleration and reuses existing downloads.
  static Future<void> downloadAndInstall(
    String url,
    String assetName,
    ValueChanged<double> onProgress,
    VoidCallback onDone,
    ValueChanged<String> onError, {
    int expectedSize = 0,
    String expectedDigest = '',
    String expectedMd5 = '',
  }) async {
    try {
      if (!_isTrustedDownloadUrl(url)) {
        throw const FormatException('更新地址不可信，已拒绝下载');
      }

      final root = await getTemporaryDirectory();
      final dir = Directory('${root.path}/updates');
      await dir.create(recursive: true);
      final safeName = assetName.trim().isEmpty
          ? 'daidai_update.apk'
          : assetName.replaceAll(RegExp(r'[^A-Za-z0-9._-]'), '_');
      final filePath = '${dir.path}/$safeName';

      final existingFile = File(filePath);
      final tempFile = File('$filePath.download');
      bool needsDownload = true;

      if (await existingFile.exists()) {
        if (await _isCachedInstallerValid(
          existingFile,
          expectedSize: expectedSize,
          expectedDigest: expectedDigest,
          expectedMd5: expectedMd5,
        )) {
          needsDownload = false;
          onProgress(1.0);
        }
      }

      if (needsDownload) {
        final downloadUrl = _applyGitHubMirror(url);
        if (await tempFile.exists()) {
          await tempFile.delete();
        }

        final response = await _dio.download(
          downloadUrl,
          tempFile.path,
          onReceiveProgress: (received, total) {
            if (total > 0) {
              onProgress(received / total);
            }
          },
          options: Options(receiveTimeout: const Duration(minutes: 10)),
        );
        final finalHost = response.realUri.host.toLowerCase();
        if (!(_isTrustedDownloadUrl(response.realUri.toString()) ||
            finalHost == _kGitHubReleaseHost ||
            finalHost == _kGitHubMirrorHost ||
            finalHost.endsWith('.$_kGitHubAssetHost'))) {
          throw const FormatException('更新资源跳转到了不受信任的来源');
        }
        if (!await _isCachedInstallerValid(
          tempFile,
          expectedSize: expectedSize,
          expectedDigest: expectedDigest,
          expectedMd5: expectedMd5,
        )) {
          await tempFile.delete();
          throw StateError('安装包校验失败，请重新下载');
        }
        if (await existingFile.exists()) {
          await existingFile.delete();
        }
        await tempFile.rename(filePath);
      }

      if (!await _isCachedInstallerValid(
        existingFile,
        expectedSize: expectedSize,
        expectedDigest: expectedDigest,
        expectedMd5: expectedMd5,
      )) {
        throw StateError('安装包校验失败，请重新下载');
      }

      if (Platform.isAndroid) {
        final originalHost = Uri.parse(url).host.toLowerCase();
        await _platform.invokeMethod('installApk', {
          'path': filePath,
          'sourceHost': originalHost,
        });
        _installerOpened = true;
      }
      onDone();
    } catch (e) {
      try {
        await _clearUpdateCacheDuringUpdate();
      } on FileSystemException {
        // Keep the original update error visible to the user.
      }
      onError(e.toString());
    }
  }

  static Future<bool> _isCachedInstallerValid(
    File file, {
    required int expectedSize,
    required String expectedDigest,
    String expectedMd5 = '',
  }) async {
    if (!await file.exists()) {
      return false;
    }
    final size = await file.length();
    if (expectedSize > 0 && size != expectedSize) {
      return false;
    }
    if (expectedSize <= 0 && size <= 1024 * 1024) {
      return false;
    }
    if (expectedMd5.isNotEmpty &&
        !await _matchesHash(file, expectedMd5, md5)) {
      return false;
    }
    return _matchesDigest(file, expectedDigest);
  }

  static Future<bool> _matchesDigest(File file, String expectedDigest) async {
    final normalized = expectedDigest.trim().toLowerCase();
    if (normalized.isEmpty) {
      return true;
    }
    final expected = normalized.startsWith('sha256:')
        ? normalized.substring('sha256:'.length)
        : normalized;
    if (expected.isEmpty) {
      return true;
    }
    final actual = await sha256.bind(file.openRead()).first;
    return actual.toString().toLowerCase() == expected;
  }

  /// Show update dialog.
  static Future<void> showUpdateDialog(
    BuildContext context,
    AppUpdateInfo info,
  ) async {
    if (!context.mounted) return;
    final isLight = Theme.of(context).brightness == Brightness.light;

    await showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (dialogCtx) => _UpdateDialog(
        info: info,
        isLight: isLight,
      ),
    );
  }

  static Future<bool> openUpdatePage(AppUpdateInfo info) async {
    final uri = Uri.tryParse(info.releasePageUrl);
    if (uri == null ||
        uri.scheme != 'https' ||
        uri.host.toLowerCase() != _kGitHubDownloadHost) {
      return false;
    }
    try {
      return await _platform.invokeMethod<bool>(
            'openExternalUrl',
            {'url': uri.toString()},
          ) ??
          false;
    } on PlatformException {
      return false;
    } on MissingPluginException {
      return false;
    }
  }
}

class _UpdateDialog extends StatefulWidget {
  final AppUpdateInfo info;
  final bool isLight;
  const _UpdateDialog({required this.info, required this.isLight});

  @override
  State<_UpdateDialog> createState() => _UpdateDialogState();
}

class _UpdateDialogState extends State<_UpdateDialog> {
  bool _downloading = false;
  double _progress = 0;
  String? _error;

  Future<void> _openUpdatePage() async {
    final opened = await AppUpdateService.openUpdatePage(widget.info);
    if (!mounted) return;
    if (opened) {
      Navigator.pop(context);
    } else {
      setState(() => _error = '无法打开 Release 页面，请检查系统浏览器设置');
    }
  }

  void _startDownload() {
    if (widget.info.downloadUrl.isEmpty) {
      setState(() => _error = '未找到 APK 下载链接');
      return;
    }
    setState(() {
      _downloading = true;
      _progress = 0;
      _error = null;
    });

    AppUpdateService.downloadUpdateAndInstall(
      widget.info,
      (p) {
        if (mounted) setState(() => _progress = p);
      },
      () {
        if (mounted) setState(() => _downloading = false);
      },
      (e) {
        if (mounted) {
          setState(() {
            _downloading = false;
            _error = '下载失败: $e';
          });
        }
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    final isAndroid = !kIsWeb && defaultTargetPlatform == TargetPlatform.android;

    return AlertDialog(
      title: const Text('发现新版本'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Text(
                'v${widget.info.currentVersion}',
                style: TextStyle(
                  fontSize: 13,
                  color: widget.isLight
                      ? AppColors.slate500
                      : AppColors.slate400,
                ),
              ),
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8),
                child: Icon(Icons.arrow_forward, size: 14),
              ),
              Text(
                'v${widget.info.latestVersion}',
                style: const TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w700,
                  color: AppColors.primary,
                ),
              ),
            ],
          ),
          if (widget.info.releaseNotes.isNotEmpty) ...[
            const SizedBox(height: 12),
            const Text(
              '更新内容',
              style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 6),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 200),
              child: SingleChildScrollView(
                child: Text(
                  widget.info.releaseNotes,
                  style: TextStyle(
                    fontSize: 12,
                    color: widget.isLight
                        ? AppColors.slate600
                        : AppColors.slate300,
                    height: 1.5,
                  ),
                ),
              ),
            ),
          ],
          if (_downloading) ...[
            const SizedBox(height: 16),
            LinearProgressIndicator(
              value: _progress,
              color: AppColors.primary,
              backgroundColor: widget.isLight
                  ? AppColors.slate200
                  : AppColors.slate800,
            ),
            const SizedBox(height: 6),
            Center(
              child: Text(
                '下载中 ${(_progress * 100).toStringAsFixed(0)}%',
                style: const TextStyle(fontSize: 12),
              ),
            ),
          ],
          if (_error != null) ...[
            const SizedBox(height: 12),
            Text(
              _error!,
              style: const TextStyle(fontSize: 12, color: AppColors.red500),
            ),
          ] else ...[
            const SizedBox(height: 12),
            Text(
              '更新包通过 GitHub 加速镜像下载，校验包名与签名后再安装。已下载的安装包会自动复用，无需重复下载。',
              style: TextStyle(
                fontSize: 12,
                color: widget.isLight
                    ? AppColors.slate600
                    : AppColors.slate300,
                height: 1.4,
              ),
            ),
          ],
        ],
      ),
      actions: _downloading
          ? null
          : [
              Row(
                children: [
                  Expanded(
                    child: SizedBox(
                      height: 44,
                      child: OutlinedButton(
                        onPressed: () => Navigator.pop(context),
                        child: const Text('稍后'),
                      ),
                    ),
                  ),
                  if (isAndroid && widget.info.downloadUrl.isNotEmpty) ...[
                    const SizedBox(width: 12),
                    Expanded(
                      child: SizedBox(
                        height: 44,
                        child: FilledButton(
                          onPressed: _startDownload,
                          child: const Text('立即更新'),
                        ),
                      ),
                    ),
                  ] else if (widget.info.releasePageUrl.isNotEmpty) ...[
                    const SizedBox(width: 12),
                    Expanded(
                      child: SizedBox(
                        height: 44,
                        child: FilledButton(
                          onPressed: _openUpdatePage,
                          child: const Text('查看更新'),
                        ),
                      ),
                    ),
                  ],
                ],
              ),
            ],
    );
  }
}
