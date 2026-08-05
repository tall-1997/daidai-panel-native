# daidai-panel-native

呆呆面板 Android 本机版。在普通非 Root ARM64 Android 手机上安装一个 APK，即可运行自包含本地面板，无需 Docker、远程服务器、Termux 或额外配置。

当前版本：**v0.4.3**

## 核心能力

- 内置 Python 3.14、Node.js 26、npm 和 Android Shell 运行时。
- 导入、编辑和运行 Python、JavaScript、TypeScript 与 Shell 脚本。
- 脚本异步运行，启动后立即返回运行 ID，支持增量实时日志、停止和清理。
- 任务 CRUD、批量运行、Cron 调度、任务视图、日志、统计、复制和置顶。
- 任务 `before` / `after` Hooks，以及任务完成 Android 本地通知。
- 环境变量 CRUD、分组、排序、导入导出和运行时注入。
- pip 与 npm 依赖安装、安装后验证、镜像配置和依赖导出。
- 订阅管理、拉取、停止、日志和 SSE 拉取状态。
- Android 本地通知、Webhook、ntfy 与 Gotify 通知渠道。
- 用户、安全会话、登录日志、IP 白名单、SSH Key、平台令牌和 Open API 管理。
- 本地备份创建、上传、下载、删除及事务恢复。
- 支持 JSON、`.tgz` / `.tar.gz` 和 Go AES-256-GCM `.enc` 后端面板备份导入。
- 深色/浅色主题、自定义背景、应用锁及针对滚动和玻璃效果的性能优化。

## 脚本实时日志

脚本页面的运行接口采用异步模型：

1. `POST /api/scripts/run` 或 `/api/scripts/run-code` 立即返回 `run_id`。
2. 后台执行进程逐行采集 stdout/stderr。
3. `GET /api/scripts/run/{run_id}/logs` 返回增量日志和运行状态。
4. `PUT /api/scripts/run/{run_id}/stop` 会终止实际进程。

日志具有并发保护和容量限制，避免长时间运行脚本无限占用内存。

## Android 平台说明

- 首期发行包仅包含 `arm64-v8a`。
- 本地服务只监听 `127.0.0.1`，并校验 Host、Origin、认证令牌和请求边界。
- Python 与 npm 纯脚本包兼容性最好；需要 glibc、桌面 Linux API、`node-gyp` 或不支持 Android ARM64 的原生扩展可能无法安装。
- 普通非 Root Android 不能真实安装 apt/yum/apk 系统包。Linux 系统依赖请求会明确返回不支持，绝不会伪报安装成功。
- 内置 Python/Node 运行时属于 APK 资源，不支持从应用内卸载。
- TOTP 2FA 在本地 fallback 中明确标记为不支持，不会伪报启用成功。

## 目录结构

- `app/`：Flutter APP、Android `:panel` 进程、本地 HTTP fallback、Python/Node runtime。
- `panel/`：Go 面板源码与可嵌入 `server/mobilecore`。
- `.monkeycode/`：项目架构、需求和技术设计资料。

## 构建环境

- Flutter SDK（Dart 3.11 或兼容版本）
- Android SDK 36 / NDK
- JDK 21
- Windows、Linux 或 macOS Flutter 构建环境

Windows 示例：

```cmd
cd app
flutter analyze --no-pub
flutter test --no-pub

cd android
gradlew.bat --no-daemon -Dorg.gradle.java.home=C:\path\to\jdk-21 :app:assembleRelease
```

发行 APK 输出：

```text
app/build/app/outputs/apk/release/app-release.apk
```

如果需要重新构建嵌入式 Go Core：

```bash
cd app
PANEL_SOURCE_DIR=../panel bash scripts/build-mobile-core-aar.sh
```

## 验证范围

发布前执行以下真机回归：

- APP 冷启动和本地面板恢复。
- 底栏及“更多”页面模拟点击与布局检查。
- JS/Python/Shell 导入、直接运行、任务运行和环境变量注入。
- 脚本实时日志增量、长脚本停止、非法路径拒绝。
- Cron、Hooks、任务日志和本地通知。
- pip/npm 安装及安装后验证。
- 用户、安全、SSH、平台令牌、Open API 和订阅管理。
- 备份创建、修改数据、恢复及恢复结果校验。
- Flutter analyze、Flutter tests、Android debug/release 构建。

## 上游来源与致谢

本仓库整合并改造以下开源项目：

| 组件 | 来源 | 用途 | 许可证 |
| --- | --- | --- | --- |
| Flutter App | `https://github.com/linzixuanzz/Dumb-Panel-APP` | 移动端 UI 与管理体验基础 | 以 `app/LICENSE` 为准 |
| Go 面板 | `https://github.com/linzixuanzz/daidai-panel` | 任务、脚本、依赖、订阅等核心能力基础 | 以 `panel/LICENSE` 为准 |

## 主要第三方依赖

| 组件 | 用途 | 许可证系列 |
| --- | --- | --- |
| Flutter / AndroidX / Kotlin | APP 与 Android 运行环境 | BSD-3-Clause / Apache-2.0 |
| Riverpod / Dio / GoRouter | 状态管理、网络和路由 | MIT / BSD 系列 |
| NanoHTTPD | Kotlin 本地 HTTP 服务 | BSD-3-Clause |
| WorkManager | 服务恢复与后台宿主 | Apache-2.0 |
| Commons Compress | Go `.tgz` 备份兼容 | Apache-2.0 |
| flutter_local_notifications | Android 本地通知 | BSD-3-Clause |
| liquid_glass_easy | UI 玻璃效果 | 以组件仓库声明为准 |
| Gin / GORM / robfig cron | Go Core HTTP、数据和调度 | MIT |

完整传递依赖以 `go.mod`、`pubspec.yaml`、`build.gradle.kts` 和锁文件为准。

## 许可证

- 本仓库新增代码使用根目录 `LICENSE` 中的 MIT License。
- `app/`、`panel/` 中的上游代码和资源继续遵循各自许可证及版权声明。
- 第三方依赖许可证归对应作者所有。

如发现许可证、NOTICE、依赖归属或功能问题，欢迎提交 Issue 或 Pull Request。
