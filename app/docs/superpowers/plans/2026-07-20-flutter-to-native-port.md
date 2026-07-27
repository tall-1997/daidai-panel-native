# 呆呆面板 Flutter → Android/iOS 原生移植计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Flutter 呆呆面板完整移植为 Android 原生 (Kotlin + Jetpack Compose + MVVM) 和 iOS 原生 (SwiftUI)

**Architecture:** 
- Android: Kotlin + Jetpack Compose + MVVM (ViewModel + StateFlow) + Retrofit + DataStore + Material3
- iOS: SwiftUI + MVVM (ObservableObject) + URLSession + Combine + Keychain

## Global Constraints

- 禁止保留任何 Dart/Flutter 语法
- Android 网络: Retrofit/OkHttp (替代 Dio)
- iOS 网络: URLSession (替代 Dio)
- Android 状态: ViewModel + StateFlow (替代 Riverpod)
- iOS 状态: ObservableObject + @Published (替代 Riverpod)
- Android 存储: EncryptedSharedPreferences + DataStore (替代 FlutterSecureStorage + SharedPreferences)
- iOS 存储: Keychain + UserDefaults (替代 FlutterSecureStorage + SharedPreferences)
- 毛玻璃: Android=RenderEffect+BlurMaskFilter, iOS=.ultraThinMaterial
- 所有卡片统一实现液态毛玻璃半透明磨砂效果

## 项目资产摘要

- **27 个页面** | **100+ API 端点** | **15+ 数据模型**
- **核心模块:** Auth, Dashboard, Tasks, Logs, Envs, Scripts, Subscriptions, Notifications, Deps, Users, Security, System, OpenAPI, Settings

## 执行策略

由于项目规模巨大，采用以下策略：
1. 先建立核心基础设施（网络/存储/认证/主题）
2. 再逐页面移植，每个页面包含完整的 Model → Repository → ViewModel → View
3. Android 和 iOS 独立实现，交替推进
4. 每个 Task 完成后可独立编译验证

## Phase 0: 核心基础设施

### Task 1: Android 项目脚手架
- 项目结构、Gradle 配置、Hilt 依赖注入
- 文件: `build.gradle.kts`, `DaidaiApp.kt`, `MainActivity.kt`, `AndroidManifest.xml`

### Task 2: Android 数据模型层
- 所有 API 响应模型: Task, EnvVar, User, TaskLog, NotifyChannel, Dependency, Subscription, ApiResponse, PaginatedData
- 文件: `data/model/*.kt`

### Task 3: Android 网络层
- Retrofit ApiService 接口定义（全部 100+ 端点）
- OkHttp 拦截器: AuthInterceptor (Token 注入 + 401 自动刷新), UserAgentInterceptor
- SSE 客户端: OkHttp SSE 实现
- 文件: `core/network/*.kt`

### Task 4: Android 本地存储层
- EncryptedSharedPreferences: Token/用户/面板配置
- DataStore: 主题设置/UI 状态
- 文件: `core/storage/*.kt`

### Task 5: Android 认证层
- AuthRepository: 登录/登出/Token 刷新/可信登录
- AuthViewModel: 认证状态管理
- 文件: `core/auth/*.kt`

### Task 6: Android 主题系统
- AppColors: 完整色板（Emerald + Slate + 玻璃色）
- AppTheme: Material3 主题配置
- ThemeViewModel: 主题模式/玻璃模式/背景图片
- 文件: `core/theme/*.kt`

### Task 7: iOS 项目脚手架
- Xcode 项目结构、Swift Package 依赖
- 文件: `ios-native/DaidaiPanel.xcodeproj/`, `DaidaiPanelApp.swift`

### Task 8: iOS 数据模型层
- 所有 Codable 模型
- 文件: `Models/*.swift`

### Task 9: iOS 网络层
- URLSession + async/await API 客户端
- Token 自动刷新拦截器
- SSE 客户端
- 文件: `Network/*.swift`

### Task 10: iOS 本地存储层
- Keychain wrapper: Token/用户
- UserDefaults: 主题设置
- 文件: `Storage/*.swift`

### Task 11: iOS 认证层
- AuthService + AuthViewModel
- 文件: `Auth/*.swift`

### Task 12: iOS 主题系统
- Color 扩展 + AppTheme
- ThemeManager (ObservableObject)
- 文件: `Theme/*.swift`

## Phase 1: 共享 UI 组件

### Task 13: Android 毛玻璃组件
- GlassCard: RenderEffect 模糊 + 圆角 + 边框
- GlassScaffold: 背景图 + 模糊 + 内容层
- GlassTabBar: 底部导航栏毛玻璃效果
- 文件: `ui/components/Glass*.kt`

### Task 14: Android 通用组件
- AppCard, AppListTile, AppBackground
- 文件: `ui/components/App*.kt`

### Task 15: iOS 毛玻璃组件
- GlassCard: .ultraThinMaterial + 圆角 + 边框
- GlassScaffold: 背景图 + blur
- 文件: `Components/Glass*.swift`

### Task 16: iOS 通用组件
- AppCard, AppListTile, AppBackground
- 文件: `Components/App*.swift`

## Phase 2: 认证流程

### Task 17: Android 启动引导 + 登录
- AppBootPage → ServerConfigPage → LoginPage
- 极验验证码 WebView 集成
- 文件: `ui/login/*.kt`

### Task 18: iOS 启动引导 + 登录
- BootView → ServerConfigView → LoginView
- 文件: `Views/Login/*.swift`

## Phase 3: 主框架

### Task 19: Android 主框架
- MainScaffold: 5 Tab 底部导航 + 毛玻璃
- Dashboard: 系统资源 + 任务统计 + 趋势图
- 文件: `ui/main/*.kt`, `ui/dashboard/*.kt`

### Task 20: iOS 主框架
- MainTabView: 5 Tab + .ultraThinMaterial
- DashboardView: 系统资源 + 任务统计
- 文件: `Views/Main/*.swift`, `Views/Dashboard/*.swift`

## Phase 4: 核心业务页面

### Task 21-24: Android 任务/日志/环境变量/更多
### Task 25-28: iOS 任务/日志/环境变量/更多

## Phase 5: 管理页面

### Task 29-36: Android 脚本/订阅/通知/依赖/用户/安全/系统/备份
### Task 37-44: iOS 脚本/订阅/通知/依赖/用户/安全/系统/备份

## Phase 6: 工具类 + 平台差异

### Task 45: Android 工具类
- 时间格式化、ANSI 解析、API 响应处理、弹窗工具
- 文件: `util/*.kt`

### Task 46: iOS 工具类
- 时间格式化、ANSI 解析、API 响应处理
- 文件: `Utilities/*.swift`

### Task 47: 平台差异说明
- Android: 后台任务(WorkManager), 权限(ActivityResultContracts), 剪切板(ClipboardManager)
- iOS: 后台任务(BGTaskScheduler), 权限(Info.plist), 剪切板(UIPasteboard)
- 文件: `docs/platform-differences.md`
