# Requirements Document

## Introduction

为 GitHub 直装版 Android 应用增加 bsdiff 差分更新能力，并保留完整 APK 回退链路。

## Requirements

### Requirement 1

**User Story:** AS Android 用户, I want 下载较小的差分包, so that 应用升级减少网络流量。

#### Acceptance Criteria

1. WHEN 应用检查更新, 应用 SHALL 请求 GitHub 托管的固定 JSON 配置地址。
2. WHEN 远端版本高于本地版本, 应用 SHALL 选择与本地版本和旧 APK 哈希匹配的差分包。
3. IF 差分条件不匹配, 应用 SHALL 使用完整 APK 更新链路。

### Requirement 2

**User Story:** AS Android 用户, I want 更新文件经过完整性校验, so that 损坏文件不会进入安装流程。

#### Acceptance Criteria

1. WHEN 差分包下载完成, 应用 SHALL 校验文件大小、MD5 和 SHA-256。
2. WHEN APK 合并完成, 应用 SHALL 校验目标 APK 大小、MD5 和 SHA-256。
3. IF 任一校验失败, 应用 SHALL 清理无效产物并回退完整 APK 下载。

### Requirement 3

**User Story:** AS Android 用户, I want 系统安装器完成升级, so that Android 签名和权限规则继续生效。

#### Acceptance Criteria

1. WHEN 补丁校验通过, 应用 SHALL 通过 MethodChannel 在后台调用原生 bspatch。
2. WHEN 目标 APK 校验通过, 应用 SHALL 唤起 Android 系统安装器。
3. WHILE 差分合并执行, 应用 SHALL 保持 Flutter UI 响应。

### Requirement 4

**User Story:** AS 发布维护者, I want CI 自动生成补丁和配置, so that Release 资产保持一致。

#### Acceptance Criteria

1. WHEN Android APK 构建完成, CI SHALL 下载上一稳定版本 APK并生成 BSDIFF40 补丁。
2. WHEN 补丁生成完成, CI SHALL 使用 bspatch 重建并逐字节比较目标 APK。
3. WHEN Release 发布, CI SHALL 上传完整 APK、差分包和 `android-update.json`。
