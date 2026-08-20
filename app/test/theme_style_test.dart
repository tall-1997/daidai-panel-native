import 'package:daidai_app/core/theme/app_theme.dart';
import 'package:daidai_app/core/theme/theme_provider.dart';
import 'package:daidai_app/features/settings/views/theme_settings_page.dart';
import 'package:daidai_app/shared/widgets/app_background.dart';
import 'package:daidai_app/shared/widgets/app_card.dart';
import 'package:daidai_app/shared/widgets/main_scaffold.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:liquid_glass_easy/liquid_glass_easy.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('AppStyleSettings', () {
    test('defaults to Pure Flat and system color mode', () {
      const settings = AppStyleSettings();

      expect(settings.visualStyle, AppVisualStyle.pureFlat);
      expect(settings.themeMode, ThemeMode.system);
    });

    test('updates visual style independently', () {
      const settings = AppStyleSettings(
        themeMode: ThemeMode.dark,
        backgroundImagePath: '/background.png',
        blurIntensity: 8,
      );

      final updated = settings.copyWith(
        visualStyle: AppVisualStyle.liquidGlass,
      );

      expect(updated.visualStyle, AppVisualStyle.liquidGlass);
      expect(updated.themeMode, ThemeMode.dark);
      expect(updated.backgroundImagePath, '/background.png');
      expect(updated.blurIntensity, 8);
    });

    test('clears a configured background image', () {
      const settings = AppStyleSettings(
        backgroundImagePath: '/background.png',
      );

      expect(
        settings.copyWith(clearBackgroundImage: true).backgroundImagePath,
        isNull,
      );
    });
  });

  group('AppStyleNotifier', () {
    test('uses Pure Flat when the visual style setting is absent', () async {
      SharedPreferences.setMockInitialValues({'theme_mode': 2});
      final notifier = AppStyleNotifier();
      await notifier.initialized;

      expect(notifier.state.visualStyle, AppVisualStyle.pureFlat);
      expect(notifier.state.themeMode, ThemeMode.dark);
    });

    test('loads and persists Liquid Glass', () async {
      SharedPreferences.setMockInitialValues({
        'visual_style': AppVisualStyle.liquidGlass.name,
      });
      final notifier = AppStyleNotifier();
      await notifier.initialized;
      expect(notifier.state.visualStyle, AppVisualStyle.liquidGlass);

      await notifier.setVisualStyle(AppVisualStyle.pureFlat);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getString('visual_style'), AppVisualStyle.pureFlat.name);
    });

    test('falls back to Pure Flat for an unknown persisted value', () async {
      SharedPreferences.setMockInitialValues({'visual_style': 'unknown'});
      final notifier = AppStyleNotifier();
      await notifier.initialized;

      expect(notifier.state.visualStyle, AppVisualStyle.pureFlat);
    });

    test('does not overwrite a style changed while settings load', () async {
      SharedPreferences.setMockInitialValues({
        'theme_mode': ThemeMode.dark.index,
        'visual_style': AppVisualStyle.liquidGlass.name,
      });
      final notifier = AppStyleNotifier();

      await notifier.setVisualStyle(AppVisualStyle.pureFlat);
      await notifier.initialized;

      expect(notifier.state.visualStyle, AppVisualStyle.pureFlat);
      expect(notifier.state.themeMode, ThemeMode.dark);
    });
  });

  group('style-aware shared controls', () {
    test('Pure Flat theme uses opaque shared surfaces', () {
      final theme = AppTheme.light();
      final buttonColor = theme.outlinedButtonTheme.style?.backgroundColor
          ?.resolve(<WidgetState>{});

      expect(theme.colorScheme.surface.a, 1);
      expect(theme.cardTheme.color?.a, 1);
      expect(theme.inputDecorationTheme.fillColor?.a, 1);
      expect(theme.dialogTheme.backgroundColor?.a, 1);
      expect(theme.bottomSheetTheme.backgroundColor?.a, 1);
      expect(theme.popupMenuTheme.color?.a, 1);
      expect(theme.navigationBarTheme.backgroundColor?.a, 1);
      expect(buttonColor?.a, 1);
    });

    testWidgets('Pure Flat omits liquid glass render widgets', (tester) async {
      await tester.pumpWidget(
        _testApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.pureFlat),
        ),
      );

      expect(find.byType(LiquidGlassLens), findsNothing);
      expect(find.byType(LiquidGlassButton), findsNothing);
      expect(find.byType(LiquidGlassToggle), findsNothing);
      expect(find.byType(LiquidGlassSlider), findsNothing);
      expect(find.byType(Material), findsWidgets);
      expect(find.byType(Switch), findsOneWidget);
      expect(find.byType(Slider), findsOneWidget);

      final surfaceMaterial = tester.widget<Material>(
        find
            .descendant(
              of: find.byKey(const Key('test-surface')),
              matching: find.byType(Material),
            )
            .first,
      );
      expect(surfaceMaterial.color?.a, 1);
    });

    testWidgets('Liquid Glass uses liquid glass render widgets', (tester) async {
      await tester.pumpWidget(
        _testApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.liquidGlass),
        ),
      );

      expect(find.byType(LiquidGlassLens), findsWidgets);
      expect(find.byType(LiquidGlassButton), findsOneWidget);
      expect(find.byType(LiquidGlassToggle), findsOneWidget);
      expect(find.byType(LiquidGlassSlider), findsOneWidget);
    });
  });

  group('style-aware page rendering', () {
    testWidgets('Pure Flat main shell omits liquid glass page widgets', (
      tester,
    ) async {
      final router = _testRouter();
      addTearDown(router.dispose);

      await tester.pumpWidget(
        _routerTestApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.pureFlat),
          router,
        ),
      );

      expect(find.byType(NavigationBar), findsOneWidget);
      expect(find.byType(LiquidGlassScaffold), findsNothing);
      expect(find.byType(LiquidGlassView), findsNothing);
      expect(find.byType(LiquidGlassBottomNavBar), findsNothing);
    });

    testWidgets('Pure Flat background extends behind the status bar', (
      tester,
    ) async {
      final router = _testRouter();
      addTearDown(router.dispose);

      await tester.pumpWidget(
        _routerTestApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.pureFlat),
          router,
        ),
      );

      final overlayFinder = find.byKey(const ValueKey('pure-flat-system-ui'));
      final overlay = tester.widget<AnnotatedRegion<SystemUiOverlayStyle>>(
        overlayFinder,
      );
      expect(overlay.value.statusBarColor, Colors.transparent);
      expect(overlay.value.systemStatusBarContrastEnforced, isFalse);

      final scaffoldFinder = find.ancestor(
        of: find.byType(NavigationBar),
        matching: find.byType(Scaffold),
      );
      final scaffold = tester.widget<Scaffold>(scaffoldFinder.first);
      final body = scaffold.body! as Stack;
      expect(body.children.first, isNot(isA<SafeArea>()));
      expect(body.children.last, isA<SafeArea>());
      expect(find.byType(LiquidGlassView), findsNothing);
    });

    testWidgets('Liquid Glass main shell retains liquid glass page widgets', (
      tester,
    ) async {
      final router = _testRouter();
      addTearDown(router.dispose);

      await tester.pumpWidget(
        _routerTestApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.liquidGlass),
          router,
        ),
      );

      expect(find.byType(LiquidGlassScaffold), findsOneWidget);
    });

    testWidgets('AppBackground selects the matching render path', (
      tester,
    ) async {
      await tester.pumpWidget(
        _backgroundTestApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.pureFlat),
        ),
      );
      expect(find.byType(LiquidGlassView), findsNothing);

      await tester.pumpWidget(
        _backgroundTestApp(
          const AppStyleSettings(visualStyle: AppVisualStyle.liquidGlass),
        ),
      );
      expect(find.byType(LiquidGlassView), findsOneWidget);
    });

    testWidgets('Theme Settings shows blur controls only for Liquid Glass', (
      tester,
    ) async {
      await tester.pumpWidget(
        _themeSettingsTestApp(
          const AppStyleSettings(
            visualStyle: AppVisualStyle.pureFlat,
            backgroundImagePath: '/missing-background.png',
          ),
        ),
      );
      expect(find.text('模糊强度'), findsNothing);

      await tester.tap(find.text('Liquid Glass'));
      await tester.pump();
      await tester.drag(find.byType(ListView), const Offset(0, -400));
      await tester.pump();

      expect(find.text('模糊强度'), findsOneWidget);
      expect(find.byType(LiquidGlassSlider), findsOneWidget);
    });
  });
}

