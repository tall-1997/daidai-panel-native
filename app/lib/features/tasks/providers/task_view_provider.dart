import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_endpoints.dart';
import '../../../core/network/dio_client.dart';
import '../../../shared/models/task_view.dart';
import '../../../shared/utils/api_utils.dart';

class TaskViewState {
  final List<TaskView> items;
  final bool loading;
  final String? error;

  const TaskViewState({this.items = const [], this.loading = false, this.error});

  TaskViewState copyWith({List<TaskView>? items, bool? loading, String? error}) =>
      TaskViewState(items: items ?? this.items, loading: loading ?? this.loading, error: error);
}

final taskViewProvider =
    StateNotifierProvider<TaskViewNotifier, TaskViewState>((ref) => TaskViewNotifier());

class TaskViewNotifier extends StateNotifier<TaskViewState> {
  TaskViewNotifier() : super(const TaskViewState());

  Future<void> load() async {
    state = state.copyWith(loading: true, error: null);
    try {
      final response = await DioClient.instance.dio.get(ApiEndpoints.taskViews);
      final data = response.data;
      final items = data is List
          ? data.whereType<Map>().map((e) => TaskView.fromJson(Map<String, dynamic>.from(e))).toList()
          : <TaskView>[];
      state = TaskViewState(items: items);
    } catch (error) {
      state = state.copyWith(loading: false, error: extractErrorMessage(error, '任务视图加载失败'));
      rethrow;
    }
  }

  Future<void> save({int? id, required String name, required String filters, required String sortRules, bool hidden = false}) async {
    final data = {'name': name, 'filters': filters, 'sort_rules': sortRules, 'hidden': hidden};
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
    await DioClient.instance.dio.put(ApiEndpoints.taskViewsReorder, data: {
      'views': [for (var i = 0; i < views.length; i++) {'id': views[i].id, 'sort_order': i + 1, 'hidden': views[i].hidden}],
    });
    await load();
  }
}
