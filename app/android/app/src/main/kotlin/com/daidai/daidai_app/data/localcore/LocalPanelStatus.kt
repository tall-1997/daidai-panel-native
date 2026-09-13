package com.daidai.daidai_app.data.localcore

/**
 * 本地面板运行时状态（占位结构）。
 *
 * 阶段 0 仅定义状态模型的形状，供 UI 层订阅 `StateFlow`。
 * 阶段 1 接入 `LocalPanelRuntime`（见 `core/` 资产）后补齐真实字段：
 *  - base_url：本地 HTTP fallback 服务器的访问地址（规划 §2.4）
 *  - local_token：本地鉴权令牌（路径 B 托管本地登录）
 *  - fallbackMode：started / stopped / diagnostic 三态
 */
data class LocalPanelStatus(
    val fallbackMode: PanelMode = PanelMode.STOPPED,
    val baseUrl: String? = null,
    val localToken: String? = null,
    val message: String? = null,
)

/** 本地面板运行模式：started（托管本地）/ stopped（未启）/ diagnostic（本地恢复诊断）。 */
enum class PanelMode {
    STARTED,
    STOPPED,
    DIAGNOSTIC,
}
