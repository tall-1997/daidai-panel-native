# UI 渲染回归防护指南

## 背景

`v0.1.29` 修复了深色模式下内容卡片偶发纯黑块、滚动时玻璃透明、彩色边缘和离屏纹理异常问题。日志页面一直表现稳定，因此本次以日志列表卡片作为全应用内容卡片的渲染基线。

## 根因

问题来源于滚动内容中逐卡片创建 `GlassCard(useOwnLayer: true)`。即使使用 `GlassQuality.minimal`，组件仍会使用背景模糊或离屏合成。卡片进入滚动、裁剪、回收、复用或位移动画后，部分设备的 GPU 合成纹理可能显示为纯黑矩形或透明表面。

深色颜色值不是主要原因。日志页使用相同的 `AppColors.darkSurface` 和 `AppColors.darkBorder`，通过普通 `Container + BoxDecoration` 能稳定呈现蓝灰半透明表面。

## 历史稳定架构

- 主页面使用 `LiquidGlassScaffold`。
- 底部导航使用 `LiquidGlassBottomNavBar`。
- 内容卡片统一使用基于 `LiquidGlassLens` 的 `AppCard`。
- 浅色表面使用 `AppColors.lightSurface`。
- 深色表面使用 `AppColors.darkSurface`。
- 深色边框使用 `AppColors.darkBorder`。
- 滚动列表优先使用 `ListView.builder` 或 Sliver 惰性构建。
- 滚动 Lens 使用低失真、低模糊、零色散配置，并关闭 Stretch Overscroll。

## 禁止回归模式

内容级卡片避免重新引入以下组合：

```dart
GlassCard(
  useOwnLayer: true,
  child: ...,
)
```

尤其避免在 `ListView`、`ReorderableListView`、`Transform`、`AnimatedContainer`、`Dismissible` 或多重 `ClipRRect` 内使用逐卡片玻璃层。

## 修改范围

`v0.1.29` 将以下页面的内容卡片统一到稳定表面：

- 仪表盘和统计图表
- 定时任务与任务表单
- 环境变量
- 用户管理
- 通知渠道与本地通知
- 依赖管理
- 安全日志与会话
- OpenAPI
- 脚本管理
- 订阅管理
- 系统设置、面板设置与备份
- 应用锁
- 更多与主题设置

## 回归检查清单

每次修改卡片或背景实现后执行：

1. 浅色模式检查页面背景、卡片、输入框、Chip 和文字对比度。
2. 深色模式检查卡片保持蓝灰色层次，无纯黑矩形。
3. 快速连续上下滚动，检查卡片无透明跳变、彩虹条和残影。
4. 展开和折叠任务分组，检查卡片内容与背景稳定。
5. 测试任务侧滑、删除按钮和“更多”操作面板。
6. 使用窄屏和较大字体检查横向溢出。
7. 确认内容代码中没有新增 `GlassCard(`。
8. 执行 Android Release 构建。
9. 执行 iOS Release 无签名构建。

## 检查命令

```bash
# 内容卡片应通过 AppCard 实现
rg "GlassCard\\(" lib

# 检查差异格式
git diff --check

# 构建 Android
flutter build apk --release

# 构建 iOS
flutter build ios --release --no-codesign
```

正常情况下，旧 `GlassCard(` 与 `liquid_glass_widgets` 搜索结果为空；液态玻璃统一由 `liquid_glass_easy` 提供。

## 统一控件视觉

`v0.1.30` 起，按钮、搜索框、Chip、选项卡片和内容卡片统一采用底部导航栏的视觉语言：半透明表面、蓝灰深色基底、白色浅色高光、低对比描边和 Emerald 状态光晕。

- 标准 Material 控件由 `AppTheme` 统一配置。
- 自定义控件使用 `appGlassDecoration`。
- 页头图标操作使用 `AppGlassIconButton`。
- 危险操作使用红色前景和玻璃背景。
- 主要操作使用 Emerald 前景和玻璃背景。
- 真实 `BackdropFilter` 继续限定在页面级背景和固定覆盖层。

## liquid_glass_easy 3.3.1 迁移

- 主页面使用 `LiquidGlassScaffold`。
- 底部导航使用 `LiquidGlassBottomNavBar`。
- 内容卡片使用 `LiquidGlassLens`。
- 筛选和操作表面使用 `AppLiquidGlassSurface`。
- 页头按钮使用 `AppGlassIconButton`。
- 所有包引用通过 `package:liquid_glass_easy/liquid_glass_easy.dart` 公共入口。
- 全局关闭 Stretch Overscroll，防止 Android 滚动边缘 Lens 变黑。
- 滚动卡片采用低失真、低模糊、零色散配置。
- 搜索框使用 `AppLiquidGlassInput`。
- 独立开关使用 `AppLiquidGlassToggle`。
- 模糊强度使用 `LiquidGlassSlider`。
- 高曝光主按钮使用 `AppLiquidGlassButton`。
- 交互型 Chip 使用 `AppLiquidGlassChoiceChip`、`AppLiquidGlassActionChip` 或 `AppLiquidGlassInputChip`。
- 纯展示状态徽标保留轻量绘制，避免无收益的 Lens 嵌套。
- 高价值确认对话框使用 `AppLiquidGlassDialogActions`。
- 卡片内部辅助链接、分页、编辑器工具和纯关闭按钮可保留 Material，控制 Lens 嵌套数量。
- SnackBar 使用悬浮玻璃主题，底部保留 92px 导航安全距离。
- 高频反馈通过 `AppGlassNotice` 显示，先隐藏当前提示再展示新提示。
- Dialog、BottomSheet 和 PopupMenu 使用统一半透明表面、描边、圆角和遮罩。
- 设置菜单、系统操作、订阅历史和脚本版本历史使用 `performanceMode` Lens。
- 终端、编辑器、状态点和纯展示徽标继续使用轻量绘制。
- 完整卡片、菜单入口、排序条目和可点击信息表面统一使用 `AppCard` 或 `AppLiquidGlassSurface`。
- 全仓业务代码通过 `AppGlassNotice` 显示反馈，直接 `showSnackBar` 仅保留在共享实现内部。
- 任务展开结构保持一张主 Lens、一个共享侧滑 Lens，内部摘要使用透明分区和轻量徽标。
- 应用锁核心面板使用 `AppCard`，外层遮罩与背景模糊继续保留。
- 取消、确认、保存、删除和流程切换动作统一使用 `AppLiquidGlassDialogActions` 或 `AppLiquidGlassButton`。
- 自动完成弹出选项使用低成本 `AppLiquidGlassSurface`。
- 任务卡不再提供侧滑动作层，点击查看日志、长按选择，运行/停止按钮保留在卡片内部。
- 环境变量卡保持至少 12px 外间距、16px 圆角裁剪，变量值最多 6 行，备注最多 2 行。
