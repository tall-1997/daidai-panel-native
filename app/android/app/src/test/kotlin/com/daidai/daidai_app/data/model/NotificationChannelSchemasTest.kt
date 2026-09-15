package com.daidai.daidai_app.data.model

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 通知渠道 schema 完整性测试（F3）。
 *
 * 服务端推送渠道注册表 [NotificationChannelSchemas] 必须与 Go 端
 * `panel/server/model/notify_channel_registry.go` 的 22 渠道保持逐一对齐：
 *  - 恰好 22 个服务端渠道（android_local 是本机通知，单列不算）；
 *  - 渠道类型唯一（不能重复注册导致本地 fallback 的 types 接口出现两条同名）；
 *  - 每个渠道都有非空名称；
 *  - 必填字段（required=true）必须有非空 label（表单要能显示）；
 *  - 字段键在渠道内唯一（show_when 互斥的同键重复除外，那是 wecom/wecom_app
 *    的 msg_type 分支声明，Go 端允许且由互斥测试兜底）。
 */
class NotificationChannelSchemasTest {

    private val expectedServerTypes = setOf(
        "webhook", "email", "telegram", "dingtalk", "wecom", "wecom_app",
        "bark", "pushplus", "serverchan", "feishu", "gotify", "pushdeer",
        "pushme", "chanify", "igot", "qmsg", "pushover", "discord", "slack",
        "ntfy", "wxpusher", "custom",
    )

    @Test
    fun `serverChannels covers exactly the 22 go-backend channels`() {
        val actual = NotificationChannelSchemas.serverChannels.map { it.type }.toSet()
        assertEquals("服务端渠道类型集合应与 Go 端 22 渠道一致", expectedServerTypes, actual)
        assertEquals("服务端渠道数量应为 22", 22, actual.size)
    }

    @Test
    fun `allChannels prepends android_local without duplicating server types`() {
        val all = NotificationChannelSchemas.allChannels
        assertEquals(23, all.size)
        assertEquals("android_local", all.first().type)
        val types = all.map { it.type }
        assertEquals("渠道类型应唯一（含 android_local）", types.size, types.toSet().size)
    }

    @Test
    fun `every channel has a non-blank name`() {
        NotificationChannelSchemas.allChannels.forEach { channel ->
            assertFalse("渠道 ${channel.type} 名称不能为空", channel.name.isBlank())
        }
    }

    @Test
    fun `every required field has a label and a non-blank key`() {
        NotificationChannelSchemas.serverChannels.forEach { channel ->
            channel.fields.forEach { field ->
                assertFalse("渠道 ${channel.type} 字段 key 不能为空", field.key.isBlank())
                if (field.required) {
                    assertFalse("渠道 ${channel.type} 必填字段 ${field.key} 必须有 label", field.label.isBlank())
                }
            }
        }
    }

    @Test
    fun `field keys are unique within a channel`() {
        NotificationChannelSchemas.serverChannels.forEach { channel ->
            val keys = channel.fields.map { it.key }
            val dup = keys.groupBy { it }.filterValues { it.size > 1 }.keys
            // Go 端允许同键按 show_when 互斥重复（wecom / wecom_app 的 content_template），
            // 但其余重复应被拦截。
            val allowedDup = if (channel.type == "wecom" || channel.type == "wecom_app") {
                setOf("content_template")
            } else emptySet()
            dup.forEach { key ->
                assertTrue(
                    "渠道 ${channel.type} 字段 $key 重复且不在允许集合中",
                    key in allowedDup,
                )
            }
        }
    }

    @Test
    fun `select fields have non-empty options`() {
        NotificationChannelSchemas.serverChannels.forEach { channel ->
            channel.fields.filter { it.widget == "select" }.forEach { field ->
                assertTrue(
                    "渠道 ${channel.type} 下拉字段 ${field.key} 必须有选项",
                    field.options.isNotEmpty(),
                )
            }
        }
    }

    @Test
    fun `parsing types json round-trips to the same type set`() {
        val raw = buildString {
            append("{\"data\":[")
            NotificationChannelSchemas.serverChannels.forEachIndexed { i, ch ->
                if (i > 0) append(",")
                append("{\"type\":\"${ch.type}\",\"name\":\"${ch.name}\",\"icon\":\"${ch.icon}\",\"fields\":[]}")
            }
            append("]}")
        }
        val parsed = parseNotificationChannelTypes(raw)
        assertEquals(
            expectedServerTypes,
            parsed.map { it.type }.toSet(),
        )
    }
}
