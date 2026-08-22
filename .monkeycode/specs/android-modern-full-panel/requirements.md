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

## Requirement 12：统一任务运行语义

**User Story:** 作为本地面板用户，我希望 Go Core 与 Kotlin fallback 提供一致的任务行为，以便设备兼容路径变化时保持可预期结果。

1. WHEN 任一执行路径启动任务, Executor SHALL 向主命令、前置脚本和后置脚本注入已启用的面板环境变量。
2. WHEN 任一执行路径停止任务, Executor SHALL 终止对应进程树并持久化唯一终态。
3. WHEN 任一执行路径产生输出, Executor SHALL 增量持久化 stdout、stderr、游标和运行状态。
4. WHEN 客户端使用日志游标重连, Local Core SHALL 从游标后继续发送持久日志。
5. WHEN Python 或 Node.js 任务因缺失依赖失败且自动安装已启用, Dependency Manager SHALL 安装识别出的依赖并按有界次数重跑任务。

## Requirement 13：统一且可配置的镜像源

**User Story:** 作为本地面板用户，我希望使用默认国内镜像并可自由切换，以便在不同网络环境下安装依赖。

1. Local Runtime SHALL 默认使用 Huawei Alpine APK、Alibaba Python pip 和 npmmirror Node.js npm 镜像。
2. WHEN 管理员保存镜像设置, Local Runtime SHALL 将 pip、npm 和 APK 镜像持久化并应用于后续安装及任务环境。
3. WHEN 管理员选择任意有效 HTTP(S) 镜像或官方镜像, Local Runtime SHALL 保留管理员选择的地址。
4. WHEN rootfs 初始化或 App 重启, Local Runtime SHALL 保留已保存镜像并仅为缺失配置写入默认值。
5. Remote Panel SHALL 继续使用远程后端提供的镜像 API和配置语义。

## Requirement 14：后端版本与能力握手

**User Story:** 作为同时管理本地和远程面板的用户，我希望客户端识别实例版本与能力，以便只看到可用功能。

1. WHEN 客户端连接本地或远程面板, Panel SHALL 返回实例模式、后端版本、API 版本、Schema 版本和细粒度能力状态。
2. WHEN 旧版远程面板缺少握手接口, Client SHALL 使用端点探测生成 legacy capability profile。
3. WHEN 后端返回 `PLATFORM_CAPABILITY`, Client SHALL 更新当前实例能力并展示结构化原因。
4. WHEN 实例 ID、后端版本或 capability revision 变化, Client SHALL 失效旧能力缓存并重新握手。
5. Embedded Panel SHALL 记录固定上游 Release 与 commit，且构建检查 SHALL 验证记录与导入源码一致。

## Requirement 15：功能可见性与完整远程客户端

**User Story:** 作为面板用户，我希望同一 App 完整管理受支持的远程面板并隐藏不可用功能。

1. WHILE 远程面板声明功能可用, Client SHALL 提供与后端 API 对应的完整管理入口和操作。
2. IF 页面读取能力为 `unsupported`, Client SHALL 隐藏菜单入口并阻止深链进入。
3. IF 页面读取能力为 `disabled` 或 `temporaryUnavailable`, Client SHALL 显示页面状态与原因并禁用受影响操作。
4. IF 写入或执行能力不可用, Client SHALL 保留只读页面并隐藏或禁用对应操作按钮。
5. WHEN 用户切换本地与远程实例, Client SHALL 使用目标实例独立的角色、版本和能力配置刷新菜单、路由和操作。

## Requirement 16：实时任务反馈与日志一致性

**User Story:** 作为任务用户，我希望运行后立即看到脚本输出并可靠打开历史日志，以便判断任务执行进度和结果。

1. WHILE 任务处于排队或启动状态, Client SHALL 持续跟踪任务直到终态或用户离开页面。
2. WHEN 任务产生 stdout 或 stderr, Client SHALL 在下一次可用推送或增量轮询周期展示新输出。
3. WHEN 内存实时日志已释放, Local Core SHALL 从持久日志恢复最新输出、游标和终态。
4. WHEN 用户打开日志文件, Local Core SHALL 返回统一的文件名、路径、日志 ID、大小和创建时间字段。
5. WHEN 日志列表包含有效任务关联, Local Core SHALL 返回任务名称。

## Requirement 17：Unicode 脚本执行

**User Story:** 作为使用中文脚本名的用户，我希望任务命令、日志和下载完整保留脚本路径，以便正常执行与定位错误。

1. WHEN 任务命令包含引号、空格、中文路径或脚本参数, Executor SHALL 解析脚本路径和参数为独立结构化字段。
2. WHEN 路径经过 URL、数据库和日志文件接口, System SHALL 保持 UTF-8 内容和字面百分号语义。
3. IF 脚本路径无效, Client SHALL 展示规范化后的路径与可操作错误原因。

