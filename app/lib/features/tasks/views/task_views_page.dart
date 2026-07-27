import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../shared/models/task_view.dart';
import '../../../shared/widgets/app_async_state.dart';
import '../../../shared/widgets/app_card.dart';
import '../providers/task_view_provider.dart';

class TaskViewsPage extends ConsumerStatefulWidget {
  const TaskViewsPage({super.key});

  @override
  ConsumerState<TaskViewsPage> createState() => _TaskViewsPageState();
}

class _TaskViewsPageState extends ConsumerState<TaskViewsPage> {
  @override
  void initState() {
    super.initState();
    Future.microtask(() => ref.read(taskViewProvider.notifier).load());
  }

  bool _validRules(String value) {
    try {
      return jsonDecode(value) is List;
    } catch (_) {
      return false;
    }
  }

  Future<void> _edit([TaskView? view]) async {
    final name = TextEditingController(text: view?.name);
    final filters = TextEditingController(text: view?.filters ?? '[]');
    final sortRules = TextEditingController(text: view?.sortRules ?? '[]');
    var hidden = view?.hidden ?? false;
    final save = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setDialogState) => AlertDialog(
          title: Text(view == null ? '新建视图' : '编辑视图'),
          content: SingleChildScrollView(
            child: Column(children: [
              TextField(controller: name, decoration: const InputDecoration(labelText: '名称')),
              TextField(controller: filters, maxLines: 3, decoration: const InputDecoration(labelText: '筛选规则 JSON')),
              TextField(controller: sortRules, maxLines: 3, decoration: const InputDecoration(labelText: '排序规则 JSON')),
              SwitchListTile(value: hidden, title: const Text('隐藏视图'), onChanged: (value) => setDialogState(() => hidden = value)),
            ]),
          ),
          actions: [
            AppLiquidGlassDialogActions(actions: [
              AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)),
              AppGlassDialogAction(label: '保存', onPressed: () => Navigator.pop(ctx, true)),
            ]),
          ],
        ),
      ),
    );
    if (save != true || !mounted) return;
    if (name.text.trim().isEmpty || !_validRules(filters.text) || !_validRules(sortRules.text)) {
      AppGlassNotice.show(context, '名称不能为空，筛选和排序规则必须是 JSON 数组');
      return;
    }
    await ref.read(taskViewProvider.notifier).save(
      id: view?.id,
      name: name.text.trim(),
      filters: filters.text,
      sortRules: sortRules.text,
      hidden: hidden,
    );
  }

  Future<void> _delete(TaskView view) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('删除任务视图'),
        content: Text('确定删除「${view.name}」吗？'),
        actions: [
          AppLiquidGlassDialogActions(actions: [
            AppGlassDialogAction(label: '取消', onPressed: () => Navigator.pop(ctx, false)),
            AppGlassDialogAction(label: '删除', variant: AppLiquidGlassButtonVariant.danger, onPressed: () => Navigator.pop(ctx, true)),
          ]),
        ],
      ),
    );
    if (confirmed == true) await ref.read(taskViewProvider.notifier).delete(view.id);
  }

  @override
  Widget build(BuildContext context) {
    final state = ref.watch(taskViewProvider);
    return Scaffold(
      backgroundColor: Colors.transparent,
      appBar: AppBar(title: const Text('任务视图'), actions: [AppGlassIconButton(icon: Icons.add, onTap: () => _edit())]),
      body: AppAsyncState(
        loading: state.loading,
        error: state.error,
        empty: state.items.isEmpty,
        emptyText: '暂无任务视图',
        onRetry: () => ref.read(taskViewProvider.notifier).load(),
        child: ReorderableListView(
          onReorder: (oldIndex, newIndex) {
            final views = [...state.items];
            if (newIndex > oldIndex) newIndex--;
            views.insert(newIndex, views.removeAt(oldIndex));
            ref.read(taskViewProvider.notifier).reorder(views);
          },
          padding: const EdgeInsets.all(20),
          children: [
            for (final view in state.items)
              AppCard(
                key: ValueKey(view.id),
                margin: const EdgeInsets.only(bottom: 8),
                child: ListTile(
                  title: Text(view.name),
                  subtitle: Text(view.hidden ? '已隐藏' : '可见'),
                  onTap: () => _edit(view),
                  trailing: IconButton(icon: const Icon(Icons.delete_outline), onPressed: () => _delete(view)),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
