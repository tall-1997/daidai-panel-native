# Flutter → 原生 Android 迁移规划

> 文档版本：v1.0（2026-09-13）
> 适用范围：`daidai-panel-native` 仓库（本地面板 App 客户端）
> 目标：将全部 17 个 Flutter 模块迁移到原生 Kotlin + Jetpack Compose，移除 Flutter 依赖与 MethodChannel，Kotlin core（LocalPanelStore 等）与 Compose UI 同进程整合。
> 本文件所有结论均基于仓库真实代码调研，关键依据标注了文件路径。

---

## 0. 调研结论速览（给决策者）

| 项 | 结论 |
|---|---|
| MethodChannel 契约 | **3 个活跃 channel（2 个 MethodChannel + 1 个 EventChannel）+ 1 个死 channel**；Dart 侧共调用 **11 个 method**（Kotlin 实现 10 个），其中 `openExternalUrl`（app_install）**Dart 调用但 Kotlin 未实现**，`com.daidai.app/root` 为无调用死通道（详见 §1.3、§6.2） |
| 本地 HTTP fallback 服务器 | **建议保留**。Flutter/Compose UI 的移动端数据通路是「UI → HTTP(REST) → LocalPanelHttpServer → LocalPanelStore」，与 Go 远端模式**同一套 API 契约**；`/local-ui` Web 面板与 `openBrowserPanel` 功能依赖它。消除的只是「控制面 MethodChannel」，不是 HTTP 数据面（详见 §2.4） |
| 数据库 | LocalPanelStore SQLite，**SCHEMA_VERSION=19，25 张表**（tasks 34 列、envs 9 列、dependencies 9 列等），由 Kotlin 单写者维护。**初期不建议 Compose UI 直连 SQLite**，走 HTTP 保持与 Go 契约一致（详见 §2.5） |
| 主题 | AppColors 共约 **50 个颜色令牌**（Primary/Slate 11 阶/液态玻璃 15 个/MIUIX 5 个/功能色 11 个/终端色 5 个/状态色 5 个），见 §1.5。**主题方案：采用 compose-miuix-ui/miuix 作为 Compose 主题/组件基础，替代 flutter_miuix**（已核实存在，GitHub 1225★，Compose Multiplatform UI 库） |
| 路由 | 代码实际 **39 条 path**（任务背景所写 41 条与代码不符，详见 §1.4 说明） |
| Go 后端 API | 449 条移动端 API 契约（389 supported + 60 android_equivalent，JWT 420 / 公开 29），见 `contracts/backend-api-mobile.json` |
| 工作拆分 | 建议 6 阶段路线图，约 45–55 人日，阶段 0+1（server_config + login 三路径试点）为核心验证点 |

---

## 1. 现状盘点

### 1.1 17 模块功能表（页面/功能/插件依赖/数据通路）

统计口径：`app/lib/features/*`，共 50 个 Dart 文件 / 34,266 行；`app/lib/core` 32 文件 / 6,833 行；`app/lib/shared` 25 文件 / 3,694 行；加 `app.dart`、`main.dart` 合计 **109 文件 / 约 4.5 万行**（与任务背景一致）。

| # | 模块 | 文件数/行数 | 页面（对应路由） | 核心功能 | 主要插件依赖 | 数据通路 |
|---|---|---|---|---|---|---|
| 1 | app_lock | 4 / 1,751 | AppLockSettingsPage（`/app-lock`）、AppLockGate（根门禁） | 应用锁、生物识别解锁、后台锁屏 | flutter_riverpod、local_auth、shared_preferences | 本地偏好 + local_auth |
| 2 | dashboard | 5 / 1,382 | DashboardPage（`/dashboard`） | 概览统计、趋势图、运行状态 | fl_chart、dio | dio → `/api/...` 聚合统计（见 `dashboard/providers/dashboard_provider.dart`） |
| 3 | deps | 4 / 2,439 | DepListPage（`/deps`）、DepLogStreamPage（`/deps/:id/log-stream`） | 依赖包管理、安装日志流 | dio、file_picker | dio REST + 日志流 |
| 4 | envs | 1 / 2,587 | EnvListPage（`/envs`） | 环境变量 CRUD、分组、启停 | dio | dio REST |
| 5 | login | 3 / 1,353 | AppBootPage（`/boot`）、LoginPage（`/login`） | 启动分流、登录、极验 WebView 滑块验证码 | dio、webview_flutter、flutter_secure_storage、shared_preferences | dio `/api/auth/*`；Geetest 用 `webview_flutter`（`features/login/widgets/geetest_captcha_dialog.dart`） |
| 6 | logs | 2 / 1,429 | LogListPage（`/logs`）、LogStreamPage（`/logs/:id/stream`） | 任务日志列表、实时日志流 | dio、sse_client | dio REST + SSE（`core/network/sse_client.dart`） |
| 7 | notifications | 2 / 1,990 | NotificationListPage（`/notifications`）、LocalNotificationSettingsPage（`/local-notifications`） | 通知渠道管理、发送测试、本地通知设置 | flutter_local_notifications、dio、shared_preferences | dio REST + 本地通知插件（`core/services/local_notification_service.dart`） |
| 8 | openapi | 1 / 1,289 | OpenApiPage（`/open-api`）、OpenApiLogsPage（`/open-api/:id/logs`） | OpenAPI 应用/令牌/日志 | dio | dio REST |
| 9 | profile | 1 / 186 | ProfilePage（`/profile`） | 用户信息、头像 | dio | dio REST |
| 10 | scripts | 2 / 3,257 | ScriptListPage（`/scripts`）、ScriptViewPage（`/scripts/view`） | 脚本列表、版本管理、编辑器、执行 | dio、file_picker | dio REST |
| 11 | security | 3 / 2,352 | SecurityPage（`/security`）、SshKeysPage（`/ssh-keys`） | 安全审计、登录日志、IP 白名单、SSH 密钥 | dio | dio REST |
| 12 | server_config | 1 / 531 | ServerConfigPage（`/server-config`） | 服务器地址/多面板管理、连接检测、托管本地面板接管 | MethodChannelLocalPanelHost、SecureStorage、dio | 本地存储 + MethodChannel 控制 + dio 连通性检测（`features/server_config/views/server_config_page.dart`） |
| 13 | settings | 3 / 1,505 | MorePage（`/more`）、SponsorPage（`/sponsor`）、ThemeSettingsPage（`/theme-settings`） | 设置聚合、主题切换、赞助 | flutter_riverpod、shared_preferences | 本地偏好（`core/theme/theme_provider.dart`） |
| 14 | subscriptions | 1 / 1,863 | SubscriptionListPage（`/subscriptions`）、SubscriptionPullStreamPage（`/subscriptions/:id/pull-stream`）、SubscriptionLogsPage（`/subscriptions/:id/logs`） | 订阅管理、拉取流、日志 | dio、sse_client | dio REST + SSE |
| 15 | system | 8 / 3,974 | SystemSettingsPage、PanelSettingsPage、PanelLogPage、BackupPage、HealthCheckPage、ConfigScriptPage、PlatformTokensPage、CapabilityUnavailablePage、AndroidRuntimePage（`/system-settings`、`/panel-settings`、`/panel-log`、`/backup`、`/health-check`、`/config-script`、`/platform-tokens`、`/android-runtime`） | 系统设置、面板设置、备份恢复、健康诊断、配置脚本、平台令牌、Android 运行时 | dio、file_picker、MethodChannelLocalPanelHost（`system_settings_page.dart` 调用 getStatus/restart） | dio REST + MethodChannel 控制 + capability 门控（`core/network/panel_capability_registry.dart`） |
| 16 | tasks | 8 / 5,581 | TaskListPage、TaskFormPage、TaskViewsPage、TaskLiveLogPage（`/tasks`、`/tasks/new`、`/tasks/edit`、`/tasks/:id/live-logs`、`/task-views`） | 任务 CRUD、定时调度、实时日志 | dio、sse_client、crypto | dio REST + SSE 实时日志（`features/tasks/views/task_list_page.dart`） |
| 17 | users | 1 / 797 | UserListPage（`/users`） | 用户管理 | dio | dio REST |

