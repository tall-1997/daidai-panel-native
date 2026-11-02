package model

import (
	"strconv"
	"strings"
	"time"
)

// WecomTrigger 是一套「企业微信自建应用 → 面板」的回调接入配置（issue #145，v3.3.2）。
//
// 刻意独立成表，不塞进 notify_channels：
//   - notify_channels 的 config 键与 service/notifier.go 由 notifier_schema_binding_test.go
//     双向绑死，凡是 notifier.go 不读的键都会被判成「假字段」报红；
//   - 方向也是反的：通知渠道是出站（面板去登录企业微信发消息），这里是入站
//     （企业微信来敲面板，面板要验它的身份），两套凭据的语义完全不同。
//
// 触发执行用的凭据不落这张表：走 OpenAppID 指向的开放 API 应用（app_key / app_secret），
// 复用现成的 scope 校验、限流与 ApiCallLog 审计，不另建一套无审计的旁路。
type WecomTrigger struct {
	ID   uint   `gorm:"primarykey" json:"id"`
	Name string `gorm:"size:128;not null" json:"name"`
	// CorpID 用来校验解密后明文尾部的 receiveid，对不上一律丢弃 ——
	// 这是这条公网回调唯一的身份锚点，不能留任何「没填就跳过」的口子。
	CorpID string `gorm:"size:64;not null" json:"corp_id"`
	// AgentID 存字符串而不是数字：企业微信后台给的就是一串数字，
	// 面板别处（notify_channel_registry 的 wecom_app 渠道）也是按字符串存的，保持一致。
	AgentID string `gorm:"size:32;default:''" json:"agent_id"`
	// CallbackToken / EncodingAESKey 是企业微信后台「接收消息服务器配置」里那两项。
	// 两者都不下发给客户端（json:"-"）：泄漏任意一个都等于把这条公网入口交出去。
	CallbackToken  string `gorm:"size:128;not null" json:"-"`
	EncodingAESKey string `gorm:"size:64;not null" json:"-"`
	// OpenAppID 指向回放 /tasks 接口时用的开放 API 应用，它的 scopes 至少要含 tasks。
	OpenAppID uint `gorm:"index" json:"open_app_id"`
	// TaskWhitelist 是允许被触发的任务白名单，用逗号（半角或全角）或换行分隔。
	// 每一项既可以写任务 ID 也可以写任务名，**指令用哪种形式就按哪种形式匹配**：
	// 指令是「run 12」就拿 12 去比，指令是「运行 签到任务」就拿名字去比，
	// 两种触发方式都想放行就两条都写上。
	// 留空 = 不限制（默认值；这样升级后的行为与没有白名单概念时一致）。
	TaskWhitelist string    `gorm:"size:1024;default:''" json:"task_whitelist"`
	Enabled       bool      `gorm:"default:true" json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (WecomTrigger) TableName() string {
	return "wecom_triggers"
}

func (t *WecomTrigger) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":             t.ID,
		"name":           t.Name,
		"corp_id":        t.CorpID,
		"agent_id":       t.AgentID,
		"open_app_id":    t.OpenAppID,
		"task_whitelist": t.TaskWhitelist,
		"enabled":        t.Enabled,
		"created_at":     t.CreatedAt,
		"updated_at":     t.UpdatedAt,
	}
}

// AllowsTask 判断某条指令要触发的任务是否在白名单内。
// taskID 为 0 表示这次是按名称触发，taskName 为空表示按 ID 触发，两者只会有一个有值。
func (t *WecomTrigger) AllowsTask(taskID uint, taskName string) bool {
	entries := splitWecomTaskWhitelist(t.TaskWhitelist)
	if len(entries) == 0 {
		// 留空 = 不限制。这是默认值，别改成「留空 = 全部禁止」：
		// 那会让所有升级上来的（以及刚建好还没来得及填的）配置静默失效。
		return true
	}

	id := ""
	if taskID > 0 {
		id = strconv.FormatUint(uint64(taskID), 10)
	}
	name := strings.TrimSpace(taskName)
	for _, entry := range entries {
		if id != "" && entry == id {
			return true
		}
		if name != "" && entry == name {
			return true
		}
	}
	return false
}

// splitWecomTaskWhitelist 按半角逗号、全角逗号与换行切分白名单，并丢掉空项。
// 全角逗号必须认：用户在手机上填这个框时输入法默认就是全角。
func splitWecomTaskWhitelist(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == '\n' || r == '\r'
	})
	entries := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			entries = append(entries, trimmed)
		}
	}
	return entries
}
