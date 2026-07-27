# 呆呆面板 Flutter

呆呆面板 Flutter 是面向 Android 和 iOS 的移动端客户端，用于连接[呆呆面板](https://github.com/linzixuanzz/daidai-panel)服务并在手机端管理定时任务、脚本、环境变量、依赖、安全设置和开放 API。项目基于 [Dumb-Panel-APP](https://github.com/linzixuanzz/Dumb-Panel-APP) 演进而来，采用 Riverpod 状态管理和 GoRouter 路由，界面风格为液态玻璃 (Liquid Glass)。

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Flutter](https://img.shields.io/badge/Flutter-3.x-02569B.svg?logo=flutter)](https://flutter.dev)
[![Latest Release](https://img.shields.io/github/v/release/tall-1997/daidai-flutter)](https://github.com/tall-1997/daidai-flutter/releases/latest)
[![Build Android & iOS](https://github.com/tall-1997/daidai-flutter/actions/workflows/build.yml/badge.svg)](https://github.com/tall-1997/daidai-flutter/actions/workflows/build.yml)

本仓库遵循开放协作原则，欢迎提交 Issue、讨论和 Pull Request。项目保留上游作者署名，并在 [第三方开源声明](docs/THIRD_PARTY_NOTICES.md) 中列出主要依赖及许可证信息。

## 版本

- App 版本：`v0.1.56`
- Dart SDK：`>=3.11.3`
- 适配面板：`v2.3.0+`

## 更新说明

### v0.1.56

- 新增 Android 普通版本地面板 MVP，App 可自动启动回环 API与本地 SQLite。
- 本地支持管理员初始化、登录、主页、任务、环境变量和依赖记录基础功能。
- 修复依赖日志错误终态、自动重连提示和长日志内存持续增长问题。

### v0.1.55

- 修复 Android 多次更新后 APK、差分包和下载残片持续占用缓存空间的问题。
- App 冷启动、开始更新、更新失败及安装器返回时自动清理更新缓存。

### v0.1.54

- 清理剩余行为型 Analyzer 问题，补齐异步上下文生命周期保护。
- 迁移 Flutter 下拉表单弃用 API，并清理用户卡冗余上下文与引用字段。

### v0.1.53

- 继续修复启动登录生命周期、通知深链、任务视图、配置脚本和 Android Runtime 状态问题。
- 迁移 14 处 Flutter 弃用下拉 API，进一步清理冗余上下文和字段。

### v0.1.52

- 完成 SSE、认证刷新、更新安装、分页、权限路由和通知深链稳定性加固。
- 重构 Android Runtime、任务视图、config.sh 和环境高级工具状态管理。
- 清理无引用导入、变量和环境变量旧方法，Analyzer 问题显著减少。

### v0.1.51

- Dashboard 自动更新检查调整为每 24 小时最多一次，同版本自动提醒每天最多一次。

### v0.1.50

- 主动更新检查可区分网络失败、最新版本、发现更新和 Release 缺少安装包。

### v0.1.49

- 本地任务通知支持点击跳转对应任务日志，兼容前台、后台和冷启动场景。

### v0.1.48

- 完成剩余日志状态、Dashboard 指标、本地通知图标和应用锁状态的透明语义色统一。

### v0.1.47

- 修复环境变量详情和全局弹层表面过透明、背景遮罩过度压暗问题。
- 任务“已启用”等状态徽标改为低透明语义玻璃色。
- 同步修复依赖、订阅、Cron、元数据和日志状态标签的浅色实底。

### v0.1.46

- 第二阶段新增平台令牌、高级配置脚本和 Android/Magisk 运行时管理。
- 新增 pip/npm 实际依赖清单与环境变量高级工具。
- 完成后端现有能力两阶段移动端映射。

### v0.1.45

- 第一阶段新增个人资料、头像、用户名与密码管理。
- 新增系统健康诊断和服务端任务自定义视图。
- 增加客户端角色路由守卫与共享 Loading/Empty/Error 状态。

### v0.1.44

- 完成 Android bsdiff 差分升级正式发布，并兼容 Android 23–27 的已安装 APK版本信息读取。
- Release 自动发布完整 APK、差分 patch 和固定地址 `android-update.json`。
- 补充 XeonBsDiff 第三方开源声明。

### v0.1.43

- Android 支持 GitHub 静态 JSON 驱动的 bsdiff 差分增量升级。
- 差分包通过原生后台 bspatch 与已安装 APK合并，完成后唤起系统安装器。
- 补丁和目标 APK执行文件大小、MD5 与 SHA-256 校验，异常时自动回退完整 APK。
- GitHub Actions 自动生成、反向验证并发布差分包与 `android-update.json`。

### v0.1.42

- 定时任务页搜索、批量、排序、筛选、任务卡操作和底部操作区域统一为底部导航同款 Liquid Glass 材质。
- 降低全局原生按钮浅色背景不透明度，修复其他页面同类白色及偏白实底按钮。
- 保留按钮文字、图标、尺寸、点击区域、业务回调和危险操作语义。

### v0.1.41

- 统一弹窗、底部面板、信息预览、SSH 公钥和应用锁图案点位的 Liquid Glass 风格。
- 任务菜单“日志文件”改为结构化文件列表，可点击进入对应日志详情。
- root 二级页面使用无过渡导航和稳定首帧背景，修复更多页跳转时整屏闪烁。

### v0.1.40

- 修复认证刷新、SSE 重连、应用锁初始化、异步生命周期和请求竞态问题。
- 为订阅、依赖、安全日志和 Open API 日志补齐分页加载。
- 提升环境变量窄屏、键盘弹出和 Liquid Glass 滚动性能表现。
- 增加 API 解析回归测试与 Flutter Analyze/Test CI 质量门禁。

### v0.1.39

- 移除任务展开卡片中无效的“禁用 / 更多 / 删除”模糊侧滑按钮和整套侧滑手势状态。
- 任务卡恢复为单一点击/长按 Lens，保留运行/停止、状态、Cron 和订阅信息。
- 环境变量卡增加稳定间距和圆角裁剪，限制长值与备注行数，修复卡片紧贴和视觉重叠。

### v0.1.38

- 完成全应用 `liquid_glass_easy 3.3.1` 原生控件、交互动作和弹层迁移。
- 任务主卡、侧滑操作条、应用锁面板和高价值对话框统一为真实 Liquid Glass Lens。
- 全仓业务提示统一使用玻璃浮动通知，完整卡片和可点击表面统一使用共享 Lens 组件。
- Android/iOS Release 构建与 Shader 资产验证通过。

完整历史更新见 [GitHub Releases](https://github.com/tall-1997/daidai-flutter/releases)。

### v0.1.22

- 玻璃效果：安全设置中的登录日志和在线会话列表改用真实 GlassCard 渲染，提升深色模式和快速滚动下的视觉一致性。

### v0.1.21

- 玻璃效果：通知渠道列表改用真实 GlassCard 渲染，提升深色模式和快速滚动下的视觉一致性。

### v0.1.20

- 深色模式：将暗色玻璃主题从白色高光玻璃调整为深色玻璃基底，更符合深色模式定义。
- 深色模式：提高卡片和输入框暗色底色不透明度，改善主页面和一级页面列表卡片对比度。
- 玻璃效果：滚动列表中的 GlassCard 明确固定为 standard 质量，减少快速滑动时玻璃效果变透明的问题。

### v0.1.19

- 玻璃效果：用户管理列表改用真实 GlassCard 渲染，提升深色模式和快速滚动下的视觉一致性。
- 稳定性：补充任务列表 GlassCard 所需 Liquid Glass 导入，确保任务列表玻璃层构建通过。

### v0.1.18

- 玻璃效果：任务列表卡片采用低风险 `AnimatedContainer + GlassCard` 包裹结构，保留左滑动画并增强液态玻璃稳定性。
- 稳定性：修复上一轮任务列表真实玻璃改造引入的构建语法问题。

### v0.1.17

- 主题：修复深色模式下主页面及一级页面卡片底色偏浅、对比不足的问题。
- 玻璃效果：固定滚动列表玻璃质量为 standard，关闭自适应质量自动降级，避免快速滑动时卡片看起来失去液态玻璃效果。
- 列表：任务列表和环境变量列表改用真实 GlassCard 渲染，替换伪玻璃 Container 背景。

### v0.1.16

- 清理：删除未使用的 `AuthService.changePassword()` 方法。
- 清理：删除未使用的 `ApiEndpoints.password` 常量。

### v0.1.15

- 通知：自定义通知渠道配置 JSON 解析失败时会阻止保存并提示格式错误。
- 清理：移除 `main.dart` 中未使用的导入，降低 analyzer warning 风险。

### v0.1.14

- 清理：删除 AuthService 中已被各功能模块 provider 替代的 legacy API 包装代码。
- 清理：删除未使用的 ApiEndpoints 常量，减少旧接口误用风险。

### v0.1.13

- 日志流：SSE 认证刷新失败时主动释放连接资源，避免认证失效后残留连接。
- 更新：安装包下载改为临时文件校验成功后再替换正式缓存，弱网失败时不破坏旧缓存。
- 通知：iOS 启动初始化阶段不再主动申请通知权限，仅在用户点击请求权限时弹窗。
- Open API：调用日志分页加载增加最大页数与重复页保护，避免异常 total 导致连续请求。
- 环境变量：排序模式会保存所有拖拽步骤，避免连续拖动后只提交最后一次移动。

### v0.1.12

- 兼容性：将 `DropdownButtonFormField.initialValue` 全部替换为 `value`，兼容更多 Flutter 稳定版。
- 任务：编辑历史任务时，已保存但当前运行时不可用的 Python 版本会保留为可选项，避免下拉值不匹配崩溃。
- 日志流：SSE 解析支持 CRLF 行尾，修复实时日志可能不刷新的问题。
- 登录：初始化管理员失败时停止后续登录流程，保留初始化状态并显示明确错误。

### v0.1.11

- 应用锁：后台返回触发锁定时改用根 ProviderContainer，不再依赖 `rootElement!` 强制解包，异常会输出调试日志。
- 架构：标记 AuthService 中旧式功能 API 包装，避免后续新功能误用旧路径或旧 HTTP 方法。
- Open API：调用日志改为自动分页加载全部记录，避免固定 page size 导致日志截断。

### v0.1.10

- 任务：新增 Cron 模板加载和 Cron 表达式解析预览。
- 任务：新增任务统计和任务日志文件入口。
- Open API：新增获取访问 Token 功能，可通过 App Key/App Secret 获取 24 小时访问令牌。
- 通知：新增主动发送通知入口，可选择渠道并发送自定义标题和正文。
- 系统：新增部署与 Python 运行时状态信息展示。
- 日志流：SSE 支持 Token 过期自动刷新后重连，并按标准 SSE 聚合多行 data 事件。
- 本地通知：权限检查不再触发系统权限申请弹窗，仅在用户点击申请权限时请求授权。
- 环境变量：加载失败时显示明确错误与重试入口。

### v0.1.9

- 任务：新增任务导入/导出入口，支持保存任务导出文件和从 JSON 文件导入任务。
- 环境变量：新增环境变量导入/导出入口，支持导出全部变量和从 JSON 文件导入变量。
- 登录：本地地址、localhost、内网 IP 默认使用 HTTP 协议，公网地址继续默认使用 HTTPS。
- 更新：下载更新包前校验 Release asset 的文件大小和 SHA256 digest，避免复用旧包或损坏包。
- 仪表盘：核心概览数据与可选接口分离加载，面板设置或版本接口异常时仍可展示仪表盘。
- 文档：新增上游功能与问题候选清单，便于按 P0/P1/P2 继续排期。

### v0.1.8

- 更新检测：修复安装同版本修复包后仍反复提示更新的问题，去除同版本 `published_at` 时间回退判定
- 更新检测：保留版本号和 build number 比较，安装当前 Release 后会正确显示已是最新版本
- 发布：重新生成 Android APK 和 iOS IPA 安装包，Release 与仓库说明同步更新

### v0.1.7

- 主题：移除经典风格，全部页面统一使用液态玻璃效果；全局 LiquidGlass theme 配置对齐 iOS26 ultraThickMaterial 材质（light: thickness=32/blur=12, dark: thickness=48/blur=18）
- 导航：主脚手架使用 LiquidGlassScaffold，底部选项卡使用 LiquidGlassBottomNavBar 折射材质
- 界面：恢复主题设置中的背景图片选择器和模糊强度滑块（0-20 可调）
- 界面：统一全部二级/三级页面卡片背景使用 glassCardColor，移除硬编码 Colors.white/slate900
- 架构：清理 glassMode 状态字段和所有条件分支，减少 200+ 行冗余代码

- 应用锁：开启开关时要求先配置解锁方式，未配置时引导用户前往设置
- 主题：修复经典模式下背景图片被过度模糊遮盖导致不可见问题
- 环境变量：列表卡片布局优化，增大文字和间距，长值自动折行（maxLines 2→8）
- 依赖管理：卡片扩容（padding 10→14），状态/版本/副标题字体增大，间距优化
- 任务列表：状态标签、底部时间、计划摘要字体和间距全面上调
- 用户管理：角色标签、登录/创建时间字体上调，行间距增加
- 日志/订阅/通知：统一调优辅助文字字体大小和行间距

### v0.1.5

- 应用锁：修复生物识别验证失败后界面卡死问题，静默失败时自动切换到密码/图案解锁
- 应用锁：修复仅开启生物识别时可能导致的 lockout 风险
- 界面：修复液态玻璃模式下滑动时底部选项卡变透明问题
- 通知：添加后台通知回调处理和 AppLifecycleObserver，修复后台运行时不推送通知问题
- 安全：添加 WidgetsBindingObserver 监听应用生命周期，切回前台自动触发二次验证

### v0.1.4

- 安全中心：新增登录统计和审计日志 Tab，当前共 6 个 Tab
- SSH 密钥管理：新增 SSH Key 增删改查页面，支持在订阅中关联 SSH Key 进行 Git 认证
- 订阅管理：创建/编辑订阅时新增 SSH Key 下拉选择器
- 面板设置：新增可视化面板外观配置页面（标题、图标、编辑器/日志背景色）
- 脚本调试：新增 `_runCode()` 代码直接执行和 `_clearRun()` 清除运行记录
- 登录诊断：健康检查失败时显示 CORS / NAS 反向代理配置指引，HTTP 状态码异常时给出友好提示
- 移除 `android-native/` 和 `ios-native/` 原生模块

### v0.1.3

- 初始版本，提供 14 个功能模块的完整管理能力

## 软件架构设计

### 分层架构

```
lib/
  core/        -- 基础设施层：认证、网络、存储、主题、路由、系统服务
  features/    -- 功能模块层：每个模块含 views / providers / widgets
  shared/      -- 共享层：数据模型、工具类、公共 UI 组件
```

### 数据流

```
UI (Views) -> Riverpod Providers -> AuthService / DioClient -> REST API
                                                     \-> SSE Client -> Stream
```

- **Views**：用户界面层，Flutter Widget 构建
- **Providers**：Riverpod 状态管理，负责数据获取、缓存与业务逻辑
- **Services**：与后端通信的 API 封装层，包括 REST（Dio）和 SSE 流式推送
- **Storage**：flutter_secure_storage + SharedPreferences 双层持久化

### 路由设计

使用 GoRouter 声明式路由，通过认证守卫控制访问权限。底部导航栏 5 个 Tab：仪表盘、任务、日志、环境变量、更多。

### 主题系统

Material 3 主题 + `liquid_glass_easy 3.3.1` 实时折射液态玻璃风格。主界面使用 `LiquidGlassScaffold` 和 `LiquidGlassBottomNavBar`，内容组件使用 `LiquidGlassLens`，支持 Impeller/Skia、浅色/深色模式、自定义背景图片和模糊强度调节。

## 核心功能

### 功能模块总览

| 模块 | 功能概述 |
|------|----------|
| 登录与认证 | 用户名/密码 + TOTP 两步验证，支持极验验证码，本地可信登录会话 7 天有效期 |
| 仪表盘 | 系统概览、CPU/内存/磁盘资源卡片、任务统计、App 版本更新检测 |
| 定时任务 | 任务增删改查、Cron 表达式、启停/置顶/复制/批量操作、导入导出、通知绑定 |
| 执行日志 | 日志列表搜索筛序、批量删除、SSE 实时流式日志、日志清理 |
| 脚本管理 | 脚本文件树浏览/编辑/上传/下载、版本控制、代码直接运行调试 |
| 环境变量 | 变量增删改查、分组/排序/启停/批量操作、导入导出 |
| 依赖管理 | pip/npm 依赖安装/卸载/重装、Python 运行时版本切换、安装日志流式输出 |
| 订阅管理 | Git 仓库/单文件订阅、同步/启停、SSH Key 关联认证 |
| 通知管理 | 钉钉/企微/飞书/Bark 等渠道配置、启停/测试发送、本地推送通知 |
| 安全中心 | 登录日志、在线会话、IP 白名单、审计日志、登录统计、两步验证 |
| 开放 API | API Token 和应用管理、创建/启禁用/重置密钥 |
| 用户管理 | 系统用户增删改查、启禁用 |
| 系统设置 | 并发限制、日志留存、面板更新、数据备份与恢复 |
| 应用锁 | 密码/图案/生物识别，SHA256 迭代哈希存储 |
| 面板设置 | 面板外观配置（标题/图标/编辑器背景/日志背景） |
| SSH 密钥 | SSH Key 增删改查，供订阅 Git 认证使用 |

### 功能模块与依赖库

| 模块 | 引用的依赖库 | 用途 |
|------|-------------|------|
| 路由导航 | `go_router` | 声明式路由、认证守卫、深层链接 |
| 状态管理 | `flutter_riverpod` | 全局状态共享、异步数据加载与缓存 |
| 网络请求 | `dio` + `http` | REST API 调用、Token 自动刷新拦截器 |
| SSE 流式 | `dio` + `http` | 服务端事件流接收，断线自动重连 |
| 安全存储 | `flutter_secure_storage` | Token、用户信息、面板配置的加密存储 |
| 本地存储 | `shared_preferences` | UI 状态、主题偏好、应用锁配置 |
| 图表展示 | `fl_chart` | 仪表盘 CPU/内存趋势图 |
| 生物识别 | `local_auth` | 指纹/面部识别应用锁 |
| 密码哈希 | `crypto` | SHA256 迭代哈希存储应用锁密码 |
| 主题 UI | `liquid_glass_easy` | LiquidGlassLens 卡片、LiquidGlassScaffold、LiquidGlassBottomNavBar |
| 国际化 | `intl` | 日期时间中文格式化 |
| 文件选择 | `file_picker` | 脚本上传、备份文件选择 |
| 设备信息 | `device_info_plus` | 客户端 User-Agent 构建 |
| 应用信息 | `package_info_plus` | 版本号读取、更新检测 |
| 文件路径 | `path_provider` | 应用文档目录访问 |
| WebView | `webview_flutter` | 极验验证码 WebView 弹窗 |
| 本地通知 | `flutter_local_notifications` | 任务执行完成/系统通知推送 |
| 图标生成 | `flutter_launcher_icons` | Android/iOS 应用图标自动生成 |
| 代码分析 | `flutter_lints` | Dart 代码规范检查 |

## 下载安装

| 平台 | 安装包 |
|------|--------|
| Android | [daidai-flutter-v0.1.39-android.apk](https://github.com/tall-1997/daidai-flutter/releases/download/v0.1.39/daidai-flutter-v0.1.39-android.apk) |
| iOS | [daidai-flutter-v0.1.39-ios.ipa](https://github.com/tall-1997/daidai-flutter/releases/download/v0.1.39/daidai-flutter-v0.1.39-ios.ipa) |

所有版本见 [GitHub Releases](https://github.com/tall-1997/daidai-flutter/releases)。

## 连接配置

启动 App 后在登录页填写面板地址：

- 默认地址：`http://127.0.0.1:5700`
- 常规接口：`/api`
- 流式接口：`/api/v1`

## 本地构建

```bash
flutter pub get
flutter analyze
flutter test
flutter build apk --release
flutter build ios --release --no-codesign
```

## 云端构建

推送到 `main` 分支会触发 GitHub Actions 自动构建 APK 和 IPA 并发布到 Release。工作流包括：

- Android 构建 (`android-build.yml`)
- iOS 构建 (`ios-build.yml`)
- 统一构建 (`build.yml`)
- Release 发布 (`release.yml`)

## 开源引用与致谢

本项目是社区维护的开源客户端，基于以下项目和开源生态构建：

- [Dumb-Panel-APP](https://github.com/linzixuanzz/Dumb-Panel-APP) -- 原始 Flutter 客户端，提供核心功能模块和 UI 设计；上游仓库当前未检测到独立 LICENSE 文件，使用和分发前请核对上游声明
- [daidai-panel](https://github.com/linzixuanzz/daidai-panel) (MIT) -- 呆呆面板后端服务，提供 API 接口和数据模型
- [Flutter](https://flutter.dev) (BSD-3-Clause) -- Google 的跨平台 UI 框架
- [Riverpod](https://riverpod.dev) -- Dart/Flutter 响应式状态管理库
- [GoRouter](https://pub.dev/packages/go_router) -- Flutter 声明式路由库
- [Dio](https://pub.dev/packages/dio) -- Dart HTTP 客户端
- [fl_chart](https://pub.dev/packages/fl_chart) -- Flutter 图表库
- [Liquid Glass Easy](https://github.com/AhmeedGamil/liquid_glass_easy) (MIT) -- 实时折射液态玻璃 UI 组件库
- [flutter_secure_storage](https://pub.dev/packages/flutter_secure_storage) -- Flutter 安全存储
- [local_auth](https://pub.dev/packages/local_auth) -- 本地生物认证
- [webview_flutter](https://pub.dev/packages/webview_flutter) -- Flutter WebView

完整依赖、来源与许可证清单见 [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)。依赖库的商标、版权和许可证归各自权利人所有。

## 参与贡献

- 提交问题前请先搜索已有 Issue，并提供版本、平台、复现步骤和日志。
- 代码贡献请参阅 [CONTRIBUTING.md](CONTRIBUTING.md)。
- 安全问题请参阅 [SECURITY.md](SECURITY.md)，避免在公开 Issue 中披露敏感细节。

## 许可证

MIT License