**关键观察**：
- 数据通路高度统一：**除 app_lock / settings / server_config 的本地偏好与控制外，全部业务模块走 dio → REST `/api/*`**；实时流（任务日志、订阅拉取、依赖日志）走 **SSE**（`sse_client.dart`，基于 `http` 包手写）。
- 「托管本地面板」模式下，UI 并不直连 Kotlin core 对象，而是通过 `LocalPanelHttpServer` 暴露的 `http://127.0.0.1:<port>/api/*` REST 接口（与 Go 同契约）。MethodChannel 只承担**控制面**（启停/状态/浏览器 URL）。
- capability 门控（`PanelCapabilityRegistry`）贯穿 system/tasks 等页面，用于远端 Go 不支持时降级或提示。

### 1.2 Flutter 插件 → 原生等价物映射表

来源：`app/pubspec.yaml`（20 个直接依赖，见附录 §6.1 完整明细表）。

| Flutter 插件（版本） | 用途 | 原生等价物（建议） |
|---|---|---|
| flutter_riverpod ^2.6.1 | 状态管理 / DI | **ViewModel + StateFlow（架构层）**，配合 Hilt ViewModel（见 §2.2） |
| go_router ^14.8.1 | 路由 | **Navigation Compose**（androidx.navigation:navigation-compose 2.8+） |
| dio ^5.7.0 | HTTP 客户端 | **Retrofit 2.11 + OkHttp 4.12**（或 Ktor client）；拦截器逻辑移植自 `core/auth/auth_interceptor.dart` |
| http ^1.2.2 | SSE 基础 | OkHttp（手写 SSE 解析，移植 `core/network/sse_client.dart` 逻辑） |
| flutter_secure_storage ^9.2.4 | 安全存储 | **EncryptedSharedPreferences**（androidx.security:security-crypto）或 Keystore 直管；已有 `LocalPanelStore`/`PanelProcessLocalToken` 可参考 |
| shared_preferences ^2.3.5 | 偏好存储 | **Jetpack DataStore Preferences**（androidx.datastore:datastore-preferences） |
| local_auth ^2.3.0 | 生物识别 | **androidx.biometric:biometric**（1.1.0） |
| crypto ^3.0.6 | MD5/SHA 摘要 | `java.security.MessageDigest`（MainActivity 已有 `digest()` 实现，第 308–325 行） |
| fl_chart ^0.70.2 | 图表 | **Vico**（com.patrykandpatrick.vico，Compose 原生）或 MPAndroidChart；dashboard 趋势图对应移植 |
| intl ^0.19.0 | 格式化 | `java.text.*` / `android.icu.*` / kotlinx-datetime |
| logger ^2.5.0 | 日志 | **Timber**（或沿用现有 log 文件方案） |
| device_info_plus ^11.2.2 | 设备信息 | `Build.VERSION` / `PackageManager`（`LocalPanelHttpServer` 已有 `appVersionName()` 等） |
| file_picker ^8.1.7 | 文件选择 | **SAF**：ActivityResultContracts.GetContent / OpenDocument（备份导入、脚本上传、依赖包选择） |
| package_info_plus ^8.3.1 | 包信息 | `PackageManager`（versionName/versionCode，参考 MainActivity `getInstalledApkInfo`） |
| path_provider ^2.1.5 | 目录路径 | `context.filesDir` / `cacheDir` / `getExternalFilesDir`（代码中大量等价用法在 core 内） |
| webview_flutter ^4.10.0 | WebView | **androidx.webkit** / 原生 `WebView` + `WebViewClient` + JS Bridge（极验验证码必须保留，见 §4） |
| flutter_local_notifications ^18.0.1 | 本地通知 | **androidx.core NotificationCompat** + WorkManager/前台服务调度（`LocalPanelHostService` 已在使用前台服务模式） |
| flutter_miuix ^1.1.1 | MIUIX/HyperOS 组件 | **compose-miuix-ui/miuix**（见 §2.6） |
| liquid_glass_easy ^3.3.1 | 液态玻璃效果 | Compose 自绘（Blur + 着色叠加），令牌见 §1.5「玻璃色板」 |
| cupertino_icons ^1.0.8 | 图标 | Material Icons / MIUIX 自带图标 |

### 1.3 MethodChannel 契约清单（channel/method/参数/返回）

调研文件：`app/lib/core/local_panel/method_channel_local_panel_host.dart`、`app/lib/core/services/app_update_service.dart`、`app/android/.../MainActivity.kt`、`RootMethodChannelPolicy.kt`、`LocalPanelServiceClient.kt`、`ILocalPanelService.aidl`。

**通道一览（3 活跃 + 1 死）**：

| Channel 名 | 类型 | 活跃 | 说明 |
|---|---|---|---|
| `com.daidai.panel/local_host` | MethodChannel | ✅ | 本地面板控制（Dart：`MethodChannelLocalPanelHost`；Kotlin：MainActivity 第 114–161 行） |
| `com.daidai.panel/local_host/events` | EventChannel | ✅ | 本地面板状态流（Kotlin：MainActivity 第 163–174 行） |
| `com.daidai.panel/app_install` | MethodChannel | ✅ | APK 更新/安装（Dart：`AppUpdateService._platform`；Kotlin：MainActivity 第 38–112 行） |
| `com.daidai.app/root` | MethodChannel | ❌ 死通道 | 仅 Kotlin 声明（MainActivity 第 32–36 行 + `RootMethodChannelPolicy` 全部 `NOT_IMPLEMENTED`），Dart 侧无任何调用 → **可删除** |

