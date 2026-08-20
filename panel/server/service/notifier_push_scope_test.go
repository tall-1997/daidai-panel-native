package service

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestBroadcastWithoutDefaultChannelLogsWarning 锁住「严格不兜底 + 留痕」这条产品决策。
//
// 广播 0 命中在改造前是完全静默的：这条分支上的调用方（资源告警、登录通知、静默更新结果、
// 未绑定渠道的任务通知）全都不看返回值，也没有任何日志。用户只要把所有渠道都设成
// 「绑定推送」，这些通知就会全部人间蒸发且零线索 —— 这行 warn 是唯一可查的痕迹。
func TestBroadcastWithoutDefaultChannelLogsWarning(t *testing.T) {
	testutil.SetupTestEnv(t)

	channel := &model.NotifyChannel{
		Name:      "脚本专用渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/webhook"}`,
		PushScope: model.NotifyPushScopeBound,
		Enabled:   true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	var logBuf bytes.Buffer
	originalWriter := log.Default().Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(originalWriter) })

	// 走异步广播入口：它取渠道和判空都是同步的，只有真正发送才 go 出去，
	// 所以这里不会有竞态 —— 一条都没取到，压根不会起 goroutine。
	SendNotification("资源告警", "CPU 使用率 95%")

	output := logBuf.String()
	if !strings.Contains(output, "notification broadcast skipped") {
		t.Fatalf("广播 0 命中时必须留一行 warn，实际日志: %q", output)
	}
}

// TestBroadcastHitsOnlyDefaultScopeChannels 直接锁 loadEnabledNotificationChannels 的筛选结果。
//
// handler 那边是端到端验证，这里是筛选点本身的单元验证：定向忽略 push_scope、广播排除 bound、
// 空串（历史行）仍参与广播。三条缺一不可。
func TestBroadcastHitsOnlyDefaultScopeChannels(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultChannel := &model.NotifyChannel{
		Name:      "广播渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/a"}`,
		PushScope: model.NotifyPushScopeDefault,
		Enabled:   true,
	}
	boundChannel := &model.NotifyChannel{
		Name:      "脚本专用渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/b"}`,
		PushScope: model.NotifyPushScopeBound,
		Enabled:   true,
	}
	legacyChannel := &model.NotifyChannel{
		Name:      "历史渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/c"}`,
		PushScope: model.NotifyPushScopeDefault,
		Enabled:   true,
	}
	disabledChannel := &model.NotifyChannel{
		Name:      "禁用渠道",
		Type:      "webhook",
		Config:    `{"url":"https://example.com/d"}`,
		PushScope: model.NotifyPushScopeDefault,
		Enabled:   true,
	}
	for _, ch := range []*model.NotifyChannel{defaultChannel, boundChannel, legacyChannel, disabledChannel} {
		if err := database.DB.Create(ch).Error; err != nil {
			t.Fatalf("create notification channel %q: %v", ch.Name, err)
		}
	}
	// GORM 的 default tag 会把零值替换成 'default'，历史空串形态只能用原生 SQL 造。
	if err := database.DB.Exec("UPDATE notify_channels SET push_scope = '' WHERE id = ?", legacyChannel.ID).Error; err != nil {
		t.Fatalf("blank out push_scope: %v", err)
	}
	// 禁用渠道用 Update 单独改，绕开 GORM 把 false 当零值省略的老坑。
	if err := database.DB.Model(disabledChannel).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable notification channel: %v", err)
	}

	broadcast, err := loadEnabledNotificationChannels(nil)
	if err != nil {
		t.Fatalf("load broadcast channels: %v", err)
	}
	got := map[string]bool{}
	for _, ch := range broadcast {
		got[ch.Name] = true
	}
	if !got["广播渠道"] {
		t.Fatalf("默认推送渠道必须参与广播，实际命中 %#v", got)
	}
	if !got["历史渠道"] {
		t.Fatalf("push_scope 为空串的历史行必须仍参与广播（过滤条件不能写成 = 'default'），实际命中 %#v", got)
	}
	if got["脚本专用渠道"] {
		t.Fatalf("绑定推送渠道不应参与广播，实际命中 %#v", got)
	}
	if got["禁用渠道"] {
		t.Fatalf("禁用渠道不应参与广播，实际命中 %#v", got)
	}

	targeted, err := loadEnabledNotificationChannels([]uint{boundChannel.ID})
	if err != nil {
		t.Fatalf("load targeted channels: %v", err)
	}
	if len(targeted) != 1 || targeted[0].ID != boundChannel.ID {
		t.Fatalf("定向分支必须完全忽略 push_scope，实际 %#v", targeted)
	}
}

