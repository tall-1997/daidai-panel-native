import 'local_panel_models.dart';

abstract interface class LocalPanelHost {
  Future<LocalPanelStatus> ensureStarted();

  Future<LocalPanelStatus> getStatus();

  Future<LocalPanelStatus> restart();

  Future<LocalPanelStatus> stop();

  Future<LocalPanelStatus> setPersistentSchedulingEnabled(bool enabled);

  Future<String> openBrowserPanel();

  Stream<LocalPanelStatus> watchStatus();
}