**`com.daidai.panel/local_host` — 6 个 method**：

| Method | Dart 参数 | 返回 | Kotlin 实现路径 |
|---|---|---|---|
| `ensureStarted` | 无 | LocalPanelStatus JSON map | MethodChannel → `LocalPanelServiceClient.ensureStarted` → AIDL `ensureStarted` → `LocalPanelRuntime.tryEnsureStarted` |
| `getStatus` | 无 | LocalPanelStatus JSON map | → `LocalPanelServiceClient.status` → AIDL `status` → `LocalPanelRuntime.status` |
| `restart` | 无 | LocalPanelStatus JSON map（并 emitAfter 广播） | → `restart` → AIDL `restart` |
| `stop` | 无 | LocalPanelStatus JSON map（并 emitAfter 广播） | → `stop` → AIDL `stop` |
| `setPersistentSchedulingEnabled` | `{enabled: Boolean}` | LocalPanelStatus JSON map（并 emitAfter） | → 按 enabled 启动前台服务（`LocalPanelHostService.ACTION_ENABLE_PERSISTENT`）→ `setPersistentSchedulingEnabled` |
| `openBrowserPanel` | 无 | String（浏览器面板 URL） | → `LocalPanelServiceClient.createBrowserUrl` → AIDL `createBrowserUrl` |

LocalPanelStatus JSON 关键字段（`local_panel_models.dart` / `LocalPanelHostService`）：`phase`（stopped/starting/migrating/ready/degraded/failed）、`base_url`、`instance_id`、`core_version`、`schema_version`、`failure_stage`、`message`、`foreground_service_enabled`、`local_token`、`fallback_mode`、`core_status`、`scheduler_host_state`、`scheduler_recovery_trigger`、`scheduler_guarantee_state`、`scheduler_guarantee_reason`、`scheduler_intervention`、`resource_snapshot`、`platform_capabilities`。

**`com.daidai.panel/local_host/events` — 1 个事件流**：
- 事件：LocalPanelStatus JSON map（restart/stop/setPersistentSchedulingEnabled 后由 Kotlin 主动推送；Dart 侧 `watchStatus()` 为广播流）。

**`com.daidai.panel/app_install` — 5 个 method**：

| Method | Dart 参数 | 返回 | 错误码 |
|---|---|---|---|
| `installApk` | `{path: String, sourceHost: String?}` | null | `INSTALL_ERROR`、`INVALID_ARGS` |
| `getInstalledApkInfo` | 无 | map：`packageName`、`versionName`、`versionCode`、`size`、`md5`、`sha256` | `APK_INFO_ERROR` |
| `getRuntimeAbi` | 无 | String（如 `arm64` / `x86_64`，来自 `AndroidLinuxRuntime.currentAbi()`） | — |
| `applyPatch` | `{patchPath: String, outputName: String}` | map：`{path}` | `PATCH_ERROR`、`INVALID_ARGS`（含目录穿越安全校验） |
| `openExternalUrl` | `{url: String}` | Boolean | ⚠️ **Dart 调用（app_update_service.dart 第 678–682 行）但 Kotlin 未实现**，落入 `else -> result.notImplemented()` → PlatformException → Dart 捕获返回 false。属隐藏缺陷，迁移时需决定实现或删除 |

**`com.daidai.app/root` — 死通道**：无 method 实现，Dart 无调用。

**迁移后 MethodChannel 的替代**：见 §2.4。

### 1.4 路由全集（path → 页面）

调研文件：`app/lib/core/router/app_router.dart`（共 419 行，39 条 `GoRoute.path`，`initialLocation = '/boot'`）。

> ⚠️ **差异说明**：任务背景写「41 条 path」，实际代码为 **39 条**（`app_router.dart` 第 165–416 行共 39 个 `GoRoute`）。如加上 `initialLocation` 则为 40 个可导航位置。迁移验收以本表为准。

| # | Path | 页面（目标 Widget） | 壳层级 | 备注 |
|---|---|---|---|---|
| 1 | `/boot` | AppBootPage | root | 启动分流 |
| 2 | `/server-config` | ServerConfigPage | root | 可 `manageMode` |
| 3 | `/login` | LoginPage | root | 支持 `?manual=1` |
| 4 | `/dashboard` | DashboardPage | shell（底栏 1） | |
| 5 | `/tasks` | TaskListPage | shell（底栏 2） | |
| 6 | `/logs` | LogListPage | shell（底栏 3） | |
| 7 | `/envs` | EnvListPage | shell（底栏 4） | |
| 8 | `/more` | MorePage | shell（底栏 5） | |
| 9 | `/task-views` | TaskViewsPage | root | |
| 10 | `/profile` | ProfilePage | root | |
| 11 | `/android-runtime` | AndroidRuntimePage | root | capability: android_runtime + runtimeMutation |
| 12 | `/config-script` | ConfigScriptPage | root | capability: config_script |
| 13 | `/platform-tokens` | PlatformTokensPage | root | capability: platform_tokens |
| 14 | `/health-check` | HealthCheckPage | root | capability: health_check |
| 15 | `/tasks/new` | TaskFormPage | root | `extra: TaskFormPrefill` |
| 16 | `/tasks/edit` | TaskFormPage | root | `extra: Task` |
| 17 | `/tasks/:id/live-logs` | TaskLiveLogPage | root | 参数：id；extra: 任务名 |
| 18 | `/logs/:id/stream` | LogStreamPage | root | 参数：id |
| 19 | `/subscriptions` | SubscriptionListPage | root | |
| 20 | `/subscriptions/:id/pull-stream` | SubscriptionPullStreamPage | root | 参数：id |
| 21 | `/subscriptions/:id/logs` | SubscriptionLogsPage | root | 参数：id；extra: 订阅名 |
| 22 | `/scripts` | ScriptListPage | root | |
| 23 | `/scripts/view` | ScriptViewPage | root | extra: 脚本路径 |
| 24 | `/notifications` | NotificationListPage | root | |
| 25 | `/local-notifications` | LocalNotificationSettingsPage | root | |
| 26 | `/deps` | DepListPage | root | |
| 27 | `/deps/:id/log-stream` | DepLogStreamPage | root | 参数：id |
| 28 | `/users` | UserListPage | root | |
| 29 | `/security` | SecurityPage | root | |
| 30 | `/app-lock` | AppLockSettingsPage | root | |
| 31 | `/theme-settings` | ThemeSettingsPage | root | |
| 32 | `/system-settings` | SystemSettingsPage | root | |
| 33 | `/panel-settings` | PanelSettingsPage | root | |
| 34 | `/panel-log` | PanelLogPage | root | |
| 35 | `/backup` | BackupPage | root | |
| 36 | `/open-api` | OpenApiPage | root | |
| 37 | `/ssh-keys` | SshKeysPage | root | |
| 38 | `/sponsor` | SponsorPage | root | |
| 39 | `/open-api/:id/logs` | OpenApiLogsPage | root | 参数：id |

