package com.daidai.daidai_app.data.model

/**
 * 通知渠道字段选项（select 控件的一个可选项）。
 *
 * 对齐 Go 端 `system_config_registry.go` 的 SystemConfigOption。
 */
data class NotificationFieldOption(
    val value: String,
    val label: String,
)

/**
 * 字段显示条件：当 `key` 字段的当前值命中 `values` 之一时才显示本字段。
 *
 * 对齐 Go 端 `notify_channel_registry.go` 的 NotifyFieldCondition。
 * 当前只有 wecom / wecom_app 两个渠道使用（按 msg_type 分支）。
 */
data class NotificationFieldShowWhen(
    val key: String,
    val values: List<String>,
)

/**
 * 通知渠道配置字段描述（对齐 Go 端 NotifyFieldDefinition 的 JSON 形状）。
 *
 * @param key 配置键（写入渠道 config 的键名）
 * @param label 表单标签
 * @param placeholder 输入占位提示
 * @param widget 控件类型：input / password / textarea / select
 * @param required 是否必填（口径与 Go 端一致：notifier.go 单独判空才为 true）
 * @param default 服务端留空时的回退值；有回退值的字段可用来预填
 * @param options select 控件的候选项
 * @param showWhen 非空时按依赖字段等值匹配控制显隐
 */
data class NotificationFieldSpec(
    val key: String,
    val label: String,
    val placeholder: String = "",
    val widget: String = "input",
    val required: Boolean = false,
    val default: String = "",
    val options: List<NotificationFieldOption> = emptyList(),
    val showWhen: NotificationFieldShowWhen? = null,
) {
    companion object {
        /** 从服务端 types 接口返回的单个字段 JSON 对象解析。 */
        fun fromJson(json: org.json.JSONObject): NotificationFieldSpec {
            val options = json.optJSONArray("options")?.let { arr ->
                (0 until arr.length()).mapNotNull { i ->
                    arr.optJSONObject(i)?.let {
                        NotificationFieldOption(
                            value = it.optString("value"),
                            label = it.optString("label"),
                        )
                    }
                }
            } ?: emptyList()
            val showWhen = json.optJSONObject("show_when")?.let {
                val values = it.optJSONArray("values")?.let { arr ->
                    (0 until arr.length()).map { idx -> arr.optString(idx) }
                } ?: emptyList()
                NotificationFieldShowWhen(key = it.optString("key"), values = values)
            }
            return NotificationFieldSpec(
                key = json.optString("key"),
                label = json.optString("label"),
                placeholder = json.optString("placeholder"),
                widget = json.optString("widget", "input"),
                required = json.optBoolean("required", false),
                default = json.optString("default"),
                options = options,
                showWhen = showWhen,
            )
        }
    }
}

/**
 * 通知渠道类型定义（对齐 Go 端 NotifyChannelDefinition 的 JSON 形状）。
 *
 * 与服务端 `GET /api/notifications/types` 返回的单个元素同构，
 * 本地 fallback 的 `/notifications/types` 接口也按此形状下发，
 * 因此 UI 无论是连远程 Go Core 还是本地 fallback 都渲染同一份表单。
 */
data class NotificationChannelType(
    val type: String,
    val name: String,
    val icon: String = "",
    val fields: List<NotificationFieldSpec> = emptyList(),
) {
    companion object {
        /** 从服务端 types 接口返回的单个 JSON 对象解析，失败时回退为最小默认值。 */
        fun fromJson(json: org.json.JSONObject): NotificationChannelType {
            val fields = json.optJSONArray("fields")?.let { arr ->
                (0 until arr.length()).mapNotNull { i ->
                    arr.optJSONObject(i)?.let { NotificationFieldSpec.fromJson(it) }
                }
            } ?: emptyList()
            return NotificationChannelType(
                type = json.optString("type"),
                name = json.optString("name"),
                icon = json.optString("icon"),
                fields = fields,
            )
        }
    }
}

/**
 * 通知渠道 schema 注册表 —— 与 Go 端 `panel/server/model/notify_channel_registry.go`
 * 的 22 个渠道逐一对齐（type / name / fields 及字段属性）。
 *
 * 这是本模块的单一事实来源：
 *  - 本地 fallback 的 `/notifications/types` 接口从这里生成 JSON；
 *  - Compose 配置表单按这里渲染字段；
 *  - schema 完整性由 `NotificationChannelSchemasTest` 兜底。
 *
 * 注意：`android_local` 是本地 Android 系统通知渠道（仅本地存在，Go 端没有），
 * 单独保留并放在列表首位。
 */
object NotificationChannelSchemas {

