# 去 Flutter 规划（阶段 5）

> 目标：在 Compose 原生 UI 已覆盖全部 17 个 feature 模块之后，彻底移除 Flutter
> 引擎与 Dart 代码，让 Android 端仅以原生 Gradle/Compose 形态交付。
>
> **本规划只做分析与方案，不包含任何实际删除/改动命令。** 执行时按「分步执行计划」
> 逐段落地并各自验证（每步可单独回滚）。所有结论基于当前仓库实际代码核查。

---

## 0. 前提确认（去 Flutter 的准入门槛）

去 Flutter 只有一个硬性门槛：**Compose 原生入口必须覆盖全部用户可达路径**。

当前核查结果：

- `app/android/.../ui/screens/` 下已有 **13 个 feature 包、共 47 个 Kotlin 文件**：
  applock / deps / envs / logs / notifications / openapi / scripts / security /
  settings / subscriptions / system / tasks / users。
  对照 Dart `app/lib/features/` 的 17 个模块（app_lock/dashboard/deps/envs/login/logs/
  notifications/openapi/profile/scripts/security/server_config/settings/subscriptions/
  system/tasks/users），dashboard/login/settings/system/server_config 等已并入
  Compose 根屏（`ui/screens/*.kt`），profile/openapi 写 / notifications 本机侧仍在补齐。
- 入口 `NativeMainActivity`（`ComponentActivity`）仅通过 adb 显式启动（阶段 0 共存态）。

**结论：启动切换前，须先验收「profile 管理」「openapi token 写回」「本机通知」
三条入口在 Compose 下均有可用实现**（任务上下文标注为「补齐中」）。这三项未闭环前，
不要执行本文第 1 步（切 launcher），以免出现用户可触发但无实现的路径。

---

## 1. 切换清单（MainActivity / Manifest / Application）

### 1.1 launcher 切换（AndroidManifest.xml）

`app/android/app/src/main/AndroidManifest.xml`（当前行号）：

| 行 | 现状 | 改动 |
|----|------|------|
| L19–40 | `.MainActivity`（FlutterActivity）带 `MAIN`/`LAUNCHER` intent-filter，是 launcher | **删除该 Activity 声明**，或降级为不导出；把 launcher intent-filter 移到 `.NativeMainActivity` |
| L41–49 | `.NativeMainActivity` 普通 Activity，无 intent-filter，`android:exported="true"` | **原子地**：(a) 去掉 MainActivity，把 `MAIN`+`LAUNCHER` filter、`taskAffinity=""`、`launchMode="singleTop"` 迁到 NativeMainActivity；(b) 移除 flutter 专用 `configChanges` 值外的冗余项 |
| L70–72 | `<meta-data name="flutterEmbedding" value="2"/>` | **删除**（仅供 flutter tool 生成 PluginRegistrant 用） |
| L88–98 | `<queries>` PROCESS_TEXT（`io.flutter.plugin.text.ProcessTextPlugin` 需要） | **删除**（Kotlin 侧无 ProcessText 插件依赖） |
| L26–35 | MainActivity 上 flutter `NormalTheme` meta-data | 随 MainActivity 一起移除；NativeMainActivity 复用 `@style/LaunchTheme` 即可 |

> Launcher 切换应保留 MainActivity 仍可由 `adb` 手动启动（过渡期回滚/对照），
> 只需把 `exported` 保留并在 `AndroidManifest` 里注释说明，不急着物理删文件。

### 1.2 FlutterEngine / MethodChannel 处置

`app/android/app/src/main/kotlin/com/daidai/daidai_app/MainActivity.kt`（14,910 字节，
`io.flutter.*` 依赖：

- L18 `class MainActivity : FlutterActivity()` + L29 `configureFlutterEngine(...)`。
- L19–22 四个 channel 常量：`com.daidai.app/root`、`com.daidai.panel/app_install`、
  `com.daidai.panel/local_host`、`com.daidai.panel/local_host/events`。
