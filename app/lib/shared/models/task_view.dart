class TaskView {
  final int id; final String name; final String filters; final String sortRules; final bool hidden; final int sortOrder;
  const TaskView({required this.id, required this.name, required this.filters, required this.sortRules, required this.hidden, required this.sortOrder});
  factory TaskView.fromJson(Map<String,dynamic> j)=>TaskView(id:(j['id'] as num).toInt(),name:j['name']?.toString()??'',filters:j['filters']?.toString()??'[]',sortRules:j['sort_rules']?.toString()??'[]',hidden:j['hidden']==true,sortOrder:(j['sort_order'] as num?)?.toInt()??0);
}