结构特征：
- **5 个 shell 底部导航**（dashboard/tasks/logs/envs/more，`ShellRoute`，`_shellNavigatorKey`），其余为 root 级 push。
- **路由守卫**：第 84–162 行 redirect 逻辑基于登录态、角色（admin/operator 路由白名单）、capability；受保护位置在登录后回跳（`_pendingProtectedLocation`）。
- 参数传递方式：path 参数（id）+ `extra` 对象（Task / TaskFormPrefill / String），Navigation Compose 需用 navArgument + SavedStateHandle 等价方案。

### 1.5 主题令牌清单（AppColors / MIUIX 颜色）

调研文件：`app/lib/core/theme/app_theme.dart`（AppColors 第 6–68 行）、`app_miuix_theme.dart`、`theme_provider.dart`。

**主色（Primary）**：

| 令牌 | 值 | 语义 |
|---|---|---|
| `primary` | `#10B981` (Emerald-500) | 品牌主色 |
| `primaryLight` | `#D1FAE5` (Emerald-100) | 浅主色 |
| `primaryDark` | `#059669` (Emerald-600) | 深主色 |

**Slate 中性色（11 阶）**：`slate50 #F8FAFC`、`slate100 #F1F5F9`、`slate200 #E2E8F0`、`slate300 #CBD5E1`、`slate400 #94A3B8`、`slate500 #64748B`、`slate600 #475569`、`slate700 #334155`、`slate800 #1E293B`、`slate900 #0F172A`、`slate950 #020617`。

**液态玻璃色板（15 个）**：`glassBg #F2F2F7`、`glassCard #FFFFFF`、`glassCardBorder #E5E5EA`、`glassDivider #E5E5EA`、`lightPage #F4F7F9`、`darkPage #07111F`、`lightSurface 0xE6FFFFFF`、`darkSurface 0xD91A2638`、`lightSurfaceMuted 0xCCF8FAFC`、`darkSurfaceMuted 0xCC111C2D`、`lightBorder 0x99FFFFFF`、`darkBorder 0x6636475C`、`lightControl 0x2EFFFFFF`、`darkControl 0x52111C2D`、`lightControlPressed 0x52FFFFFF`、`darkControlPressed 0x70334459`。

**MIUIX 色（5 个）**：`miuixRed #E5534B`、`miuixGreen #30A14E`、`miuixBlue #3B82F6`、`miuixPurple #8B5CF6`、`miuixYellow #D4A017`。

**功能色（11 个）**：`blue500 #3B82F6`、`blue600 #2563EB`、`blue100 #DBEAFE`、`purple500 #8B5CF6`、`purple600 #7C3AED`、`purple100 #EDE9FE`、`red500 #EF4444`、`red600 #DC2626`、`red100 #FEE2E2`、`red50 #FEF2F2`、`amber500 #F59E0B`。

**日志终端色（5 个）**：`termBg #FFFFFF`、`termBgDark #000000`、`termText #0F172A`、`termBlue #60A5FA`、`termGreen #34D399`、`termRed #F87171`。

**状态色（5 个）**：`successColor = primary`、`errorColor = red500`、`warningColor = amber500`、`runningColor = primary`、`disabledColor = slate300`。

**主题结构**：
- `AppVisualStyle { miuix, liquidGlass }`，`AppTheme.light()/dark()` 生成两套 ThemeData；`AppMiuixTheme` 在 Material 之上注入 `MiuixThemeData`（`lightColorScheme().copy(primary: AppColors.primary, primaryVariant: AppColors.primaryDark)`）。
- 样式设置持久化键：`theme_mode`、`visual_style`、`background_image_path`、`blur_intensity`（`theme_provider.dart`）。

**Compose 落地**：颜色令牌原样映射为 `Color(0xFF10B981)` 常量对象；MIUIX 色板由 compose-miuix 的 `MiuixTheme`/`MiuixColors` 承载，主色覆盖方式与 Flutter 版一致（见 §2.6）。

---

## 2. 目标架构

### 2.1 分层（Compose UI / ViewModel(StateFlow) / Repository / Data）

```
app/src/main/kotlin/com/daidai/daidai_app/
├── ui/                     # Compose UI 层（Screen/Component/Theme）
│   ├── theme/              # AppColors.kt（令牌迁移自 app_theme.dart）+ MiuixTheme 接入
│   ├── navigation/         # NavHost + 路由表（迁移自 app_router.dart）
│   └── feature/<module>/   # 17 模块各自：Screen + 局部组件
├── presentation/           # ViewModel（StateFlow）+ UiState（sealed class）
├── domain/                 # 领域模型 + Repository 接口（与 Go 契约 DTO 对齐）
├── data/                   # Repository 实现
│   ├── remote/             # Retrofit 客户端（449 条 API 契约）
│   ├── localcore/          # LocalPanelRuntime/LocalPanelStore 直调门面（取代 MethodChannel）
│   └── prefs/              # DataStore / EncryptedSharedPreferences
└── core/                   # 现有 Kotlin core（LocalPanelStore、LocalPanelHostService、
                            #   LocalPanelHttpServer、AndroidLinuxRuntime 等，基本不动）
```

- **单向数据流**：Screen → ViewModel（StateFlow）→ Repository → Data；事件（点击）走 ViewModel，状态（StateFlow）驱动重组。
- 与现有 Riverpod 的对应关系：Provider/StateNotifierProvider → Hilt `@HiltViewModel` + `StateFlow`；`FutureProvider` → `UiState.Loading/Success/Error`。
- **核心原则**：`core/` 下 38 个 Kotlin 文件是既有资产，**保留不动**；新增 `data/localcore/` 作为「同进程直调」的唯一门面，禁止 UI 直接摸 core 内部对象。

### 2.2 依赖注入（Hilt/Koin 选型+理由）

**选型：Hilt（androidx.hilt:hilt-navigation-compose）。**

