package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject

/**
 * 订阅（Subscription）模块数据模型（阶段 3-5，Compose 原生）。
 *
 * 迁移自 Flutter `app/lib/shared/models/subscription.dart` 的字段子集，在列表 / 详情 / 编辑
 * 所需的最小字段（id / name / url / targetPath / enabled / lastPullAt）之上，补齐与后端
 * 契约对齐的规则字段：branch / schedule / whitelist / blacklist / dependOn / subPath /
 * alias / preScript / hookScript / authType / authUsername / sshKeyId / autoAddTask /
 * autoDelTask / forceOverwrite。
 *
 * 字段名（JSON key）以 Go 后端 panel/server/model/subscription.go 与 Android 托管本地面板
 * LocalPanelStore 的 local_subscriptions 表同时支持为准：
 *  - `save_dir`：目标保存路径（服务端对“目标路径”的字段名）。
 *  - `depend_on`：依赖规则（客户端统一命名为 dependOn，传输时还原为 depend_on）。
 *  - `last_pull_at`：最近拉取时间；托管本地面板返回 `last_sync`，亦作兼容解析。
 *  - `ssh_key_id`：引用 ssh_keys 表的密钥 ID（两端均不接受内联私钥文本）。
 *  - `auth_type` 取值 "" / "token" / "ssh"（Go 侧 NormalizeSubscriptionAuthType）。
 * 其余字段缺省时回退到安全默认值。
 */
data class Subscription(
    val id: Long = 0L,
    val name: String = "",
    val url: String = "",
    /** 目标保存路径（对后端 `save_dir` / `target_path`）。 */
    val targetPath: String = "",
    val enabled: Boolean = true,
    /** 最近一次拉取时间；可能为空字符串（尚未拉取）。 */
    val lastPullAt: String = "",
    /** Git 分支；空 = 仓库默认分支。 */
    val branch: String = "",
    /** 定时同步 cron 表达式；空 = 不自动同步。 */
    val schedule: String = "",
    /** 白名单规则：`,` 或 `|` 分隔，子串包含匹配；空 = 全部命中检出范围。 */
    val whitelist: String = "",
    /** 黑名单规则：对白名单与依赖规则命中的文件都生效（命中即排除）。 */
    val blacklist: String = "",
    /** 依赖规则：参与检出并落盘，但不据此建定时任务。 */
    val dependOn: String = "",
    /** 仓库内子路径（`sub_path`）。 */
    val subPath: String = "",
    /** 显示别名。 */
    val alias: String = "",
    /** 拉取前执行的预处理脚本。 */
    val preScript: String = "",
    /** 拉取完成后执行的钩子脚本。 */
    val hookScript: String = "",
    /** 鉴权方式：""（无）/ "token" / "ssh"。 */
    val authType: String = "",
    /** Token 鉴权的用户名（可选）。 */
    val authUsername: String = "",
    /** 后端是否持有 token（仅回读，不回写明文 token）。 */
    val hasAuthToken: Boolean = false,
    /** 引用的 SSH 密钥 ID（ssh_keys 表）；null = 未使用。 */
    val sshKeyId: Long? = null,
    /** CA 证书路径。 */
    val caCertPath: String = "",
    val autoAddTask: Boolean = false,
    val autoDelTask: Boolean = false,
    val forceOverwrite: Boolean = true,
) {
    companion object {
        /** 从单个订阅 JSON 对象容错解析。targetPath 优先 `save_dir`，其次 `target_path`。 */
        fun fromJson(json: JSONObject): Subscription = Subscription(
            id = json.optLong("id"),
            name = json.optString("name"),
            url = json.optString("url"),
            targetPath = json.optString("save_dir")
                .takeIf { it.isNotBlank() }
                ?: json.optString("target_path"),
            enabled = if (json.has("enabled")) json.optBoolean("enabled") else true,
            lastPullAt = json.optString("last_pull_at").takeIf { it.isNotEmpty() && it != "null" } ?: ""
                .takeIf { it.isNotBlank() }
                ?: json.optString("last_sync"),
            branch = json.optString("branch"),
            schedule = json.optString("schedule"),
            whitelist = json.optString("whitelist"),
            blacklist = json.optString("blacklist"),
            dependOn = json.optString("depend_on"),
            subPath = json.optString("sub_path"),
            alias = json.optString("alias"),
            preScript = json.optString("pre_script"),
            hookScript = json.optString("hook_script"),
            authType = json.optString("auth_type"),
            authUsername = json.optString("auth_username"),
            hasAuthToken = json.has("has_auth_token") && json.optBoolean("has_auth_token"),
            sshKeyId = if (json.isNull("ssh_key_id")) null else json.optLong("ssh_key_id").takeIf { it > 0L },
            caCertPath = json.optString("ca_cert_path"),
            autoAddTask = json.optBoolean("auto_add_task"),
            autoDelTask = json.optBoolean("auto_del_task"),
            forceOverwrite = if (json.has("force_overwrite")) json.optBoolean("force_overwrite") else true,
        )
    }
}

