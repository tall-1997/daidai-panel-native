# 原生客户端能力缺口分析（对照后端面板全部功能）

- 日期：2026-09-15
- 基线：`main@45d6916`（feat(android): close panel capability gaps across native modules）
- 对照面：`panel/server/handler/*`（18 个 handler，Go 服务端）+ `LocalPanelStore.kt`/`LocalPanelHttpServer.kt`（Android 内嵌面板）↔ `app/android/.../data/repository/*` + `ui/screens/*`（Compose 客户端）
- 结论：核心日常功能覆盖约七成；服务端「高阶/管理面」存在成体系缺口，其中大部分**本地服务器端已实现、仅缺客户端**。

## 一、已完整对齐（无需处理）

认证（登录/初始化/refresh/登出/自动登录/TOTP）、任务基础 CRUD+运行+启停+timeout/retry/stop-at/依赖/hooks/并发/cron 预设、环境变量全套（分组/排序/置顶/批量/青龙导入导出/掩码）、用户管理+重置密码、安全（登录/审计日志、会话下线、2FA 状态、IP 白名单）、脚本树/内容/运行/停止、依赖安装/卸载/重装/pip/npm、订阅 CRUD+拉取+认证、通知 22 渠道 schema+测试发送+推送偏好、备份/健康检查、OpenAPI 列表/创建/启停/重置密钥、个人中心登出。

## 二、缺口清单

### A. 整模块缺失（服务端就绪、客户端零代码）

| # | 模块 | 服务端位置 | 说明 |
|---|------|-----------|------|
| A1 | 平台令牌 | `platform_token.go`（GET/POST /platforms、令牌 CRUD/启停）、LocalPanelStore 已实现 | 客户端无仓库无 UI |
| A2 | 终端 PTY UI | `terminal.go`（sessions/input/resize/stop）、LocalPanelStore 11 处 | Android 无终端屏幕 |
| A3 | SSH 密钥管理页 | `ssh_key.go` CRUD、LocalPanelStore 已实现 | `Routes.SECURITY_SSH_KEYS` 为死路由；订阅 SSH 认证无处管理密钥 |

### B. 任务高阶能力（服务端 30+ 端点，客户端仅 CRUD/run/启停）

| # | 能力 | 端点 |
|---|------|------|
| B1 | 置顶/复制 | `PUT /:id/pin|unpin`、`POST /:id/copy`（本地服务器已实现） |
| B2 | 批量操作 | `POST /batch/run`、`PUT /batch/enable|disable|add-labels` |
| B3 | 任务视图 | `PUT/DELETE /views/:viewId` |
| B4 | 日志文件管理 | `GET/DELETE /:id/log-files[/:filename]`、下载、raw-ticket |
| B5 | 实时日志 | `GET /:id/live-logs`、`GET /:id/latest-log` |
| B6 | 统计/其他 | `GET /:id/stats`、`PUT /:id/restore-subscription-default`、`POST /batch/run` |

### C. 其他模块高阶能力

| # | 模块 | 缺口 |
|---|------|------|
| C1 | 日志 | 单条详情 `GET /:id`、原文 `raw`、SSE `stream`、`raw-ticket` |
| C2 | 脚本 | 运行历史 `run/:run_id/logs`、删除运行、版本 `versions/:id` + rollback |
| C3 | 依赖 | 单任务 `status/log-stream/cancel`、批量重装/删除、`export`、镜像源 GET/PUT |
| C4 | 订阅 | `pull-stream` 实时日志、`GET /:id/logs`、`pull/stop` |
| C5 | 通知 | 渠道 CRUD `PUT/DELETE /:id`、启停、单渠道 `POST /:id/test` |
| C6 | OpenAPI | `view-secret`（事后查看密钥）、`GET /apps/:id/logs` |
| C7 | 认证/个人 | 改密码/用户名（`PUT /api/auth/password|username`）、头像上传/删除、登录验证码（captcha-config + GeeTest） |
| C8 | 系统管理 | panel-settings、config-script、machine-code、panel-log 查看、备份上传/下载、restore-progress |
| C9 | 设置页 | ABOUT / PANEL_SETTINGS 死入口 |

### D. 合理差异（不算缺口）

- `POST /system/update` → Android 按语义返回 `immutable_apk`（平台安装器负责更新）
- `android_runtime` install/uninstall 面向远端管理场景，Android 本机即运行时

## 三、实施批次

- **第一批（本批）**：A3 SSH 密钥页；C7 改密码/用户名 + 头像；C6 OpenAPI view-secret + 调用日志；C5 通知渠道 CRUD + 单渠道测试；B1+B2(部分) 任务 pin/copy/批量运行
- 第二批：B4/B5、C1、C4、C3（流式与日志文件体验）
- 第三批：A1、A2、C2、C8、B3、C9（管理面与终端）

## 四、复核方式

每次以 `handler/<mod>.go` 路由 + `LocalPanelStore.kt` 分发为契约源，逐端点核对客户端实现；UI 层核对导航可达性与确认对话框；编译与单测以 CI（verify-build + x86 模拟器 smoke）为准。
