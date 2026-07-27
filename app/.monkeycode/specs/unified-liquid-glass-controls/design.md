# 统一液态玻璃控件设计

Feature Name: unified-liquid-glass-controls
Updated: 2026-07-22

## 设计说明

UI 基于 `liquid_glass_easy 3.3.1` 重构。主 Shell 使用 `LiquidGlassScaffold`，底部导航使用 `LiquidGlassBottomNavBar`，卡片和自定义控件使用布局驱动的 `LiquidGlassLens`。

## 架构

- `LiquidGlassScaffold` 提供 Impeller 实时背景和 Skia 捕获管线。
- `LiquidGlassBottomNavBar` 提供五入口折射导航和动态选择胶囊。
- `LiquidGlassLens` 提供卡片、筛选、选项和操作按钮。
- `appLiquidGlassStyle` 统一浅色、深色、选中和滚动性能配置。
- `MaterialScrollBehavior(overscroll: false)` 防止 Android Stretch Overscroll 隔离 Lens 背景。

## 组件接口

- `AppCard`：基于 `LiquidGlassLens` 的内容卡片和选项卡片。
- `AppLiquidGlassSurface`：基于 `LiquidGlassLens` 的筛选和操作表面。
- `AppGlassIconButton`：基于 `LiquidGlassLens` 的页头按钮。
- `AppLiquidGlassInput`：固定搜索框和筛选输入的低成本 Lens 外壳。
- `AppLiquidGlassToggle`：统一 `LiquidGlassToggle` 的主题、禁用态和像素比。
- `AppLiquidGlassButton`：统一 `LiquidGlassButton` 的主要、次要、危险、警告和加载状态。
- `AppLiquidGlassChoiceChip`：交互型 ChoiceChip 与 FilterChip。
- `AppLiquidGlassActionChip`：快捷操作 Chip。
- `AppLiquidGlassInputChip`：可删除标签 Chip。
- `LiquidGlassSlider`：主题背景模糊强度滑块。
- `AppLiquidGlassDialogActions`：1–3 个响应式对话框动作按钮，支持窄屏纵向排列。
- `AppGlassNotice`：统一 SnackBar 队列、图标、语义色和持续时间。
- 高频设置菜单、系统操作、订阅日志和脚本版本历史使用低成本 Lens 整卡。
- Security、OpenAPI、任务/变量排序、依赖概览、服务器、赞助和备份完整表面已统一为 Lens。
- 任务主卡、侧滑操作条和应用锁面板已统一为真实 Lens；Cron 与订阅摘要改为主卡内部透明分区。
- 高价值 Dialog、BottomSheet 主动作和自动完成选项面板已完成 Lens 迁移。
- `InputDecorationTheme`：搜索框和表单输入。
- `FilledButtonThemeData`：主要操作按钮。
- `OutlinedButtonThemeData`：次要操作按钮。
- `TextButtonThemeData`：文字操作按钮。
- `ChipThemeData`：ChoiceChip、FilterChip、ActionChip 和 InputChip。
- `TabBarThemeData`：页面选项卡。

## 正确性约束

- 项目不再依赖 `liquid_glass_widgets`。
- 项目仅通过 `liquid_glass_easy` 公共 barrel API 使用组件。
- 滚动 Lens 使用低失真、低模糊、零色散配置。
- 全局关闭 Stretch Overscroll。
- 危险操作保留红色前景语义。
- 主要操作保留 Emerald 前景语义。
- 所有圆角玻璃控件使用裁剪或 Material 形状限制绘制范围。

## 测试策略

- 浅色和深色模式逐页检查。
- 快速滚动任务、日志、变量、依赖、用户和通知列表。
- 检查页头按钮、搜索框、Chip、主题模式和批量按钮。
- 检查 Toggle 滑动动画、Slider 拖动和禁用状态。
- 检查危险确认对话框的取消、确认和返回值。
- 检查 toast 位于底部导航上方、横向可关闭且不会队列堆积。
- 全仓直接 SnackBar 仅允许存在于 `AppGlassNotice` 内部。
- 检查 320dp 窄屏和较大字体。
- 构建 Android Release APK。
- 构建 iOS Release IPA。
