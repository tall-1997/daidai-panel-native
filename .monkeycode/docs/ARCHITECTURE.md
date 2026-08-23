# 系统架构

## 架构原则

- Flutter UI 保持统一的本地与远程管理体验。
- Go 业务规则集中在可重入 Core，HTTP API 与 SSE 保持上游兼容。
- Android 宿主管理生命周期、前台服务、平台安全存储和系统状态。
- 运行时通过稳定平台接口接入任务执行器。
- 上游代码保持低侵入，契约测试作为同步门禁。
- 管理 Core 与运行时就绪状态分离：运行时组件 blocked 时，Core 以 `degraded-ready` 保留管理能力，执行入口保持 fail-closed。

## 总体架构

```mermaid
graph TD
    A["Flutter App"] --> B["Local Instance Adapter"]
    A --> C["Remote Instance Adapter"]
    B --> D["Android Foreground Service"]
    D --> E["gomobile Lifecycle Binding"]
    E --> F["Go HTTP Core"]
    F --> G["SQLite and Data Store"]
    F --> H["Scheduler"]
    F --> I["Execution Platform"]
    I --> J["Bundled Runtimes"]
    I --> K["Process and Resource Host"]
    C --> L["Remote Daidai Panel"]
```

## 仓库结构目标

```text
daidai-panel-native/
├── app/                         # 导入的 Flutter App
├── panel/                       # 导入的 Go 面板上游
├── mobile-core/                 # 可重入 Core 与 gomobile 薄绑定
├── android-host/                # Service、WorkManager 与平台能力
├── runtimes/                    # 运行时构建脚本、清单和补丁
├── contracts/                   # API、SSE 与能力契约测试
├── scripts/                     # 上游同步和发布工具
└── .monkeycode/                 # 项目文档与功能规格
```

实际导入时优先采用 Git Submodule 保存两个上游仓库，移动端适配代码通过独立目录和小型上游补丁维护。CI 固定上游 commit，并记录到发布版本清单。

## 核心组件

### Go Core

Go Core 从后端 `main()` 抽离，提供可重复启动和显式停止的实例：

```text
NewCore(options) -> Core
Core.Start() -> endpoint
Core.Stop(context)
Core.Status() -> health
```

Core 持有配置、数据库、调度器、执行器和 HTTP Server。数据库初始化、迁移和后台任务启动均返回可处理错误。系统更新、进程退出和宿主资源信息通过平台接口实现。

移动路由注册真实 Auth、Task、Log、Script、Env、Subscription、Notification、Security、System、OpenAPI、Deps、Config、Platform Token、Sponsor 和 Android Runtime Handler。capability middleware 对受控路由执行以下分派：`enabled` 继续进入真实 Handler；disabled 或未声明状态返回稳定的 `PLATFORM_CAPABILITY` 响应。

运行时基线独立汇总八个 runtime ID 的资产完整性与 smoke 状态。任一组件或检查受阻时，Core 状态为 `degraded-ready`，管理 HTTP Core、恢复和诊断仍可用；Runtime Locator 拒绝受阻组件的执行。

### Android Host

Android Host 提供：

- Foreground Service 与持续运行通知。
- WorkManager 恢复检查和错过任务补偿。
- Keystore、私有目录、文件导入导出和本地通知。
- 电量、温度、存储和网络状态。
- Core 限次重启与任务中断恢复。

嵌入式 Go Core无法达到 ready 时，Kotlin fallback 仅暴露 health、capability、认证和备份恢复诊断路由，状态为 `degraded`、模式为 `diagnostic`。fallback 与 Go Core 使用同一安装级 local token，并要求精确 `Host=127.0.0.1:<port>`、`Origin=http://127.0.0.1:<port>` 和 `X-Daidai-Local-Token`。

### Execution Platform

```text
RuntimeProvider     解析固定运行时及其版本
ProcessRunner       启动、停止和回收受管进程树
GitProvider         提供受控 Git 操作
DependencyManager   安装兼容清单内依赖
ResourceProvider    提供 App 与设备资源状态
LifecycleHost       接收前台服务和重启请求
```

