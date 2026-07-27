class ApiEndpoints {
  static const String baseApi = '/api';
  static const String baseApiV1 = '/api/v1';

  // Auth
  static const String checkInit = '$baseApi/auth/check-init';
  static const String init = '$baseApi/auth/init';
  static const String login = '$baseApi/auth/login';
  static const String logout = '$baseApi/auth/logout';
  static const String refresh = '$baseApi/auth/refresh';
  static const String user = '$baseApi/auth/user';
  static const String authUsername = '$baseApi/auth/username';
  static const String authPassword = '$baseApi/auth/password';
  static const String authAvatar = '$baseApi/auth/avatar';
  static const String captchaConfig = '$baseApi/auth/captcha-config';

  // System
  static const String health = '$baseApiV1/health';
  static const String systemInfo = '$baseApi/system/info';
  static const String dashboard = '$baseApi/system/dashboard';
  static const String systemVersion = '$baseApi/system/version';
  static const String checkUpdate = '$baseApi/system/check-update';
  static const String panelSettings = '$baseApi/system/panel-settings';
  static const String panelLog = '$baseApi/system/panel-log';
  static const String sponsors = '$baseApi/sponsors';
  static const String backup = '$baseApi/system/backup';
  static const String backups = '$baseApi/system/backups';
  static const String backupUpload = '$baseApi/system/backup/upload';
  static String backupDownload(String filename) =>
      '$baseApi/system/backup/download?filename=${Uri.encodeQueryComponent(filename)}';
  static const String restore = '$baseApi/system/restore';
  static const String restoreProgress = '$baseApi/system/restore/progress';
  static const String systemHealthCheck = '$baseApi/system/health-check';
  static const String taskViews = '$baseApi/tasks/views';
  static const String taskViewsReorder = '$baseApi/tasks/views/reorder';
  static String taskViewById(int id) => '$baseApi/tasks/views/$id';

  // Tasks
  static const String tasks = '$baseApi/tasks';
  static String taskById(int id) => '$baseApi/tasks/$id';
  static String taskRun(int id) => '$baseApi/tasks/$id/run';
  static String taskStop(int id) => '$baseApi/tasks/$id/stop';
  static String taskEnable(int id) => '$baseApi/tasks/$id/enable';
  static String taskDisable(int id) => '$baseApi/tasks/$id/disable';
  static String taskPin(int id) => '$baseApi/tasks/$id/pin';
  static String taskUnpin(int id) => '$baseApi/tasks/$id/unpin';
  static String taskCopy(int id) => '$baseApi/tasks/$id/copy';
  static String taskLatestLog(int id) => '$baseApi/tasks/$id/latest-log';
  static String taskLiveLogs(int id) => '$baseApi/tasks/$id/live-logs';
  static String taskLogFiles(int id) => '$baseApi/tasks/$id/log-files';
  static String taskStats(int id) => '$baseApi/tasks/$id/stats';
  static const String tasksBatchEnable = '$baseApi/tasks/batch/enable';
  static const String tasksBatchDisable = '$baseApi/tasks/batch/disable';
  static const String tasksBatchDelete = '$baseApi/tasks/batch/delete';
  static const String tasksBatchRun = '$baseApi/tasks/batch/run';
  static const String tasksExport = '$baseApi/tasks/export';
  static const String tasksImport = '$baseApi/tasks/import';
  static const String cronParse = '$baseApi/tasks/cron/parse';
  static const String cronTemplates = '$baseApi/tasks/cron/templates';
  static const String notificationChannels =
      '$baseApi/tasks/notification-channels';

  // Logs
  static const String logs = '$baseApi/logs';
  static String logById(int id) => '$baseApi/logs/$id';
  static String logStream(int id) => '$baseApiV1/logs/$id/stream';
  static const String logsBatchDelete = '$baseApi/logs/batch-delete';
  static const String logsClean = '$baseApi/logs/clean';

  // Scripts
  static const String scripts = '$baseApi/scripts';
  static const String scriptsTree = '$baseApi/scripts/tree';
  static const String scriptsContent = '$baseApi/scripts/content';
  static String scriptsDownload(String path) =>
      '$baseApi/scripts/download?path=${Uri.encodeQueryComponent(path)}';
  static const String scriptsUpload = '$baseApi/scripts/upload';
  static const String scriptsDirectory = '$baseApi/scripts/directory';
  static const String scriptsRename = '$baseApi/scripts/rename';
  static const String scriptsMove = '$baseApi/scripts/move';
  static const String scriptsCopy = '$baseApi/scripts/copy';
  static const String scriptsVersions = '$baseApi/scripts/versions';
  static String scriptVersionRollback(int id) =>
      '$baseApi/scripts/versions/$id/rollback';
  static const String scriptsRun = '$baseApi/scripts/run';
  static const String scriptsRunCode = '$baseApi/scripts/run-code';
  static String scriptsRunLogs(String runId) =>
      '$baseApi/scripts/run/$runId/logs';
  static String scriptsRunStop(String runId) =>
      '$baseApi/scripts/run/$runId/stop';
  static String scriptsRunClear(String runId) => '$baseApi/scripts/run/$runId';
  static const String scriptsFormat = '$baseApi/scripts/format';

  // Envs
  static const String envs = '$baseApi/envs';
  static String envById(int id) => '$baseApi/envs/$id';
  static String envEnable(int id) => '$baseApi/envs/$id/enable';
  static String envDisable(int id) => '$baseApi/envs/$id/disable';
  static const String envsBatchDelete = '$baseApi/envs/batch';
  static const String envsBatchEnable = '$baseApi/envs/batch/enable';
  static const String envsBatchDisable = '$baseApi/envs/batch/disable';
  static const String envsBatchGroup = '$baseApi/envs/batch/group';
  static const String envsSort = '$baseApi/envs/sort';
  static const String envsGroups = '$baseApi/envs/groups';
  static const String envsExportAll = '$baseApi/envs/export-all';
  static const String envsImport = '$baseApi/envs/import';

  // Subscriptions
  static const String subscriptions = '$baseApi/subscriptions';
  static String subscriptionById(int id) => '$baseApi/subscriptions/$id';
  static String subscriptionEnable(int id) =>
      '$baseApi/subscriptions/$id/enable';
  static String subscriptionDisable(int id) =>
      '$baseApi/subscriptions/$id/disable';
  static String subscriptionPull(int id) => '$baseApi/subscriptions/$id/pull';
  static String subscriptionPullStop(int id) =>
      '$baseApi/subscriptions/$id/pull/stop';
  static String subscriptionPullStream(int id) =>
      '$baseApiV1/subscriptions/$id/pull-stream';
  static String subscriptionLogs(int id) => '$baseApi/subscriptions/$id/logs';

  // Notifications
  static const String notifications = '$baseApi/notifications';
  static String notificationById(int id) => '$baseApi/notifications/$id';
  static String notificationEnable(int id) =>
      '$baseApi/notifications/$id/enable';
  static String notificationDisable(int id) =>
      '$baseApi/notifications/$id/disable';
  static String notificationTest(int id) => '$baseApi/notifications/$id/test';
  static const String notificationTypes = '$baseApi/notifications/types';
  static const String notificationSend = '$baseApi/notifications/send';

  // Deps
  static const String deps = '$baseApi/deps';
  static String depById(int id) => '$baseApi/deps/$id';
  static String depStatus(int id) => '$baseApi/deps/$id/status';
  static String depReinstall(int id) => '$baseApi/deps/$id/reinstall';
  static String depCancel(int id) => '$baseApi/deps/$id/cancel';
  static String depLogStream(int id) => '$baseApiV1/deps/$id/log-stream';
  static const String depsBatchDelete = '$baseApi/deps/batch-delete';
  static const String depsMirrors = '$baseApi/deps/mirrors';
  static const String depsPythonRuntimes = '$baseApi/deps/python-runtimes';
  static const String depsPythonRuntimeDefault =
      '$baseApi/deps/python-runtime-default';

  // Users
  static const String users = '$baseApi/users';
  static String userById(int id) => '$baseApi/users/$id';
  static String userResetPassword(int id) =>
      '$baseApi/users/$id/reset-password';

  // Security
  static const String security = '$baseApi/security';
  static const String loginLogs = '$baseApi/security/login-logs';
  static const String sessions = '$baseApi/security/sessions';
  static const String sessionsOthers = '$baseApi/security/sessions/others';
  static String sessionById(int id) => '$baseApi/security/sessions/$id';
  static const String ipWhitelist = '$baseApi/security/ip-whitelist';
  static String ipWhitelistById(int id) => '$baseApi/security/ip-whitelist/$id';
  static const String auditLogs = '$baseApi/security/audit-logs';
  static const String loginStats = '$baseApi/security/login-stats';
  static const String twoFaSetup = '$baseApi/security/2fa/setup';
  static const String twoFaVerify = '$baseApi/security/2fa/verify';
  static const String twoFa = '$baseApi/security/2fa';
  static const String twoFaStatus = '$baseApi/security/2fa/status';

  // Configs
  static const String configs = '$baseApi/configs';
  static const String configsBatch = '$baseApi/configs/batch';

  // SSH Keys
  static const String sshKeys = '$baseApi/ssh-keys';
  static String sshKeyById(int id) => '$baseApi/ssh-keys/$id';

  // Open API
  static const String openApiToken = '$baseApi/open-api/token';
  static const String openApiApps = '$baseApi/open-api/apps';
  static String openApiAppById(int id) => '$baseApi/open-api/apps/$id';
  static String openApiAppEnable(int id) => '$baseApi/open-api/apps/$id/enable';
  static String openApiAppDisable(int id) =>
      '$baseApi/open-api/apps/$id/disable';
  static String openApiAppResetSecret(int id) =>
      '$baseApi/open-api/apps/$id/reset-secret';
  static String openApiAppViewSecret(int id) =>
      '$baseApi/open-api/apps/$id/view-secret';
  static String openApiAppLogs(int id) => '$baseApi/open-api/apps/$id/logs';
  static const String platforms = '$baseApi/platform-tokens/platforms';
  static String platformById(int id) => '$baseApi/platform-tokens/platforms/$id';
  static const String platformTokens = '$baseApi/platform-tokens';
  static String platformTokenById(int id) => '$baseApi/platform-tokens/$id';
  static String platformTokenEnable(int id) => '$baseApi/platform-tokens/$id/enable';
  static String platformTokenDisable(int id) => '$baseApi/platform-tokens/$id/disable';
  static const String configScript = '$baseApi/system/config-script';
  static const String androidRuntimeStatus = '$baseApi/android-runtime/status';
  static const String androidRuntimeInstall = '$baseApi/android-runtime/install';
  static const String androidRuntimeUninstall = '$baseApi/android-runtime/uninstall';
  static const String depsPip = '$baseApi/deps/pip';
  static const String depsNpm = '$baseApi/deps/npm';
  static const String depsExport = '$baseApi/deps/export';
  static const String depsBatchReinstall = '$baseApi/deps/batch-reinstall';
  static const String localCapabilities = '$baseApi/local/capabilities';
  static const String localRuntimes = '$baseApi/local/runtimes';
  static String localRuntime(String id) =>
      '$localRuntimes/${Uri.encodeComponent(id)}';
  static String localRuntimeInstall(String id) =>
      '${localRuntime(id)}/install';
  static String localRuntimeDiagnostics(String id) =>
      '${localRuntime(id)}/diagnostics';
  static const String localOperations = '$baseApi/local/operations';
  static String localOperation(String id) =>
      '$localOperations/${Uri.encodeComponent(id)}';
  static String localOperationCancel(String id) =>
      '${localOperation(id)}/cancel';
  static String localOperationStream(String id) =>
      '$baseApiV1/local/operations/${Uri.encodeComponent(id)}/stream';
  static const String envsBatchRename = '$baseApi/envs/batch/rename';
  static String envMoveTop(int id) => '$baseApi/envs/$id/move-top';
  static String envCancelTop(int id) => '$baseApi/envs/$id/cancel-top';
  static const String envsExportFiles = '$baseApi/envs/export-files';
}