## Requirement 18：依赖复用与存储治理

**User Story:** 作为本地脚本用户，我希望多个脚本复用已安装依赖并限制缓存增长，以便减少存储和重复下载。

1. Dependency Manager SHALL 使用依赖类型、规范包名和运行时版本作为唯一身份。
2. WHEN 并发任务请求同一依赖, Dependency Manager SHALL 复用一个安装操作并共享结果。
3. WHEN 已安装版本满足请求, Dependency Manager SHALL 跳过网络安装。
4. Local Runtime SHALL 将 Python 用户依赖存储在运行时目录之外并向所有同版本脚本提供共享路径。
5. Local Runtime SHALL 对 pip/npm 缓存、任务日志、脚本运行日志、备份和临时文件执行有界保留与清理。

## Requirement 19：后台资源与本机浏览器访问

**User Story:** 作为本地面板用户，我希望后台任务可靠运行并能从设备浏览器打开面板，以便兼顾自动化和管理体验。

1. WHILE 持续调度启用, Android Host SHALL 使用有界任务并发、队列和日志内存窗口。
2. WHEN 持续调度关闭且客户端解绑, Android Host SHALL 停止 Core、子进程和周期恢复任务并释放资源。
3. WHEN App 停止或重启本地实例, Android Host SHALL 清除旧端点缓存并验证新端点。
4. Local Web SHALL 仅监听动态 `127.0.0.1` 端口并托管构建时签名打包的静态前端。
5. WHEN 用户从 App 请求浏览器访问, Local Web SHALL 使用短时一次性票据换取 HttpOnly、SameSite=Strict 的本机浏览器会话。

## Requirement 20：实例切换一致性

**User Story:** 作为同时使用本地与远程面板的用户，我希望切换实例后所有请求立即使用目标实例，以便可靠返回本地面板。

1. WHEN 用户切换到 managed-local 实例, Client SHALL 从 Android Host 获取实时 endpoint、instance ID 和 local token。
2. WHEN managed-local endpoint 发生变化, Client SHALL 原子更新活动实例、Dio session 和持久化 endpoint。
3. WHEN 用户切换实例, Client SHALL 失效旧认证 epoch、token、能力缓存和实例数据状态。
4. IF Android Host 缓存的 endpoint 已失效, Android Host SHALL 重新验证 Core 状态并返回新 endpoint。

## Requirement 21：后台空闲与调度保障

**User Story:** 作为 Android 用户，我希望 App 后台空闲时进入系统缓存状态，同时保留定时任务与通知能力。

1. WHILE App 位于前台, Client SHALL 保持本地连接监控和即时管理能力。
2. WHEN App 进入后台, Client SHALL 暂停 UI 连接轮询和页面级网络活动。
3. WHEN fallback scheduler 空闲, Scheduler SHALL 按分钟边界检查任务并避免秒级空转。
4. WHEN Android 创建调度恢复任务, App SHALL 使用已初始化的 WorkManager。
5. WHILE 持续调度启用, Foreground Service SHALL 保持任务和本地通知执行能力。

## Requirement 22：版本化依赖安装

**User Story:** 作为脚本作者，我希望安装脚本要求的准确依赖版本，以便脚本使用兼容 API。

1. WHEN Python 依赖请求包含 `python_version`, Dependency Manager SHALL 仅安装到目标运行时。
2. WHEN 依赖包含精确版本或范围, Dependency Manager SHALL 比较已安装版本并在约束不满足时执行安装。
3. Dependency Manager SHALL 使用规范包名执行查重、验证和卸载，并保留原始请求 spec。
4. IF 依赖 spec 以命令选项前缀开头, Dependency Manager SHALL 拒绝请求并返回参数错误。

## Requirement 23：脚本参数与变量同构

**User Story:** 作为自动化用户，我希望 Go Core 与 fallback 完整保留参数和变量，以便脚本在两条执行路径得到相同输入。

1. WHEN 命令包含显式空引号参数, Executor SHALL 在 argv 中保留对应空字符串位置。
2. WHEN 同名环境变量包含多个账号值, Executor SHALL 按稳定顺序无损合并空值、反斜杠和字面 `&`。
3. WHEN 用户变量名称属于平台保留集合, Executor SHALL 保留平台提供的通知凭据和运行时路径。
4. WHEN `desi` 或 `conc` 选择账号, Executor SHALL 使用统一的环境变量 split/join 契约。

## Requirement 24：按实例模式展示系统设置

**User Story:** 作为管理员，我希望系统页面仅显示当前实例可用的操作和完整状态，以便避免调用无效服务器功能。

