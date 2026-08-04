enum DependencyOperationState {
  pending,
  running,
  success,
  failed,
  aborted,
  unknown,
  canceled,
}

class DependencyOperation {
  final String id;
  final String kind;
  final DependencyOperationState state;
  final String phase;
  final double progress;
  final bool cancellable;
  final int sequence;
  final int? exitCode;
  final String errorCode;
  final String message;

  const DependencyOperation({
    required this.id,
    required this.kind,
    this.state = DependencyOperationState.pending,
    this.phase = '',
    this.progress = 0,
    this.cancellable = false,
    this.sequence = 0,
    this.exitCode,
    this.errorCode = '',
    this.message = '',
  });

  bool get terminal => switch (state) {
    DependencyOperationState.success ||
    DependencyOperationState.failed ||
    DependencyOperationState.aborted ||
    DependencyOperationState.unknown ||
    DependencyOperationState.canceled => true,
    _ => false,
  };

  factory DependencyOperation.fromJson(Map<String, dynamic> json) {
    final rawProgress = json['progress'];
    final parsedProgress = rawProgress is num
        ? rawProgress.toDouble()
        : double.tryParse(rawProgress?.toString() ?? '') ?? 0;
    return DependencyOperation(
      id: (json['id'] ?? json['operation_id'])?.toString() ?? '',
      kind: json['kind']?.toString() ?? '',
      state: DependencyOperationState.values.firstWhere(
        (state) => state.name == json['state']?.toString().toLowerCase(),
        orElse: () => DependencyOperationState.unknown,
      ),
      phase: (json['phase'] ?? json['stage'])?.toString() ?? '',
      progress: parsedProgress.clamp(0, 100).toDouble(),
      cancellable: json['cancellable'] == true,
      sequence: _int(json['sequence']),
      exitCode: _nullableInt(json['exit_code']),
      errorCode: json['error_code']?.toString() ?? '',
      message: json['message']?.toString() ?? '',
    );
  }
}

int _int(dynamic value) => _nullableInt(value) ?? 0;

int? _nullableInt(dynamic value) {
  if (value is num) return value.toInt();
  return int.tryParse(value?.toString() ?? '');
}