当前八个 runtime ID 已统一到 `runtime/manifest.json`、`runtime/compatibility.json`、`runtime/smoke-evidence.json`、APK 元数据提取和 device smoke evidence。Alpine rootfs 作为首选完整 Linux 执行层，内置 `apk`、Bash、Python、pip、PyCryptodome、Node.js、npm、uv、pnpm、CA、Git 和 SSH。PRoot helper loader 作为签名 APK ELF 交付，并通过 `PROOT_LOADER` 覆盖上游 Termux 安装路径。生产设备的 API 28/4K、API 35/4K、API 35/16K 通过证据继续由 device smoke 收集。

### Flutter Adapter

本地实例适配器通过 Platform Channel 调用 `StartCore`、`StopCore`、`Status` 和 `Endpoint`。Core 就绪后，Dio 与 SSE 使用动态回环端点，业务页面继续使用现有 API Repository。

实例切换保留完整 `PanelConfig` 类型。本地实例通过 Android Host 重新解析动态 endpoint、instance ID 和 local token，再由连接 monitor 原子更新持久化实例与 Dio session；远程实例继续使用网络健康检查。Android Host 每次启动请求都会复核 Core/fallback 状态，避免返回已失效端口缓存。

### Task Logs and Dependencies

Go Core 与 Kotlin fallback 使用统一任务日志契约：排队状态持续跟踪，运行输出通过增量 cursor 推送，内存日志释放后回读持久日志。日志文件响应包含 `filename`、`path`、`log_id`、大小和创建时间，日志列表关联任务名称。

本地依赖以类型、规范包名和运行时版本作为安装身份。并发任务共享同一安装操作；Python 用户依赖位于运行时目录之外，npm 与 pip 使用共享目录和有界缓存。Python、Node 和依赖安装优先进入 rootfs，Android/Bionic runtime 提供快速兼容路径。fallback 使用固定并发 2、队列 32，并限制内存日志窗口。

Shell planner 对 `.sh`、`.bash`、task command 和 hook 执行保守 Bashism 扫描；POSIX 输入使用 rootfs `/bin/sh`，Bash shebang、数组、双中括号、process substitution 等语法使用 rootfs `/bin/bash`。Go Core 与 Kotlin fallback 暴露同一 PTY session API，支持输入、base64 原始输出块、resize、停止、会话限额和进程组回收。

依赖 spec 保留请求版本并按规范包名查重。显式 Python runtime 请求只安装目标版本；精确 pip/npm 版本必须与已安装版本一致。Go 与 Kotlin tokenizer 保留显式空 argv，多账号环境变量使用带转义的无损 split/join 契约，平台通知凭据与 runtime 路径禁止被数据库变量覆盖。

### Background Idle Policy

Flutter 本地连接 monitor 仅在前台执行 30 秒 reconcile，进入后台后取消 Timer，恢复时立即复核。Kotlin fallback scheduler 启动时检查当前分钟，随后按分钟边界唤醒。WorkManager 通过 `Configuration.Provider` 按需初始化，周期恢复仅在持续调度开启时注册。持续调度 Foreground Service 与原生任务通知行为保持不变。

非持续模式的完整 Core idle shutdown 仍需要持久化 next-run、系统单次唤醒和任务完成回执；当前版本保留 Core 调度以避免后台定时任务丢失。

### Instance-Aware Settings

managed-local 系统页展示动态 API endpoint、Core 状态、Android `:panel` 管理方式、前台服务、调度保障、恢复触发、可用内存和 Python runtime。APK 本地实例将后端自更新与 runtime mutation 声明为 unsupported，本地 Core 重启通过 MethodChannel 完成并采用返回的新 endpoint。服务器实例继续展示后端更新与 service manager 信息。

### Android Compatibility Modes

