# Android 普通版本地核心 MVP 需求

## 简介

Android 普通版在 App 私有沙箱内提供本地面板能力，首期完整覆盖主页、任务管理、环境变量和依赖安装。依赖安装是首期最高优先级，目标运行时为 Android ARM64 的 Python 3.12 与 Node.js 20。

## 术语

- **本地核心**：由 Android App 管理的面板业务服务。
- **运行时组件**：经过签名和摘要校验的 Python、Node.js 或工具组件。
- **依赖操作**：运行时安装、pip/npm 安装、卸载、重装或取消形成的异步操作。
- **能力清单**：描述设备、运行时、调度和包兼容性的结构化数据。

## Requirement 1：本地实例

1. WHEN 用户首次进入本地模式, App SHALL 创建本地核心数据目录并启动本地核心。
2. WHEN 本地核心就绪, App SHALL 建立本地会话并加载主页。
3. IF 本地核心启动失败, App SHALL 展示失败阶段、诊断信息和重试入口。
4. WHILE 持续调度已启用, App SHALL 使用 Android 前台服务承载本地核心。

## Requirement 2：主页完整性

1. WHEN 用户打开主页, App SHALL 展示本地核心、设备资源、数据占用和运行时状态。
2. WHEN 主页数据部分加载失败, App SHALL 保留成功数据并展示失败项。
3. WHEN 用户刷新主页, App SHALL 并发刷新系统、任务统计、能力和版本信息。

## Requirement 3：任务管理完整性

1. App SHALL 提供任务 CRUD、启停、手动运行、停止、批量操作、排序、视图和日志。
2. WHEN 用户保存任务, App SHALL 校验任务所需运行时和调度能力。
3. WHEN 任务执行, App SHALL 持久化状态、退出码、日志、耗时和触发来源。
4. IF 任务日志连接中断, App SHALL 恢复日志游标并保持终态准确。

## Requirement 4：环境变量完整性

1. App SHALL 提供环境变量 CRUD、启停、分组、排序、批量操作和导入导出。
2. WHEN App 持久化敏感变量, 本地核心 SHALL 使用平台密钥保护变量值。
3. WHEN 任务启动, 本地核心 SHALL 注入任务作用域内已启用的变量。
4. IF 导入内容包含冲突或无效项, App SHALL 展示预览和逐项结果。

## Requirement 5：依赖安装完整性

1. App SHALL 展示 Python、Node.js 运行时版本、架构、状态、大小、校验和包兼容等级。
2. WHEN 用户安装依赖, 本地核心 SHALL 创建具有唯一 ID 的依赖操作。
3. WHILE 依赖操作运行, App SHALL 展示队列、阶段、进度、日志和取消能力。
4. WHEN 依赖操作结束, App SHALL 区分成功、失败、取消、超时和连接错误。
5. IF 包需要缺失的原生 ABI, App SHALL 返回结构化兼容错误和解决建议。
6. IF 存储空间低于下载、解压和回滚所需空间, App SHALL 在下载前终止操作并报告所需空间。
7. WHEN 安装成功, App SHALL 刷新依赖记录和实际 pip/npm 包清单。

## Requirement 6：日志和恢复

1. App SHALL 限制内存日志条数并将完整日志保存到本地核心。
2. WHEN SSE 重连, App SHALL 使用事件序号恢复后续日志。
3. WHEN App 重新进入依赖操作页面, App SHALL 从操作状态恢复日志和终态。
4. IF App 在安装期间退出, 本地核心 SHALL 保持操作状态并在下次启动时完成恢复或回滚。

## Requirement 7：安全与供应链

1. 本地核心 SHALL 仅接受回环地址连接和 App 生成的本地授权凭据。
2. App SHALL 校验运行时组件签名、SHA-256、平台和架构。
3. App SHALL 将运行时、依赖、日志和临时文件置于独立配额目录。
4. WHEN 操作失败或取消, App SHALL 清理临时文件并保留可诊断日志。
