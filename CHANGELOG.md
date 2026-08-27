# Changelog

## v1.0.20 - 2026-08-27

### 修复

- SSE 可重试错误（408/425/429/5xx）不再误报为终态「连接错误」，仅触发重连，状态栏不再误导。
- README 与 runtime 依赖配置的发行版说明由 Alpine 更新为 Ubuntu 24.04。

## v1.0.19 - 2026-08-27

### 切换为 Ubuntu 唯一发行版

- 内置 Linux 环境由 Alpine(musl) 切换为 Ubuntu 24.04（noble，glibc），提供更广的二进制兼容性。
- rootfs 内置 bash、python3、pip、nodejs、npm、pnpm、git、curl、openssh-client、tzdata，包管理器使用 apt。
- 恢复 Ubuntu rootfs 制作脚本（qemu + chroot 产物），默认软件源为阿里云镜像。
- 前端依赖管理镜像预设恢复 Ubuntu apt 源。
- 删除 Alpine rootfs 资产与制作脚本。

注意：Ubuntu 使用 glibc，其 rseq 等新系统调用在 Android 应用域 seccomp 下曾导致 SIGSYS，本轮通过 `GLIBC_TUNABLES=glibc.pthread.rseq=0` 规避，仍需真机验证脚本执行稳定性。

## v1.0.18 - 2026-08-27

### 登录链路可靠性（浏览器面板）

- 修复登录 401 被误判为 token 过期进入刷新流程，导致「密码错误/账号锁定/两步验证」提示全部失效。
- 修复 refresh 无超时导致的 `isRefreshing` 死锁（登录转圈卡死到 30s Network Error 根因）。
- Network Error / 超时归一化为中文提示「网络异常，请检查连接」。
- checkInit 与版本检查加超时，不再无限卡「正在加载...」。
- 路由守卫瞬时网络错误不再强制登出用户。
- 登录防重复提交。
- 默认账号 `admin` / `admin123` 已在 README 标注。

### 后台定时任务可靠性

- Cron 任务按 ID 去重，队列满时延迟重入队，不再静默丢弃定时任务。
- 非持久化调度模式也通过 WorkManager 周期唤醒执行定时任务（作为持续调度的降级补充）。
- 定时备份改为异步执行，不再阻塞 cron 调度判断。
- 设备资源受限（电量/存储/内存/高温）时暂停低优先级备份并通知用户。

### 临时前台会话体验

- 浏览器面板临时会话不再随返回 App 自动结束，改为通知栏「关闭」按钮手动结束。
- 临时会话通知使用独立标题「呆呆面板浏览器面板」与「关闭」按钮，与持久化调度区分。

### 安全

- access_token 24 小时过期、refresh_token 轮换，泄露后不再长期有效。
- Open API 限流改用 datetime 比较并原子化，修复并发突破与凌晨计数偏差；日志记录真实状态码（成功 200 / 超限 429）。
- 头像上传增加 5MB 大小与 jpg/png/gif/webp 类型白名单。
- 畸形 JSON 请求返回 400 而非 500，且不再回显内部错误信息。
- 修复 userJson 硬编码 `admin` 导致的角色隔离失效。

### 健壮性与优化

- 前台面板轮询在健康状态下降到 5 分钟，失败自动回退高频。
- Dashboard 进度条 clamp 与生命周期感知，避免后台耗电与越界崩溃。
- 应用锁 40000 轮哈希移到后台 isolate，避免低端设备卡顿。
- 日志批量删除加页数上限，避免运行中任务持续产生日志导致死循环。
- 生物识别异常兜底，避免 `MissingPluginException` 红屏。
- SSE 短暂 inactive 不主动断开，避免下拉通知栏群涌重连。
- 数据库过期记录定期清理（调用日志 30 天、安全审计/登录日志 90 天、过期会话与 token）。
- 端口探测地址与监听地址统一为 `0.0.0.0`，修复探测不一致。
- 硬编码英文错误文案统一为中文。

## v1.0.17 - 2026-08-27

### 移除 Ubuntu 发行版

- 默认并唯一发行版改为 Alpine(musl)，删除 Ubuntu 发行版支持。
- 删除 Ubuntu rootfs 资产（约 77 MB）、Ubuntu 下载源、`prepare-android-ubuntu-rootfs.sh` 构建脚本与验证 contract。
- APK 体积约 227 MB 减小到约 150 MB。
- 前端「依赖管理」页删除 Ubuntu/Debian 镜像预设，`linux_mirror_supported` 仅对 Alpine(apk) 为 true。

### 本机 / 局域网浏览器面板访问

