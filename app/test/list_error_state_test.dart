import 'package:daidai_app/features/scripts/views/script_list_page.dart';
import 'package:daidai_app/features/users/views/user_list_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('user list error can be retained and cleared', () {
    final failed = const UserListState().copyWith(error: '用户加载失败');

    expect(failed.copyWith(loading: true).error, '用户加载失败');
    expect(failed.copyWith(clearError: true).error, isNull);
  });

  test('script list error can be retained and cleared', () {
    final failed = const ScriptState().copyWith(error: '脚本加载失败');

    expect(failed.copyWith(loading: true).error, '脚本加载失败');
    expect(failed.copyWith(clearError: true).error, isNull);
  });
}
