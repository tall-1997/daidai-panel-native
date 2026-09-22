import 'package:daidai_app/core/services/app_update_service.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('cached installer is never reused without a digest or md5', () {
    expect(
      cachedInstallerHasRequiredMetadata(
        fileSize: 2 * 1024 * 1024,
        expectedSize: 0,
        expectedDigest: '',
        expectedMd5: '',
      ),
      isFalse,
    );
    expect(
      cachedInstallerHasRequiredMetadata(
        fileSize: 10,
        expectedSize: 10,
        expectedDigest: 'abc',
        expectedMd5: '',
      ),
      isTrue,
    );
    expect(
      cachedInstallerHasRequiredMetadata(
        fileSize: 9,
        expectedSize: 10,
        expectedDigest: 'abc',
        expectedMd5: '',
      ),
      isFalse,
    );
    expect(
      cachedInstallerHasRequiredMetadata(
        fileSize: 2 * 1024 * 1024,
        expectedSize: 0,
        expectedDigest: 'sha256:',
        expectedMd5: '',
      ),
      isFalse,
    );
  });

  test('github release picker skips drafts and keeps prereleases', () {
    expect(
      pickLatestPublishedGithubRelease([
        {'tag_name': 'v9', 'draft': true},
        {'tag_name': 'v2.0.1', 'draft': false, 'prerelease': true},
      ])?['tag_name'],
      'v2.0.1',
    );
    expect(pickLatestPublishedGithubRelease({'tag_name': 'v9', 'draft': true}), isNull);
  });

  test('release asset urls use the tag not latest', () {
    expect(
      githubReleaseAssetUrl(
        repo: 'tall-1997/daidai-panel-native',
        tag: 'v2.0.1',
        asset: 'android-update.json',
      ),
      'https://github.com/tall-1997/daidai-panel-native/releases/download/v2.0.1/android-update.json',
    );
  });
}