- 本地 HTTP 服务由 `127.0.0.1` 改为 `0.0.0.0` 监听，支持本机浏览器与局域网浏览器通过账号密码 + JWT 登录面板。
- 新增一次性票据（ticket）+ 浏览器会话（browser_session）机制，App 内安全拉起系统浏览器。
- App 内请求带 local-token，局域网浏览器强制 JWT，Host / Origin 按来源三分流校验。
- 登录失败锁定：`security_login_attempts` 表，同一来源 5 次失败锁定 15 分钟。
- 临时前台会话保活：打开浏览器时自动拉起 `specialUse` 前台服务，避免 MIUI 后台冻结导致面板端口不可达；返回 App 自动结束，不残留通知。
- 修复 keep-alive 连接空闲 5 秒被关闭导致浏览器登录请求失败（socket read timeout 5s → 60s）。

### Open API 完整实现

- 新增 `POST /api/open-api/token`：用 App Key + App Secret 换取 access_token（24 小时有效）。
- 业务 API 支持 open API token 鉴权（`Bearer`），按 scopes 权限范围默认拒绝，rate_limit 超限返回 429，调用写入日志。
- 修复创建 / 重置密钥弹窗的 App Key / App Secret 字段，列表补齐「今日调用」统计。
- 管理侧（`/api/open-api/apps`）仍仅允许面板登录 JWT，open API token 不能越权管理应用。

### Linux 运行时兼容

- 应用 proot/alpine 兼容技巧：apk-tools 2.x 静态版 + mkdir 并发锁、移除 `--link2symlink`、bind 系统目录（/apex、/system 等）。

### UI 整合

- 系统依赖清单并入依赖管理页，支持导出 Python/Node 与顺序批量重装。
- Android 运行时入口移入系统设置页；环境变量高级工具（批量改名/置顶）并入变量列表页。
- 修复高级配置脚本 GET 返回空内容的问题。

### 通知修复

- 强制覆盖过期的 sendNotify.js 副本，任务型脚本执行同样部署 notify shims。
- 本地通知请求允许缺失 Origin。

### 修复本地面板连接被拒绝的竞态

- `LocalPanelHostService` 启动失败不再静默吞异常，改为重试 3 次（延迟递增），并把失败状态回传给 UI。
- `ManagedLocalConnectionMonitor` 在 core 非 ready 时主动清空旧的本地会话，避免 UI 死抓已失效的回环地址与 token；binder 瞬时异常仍保留旧会话下次重试。

### Release 说明自动生成

- Release 说明改为从 `CHANGELOG.md` 动态提取对应版本的实际更新日志，不再使用硬编码模板。

## v1.0.16 - 2026-08-26

### 运行时崩溃修复：应用域 seccomp 导致脚本 SIGSYS

真机运行脚本与安装依赖时偶发 `SIGSYS`（signal 31）崩溃，根因是三层叠加：Android 应用域继承的 seccomp filter + glibc(Ubuntu) 二进制 + 固定注入的 `PROOT_NO_SECCOMP=1`，导致 guest 进程被 seccomp 拦截后直接被杀。

- 默认发行版由 Ubuntu 改回 Alpine(musl)，规避 glibc 对 rseq 等新系统调用的依赖。
- 移除固定注入的 `PROOT_NO_SECCOMP=1`，恢复 proot seccomp 加速（日志 `ptrace acceleration (seccomp mode 2) enabled`）。
- PRoot 由 proot-me 5.4.0 切换为 termux/proot 5.1.107.92 fork，新增 `--sysvipc` 与 `-L` 动态 loader，补齐 `libandroid-shmem.so` 与动态 `libtalloc.so`。
- Ubuntu 场景额外注入 `GLIBC_TUNABLES=glibc.pthread.rseq=0` 禁用 rseq。

### 移除嵌入式 Go Core

- 删除 Go Core 二进制桥接（GoCoreBridge / GoCoreReflectionContract / GoCoreResultMapper）与 `mobilecore.aar`，Kotlin fallback 成为唯一本地后端。
- 本地业务 API、任务、脚本、依赖、通知与健康检查全部由 Kotlin fallback 提供。

### 脚本日志实时输出

- 注入 `PYTHONUNBUFFERED=1` 与 `PYTHONIOENCODING=utf-8`，脚本 `print` 实时刷新，不再因管道块缓冲导致输出迟迟不显示。
- 移除 proot `-v 1` 诊断日志，脚本运行日志干净、实时。

### 依赖与运行时

