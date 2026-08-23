# 呆呆面板原生版项目文档

## 项目愿景

用户只安装呆呆面板 App，即可在 Android 手机本机创建、管理和运行面板，无需准备 Docker、服务器或独立后端服务。

产品面向非 Root Android，通过 GitHub Release 提供 ARM64 完整版。ARM64 App 内置面板核心、Python、Node.js、TypeScript、受控 Shell、Git 与 Go 工具链；ARM32 与 x86_64 多架构预研保留在 `shelved/multi-arch-runtime` 分支。

## 已确认范围

- 导入并持续同步现有 Flutter App 与 Go 后端。
- 采用 Android Foreground Service、gomobile 生命周期薄绑定和 Go 回环 HTTP Core。
- 所有运行时随 APK 交付，安装后可离线完成基础执行。
- 每条发布轨道提供一个自包含 APK，同一设备同一时刻安装一个轨道。
- 发布现代 SDK 主版本和 Legacy 实验版本，两者使用同一应用 ID、签名、版本代码、数据格式和 API 契约。
- 现代版使用 Yaegi 执行受控 Go 源码，并通过 Go 工具链提供模块准备与构建导出；Legacy 版提供实验性的 `go run`、`go test` 和 `go build`。
- 首版只监听回环地址，局域网访问能力进入后续规划。

## 当前实现状态

- 单一版本源为仓库根目录 `VERSION.json`，当前版本 `1.0.13`，Android version code 为 `1000130`。
- capability 状态为 `enabled` 时，请求进入已注册的真实 Go Handler；禁用或未声明能力返回稳定的 `PLATFORM_CAPABILITY` 结果。
- Go Core 在运行时组件受阻时保持管理核心可用并报告 `degraded-ready`，运行时执行保持 fail-closed。
- Kotlin fallback 提供控制面兼容 API，复用安装级 token，并严格校验回环 Host 与 Origin；能力响应逐项标记 native runtime、Git 订阅、Open API token、2FA 和多设备会话支持状态。
- Alpine rootfs 已成为首选完整 Linux 执行层，内置 Bash、Python、pip、Node.js、npm、uv、pnpm、CA、Git 与 SSH，并提供交互式 PTY session。
- 八个 runtime ID 已统一清单、兼容性、smoke、APK 和设备证据契约；严格真机 smoke 与 16 KB 运行证据继续由发布矩阵收集。
- Go、Flutter、Kotlin、Web、route、race 和 ARM64 构建门禁已通过；ARM64 真机长稳与严格 16 KB 设备证据继续作为发布后补充矩阵。

## 文档索引

- `ARCHITECTURE.md`：当前系统边界、capability 分派、降级状态、运行时契约和交付结构。
- `RELEASE_EVIDENCE.md`：snapshot/tag 门禁差异、CI 顺序、发布证据与未完成矩阵。
- `../specs/android-local-panel/requirements.md`：EARS 格式产品需求与验收标准。
- `../specs/android-local-panel/design.md`：Android 完整执行版技术设计。

## 上游基线

- Flutter App：<https://github.com/linzixuanzz/Dumb-Panel-APP>
- Go 面板：<https://github.com/linzixuanzz/daidai-panel>
- upstream 主分支基线提交：`7c5e8e6e1acc40b03febeedd64544fbde726562c`

## 产品边界

“完整执行”表示 APK 内覆盖 Python、Node.js、TypeScript、受控 Shell、Git 与 Go 任务主链路。Android ARM64/Bionic 兼容清单定义可安装的原生 Python 扩展、Node addon 和 CGO 依赖。通用 Linux 包管理器、任意 glibc 二进制和系统级操作不属于普通 Android 沙箱能力。