- L32–36 ROOT channel（仅 `notImplemented`，经 `RootMethodChannelPolicy`）。
- L38+ `INSTALL_CHANNEL`（`installApk` / `getInstalledApkInfo`，含 updateExecutor）。
- LOCAL_HOST channel + EventChannel（localHostEventSink），调 `LocalPanelServiceClient`。

**处置策略（按依赖重构，非简单删除）：**

1. `installApk` / `getInstalledApkInfo` / 文件安装逻辑：抽成一个 **纯 Kotlin 的单例或
   Context 工具类**（例如 `ApkInstaller`），与 Flutter 无关，供 Compose 的
   「应用更新/设置」页面直接调用（替换 Dart 端 `app_update_service.dart` 的
   `MethodChannel('com.daidai.panel/app_install')`，见第 4 节）。
2. LOCAL_HOST channel 的 `ensureStarted/status/restart/stop/openBrowser` 语义：
   Compose 侧已有 **`PanelCoreController`（`data/localcore/PanelCoreController.kt`）**，
   它**不经过 Flutter MethodChannel**，直接驱动 `LocalPanelRuntime`（同进程）
   或 `LocalPanelServiceClient`（AIDL/跨进程前台服务）。
   - 同进程路径：用 `PanelCoreController`（已 exist）。
   - 跨进程路径：用 AIDL `ILocalPanelService`（`aus/main/aidl/.../ILocalPanelService.aidl`）+
     `LocalPanelHostService`（前台服务，`:panel` 进程）。
3. ROOT channel（本就走 `notImplemented`）与 `RootMethodChannelPolicy`：**整段可删**，
   无实际功能。
4. `MainActivity.kt` 在去 Flutter 后**整体删除**；其私有工具（install、哈希、FileProvider 授权）
   归并到 ApkInstaller 后不再有遗留引用。

### 1.3 Application

`.../kotlin/com/daidai/daidai_app/DaidaiApplication.kt`：

- L6 `: FlutterApplication()`、import `io.flutter.app.FlutterApplication`。
- 实际功能仅：L8 `super.onCreate()` + L12 `AppServices.initialize(this)`
  + L15–19 `Configuration.Provider.workManagerConfiguration`。

**改动：** 改继承 `android.app.Application`（替换 `FlutterApplication`），保留
`AppServices.initialize` 与 `workManagerConfiguration`。`onCreate` 时
`super.onCreate()` 不再需要 Flutter 引擎初始化，反而**降低启动耗时**。
删除对 `io.flutter.*` 的全部 import。

> `DaidaiApplication` 里对 `AppServices.initialize` 的调用已先于 UI 装配，去掉
> FlutterApplication 不影响 Compose 访问 `AppServices.configRepository/loginRepository`。

---

## 2. 删除清单（文件 / 目录 / 依赖 / 构建任务）与顺序

### 2.1 Dart 与 Flutter 工程文件

| 路径（相对 repo） | 类型 | 量级/说明 |
|------------------|------|-----------|
| `app/lib/` | Dart 源码 | **109 个 .dart**（全 17 模块 + core/shared） |
| `app/pubspec.yaml` / `app/pubspec.lock` | 依赖清单 | pubspec 含 20+ Dart 依赖（flutter_miuix/riverpod/dio/go_router/webview_flutter/local_notifications 等） |
| `app/test/` | Dart 测试 | Flutter 测试夹具 |
| `app/ios/` | iOS 工程 | 保留（若本规划仅限 Android；跨端部署才移除） |
| `app/.dart_tool/` | 工具缓存 | 可随 `git clean` 删除 |
| `app/.flutter-plugins-dependencies` | Flutter 插件清单 | 删除 |
| `app/.metadata` | Flutter 元数据 | 删除 |
| `app/analysis_options.yaml` / `CONTRIBUTING.md` 内 flutter 段 | 配置 | 按需清理 |
| `app/assets/` | Flutter assets（`icon.png` 等） | **保留 icon.png**（被 res/launcher 引用）改由 res 管理；其余随 Dart 删 |
| `app/android/.../java/io/flutter/plugins/GeneratedPluginRegistrant.java` | 生成物 | 删除（flutter 工具生成，去插件的杀之） |