- 中文脚本名 JSON body 解析改用 UTF-8，修复中文路径乱码。
- 新增 XZ 压缩 rootfs 解压支持（`org.tukaani:xz`）。
- 修复 Ubuntu rootfs 下 pnpm 检测路径（`/usr/local/bin/pnpm`）。
- 恢复华为云 / 阿里云 / npmmirror 默认镜像源。
- 健康诊断 `/api/system/health-check` 恢复 Python / Node.js / TypeScript runtime smoke 结果，并将误导性的 "Embedded Go core" 项改为 "Local panel core"。

### 内置 Linux 终端

- 删除 Termux 预编译 .so，改用 NDK 自编译 PRoot、loader 与 BusyBox，无需依赖 Termux。
- 修复 proot TLS 段 16 KB 对齐，解决 Android 15/16 设备加载失败。
- 新增 `/api/android-runtime/distribution` 端点，支持 alpine/ubuntu 发行版选择。

## v1.0.15 - 2026-08-23

### Rootfs Guest PATH

- 所有 PRoot 任务、依赖安装、健康检查和 PTY 统一注入 Linux guest `PATH`。
- 修复 npm 的 `#!/usr/bin/env node` 在 Android 宿主 PATH 下找不到 rootfs Node 的问题。
- Device Smoke 新增 `/usr/bin/npm --version` 门禁，直接覆盖 env shebang 启动链。

## v1.0.14 - 2026-08-23

### Android 依赖安装

- 移除 Android/Bionic 原生 wheel 阻断开关和 PyCryptodome 特判。
- pip 允许纯 Python wheel、原生 wheel和源码发行包，Go Core 与 Kotlin fallback 使用一致参数。
- 移除 Node 受限包名单，所有合法 npm 包均进入真实安装流程。
- 原生依赖首次失败后自动安装 rootfs C/C++、Python headers、Linux headers 和 Rust 工具链并重试一次。
- 保留结构化参数、包名校验、私有安装目录、超时、缓存上限和安装后导入验证。

## v1.0.13 - 2026-08-23

### Python Crypto 兼容

- Alpine rootfs 预装官方 `py3-pycryptodome`，直接支持 `Crypto.Cipher.AES` 和 `PKCS1_v1_5`。
- rootfs Python 自动依赖安装使用同一解释器和 `PYTHONPATH` 执行真实 import 验证。
- rootfs 模式不再套用 Android/Bionic 原生 wheel 阻断策略，也不再使用 Android Python 的 `pip list` 判断 rootfs 可导入性。
- Device Smoke 新增 PyCryptodome 双模块导入门禁。
- PRoot/BusyBox 构建默认验证已提交制品；显式刷新才访问固定上游包，避免 Termux 仓库滚动清理导致 CI 404。

## v1.0.12 - 2026-08-23

### Android PRoot Loader

- 将固定版本 PRoot 的 AArch64 helper loader 作为签名 APK ELF 交付，并通过 `PROOT_LOADER` 覆盖 Termux 绝对 loader 路径。
- Kotlin fallback、runtime health smoke 与 Go Core PTY 使用同一 PRoot loader 环境契约。
- rootfs 在 PRoot 或 helper loader 缺失时 fail-closed，避免任务进入含糊的 guest `execve` 失败。
- CI 校验 helper loader 的 SHA-256、AArch64、16 KB `PT_LOAD` alignment 和 manifest override；Device Smoke 直接执行 rootfs `/usr/bin/env`。

## v1.0.11 - 2026-08-22

### Android Linux Runtime

- Alpine rootfs 升级为首选执行层，内置 `apk`、Bash、Python 3、pip、Node.js、npm、uv、pnpm 和 CA 证书。
- Python、Node 和依赖自动安装优先使用 rootfs，Android/Bionic runtime 作为快速兼容路径。
- rootfs manifest 记录包、命令、能力、大小和 SHA-256；APK 升级后按资产摘要自动刷新已安装 rootfs。
- PRoot、BusyBox 和依赖固定 Termux 包版本与 SHA-256，并对 AArch64、动态依赖和 16 KB `PT_LOAD` alignment 执行 fail-closed 门禁。

### Shell 与终端

- 新增保守 Bashism 检测，识别 shebang、双中括号、数组、process substitution、brace expansion 等语法并自动路由 rootfs Bash。
- 脚本、普通 task command 和前后置 hook 统一使用 rootfs shell planner。
- Go Core 与 Kotlin fallback 新增 PTY session API，支持输入、增量原始输出、resize、停止、会话限额和进程组回收。
- 本地 Web 新增交互终端页面，并限制前后端输出保留量和页面离开后的会话生命周期。

