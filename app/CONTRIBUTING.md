# 贡献指南

感谢参与呆呆面板 Flutter 的开发与维护。

## 提交 Issue

请提供以下信息：

- App 版本与安装来源
- Android/iOS 版本和设备型号
- 面板版本与部署方式
- 清晰的复现步骤
- 预期行为和实际行为
- 经过脱敏的日志或截图

安全漏洞请按 `SECURITY.md` 提交，避免公开敏感细节。

## 本地开发

```bash
flutter pub get
flutter analyze
flutter test
flutter run
```

## Pull Request

- 从最新 `main` 分支创建功能分支。
- 保持改动聚焦，避免混入无关格式化。
- 保留现有业务逻辑、中文文案和跨平台行为。
- UI 改动需验证浅色、深色、快速滚动和窄屏布局。
- 提交前运行 Android/iOS 构建或说明未执行的原因。
- 新增依赖时更新 `docs/THIRD_PARTY_NOTICES.md`，注明来源和许可证。

## 提交信息

建议使用 Conventional Commits：

```text
feat: add feature
fix: resolve issue
refactor: simplify implementation
docs: update documentation
chore: maintain project
```
