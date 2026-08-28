import 'package:daidai_app/core/services/task_completion_observer.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('notifies only when an observed running task reaches a terminal state', () {
    expect(
      shouldNotifyTaskCompletion(
        wasRunning: true,
        isRunning: false,
        isQueued: false,
      ),
      isTrue,
    );
    expect(
      shouldNotifyTaskCompletion(
        wasRunning: true,
        isRunning: false,
        isQueued: true,
      ),
      isFalse,
    );
    expect(
      shouldNotifyTaskCompletion(
        wasRunning: null,
        isRunning: false,
        isQueued: false,
      ),
      isFalse,
    );
  });

  test('stop and restart invalidate completion from an older request', () {
    final requests = TaskCompletionRequestGeneration();
    final stoppedRequest = requests.begin();

    requests.invalidate();
    final restartedRequest = requests.begin();

    expect(requests.isCurrent(stoppedRequest), isFalse);
    expect(requests.isCurrent(restartedRequest), isTrue);
  });
}