1. WHILE 当前实例为 managed-local, Client SHALL 展示本地 endpoint、Android Core 管理方式和 runtime 摘要。
2. WHILE 当前实例为 managed-local, Client SHALL 隐藏后端自更新、systemd 服务和 runtime mutation 操作。
3. WHEN 用户重启 managed-local Core, Client SHALL 使用 Android Host lifecycle API 并采用返回的新 endpoint。
4. WHILE 当前实例为 remote, Client SHALL 保留后端更新和服务管理信息。

## Requirement 25：跨实例备份导入

**User Story:** 作为面板管理员，我希望把远程面板导出的备份导入本地实例，以便迁移任务和配置。

1. WHEN 用户选择备份文件, Client SHALL 允许 `.json`、`.enc`、`.tgz` 和 `.tar.gz` 文件进入内容校验流程。
2. IF 文件扩展名不在支持集合, Client SHALL 在上传前展示文件名与支持格式。
3. WHEN fallback 接收备份文件, Backup Service SHALL 在读取内容前执行文件大小限制。
4. Backup Service SHALL 使用 `daidai-panel-backup` 0.4.0 canonical manifest 导入和导出配置、任务、环境变量、订阅、SSH Key、依赖、任务日志、Task View 与脚本。
5. WHEN Go Core 或 Kotlin fallback 恢复对端备份, Backup Service SHALL 重映射通知渠道、SSH Key、任务依赖和任务日志外键。
6. WHEN manifest 明确提供 selection, Restore Service SHALL 仅替换选中类别并保留未选类别。
7. WHEN Kotlin fallback 创建带密码备份, Backup Service SHALL 生成与 Go Core 相同的 SHA-256 密钥派生 AES-GCM `.enc` 文件。
8. WHILE 恢复跨设备备份, Restore Service SHALL 保留目标设备用户、会话、2FA 和设备绑定凭据。

## Requirement 27：Android 运行时与本地 Web 兼容

**User Story:** 作为 Android 用户，我希望本地面板在现代系统、低版本设备和受限云环境中提供可预期能力。

1. WHEN Android 16 启动本地实例, Runtime Host SHALL 使用 Kotlin fallback 并独立探测 Node、Python 和本地执行能力。
2. WHEN Node runtime 初始化暂时失败, Runtime Host SHALL 保留失败原因并允许后续依赖安装重试。
3. WHEN npm 安装本地归档、URL 或 alias, Dependency Manager SHALL 根据安装前后顶层依赖变化验证安装结果。
4. WHEN 用户打开本地 Web, Browser Host SHALL 使用动态回环端口、`/local-ui/` Router base 和一次性 ticket。
5. WHEN fallback 登录成功, Auth API SHALL 返回与 Go Backend 一致的顶层 token 和 user 字段。
6. WHEN 用户查看 Open API Secret, Backend SHALL 在验证当前管理员密码后返回 `data.app_secret`。
7. Script Runtime SHALL 内置 Python、CommonJS 和 ESM 可解析的 `notify` helper。
8. WHILE ABI 为 x86_64, Runtime Host SHALL 使用控制面降级路径并跳过 ARM64 Go Core。

## Requirement 28：多架构发行

**User Story:** 作为不同 Android 架构的用户，我希望下载匹配设备的安装包，以便减少下载体积并获得准确的能力声明。

1. Release Pipeline SHALL 生成 ARM64、ARM32、x86_64 和 universal 四类签名 APK。
2. ARM64 APK SHALL 包含完整 Go Core、Python、Node、Shell 与 rootfs 运行时。
3. ARM32 和 x86_64 APK SHALL 仅包含目标架构 native libraries 并提供控制面降级能力。
4. Universal APK SHALL 包含 armeabi-v7a、arm64-v8a 和 x86_64 Flutter/Go ABI，并按设备 ABI 动态选择完整或降级模式。
5. Release Pipeline SHALL 为每个 APK 生成独立 SHA-256 并验证签名证书。

## Requirement 26：通知渠道与任务终态推送

**User Story:** 作为自动化用户，我希望任务结束后按配置渠道收到通知，以便及时了解执行结果。

1. Notification Registry SHALL 以 `pushplus` 作为 PushPlus 标准类型并兼容历史 `pludplus` 拼写。
2. WHEN 任务进入成功、失败或终止状态, Executor SHALL 按对应开关分派一次通知。
3. WHEN 任务绑定渠道, Dispatcher SHALL 仅发送到目标渠道；未绑定任务 SHALL 发送到 enabled default 渠道。
4. WHEN 客户端编辑脱敏凭据, Backend SHALL 保留原始凭据字段。
5. WHEN 通知字段包含 `show_when`, Client SHALL 仅展示和校验当前条件命中的字段。
6. Kotlin fallback SHALL 支持 Android local、Webhook、Telegram、钉钉、飞书、Bark、PushPlus、Server酱、PushDeer、Discord、Slack、ntfy、Gotify 和 WxPusher 渠道。
6. WHILE 浏览器会话有效, 业务 API SHALL 继续执行 JWT、角色和权限校验。
