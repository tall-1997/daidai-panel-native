# daidai-panel-native

呆呆面板 Android 本机版。用户安装一个 APK 后即可在普通非 Root ARM64 手机上创建和管理本地面板，无需 Docker、服务器或独立后端。

## 目录

- `app/`：Flutter App 与 Android `:panel` 进程宿主。
- `panel/`：Go 面板源码和可嵌入 `server/mobilecore`。
- `.monkeycode/`：项目架构、需求和技术设计。

## 本地验证

```bash
cd panel/server
go test -race ./mobilecore ./database

cd ../../app
PANEL_SOURCE_DIR=../panel bash scripts/build-mobile-core-aar.sh
flutter test
flutter analyze
flutter build apk --release
```

首期只构建 `arm64-v8a`。Android Core 监听动态 `127.0.0.1` 端口，并要求安装级 token、严格 Host 与 Origin 校验。
