import 'package:daidai_app/features/system/views/backup_page.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('backup picker accepts complete supported suffixes', () {
    for (final filename in [
      'backup.json',
      'backup.enc',
      'backup.tgz',
      'backup.tar.gz',
      'BACKUP.TGZ',
    ]) {
      expect(isSupportedBackupFilename(filename), isTrue, reason: filename);
    }
  });

  test('backup picker rejects unrelated and ambiguous gzip files', () {
    for (final filename in ['backup.gz', 'backup.zip', 'backup.tgz.json.exe', '']) {
      expect(isSupportedBackupFilename(filename), isFalse, reason: filename);
    }
  });

  test('restore progress retry uses bounded exponential backoff', () {
    expect(restoreProgressRetryDelay(0), const Duration(seconds: 1));
    expect(restoreProgressRetryDelay(1), const Duration(seconds: 2));
    expect(restoreProgressRetryDelay(4), const Duration(seconds: 16));
    expect(restoreProgressRetryDelay(20), const Duration(seconds: 16));
  });
}
