class AndroidUpdateAsset {
  final String url;
  final String name;
  final int size;
  final String md5;
  final String sha256;

  const AndroidUpdateAsset({
    required this.url,
    required this.name,
    required this.size,
    required this.md5,
    required this.sha256,
  });

  factory AndroidUpdateAsset.fromJson(Map<String, dynamic> json) {
    return AndroidUpdateAsset(
      url: json['url']?.toString() ?? '',
      name: json['name']?.toString() ?? '',
      size: _toInt(json['size']),
      md5: json['md5']?.toString().toLowerCase() ?? '',
      sha256: json['sha256']?.toString().toLowerCase() ?? '',
    );
  }
}

class AndroidUpdatePatch extends AndroidUpdateAsset {
  final String fromVersion;
  final int fromVersionCode;
  final String oldApkMd5;
  final String oldApkSha256;

  const AndroidUpdatePatch({
    required super.url,
    required super.name,
    required super.size,
    required super.md5,
    required super.sha256,
    required this.fromVersion,
    required this.fromVersionCode,
    required this.oldApkMd5,
    required this.oldApkSha256,
  });

  factory AndroidUpdatePatch.fromJson(Map<String, dynamic> json) {
    final asset = AndroidUpdateAsset.fromJson(json);
    return AndroidUpdatePatch(
      url: asset.url,
      name: asset.name,
      size: asset.size,
      md5: asset.md5,
      sha256: asset.sha256,
      fromVersion: json['fromVersion']?.toString() ?? '',
      fromVersionCode: _toInt(json['fromVersionCode']),
      oldApkMd5: json['oldApkMd5']?.toString().toLowerCase() ?? '',
      oldApkSha256: json['oldApkSha256']?.toString().toLowerCase() ?? '',
    );
  }
}

class AndroidUpdateManifest {
  final int schemaVersion;
  final String packageName;
  final String version;
  final int versionCode;
  final String releaseNotes;
  final AndroidUpdateAsset full;
  final List<AndroidUpdatePatch> patches;
  final Map<String, AndroidUpdatePackage> packages;

  const AndroidUpdateManifest({
    required this.schemaVersion,
    required this.packageName,
    required this.version,
    required this.versionCode,
    required this.releaseNotes,
    required this.full,
    required this.patches,
    this.packages = const {},
  });

  factory AndroidUpdateManifest.fromJson(
    Map<String, dynamic> json, {
    String? abi,
  }) {
    final schemaVersion = _toInt(json['schemaVersion']);
    final rawPackages = json['packages'];
    final packages = <String, AndroidUpdatePackage>{};
    if (rawPackages is Map) {
      for (final entry in rawPackages.entries) {
        if (entry.value is! Map) {
          throw const FormatException('Invalid ABI package metadata');
        }
        packages[entry.key.toString()] = AndroidUpdatePackage.fromJson(
          Map<String, dynamic>.from(entry.value as Map),
        );
      }
    }
    final selectedPackage = abi == null ? null : packages[abi];
    if (schemaVersion == 2 && selectedPackage == null) {
      throw const FormatException('Missing APK metadata for current ABI');
    }
    final full = selectedPackage?.full ??
        (json['full'] is Map
            ? AndroidUpdateAsset.fromJson(
                Map<String, dynamic>.from(json['full'] as Map),
              )
            : null);
    if (full == null) throw const FormatException('Missing full APK metadata');
    final patches = json['patches'];
    return AndroidUpdateManifest(
      schemaVersion: schemaVersion,
      packageName: json['packageName']?.toString() ?? '',
      version: json['version']?.toString() ?? '',
      versionCode: _toInt(json['versionCode']),
      releaseNotes: json['releaseNotes']?.toString() ?? '',
      full: full,
      patches:
          selectedPackage?.patches ??
          (patches is List
              ? patches
                    .whereType<Map>()
                    .map(
                      (item) => AndroidUpdatePatch.fromJson(
                        Map<String, dynamic>.from(item),
                      ),
                    )
                    .toList()
              : const []),
      packages: packages,
    );
  }
}

class AndroidUpdatePackage {
  final AndroidUpdateAsset full;
  final List<AndroidUpdatePatch> patches;

  const AndroidUpdatePackage({required this.full, required this.patches});

  factory AndroidUpdatePackage.fromJson(Map<String, dynamic> json) {
    final full = json['full'];
    if (full is! Map) {
      throw const FormatException('Missing ABI full APK metadata');
    }
    final patches = json['patches'];
    return AndroidUpdatePackage(
      full: AndroidUpdateAsset.fromJson(Map<String, dynamic>.from(full)),
      patches: patches is List
          ? patches
                .whereType<Map>()
                .map(
                  (item) => AndroidUpdatePatch.fromJson(
                    Map<String, dynamic>.from(item),
                  ),
                )
                .toList()
          : const [],
    );
  }
}

int _toInt(dynamic value) {
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '') ?? 0;
}