Android 7-15 ARM64 优先启动完整 Go Core；Android 16 使用 Kotlin fallback 并分别探测 Python、Node、Shell 和依赖安装能力。ARM32 与 x86_64 多架构预研已搁置到 `shelved/multi-arch-runtime` 分支，主分支不进入相关发布矩阵。fallback 对 exact cron 明确报告 unsupported，持续调度依赖 Foreground Service 与 WorkManager 恢复。

Node runtime 初始化只缓存成功结果，临时解包或 I/O 失败可在后续安装操作重试。用户主动发起的 npm 依赖安装启用 lifecycle scripts，并关闭 audit、fund 和 update notifier；本地 tgz、URL 和 alias 通过安装前后顶层依赖变化验证。Node launcher 由 CI 使用 16 KB linker page-size 参数重新生成，并作为 preBuild runtime 门禁输入。

Release 生成 ARM64 完整版 APK。主分支通过 ABI filter 固定 `arm64-v8a`，确保发布链路聚焦当前稳定 runtime。

### Upstream Capability Parity

`panel/server` 是 upstream 呆呆面板业务能力的移动端同源实现。ARM64 在 `:panel` 进程直接启动完整 Go Core，业务 Handler、Scheduler、Executor、SecretStore、Backup、Notification 和 Open API 与服务器项目共享代码。

ARM64 runtime 通过 Alpine rootfs 首选执行 Python、Node、Shell、Git 和 SSH，通过 Android/Bionic Python、Node 与 TypeScript runtime 提供快速兼容路径，通过 Yaegi 和独立 Go Builder 承载 Go 能力。多 ABI runtime preview 保留在 `shelved/multi-arch-runtime` 分支。

`/api/local/capabilities` 返回 `backend_parity`、`native_runtime_mode`、`recommended_execution_mode` 和逐功能 capability。Flutter 根据 capability 呈现真实可用操作，避免用设备架构推断业务能力。

本地 Web 只能通过 Android Host 生成的动态 `/local-ui/#ticket=...` URL 进入。Vite 与 Vue Router 共享 `/local-ui/` base；一次性 ticket、browser session Cookie、精确 Host/Origin 与用户 token 共同构成回环浏览器安全边界。

脚本环境内置 Python `notify.py`、CommonJS `sendNotify.js` 与同时支持 CommonJS/ESM 的 `notify` package。Android Go Core 使用临时业务 JWT、动态 Origin 和独立 local token 调用通知 API；fallback 采用相同环境变量契约。

### Backup Interoperability

Flutter 备份页使用 Android SAF 的 unrestricted picker，再按完整文件名接受 `.json`、`.enc`、`.tgz` 和 `.tar.gz`。该策略避免系统文件提供器将未知 MIME 的 tgz/enc 隐藏。Go Core 与 Kotlin fallback 统一生成和消费 `daidai-panel-backup` 0.4.0 canonical manifest；fallback 可生成 `.tgz` 和 Go AES-GCM `.enc`，并完整映射配置、任务、环境变量、订阅、SSH Key、通知渠道、依赖、任务日志、Task View 和脚本。恢复通过通知渠道、SSH Key 与任务 ID 映射回填外键，目标设备用户、会话、2FA 和设备绑定凭据保持现状。

### Notification Channels

Go notification registry 是渠道类型和字段 schema 的权威来源。Flutter 动态表单解析 `widget`、`default`、`show_when` 与 `push_scope`，条件字段只在命中消息类型时参与展示和必填校验。脱敏配置使用 `********` 占位，Go 更新接口逐字段保留原凭据。

Kotlin fallback schema 14 持久化任务的成功、失败、终止通知开关和绑定渠道 ID。任务终态统一经过通知 dispatcher：绑定任务只发送目标渠道，未绑定任务发送 enabled default 渠道。fallback 支持 Android local、Webhook、Telegram、钉钉、飞书、Bark、PushPlus、Server酱、PushDeer、Discord、Slack、ntfy、Gotify 与 WxPusher，并验证供应商业务响应。`pludplus` 历史拼写会迁移为标准 `pushplus`；Go PushPlus 使用 HTTPS。

