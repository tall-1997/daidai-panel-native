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
  static const maxEntries = 1000;

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
    final next = [...entries, entry];
    if (next.length > maxEntries) {
      next.removeRange(0, next.length - maxEntries);
    }
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
