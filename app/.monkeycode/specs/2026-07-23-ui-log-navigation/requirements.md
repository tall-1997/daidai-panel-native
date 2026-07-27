# Requirements Document

## Introduction

本功能统一应用内残留业务表面的 Liquid Glass 风格，增强任务日志文件导航，并消除更多页进入二级页面时的首帧闪烁。

## Glossary

- **业务表面**：承载表单、信息或交互操作的卡片、弹窗、面板和按钮区域。
- **日志文件项**：任务日志文件接口返回的单个物理日志文件。
- **日志记录**：日志列表接口返回且具有日志 ID 的任务执行记录。
- **二级页面**：通过 root navigator 覆盖主导航页面的功能页面。

## Requirements

### Requirement 1

**User Story:** AS 用户, I want 应用业务表面采用一致风格, so that 页面视觉层级保持统一。

#### Acceptance Criteria

1. WHEN 应用展示业务信息面板或高价值交互表面, 应用 SHALL 使用项目共享 Liquid Glass 表面组件。
2. WHILE 状态徽标、图表、终端、编辑器或背景遮罩承担功能性配色, 应用 SHALL 保留对应功能性颜色。
3. WHEN 应用展示 Dialog、BottomSheet 或 PopupMenu, 应用 SHALL 使用统一的半透明玻璃色、边框、阴影和圆角参数。

### Requirement 2

**User Story:** AS 用户, I want 从任务日志文件列表打开对应日志, so that 可以直接查看文件内容和执行状态。

#### Acceptance Criteria

1. WHEN 用户打开任务菜单中的日志文件, 应用 SHALL 展示文件名、大小和创建时间。
2. WHEN 日志文件路径与日志记录路径匹配, 应用 SHALL 将日志文件项导航到对应日志详情页。
3. IF 日志文件缺少对应日志记录, 应用 SHALL 保留文件信息并显示不可跳转说明。
4. WHILE 任务日志记录超过单页容量, 应用 SHALL 分页加载日志记录直至完成路径关联。

### Requirement 3

**User Story:** AS 用户, I want 更多页导航保持画面连续, so that 进入二级页面时没有整屏闪烁。

#### Acceptance Criteria

1. WHEN 用户从更多页进入二级页面, 应用 SHALL 在首帧绘制稳定背景底色和当前背景图片。
2. WHEN root navigator 展示二级页面, 应用 SHALL 避免新旧全屏玻璃捕获树执行默认交叉过渡。
3. WHILE 二级页面使用静态背景捕获, 应用 SHALL 在首帧同步建立玻璃捕获快照。
