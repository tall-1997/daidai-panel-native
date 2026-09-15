package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 阶段 3-4 依赖（Deps）模块的数据模型（DTO）。
 *
 * 与既有数据模型约定一致：所有 `fromJson` 字段解析均做容错（optXxx + 默认值），
 * 后端任一字段缺失或类型不符都不应导致页面崩溃。
 *
 * 后端依赖表（Go `Dependency`）字段：id / type(nodejs|python|linux) / name /
 * python_version / status / operation_id / created_at / updated_at。本模块特有的
 * 安装请求语义（manager/pip|npm + package + version）在 repository 层映射为后端
 * 的 type + names + python_version。
 */

/** 安装包管理器：pip = Python，npm = Node.js，system = Linux 系统包。 */
enum class DepManager(val id: String, val label: String, val backendType: String) {
    Pip("pip", "pip (Python)", "python"),
    Npm("npm", "npm (Node.js)", "nodejs"),
    System("system", "系统包", "system");

    companion object {
        fun fromId(id: String?): DepManager =
            values().firstOrNull { it.id.equals(id, ignoreCase = true) } ?: Npm
    }
}

/**
 * 单个依赖项（后端 GET /api/deps 列表项的最小展示字段集）。
 *
 * - [type] 域内取值约束为 `python` / `node`（后端 `nodejs`/`linux` 统归 `node`）。
 * - [version] 用于展示“已安装版本”，Python 依赖回退用运行时版本，否则展示状态文案。
 * - [runtime] 携带 Python 运行时版本（python_version），Node 依赖为空串。
 * - [status] 保留后端原始状态字（installed/installing/failed/...），供 UI 着色。
 */
data class DepItem(
    val id: Long = 0L,
    val name: String = "",
    val type: String = "node",
    val version: String = "",
    val runtime: String = "",
    val status: String = "",
) {
    val isPython: Boolean get() = type.equals("python", ignoreCase = true)

    /** 安装类型英文标签（可用于筛选/展示）。 */
    val typeLabel: String get() = if (isPython) "Python" else if (type == "linux") "Linux" else "Node"
    val active: Boolean get() = status in setOf("queued", "installing", "removing")

    companion object {
        fun fromJson(json: JSONObject): DepItem {
            val rawType = json.optString("type")
            val runtime = json.optString("python_version")
            val rawStatus = json.optString("status")
            return DepItem(
                id = json.optLong("id"),
                name = json.optString("name"),
                type = when (rawType.lowercase()) { "python" -> "python"; "linux" -> "linux"; else -> "node" },
                version = runtime.takeIf { it.isNotBlank() }
                    ?: statusLabel(rawStatus),
                runtime = runtime,
                status = rawStatus,
            )
        }
    }

    /** 后端状态字 → 面向用户的中文文案；未知状态原样返回。 */
    fun statusLabel(status: String): String = when (status) {
        "installed" -> "已安装"
        "installing" -> "安装中"
        "queued" -> "排队中"
        "failed" -> "失败"
        "removing" -> "卸载中"
        "cancelled" -> "已取消"
        else -> status.ifEmpty { "-" }
    }
}

/**
 * 系统包信息（对应系统包管理器）。
 */
data class SystemPackage(
    val name: String = "",
    val version: String = "",
    val description: String = "",
    val source: String = "", // apk/apt/dnf/yum/microdnf/zypper
)

/**
 * 安装依赖的请求载体（提交表单）。
 *
 * [version] 可选（仅 Python 依赖会把版本写入 python_version）。
 */
data class DepInstallRequest(
    val manager: DepManager = DepManager.Pip,
    val packageName: String = "",
    val version: String = "",
) {
    /** 提交前基本校验：管理器 + 包名必填。 */
    val isValid: Boolean get() = packageName.isNotBlank()
}

/** 系统包安装结果 */
data class SystemPackageInstallResult(val success: Boolean = false, val message: String = "")

/** 系统包卸载结果 */
data class SystemPackageUninstallResult(val success: Boolean = false, val message: String = "")

/** 解析后端列表响应 `{data:[...], total:N}`，取 data 数组映射为 [DepItem]。 */
fun parseDepItems(rawBody: String): List<DepItem> {
    val array: JSONArray = try {
        JSONObject(rawBody).optJSONArray("data") ?: JSONArray()
    } catch (_: Exception) {
        JSONArray()
    }
    val result = ArrayList<DepItem>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(DepItem.fromJson(element))
    }
    return result
}

/** 解析后端系统包列表响应 `{data:[...]}`，取 data 数组映射为 [SystemPackage]。 */
fun parseSystemPackages(rawBody: String): List<SystemPackage> {
    val array: JSONArray = try {
        JSONObject(rawBody).optJSONArray("data") ?: JSONArray()
    } catch (_: Exception) {
        JSONArray()
    }
    val result = ArrayList<SystemPackage>(array.length())
    for (i in 0 until array.length()) {
        val obj = array.optJSONObject(i) ?: continue
        result.add(SystemPackage(
            name = obj.optString("name"),
            version = obj.optString("version"),
            description = obj.optString("description"),
            source = obj.optString("source"),
        ))
    }
    return result
}
