# Android Modern 全功能本机面板需求

## 简介

Android Modern 全功能本机面板面向普通非 Root ARM64 设备。用户安装一个 `targetSdk 35` APK 后，可离线创建本地面板，使用后端管理功能和预置脚本运行时，无需 Docker、Termux、Magisk、服务器、首次运行时下载或手工配置本地路径。

外部服务凭据遵循按需配置原则：公开 Git 与本地功能安装即用；私有 Git、Telegram、企业微信等外部服务仅在用户主动启用时要求对应服务凭据。

## 术语

- **Modern APK**：`targetSdk 35`、`minSdk 28`、仅包含 `arm64-v8a` 的正式安装包。
- **完整管理功能**：上游面板的任务、日志、脚本、环境变量、订阅、通知、SSH、用户、安全、系统、OpenAPI、依赖、配置和平台 Token 管理。
- **预置运行时**：随 APK 签名交付的 Python、Node.js、TypeScript、受控 Shell、Git、SSH、Yaegi 和 Go 构建工具。
- **兼容清单**：经 Android ARM64、Bionic、运行时 ABI 和 16 KB page size 验证的原生扩展集合。
- **本地零配置**：安装后不要求用户提供本地端口、目录、运行时路径、数据库、容器或服务器配置。

## Requirement 1：单 APK 安装即用

**User Story:** 作为 Android 用户，我希望只安装一个 APK，以便直接创建和使用本地面板。

1. WHEN 用户首次启动 Modern APK, App SHALL 自动创建数据目录、数据库、授权密钥、运行时目录和管理员初始化流程。
2. WHEN 本地 Core 启动, App SHALL 自动发现动态回环端点并建立安全会话。
3. WHEN 设备处于离线状态, App SHALL 完成 Core 与预置运行时健康检查。
4. IF 任一组件健康检查失败, App SHALL 展示组件、阶段、错误码和恢复操作。

## Requirement 2：完整管理 API

**User Story:** 作为管理员，我希望本地模式覆盖后端全部管理模块，以便无需远程面板。

1. Local Core SHALL 提供任务、日志、脚本、环境变量和任务视图 API。
2. Local Core SHALL 提供订阅、通知、SSH Key、用户、安全和 OpenAPI API。
3. Local Core SHALL 提供依赖、配置、平台 Token、备份、健康检查和系统信息 API。
4. WHEN 服务器专属功能被调用, Local Core SHALL 返回稳定的平台能力响应并提供 Android 等价操作。

## Requirement 3：离线基础运行时

**User Story:** 作为脚本用户，我希望安装后立即执行常用语言脚本。

1. Modern APK SHALL 内置 Android ARM64/Bionic Python、Node.js、TypeScript、受控 Shell、Git 和 SSH。
2. Modern APK SHALL 内置 Yaegi 解释器、Go 构建导出工具和 SSH Transport。
3. WHEN App 首次启动, Runtime Manager SHALL 对每个运行时执行离线 smoke test并记录版本、摘要和结果。
4. IF 运行时资产摘要或签名不匹配, Runtime Manager SHALL 隔离对应资产并阻止任务调用。

## Requirement 4：任务执行与日志

**User Story:** 作为任务用户，我希望手动和定时执行脚本并查看实时日志。

1. WHEN 用户执行 Python、Node.js、TypeScript、Shell 或 Go Interpret 任务, Executor SHALL 记录状态、输出、退出码、耗时和触发来源。
2. WHEN 用户停止任务, Process Supervisor SHALL 回收受管进程树并写入唯一终态。
3. WHEN 用户订阅实时日志, Local Core SHALL 提供可重连 SSE 和持久化日志游标。
4. WHEN Go Build 任务完成, Executor SHALL 生成可导出产物、构建日志、大小和 SHA-256。

## Requirement 5：依赖管理

**User Story:** 作为脚本作者，我希望从 App 管理 Python 和 Node.js 依赖。

1. Dependency Manager SHALL 支持纯 Python wheel 和兼容清单内 Android wheel。
2. Dependency Manager SHALL 支持纯 JavaScript、WASM 和兼容清单内 Node addon。
3. Dependency Manager SHALL 默认关闭 npm lifecycle scripts并校验包来源、摘要和空间配额。
4. IF 依赖超出兼容清单, App SHALL 展示包名、ABI 冲突和可用替代方案。

## Requirement 6：Git 与订阅

**User Story:** 作为订阅用户，我希望在手机本地同步脚本仓库。

