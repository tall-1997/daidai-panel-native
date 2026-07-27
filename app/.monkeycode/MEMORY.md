# 用户指令记忆

本文件记录用户的指令、偏好和教导，用于后续协作。

## 条目

### Git 推送身份
- Date: 2026-07-22
- Context: 推送本项目代码到远程仓库
- Instructions:
  - Git 提交与推送使用用户名 `tall-1997`。
  - 远程仓库使用 `tall-1997/daidai-flutter`。
  - 提交身份使用 `tall-1997 <noreply@github.com>`。
  - 推送记录与提交信息中避免使用 `MonkeyCode-AI` 身份。
  - 远程 URL 使用标准 GitHub 地址，认证交给 Git 凭据助手，避免在仓库配置中内嵌凭据。

### UI 渲染回归验证
- Date: 2026-07-22
- Context: v0.1.29 修复深色模式纯黑卡片和滚动渲染问题
- Instructions:
  - 修改卡片渲染后，检查浅色和深色模式、快速滚动、分组展开、侧滑操作和小屏布局。
  - 内容级滚动卡片使用 `LiquidGlassLens` 的低失真、低模糊、零色散配置。
  - 页面级使用 `LiquidGlassScaffold` 和 `LiquidGlassBottomNavBar`。
  - 提交前确认 Android 与 iOS 构建均通过。

### 统一液态玻璃控件
- Date: 2026-07-22
- Context: 全应用按钮、搜索框、Chip 和选项卡片重构
- Instructions:
  - 标准控件优先通过 `AppTheme` 统一液态玻璃视觉。
  - 自定义控件复用 `appGlassDecoration` 或 `AppGlassIconButton`。
  - 视觉与底部 `LiquidGlassBottomNavBar` 保持一致。
  - 新控件需要提供圆角裁剪和窄屏防溢出处理。

### liquid_glass_easy 架构
- Date: 2026-07-22
- Context: 全应用迁移到 `liquid_glass_easy 3.3.1`
- Instructions:
  - 使用 `LiquidGlassScaffold`、`LiquidGlassBottomNavBar` 和 `LiquidGlassLens` 公共 API。
  - 禁止引用 `liquid_glass_easy/src/` 私有实现。
  - 滚动 Lens 使用低失真、低模糊和零色散性能配置。
  - Android 全局关闭 Stretch Overscroll，避免滚动边缘玻璃变黑。
  - 搜索、Toggle、Slider、主按钮和交互 Chip 优先使用项目共享的 `liquid_glass_easy` 封装。
  - 滚动卡片内部的纯展示徽标保持轻量绘制，控制 Lens 嵌套数量。
  - 高价值取消/确认对话框使用 `AppLiquidGlassDialogActions`，窄屏自动纵向排列。
  - toast 使用 `AppGlassNotice`，显示在底部导航上方并清理旧提示队列。
  - Dialog、BottomSheet 和 PopupMenu 统一使用玻璃主题的表面、圆角、描边与遮罩。
  - 高频整卡使用低成本 Lens，终端、编辑器和纯展示徽标保持轻量绘制。
  - 完整卡片和可点击信息表面统一使用 `AppCard` 或 `AppLiquidGlassSurface`。
  - 业务提示统一使用 `AppGlassNotice`，避免直接调用 `showSnackBar`。
  - 任务卡保持一张主 Lens 和一个共享侧滑 Lens，Cron/订阅摘要使用透明内容分区。
  - 高价值对话框和 BottomSheet 主动作统一使用共享液态玻璃动作组件。
  - 任务卡不使用侧滑“禁用/更多/删除”操作层；环境变量卡保持独立间距和长文本行数限制。