/**
 * 新建 / 更新订阅的写负载。
 *
 * 承载后端（Go 订阅 handler 与 Kotlin fallback LocalPanelStore）可写的全部订阅字段：
 * 名称 / 地址 / 目标路径 / 启用，以及规则字段 whitelist / blacklist / dependOn / branch /
 * schedule / subPath / alias / preScript / hookScript / 鉴权（authType / authUsername /
 * authToken / sshKeyId / caCertPath）/ autoAddTask / autoDelTask / forceOverwrite。
 * toJson 产出后端期望的 JSON：目标路径以 `save_dir` 传输，依赖规则以 `depend_on` 传输；
 * 表单管理的编辑字段（save_dir / branch / schedule / whitelist / blacklist / depend_on /
 * sub_path / alias / auth_type / auth_username / ssh_key_id）始终写入，空串/JSON null 用于
 * 在编辑时清掉旧值；令牌与脚本字段仅在非空时透传。name / url 必填由调用方保证。
 */
data class SubscriptionWritePayload(
    val name: String,
    val url: String,
    val targetPath: String = "",
    val enabled: Boolean = true,
    /** Git 分支；空 = 仓库默认分支。 */
    val branch: String = "",
    /** 定时同步 cron 表达式；空 = 不自动同步。 */
    val schedule: String = "",
    /** 白名单规则，`,` / `|` 分隔原样传输（后端负责分段）。 */
    val whitelist: String = "",
    /** 黑名单规则。 */
    val blacklist: String = "",
    /** 依赖规则，传输为 `depend_on`。 */
    val dependOn: String = "",
    /** 仓库内子路径（`sub_path`）。 */
    val subPath: String = "",
    val alias: String = "",
    val preScript: String = "",
    val hookScript: String = "",
    /** CA 证书路径（`ca_cert_path`）。 */
    val caCertPath: String = "",
    /** 鉴权方式：""（无）/ "token" / "ssh"。 */
    val authType: String = "",
    val authUsername: String = "",
    /** 访问令牌：仅在填写时写入（远程后端加密存储；本地面板暂不持久化）。 */
    val authToken: String = "",
    /** 引用的 SSH 密钥 ID（ssh_keys 表）；null / 0 = 不使用（写入 JSON null 清除）。 */
    val sshKeyId: Long? = null,
    /** 拉取后自动同步（增）定时任务；null = 不改变后端默认。 */
    val autoAddTask: Boolean? = null,
    /** 拉取后自动同步（删）失效任务；null = 不改变后端默认。 */
    val autoDelTask: Boolean? = null,
    /** 强制覆盖已存在文件；null = 不改变后端默认。 */
    val forceOverwrite: Boolean? = null,
) {
    fun toJson(): JSONObject = JSONObject().apply {
        put("name", name)
        put("url", url)
        put("enabled", enabled)
        // 表单管理的字段始终写入（空串/JSON null 用于清空旧值）。
        put("save_dir", targetPath)
        put("branch", branch)
        put("schedule", schedule)
        put("whitelist", whitelist)
        put("blacklist", blacklist)
        put("depend_on", dependOn)
        put("sub_path", subPath)
        put("alias", alias)
        put("auth_type", authType)
        put("auth_username", authUsername)
        // ssh_key_id 始终写入：null 显式清除（Go 与本地端点均接受 JSON null）。
        if (sshKeyId != null && sshKeyId > 0L) put("ssh_key_id", sshKeyId)
        else put("ssh_key_id", JSONObject.NULL)
        // 令牌与脚本字段仅在非空时透传，避免空串覆盖已存值。
        putIfNotBlank("auth_token", authToken)
        putIfNotBlank("pre_script", preScript)
        putIfNotBlank("hook_script", hookScript)
        putIfNotBlank("ca_cert_path", caCertPath)
        autoAddTask?.let { put("auto_add_task", it) }
        autoDelTask?.let { put("auto_del_task", it) }
        forceOverwrite?.let { put("force_overwrite", it) }
    }

    private fun JSONObject.putIfNotBlank(key: String, value: String) {
        if (value.isNotBlank()) put(key, value)
    }
}

