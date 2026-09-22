import 'package:daidai_app/features/login/login_url_scheme.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('defaultSchemeForServerUrl', () {
    test('uses http for loopback, emulator and RFC1918 hosts', () {
      expect(defaultSchemeForServerUrl('127.0.0.1:5700'), 'http');
      expect(defaultSchemeForServerUrl('localhost'), 'http');
      expect(defaultSchemeForServerUrl('10.0.2.2:5700'), 'http');
      expect(defaultSchemeForServerUrl('10.0.3.2'), 'http');
      expect(defaultSchemeForServerUrl('192.168.1.8:5700'), 'http');
      expect(defaultSchemeForServerUrl('10.0.0.4'), 'http');
      expect(defaultSchemeForServerUrl('172.16.0.2'), 'http');
      expect(defaultSchemeForServerUrl('169.254.1.1:5700'), 'http');
      expect(defaultSchemeForServerUrl('100.64.0.10'), 'http');
      expect(defaultSchemeForServerUrl('[::1]:5700'), 'http');
      expect(defaultSchemeForServerUrl('fe80::1'), 'http');
    });

    test('uses https for public hosts', () {
      expect(defaultSchemeForServerUrl('panel.example.com'), 'https');
      expect(defaultSchemeForServerUrl('example.com:443'), 'https');
      expect(defaultSchemeForServerUrl('8.8.8.8'), 'https');
    });
  });
}
