# 平台差异说明：Flutter → Android/iOS 原生移植

## 总览

| 维度 | Flutter (原) | Android (新) | iOS (新) |
|------|-------------|-------------|---------|
| 语言 | Dart | Kotlin | Swift |
| UI 框架 | Widget | Jetpack Compose | SwiftUI |
| 状态管理 | Riverpod (StateNotifier) | ViewModel + StateFlow | ObservableObject + @Published |
| 网络请求 | Dio | Retrofit + OkHttp | URLSession (async/await) |
| 本地存储 | FlutterSecureStorage + SharedPreferences | EncryptedSharedPreferences + DataStore | Keychain + UserDefaults |
| 路由 | go_router | Navigation Compose | NavigationStack |
| 毛玻璃效果 | liquid_glass_easy (LiquidGlassLens, LiquidGlassScaffold, LiquidGlassBottomNavBar) | RenderEffect.createBlurEffect (Android 12+) / 半透明降级 | .ultraThinMaterial |
| 图表 | fl_chart | Canvas 自绘 | Canvas 自绘 |
| 图片加载 | Image.network | Coil (AsyncImage) | AsyncImage (原生) |
| 依赖注入 | Riverpod Provider | Hilt (@HiltViewModel) | 手动注入 (@EnvironmentObject) |
| SSE 客户端 | http package | OkHttp EventSource | URLSession dataTask |
| 生物识别 | local_auth | BiometricPrompt | LocalAuthentication (LAContext) |
| 文件选择 | file_picker | ActivityResultContracts.GetContent | PhotosPicker / fileImporter |
| 剪切板 | Clipboard | ClipboardManager | UIPasteboard |
| 推送通知 | flutter_local_notifications | NotificationManager + NotificationChannel | UNUserNotificationCenter |
| 应用更新 | GitHub Release + APK 安装 | PackageInstaller (Android 12+) / intent | TestFlight / App Store |

## 关键平台差异

### 1. 毛玻璃效果实现

**Android (API 31+ / Android 12+):**
```kotlin
// 使用 RenderEffect 实现模糊
Modifier.graphicsLayer {
    renderEffect = RenderEffect.createBlurEffect(
        blurRadius, blurRadius, Shader.TileMode.CLAMP
    )
}
```
- 降级方案: API < 31 使用半透明 `Color(0xCCFFFFFF)` 背景
- 需要 `@RequiresApi(Build.VERSION_CODES.S)` 注解

**iOS:**
```swift
// 使用原生 Material
.background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16))
```
- iOS 15+ 原生支持，无需降级
- 视觉效果与 Flutter 的 `liquid_glass_easy` 实时折射效果最接近

### 2. Token 自动刷新机制

**Flutter (原):**
- Dio 拦截器 (AuthInterceptor)
- 401 时用 refresh_token 调用 /api/auth/refresh
- 排队机制: 刷新期间其他 401 请求加入队列，刷新后重发

**Android:**
- OkHttp Interceptor 实现相同逻辑
- 使用 `synchronized` 块保证线程安全
- `Authenticator` 接口可选（但 Interceptor 更灵活）

**iOS:**
- URLSession 包装层实现
- 使用 `actor` 或 `NSLock` 保证线程安全
- `async/await` 风格的刷新流程

### 3. SSE (Server-Sent Events) 流式日志

**Flutter (原):**
- `http` package 直接读取流
- 手动解析 `event:` / `data:` 行

**Android:**
- OkHttp `EventSource` (okhttp-sse 模块)
- 封装为 Kotlin `Flow<String>` 供 Compose 消费

**iOS:**
- `URLSessionDataTask` 逐行读取
- 封装为 `AsyncStream<String>` 或 Combine `Publisher`

### 4. 后台任务 / 定时同步

**Flutter (原):**
- 无后台任务（依赖后端 cron）

**Android:**
- `WorkManager` 处理后台同步
- 可设置周期性任务（如检查更新）
- 需要 `RECEIVE_BOOT_COMPLETED` 权限（可选）

**iOS:**
- `BGTaskScheduler` 注册后台任务
- 受系统调度限制（不保证精确时间）
- 需要在 Info.plist 注册 `BGTaskSchedulerPermittedIdentifiers`

### 5. 剪切板操作

**Flutter (原):**
```dart
Clipboard.setData(ClipboardData(text: value));
```

**Android:**
```kotlin
val clipboard = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
clipboard.setPrimaryClip(ClipData.newPlainText("env_value", value))
```

**iOS:**
```swift
UIPasteboard.general.string = value
```

### 6. 生物识别认证

**Flutter (原):**
- `local_auth` 插件
- `LocalAuthentication().authenticate()`

**Android:**
- `BiometricPrompt` API
- 需要 `FragmentActivity`
- 回调: `onAuthenticationSucceeded`, `onAuthenticationError`, `onAuthenticationFailed`

**iOS:**
- `LAContext` + `evaluatePolicy(.deviceOwnerAuthenticationWithBiometrics)`
- 需要 `NSFaceIDUsageDescription` in Info.plist

### 7. 文件/图片选择

**Flutter (原):**
- `file_picker` 插件