/**
 * 订阅规则辅助函数：语义与 Go 后端 panel/server/service/subscription.go 对齐。
 *
 *  - 分隔符 `,` 与 `|` 均可，分段后去空白、丢弃空段、去重（splitSubscriptionFilterPatterns）。
 *  - 匹配为子串包含（strings.Contains），非正则（subscriptionFilterContains）。
 *  - 白名单：空 = 全部命中；含通配段（*、**、*.*、.*、/、all、any、全部）= 全部命中；
 *    否则命中任一段即视为命中（matchesSubscriptionWhitelist）。
 *  - 黑名单：命中任一段即被排除；对白名单与依赖规则都生效（checkBlacklist）。
 *  - 依赖规则（depend_on）：命中任一段即视为依赖文件（matchesSubscriptionDependency），
 *    参与检出但不建任务。
 */
object SubscriptionRules {
    /** 与 Go splitSubscriptionFilterPatterns 一致：按 `,` 或 `|` 分段、trim、丢弃空段、去重。 */
    fun split(raw: String?): List<String> {
        if (raw.isNullOrBlank()) return emptyList()
        val seen = LinkedHashSet<String>()
        for (part in raw.split(',', '|')) {
            val trimmed = part.trim()
            if (trimmed.isNotEmpty()) seen.add(trimmed)
        }
        return seen.toList()
    }

    /** 与 Go isWildcardFilterPattern 一致：这些段视为“匹配全部文件”。 */
    fun isWildcard(pattern: String): Boolean = when (pattern.trim().lowercase()) {
        "*", "**", "*.*", ".*", "/", "all", "any", "全部" -> true
        else -> false
    }

    /** 与 Go normalizeSubscriptionFilterTarget 一致：trim、\ 转 /、去 ./ 与 / 前缀。 */
    fun normalize(value: String): String {
        var v = value.trim().replace('\\', '/')
        v = v.removePrefix("./").removePrefix("/")
        return v
    }

    /** 与 Go subscriptionFilterContains 一致：对归一化后的相对路径做子串包含匹配。 */
    fun contains(path: String, pattern: String): Boolean {
        val target = normalize(path)
        val p = normalize(pattern)
        return target.isNotEmpty() && p.isNotEmpty() && target.contains(p)
    }

    /** 白名单命中：空 = 全部命中；任一通配段 = 全部命中；否则任一段子串命中。 */
    fun matchesWhitelist(whitelist: String?, path: String): Boolean {
        val patterns = split(whitelist)
        if (patterns.isEmpty()) return true
        var hasNonWildcard = false
        for (pattern in patterns) {
            if (isWildcard(pattern)) return true
            hasNonWildcard = true
            if (contains(path, pattern)) return true
        }
        return !hasNonWildcard
    }

    /** 黑名单命中：任一段子串命中即被排除（通配段跳过，与 Go 一致）。 */
    fun matchesBlacklist(blacklist: String?, path: String): Boolean {
        for (pattern in split(blacklist)) {
            if (isWildcard(pattern)) continue
            if (contains(path, pattern)) return true
        }
        return false
    }

    /** 依赖规则命中：任一段子串命中即视为依赖文件（通配段跳过）。 */
    fun matchesDependOn(dependOn: String?, path: String): Boolean {
        for (pattern in split(dependOn)) {
            if (isWildcard(pattern)) continue
            if (contains(path, pattern)) return true
        }
        return false
    }
}

/**
 * 把 GET /api/subscriptions 的响应体解析为订阅列表。
 *
 * 托管本地面板与远程 Go handler 的列表响应均为 `{"data": [...]}`；
 * 少数旧端点直接返回数组，这里两种都兼容。
 */
fun parseSubscriptions(rawBody: String): List<Subscription> {
    val array = if (rawBody.trimStart().startsWith("{")) {
        try {
            JSONObject(rawBody).optJSONArray("data") ?: return emptyList()
        } catch (_: Exception) {
            return emptyList()
        }
    } else {
        try {
            JSONArray(rawBody)
        } catch (_: Exception) {
            return emptyList()
        }
    }
    val result = ArrayList<Subscription>(array.length())
    for (i in 0 until array.length()) {
        val element = array.optJSONObject(i) ?: continue
        result.add(Subscription.fromJson(element))
    }
    return result
}
