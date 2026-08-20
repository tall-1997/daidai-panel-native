import 'package:daidai_app/features/settings/views/more_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('operator automation navigation follows route role policy', () {
    expect(showsOperatorAutomation('admin'), isTrue);
    expect(showsOperatorAutomation('operator'), isTrue);
    expect(showsOperatorAutomation('viewer'), isFalse);
    expect(showsOperatorAutomation(null), isFalse);
  });
}