### CI 与发行

- canonical mobile route contract 扩展到 449 条路由。
- CI 新增 rootfs 内容、manifest、PRoot provenance、原生依赖和 16 KB alignment 验证。
- Android 版本提升至 `1.0.11+1000110`。

### 架构范围

- 暂停 ARM32 与 x86_64 多架构 runtime 预研在主分支的集成。
- 将多架构预研代码保留到 `shelved/multi-arch-runtime` 分支。
- 主分支发布链路重新聚焦 ARM64 完整本地 Go Core 与运行时。

## v1.0.5 - 2026-08-22

### 多架构能力一致性

- ARM64 继续直接运行完整 Go Core，与 upstream 后端共享业务 Handler、Scheduler、Executor、SecretStore、通知、Open API 和备份实现。
- ARM32、x86_64 和 universal 的 fallback capability 响应按 upstream 功能域逐项声明任务、Cron、重试、超时、停止计划、依赖、钩子、脚本、日志、环境变量、订阅、通知、Open API、安全、备份和监控能力。
- 增加 `backend_parity`、`native_runtime_mode` 和 `recommended_execution_mode`，客户端可以准确区分完整本地执行、控制面和远程面板模式。
- 非 ARM64 架构明确标记 Git 仓库订阅、Open API token、2FA、多设备会话和 native runtime 状态，完整执行路径推荐连接远程呆呆面板。

### 文档与仓库

- README 重写多架构能力矩阵、平台边界、下载说明和构建命令。
- 修正 README 中过期的 ARM64-only 描述和旧 APK SHA。
- 更新项目 INDEX 的版本、fallback 状态、测试状态和 upstream 基线提交。
- 扩展 runtime compatibility，记录发行 ABI 和每种架构的产品 profile。
- 为 v1.0.4 补齐从 v1.0.1 至 v1.0.4 的完整更新日志。
- 更新 GitHub 仓库描述、主页与 topics，突出 Android、多架构、upstream parity、self-hosted 和 task scheduler。

## v1.0.4 - 2026-08-22

### Upstream 能力一致性

- ARM64 APK 直接运行仓库内完整 Go Core，覆盖 upstream 呆呆面板的任务、脚本、日志、环境变量、订阅、依赖、通知、Open API、安全、备份和监控业务域。
- Kotlin fallback capability 响应按业务域声明 parity，客户端可以区分控制面 API、Git 订阅、Open API token、2FA、多设备会话和本地 native runtime 能力。
- 非 ARM64 设备保留本地控制面、备份、通知、安全管理和远程面板连接，完整脚本执行可连接远程呆呆面板。

### 定时任务

- Flutter 多 Cron 表单改为完整换行表达式提交，避免只保存第一条规则。
- 增加定时停止 Cron 的模型、表单、序列化和 fallback 调度执行。
- Go API 增加 timeout、重试次数、重试间隔、停止计划、依赖存在性、自依赖和循环依赖校验。
- Go 执行器在依赖任务缺失时明确终止当前任务。
- Kotlin fallback 持久化并执行 timeout、max retries、retry interval、depends on 和 stop schedule。
- fallback 普通命令增加任务级超时；失败任务按配置重试。
- fallback Cron 支持多行表达式，仅调度 cron 类型任务，并独立匹配停止计划。
- fallback 终止日志统一使用状态码 2，保持成功、失败、终止统计一致。

### 环境变量与依赖

- fallback 环境变量导入支持 `merge` 和 `replace`，replace 使用数据库事务。
- 同名环境变量按 `name + remarks` 识别，兼容青龙多账号变量。
- fallback 单项依赖删除执行真实 pip/npm 卸载，失败时保留记录与日志。
- Open API 管理页增加 `backup` 和 `notifications` scope。

### 备份与监控

- Flutter 备份页增加每天、每周、每月定时备份配置。
- Kotlin fallback scheduler 消费同一套 `backup_schedule_*` 配置并生成 canonical 备份。
- Dashboard 前台每 10 秒刷新 CPU、内存、磁盘和任务数据。
- 趋势图按日期升序显示成功、失败和终止三条曲线。

### 多架构发行

- Go mobile AAR 改为构建 Android 全 ABI 输出。
- 新增 ARM64、ARM32、x86_64 专属 APK 和 universal 整合 APK。
- 架构专属 APK 通过 ABI filter 仅包含目标架构 native libraries。
- universal APK 包含 `armeabi-v7a`、`arm64-v8a` 和 `x86_64`。
- 每个 APK 独立执行正式签名验证并生成 SHA-256。
- `android-update.json` 默认指向 universal APK。
- Device Smoke 构建显式包含 ARM64 与 x86_64 ABI。

