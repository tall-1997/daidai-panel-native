# Requirements Document

## Introduction

基于 daidai-panel v2.3.9 已有 API，分两阶段补齐移动端功能。所有依赖后端新增接口的第三阶段能力排除在本次实施范围外。

## Requirements

### Requirement 1

1. WHEN 用户进入个人资料页, App SHALL 提供头像、用户名和密码管理。
2. WHEN 用户名或密码修改成功, App SHALL 清理认证会话并返回登录页。
3. WHEN 头像变更成功, App SHALL 刷新当前用户状态。

### Requirement 2

1. WHEN 用户进入系统诊断页, App SHALL 展示数据库、内存、调度器和网络检查结果。
2. WHEN 用户触发立即检查, App SHALL 调用 POST 健康检查并展示最新时间。

### Requirement 3

1. WHEN 用户管理任务视图, App SHALL 支持视图 CRUD、隐藏和排序。
2. WHEN 用户选择任务视图, App SHALL 将 filters 和 sort_rules 应用于任务查询。

### Requirement 4

1. WHEN 非管理员访问管理员路由, App SHALL 返回更多页。
2. WHEN 页面加载失败, App SHALL 区分 Loading、Empty 和 Error 并提供重试。

### Requirement 5

1. App SHALL 提供平台和平台令牌 CRUD、启停和筛选。
2. App SHALL 提供 config.sh 多行编辑、复制、刷新和保存。
3. App SHALL 提供 Android runtime 状态、安装日志和卸载。
4. App SHALL 提供 pip/npm 实际清单、依赖导出和批量重装。
5. App SHALL 提供环境变量批量改名、置顶和多格式文件导出。
