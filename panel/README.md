<p align="center">
  <img src="./images/图标.png" alt="呆呆面板" width="120">
</p>

<h1 align="center">呆呆面板</h1>

<p align="center">
  <em>轻量、现代的定时任务管理面板，Docker 一键部署，开箱即用</em>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/Vue-3-4FC08D?logo=vue.js&logoColor=white" alt="Vue3">
  <img src="https://img.shields.io/badge/Element%20Plus-2.x-409EFF?logo=element&logoColor=white" alt="Element Plus">
  <img src="https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white" alt="SQLite">
  <img src="https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white" alt="Docker">
</p>

---

呆呆面板 (Daidai Panel) 是一款轻量级定时任务管理平台，采用 Go (Gin) + Vue3 (Element Plus) + SQLite 架构，专注于脚本托管与自动化任务调度。支持 Python、Node.js（含 `.js` / `.mjs`）、Shell、TypeScript、Go 等多语言脚本的定时执行与可视化管理，内置 18 种消息推送渠道、订阅管理、环境变量、依赖管理、Open API 等功能。Docker 一键部署，开箱即用。

> 最新稳定版：`v2.3.9` · [更新日志](./docs/release-notes/v2.3.9.md)<br>
> 本次重点：修复企业微信 mpnews 图文正文不换行、窗口最小化被强制全屏、桌面版 Python 版本缺失自动回退、随机延迟误延迟手动执行，以及多设备登录后旧会话无限加载。<br>
> APP 客户端：[linzixuanzz/Dumb-Panel-APP](https://github.com/linzixuanzz/Dumb-Panel-APP)

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

## 快速部署

面板官方推荐用 Docker 部署。下面的例子默认浏览器访问 `http://宿主机IP:5700`。

### 一键启动（Alpine 运行时）

```yaml
# docker-compose.yml
name: daidai-panel

services:
  daidai-panel:
    image: linzixuanzz/daidai-panel:latest
    container_name: daidai-panel
    restart: unless-stopped
    ports:
      - "5700:5700"                                # 宿主机端口:容器内 Nginx 端口
    volumes:
      - ./Dumb-Panel:/app/Dumb-Panel               # 面板数据目录，升级保留
    environment:
      - TZ=Asia/Shanghai
      - CONTAINER_NAME=daidai-panel
      - IMAGE_NAME=linzixuanzz/daidai-panel:latest
      - PANEL_UPDATE_MANAGER=watchtower
    labels:
      - com.centurylinklabs.watchtower.enable=true

  watchtower:
    image: nickfedor/watchtower:latest
    container_name: daidai-watchtower
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    labels:
      - com.centurylinklabs.watchtower.enable=false
    command:
      - --label-enable
      - --cleanup
      - --interval
      - "3600"
```

```bash
docker compose up -d
```

首次访问 `http://localhost:5700` 会进入管理员初始化。

如果 Docker Hub 访问慢，可以把 `image` 和 `IMAGE_NAME` 改成你自己信任的镜像加速地址；README 默认不再内置固定第三方镜像源。也可以到 [容器镜像监控](https://status.anye.xyz/) 查看更多 Docker Hub 镜像加速源状态，再选择可用地址填写。

这份 compose 已经是推荐的可直接上线版本：

1. 面板容器只挂业务数据目录 `./Dumb-Panel:/app/Dumb-Panel`
2. `docker.sock` 只暴露给 Watchtower，不暴露给面板容器
3. 只有打了 `com.centurylinklabs.watchtower.enable=true` 标签的容器会被自动更新
4. Watchtower 自己显式打了 `com.centurylinklabs.watchtower.enable=false`，避免被这套规则误纳入管理
5. `--cleanup` 会在更新后清理旧镜像
6. `--interval 3600` 表示每 1 小时检查一次更新
7. 当前默认使用 `nickfedor/watchtower:latest`，用于兼容新版 Docker API

如果你不想自动更新，可以删除 `watchtower` 服务、`labels` 和 `PANEL_UPDATE_MANAGER=watchtower`，然后改成在宿主机手动执行：

```bash
docker compose pull
docker compose up -d
```

想用 `docker run` 而不是 compose，推荐等价方式是分别启动面板容器和 Watchtower 容器：

```bash
docker run -d --pull=always \
  --name daidai-panel \
  --restart unless-stopped \
  -p 5700:5700 \
  -v $(pwd)/Dumb-Panel:/app/Dumb-Panel \
  -e TZ=Asia/Shanghai \
  -e CONTAINER_NAME=daidai-panel \
  -e IMAGE_NAME=linzixuanzz/daidai-panel:latest \
  -e PANEL_UPDATE_MANAGER=watchtower \
  --label com.centurylinklabs.watchtower.enable=true \
  linzixuanzz/daidai-panel:latest

docker run -d \
  --name daidai-watchtower \
  --restart unless-stopped \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --label com.centurylinklabs.watchtower.enable=false \
  nickfedor/watchtower:latest \
  --label-enable \
  --cleanup \
  --interval 3600
```

### 支持的 CPU 架构

镜像是 multi-arch manifest list，`docker pull` 时按你机器自动选对应平台：

| 架构 | 典型机器 |
|------|---------|
| `linux/amd64` | x86_64 服务器、PC、绝大多数 NAS |
| `linux/arm64` | 树莓派 4 / 5、Oracle ARM 云、Apple Silicon |
| `linux/386` | **v2.0.9 新增**：32 位 x86 老 PC、瘦客户端（仅 `:latest` 有，`:debian` 无） |
| `linux/arm/v7` | **v2.0.9 新增**：树莓派 2 / 3 / Zero 2W、老 ARMv7 盒子 / 路由器 / NAS |

> Python 运行时说明：从 `v2.3.5` 起，默认 `:latest` / `:debian` 镜像只内置默认 Python `3.12`，镜像体积更小、升级后也会自动清理旧的 `3.10 / 3.11` 托管环境。需要指定小版本时请使用 `:latest3.10` / `:latest3.11` / `:debian3.10` / `:debian3.11`；确实需要三套 Python 同时存在时使用 `:latestall` / `:debianall`。

### Alpine vs Debian 运行时

面板提供两套运行时镜像，差别只在容器内的包管理器：

| Tag | 基础镜像 | Python 运行时 | Linux 包管理 | 支持架构 | 适合谁 |
|-----|---------|---------------|-------------|---------|--------|
| `linzixuanzz/daidai-panel:latest` / `:<版本>` | `alpine:3.22`（`apk` 安装 Node.js / npm） | 单版本 `3.12` | `apk` | amd64 / arm64 / 386 / arm/v7 | 默认推荐，绝大多数场景 |
| `linzixuanzz/daidai-panel:latest3.10` | `alpine:3.22` | 单版本 `3.10` | `apk` | amd64 / arm64 | 任务明确需要 Python 3.10 |
| `linzixuanzz/daidai-panel:latest3.11` | `alpine:3.22` | 单版本 `3.11` | `apk` | amd64 / arm64 | 任务明确需要 Python 3.11 |
| `linzixuanzz/daidai-panel:latestall` | `alpine:3.22` | `3.10 / 3.11 / 3.12` | `apk` | amd64 / arm64 | 需要同时维护多个 Python 小版本依赖环境 |
| `linzixuanzz/daidai-panel:debian` | `node:20.19.0-bookworm-slim` | 单版本 `3.12` | `apt` | amd64 / arm64 / arm/v7 | 需要安装只在 Debian/Ubuntu 仓库存在、`apk` 没打包的 Linux 软件 |
| `linzixuanzz/daidai-panel:debian3.10` | `node:20.19.0-bookworm-slim` | 单版本 `3.10` | `apt` | amd64 / arm64 / arm/v7 | Debian 运行时且任务明确需要 Python 3.10 |
| `linzixuanzz/daidai-panel:debian3.11` | `node:20.19.0-bookworm-slim` | 单版本 `3.11` | `apt` | amd64 / arm64 / arm/v7 | Debian 运行时且任务明确需要 Python 3.11 |
| `linzixuanzz/daidai-panel:debianall` | `node:20.19.0-bookworm-slim` | `3.10 / 3.11 / 3.12` | `apt` | amd64 / arm64 / arm/v7 | Debian 运行时且需要三个 Python 小版本共存 |

> 说明：`:latest` 从 v2.2.16 起使用 `alpine:3.22` 作为运行时底座，并通过 `apk` 安装 Alpine 官方仓库的 `nodejs/npm`。这样可以满足 `node >= 20.19.0` 的依赖要求，同时保留 Alpine `x86` 仓库支持，继续构建 `linux/386` 镜像。
> Python 架构说明：Alpine 的 `latest3.10` / `latest3.11` / `latestall` 依赖独立 Python 运行时资产，目前仅发布 `amd64 / arm64`；32 位 x86 和 ARMv7 继续使用默认 `latest`（Python 3.12）或切换到支持 ARMv7 的 Debian 变体。

切到 Debian 运行时：

```bash
# 仓库里有现成的 compose
docker compose -f docker-compose.debian.yml up -d

# 或基于源码本地构建
docker build --build-arg VERSION=2.2.20 -f Dockerfile.debian -t daidai-panel:debian-local .
```

如果要本地构建指定 Python 版本镜像，可以额外传入：

```bash
# 单版本 Python 3.10
docker build \
  --build-arg VERSION=2.2.20 \
  --build-arg PYTHON_RUNTIME_MODE=single \
  --build-arg PYTHON_RUNTIME_VERSION=3.10 \
  -t daidai-panel:latest3.10-local .

# 三版本合集
docker build \
  --build-arg VERSION=2.2.20 \
  --build-arg PYTHON_RUNTIME_MODE=all \
  --build-arg PYTHON_RUNTIME_VERSION=3.12 \
  -t daidai-panel:latestall-local .
```

### Windows 单机版（不走 Docker）

**v2.1.0 新增**：Windows 用户可以直接下载编译好的 zip 解压运行，面板内置 Go 后端同时托管前端（无需 Nginx / Docker）。

1. 去 [GitHub Release](https://github.com/linzixuanzz/daidai-panel/releases) 下载 `daidai-windows-amd64.zip` 解压到任意目录（建议路径无空格、无中文，例如 `D:\daidai-panel`）。
2. 双击 `start.bat` 启动服务。
3. 浏览器访问 `http://localhost:5700`，首次进入创建管理员账号。

> 注意：仓库源码目录中的本地 `server/*.exe` 仅用于开发阶段临时调试，不作为可信发布产物。  
> Windows 正式发布包请始终以 GitHub Release 中 workflow 构建出的 `daidai-windows-amd64.zip` 为准。

解压后目录：

```
daidai-panel-windows-amd64/
├── daidai-server.exe     # 后端主程序（同端口同时服务前端）
├── ddp.exe               # 运维 CLI
├── web/                  # 前端静态资源（Go 通过 web_dir 直接托管）
├── config.yaml           # 端口 / 数据目录配置
├── start.bat             # 启动脚本（chcp 65001 兜底中文显示）
├── README.txt            # 详细使用说明
└── Dumb-Panel/           # 首次启动时自动创建，含数据库 / 脚本 / 日志 / 备份
```

**可选：脚本执行环境**。如需面板调度 Python / Node.js 脚本，请自行安装 Python 3.10+ 和 Node.js 20 LTS 并勾选 "Add to PATH"，重启 `start.bat` 即可（`ddp.exe`、脚本执行器会从 PATH 找到对应的 `python` / `node`）。

**Python 多版本说明**：二进制部署包不会内置 Python 3.10 / 3.11 / 3.12 三个解释器，用户只需要安装实际要使用的版本。面板会为已检测到的 Python 版本创建独立依赖环境；未安装的版本会在依赖管理里提示不可用，不影响其他版本的脚本运行。Windows 建议安装官方 Python 并保留 `py` 启动器，Linux 需要确保 `python3.10` / `python3.11` / `python3.12` 能在 PATH 中被找到。

**升级**：优先在面板后台进入「系统设置」→「概览」→「检查系统更新」→「立即更新」。二进制后台更新会自动下载对应平台的 Release 包，替换程序与前端文件，并保留现有 `config.yaml`、`Dumb-Panel\`、`data\`、`logs\`、`backups\` 等本地配置和数据目录。只有在程序目录没有写入权限、网络无法访问 GitHub Release，或后台更新失败时，才需要手动下载新版 zip 后迁移数据。

### Android Magisk 模块（Root 手机）

在已 Root 的 Android 设备上直接跑面板，无需 Docker、无需 Termux。模块会在安装阶段下载一份 Alpine 3.18 minirootfs 到 `/data/daidai`，在容器里 `apk` 装好 Python / Node.js / Git 等运行时，然后通过 `rurima` 进入容器启动后端，开机自启。

- **支持**：Magisk v24.0+ / KernelSU / APatch；Android 8.0+；`arm64` 或 `x86_64`
- **默认访问**：`http://127.0.0.1:5700`，后端绑定 `0.0.0.0`，局域网 / 内网穿透可直连
- **一键更新**：模块 `updateJson` 自动推送新版 ZIP，升级保留数据
- **下载**：[GitHub Release](https://github.com/linzixuanzz/daidai-panel/releases) 里的 `daidai-panel-magisk-vX.Y.Z.zip`

> 📱 **完整的安装 / 升级 / 卸载 / 端口配置 / 排障文档请看 → [`Magisk/README.md`](./Magisk/README.md)**

## 端口与反向代理

### 端口三兄弟

面板在容器内有 **3 个端口**，搞清它们，大多数部署问题都会消失：

| 端口 | 由谁决定 | 默认 | 要不要改 |
|------|---------|------|----------|
| **宿主机端口** | docker `-p` 左侧 | `5700` | 常改 |
| **容器内 Nginx 端口** | 环境变量 `PANEL_PORT`，`-p` 右侧应与其一致 | `5700` | 基本不改 |
| **容器内 Go 后端端口** | 环境变量 `SERVER_PORT` | `5701` | **不要改** |

```mermaid
flowchart LR
    A[浏览器<br/>http://宿主机IP:宿主机端口]
    B[宿主机端口<br/>docker -p 左侧]
    C[容器内 Nginx<br/>PANEL_PORT 默认 5700]
    D[容器内 Go API<br/>固定 5701]

    A --> B --> C
    C -->|/api/* 反代| D
```

两条经验记住就够用：

1. **Docker 部署通常只改 `-p` 左侧**，右侧保持 `5700` 即可。
2. **宿主机 Nginx / 宝塔 / Caddy 反代的目标是宿主机端口**（比如 `127.0.0.1:5700`），**别直接代理到容器内 `5701`**——SSE 会断流、鉴权会丢。

### 想改端口

**只改宿主机端口**（最常见，比如让浏览器走 8080）：

```yaml
ports:
  - "8080:5700"
```

**连容器内 Nginx 端口一起改**（只在容器内 5700 和其他服务冲突时）：`-p` 右侧必须和 `PANEL_PORT` 一致，Go 后端 `5701` 不受影响。

```bash
docker run -d --name daidai-panel \
  -p 8080:7100 \
  -e PANEL_PORT=7100 \
  ...
```

### Magisk 模块（Android Root）改端口

Magisk 模块版和 Docker 结构不一样：没有容器内 Nginx，前端 / 后端都由单个 `daidai-server` 二进制在 `PANEL_PORT` 上直接托管，**不要**直接去改 `config.yaml`——每次开机 `service.sh` 都会按 `ports.conf` 重新生成 `config.yaml`，手动改的内容会被覆盖。

改端口的唯一入口是编辑持久化目录下的 `ports.conf`：

```bash
su
vi /data/adb/daidai-panel/ports.conf
```

> 首次安装模块时会自动生成这个文件，内容带注释，直接修改对应的值即可。

里面有三个可选变量：

| 变量 | 作用 | 默认 |
|------|------|------|
| `PANEL_PORT` | 浏览器访问面板的端口（绑定 `0.0.0.0`，本机 / 局域网 / 内网穿透都能连） | `5700` |
| `SSH_PORT` | 容器内 SSH 端口（adb / Termux 登入容器调试用） | `22` |
| `EXTRA_CORS_ORIGINS` | 额外 CORS 白名单，英文逗号分隔。仅在跨域场景需要（如内网穿透公网端口与面板端口不同，或自定义域名访问） | 空 |

示例：

```ini
PANEL_PORT=6700
SSH_PORT=2222
EXTRA_CORS_ORIGINS="https://panel.example.com,https://xx.trycloudflare.com"
```

改完后重启手机，或手动执行以下命令让配置立即生效：

```bash
su -c "sh /data/adb/modules/daidai-panel/service.sh"
```

生效后在 Magisk / KernelSU / APatch 管理器里点模块卡片的「运行」按钮，可以看到当前 `PANEL_PORT` / `SSH_PORT` 的实际监听状态。完整的 Magisk 模块安装 / 升级 / 卸载文档见 [`Magisk/README.md`](./Magisk/README.md)。

### 反向代理示例

最常见是 **宿主机 Nginx → Docker 已发布端口**。面板暴露在宿主机 `5700`，反代就指向那里：

<details>
<summary><b>宿主机 Nginx 示例（HTTPS，含 SSE 支持）</b></summary>

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    '' close;
}

server {
    listen 443 ssl http2;
    server_name your-domain.com;

    ssl_certificate     /path/to/fullchain.pem;
    ssl_certificate_key /path/to/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:5700;   # 宿主机端口，不是容器内 5701

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_buffering off;                 # SSE 日志流必须关
        proxy_read_timeout 300s;
    }
}
```

</details>

如果反代本身也跑在同一 Docker 网络里，可以直接代理到 `http://daidai-panel:5700`（依然是容器内 Nginx 端口）。

**别做的事**：

- 让浏览器或反代绕过容器内 Nginx 直接访问 Go 后端 `5701`
- 把 SSE / 下载 / 鉴权接口单独绕出去
- 让 `-p` 右侧容器端口和 `PANEL_PORT` 不一致

## 更新

### 面板内一键更新（推荐）

进入「系统设置」→「概览」→ 点「检查系统更新」。系统会自动识别当前部署方式：

- **Docker 部署**：推荐交给 Watchtower 自动拉取并重建容器；面板会识别 `PANEL_UPDATE_MANAGER=watchtower` 的托管状态。早期挂载 Docker Socket 的部署仍可继续使用面板内一键更新。
- **二进制部署**：自动匹配 `daidai-windows-amd64.zip` 或 `daidai-linux-*.tar.gz`，后台下载、解压、替换程序和 `web/` 前端文件，更新过程会跳过 `config.yaml` 与数据目录，避免覆盖服务器本地配置。

### 手动更新

```bash
# Alpine 运行时
docker pull linzixuanzz/daidai-panel:latest
docker compose up -d

# Debian 运行时
docker pull linzixuanzz/daidai-panel:debian
docker compose -f docker-compose.debian.yml up -d
```

如果你使用的是指定 Python 小版本镜像，把上面的 tag 替换成正在使用的版本，例如 `latest3.10`、`latest3.11`、`latestall`、`debian3.10`、`debian3.11` 或 `debianall`，并同步更新 compose 里的 `IMAGE_NAME`，这样面板内一键更新和 Watchtower 才会继续拉取同一条镜像线。

本地基于源码自己构建的镜像，重新 build 即可：

```bash
docker build --build-arg VERSION=2.2.20 -f Dockerfile.debian -t daidai-panel:debian-local .
```

## 容器命令 `ddp`

容器里预置了 `ddp` CLI，覆盖运维、脚本 / 变量 / 任务 / 订阅管理、账号恢复等场景。统一入口：

```bash
docker exec -it daidai-panel ddp <subcommand>
```

> 没叫 `dd` 是因为会和 Linux 自带 `dd` 命令冲突。

### 状态与自检

```bash
ddp help                 # 查看所有子命令
ddp status               # 版本、数据目录、端口、任务数、资源占用、服务状态
ddp check                # 检查配置、数据库、运行目录、运行时命令、Docker Socket
ddp logs --lines 200     # 查看 panel.log
```

### 脚本

```bash
ddp script list
ddp script cat demo.py
ddp script fetch https://example.com/test.py --path tools/test.py
```

### 环境变量

```bash
ddp env list
ddp env get JD_COOKIE
ddp env set JD_COOKIE "pt_key=xxx;pt_pin=yyy;" --group 京东
ddp env delete <id>
```

### 任务与订阅

```bash
ddp task list --status running
ddp task logs 12 --lines 80
ddp task run 12                 # 同步执行任务并实时输出
ddp task stop 12                # 终止运行中的任务

ddp sub list
ddp sub logs 3 --lines 100
ddp sub pull 我的订阅            # 立即执行一次订阅拉取
```

### 运维

```bash
ddp restart                     # 重启容器内 daidai-server 进程
ddp update                      # 复用面板一键更新链路
ddp clean-logs 7                # 清理 7 天前的任务日志文件
ddp backup create --name nightly
ddp backup list
ddp backup restore <name>
ddp backup delete <name>
```

### 账号恢复（忘了密码 / 用户名）

```bash
ddp list-users                              # 忘了用户名先看这个
ddp reset-password admin NewPass123         # 单用户时可省略用户名
ddp reset-username admin newadmin
ddp disable-2fa admin                       # 传 --all 则全员禁用
ddp reset-login --all                       # 清登录失败次数，解锁被锁账号
ddp ip-whitelist list                       # 查看当前 IP 白名单
ddp ip-whitelist clear                      # IP 白名单填错进不去面板时，清空后恢复所有 IP 可访问
ddp ip-whitelist set 203.0.113.10           # 直接重设白名单，也支持 CIDR / IPv4 通配格式
```

> **忘记密码怎么办**：`docker exec -it daidai-panel ddp list-users` 查出用户名，再 `ddp reset-password <用户名> <新密码>`，不需要删数据重装。
> **IP 白名单填错怎么办**：进入容器执行 `docker exec -it daidai-panel ddp ip-whitelist clear`，清空后登录页会恢复所有 IP 可访问，再回面板重新添加正确白名单。

命令也支持直接跑完就退出的一次性形态：

```bash
docker run --rm \
  -v $(pwd)/Dumb-Panel:/app/Dumb-Panel \
  linzixuanzz/daidai-panel:latest \
  ddp version
```

## 数据目录

默认挂在 `./Dumb-Panel`，保留这一个目录 = 保留整个面板状态：

```
Dumb-Panel/
├── daidai.db          # SQLite 数据库
├── .jwt_secret        # 自动生成的 JWT 密钥
├── panel.log          # 面板运行日志
├── deps/              # Python / Node.js 依赖
├── scripts/           # 脚本文件
├── logs/              # 任务执行日志
└── backups/           # 数据备份
```

## 配置参考

面板有两层配置：

- **启动配置**：Docker 环境变量 + `config.yaml`。Docker 部署时由 `entrypoint.sh` 自动生成，一般不需要手动改。
- **运行期配置**：进面板后「系统设置」里改，落到 SQLite 的 `system_configs` 表，重启不丢失。完整项目清单见 [系统配置与运维说明](./docs/system-config-operations.md)。

### Docker 环境变量

| 变量 | 说明 | 默认 |
|------|------|------|
| `TZ` | 时区 | `Asia/Shanghai` |
| `DATA_DIR` | 数据目录 | `/app/Dumb-Panel` |
| `DB_PATH` | 数据库路径 | `${DATA_DIR}/daidai.db` |
| `PANEL_PORT` | 容器内 Nginx 端口 | `5700` |
| `SERVER_PORT` | 容器内 Go 后端端口（**不要改**） | `5701` |
| `CONTAINER_NAME` / `IMAGE_NAME` | 面板内一键更新识别自己用 | 空 |

## 技术栈

| 层 | 技术 |
|----|------|
| 前端 | Vue 3 + TypeScript + Element Plus + Pinia + Vite + Monaco Editor |
| 后端 | Go 1.25 + Gin + GORM + SQLite（`glebarez/sqlite` 纯 Go port，`CGO_ENABLED=0`） |
| 部署 | Nginx + Go Binary，Docker 多架构镜像：`linux/amd64` / `linux/arm64` / `linux/386` / `linux/arm/v7` |

## 致谢

本项目的开发离不开以下优秀的开源项目：

- **[白虎面板 (Baihu Panel)](https://github.com/engigu/baihu-panel)** — 后端框架架构参考，部分代码基于白虎面板改进
- **[青龙面板 (Qinglong)](https://github.com/whyour/qinglong)** — 功能设计参考，定时任务管理、环境变量、订阅管理等核心功能借鉴自青龙面板

感谢以上项目作者的贡献！

## LICENSE

Copyright © 2026, [linzixuanzz](https://github.com/linzixuanzz). Released under the [MIT](LICENSE).
