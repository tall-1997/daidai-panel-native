# Changelog

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
