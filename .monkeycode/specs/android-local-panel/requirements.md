# Android 本机面板需求

## 简介

Android 本机面板使用户安装所选发布轨道的单个自包含呆呆面板 APK 后，在普通非 Root ARM64 手机上离线创建本地面板，并使用内置运行时管理和执行自动化任务。产品同时保留远程面板连接能力。

## 术语

- **本地实例**：由 App 创建、启动和管理的 Go 面板 Core。
- **Modern APK**：使用现代 Android target SDK 的主发布产物。
- **Legacy APK**：提供实验性完整 Go 编译执行能力的兼容发布产物。
- **运行时基线**：随 APK 签名发布的 Python、Node.js、TypeScript、Shell、Git 和 Go 资产。
- **兼容清单**：经过 Android ARM64/Bionic 构建与验证的原生依赖集合。
- **持续运行模式**：用户启用前台服务后获得的增强调度能力。

## Requirement 1：安装即用

**User Story:** 作为首次安装用户，我希望 App 自动创建本地面板，以便直接开始管理任务。

1. WHEN 用户首次启动 App, App SHALL 校验运行时基线并创建私有数据目录。
2. WHEN 本地 Core 准备完成, App SHALL 引导用户创建管理员账号。
3. WHEN 管理员初始化完成, App SHALL 将本地实例设为默认实例。
4. IF 启动流程失败, App SHALL 展示失败阶段、诊断编号和可重复执行的恢复操作。

## Requirement 2：本地实例生命周期

**User Story:** 作为用户，我希望 App 自动管理面板服务，以便持续使用本地实例。

1. WHEN App 进入可交互状态, Android Host SHALL 确认 Core 健康状态。
2. IF Core 健康检查失败, Android Host SHALL 在 60 秒内最多重启 Core 3 次。
3. WHILE 持续运行模式启用, Android Host SHALL 使用可见 Foreground Service 承载 Core 和调度器。
4. WHEN 系统恢复 App 运行机会, Android Host SHALL 识别中断任务并记录恢复结果。

## Requirement 3：管理功能

**User Story:** 作为管理员，我希望本地模式覆盖面板管理功能，以便完成日常管理。

1. App SHALL 提供任务、日志、环境变量、脚本、订阅、通知和依赖管理。
2. App SHALL 提供用户、安全、备份、系统设置、任务视图和 Open API 管理。
3. WHEN Android 能力与上游服务器能力存在差异, App SHALL 展示能力状态、原因和可用替代操作。
4. App SHALL 保留现有远程实例连接和切换能力。

## Requirement 4：离线运行时

**User Story:** 作为自动化用户，我希望安装 APK 后即可离线运行常用脚本。

1. App SHALL 在 APK 中提供 ARM64/Bionic Python、Node.js、TypeScript、受控 Shell、Git 和 Go 运行时基线。
2. WHEN App 首次启动, App SHALL 在无网络条件下运行每个运行时的固定 smoke 脚本并校验标准输出与退出码。
3. WHEN 运行时校验失败, App SHALL 标记对应能力为故障并保留其他健康能力。
4. App SHALL 展示每个运行时的版本、磁盘占用、健康状态和 A、B、C 三级兼容等级。

## Requirement 5：脚本执行

**User Story:** 作为任务用户，我希望执行多语言脚本并获得一致的日志和状态。

1. WHEN 用户执行 Python、Node.js、TypeScript 或受控 Shell 任务, Execution Platform SHALL 持久化状态、输出、退出码、耗时和触发来源。
2. WHEN 用户停止任务, ProcessRunner SHALL 终止受管进程树并记录取消结果。
3. IF 任务超过资源或时间配额, ProcessRunner SHALL 结束任务并记录触发的限制。
4. WHEN Modern APK 执行 Go Interpret 任务, Execution Platform SHALL 使用 Yaegi 执行允许的标准库与预注册包并记录运行输出、错误和耗时。
5. WHEN Legacy APK 执行 Go 源码任务, Execution Platform SHALL 提供实验性的 `go run`、`go test` 和 `go build`。
6. WHEN Modern APK 执行 Go Build 任务, Execution Platform SHALL 使用 Go 工具链生成导出产物并记录构建输出、状态和产物摘要。

## Requirement 6：依赖与 Git

**User Story:** 作为脚本作者，我希望管理依赖和订阅，以便运行真实自动化项目。

1. WHEN 用户安装 Python 依赖, Dependency Manager SHALL 尝试安装纯 Python wheel、原生 wheel 和源码发行包，并返回包管理器的实际安装结果。
2. WHEN 用户安装 Node.js 依赖, Dependency Manager SHALL 启用 lifecycle scripts并尝试安装纯 JavaScript、WASM 和原生 addon。
3. WHEN 用户同步订阅, Git Provider SHALL 支持 HTTPS、SSH 密钥、known_hosts 和原子工作区更新。
4. IF 依赖超出兼容清单, App SHALL 展示包名、平台冲突和对应兼容清单申请文档地址。