### 2.2 Gradle 侧 Flutter 依赖

| 文件 | 位置 | 删除/替换 |
|------|------|-----------|
| `app/android/settings.gradle.kts` | L1–18 `pluginManagement { flutterSdkPath includeBuild("$flutterSdkPath/packages/flutter_tools/gradle") }` | 删除整段；不再读 `local.properties` 的 `flutter.sdk` |
| `app/android/settings.gradle.kts` | L21 `plugins { id("dev.flutter.flutter-plugin-loader") version "1.0.0" }` | 删除 |
| `app/android/app/build.gradle.kts` | L14 `id("dev.flutter.flutter-gradle-plugin")` | 删除 |
| `app/android/app/build.gradle.kts` | L509–511 `flutter { source = "../.." }` | 删除 |
| `app/android/app/build.gradle.kts` | L66 `ndkVersion = flutter.ndkVersion` | 替换为**显式常量**（如 `ndkVersion = "27.0.12077973"`）或删除（Compose 用 AGP 默认） |
| `app/android/app/build.gradle.kts` | L86–87 `versionCode = flutter.versionCode` / `versionName = flutter.versionName` | 改为**显式/来自 BuildConfig 之前先静态赋值**：从 `VERSION.json`（`scripts/version.py` 的 `ANDROID_VERSION_CODE`/`versionName`）解析，或硬编码 `versionCode`/`versionName`，去掉 `flutter.*` |
| `app/android/app/build.gradle.kts` | L53 `FLUTTER_SPLIT_PER_ABI`（env）与 L88–92 ndk abiFilters | 与 flutter split 逻辑解耦；ABI 仍由 `requestedAbis`（`ANDROID_RUNTIME_ABIS`）驱动，仅去掉 flutter split 分支 |
| 注释 L13 「The Flutter Gradle Plugin must be applied…」 | — | 一并删除 |

### 2.3 构建任务：native assets 打包（**保留，与 Flutter 无关**）

以下 task **不是 Flutter 的，去 Flutter 后必须保留**，只把依赖从 flutter 插件里剥离：

- `installLocalPanelWebDependencies` / `buildLocalPanelWeb` / `packageLocalPanelWeb`
  （local-web assets，`app/android/app/build.gradle.kts` L185–218）。
- `packageSelectedRuntimeAssets`（L220）与 runtime/rootfs verification tasks
  （`android-runtime/$abi`，jniLibs `src/main/assets/android-runtime/...`）。
- `verifyLocalPanelWeb`、rootfs 校验任务（L234+）。
- `externalNativeBuild { cmake { path = src/main/jni/CMakeLists.txt } }`（L153–157）与
  `jniLibs { useLegacyPackaging }`（L124–131）：**保留**。
- `aidl = true` 构建特性（L142）：**保留**（AIDL 前台服务路径）。

这些任务目前由 AGP/Compose 目录直接驱动，`flutter` 插件只提供 `versionCode` 等标量，
去 flutter 时**改成显式值/常量即可**，构建资产流程本身不动。

### 2.4 scripts / workflows 间接引用（同批处理）