    /** 22 个与 Go 端对齐的服务端渠道（不含 android_local）。 */
    val serverChannels: List<NotificationChannelType> = listOf(
        NotificationChannelType(
            type = "webhook", name = "Webhook", icon = "webhook",
            fields = listOf(
                field("url", "Webhook URL", "https://example.com/webhook", required = true),
            ),
        ),
        NotificationChannelType(
            type = "email", name = "邮件", icon = "mail",
            // smtp_user 和 from 是二选一关系（Go 端两者都空才报错），smtp_pass 内网无认证中继可留空，
            // 所以这三个都不标 required，由 label 里的「(可选)」提示。
            fields = listOf(
                field("smtp_host", "SMTP 主机", "smtp.qq.com", required = true),
                field("smtp_port", "SMTP 端口", "465", required = true),
                select(
                    "smtp_ssl", "SSL 连接", "自动：465 端口启用",
                    listOf("auto" to "自动 (465 启用)", "true" to "启用 SSL", "false" to "关闭 SSL"),
                    default = "auto",
                ),
                field("smtp_user", "邮箱账号", "user@example.com"),
                password("smtp_pass", "邮箱密码/授权码", "SMTP 授权码"),
                field("to", "收件人", "多个收件人用逗号分隔", required = true),
                field("from", "发件人 (可选)", "留空则使用邮箱账号"),
            ),
        ),
        NotificationChannelType(
            type = "telegram", name = "Telegram", icon = "telegram",
            fields = listOf(
                field("token", "Bot Token", "从 @BotFather 获取", required = true),
                field("chat_id", "Chat ID", "聊天/群组 ID", required = true),
                field("message_thread_id", "Topic ID (可选)", "群组话题 ID，留空则发到默认话题"),
                field("api_host", "API 地址 (可选)", "自定义 API 地址，留空使用 https://api.telegram.org", default = "https://api.telegram.org"),
                field("proxy", "代理地址 (可选)", "http/socks5 代理地址"),
            ),
        ),
        NotificationChannelType(
            type = "dingtalk", name = "钉钉", icon = "dingtalk",
            fields = listOf(
                field("webhook", "Webhook URL", "https://oapi.dingtalk.com/robot/send?access_token=xxx", required = true),
                field("secret", "加签秘钥 (可选)", "安全设置中的 SEC 开头的秘钥"),
                select(
                    "msg_type", "消息类型", "选择钉钉机器人消息类型",
                    listOf("text" to "文本", "markdown" to "Markdown"),
                    default = "markdown",
                ),
            ),
        ),
        NotificationChannelType(
            type = "wecom", name = "企业微信机器人", icon = "wecom",
            fields = listOf(
                field("webhook", "Webhook URL", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx", required = true),
                select(
                    "msg_type", "消息类型", "选择企业微信机器人消息类型",
                    listOf(
                        "text" to "文本",
                        "markdown" to "Markdown",
                        "markdown_v2" to "Markdown V2",
                        "image" to "图片",
                        "news" to "图文",
                        "template_card" to "模版卡片",
                    ),
                    default = "text",
                ),
                textarea("content_template", "文本模板", "支持 {{title}} 和 {{content}}，留空默认 {{title}}\\n{{content}}")
                    .shownWhen("msg_type", "text"),
                textarea("mentioned_list", "提醒成员 (可选)", "多个成员用逗号、分号或换行分隔，可填 @all")
                    .shownWhen("msg_type", "text"),
                textarea("mentioned_mobile_list", "提醒手机号 (可选)", "多个手机号用逗号、分号或换行分隔，可填 @all")
                    .shownWhen("msg_type", "text"),
                textarea("content_template", "内容模板", "支持 {{title}} 和 {{content}} 占位符")
                    .shownWhen("msg_type", "markdown", "markdown_v2"),
                field("image_base64", "图片 Base64", "填写图片内容对应的 Base64 数据", required = true)
                    .shownWhen("msg_type", "image"),
                field("image_md5", "图片 MD5", "填写图片内容对应的 MD5 值", required = true)
                    .shownWhen("msg_type", "image"),
                textarea(
                    "news_articles", "图文 Articles(JSON)",
                    "[{\"title\":\"{{title}}\",\"description\":\"{{content}}\",\"url\":\"https://example.com\",\"picurl\":\"https://example.com/demo.png\"}]",
                    required = true,
                ).shownWhen("msg_type", "news"),
                textarea(
                    "template_card_payload", "卡片配置(JSON)",
                    "{\"card_type\":\"text_notice\",\"main_title\":{\"title\":\"{{title}}\",\"desc\":\"{{content}}\"}}",
                    required = true,
                ).shownWhen("msg_type", "template_card"),
            ),
        ),
        NotificationChannelType(
            type = "wecom_app", name = "企业微信应用", icon = "wecom",
            fields = listOf(
                field("corp_id", "企业 ID", "企业微信 CorpID", required = true),
                password("secret", "应用 Secret", "应用 Secret", required = true),
                field("agent_id", "Agent ID", "应用 AgentId", required = true),
                field("base_url", "反代基础地址 (可选)", "留空使用 https://qyapi.weixin.qq.com，也可填你的 Nginx 反代地址"),
                field("to_user", "成员账号 (可选)", "多个成员用 | 分隔，留空默认 @all"),
                field("to_party", "部门 ID (可选)", "多个部门用 | 分隔"),
                field("to_tag", "标签 ID (可选)", "多个标签用 | 分隔"),
                select(
                    "msg_type", "消息类型", "选择企业微信应用消息类型",
                    listOf(
                        "text" to "文本",
                        "markdown" to "Markdown",
                        "image" to "图片",
                        "file" to "文件",
                        "video" to "视频",
                        "news" to "图文",
                        "mpnews" to "图文消息 (mpnews)",
                        "template_card" to "模版卡片",
                    ),
                    default = "text",
                ),
                textarea("content_template", "内容模板", "支持 {{title}} 和 {{content}} 占位符")
                    .shownWhen("msg_type", "text", "markdown"),
                field("media_id", "Media ID", "调用上传临时素材接口后得到的 media_id", required = true)
                    .shownWhen("msg_type", "image", "file", "video"),
                textarea(
                    "news_articles", "图文 Articles(JSON)",
                    "[{\"title\":\"{{title}}\",\"description\":\"{{content}}\",\"url\":\"https://example.com\",\"picurl\":\"https://example.com/demo.png\"}]",
                    required = true,
                ).shownWhen("msg_type", "news"),
                textarea(
                    "mpnews_articles", "图文消息 Articles(JSON)",
                    "[{\"title\":\"{{title}}\",\"thumb_media_id\":\"MEDIA_ID\",\"author\":\"Author\",\"content_source_url\":\"https://example.com\",\"content\":\"<p>{{content}}</p>\",\"digest\":\"Digest description\"}]",
                    required = true,
                ).shownWhen("msg_type", "mpnews"),
                textarea(
                    "template_card_payload", "卡片配置(JSON)",
                    "{\"card_type\":\"text_notice\",\"main_title\":{\"title\":\"{{title}}\",\"desc\":\"{{content}}\"}}",
                    required = true,
                ).shownWhen("msg_type", "template_card"),
                select(
                    "safe", "保密消息", "默认 0",
                    listOf("0" to "否 (0)", "1" to "是 (1)", "2" to "仅企业内分享 (2)"),
                    default = "0",
                ),
                select("enable_id_trans", "ID 转译", "默认 0", listOf("0" to "关闭 (0)", "1" to "开启 (1)"), default = "0"),
                select("enable_duplicate_check", "重复检查", "默认 0", listOf("0" to "关闭 (0)", "1" to "开启 (1)"), default = "0"),
                field("duplicate_check_interval", "去重间隔(秒)", "默认 1800，最大 14400", default = "1800"),
            ),
        ),
        NotificationChannelType(
            type = "bark", name = "Bark", icon = "bark",
            fields = listOf(
                field("key", "Device Key", "打开 Bark App 复制推送地址中的 Key，如 https://api.day.app/xxxxxx 中的 xxxxxx", required = true),
                field("server", "服务器 (可选)", "默认 https://api.day.app", default = "https://api.day.app"),
                field("sound", "推送声音 (可选)", "如 birdsong，留空使用默认"),
                field("group", "推送分组 (可选)", "消息分组名称"),
                field("icon", "图标 URL (可选)", "https://example.com/icon.png"),
                select(
                    "level", "时效性 (可选)", "推送优先级",
                    listOf("active" to "默认 (active)", "timeSensitive" to "时效性 (timeSensitive)", "passive" to "被动 (passive)"),
                ),
                field("url", "跳转 URL (可选)", "点击通知后跳转的链接"),
            ),
        ),
        NotificationChannelType(
            type = "pushplus", name = "PushPlus", icon = "pushplus",
            fields = listOf(
                field("token", "Token", "PushPlus 用户 Token", required = true),
                field("topic", "群组编码 (可选)", "一对多推送时的群组编码"),
                select(
                    "template", "模板 (可选)", "消息模板",
                    listOf("html" to "默认 (html)", "json" to "JSON", "txt" to "纯文本", "markdown" to "Markdown"),
                ),
                select(
                    "channel", "发送渠道 (可选)", "留空按 PushPlus 账号默认（微信公众号）",
                    listOf(
                        "wechat" to "微信公众号 (wechat)",
                        "app" to "App (app)",
                        "extension" to "浏览器扩展 (extension)",
                        "webhook" to "第三方 Webhook (webhook)",
                        "clawbot" to "微信 ClawBot (clawbot)",
                        "cp" to "企业微信应用 (cp)",
                        "mail" to "邮箱 (mail)",
                        "sms" to "短信 (sms，消耗 10 积分/条)",
                        "voice" to "语音 (voice，消耗 30 积分/条)",
                    ),
                ),
                field("option", "渠道编码", "webhook 渠道填 webhook 编码，企业微信应用渠道填自定义应用编码")
                    .shownWhen("channel", "webhook", "cp"),
            ),
        ),
        NotificationChannelType(
            type = "serverchan", name = "Server酱", icon = "serverchan",
            fields = listOf(
                field("key", "SendKey", "Server酱的 SendKey (SCT...)"),
            ),
        ),
        NotificationChannelType(
            type = "feishu", name = "飞书", icon = "feishu",
            fields = listOf(
                field("webhook", "Webhook URL", "https://open.feishu.cn/open-apis/bot/v2/hook/xxx", required = true),
                field("secret", "加签秘钥 (可选)", "安全设置中的签名校验秘钥"),
            ),
        ),
        NotificationChannelType(
            type = "gotify", name = "Gotify", icon = "gotify",
            fields = listOf(
                field("server", "服务器地址", "https://gotify.example.com", required = true),
                field("token", "App Token", "Gotify 应用 Token", required = true),
                field("priority", "优先级 (可选)", "0-10，默认 5", default = "5"),
            ),
        ),
        NotificationChannelType(
            type = "pushdeer", name = "PushDeer", icon = "pushdeer",
            fields = listOf(
                field("key", "PushKey", "PushDeer 的 PushKey"),
                field("server", "服务器 (可选)", "默认 https://api2.pushdeer.com", default = "https://api2.pushdeer.com"),
            ),
        ),
        NotificationChannelType(
            type = "pushme", name = "PushMe", icon = "pushme",
            fields = listOf(
                field("key", "PushMe Key", "PushMe 的 push_key", required = true),
                field("server", "接口地址 (可选)", "默认 https://push.i-i.me", default = "https://push.i-i.me"),
                field("message_type", "消息类型 (可选)", "按 PushMe 支持的 type 值填写"),
            ),
        ),
        NotificationChannelType(
            type = "chanify", name = "Chanify", icon = "chanify",
            fields = listOf(
                field("token", "Token", "Chanify 设备 Token"),
                field("server", "服务器 (可选)", "默认 https://api.chanify.net", default = "https://api.chanify.net"),
            ),
        ),
        NotificationChannelType(
            type = "igot", name = "iGot", icon = "igot",
            fields = listOf(
                field("key", "Key", "iGot 推送 Key"),
            ),
        ),
        NotificationChannelType(
            type = "qmsg", name = "Qmsg", icon = "qmsg",
            fields = listOf(
                field("key", "Qmsg Key", "Qmsg 酱的 Key", required = true),
                select("mode", "发送模式", "选择 send 或 group", listOf("send" to "私聊/默认 (send)", "group" to "群发 (group)"), default = "send"),
                field("qq", "QQ 号/群号 (可选)", "留空则按 Qmsg 端默认配置发送"),
            ),
        ),
        NotificationChannelType(
            type = "pushover", name = "Pushover", icon = "pushover",
            fields = listOf(
                field("token", "API Token", "应用 API Token"),
                field("user", "User Key", "用户 Key"),
            ),
        ),
        NotificationChannelType(
            type = "discord", name = "Discord", icon = "discord",
            fields = listOf(
                field("webhook", "Webhook URL", "https://discord.com/api/webhooks/..."),
            ),
        ),
        NotificationChannelType(
            type = "slack", name = "Slack", icon = "slack",
            fields = listOf(
                field("webhook", "Webhook URL", "https://hooks.slack.com/services/..."),
            ),
        ),
        NotificationChannelType(
            type = "ntfy", name = "ntfy", icon = "ntfy",
            fields = listOf(
                field("topic", "Topic", "订阅主题名称", required = true),
                field("server", "服务器 (可选)", "默认 https://ntfy.sh", default = "https://ntfy.sh"),
                field("token", "Token (可选)", "访问令牌，用于私有主题"),
                select(
                    "priority", "优先级 (可选)", "消息优先级",
                    listOf("1" to "最低 (1)", "2" to "低 (2)", "3" to "默认 (3)", "4" to "高 (4)", "5" to "紧急 (5)"),
                ),
            ),
        ),
        NotificationChannelType(
            type = "wxpusher", name = "WxPusher / ClawBot(iLink)", icon = "wxpusher",
            // uids 和 topic_ids 是「至少填一个」关系，两个都不标 required。
            fields = listOf(
                field("app_token", "App Token", "WxPusher 的 appToken", required = true),
                textarea("uids", "UID 列表 (可选)", "多个 UID 可用分号、逗号或换行分隔"),
                textarea("topic_ids", "Topic ID 列表 (可选)", "多个 Topic ID 可用分号、逗号或换行分隔"),
                select("content_type", "内容类型 (可选)", "默认文本消息", listOf("1" to "文本 (1)", "2" to "HTML (2)", "3" to "Markdown (3)"), default = "1"),
                field("url", "原文链接 (可选)", "消息详情页跳转地址"),
                select("verify_pay_type", "付费校验 (可选)", "默认不校验", listOf("0" to "不校验 (0)", "1" to "仅付费用户 (1)", "2" to "仅未订阅/已过期 (2)")),
                field("server", "接口地址 (可选)", "默认 https://wxpusher.zjiecode.com/api/send/message", default = "https://wxpusher.zjiecode.com/api/send/message"),
            ),
        ),
        NotificationChannelType(
            type = "custom", name = "自定义", icon = "custom",
            // 自定义渠道：headers / body 的值是 JSON 文本，但写入 config 时必须是字符串。
            fields = listOf(
                field("url", "URL", "https://example.com/api/notify"),
                select("method", "Method", "请求方法", listOf("POST" to "POST", "GET" to "GET", "PUT" to "PUT"), default = "POST"),
                field("content_type", "Content-Type", "默认 application/json", default = "application/json"),
                textarea("headers", "Headers (JSON)", "{\"Authorization\": \"Bearer xxx\"}"),
                textarea("body", "Body 模板", "使用 {{title}} 和 {{content}} 作为占位符", default = "{\"title\":\"{{title}}\",\"content\":\"{{content}}\"}"),
            ),
        ),
    )

    /** 本地 Android 系统通知渠道（仅本地 fallback 存在，Go 端无此类型）。 */
    val localChannel: NotificationChannelType = NotificationChannelType(
        type = "android_local", name = "Android 本地通知", icon = "android_local",
        fields = emptyList(),
    )

    /** 全部渠道（本地 1 个 + 服务端 22 个），本地 fallback 的 types 接口按此顺序下发。 */
    val allChannels: List<NotificationChannelType> = listOf(localChannel) + serverChannels

    /** 服务端 22 渠道的类型集合。 */
    val serverTypeSet: Set<String> = serverChannels.map { it.type }.toSet()

    /** 全部渠道的类型集合（含 android_local）。 */
    val allTypeSet: Set<String> = allChannels.map { it.type }.toSet()

    /** 按类型取渠道定义，未知类型返回 null。 */
    fun byType(type: String): NotificationChannelType? =
        allChannels.firstOrNull { it.type.equals(type, ignoreCase = true) }

    private fun field(key: String, label: String, placeholder: String = "", required: Boolean = false, default: String = "") =
        NotificationFieldSpec(key = key, label = label, placeholder = placeholder, widget = "input", required = required, default = default)

    private fun password(key: String, label: String, placeholder: String = "", required: Boolean = false, default: String = "") =
        NotificationFieldSpec(key = key, label = label, placeholder = placeholder, widget = "password", required = required, default = default)

    private fun textarea(key: String, label: String, placeholder: String = "", required: Boolean = false, default: String = "") =
        NotificationFieldSpec(key = key, label = label, placeholder = placeholder, widget = "textarea", required = required, default = default)

    private fun select(
        key: String,
        label: String,
        placeholder: String = "",
        pairs: List<Pair<String, String>>,
        default: String = "",
    ) = NotificationFieldSpec(
        key = key,
        label = label,
        placeholder = placeholder,
        widget = "select",
        required = false,
        default = default,
        options = pairs.map { NotificationFieldOption(it.first, it.second) },
    )
}

/** DSL：给字段追加显示条件（同键字段在 show_when 互斥的前提下允许重复）。 */
private fun NotificationFieldSpec.shownWhen(key: String, vararg values: String): NotificationFieldSpec =
    copy(showWhen = NotificationFieldShowWhen(key = key, values = values.toList()))