**Android:**
- `ActivityResultContracts.GetContent()` 或 `OpenDocument()`
- 需要 `READ_EXTERNAL_STORAGE` (API < 33) 或 `READ_MEDIA_IMAGES` (API 33+)

**iOS:**
- `PhotosPicker` (SwiftUI, iOS 16+)
- `fileImporter` for document picking
- 需要 `NSPhotoLibraryUsageDescription`

### 8. 推送通知

**Flutter (原):**
- `flutter_local_notifications`

**Android:**
- `NotificationManager` + `NotificationChannel` (API 26+)
- 需要 `POST_NOTIFICATIONS` 权限 (API 33+)
- 前台服务通知（如需持续运行）

**iOS:**
- `UNUserNotificationCenter`
- 需要用户授权 (`requestAuthorization`)
- 注册 remote notification（如需 APNs）

### 9. 应用内更新

**Flutter (原):**
- 检查 GitHub Release
- 下载 APK
- 调用系统安装器

**Android:**
- `PackageInstaller` API (Android 12+)
- 或 `Intent(ACTION_VIEW)` + `FileProvider`
- 需要 `REQUEST_INSTALL_PACKAGES` 权限

**iOS:**
- 无法直接安装更新（受 App Store 限制）
- 可检查版本并跳转 App Store
- 或使用 TestFlight 分发

### 10. 应用锁（图案锁）

**Flutter (原):**
- 自定义 `PatternPad` Widget
- Canvas 绘制 3x3 网格
- SHA-256 哈希存储

**Android:**
- Compose `Canvas` 绘制
- 手势检测 (`detectDragGestures`)
- `Crypto` API 哈希

**iOS:**
- SwiftUI `Canvas` 绘制
- `DragGesture` 检测
- `CryptoKit` SHA-256

## 文件统计

| 平台 | 文件数 | 代码行数 |
|------|--------|---------|
| Android (Kotlin) | 73 | ~14,166 |
| iOS (Swift) | 67 | ~10,838 |
| **合计** | **140** | **~25,004** |

## 架构对照

```
Flutter                    Android                    iOS
──────────────────────────────────────────────────────────────
lib/core/network/          core/network/              core/network/
  dio_client.dart    →       NetworkModule.kt    →     ApiService.swift
  api_endpoints.dart →       ApiEndpoints.kt    →     ApiEndpoints.swift
  sse_client.dart    →       SseClient.kt       →     SseClient.swift
  app_user_agent.dart→       UserAgentInterceptor.kt → UserAgentProvider.swift

lib/core/auth/             core/auth/                 core/auth/
  auth_provider.dart →       AuthViewModel.kt   →     AuthViewModel.swift
  auth_service.dart  →       AuthRepository.kt  →     AuthService.swift
  auth_interceptor.dart→     AuthInterceptor.kt →     AuthInterceptor.swift

lib/core/storage/          core/storage/              core/storage/
  secure_storage.dart→       SecureStorage.kt   →     KeychainStorage.swift
                           + ThemePreferences.kt →    ThemePreferences.swift

lib/core/theme/            core/theme/                core/theme/
  app_theme.dart     →       AppColors.kt       →     AppColors.swift
                           + AppTheme.kt         →    ThemeManager.swift
  theme_provider.dart→       ThemeViewModel.kt   →    ThemeManager.swift

lib/shared/models/         data/model/                data/model/
  task.dart          →       Task.kt            →     Task.swift
  env_var.dart       →       EnvVar.kt          →     EnvVar.swift
  user.dart          →       User.kt            →     User.swift
  task_log.dart      →       TaskLog.kt         →     TaskLog.swift
  notify_channel.dart→       NotifyChannel.kt   →     NotifyChannel.swift
  dependency.dart    →       Dependency.kt      →     Dependency.swift
  subscription.dart  →       Subscription.kt    →     Subscription.swift
  api_response.dart  →       ApiResponse.kt     →     ApiResponse.swift

lib/shared/widgets/        ui/components/             ui/components/
  app_card.dart      →       GlassCard.kt       →     GlassCard.swift
  main_scaffold.dart→        GlassScaffold.kt   →     GlassScaffold.swift
                           + GlassTabBar.kt     →     GlassTabBar.swift
  app_background.dart→       AppBackground.kt   →     AppBackground.swift

lib/features/dashboard/    ui/dashboard/              ui/dashboard/
  views/             →       DashboardPage.kt   →     DashboardView.swift
  providers/         →       DashboardViewModel.kt→   DashboardViewModel.swift

lib/features/tasks/        ui/tasks/                  ui/tasks/
  views/             →       TaskListPage.kt    →     TaskListView.swift
  providers/         →       TaskViewModel.kt   →     TaskViewModel.swift

lib/features/logs/         ui/logs/                   ui/logs/
  views/             →       LogListPage.kt     →     LogListView.swift
                           + LogViewModel.kt    →     LogViewModel.swift

lib/features/envs/         ui/envs/                   ui/envs/
  views/             →       EnvListPage.kt     →     EnvListView.swift
                           + EnvViewModel.kt    →     EnvViewModel.swift

lib/features/settings/     ui/settings/               ui/settings/
  more_page.dart     →       MorePage.kt        →     MoreView.swift
  theme_settings_page.dart→  ThemeSettingsPage.kt→    ThemeSettingsView.swift
```
