import '../../../shared/utils/bounded_log_buffer.dart';

enum DependencyLogPhase {
  connecting,
  streaming,
  reconnecting,
  succeeded,
  failed,
  cancelled,
  connectionError,
}

class DependencyLogState {
  static const maxEntries = defaultMaxLogLines;

  final List<String> entries;
  final DependencyLogPhase phase;
  final String? message;

  const DependencyLogState({
    this.entries = const [],
    this.phase = DependencyLogPhase.connecting,
    this.message,
  });

  bool get terminal => switch (phase) {
    DependencyLogPhase.succeeded ||
    DependencyLogPhase.failed ||
    DependencyLogPhase.cancelled ||
    DependencyLogPhase.connectionError => true,
    _ => false,
  };

  DependencyLogState add(String entry) {
    final next = [...entries];
    appendBoundedLogEntries(next, [entry]);
    return DependencyLogState(entries: next, phase: phase, message: message);
  }

  DependencyLogState transition(
    DependencyLogPhase nextPhase, {
    String? message,
  }) {
    if (terminal) return this;
    return DependencyLogState(
      entries: entries,
      phase: nextPhase,
      message: message,
    );
  }
}

DependencyLogPhase dependencyLogDonePhase(String data) {
  final normalized = data.trim().toLowerCase();
  if (normalized == 'cancelled' || normalized == 'canceled') {
    return DependencyLogPhase.cancelled;
  }
  if (normalized == 'failed' ||
      normalized == 'timeout' ||
      normalized.startsWith('error')) {
    return DependencyLogPhase.failed;
  }
  return DependencyLogPhase.succeeded;
}
