class ApiResponse<T> {
  final int code;
  final String message;
  final T? data;

  const ApiResponse({required this.code, this.message = '', this.data});

  bool get isSuccess => code == 200;
}

class PaginatedData<T> {
  final List<T> items;
  final int total;

  const PaginatedData({required this.items, required this.total});

  bool get isEmpty => items.isEmpty;
  int get length => items.length;
}