| 文件 | 引用 | 处置 |
|------|------|------|
| `scripts/android-abi-matrix.py` | L12 `"flutter_target"` 字段 + L40/L43 uniqueness 断言 | 移除该字段；`--target-platform` 不再需要 |
| `scripts/generate-release-evidence.go` | L49 `flutter_lock` / L151 `--flutter-lock app/pubspec.lock` / L177 传入 / L226 输出 / L397 `parsePubspecLock` | 移除 flutter SBOM 组件收集（或标注「legacy」跳过） |
| `scripts/version.py` | L229–230 `flutterBuildName` / `flutterBuildNumber` | 移除或保留仅作“版本展示别名”不再喂给构建 |
| `app/android/build.gradle.kts` | L17–22 无flutter 引用 | 无需改 |
| `.github/workflows/android-release.yml` | `FLUTTER_VERSION`（L39）、`Set up Flutter`（L91–96）、`FLUTTER_BUILD_NAME/NUMBER`（L143–144）、`flutter pub get/test/analyze`（L272–282）、`flutter build apk … --split-per-abi --target-platform`（L317–318）、`build/app/outputs/flutter-apk/` 路径（L320） | 见第 3 节 |
| `.github/workflows/ci.yml` | `FLUTTER_VERSION`（L21）、`flutter-quality` job（L192–219）、`changes.outputs.flutter`（L35/L61）、`Set up Flutter + pub get/analyze/test`（L261+） | 见第 3 节 |

### 2.5 删除顺序（依赖拓扑）

1. **先删 UI/静态层**：`app/lib/**`、`pubspec.yaml/lock`、`app/test/`、`.dart_tool/`、
   `.flutter-plugins-dependencies`、`.metadata`、GeneratedPluginRegistrant.java（此时无 Dart 依赖）。
2. **再拆功能**：从 `MainActivity.kt` 抽出 `ApkInstaller`（纯 Kotlin）并让 Compose
   采用 PanelCoreController/AIDL，确认无 `MainActivity.kt` 残留引用后删该文件。
3. **再拆 Gradle 标量**：`flutter.versionCode/versionName/ndkVersion` → 显式值，
   settings/build.gradle.kts 去掉 flutter 插件（先 `.kt` 无 `flutter.*` 引用后）。
4. **最后拆 CI/SBOM**：wf 改纯 gradle、删除 scripts 内 flutter_target/pubspec.lock 解析。

> 每一步后都跑 `gradle -p app/android :app:assembleDebug` 与
> `:app:assembleReleaseAndroidTest`，确保无 `io.flutter.*` / `flutter.*` 编译引用残留。

---

## 3. 构建 / 发布流程改动点（具体到 workflow 与 step）

### 3.1 `.github/workflows/android-release.yml`

| step 名 | 现在 | 改后 |
|---------|------|------|
| env `FLUTTER_VERSION`（L39） | 3.44.9 | 删除 |
| `Set up Flutter`（L91–96, subosito/flutter-action） | 装 Flutter | **删除该 step** |
| `Resolve and validate release version`（L98） | 输出 `FLUTTER_BUILD_NAME/BUILD_NUMBER`（L143–144） | 只保留 `ANDROID_VERSION_CODE` + versionName；`FLUTTER_BUILD_*` 改名为如 `ANDROID_BUILD_NAME/NUMBER` |
| `Resolve Flutter packages`（L272） / `Run Flutter tests`（L276） / `Analyze Flutter source`（L280） | Dart pub/test/analyze | **删除三步**；Kotlin 测试 `gradle -p android :app:testReleaseUnitTest`（L286）保留 |
| `Build installable release APK`（L311–322） | 对每个 ABI：`ANDROID_RUNTIME_ABIS=ABI FLUTTER_SPLIT_PER_ABI=true flutter build apk --release --target-platform ${TARGET} --split-per-abi --build-name --build-number` | **改为纯 Gradle**：`ANDROID_RUNTIME_ABIS="${ABI}" gradle -p android :app:assembleRelease --no-daemon`；APK 产物路径改到 AGP 标准 `app/android/app/build/outputs/apk/release/app-release.apk`（不再 `build/app/outputs/flutter-apk/...`） |
| `assembleReleaseAndroidTest`（L319） | `gradle -p android :app:assembleReleaseAndroidTest` | **保留不变** |
| 后续 metadata 校验（L324+，`unzip assets/manifest.json` 等） | 只校验打包进来的 `assets/...`（runtime/native 资产） | **不变**（不依赖 Flutter） |
| `verify-android-apk-abi.py`（L346/L348） | 校验 APK 内 ELF manifest/ABI | 改 APK 源路径后仍可复用；新增「断言 APK 不含 `lib/arm64-v8a/libflutter.so`」的可选校验 |

