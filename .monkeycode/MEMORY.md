# 用户指令记忆

## 项目简介
- daidai-panel-native: Flutter UI → MethodChannel → Android `:panel` process → gomobile Go Core
- 8 个 runtime: python-3.14-android-arm64, node-lts-android-arm64, typescript-stable, shell-android-arm64, git-android-arm64, ssh-android-arm64, yaegi-go, go-builder-android-arm64
- CI: ci.yml (PR), android-release.yml (构建+发布), android-device-smoke.yml (设备测试)
- VERSION: 0.3.18 (单一来源: VERSION.json + scripts/version.py)

## 条目

### x86_64 Android 模拟器 Go runtime 兼容性问题
- Date: 2026-08-02
- Context: Agent 在 CI smoke 测试中排查 x86_64 模拟器 crash 时发现
- Category: 排错调试
- Instructions:
  - Go `android/amd64` target 在 x86_64 Android 模拟器上加载 Go Core (libgojni.so) 时必定 crash，产生 DeadObjectException
  - 已排除: CGo 依赖(纯Go)、ARM 翻译层(libndk_translation)、ABI 混淆、内存不足、ELF 格式问题
  - 模拟器会在 crash 后 offline (adb 断开)，无法收集 tombstone/logcat
  - 最终方案: x86_64 emulator job 设 `continue-on-error: true`，构建验证和证据收集正常进行
  - macOS ARM64 模拟器因 HVF 不支持无法使用

### 构建脚本依赖链
- Date: 2026-08-02
- Context: Agent 在修复 CI 构建时发现
- Category: 构建编译
- Instructions:
  - Python runtime: `prepare-android-python-runtime.sh` 需在 CI 中运行
  - Node runtime: `prepare-android-node-runtime.sh` 需在 CI 中运行  
  - 这两个脚本负责下载和准备 Android runtime 包，Gradle 构建依赖它们的输出
  - `verify-runtime-contract.go` 的结构体需与 runtime metadata JSON 格式同步

### 本地验证入口限制
- Date: 2026-08-20
- Context: Agent 在执行 Android runtime 重构验证时发现
- Category: 构建编译
- Instructions:
  - 当前工作区没有 `app/android/gradlew`，环境中也没有可用的 `gradle` 或 `flutter` 命令，Android 单元测试需在具备 Gradle/Flutter SDK 的环境运行
  - 根目录 `go.work` 只包含 `./panel/server`；`scripts/` 没有独立 `go.mod`，验证脚本测试需用 `GO111MODULE=off go test` 加显式文件列表执行

### Android Alpine rootfs 构建入口
- Date: 2026-08-20
- Context: Agent 在接入完整 rootfs 执行层时发现
- Category: 构建编译
- Instructions:
  - Android 内置 Alpine rootfs 资产通过 `app/scripts/prepare-android-alpine-rootfs.sh` 生成
  - Android PRoot/BusyBox native 工具通过 `app/scripts/prepare-android-proot-busybox.sh` 从 Termux aarch64 包提取到 `app/android/app/src/main/jniLibs/arm64-v8a/`
  - 默认镜像源：Alpine APK 使用 Huawei，Python pip 使用 Alibaba，Node.js npm 使用 npmmirror
  - APK 构建前 `verifyLinuxRootfsRuntime` 会校验 `android-runtime/arm64-v8a/rootfs.tar.gz.bin`、checksum、PRoot 和 BusyBox 是否存在
