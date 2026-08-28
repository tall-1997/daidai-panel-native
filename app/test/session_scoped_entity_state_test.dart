import 'package:daidai_app/core/auth/auth_session_epoch.dart';
import 'package:daidai_app/core/network/panel_capability_registry.dart';
import 'package:daidai_app/features/scripts/views/script_list_page.dart';
import 'package:daidai_app/features/subscriptions/views/subscription_list_page.dart';
import 'package:daidai_app/features/tasks/providers/task_provider.dart';
import 'package:daidai_app/features/envs/views/env_list_page.dart';
import 'package:daidai_app/features/logs/views/log_list_page.dart';
import 'package:daidai_app/features/deps/views/dep_list_page.dart';
import 'package:daidai_app/features/dashboard/providers/dashboard_provider.dart';
import 'package:daidai_app/features/users/views/user_list_page.dart';
import 'package:daidai_app/shared/models/subscription.dart';
import 'package:daidai_app/shared/models/task.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  TaskNotifier taskNotifier() => TaskNotifier(
    initialState: TaskListState(
      tasks: [
        Task(
          id: 1,
          name: 'old',
          command: 'true',
          cronExpression: '',
          status: 0,
          createdAt: DateTime(2026),
          updatedAt: DateTime(2026),
        ),
      ],
      total: 1,
    ),
  );

  ScriptNotifier scriptNotifier() => ScriptNotifier(
    initialState: const ScriptState(
      tree: [ScriptFile(name: 'old.sh', path: '/old.sh')],
      selectedPath: '/old.sh',
      content: 'old',
    ),
  );

  SubscriptionListNotifier subscriptionNotifier() => SubscriptionListNotifier(
    initialState: SubscriptionListState(
      items: [
        Subscription(
          id: 1,
          name: 'old',
          createdAt: DateTime(2026),
          updatedAt: DateTime(2026),
        ),
      ],
      total: 1,
    ),
  );

  void expectCleared(
    TaskNotifier tasks,
    ScriptNotifier scripts,
    SubscriptionListNotifier subscriptions,
  ) {
    expect(tasks.state.tasks, isEmpty);
    expect(tasks.state.total, 0);
    expect(scripts.state.tree, isEmpty);
    expect(scripts.state.selectedPath, isNull);
    expect(scripts.state.content, isEmpty);
    expect(subscriptions.state.items, isEmpty);
    expect(subscriptions.state.total, 0);
  }

  test('auth epoch synchronously clears scoped entities', () {
    final tasks = taskNotifier();
    final scripts = scriptNotifier();
    final subscriptions = subscriptionNotifier();

    AuthSessionEpoch.advance();

    expectCleared(tasks, scripts, subscriptions);
    tasks.dispose();
    scripts.dispose();
    subscriptions.dispose();
  });

  test('panel scope synchronously clears scoped entities', () {
    final originalScope = PanelCapabilityRegistry.currentScope;
    final tasks = taskNotifier();
    final scripts = scriptNotifier();
    final subscriptions = subscriptionNotifier();

    PanelCapabilityRegistry.setCurrentScope('https://next.example.com');

    expectCleared(tasks, scripts, subscriptions);
    tasks.dispose();
    scripts.dispose();
    subscriptions.dispose();
    PanelCapabilityRegistry.setCurrentScope(originalScope);
  });

  test('auth epoch clears env log dep dashboard and user state', () {
    final envs = EnvListNotifier(
      initialState: const EnvListState(total: 1, keyword: 'old'),
    );
    final logs = LogListNotifier(
      initialState: const LogListState(total: 1, keyword: 'old'),
    );
    final deps = DepListNotifier(
      initialState: const DepListState(total: 1, selectedType: 'python'),
    );
    final dashboard = DashboardNotifier(
      initialState: const DashboardData(system: {'hostname': 'old'}),
    );
    final users = UserListNotifier(
      initialState: UserListState(
        items: [
          UserListItem(
            id: 1,
            username: 'old',
            role: 'admin',
            createdAt: DateTime(2026),
          ),
        ],
      ),
    );

    AuthSessionEpoch.advance();

    expect(envs.state.envs, isEmpty);
    expect(envs.state.total, 0);
    expect(envs.state.groups, isEmpty);
    expect(logs.state.logs, isEmpty);
    expect(logs.state.total, 0);
    expect(deps.state.items, isEmpty);
    expect(deps.state.total, 0);
    expect(dashboard.state.system, isEmpty);
    expect(dashboard.state.lastUpdated, isNull);
    expect(users.state.items, isEmpty);
    envs.dispose();
    logs.dispose();
    deps.dispose();
    dashboard.dispose();
    users.dispose();
  });

  test('panel scope clears env log dep dashboard and user state', () {
    final originalScope = PanelCapabilityRegistry.currentScope;
    final envs = EnvListNotifier(
      initialState: const EnvListState(total: 1),
    );
    final logs = LogListNotifier(
      initialState: const LogListState(total: 1),
    );
    final deps = DepListNotifier(
      initialState: const DepListState(total: 1),
    );
    final dashboard = DashboardNotifier(
      initialState: const DashboardData(dashboard: {'task_count': 1}),
    );
    final users = UserListNotifier(
      initialState: UserListState(
        items: [
          UserListItem(
            id: 1,
            username: 'old',
            role: 'admin',
            createdAt: DateTime(2026),
          ),
        ],
      ),
    );

    PanelCapabilityRegistry.setCurrentScope('https://scoped.example.com');

    expect(envs.state.total, 0);
    expect(logs.state.total, 0);
    expect(deps.state.total, 0);
    expect(dashboard.state.dashboard, isEmpty);
    expect(users.state.items, isEmpty);
    envs.dispose();
    logs.dispose();
    deps.dispose();
    dashboard.dispose();
    users.dispose();
    PanelCapabilityRegistry.setCurrentScope(originalScope);
  });
}