### 验证

- Go test、vet、关键包 race detector、Flutter test/analyze、Kotlin 单测、Panel Web build、route contract 和 runtime scripts 全部通过。
- 四类 snapshot APK 和四类正式签名 APK 均通过构建。
- Release evidence、更新清单和独立校验文件已发布。

## v1.0.3 - 2026-08-22

### 修复

- 修复 Android 16 Node runtime 初始化失败后无法重试。
- Node launcher 使用 16 KB page-size linker 参数重新构建。
- 修复 npm 本地 tgz、URL 和 alias 安装后的验证。
- 修复 Open API `app_secret` 查看、重置和 fallback 字段契约。
- Go Open API Secret 使用 SecretStore 可逆封存，历史摘要密钥引导重置。
- 新增 Python、CommonJS、ESM 三种形态的内置 `notify` helper。
- 修复 `/local-ui/` Router base 和 fallback 登录响应契约。
- x86_64 模拟器进入控制面降级模式，跳过 ARM64 runtime。

## v1.0.2 - 2026-08-21

### 备份互操作

- Go Core 与 Kotlin fallback 统一 `daidai-panel-backup 0.4.0` canonical manifest。
- 双向支持 `.tgz`、`.tar.gz`、`.json` 和 AES-GCM `.enc`。
- 覆盖配置、任务、环境变量、订阅、SSH Key、通知、依赖、任务日志、Task View 和脚本。
- 恢复时重映射通知渠道、SSH Key、任务依赖和日志任务 ID。
- 选择性恢复保留未选类别，目标设备用户、会话、2FA 和设备凭据保持现状。

## v1.0.1 - 2026-08-21

### 稳定发行

- 发布首个正式签名 ARM64 稳定版。
- 修复远程面板切回本地实例时的动态 endpoint 与授权恢复。
- 优化后台轮询、fallback 调度唤醒和 WorkManager 初始化。
- 完善依赖版本、空参数、多账号变量和 runtime 环境保护。
- 完成仓库 README、GitHub 介绍、topics 和 Release 流程整理。

## v0.4.4

### 修复

- 脚本调试页面直接显示明文 stdout、stderr 和完整 Traceback。
- 失败状态显示退出码和实际错误摘要，并提供高对比度“错误详情”区域。
- 日志接口新增 `content`、`error` 和 `log_count`，保留原始 `logs` 数组兼容性。
- 面板日志纳入最近脚本运行的真实进程输出。
- 修复 Android Python 子进程因错误 `Py_SetPath` 无法导入 `encodings`。
- 修复 Node wrapper 覆盖 `NODE_PATH`，使自动安装的 npm 包可被脚本加载。
- 新增保守型青龙脚本依赖扫描、常用 pip/npm 自动安装及配套文件缺失提示。

### 兼容性

- Python 常用导入映射支持 `yaml→pyyaml`、`bs4→beautifulsoup4`、`Crypto→pycryptodome`。
- Node 常见纯 JavaScript 包可自动安装；相对模块缺失返回 `MISSING_COMPANION_FILE`。
- 不支持 Android ARM64 的原生 Python 包返回 `ANDROID_WHEEL_UNAVAILABLE`，不再盲目源码编译。

## v0.4.3

### 修复

- 脚本运行改为异步启动，避免长脚本触发 Flutter 请求超时。
- Python、JavaScript、TypeScript 和 Shell 脚本运行期间实时增量显示 stdout/stderr。
- 停止脚本现在会终止实际进程，并返回 `stopped`、`exit_code=130`。
- 未保存代码运行会根据脚本扩展名发送正确语言类型。
- 修复脚本复制请求与 Go 后端的 `source_path/target_dir/new_name` 契约差异。
- 脚本重命名、移动和复制不再静默覆盖已有文件。
- 加强脚本路径校验，拒绝 `..`、系统绝对路径、盘符和反斜杠逃逸。
- 保留 APP 使用的 `/目录/脚本.py` 工作区根路径兼容性。
- 补充任务详情中的 `last_run_status`、`last_run_at` 和 `last_log_id`。

### 验证

- 用户提供的 `code_20260802-2.py` 在真机执行成功。
- pip 安装期间日志持续增量更新，最终 `exit_code=0`。
- JS/Python/Shell 导入、直接运行、任务运行及变量调用通过。
- Cron、Hooks、通知、依赖安装、备份恢复和管理 API 回归通过。
- Flutter tests 36/36 通过，Flutter analyze 零告警。
