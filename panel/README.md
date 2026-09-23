<p align="center">
  <img src="./images/图标.png" alt="呆呆面板" width="120">
</p>

<h1 align="center">呆呆面板</h1>

<p align="center">
  <em>呆呆面板本机版的内置面板源码，安装包只从本仓库 Release 发布</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js&logoColor=white" alt="Vue3">
  <img src="https://img.shields.io/badge/Element%20Plus-2.x-409EFF?logo=element&logoColor=white" alt="Element Plus">
  <img src="https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white" alt="SQLite">
</p>

---

本目录是呆呆面板本机版仓库中的内置面板源码，版本与根目录 `VERSION.json` 一致，当前为 **v2.0.2**。产品介绍、安装包和更新说明以仓库根目录 README 为准。

本仓库不发布 Docker 镜像、Windows 安装包或 Magisk 模块。请只从 [tall-1997/daidai-panel-native Releases](https://github.com/tall-1997/daidai-panel-native/releases) 下载 Android 安装包。

## 功能特性

- **定时任务** — Cron 表达式调度，支持重试、超时、定时停止、任务依赖、前后置钩子
- **脚本管理** — 在线代码编辑器，支持 Python、Node.js（含 `.mjs`）、Shell、TypeScript、Go，拖拽移动文件
- **执行日志** — SSE 实时日志流，历史日志查看与自动清理
- **环境变量** — 分组管理、拖拽排序、批量导入导出（兼容青龙格式）
- **订阅管理** — 自动从 Git 仓库拉取脚本，支持定期同步
- **依赖管理** — 可视化安装/卸载 Python (pip) 和 Node.js (npm) 依赖
- **通知推送** — Bark、Telegram、Server酱、企业微信、钉钉、飞书等 18 种渠道
- **开放 API** — App Key / App Secret 认证，支持第三方系统对接
- **系统安全** — 双因素认证 (2FA)、IP 白名单、登录日志、多设备会话管理
- **数据备份** — 一键备份与恢复，支持每天/每周/每月定时备份
- **系统监控** — 实时 CPU / 内存 / 磁盘监控，任务执行趋势统计

<details>
<summary><b>点击展开查看详细功能</b></summary>

### 定时任务管理
- 标准 Cron 表达式调度
- 常用时间规则快捷选择
- 任务启用/禁用状态切换
- 手动触发执行
- 任务超时控制与重试机制
- 前后置钩子（任务依赖链）
- 多实例并发控制

### 脚本文件管理
- 在线代码编辑器（语法高亮）
- 支持创建、重命名、删除文件
- 支持文件上传与拖拽移动
- 脚本版本管理
- 调试运行与实时日志输出
- 支持 `.mjs` 脚本调试与任务执行

### 执行日志
- SSE 实时日志流
- 执行状态追踪（成功/失败/超时/手动终止）
- 执行耗时统计
- 日志自动清理策略

### 环境变量
- 安全存储敏感配置
- 变量值脱敏显示
- 分组管理与拖拽排序
- 批量导入导出（兼容青龙格式）
- 任务执行时自动注入

### 订阅管理
- Git 仓库自动拉取
- 定期同步（Cron 调度）
- SSH Key / Token 认证
- 白名单/黑名单过滤
  - 白名单不仅筛选任务，还会参与实际检出范围：只有命中白名单的文件会落盘，主脚本依赖的辅助文件需一并加入白名单；依赖说明仅备注、不参与检出。

### 消息推送
- 18 种主流推送渠道
- 任务执行结果通知
- 系统事件告警
- 自定义推送模板

### 系统设置
- 双因素认证 (2FA / TOTP)
- IP 白名单
- 登录日志与多设备会话管理（可配置网页端 / APP 端最大会话数）
- 数据备份与恢复（含视图数据）
- 定时备份（每天 / 每周 / 每月）
- 面板标题与图标自定义

</details>

## 效果图

<details>
<summary><b>点击展开查看界面截图</b></summary>

| 功能 | 截图 |
|------|------|
| 仪表盘 | ![仪表盘](./images/仪表盘.png) |
| 定时任务 | ![定时任务](./images/定时任务.png) |
| 执行日志 | ![执行日志](./images/执行日志.png) |
| 用户管理 | ![用户管理](./images/用户管理.png) |
| 脚本管理 | ![脚本管理](./images/脚本管理.png) |
| 环境变量 | ![环境变量](./images/环境变量.png) |
| 订阅管理 | ![订阅管理](./images/订阅管理.png) |
| 通知渠道 | ![通知渠道](./images/通知渠道.png) |
| Open API | ![Open API](./images/Open%20API.png) |
| 依赖管理 | ![依赖管理](./images/依赖管理.png) |
| 系统设置 | ![系统设置](./images/系统设置.png) |
| 个人设置 | ![个人设置](./images/个人设置.png) |

</details>

## 安装与发布

本目录是仓库内的面板源码，不是独立安装包。

本仓库不发布 Docker 镜像、Windows 安装包、Linux 二进制包或 Magisk 模块，也不提供其他仓库的下载地址。请只从 [tall-1997/daidai-panel-native Releases](https://github.com/tall-1997/daidai-panel-native/releases) 下载 Android 安装包。

产品介绍和更新说明见仓库根目录 [README.md](../README.md) 与 [CHANGELOG.md](../CHANGELOG.md)。