1. Git Provider SHALL 支持公开 HTTPS clone、fetch、checkout、reset 和 sparse checkout。
2. WHERE 用户配置私有仓库凭据, Git Provider SHALL 支持 Token 或 SSH Key 鉴权。
3. WHEN 订阅更新工作区, Subscription Service SHALL 使用 staging 和原子切换保留上一个健康版本。
4. IF Host Key 发生变化, Git Provider SHALL 停止 SSH 操作并要求用户确认新指纹。

## Requirement 7：Cron 与后台恢复

**User Story:** 作为自动化用户，我希望任务在 Android 允许的后台窗口内可靠运行。

1. WHILE 用户启用持续调度, App SHALL 使用可见 Foreground Service 承载 Core、Scheduler 和活动任务。
2. WHEN 系统恢复 App 运行, Scheduler SHALL 在 15 分钟窗口内为每个任务的最近一次错过计划最多启动一次补偿并记录更早遗漏。
3. WHEN 设备重启, App SHALL 恢复调度状态并记录需要用户介入的系统限制。
4. App SHALL 展示前台持续、系统补偿和系统停止三种调度保障等级。

## Requirement 8：安全与敏感数据

**User Story:** 作为用户，我希望本地凭据和脚本数据受到移动平台保护。

1. Local Core SHALL 监听动态 `127.0.0.1` 端口并校验安装级 Token、Host 和 Origin。
2. Secret Store SHALL 加密环境变量、SSH 私钥、通知密钥、OpenAPI Secret、平台 Token 和 2FA Secret。
3. Executor SHALL 过滤运行时环境覆盖项并限制工作目录位于授权根目录。
4. Diagnostic Export SHALL 对 Token、Cookie、环境变量值、私钥和本地路径执行脱敏。
5. WHEN 用户首次执行或内容摘要发生变化的订阅脚本或原生扩展, App SHALL 告知授权代码可获得接近完整 App 私有数据读写权限并要求按来源与 SHA-256 重新授权。

## Requirement 9：备份、更新与恢复

**User Story:** 作为用户，我希望升级或故障后恢复完整本地面板。

1. BEFORE Schema 或运行时迁移, App SHALL 创建通过完整性校验的恢复快照。
2. WHEN 用户导出备份, App SHALL 写入 Schema、运行时清单、文件摘要和加密参数。
3. IF 新版本健康检查失败, Recovery Manager SHALL 在业务 worker 启动前恢复上一健康数据代际。
4. App SHALL 通过 Android Storage Access Framework 导入和导出可移植加密备份。

## Requirement 10：平台能力口径

**User Story:** 作为用户，我希望 App 清楚展示 Android 与 Linux 服务器的能力差异。

1. Modern APK SHALL 使用 Yaegi 替代原义 `go run` 和 `go test`。
2. Modern APK SHALL 将 Linux 系统包管理映射为签名 Android 兼容组件管理。
3. Modern APK SHALL 将 Docker、Watchtower 和 systemd 更新替换为 App 与 Core 协同更新。
4. Modern APK SHALL 对原生依赖、后台调度和外部凭据要求展示明确能力状态。

## Requirement 11：发布门禁

**User Story:** 作为用户，我希望下载经过完整验证的安装包。

1. Release Candidate SHALL 通过 Core、Flutter、Kotlin、AAR、APK 和 SHA-256 构建门禁。
2. Release Candidate SHALL 在断网条件下分别通过 Python、Node.js、TypeScript、Shell、Git、SSH、Yaegi 和 Go Builder 八类 smoke test。
3. Release Candidate SHALL 在 API 28、29、31、34 和 35 真机或等价设备矩阵通过安装与核心流程。
4. Release Candidate SHALL 通过 4 KB 与 16 KB page size 检查。
5. Release Candidate SHALL 通过 100 次 Core 启停、24 小时和 7 天调度测试。
6. Build Pipeline SHALL 发布 APK、SHA-256、SBOM、第三方许可、运行时清单和兼容矩阵。
7. Build Pipeline SHALL 为上游全部路由生成移动端追踪矩阵，且每个路由具有原样支持、Android 等价或稳定能力响应状态。
8. Release Candidate SHALL 达到 100 次 Core 启停零数据库损坏、零崩溃和零 ANR。
9. WHILE Foreground Service 持续健康, Scheduler SHALL 达到 99% 的实际启动任务在计划时间后 60 秒内启动，并单独统计策略跳过任务。
10. WHEN Android 授予恢复执行窗口, Scheduler SHALL 在窗口开始后 15 分钟内执行补偿或写入明确中断记录。
11. Release Candidate SHALL 为八类运行时分别记录 Runtime ID、版本、入口、隔离等级、smoke 输出、超时和失败状态。