理由：
1. 现有 core 已是「单例 + ApplicationContext」风格（`LocalPanelRuntime` object、`LocalPanelStore` 构造注入 context），Hilt 的 `@Singleton` + `@ApplicationContext` 与其天然契合。
2. 需要与 **WorkManager、前台 Service（LocalPanelHostService）、Application** 集成——Hilt 对这三者有官方支持（`@HiltWorker`、`@AndroidEntryPoint`），Koin 无编译期校验。
3. ViewModel + Navigation Compose 集成开箱即用（`hiltViewModel()`）。
4. 团队若偏好轻量，可在 Repository 层用接口 + 简单 ServiceLocator 兜底，但推荐 Hilt 为主。

反方意见（记录在案）：Koin 启动更快、无 kapt/ksp 依赖，但多模块大项目上可维护性不如 Hilt；不推荐在本项目使用。

### 2.3 导航（Navigation Compose）

- 用 `androidx.navigation:navigation-compose` 重构 `app_router.dart` 的 39 条路由；5 个底部 tab 用 `NavigationBar` + 嵌套 NavHost（等价 shell 路由），root 页面用独立 NavHost 栈。
- **守卫逻辑**：`app_router.dart` 第 84–162 行的 redirect（登录态 / admin-operator 白名单 / capability 门控）→ Compose 的 `NavHost` 外层包一个 `rememberNavController` + 状态驱动跳转，或使用官方 `Navigation` 的 `navController.addOnDestinationChangedListener` + 守卫拦截。
- 参数传递：path 参数 → `navArgument { type = NavType.IntType/LongType }` + `SavedStateHandle`；对象参数（Task / TaskFormPrefill / String）→ 改为 **传 id + 在 ViewModel 内经 Repository 取数**（比 `extra` 更可恢复），String 小参可直接放 navArgument。
- 深链/通知跳转：`LocalNotificationService().setNavigationHandler` 的 push 逻辑迁移为 `navController.navigate(route)`。

### 2.4 MethodChannel 消除方案（core 同进程直调，本地 HTTP fallback 去留建议）

**总体思路：消除「控制面」MethodChannel/EventChannel，保留「数据面」HTTP。**

**2.4.1 控制面（`com.daidai.panel/local_host` + `local_host/events`）→ 同进程直调**

新增 `data/localcore/PanelCoreController`（单例门面），内部封装：
- `LocalPanelRuntime.tryEnsureStarted / status / restart / stop / setPersistentSchedulingEnabled`（已有 AIDL 服务端逻辑，直接调 object 即可，前台服务保活逻辑保留在 `LocalPanelHostService`）；
- `LocalPanelServiceClient.createBrowserUrl` 等价逻辑（`LocalPanelRuntime.createBrowserUrl`）；
- 状态广播：`LocalPanelRuntime` 已有状态缓存（`cachedResult`），改为暴露 `MutableStateFlow<LocalPanelStatus>`，替代 EventChannel 的 `watchStatus()`；
- Compose 侧 `ManagedLocalConnectionMonitor` 等价物订阅该 StateFlow。

收益：消除跨进程 AIDL 往返（MainActivity → LocalPanelServiceClient → AIDL → Service）中的 MethodChannel 段，UI 与 core 同进程直调；AIDL 服务与前台服务保留（进程隔离与保活仍需要）。

**2.4.2 数据面（HTTP REST）→ 保留 LocalPanelHttpServer**

`LocalPanelHttpServer`（NanoHTTPD，`0.0.0.0:随机端口`）服务两类内容：
1. `/api/*` REST 路由（与 Go 同契约：auth/tasks/envs/deps/configs/system/…，见 LocalPanelHttpServer.kt 第 200–274 行）；
2. `/local-ui` 本地 Web 面板 + `LocalBrowserAccess`（浏览器会话、host/origin/local-token 三重校验，第 94–168 行）。

**建议：保留**，理由：
1. **契约一致性**：移动端在「远程 Go」与「托管本地」两种模式下，Repository 层面对的是同一套 REST 接口（`DioClient` 只换 baseUrl）。移除 HTTP 服务器意味着 Compose UI 每种模式都要写两套数据通路（HTTP + 直连 SQLite），违背目标「双模式同契约」；
2. **Web 面板依赖**：`openBrowserPanel` 打开的正是 `/local-ui`（LocalWebAssets 打包的 web/ 前端），`LocalBrowserAccess` 整个模块依赖 HTTP 服务器存在；
3. **成本低**：服务器本身是现有资产，不动它就没有新风险；而直连 SQLite 会引入双写/迁移冲突（见 §2.5、§4）。

**注意**：`LocalPanelRuntime` 的 `endpoint`（`http://127.0.0.1:<port>`）仍作为托管本地面板的 baseUrl 提供给 Compose Repository；`local_token` 通过 `PanelProcessLocalToken`/HTTP 头继续传递。

**2.4.3 `com.daidai.panel/app_install` → 原生调用**

MainActivity 中 installApk/getInstalledApkInfo/getRuntimeAbi/applyPatch 逻辑（第 38–112 行）抽为 `core/AppUpdateManager`（或并入 `AppUpdateService` Kotlin 版），Compose 直接调用。`openExternalUrl` 未实现：**迁移时补齐**（`Intent.ACTION_VIEW` 打开 https URL）或删除该 Dart 调用点（见 §4 风险表）。

**2.4.4 `com.daidai.app/root` → 直接删除**（死通道）。

### 2.5 数据访问策略（SQLite 直连 vs Go API vs 混合）

**结论：混合，但以「经 HTTP（REST，与 Go 同契约）」为主，SQLite 只读缓存为辅（初期可不做）。**

| 场景 | 策略 | 理由 |
|---|---|---|
| 远程 Go 模式 | Retrofit → Go 449 条 API | 现状即如此，契约文件 `contracts/backend-api-mobile.json` 已有 449 条（389 supported + 60 android_equivalent） |
| 托管本地模式 | Retrofit → LocalPanelHttpServer（同契约） | 保持双模式同代码路径，`DioClient` 等价物只换 baseUrl |
| 本地恢复诊断模式 | Retrofit → 诊断端点（`fallbackMode == 'diagnostic'`） | 现状 `resolveManagedLocalDiagnostic` 已定义 |
| SQLite 直连 | **初期禁止写入，只读缓存可选** | LocalPanelStore 是唯一写者（SCHEMA_VERSION=19、25 张表、onUpgrade 2→18 迁移链）。UI 直连会引入双写、迁移并发、锁竞争风险；若确需（如离线缓存 envs/tasks），只读缓存由 Store 既有查询接口暴露，不另开写路径 |