> 版本号注入：去 Flutter 后 versionCode/versionName 由 Gradle 显式值承载。
> 建议新增一步 `python3 scripts/version.py show --format shell` 写 `ANDROID_VERSION_CODE`
> 后经 `-PversionCode=... -PversionName=...` 注入 `defaultConfig`（替换原
> `--build-name/--build-number`），保持单一事实来源 `VERSION.json`。

### 3.2 `.github/workflows/ci.yml`

- env `FLUTTER_VERSION`（L21）：删除。
- `changes` filter 的 `flutter`（L35/L61）：删除或改名为 `dart`（不再触发）。
- `flutter-quality` job（L192–219）与 job 内 `Set up Flutter / pub get / analyze / test`
  （L205–219）：**整个 job 删除**；Dart 测试质量由 `app/test` 移除后空缺，
  由 Kotlin 单测（`kotlin-test` job）承接。
- `kotlin-test`/其他 job 内的 `Set up Flutter + pub get`（L261+）：**删除 flutter 部分**，
  保留 node/go 设管（Web assets & Go server 仍需要）。
- `needs: flutter-quality` 引用（L388 等）：移除该依赖。

### 3.3 构建命令对开发者本地的影响

- 之前：`flutter build apk`（须装 Flutter）。之后：`gradle -p android :app:assembleRelease`。
- 单一 ABI 约束（`requestedAbis.size == 1`，`build.gradle.kts` L52）由
  `ANDROID_RUNTIME_ABIS` env 继续控制，去掉 `FLUTTER_SPLIT_PER_ABI`。
- native assets 打包由 `packageSelectedRuntimeAssets`/`packageLocalPanelWeb` 等 Gradle task
  串联到 `sourceSets.main.assets`（L159–169），**与 Flutter 无关，流程不变**。

---

## 4. 功能对接口（MethodChannel / AIDL / WebView / Notification 替代路径）

### 4.1 MethodChannel `com.daidai.panel/local_host` + `/events`

- **关系**：Dart `method_channel_local_panel_host.dart` 走 MethodChannel(apply native
  MainActivity)。Compose 侧**已绕过**，走：
  - 同进程：`PanelCoreController` → `LocalPanelRuntime`（`data/localcore`）。
  - 跨进程/前台服务：AIDL `ILocalPanelService`（`LocalPanelHostService`，`:panel`
    进程的**前台服务**，`FOREGROUND_SERVICE_SPECIAL_USE`）。
- **验收标准**：Compose 的 Login/ServerConfig/系统设置页在`ensureStarted/status/restart/
  stop/openBrowser` 上与 Dart 版结果一致；本地 Web UI 在启动后可达。

### 4.2 MethodChannel `com.daidai.panel/app_install`

- Dart `app_update_service.dart` 用它做 `installApk` / `getInstalledApkInfo`。
- **替代**：deflutter 时抽 `ApkInstaller`（纯 Kotlin，复用 `MainActivity.kt` 内的
  `installApk`、`MessageDigest.sha256`、FileProvider 授权逻辑），由「应用更新」
  页面直接调用。删除 dart 侧对应 MethodChannel。

### 4.3 MethodChannel `com.daidai.app/root`

- 仅 `notImplemented`，无实际调用方。**直接砍掉** `RootMethodChannelPolicy` + 该 channel。

### 4.4 webview_flutter（极验 / GeeTest 等网页验证）