Widget _testApp(AppStyleSettings settings) {
  return ProviderScope(
    key: ValueKey(settings.visualStyle),
    overrides: [appStyleProvider.overrideWith((ref) => _FixedStyleNotifier(settings))],
    child: MaterialApp(
      theme: AppTheme.light(visualStyle: settings.visualStyle),
      home: Scaffold(
        body: Column(
          children: [
            const AppCard(child: Text('Card')),
            AppListTile(icon: Icons.list, title: 'List tile', onTap: () {}),
            AppLiquidGlassSurface(
              key: const Key('test-surface'),
              child: const SizedBox(
                key: Key('test-surface-content'),
              ),
            ),
            AppStyleSlider(value: 0.5, onChanged: (_) {}),
            AppLiquidGlassButton(label: 'Button', onPressed: () {}),
            AppLiquidGlassToggle(value: true, onChanged: (_) {}),
          ],
        ),
      ),
    ),
  );
}

GoRouter _testRouter() {
  return GoRouter(
    initialLocation: '/dashboard',
    routes: [
      GoRoute(
        path: '/dashboard',
        builder: (context, state) => const MainScaffold(
          child: Center(child: Text('Dashboard')),
        ),
      ),
    ],
  );
}

Widget _routerTestApp(AppStyleSettings settings, GoRouter router) {
  return ProviderScope(
    key: ValueKey(settings.visualStyle),
    overrides: [
      appStyleProvider.overrideWith((ref) => _FixedStyleNotifier(settings)),
    ],
    child: MaterialApp.router(
      theme: AppTheme.light(visualStyle: settings.visualStyle),
      routerConfig: router,
    ),
  );
}

Widget _backgroundTestApp(AppStyleSettings settings) {
  return ProviderScope(
    key: ValueKey(settings.visualStyle),
    overrides: [
      appStyleProvider.overrideWith((ref) => _FixedStyleNotifier(settings)),
    ],
    child: MaterialApp(
      theme: AppTheme.light(visualStyle: settings.visualStyle),
      home: const AppBackground(child: Text('Content')),
    ),
  );
}

Widget _themeSettingsTestApp(AppStyleSettings settings) {
  return ProviderScope(
    key: ValueKey(settings.visualStyle),
    overrides: [
      appStyleProvider.overrideWith((ref) => _FixedStyleNotifier(settings)),
    ],
    child: MaterialApp(
      theme: AppTheme.light(visualStyle: settings.visualStyle),
      home: const ThemeSettingsPage(),
    ),
  );
}

class _FixedStyleNotifier extends AppStyleNotifier {
  _FixedStyleNotifier(AppStyleSettings settings)
      : super(initialState: settings, loadPersistedSettings: false);
}