DB schema 事实（调研自 `LocalPanelStore.kt`）：核心业务表 `local_users`(9 列)、`local_sessions`(7)、`tasks`(34)、`envs`(9)、`dependencies`(9)、`task_views`、`task_logs_local`、`script_versions`、`script_runs`、`notification_channels`、`local_subscriptions`、`operations`、`subscription_logs`、`ssh_keys`、`platforms`、`platform_tokens`、`open_api_apps`、`open_api_logs`、`open_api_tokens`、`security_login_logs`、`security_sessions`、`security_ip_whitelist`、`security_audit_logs`、`security_login_attempts`、`local_configs` 共 25 张。

### 2.6 主题方案：采用 compose-miuix-ui/miuix（替代 flutter_miuix）

**结论（已核实）**：MIUIX 有开源 Compose 原生实现 **`compose-miuix-ui/miuix`**（GitHub 仓库，约 1225★，定位 "A UI library for Compose Multiplatform"），可作为 Compose 主题/组件的直接替代，替代 flutter_miuix。

**集成方式（落地建议）**：
1. **版本**：以仓库 release 为准（实施时锁定最近稳定 tag；`flutter_miuix ^1.1.1` 对应的组件集——Button/Card/Dialog/Sheet/Icon/Toast 等在 compose-miuix 中均有对应）。
2. **坐标**：`implementation("top.yukozaki:compose-miuix:${version}")`（以仓库 README 实际发布坐标为准，实施第一步核对）。
3. **主题接入**（对应 `app_miuix_theme.dart`）：Compose 侧 `MiuixTheme` 包裹 `NavHost`；用 §1.5 令牌生成 `MiuixColors` 的 light/dark 变体，主色覆盖为 `AppColors.primary(#10B981)`、`primaryVariant(#059669)`，与 Flutter 版 `lightColorScheme().copy(primary: ..., primaryVariant: ...)` 一一对应。
4. **组件替换映射**：flutter_miuix 的 `MiuixButton/MiuixCard/MiuixDialog/MiuixSheet/MiuixIcon` → compose-miuix 同名组件；`AppBackground`/`AppLiquidGlassButton` 等自绘组件保留 Compose 自绘（令牌用 §1.5 玻璃色板）。
5. **视觉风格切换**：`AppVisualStyle.miuix/liquidGlass` 两套方案在 Compose 侧用 CompositionLocal 切换（`LocalAppVisualStyle`），持久化键沿用 `visual_style`。
6. **验证**：对照截图逐页比对（色值、圆角 16dp 卡片、12dp 输入框、SnackBar 浮动胶囊等，`app_theme.dart` 中均有具体数值）。

---

## 3. 分阶段路线图（依赖顺序）

> 预估人日为有经验工程师口径。各阶段「可否并行」指与相邻阶段是否可并行开展。

| 阶段 | 交付物 | 涉及模块 | 预估人日 | 验收标准 | 可否并行 |
|---|---|---|---|---|---|
| **阶段 0：工程骨架** | Compose 工程搭建（Activity + NavHost + Hilt + Theme 令牌库 + PanelCoreController 门面 + Retrofit 客户端骨架 + 449 条契约 DTO 生成）；删除 `com.daidai.app/root` 死通道 | 全局 | 5–7 | 空壳 App 能启动进入 `/boot`；Hilt 图可解析；AppColors 令牌迁移完成；Retrofit 客户端可 ping `/api/health`（Go 与本地各一） | —（前置） |
| **阶段 1：试点（server_config + login 三路径跑通）** | ServerConfigPage、AppBootPage、LoginPage Compose 版；登录态 StateFlow；SSE 基础库（OkHttp）移植；SecureStorage→EncryptedSharedPreferences 迁移；极验 WebView 原生版；三路径（远程 Go / 托管本地 / 本地恢复诊断）完整登录 | server_config、login、core(auth/network/storage) | 8–10 | 三路径均能登录并进入 dashboard（先占位页）；`com.daidai.panel/local_host` 控制面 MethodChannel 在试点页不再被使用；token 持久化互通 | 与阶段 0 微重叠（搭骨架即可开始） |
| **阶段 2：只读/观察类模块** | Dashboard、Logs、Profile、System 只读面（健康诊断/面板设置/面板日志/Android 运行时展示）Compose 版；capability 门控移植；fl_chart → Vico 图表 | dashboard、logs、profile、system(读) | 8–10 | 只读页面功能与 Flutter 版等价；SSE 日志流可用；capability 降级提示一致 | 可与阶段 3 并行（不同模块） |
| **阶段 3：业务 CRUD 模块** | Tasks、Envs、Deps、Scripts、Subscriptions、Users、Notifications Compose 版（列表+表单+实时流+通知渠道） | tasks、envs、deps、scripts、subscriptions、users、notifications | 12–15 | CRUD 全链路（含任务编辑表单、依赖安装日志、订阅拉取流）与 Flutter 版功能等价；本地模式与远程模式行为一致 | 可与阶段 2 并行 |
| **阶段 4：安全/设置/管理面** | Security、AppLock、Settings、OpenAPI、ServerConfig 管理模式、Backup/健康诊断写操作、主题切换（compose-miuix 全量接入） | security、app_lock、settings、openapi、system(写)、server_config | 6–8 | 应用锁/生物识别可用；备份恢复可用；主题 miuix/liquidGlass 双风格可切换；`app_install` 更新链路（installApk/applyPatch）原生版可用 | 依赖阶段 1 登录态 |
| **阶段 5：收尾去 Flutter** | 删除 `app/lib/`（或归档）、pubspec 依赖移除、`com.daidai.panel/local_host` 等 MethodChannel/EventChannel 删除、MainActivity 简化为 Compose Activity、回归测试 + 截图比对、发布流程切换（去掉 Flutter 打包步骤） | 全局 | 6–8 | 仓库不再有 Flutter 代码路径；APK 仅含原生实现；三模式回归通过；更新/升级链路（含 applyPatch）通过 | 依赖阶段 2–4 |

**合计：45–55 人日。** 关键路径：阶段 0 → 1 → 4 → 5（阶段 2/3 可与 4 部分重叠）。

---

## 4. 风险与缓解