## Requirement 7：调度与恢复

**User Story:** 作为任务用户，我希望了解任务调度保障并恢复错过的任务。

1. WHILE 持续运行模式启用, Scheduler SHALL 在 Foreground Service 生命周期内维护 Cron 计划。
2. WHEN Android Host 恢复运行, Scheduler SHALL 在 15 分钟恢复窗口内按每个任务最多一次的策略补偿错过任务。
3. WHEN 用户查看任务, App SHALL 展示当前调度保障等级和最近中断记录。
4. IF 设备进入低电量、过热或低存储状态, Scheduler SHALL 暂停低优先级任务并记录原因。

## Requirement 8：本地通信安全

**User Story:** 作为用户，我希望本地面板仅由当前 App 受控访问，以便保护敏感数据。

1. Go Core SHALL 监听动态 `127.0.0.1` 端口。
2. WHEN Flutter 请求本地 API, Go Core SHALL 校验安装级授权密钥、Host 和 Origin。
3. Android Host SHALL 使用 Keystore 保护本地授权密钥和备份主密钥。
4. App SHALL 对环境变量敏感值执行字段加密。

## Requirement 9：数据、备份与升级

**User Story:** 作为用户，我希望升级或故障后保留面板状态。

1. App SHALL 分区存储数据库、脚本、日志、依赖、运行时、备份和诊断数据。
2. BEFORE Schema 迁移, App SHALL 创建带完整性校验的恢复快照。
3. IF 升级健康检查失败, App SHALL 进入只读诊断状态并提供当前 Release 的 Recovery APK 安装入口。
4. WHEN 用户导出备份, App SHALL 写入 App 版本、Core 版本、Schema、文件摘要和运行时需求。
5. WHEN 用户在 Modern APK 与 Legacy APK 之间切换, App SHALL 通过可移植加密备份保持数据兼容。
6. WHEN 用户创建可移植备份, App SHALL 使用用户口令派生的密钥封装随机归档密钥。
7. WHEN Recovery Core 提交恢复数据, Recovery Core SHALL 使用持久化事务日志和单次原子活动指针替换。
8. IF 系统在恢复任一阶段中断, Recovery Core SHALL 在启动业务 worker 前幂等完成恢复或重新激活旧代际。
9. WHEN 活动数据代际被切出, Recovery Core SHALL 释放旧代际写租约并保留旧代际到新代际验证完成。

## Requirement 10：发布与上游同步

**User Story:** 作为维护者，我希望持续吸收上游改进，以便保持功能兼容和可维护性。

1. Build Pipeline SHALL 记录 Flutter 上游 commit、Go 上游 commit、运行时版本和 Schema 版本。
2. WHEN 上游更新被检测到, Build Pipeline SHALL 生成可审查的同步变更并运行契约测试。
3. WHEN GitHub Release 创建, Build Pipeline SHALL 发布 `daidai-panel-android-arm64-modern.apk`、`daidai-panel-android-arm64-legacy.apk`、`daidai-panel-android-arm64-recovery.apk`、SHA-256、签名指纹、SBOM、许可清单、版本清单和迁移说明。
4. IF API、SSE、迁移或运行时门禁失败, Build Pipeline SHALL 阻止发布产物进入稳定轨道。

## Requirement 11：质量门禁

**User Story:** 作为用户，我希望本机面板在真实 Android 环境中稳定运行。

1. Release Candidate SHALL 通过 100 次 Core 启停且数据库完整性检查全部成功。
2. Release Candidate SHALL 通过 Android 4 KB 与 16 KB page size 设备运行时测试。
3. Release Candidate SHALL 通过 7 天持续调度测试并生成漏执行、补偿和时间偏差报告。
4. Release Candidate SHALL 通过路径穿越、归档完整性、依赖来源和诊断脱敏测试。
5. Release Candidate SHALL 在断网条件下通过六类运行时基础执行测试。
6. Modern APK SHALL 小于或等于 500 MB，安装后只读基线 SHALL 小于或等于 1.5 GB。
7. WHEN 可用存储低于 APK 大小两倍加 1 GB, App SHALL 在升级或首次展开前阻止操作并展示所需空间。
8. WHILE Foreground Service 健康运行且系统未强制停止 App, 7 天调度测试 SHALL 达到 99% 的任务在计划时间后 60 秒内启动和 100% 的任务在恢复窗口内执行或产生明确中断记录。
9. Legacy APK SHALL 小于或等于 650 MB，安装后只读基线 SHALL 小于或等于 2 GB。
10. Release Candidate SHALL 通过 API 26、27、28 边界设备矩阵中适用轨道的安装、Core 启动、Foreground Service 和 Go 能力测试。
11. Release Candidate SHALL 通过恢复事务每个 I/O 边界的进程终止、断电、短写、空间耗尽、`fsync` 失败和 `rename` 失败注入测试。