- Dart 用 `webview_flutter: ^4.10.0`（pubspec L44）渲染网页（极验、内嵌 H5）。
- **替代**：原生 `android.webkit.WebView`（Compose 内用 `AndroidView` 承载），宿主在
  `NativeMainActivity` 生命周期里，`onBackPressed` 转发 WebView 后退栈、处理 JS bridge。
- 验收：登录/注册页内嵌验证码流程与原 webview_flutter 行为一致。

### 4.5 flutter_local_notifications → 原生 Notification

- Dart 用 `flutter_local_notifications: ^18.0.1`（pubspec L47）。
- **替代**：原生 `NotificationManager` + `NotificationChannel`。代码库**已有多处原生
  直达**：`LocalPanelHostService.kt` L306/L314/L329/L334（IMPORTANCE_LOW channel +
  POST_NOTIFICATIONS runtime 处理）、`LocalPanelStore.kt` L1558。去 Flutter 时把
  Dart 侧的「本机通知」入口（`features/notifications`）接到这些原生 NotificationManager
  调用；Compose `ui/screens/notifications/` 页面已存在（4 个 kt）承接。
- 需补：POST_NOTIFICATIONS 运行时授权请求（Android 13+）在 Compose 侧复用一个
  permission 协程，替换 flutter_local_notifications 封装的授权。

### 4.6 其它 Dart 能力映射（一次性清单）

| Dart 依赖/能力 | 原生替代 |
|----------------|----------|
| `flutter_secure_storage` / `shared_preferences` | `EncryptedSharedPreferences` / `SharedPreferences` + keystore（`DependencyStorage.kt` 已含） |
| `local_auth` `crypto` | 原生 `BiometricPrompt` + `java.security.MessageDigest` |
| `package_info_plus` / `device_info_plus` | `BuildConfig`/`Build` + `PackageManager` |
| `path_provider` `file_picker` | `FileProvider` + `ActivityResultContracts.CreateDocument/GetContent` |
| `http`/`dio`/`intl`/`logger` | Compose 已有 `PanelHttpClient`（data/remote）+ 原生 Log |
| `go_router` | `AppNavHost`（navigation-compose，`ui/navigation/AppNavHost.kt`） |
| `flutter_miuix` / `liquid_glass_easy` 主题 | `miuix-android:0.8.8`（build.gradle.kts L506）+ `ui/theme/` |
| `fl_chart` 图表 | 原生 `Compose` Canvas / 三方图表（按 feature dashboard 选择） |

> 上表能力多数在 Compose 侧**已有原生实现**（ref: `ui/`、`data/`、`di/`），
> deflutter 主要是“让这些原生实现成为唯一路径、移除 Dart 副本”。

---

## 5. 分步执行计划（2–4 步，每步可验证 + 可回滚）

### Step A：Compose 入口覆盖验收 + 抽离 ApkInstaller
- 动作：补齐 profile/openapi 写回/本机通知的 Compose 实现并进 AppNavHost；
  把 `MainActivity.kt` 的 install/哈希逻辑抽成 `ApkInstaller`（纯 Kotlin）；
  `DaidaiApplication` 改继承 `android.app.Application`（去掉 FlutterApplication）。
- **验证**：`gradle -p android :app:assembleRelease` 通过；`adb shell am start
  -n com.daidai.daidai_app/.NativeMainActivity` 全流程走通（登录→dashboard→各模块）；
  无 `io.flutter.*` import（除 MainActivity 外，application 已不引 flutter）。
- 回滚：仅涉及新增/重构类，彼此独立，可单文件 revert。

### Step B：切 launcher + 删 MainActivity/Manifest
- 动作：把 `MAIN/LAUNCHER` filter 移到 NativeMainActivity，删 MainActivity 的
  intent-filter、`NormalTheme`、`flutterEmbedding`、PROCESS_TEXT queries；删除
  `MainActivity.kt`、`GeneratedPluginRegistrant.java`；删除 `RootMethodChannelPolicy`。
