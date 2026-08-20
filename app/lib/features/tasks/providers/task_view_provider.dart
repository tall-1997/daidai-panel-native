import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../core/network/panel_capability_registry.dart';
import '../../../core/auth/auth_session_epoch.dart';
import '../../../shared/models/task_view.dart';
import '../../../shared/utils/api_utils.dart';

class TaskViewState {
  final List<TaskView> items;
  final bool loading;
  final String? error;
  final bool supported;

  const TaskViewState({
    this.items = const [],
    this.loading = false,
    this.error,
    this.supported = true,
  });

  TaskViewState copyWith({
    List<TaskView>? items,
    bool? loading,
    String? error,
    bool? supported,
  }) => TaskViewState(
    items: items ?? this.items,
    loading: loading ?? this.loading,
    error: error,
    supported: supported ?? this.supported,
  );
}

final taskViewProvider = StateNotifierProvider<TaskViewNotifier, TaskViewState>(
  (ref) => TaskViewNotifier(),
);

List<TaskView> parseTaskViewsResponse(dynamic responseData) {
  final data = extractData(responseData);
  if (data is! List) return const [];
  return data
      .whereType<Map>()
      .map((item) => TaskView.fromJson(Map<String, dynamic>.from(item)))
      .toList();
}

class TaskViewNotifier extends StateNotifier<TaskViewState> {
  TaskViewNotifier() : super(const TaskViewState());

  String? _scope;
  int _loadRequestId = 0;

  Future<void> load() async {
    final requestId = ++_loadRequestId;
    final capabilityScope = PanelCapabilityRegistry.currentScope;
    final sessionScope = AuthSessionEpoch.scoped(capabilityScope);
    if (_scope != sessionScope) {
      _scope = sessionScope;
      state = const TaskViewState();
    }
    if (PanelCapabilityRegistry.isUnsupported(
      PanelCapability.taskViews,
      scope: capabilityScope,
    )) {
      if (requestId == _loadRequestId) {
        state = const TaskViewState(supported: false);
      }
      return;
    }
    state = state.copyWith(loading: true, error: null);
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.taskViews);
      if (requestId != _loadRequestId ||
          sessionScope !=
              AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      PanelCapabilityRegistry.recordSupported(
        PanelCapability.taskViews,
        scope: capabilityScope,
      );
      final items = parseTaskViewsResponse(response.data);
      state = TaskViewState(items: items);
    } catch (error) {
      if (requestId != _loadRequestId ||
          sessionScope !=
              AuthSessionEpoch.scoped(PanelCapabilityRegistry.currentScope)) {
        return;
      }
      final capabilityState = PanelCapabilityRegistry.recordFailure(
        PanelCapability.taskViews,
        error,
        scope: capabilityScope,
      );
      if (capabilityState == PanelCapabilityState.unsupported) {
        state = const TaskViewState(supported: false);
        return;
      }
      state = state.copyWith(
        loading: false,
        error: extractErrorMessage(error, '任务视图加载失败'),
      );
    }
  }

  Future<void> save({
    int? id,
    required String name,
    required String filters,
    required String sortRules,
    bool hidden = false,
  }) async {
    final data = {
      'name': name,
      'filters': filters,
      'sort_rules': sortRules,
      'hidden': hidden,
    };
    if (id == null) {
      await DioClient.instance.dio.post(ApiEndpoints.taskViews, data: data);
    } else {
      await DioClient.instance.dio.put(ApiEndpoints.taskViewById(id), data: data);
    }
    await load();
  }

  Future<void> delete(int id) async {
    await DioClient.instance.dio.delete(ApiEndpoints.taskViewById(id));
    await load();
  }

  Future<void> reorder(List<TaskView> views) async {
    await DioClient.instance.dio.put(
      ApiEndpoints.taskViewsReorder,
      data: {
        'views': [
          for (var i = 0; i < views.length; i++)
            {
              'id': views[i].id,
              'sort_order': i + 1,
              'hidden': views[i].hidden,
            },
        ],
      },
    );
    await load();
  }
}
