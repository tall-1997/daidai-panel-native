package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.remote.PanelRequests
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject

/** SSH 密钥条目（服务端对私钥做掩码，仅详情接口返回明文）。 */
data class SshKey(
    val id: Long = 0L,
    val name: String = "",
    val privateKeyMasked: String = "",
    val createdAt: String = "",
    val updatedAt: String = "",
) {
    companion object {
        fun fromJson(json: JSONObject): SshKey = SshKey(
            id = json.optLong("id"),
            name = json.optString("name"),
            privateKeyMasked = json.optString("private_key"),
            createdAt = json.optString("created_at").takeIf { it != "null" } ?: "",
            updatedAt = json.optString("updated_at").takeIf { it != "null" } ?: "",
        )
    }
}

/**
 * SSH 密钥仓库：`GET/POST /api/ssh-keys`、`GET/PUT/DELETE /api/ssh-keys/:id`。
 * 创建与更新提交 `{name, private_key}`；更新时 "********" 视为保持原值（服务端语义）。
 */
class SshKeysRepository(
    baseUrl: String,
    private val accessToken: String? = null,
) {
    private val baseUrl: String = baseUrl.trim().trimEnd('/')

    private fun execute(method: String, path: String, json: String? = null): String =
        withContext(Dispatchers.IO) {
            PanelRequests.execute(method, baseUrl + path, json, accessToken = accessToken)
        }

    suspend fun list(): List<SshKey> = withContext(Dispatchers.IO) {
        val body = PanelRequests.execute("GET", "$baseUrl/api/ssh-keys", accessToken = accessToken)
        val data = runCatching { JSONObject(body).optJSONArray("data") }.getOrNull() ?: JSONArray()
        (0 until data.length()).map { SshKey.fromJson(data.optJSONObject(it) ?: JSONObject()) }
    }

    suspend fun create(name: String, privateKey: String): Long = withContext(Dispatchers.IO) {
        val body = PanelRequests.execute(
            "POST",
            "$baseUrl/api/ssh-keys",
            json = JSONObject().put("name", name).put("private_key", privateKey).toString(),
            accessToken = accessToken,
        )
        JSONObject(body).optJSONObject("data")?.optLong("id") ?: 0L
    }

    suspend fun update(id: Long, name: String, privateKey: String) = withContext(Dispatchers.IO) {
        val payload = JSONObject().put("name", name)
        if (privateKey.isNotBlank() && privateKey != "********") {
            payload.put("private_key", privateKey)
        }
        PanelRequests.execute("PUT", "$baseUrl/api/ssh-keys/$id", json = payload.toString(), accessToken = accessToken)
    }

    suspend fun delete(id: Long) = withContext(Dispatchers.IO) {
        PanelRequests.execute("DELETE", "$baseUrl/api/ssh-keys/$id", accessToken = accessToken)
    }
}