- **验证**：`gradle :app:assembleRelease` + `:app:assembleReleaseAndroidTest` 通过；
  `adb` 安装后从桌面图标冷启动直达 Compose；全 feature 冒烟通过；Manifest 无
  `io.flutter` 引用。
- 回滚：Manifest 是单文件/单 commit，切回 launcher 即可恢复 Flutter 入口（Main*Activity
  提前备份）。

### Step C：去 Gradle Flutter 插件 + 删除 Dart 源码
- 动作：settings.gradle.kts 删 flutter-plugin-loader 与 pluginManagement 内 flutter 路径；
  build.gradle.kts 删 `dev.flutter.flutter-gradle-plugin`/`flutter {}`，`versionCode/Name/
  ndkVersion` 改显式（或经 `-P` 注入）；删 `app/lib/`、pubspec.*、test/、ios/、.dart_tool/
  .metadata/.flutter-plugins。
- **验证**：`gradle -p android :app:assembleRelease`（暴露之前隐藏的 `flutter.*` 引用，
  逐项修到编译过）；产物 APK 可装、ABI/manifest 校验 `verify-android-apk-abi.py` 通过；
  `grep -R "io.flutter\|flutter:" app/android --include=*.kts --include=*.kt` 无命中。
- 回滚：Dart 源码删除前 commit 存档；若 Gralde 改动引入问题，`git revert` 该步，Flutter
  构建（Step B 的 MainActivity 仍在 git 历史）可复原。

### Step D：CI/SBOM 改造 + 发布验证
- 动作：android-release.yml 删 flutter 安装/三大 Dart step、改纯 gradle 构建与产物路径、
  改版本注入；ci.yml 删 flutter-quality job 与 flutter 引用；android-abi-matrix.py 去
  `flutter_target`；generate-release-evidence.go 去 pubspec.lock 解析。
- **验证**：`workflow_dispatch` 跑一次 `android-release.yml`（snapshot 通道）产出可安装
  release APK 且 metadata/extract 步骤全绿；ci.yml `push main` 全 job 绿（不再有
  flutter-quality 失败）；SBOM 不再报 pubspec.lock 缺失。
- 回滚：工作流文件独立，可整体 revert 回 flutter 构建（重装 Flutter 后即恢复发布）。

> 依赖/路径见第 3.1–3.3。三项功能对接口（local_host、app_install、webview/notification）
> 在 Step A 就必须闭环，后续步骤不再回到功能层。

---

## 6. 风险与回归（重点盯防）

1. **MethodChannel 语义偏移**：Dart `method_channel_local_panel_host` 走 cross-process
   AIDL（前台服务）；Compose `PanelCoreController` 走**同进程** `LocalPanelRuntime`。
   两者的 `phase`/`base_url`/`local_token` 取值与重试语义可能不同 → 回归时对比
   「Dart 版 vs Compose 版」在 ensure/restart 后的状态机结果。
2. **前台服务权限**：`FOREGROUND_SERVICE`/`FOREGROUND_SERVICE_SPECIAL_USE`（Manifest
   L7–8）与 Android 14+ 前台服务类型校验必须保持；切 launcher 时勿动 service/receiver
   声明（`LocalPanelHostService`、`LocalPanelRecoveryReceiver`）。
3. **WiFi/Timeout 与 Boot 恢复**：`LocalPanelRecoveryReceiver` 的 BOOT_COMPLETED 恢复回调是
   原生 receiver，去 Flutter 无影响，但须确认 Compose 启动后仍能触发 Recovery 流程。
4. **webview_flutter → 原生 WebView**：cookie/JS bridge/后退栈行为差异、极验验证码在原生
   WebView 的兼容性（`android:usesCleartextTraffic`/`networkSecurityConfig` 已配）。
5. **Notification 授权**：Android 13+ `POST_NOTIFICATIONS` 动态授权需在 Compose 侧显式
   请求（替代 flutter_local_notifications 封装）。