| # | 风险 | 影响 | 缓解措施 |
|---|---|---|---|
| R1 | **Go 后端去留** | 449 条 API 契约（389 supported + 60 android_equivalent）是移动端数据层唯一标准。若后端被废弃，本地 fallback 成为唯一数据源，契约漂移风险高 | 迁移期间 **Go 后端保留**，Remote 模式仍是主数据源；以 `contracts/backend-api-mobile.json` 为契约单一来源，阶段 0 生成 DTO；后续是否下线 Go 单独决策，不影响本次迁移 |
| R2 | **极验 WebView 验证码** | 登录必经滑块验证，Compose 无现成等价 | 保留原生 WebView + JS Bridge（`geetest_captcha_dialog.dart` 逻辑 1:1 移植，`loadHtmlString + JavaScriptChannel('CaptchaBridge')` → `evaluateJavascript` + `addJavascriptInterface`）；阶段 1 必须验证通过 |
| R3 | **schema 迁移** | SCHEMA_VERSION=19，onUpgrade 覆盖 2→18；若 Compose UI 直连 SQLite 会与 Store 写路径竞争 | 初期**禁止 UI 直连写**（见 §2.5）；只读缓存若引入，也必须走 Store 既有接口；阶段 5 前不触碰 schema |
| R4 | **双栈过渡策略** | Flutter 与 Compose 并存期：路由、登录态、主题三处状态需同步，避免「两个壳」 | 采用**功能开关灰度**：以登录态/面板类型为开关，Compose 页面从阶段 1 起逐步接管路由，Flutter 页面保留兜底；状态以原生侧为准（StateFlow 单写者）；不搞 Flutter 嵌 Compose 或 Compose 嵌 Flutter 的混排，降低复杂度 |
| R5 | **SSE 实时流** | 任务实时日志/订阅拉取/依赖日志均依赖 SSE（`sse_client.dart` 手写），OkHttp 无内置 SSE | 用 OkHttp + `okhttp-sse`（或手写 `text/event-stream` 解析，移植现有状态机）在阶段 1 落地基础库，阶段 2/3 复用 |
| R6 | **MethodChannel 消除的契约遗漏** | `openExternalUrl` 等「Dart 调用但 Kotlin 未实现」的隐藏接口易漏 | 以 §6.2 完整契约为检查单，迁移时逐条处置（实现或删除）；`com.daidai.app/root` 死通道直接删除 |
| R7 | **应用锁/生物识别/通知的时序差异** | local_auth、flutter_local_notifications 有各自生命周期与回调语义 | 用 androidx.biometric 与 NotificationCompat/WorkManager 重写时，保持「后台→前台锁定」「通知点击路由」两个行为基准做回归用例 |
| R8 | **MIUIX 主题差异** | flutter_miuix 与 compose-miuix 组件 API 存在差异 | **已缓解**：compose-miuix-ui/miuix 已核实可作替代；阶段 0 建立主题令牌库 + 阶段 4 截图比对基线，逐组件核对圆角/间距/色彩 |
| R9 | **路由数差异** | 任务背景写 41 条路由，实际 39 条 | 以 §1.4 表为准，缺失页面在迁移清单中显式标注，避免「迁移了不存在的页面」 |
| R10 | **构建/发布流程** | 移除 Flutter 后，现有 Gradle（flutter-gradle-plugin、`packageSelectedRuntimeAssets` 等任务、assets 打包 local-web）需重构 | 阶段 5 保留本地 web 资产打包任务（`LocalWebAssets` 依赖 assets 目录），仅替换 flutter 插件与 FlutterActivity；发布流程去掉 Flutter 步骤，保留签名/混淆/运行时资产校验 |

---

## 5. 第一步实施建议（阶段 0 + 1 试点：server_config + login 三路径跑通）

**建议的第一步（2 周内）**，以此验证「UI 同进程直调 core + 保留 HTTP 数据面 + compose-miuix 主题」三条核心假设是否成立，再放量到其余模块。

### 5.1 具体任务清单

1. **工程骨架（阶段 0）**
   - 新建 `MainActivity : ComponentActivity()`（替换 FlutterActivity），接入 Compose + Hilt。
   - 迁移 `AppColors` 全部令牌到 `ui/theme/AppColors.kt`；引入 compose-miuix，`MiuixTheme` 包裹 NavHost，主色覆盖为 `#10B981`。
   - 建 `data/localcore/PanelCoreController`：封装 `LocalPanelRuntime` 启停/状态 + `MutableStateFlow<LocalPanelStatus>`（替代 MethodChannel + EventChannel）。
   - 建 Retrofit 客户端骨架，仅实现 auth 三接口（check-init / init / login）+ `/api/health`；baseUrl 由「当前面板」解析（远程 URL 或本地 endpoint）。
   - 建 `data/prefs/`：EncryptedSharedPreferences 迁移 `SecureStorage` 的 serverUrl/tokens/panel 配置。

2. **三路径试点（阶段 1）**
   - **路径 A 远程 Go**：ServerConfigPage 输入 `https://xxx` → Retrofit 调 `/api/auth/check-init`、`/api/auth/login` → 存 token → 进 dashboard 占位页。
   - **路径 B 托管本地**：boot 流程 `PanelCoreController.ensureStarted()` → 取 `LocalPanelStatus.base_url` + `local_token` → Retrofit 指向 `127.0.0.1:<port>`（带 `x-daidai-local-token` 头）→ `/api/auth/login`（本地账号）→ 进 dashboard。
   - **路径 C 本地恢复诊断**：`status.fallbackMode == 'diagnostic'` → 指向诊断端点，登录降级页面可通。
   - 极验验证码：原生 WebView 移植 `geetest_captcha_dialog.dart`。

3. **验收标准（阶段 0+1 出口条件）**
   - 三路径均能完成登录并进入 dashboard（dashboard 可为占位）；
   - 试点页面 **不再经过任何 MethodChannel**（`local_host` 控制面在 Compose 侧零引用）；
   - token 可被原生与后续页面共享（EncryptedSharedPreferences 落盘）；
   - compose-miuix 主题渲染与 Flutter 版截图一致（主色/圆角/深色模式）。

### 5.2 不建议做的事（第一步）

- ❌ 不要开始迁移 tasks/envs 等 CRUD 模块（依赖登录态与 SSE 基础库）。
- ❌ 不要让 Compose UI 直连 `LocalPanelStore` SQLite。
- ❌ 不要移除 `LocalPanelHttpServer` / 本地 Web 面板。
- ❌ 不要试图让 Flutter 与 Compose 页面混排互跳（复杂度高、收益低）。

---

## 6. 附录

### 6.1 插件替代方案明细表

来源：`app/pubspec.yaml`。版本为 pubspec 声明值。

