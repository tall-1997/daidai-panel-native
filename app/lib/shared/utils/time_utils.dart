import 'package:intl/intl.dart';

final DateFormat _timeFullFormatter = DateFormat('yyyy年M月d日 HH:mm:ss');
final DateFormat _timeShortFormatter = DateFormat('M月d日 HH:mm');

String formatTimeCn(DateTime? value, {bool short = false}) {
  // 统一中文时间格式，避免页面里混用 MM-dd、yyyy/MM/dd 之类的显示方式。
  if (value == null) {
    return '-';
  }

  final localTime = value.toLocal();
  if (short) {
    return _timeShortFormatter.format(localTime);
  }
  return _timeFullFormatter.format(localTime);
}