// TestBackupRoundTripPreservesPushScope 锁住备份 / 恢复链路。
//
// push_scope 在备份侧一共有四处手抄（BackupNotifyChannel 结构体、采集、旧版 manifest 转换、
// 恢复写库），漏一处的表现是「备份还原后所有渠道退回默认推送」，只有真做一次导出导入才发现。
// 这里刻意经过一次真实的 JSON 往返，json tag 漏了同样会挂。
func TestBackupRoundTripPreservesPushScope(t *testing.T) {
	testutil.SetupTestEnv(t)

	channels := []*model.NotifyChannel{
		{
			Name:      "广播渠道",
			Type:      "webhook",
			Config:    `{"url":"https://example.com/a"}`,
			PushScope: model.NotifyPushScopeDefault,
			Enabled:   true,
		},
		{
			Name:      "脚本专用渠道",
			Type:      "webhook",
			Config:    `{"url":"https://example.com/b"}`,
			PushScope: model.NotifyPushScopeBound,
			Enabled:   true,
		},
	}
	for _, ch := range channels {
		if err := database.DB.Create(ch).Error; err != nil {
			t.Fatalf("create notification channel %q: %v", ch.Name, err)
		}
	}

	bundle, err := snapshotConfigBundle()
	if err != nil {
		t.Fatalf("snapshot config bundle: %v", err)
	}

	raw, err := json.Marshal(bundle.NotifyChannels)
	if err != nil {
		t.Fatalf("marshal notify channels: %v", err)
	}
	var decoded []BackupNotifyChannel
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal notify channels: %v", err)
	}

	if err := database.DB.Exec("DELETE FROM notify_channels").Error; err != nil {
		t.Fatalf("clear notify channels: %v", err)
	}
	if _, err := restoreNotifyChannels(database.DB, decoded); err != nil {
		t.Fatalf("restore notify channels: %v", err)
	}

	var restored []model.NotifyChannel
	if err := database.DB.Order("name ASC").Find(&restored).Error; err != nil {
		t.Fatalf("reload notify channels: %v", err)
	}
	scopes := map[string]string{}
	for _, ch := range restored {
		scopes[ch.Name] = ch.PushScope
	}
	if scopes["广播渠道"] != model.NotifyPushScopeDefault {
		t.Fatalf("恢复后默认推送渠道应保持 default，实际 %q", scopes["广播渠道"])
	}
	if scopes["脚本专用渠道"] != model.NotifyPushScopeBound {
		t.Fatalf("恢复后绑定推送标记必须保留，实际 %q", scopes["脚本专用渠道"])
	}

	// 老备份没有 push_scope 键：恢复后必须落成 default，不能变成空串或非法值。
	if err := database.DB.Exec("DELETE FROM notify_channels").Error; err != nil {
		t.Fatalf("clear notify channels: %v", err)
	}
	if _, err := restoreNotifyChannels(database.DB, []BackupNotifyChannel{{
		Name:    "老备份渠道",
		Type:    "webhook",
		Config:  `{"url":"https://example.com/legacy"}`,
		Enabled: true,
	}}); err != nil {
		t.Fatalf("restore legacy notify channel: %v", err)
	}
	var legacy model.NotifyChannel
	if err := database.DB.Where("name = ?", "老备份渠道").First(&legacy).Error; err != nil {
		t.Fatalf("reload legacy channel: %v", err)
	}
	if legacy.PushScope != model.NotifyPushScopeDefault {
		t.Fatalf("老备份恢复后应落成 default，实际 %q", legacy.PushScope)
	}
}