删除通知渠道时，Go 与 fallback 在同一事务中清空任务绑定，避免悬空渠道 ID。任务进入 Go `OnTaskFailed` 前置失败路径时也会触发 failure 通知。

### Loopback Web Access

Android 构建将 Panel Web 以 `/local-ui/` base 打包到 APK。Go Core 与 Kotlin fallback 仅在动态 `127.0.0.1` 端口托管静态资源。App 生成 30 秒单次票据并放入 URL fragment；页面通过 POST 兑换 15 分钟 `HttpOnly`、`SameSite=Strict` Cookie。Cookie 仅替代本地传输层 Token，业务 API 继续执行 JWT 与角色授权。静态响应启用 CSP、frame deny、no-referrer 与 nosniff。

## 数据流

```mermaid
sequenceDiagram
    participant U as User
    participant F as Flutter
    participant A as AndroidHost
    participant C as GoCore
    participant R as Runtime
    U->>F: OpenApp
    F->>A: EnsureLocalInstance
    A->>C: StartWithPrivatePaths
    C-->>A: ReadyEndpoint
    A-->>F: EndpointAndCredential
    F->>C: APIRequest
    C->>R: ExecuteTask
    R-->>C: OutputAndExitCode
    C-->>F: SSEAndPersistedResult
```

## 发布结构

- 单一版本源为 `VERSION.json`，当前版本为 `1.0.14`、Android version code 为 `1000140`；`scripts/version.py` 校验派生版本字段。
- Modern APK：`minSdk 24`、`compileSdk 36`、`targetSdk 35`。当前管理 Core 已接线，完整受控运行时交付仍受真实资产和设备 smoke 门禁约束。
- Legacy APK：`minSdk 26`、`compileSdk 35`、`targetSdk 28`，增加私有目录 ELF 执行能力以承载实验性的 `go run`、`go test` 和 `go build`，支持 Android 8 至 Android 15 的已验证设备。
- 每个 Release 分配十位版本代码区间：Modern 使用 `releaseBase + 0`，Legacy 使用 `releaseBase + 1`，基于上一稳定源码构建的 Recovery APK 使用 `releaseBase + 2`。Modern 与 Legacy 通过可移植备份切换；Recovery APK 以更高版本代码覆盖当前故障版本并恢复迁移快照。
- Legacy 轨道每次发布均验证最新 Android 稳定版的安装能力；系统提高最低可安装 target SDK 后，该轨道停止支持受影响系统版本。
- 当前 CI 生成 Modern APK、SHA-256、更新清单和 release evidence。Recovery APK 的独立构建、签名、上传与升级验证仍待实现和取证。

## CI 门禁

1. 读取并校验 `VERSION.json`。
2. tag 路径先校验发布签名 secrets、准备 keystore，并要求同 commit 成功的 device smoke workflow，核验三个 matrix ID 各含八项 pass。
3. 准备 runtime，执行 Go 与契约脚本检查，校验 route contract。
4. 构建并检查 mobile Core AAR，随后执行 Flutter 与 Kotlin 检查。
5. snapshot 分支生成本地 smoke evidence；两条路径均在 APK 构建前后校验 runtime contract，tag 增加 `--strict`。
6. 构建 APK、提取并比对元数据、生成 hash、更新清单和 release evidence。tag 继续校验签名并发布；snapshot 仅上传 CI 产物，可保留 blocked evidence。

当前仓库记录未证明 Kotlin、Flutter、emulator、physical device 或真实设备矩阵已经通过。

## 安全边界

- Core 监听 `127.0.0.1` 动态端口。
- 每次安装生成本地授权密钥，API 校验 Host、Origin 和本地授权头。
- 工作目录经过规范化并限制在 App 私有空间或用户授权目录。
- 进程代理接受结构化程序 ID、argv、环境和工作目录。
- pip 接受纯 Python wheel、原生 wheel 和源码发行包；用户主动发起的 npm 安装启用 lifecycle scripts。
- 日志和诊断包对 Token、Cookie、环境变量值和私钥执行脱敏。