6. **版本号单一来源**：从 `flutter --build-name/--build-number` 切到 Gradle `-P` 注入后，
   保证 `VERSION.json`/`android-abi-matrix.py`/release-evidence 三者一致，否则 tag 校验
   （`GITHUB_REF_NAME == v${VERSION}`）会失败。
7. **产物路径漂移**：`build/app/outputs/flutter-apk/` → `app/android/app/build/outputs/apk/
   release/`，后续所有 cp/verify 步骤路径要同步改，否则发布步骤取到旧文件。
8. **regression：adb 启动 NativeMainActivity 已是唯一入口**后，Flutter 期用于对照的
   旧 APK 无法共存（同 packageName），回归需新 APK + 旧 APK 分开设备/串口比对。

---

## 7. 产物对比：体积与启动收益预估

### 7.1 APK 体积

| 组成 | 估算 | 说明 |
|------|------|------|
| `libflutter.so`（arm64-v8a） | 已打包约 **13–15 MB** | Flutter 引擎本体，去 Flutter 后**整份移除** |
| Dart 产物 `libapp.so` / kernel_blob | ~2–4 MB | 随 Dart 删 |
| flutter 各插件 .so（webview/notifications 等） | ~1–3 MB | 原生替代后不再需要 |
| **去除合计** | **约 16–22 MB / ABI** | arm64 单 ABI 口径 |
| 保留：AGP/Compose/androidx、jniLibs 原生 runtime（libpython_exec 等按 Manifest **保留**）、native assets(rootfs)、rootfs assets | — | `packaging { excludes += "**/libpython_exec.so" }` 等仍生效 |

> 去 Flutter 后 APK 体积预估 **降低 15 MB 以上（arm64）**；若原先 `--split-per-abi`
> 逐 ABI 出包，收益按各 ABI 同样成立。本地 web / rootfs / AIDL client 等原生资源
> 不受影响，仍是体积大头（属业务设计而非平台开关）。

### 7.2 启动速度

- 移除 `ComponentActivity` 之上的一层 `FlutterActivity` 初始化、Dart isolate 预热、
  `dartExecutor` 注册与 `FlutterApplication` 引擎创建。
- `DaidaiApplication.onCreate` 不再 `super.onCreate()` 创建嵌入引擎，**Application 阶段
  return 更早**。
- 首屏 Compose `AppNavHost` 直接走 `onCreate → setContent`，预计**冷启动（Java/kotlin
  Activity 创建到首帧）提速数百毫秒量级**；热路径（后台恢复）无引擎重载。

> 明确量化口径需在第 3 步前后各跑 `adb reboot` + `adb shell am start -W` 冷启动打点
> （`TotalTime`/`WaitTime`），记录到第 3.1 节留档验收。

---

## 8. 结论摘要

- **切换点**：launcher 从 `MainActivity`（FlutterActivity）→ `NativeMainActivity`
  （ComponentActivity）；Application 去 `FlutterApplication`；删 `flutterEmbedding`/
  PROCESS_TEXT/NormalTheme/ROOT channel。
- **删除量**：109 个 Dart 文件 + pubspec.* + app/test + ios + .dart_tool 等；Gradle 侧
  flutter-plugin-loader/flutter-gradle-plugin/flutter{} 块；3 个 workflow、4 个脚本引用。
- **构建改动**：`flutter build apk → gradle :app:assembleRelease`，native assets 打包任务
  与签名/混淆/AIDL 全部保留，只替换 `flutter.versionCode/Name/ndkVersion` 为显式值。
- **功能对接口**：local_host→PanelCoreController/AIDL；app_install→ApkInstaller；root
  →砍；webview→原生 WebView；notification→NotificationManager（代码已含）。
- **收益**：APK 去 Flutter 后约 **-15 MB+（arm64）**，冷启动提速毫秒级–数百毫秒；
  独立 ABI/runtime 资产不受影响，可逐步（4 步，各自验证/回滚）平稳落地。
