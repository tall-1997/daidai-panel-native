# 呆呆面板原生版项目文档

## 项目愿景

用户只安装呆呆面板 App，即可在 Android 手机本机创建、管理和运行面板，无需准备 Docker、服务器或独立后端服务。

首期产品以非 Root Android ARM64 为目标，通过 GitHub Release 提供离线完整 APK。App 内置面板核心、Python、Node.js、TypeScript、受控 Shell、Git 与 Go 工具链，同时保留连接远程呆呆面板的能力。

## 已确认范围

- 导入并持续同步现有 Flutter App 与 Go 后端。
- 采用 Android Foreground Service、gomobile 生命周期薄绑定和 Go 回环 HTTP Core。
- 所有运行时随 APK 交付，安装后可离线完成基础执行。
- 每条发布轨道提供一个自包含 APK，同一设备同一时刻安装一个轨道。
- 发布现代 SDK 主版本和 Legacy 实验版本，两者使用同一应用 ID、签名、版本代码、数据格式和 API 契约。
- 现代版使用 Yaegi 执行受控 Go 源码，并通过 Go 工具链提供模块准备与构建导出；Legacy 版提供实验性的 `go run`、`go test` 和 `go build`。
- 首版只监听回环地址，局域网访问能力进入后续规划。

## 文档索引

- `ARCHITECTURE.md`：系统边界、组件、数据流和交付结构。
- `../specs/android-local-panel/requirements.md`：EARS 格式产品需求与验收标准。
- `../specs/android-local-panel/design.md`：Android 完整执行版技术设计。

## 上游基线

- Flutter App：<https://github.com/linzixuanzz/Dumb-Panel-APP>
- Go 面板：<https://github.com/linzixuanzz/daidai-panel>
- 分析版本：呆呆面板 `v2.3.9`

## 产品边界

“完整执行”表示 APK 内覆盖 Python、Node.js、TypeScript、受控 Shell、Git 与 Go 任务主链路。Android ARM64/Bionic 兼容清单定义可安装的原生 Python 扩展、Node addon 和 CGO 依赖。通用 Linux 包管理器、任意 glibc 二进制和系统级操作不属于普通 Android 沙箱能力。
