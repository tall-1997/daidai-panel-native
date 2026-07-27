class PythonRuntimeInfo {
  final String version;
  final String label;
  final bool isDefault;
  final String venvPath;
  final bool venvHealthy;
  final String pythonPath;
  final String pipPath;
  final bool available;
  final String message;

  const PythonRuntimeInfo({
    required this.version,
    required this.label,
    this.isDefault = false,
    this.venvPath = '',
    this.venvHealthy = false,
    this.pythonPath = '',
    this.pipPath = '',
    this.available = false,
    this.message = '',
  });

  factory PythonRuntimeInfo.fromJson(Map<String, dynamic> json) {
    return PythonRuntimeInfo(
      version: json['version']?.toString() ?? '',
      label: json['label']?.toString() ?? '',
      isDefault: json['default'] == true,
      venvPath: json['venv_path']?.toString() ?? '',
      venvHealthy: json['venv_healthy'] == true,
      pythonPath: json['python_path']?.toString() ?? '',
      pipPath: json['pip_path']?.toString() ?? '',
      available: json['available'] == true,
      message: json['message']?.toString() ?? '',
    );
  }
}
