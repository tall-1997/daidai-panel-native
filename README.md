# daidai-panel-native

呆呆面板 Android 本机版。用户安装一个 APK 后即可在普通非 Root ARM64 手机上创建和管理本地面板，无需 Docker、服务器或独立后端。

本项目尊重开源协作与许可证要求，保留上游项目和第三方组件的许可声明。仓库根目录使用 MIT License；各上游目录和依赖仍遵循其原始许可证。

## 目录

- `app/`：Flutter App 与 Android `:panel` 进程宿主。
- `panel/`：Go 面板源码和可嵌入 `server/mobilecore`。
- `.monkeycode/`：项目架构、需求和技术设计。

## 本地验证

```bash
cd panel/server
go test -race ./mobilecore ./database

cd ../../app
PANEL_SOURCE_DIR=../panel bash scripts/build-mobile-core-aar.sh
flutter test
flutter analyze
flutter build apk --release
```

首期只构建 `arm64-v8a`。Android Core 监听动态 `127.0.0.1` 端口，并要求安装级 token、严格 Host 与 Origin 校验。

## 上游来源与致谢

本仓库整合并改造以下开源项目：

| 组件 | 来源 | 用途 | 许可证 |
| --- | --- | --- | --- |
| Flutter App | `https://github.com/linzixuanzz/Dumb-Panel-APP` | 移动端 UI 与管理体验基础 | 以 `app/LICENSE` 为准 |
| Go 面板 | `https://github.com/linzixuanzz/daidai-panel` | 面板后端、任务、脚本、依赖、订阅等核心能力基础 | 以 `panel/LICENSE` 为准 |

如果上游项目许可证、版权声明或 NOTICE 文件更新，本仓库会同步保留对应声明。

## 主要第三方依赖与许可证

以下列出直接依赖和构建关键依赖。完整传递依赖以 `go.mod`、`pubspec.yaml`、`build.gradle.kts`、锁文件和 Release evidence 中的 SBOM/许可证报告为准。

### Go / Core

| 依赖 | 用途 | 常见许可证 |
| --- | --- | --- |
| `github.com/gin-gonic/gin` | HTTP API 框架 | MIT |
| `github.com/gin-contrib/cors` | CORS 中间件 | MIT |
| `github.com/glebarez/sqlite` | SQLite GORM Driver | MIT/BSD 系列，具体以模块声明为准 |
| `gorm.io/gorm` | ORM | MIT |
| `github.com/golang-jwt/jwt/v5` | JWT | MIT |
| `github.com/google/uuid` | UUID | BSD-3-Clause |
| `github.com/robfig/cron/v3` | Cron 调度 | MIT |
| `golang.org/x/crypto` | 加密与 KDF | BSD-3-Clause |
| `golang.org/x/net` | 网络工具库 | BSD-3-Clause |
| `golang.org/x/mobile` | gomobile / gobind | BSD-3-Clause |
| `gopkg.in/yaml.v3` | YAML 解析 | Apache-2.0 / MIT 双许可，具体以模块声明为准 |

### Flutter / Dart

| 依赖 | 用途 | 常见许可证 |
| --- | --- | --- |
| Flutter SDK | UI 框架 | BSD-3-Clause |
| `flutter_riverpod` | 状态管理 | MIT |
| `dio` | HTTP 客户端 | MIT |
| `http` | HTTP 客户端 | BSD-3-Clause |
| `go_router` | 路由 | BSD-3-Clause |
| `flutter_secure_storage` | 安全存储 | BSD-3-Clause |
| `shared_preferences` | 本地偏好存储 | BSD-3-Clause |
| `local_auth` | 生物认证 | BSD-3-Clause |
| `crypto` | Dart 加密工具 | BSD-3-Clause |
| `fl_chart` | 图表 | MIT |
| `intl` | 国际化 | BSD-3-Clause |
| `logger` | 日志 | MIT |
| `device_info_plus` | 设备信息 | BSD-3-Clause |
| `file_picker` | 文件选择 | MIT |
| `package_info_plus` | 应用信息 | BSD-3-Clause |
| `path_provider` | 平台路径 | BSD-3-Clause |
| `webview_flutter` | WebView | BSD-3-Clause |
| `flutter_local_notifications` | 本地通知 | BSD-3-Clause |
| `liquid_glass_easy` | UI 组件效果 | 以其 pub.dev / 仓库声明为准 |

### Android / Kotlin

| 依赖 | 用途 | 常见许可证 |
| --- | --- | --- |
| Android Gradle Plugin / AndroidX | Android 构建与支持库 | Apache-2.0 |
| Kotlin Gradle Plugin / Kotlin stdlib | Kotlin 编译与运行库 | Apache-2.0 |
| `androidx.work:work-runtime-ktx` | WorkManager 恢复任务 | Apache-2.0 |
| `org.nanohttpd:nanohttpd` | Kotlin fallback 本地 HTTP 服务 | BSD-3-Clause |
| `com.android.tools:desugar_jdk_libs` | Java API desugaring | Apache-2.0 |
| `com.xeonyu:bsdiff` | APK 差分更新 | 以其 Maven/仓库声明为准 |
| `org.json:json` | JSON 测试依赖 | Public Domain / JSON License，具体以模块声明为准 |

## 许可证说明

- 本仓库新增代码以 `LICENSE` 中的 MIT License 发布。
- `app/` 与 `panel/` 中来自上游项目的代码、资源和历史版权声明分别以 `app/LICENSE`、`panel/LICENSE` 和上游仓库声明为准。
- 第三方依赖的许可证归各自作者所有。本项目不会移除或重写第三方版权声明。
- Release 产物会逐步补充 SBOM、第三方许可清单和 Runtime manifest，便于审计与再分发。

## 合规贡献

如果你发现许可证、版权归属、NOTICE 或依赖声明存在遗漏，请提交 Issue 或 Pull Request。我们会按照开源许可证要求补齐署名、链接和许可文本。
