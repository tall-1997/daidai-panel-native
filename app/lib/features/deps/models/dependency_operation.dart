enum DependencyOperationState {
  queued,
  running,
  succeeded,
  failed,
  cancelled,
  timedOut,
}

class DependencyOperation {
  final String id;
  final String kind;
  final DependencyOperationState state;
  final String stage;
  final double progress;
  final bool cancellable;
  final int sequence;
  final int? exitCode;
  final String errorCode;
  final String message;

  const DependencyOperation({
    required this.id,
    required this.kind,
    this.state = DependencyOperationState.queued,
    this.stage = '',
    this.progress = 0,
    this.cancellable = false,
    this.sequence = 0,
    this.exitCode,
    this.errorCode = '',
    this.message = '',
  });

  bool get terminal => switch (state) {
    DependencyOperationState.succeeded ||
    DependencyOperationState.failed ||
    DependencyOperationState.cancelled ||
    DependencyOperationState.timedOut => true,
    _ => false,
  };

  factory DependencyOperation.fromJson(Map<String, dynamic> json) {
    final rawProgress = json['progress'];
    final parsedProgress = rawProgress is num
        ? rawProgress.toDouble()
        : double.tryParse(rawProgress?.toString() ?? '') ?? 0;
    return DependencyOperation(
      id: json['id']?.toString() ?? '',
      kind: json['kind']?.toString() ?? '',
      state: DependencyOperationState.values.firstWhere(
        (state) => state.name == json['state']?.toString(),
        orElse: () => DependencyOperationState.failed,
      ),
      stage: json['stage']?.toString() ?? '',
      progress: parsedProgress.clamp(0, 1).toDouble(),
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
