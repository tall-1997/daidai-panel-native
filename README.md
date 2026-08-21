# 呆呆面板 Android 本机版

[![Quality](https://github.com/tall-1997/daidai-panel-native/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/tall-1997/daidai-panel-native/actions/workflows/ci.yml)
[![Android Build](https://github.com/tall-1997/daidai-panel-native/actions/workflows/android-release.yml/badge.svg?branch=main)](https://github.com/tall-1997/daidai-panel-native/actions/workflows/android-release.yml)
[![Device Smoke](https://github.com/tall-1997/daidai-panel-native/actions/workflows/android-device-smoke.yml/badge.svg?branch=main)](https://github.com/tall-1997/daidai-panel-native/actions/workflows/android-device-smoke.yml)
[![Release](https://img.shields.io/github/v/release/tall-1997/daidai-panel-native)](https://github.com/tall-1997/daidai-panel-native/releases/latest)
[![License](https://img.shields.io/github/license/tall-1997/daidai-panel-native)](LICENSE)

呆呆面板 Android 本机版面向普通非 Root ARM64 Android 设备。安装一个 APK 即可运行自包含本地面板，同时保留远程面板连接能力，无需 Docker、Termux 或独立服务器。

当前稳定版：**v1.0.2**

Android versionCode：**1000020**

默认分支：**main**

## 下载

- 最新稳定版：[GitHub Releases](https://github.com/tall-1997/daidai-panel-native/releases/latest)
- v1.0.2：[发行说明与附件](https://github.com/tall-1997/daidai-panel-native/releases/tag/v1.0.2)
- ARM64 APK：`daidai-panel-native-1.0.2-release-arm64.apk`
- APK SHA-256：`668d8632086b0ec5cd1ebec6a96be4ee58ff84ac693ceb8d5c01b4f062b1c440`

正式 Release 同时提供 APK 校验文件、`android-update.json` 和 release evidence 证据包。

## 核心能力

- Flutter UI 统一管理 Android 本地实例和远程呆呆面板。
- 本地 Go Core 运行于 Android `:panel` 独立进程，并监听动态 `127.0.0.1` 端口。
- Kotlin fallback 在 Go Core 不可用时提供兼容管理与执行能力。
- 支持任务、Cron、脚本、日志、环境变量、订阅、通知、用户、安全、SSH、Open API、平台令牌和备份恢复。
- 内置 Python 3.14、Node.js 18.20.4、TypeScript、受控 Shell、Git、SSH、Yaegi 和 Go Builder 运行时契约。
- 支持 pip/npm 依赖安装、指定版本、安装去重、共享目录、镜像配置和缓存限制。
- 支持实时增量日志、持久日志回读、任务停止、进程树回收和 Android 本地通知。
- 支持中文脚本路径、空格、引号、显式空参数和多账号变量。
- 支持 App 内安全打开本机浏览器面板，一次性票据仅用于动态回环地址。
- 本地接口校验安装级 Token、精确 Host、Origin、JWT、角色和权限。

## 平台边界

- 正式 APK 当前仅提供 `arm64-v8a`。
- 本地服务仅监听 `127.0.0.1`，不会暴露到局域网。
- Python 与 npm 纯脚本包兼容性最好。
- 依赖 glibc、桌面 Linux API、`node-gyp` 或不兼容 Android ARM64 的原生扩展可能无法使用。
- APK 内置 runtime 随 App 更新，本地实例不提供后端自更新或 runtime 卸载。
- 持续调度开启时使用可见 Foreground Service；普通后台模式会暂停 Flutter 连接轮询并降低 fallback 调度唤醒频率。
- GitHub 云 runner 不支持 ARM64 Android 模拟器，ARM64 矩阵以 blocked evidence 如实记录，真机证据可通过 self-hosted runner 补充。

## 仓库与分支

仓库地址：[tall-1997/daidai-panel-native](https://github.com/tall-1997/daidai-panel-native)

| 项目 | 当前状态 |
| --- | --- |
| 默认分支 | `main` |
| 远程开发分支 | `main` |
| 当前稳定标签 | `v1.0.2` |
| 单一版本源 | `VERSION.json` |
| Android 应用 ID | `com.daidai.daidai_app` |
| 最低 Android API | 28 |
| 目标 Android API | 35 |
| 编译 Android API | 36 |
| 支持 ABI | `arm64-v8a` |

### 分支策略

- `main` 保存已通过 CI 的集成代码，也是 GitHub Actions snapshot 构建来源。
- 功能开发使用短生命周期分支，完成评审与门禁后快进或合并到 `main`。
- 稳定版本使用 `vMAJOR.MINOR.PATCH` 标签和 GitHub Stable Release。
- `VERSION.json` 中的版本必须与 App、Go Core 和 Panel Web 派生版本一致。
- 提交签名前确认工作区仅包含目标修改，并保持密钥、JKS 和密码位于 GitHub Secrets。

### 当前发布链路

`main` 推送自动触发：

1. `PR and Main Quality`
2. `Android Build and Release` snapshot
3. `Android Device Runtime Smoke`

稳定发布通过 `Android Build and Release` 的 `workflow_dispatch` 执行 `stable` 通道。工作流会验证版本、JKS、证书指纹、Go Core、Flutter、Kotlin、AAR、运行时契约、APK 元数据、SHA-256 和同提交设备证据，然后创建 GitHub Release。

## 目录结构

| 路径 | 用途 |
| --- | --- |
| `app/` | Flutter App、Android Host、Kotlin fallback 和移动端测试 |
| `panel/server/` | Go Core、HTTP API、Scheduler、Executor 和服务测试 |
| `panel/web/` | 本机浏览器面板 Web 前端 |
| `runtime/` | 运行时清单、兼容矩阵和 smoke evidence |
| `contracts/` | 移动端 API 路由契约 |
| `scripts/` | 版本、运行时、路由和发行证据工具 |
| `.github/workflows/` | Quality、Android Build 和 Device Smoke 工作流 |
| `.monkeycode/docs/` | 项目架构与开发文档 |
| `.monkeycode/specs/` | 需求、设计和实施任务 |

## 构建环境

CI 使用以下工具链：

| 工具 | 版本 |
| --- | --- |
| Flutter | 3.41.5 stable |
| Dart | Flutter 3.41.5 内置版本 |
| Go | 1.25.0 |
| Java | Temurin 17 |
| Gradle | 9.1.0 |
| Node.js | 20，Panel Web 构建工具 |
| Android compileSdk | 36 |
| Android targetSdk | 35 |
| Android minSdk | 28 |

本地构建还需要 Android SDK、NDK、CMake、Linux shell 工具和可访问运行时资产源的网络环境。

## 开发构建

### 1. 校验版本

```bash
python3 scripts/version.py check
python3 scripts/version.py show
```

版本升级时先修改 `VERSION.json`，再同步派生文件：

```bash
python3 scripts/version.py sync
python3 scripts/version.py check
```

### 2. 测试 Go Core

```bash
go -C panel/server test ./...
go -C panel/server vet ./...
```

关键并发包可执行 race 检查：

```bash
go -C panel/server test -race ./mobilecore ./router ./handler ./service ./database
```

### 3. 测试 Flutter

```bash
cd app
flutter pub get
flutter analyze
flutter test
```

### 4. 准备 Android runtime

```bash
cd app
bash scripts/prepare-android-python-runtime.sh
bash scripts/prepare-android-node-runtime.sh
bash scripts/prepare-android-alpine-rootfs.sh
bash scripts/prepare-android-proot-busybox.sh
```

### 5. 构建嵌入式 Go Core AAR

```bash
cd app
PANEL_SOURCE_DIR=../panel bash scripts/build-mobile-core-aar.sh
```

### 6. 运行 Kotlin 单元测试

```bash
gradle -p app/android :app:testDebugUnitTest --no-daemon --stacktrace
```

### 7. 构建 ARM64 release APK

```bash
cd app
flutter build apk --release --target-platform android-arm64
```

Flutter APK 输出：

```text
app/build/app/outputs/flutter-apk/app-release.apk
```

### 8. 构建本机 Panel Web

```bash
cd panel/web
npm ci
VITE_LOCAL_WEB_BUILD=true npm run build
```

Android Gradle 构建会自动将本机 Web 资源打包到 APK 的 `/local-ui/` 路径。

## CI 构建

### Quality

工作流：`.github/workflows/ci.yml`

Quality 根据改动路径执行：

- Go test、vet 和 race
- Flutter analyze 与 tests
- Kotlin unit tests
- 路由契约检查
- Panel Web build
- 发行与运行时脚本测试

### Android Build

工作流：`.github/workflows/android-release.yml`

支持三种通道：

| 通道 | 用途 | GitHub Release |
| --- | --- | --- |
| `snapshot` | main 日常验证 | 仅 Actions artifact |
| `prerelease` | 可安装预发行 APK | 手动创建或工作流扩展 |
| `stable` | 正式签名稳定版 | 自动创建 Stable Release |

手动触发 snapshot：

```bash
gh workflow run android-release.yml \
  --repo tall-1997/daidai-panel-native \
  --ref main \
  -f release_channel=snapshot
```

### Device Smoke

工作流：`.github/workflows/android-device-smoke.yml`

默认执行 debug APK 构建、smoke driver 测试、x64 API 34/35 诊断和 ARM64 blocked evidence。连接 self-hosted ARM64 Android runner 后，可通过 `run_physical_device` 输入执行真机矩阵。

## 正式签名与发布

### GitHub Secrets

仓库需要配置以下 Secrets：

| Secret | 内容 |
| --- | --- |
| `KEYSTORE_BASE64` | JKS 文件的 Base64 内容 |
| `KEYSTORE_PASSWORD` | JKS store password |
| `KEYSTORE_ALIAS` | 签名 alias |
| `KEYSTORE_KEY_PASSWORD` | 私钥密码 |
| `KEYSTORE_CERT_SHA256` | 签名证书 SHA-256 |

密钥、密码和 JKS 文件禁止提交到仓库。`app/.gitignore` 与 `app/android/.gitignore` 已忽略常见 keystore 文件。

### 发布步骤

1. 修改 `VERSION.json`。
2. 执行 `python3 scripts/version.py sync`。
3. 执行 `python3 scripts/version.py check`。
4. 提交并推送 `main`。
5. 等待 Quality、snapshot Build 和 Device Smoke 成功。
6. 触发 stable 工作流。
7. 核验 Release、APK、SHA-256、更新清单和 evidence 包。

触发 stable 发布：

```bash
gh workflow run android-release.yml \
  --repo tall-1997/daidai-panel-native \
  --ref main \
  -f release_channel=stable
```

工作流对正式 APK 执行：

- JKS 可用性、alias 和密码校验
- JKS 证书 SHA-256 校验
- APK 签名结构校验
- APK 证书指纹与 JKS 证书匹配
- 运行时清单、兼容矩阵和内嵌 metadata 比对
- APK SHA-256 与 `android-update.json` 生成
- release evidence、SBOM 和第三方许可证打包

## 验证清单

发布前重点验证：

- 冷启动、本地 Core 恢复和动态 endpoint 更新
- 远程实例切换回本地实例
- Python、JavaScript、TypeScript、Shell 和 Go 任务
- 中文路径、空参数、环境变量和多账号变量
- 精确版本 pip/npm 依赖安装和导入
- Cron、Hooks、任务日志、SSE 和本地通知
- 后台与前台切换、持续调度和 WorkManager 恢复
- 用户、安全、SSH、平台令牌、Open API 和订阅
- 备份创建、导出、导入和事务恢复
- 本机浏览器面板的一次性票据、Cookie、JWT 和权限边界

## 项目文档

- 架构：`.monkeycode/docs/ARCHITECTURE.md`
- 发布证据：`.monkeycode/docs/RELEASE_EVIDENCE.md`
- 项目索引：`.monkeycode/docs/INDEX.md`
- 完整需求与设计：`.monkeycode/specs/android-modern-full-panel/`

## 上游来源

| 组件 | 来源 | 用途 |
| --- | --- | --- |
| Flutter App | [linzixuanzz/Dumb-Panel-APP](https://github.com/linzixuanzz/Dumb-Panel-APP) | 移动端 UI 与管理体验基础 |
| Go 面板 | [linzixuanzz/daidai-panel](https://github.com/linzixuanzz/daidai-panel) | 任务、脚本、依赖和订阅核心能力基础 |

## 许可证

- 本仓库新增代码使用根目录 `LICENSE` 中的 MIT License。
- `app/` 与 `panel/` 中的上游代码和资源遵循各自许可证及版权声明。
- 第三方依赖许可证归对应作者所有，完整清单以锁文件和 Release evidence 为准。

问题与建议请提交到 [GitHub Issues](https://github.com/tall-1997/daidai-panel-native/issues)。