| Flutter 插件 | 版本 | 使用文件数（app/lib） | 原生等价物 | 迁移难度 |
|---|---|---|---|---|
| flutter_riverpod | ^2.6.1 | 41 | ViewModel + StateFlow（架构层） | 中（重构量大） |
| go_router | ^14.8.1 | 23 | Navigation Compose | 中 |
| dio | ^5.7.0 | 19 | Retrofit + OkHttp | 中 |
| http | ^1.2.2 | 1 | OkHttp（SSE 手写解析） | 低 |
| flutter_secure_storage | ^9.2.4 | 1 | EncryptedSharedPreferences / Keystore | 低 |
| shared_preferences | ^2.3.5 | 4 | DataStore Preferences | 低 |
| local_auth | ^2.3.0 | 1 | androidx.biometric | 低 |
| crypto | ^3.0.6 | 2 | MessageDigest（MainActivity 已有） | 低 |
| fl_chart | ^0.70.2 | 1 | Vico | 中 |
| intl | ^0.19.0 | 2 | java.text / android.icu | 低 |
| logger | ^2.5.0 | 1 | Timber | 低 |
| device_info_plus | ^11.2.2 | 1 | Build / PackageManager | 低 |
| file_picker | ^8.1.7 | 8 | SAF（GetContent/OpenDocument） | 中 |
| package_info_plus | ^8.3.1 | 2 | PackageManager | 低 |
| path_provider | ^2.1.5 | 2 | context.filesDir / cacheDir | 低 |
| webview_flutter | ^4.10.0 | 1 | androidx WebView + JS Bridge | 中（极验必须验证） |
| flutter_local_notifications | ^18.0.1 | 1 | NotificationCompat + WorkManager | 中 |
| flutter_miuix | ^1.1.1 | 5 | **compose-miuix-ui/miuix** | 中 |
| liquid_glass_easy | ^3.3.1 | 4 | Compose 自绘（令牌 §1.5） | 中 |
| cupertino_icons | ^1.0.8 | — | Material Icons | 低 |

### 6.2 MethodChannel 契约完整表

**6.2.1 `com.daidai.panel/local_host`（MethodChannel，Dart 侧 `MethodChannelLocalPanelHost`）**

| Method | Dart 参数 | 返回 | Kotlin 处理 | 迁移后替代 |
|---|---|---|---|---|
| ensureStarted | — | LocalPanelStatus map | MainActivity L116 → LocalPanelServiceClient.ensureStarted → AIDL ensureStarted → LocalPanelRuntime.tryEnsureStarted | PanelCoreController.ensureStarted() |
| getStatus | — | LocalPanelStatus map | L117 → status → AIDL status → LocalPanelRuntime.status | PanelCoreController.status() |
| restart | — | LocalPanelStatus map（emitAfter） | L118 → restart（emitAfter=true 推事件） | PanelCoreController.restart() |
| stop | — | LocalPanelStatus map（emitAfter） | L119–121 → stop | PanelCoreController.stop() |
| setPersistentSchedulingEnabled | `{enabled: Boolean}` | LocalPanelStatus map（emitAfter） | L122–139 → 启动/管理前台服务 + setPersistentSchedulingEnabled | PanelCoreController.setPersistentSchedulingEnabled(Boolean) |
| openBrowserPanel | — | String（面板 URL） | L140–157 → createBrowserUrl → AIDL createBrowserUrl → LocalPanelRuntime.createBrowserUrl | PanelCoreController.openBrowserPanel() |

**6.2.2 `com.daidai.panel/local_host/events`（EventChannel）**

| 事件 | 载荷 | Kotlin 处理 | 迁移后替代 |
|---|---|---|---|
| LocalPanelStatus | LocalPanelStatus JSON map | MainActivity L163–174，onListen 后立即 emit + restart/stop/setPersistentSchedulingEnabled 后 emitAfter | PanelCoreController.statusFlow: StateFlow<LocalPanelStatus> |

**6.2.3 `com.daidai.panel/app_install`（MethodChannel，Dart 侧 `AppUpdateService._platform`）**

| Method | Dart 参数 | 返回 | Kotlin 处理 | 迁移后替代 |
|---|---|---|---|---|
| installApk | `{path, sourceHost?}` | null | MainActivity L40–52（installApk 安装 + 错误码） | AppUpdateManager.installApk(path) |
| getInstalledApkInfo | — | map{packageName,versionName,versionCode,size,md5,sha256} | L53–77 | AppUpdateManager.installedApkInfo() |
| getRuntimeAbi | — | String | L78 `AndroidLinuxRuntime.currentAbi()` | AndroidLinuxRuntime.currentAbi()（直调） |
| applyPatch | `{patchPath, outputName}` | map{path} | L79–107（updates 目录安全校验 + bsdiff） | AppUpdateManager.applyPatch(patchPath, outputName) |
| openExternalUrl | `{url}` | Boolean | ⚠️ **未实现**（落入 notImplemented） | 实现（Intent.ACTION_VIEW）或删除调用 |

**6.2.4 `com.daidai.app/root`（MethodChannel）**

| Method | 状态 | 说明 |
|---|---|---|
| （任意） | 全部 NOT_IMPLEMENTED | MainActivity L32–36 + RootMethodChannelPolicy.kt；Dart 无调用 → 死通道，删除 |

### 6.3 关键调研文件索引

| 主题 | 文件 |
|---|---|
| 路由 | `app/lib/core/router/app_router.dart`（419 行，39 路由） |
| 主题 | `app/lib/core/theme/app_theme.dart`、`app_miuix_theme.dart`、`theme_provider.dart` |
| 控制面 MethodChannel | `app/lib/core/local_panel/method_channel_local_panel_host.dart`、`local_panel_host.dart`、`local_panel_models.dart`、`boot_endpoint_resolver.dart`、`local_panel_session_resolver.dart`、`managed_local_connection_monitor.dart` |
| 安装 MethodChannel | `app/lib/core/services/app_update_service.dart` |
| Kotlin 通道处理 | `app/android/app/src/main/kotlin/com/daidai/daidai_app/MainActivity.kt`、`RootMethodChannelPolicy.kt`、`LocalPanelServiceClient.kt`、`app/src/main/aidl/com/daidai/daidai_app/ILocalPanelService.aidl` |
| 本地 core | `LocalPanelRuntime.kt`、`LocalPanelHttpServer.kt`、`LocalPanelStore.kt`（25 表 / SCHEMA_VERSION=19）、`LocalPanelHostService.kt`、`LocalWebAssets.kt`、`LocalBrowserAccess.kt` |
| 网络层 | `app/lib/core/network/dio_client.dart`、`api_endpoints.dart`、`sse_client.dart`、`panel_capability_registry.dart`、`panel_profile.dart` |
| 存储 | `app/lib/core/storage/secure_storage.dart` |
| 契约 | `contracts/backend-api-mobile.json`（449 路由：389 supported / 60 android_equivalent；JWT 420 / 公开 29） |
| 构建 | `app/pubspec.yaml`、`app/android/app/build.gradle.kts`（compileSdk 36 / minSdk 24 / targetSdk 35；NanoHTTPD 2.3.1、Work 2.11.1、commons-compress） |