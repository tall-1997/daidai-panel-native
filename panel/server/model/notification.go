package model

import (
	"strings"
	"time"
)

// 通知渠道的推送范围（push_scope）。
//
// default 表示参与广播：调用方没有指定渠道时（资源告警、登录通知、静默更新结果、
// 未绑定渠道的任务通知、脚本里不带 channel_id 的 notify）会命中这些渠道。
//
// bound 表示不参与广播：只有被任务或调用方显式绑定（传了 channel_id / channel_ids）时才推送。
// 「一个脚本对应一个通知」靠的就是这个 —— 把专用渠道设成 bound，广播就再也污染不到它。
//
// 为什么必须是字符串枚举，不能做成 IsDefault bool：
// 同一张表的 `Enabled bool gorm:"default:true"` 已经有一个踩过的活体坑 —— GORM 把 false
// 当零值从 INSERT 里省略，DB 侧的 DEFAULT true 反而生效，于是备份恢复
// （service/backup_runtime.go 的 restoreNotifyChannels 用的就是 tx.Create）会把一条禁用渠道
// 静默写回启用状态。回归测试为了造出禁用渠道，被迫写成 Select("*").Create + 单独 Update。
// bool 版本会原样复刻这个坑：任何一处漏传都会把 bound 悄悄翻成 default，
// 表现是「用户设的专用渠道莫名其妙又开始收广播了」。
// 字符串的 Go 零值 "" 正好等价「默认推送」，漏填只会退回升级前的老行为，方向是安全的。
const (
	NotifyPushScopeDefault = "default"
	NotifyPushScopeBound   = "bound"
)

// NormalizeNotifyPushScope 归一 push_scope 取值。
//
// 空串一律按「默认推送」处理，覆盖三种真实来源：
//   - 老库补列、手工改库等历史路径留下的空值行；
//   - 备份文件里根本没有这个键的老 manifest；
//   - 独立发版的 APP 提交的、不带这个字段的请求。
//
// 第二个返回值为 false 表示取值非法，由调用方决定报 400 还是回落，这里不擅自纠正 ——
// 悄悄把拼错的 "bind" 当成 default，等于把用户的隔离意图反向执行。
func NormalizeNotifyPushScope(raw string) (string, bool) {
	switch strings.TrimSpace(raw) {
	case "", NotifyPushScopeDefault:
		return NotifyPushScopeDefault, true
	case NotifyPushScopeBound:
		return NotifyPushScopeBound, true
	default:
		return "", false
	}
}

// NotifyChannel 的 PushScope 取值见上方常量注释。
//
// 它带了 gorm 的 DB 默认值，而这在这里是安全的：GORM 会把零值 "" 换成 'default' 再落库，
// 与「空串按默认处理」是同一个语义，不会反转用户意图。
// 同一个机制作用在 Enabled bool 上就是个坑（false 被换成 true），差别只在于
// 字符串的零值方向是对的、bool 的零值方向是反的 —— 这正是选字符串枚举的理由。
type NotifyChannel struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	Name           string     `gorm:"size:128;not null" json:"name"`
	Type           string     `gorm:"size:32;not null" json:"type"`
	Config         string     `gorm:"type:text;default:'{}'" json:"-"`
	PushScope      string     `gorm:"size:16;not null;default:'default'" json:"push_scope"`
	Enabled        bool       `gorm:"default:true" json:"enabled"`
	TodaySendCount int        `gorm:"default:0" json:"-"`
	TodaySendDate  string     `gorm:"size:10;default:''" json:"-"`
	LastTestAt     *time.Time `json:"last_test_at"`
	LastTestStatus string     `gorm:"size:16;default:''" json:"last_test_status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (NotifyChannel) TableName() string {
	return "notify_channels"
}

// EffectivePushScope 返回归一后的推送范围，供序列化与展示使用。
// 非法值（理论上进不来，两个写入口都做了白名单）按 default 展示，与广播 SQL 的
// `COALESCE(push_scope,'') <> 'bound'` 保持同向：只有明确写着 bound 才排除出广播。
func (n *NotifyChannel) EffectivePushScope() string {
	if scope, ok := NormalizeNotifyPushScope(n.PushScope); ok {
		return scope
	}
	return NotifyPushScopeDefault
}

func (n *NotifyChannel) ToDict() map[string]interface{} {
	todaySendCount := 0
	if n.TodaySendDate == time.Now().Format("2006-01-02") {
		todaySendCount = n.TodaySendCount
	}

	return map[string]interface{}{
		"id":               n.ID,
		"name":             n.Name,
		"type":             n.Type,
		"config":           n.Config,
		"push_scope":       n.EffectivePushScope(),
		"enabled":          n.Enabled,
		"today_send_count": todaySendCount,
		"last_test_at":     n.LastTestAt,
		"last_test_status": n.LastTestStatus,
		"created_at":       n.CreatedAt,
		"updated_at":       n.UpdatedAt,
	}
}
