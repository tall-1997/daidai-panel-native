class User {
  final int id;
  final String username;
  final String role;
  final bool enabled;
  final String? avatarUrl;
  final DateTime? lastLoginAt;
  final DateTime createdAt;
  final DateTime updatedAt;

  const User({
    required this.id,
    required this.username,
    required this.role,
    required this.enabled,
    this.avatarUrl,
    this.lastLoginAt,
    required this.createdAt,
    required this.updatedAt,
  });

  bool get isAdmin => role == 'admin';
  bool get isOperator => role == 'operator' || isAdmin;
  bool get isViewer => true;

  bool hasMinRole(String minRole) {
    const hierarchy = {'viewer': 0, 'operator': 1, 'admin': 2};
    return (hierarchy[role] ?? 0) >= (hierarchy[minRole] ?? 0);
  }

  factory User.fromJson(Map<String, dynamic> json) {
    final rawAvatar = json['avatar_url']?.toString();
    return User(
      id: (json['id'] as num).toInt(),
      username: json['username'] as String,
      role: json['role'] as String? ?? 'viewer',
      enabled: json['enabled'] as bool? ?? true,
      avatarUrl: (rawAvatar != null && rawAvatar.isNotEmpty) ? rawAvatar : null,
      lastLoginAt: _parseDate(json['last_login_at']),
      createdAt: _parseDate(json['created_at']) ?? DateTime.now(),
      updatedAt: _parseDate(json['updated_at']) ?? DateTime.now(),
    );
  }

  Map<String, dynamic> toJson() => {
    'id': id,
    'username': username,
    'role': role,
    'enabled': enabled,
    'avatar_url': avatarUrl,
  };
}

DateTime? _parseDate(dynamic v) {
  if (v == null) return null;
  if (v is String && v.isNotEmpty) {
    return DateTime.tryParse(v);
  }
  return null;
}
