package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 这一组用例补的是「广播语义」这块长期的测试真空。
//
// 改造前 notification_send_regression_test.go 的 4 个用例全部显式传 channel_id（走定向分支），
// notifier_test.go 的 19 个用例都直调 sendToChannel 根本不经过筛选 ——
// 也就是说即使把 loadEnabledNotificationChannels 的过滤条件写反，测试也照样全绿。

// countingWebhook 起一个只统计命中次数的 webhook 服务端。
func countingWebhook(t *testing.T) (url string, hits *int32) {
	t.Helper()

	counter := new(int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(counter, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server.URL, counter
}

func mustCreatePushScopeChannel(t *testing.T, name, webhookURL, pushScope string) *model.NotifyChannel {
	t.Helper()

	channel := &model.NotifyChannel{
		Name:      name,
		Type:      "webhook",
		Config:    `{"url":"` + webhookURL + `"}`,
		PushScope: pushScope,
		Enabled:   true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel %q: %v", name, err)
	}
	return channel
}

func mustOperatorHeaders(t *testing.T, username string) map[string]string {
	t.Helper()

	user := testutil.MustCreateUser(t, username, "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	return map[string]string{"Authorization": "Bearer " + token}
}

// TestNotificationBroadcastOnlyHitsDefaultPushScopeChannels 是本次改动的核心验收：
// 不传 channel_id 时只发「默认推送」渠道，「绑定推送」渠道一条都收不到。
func TestNotificationBroadcastOnlyHitsDefaultPushScopeChannels(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultURL, defaultHits := countingWebhook(t)
	boundURL, boundHits := countingWebhook(t)

	mustCreatePushScopeChannel(t, "广播渠道", defaultURL, model.NotifyPushScopeDefault)
	mustCreatePushScopeChannel(t, "脚本专用渠道", boundURL, model.NotifyPushScopeBound)

	rec := performJSONRequest(
		newProtectedRouter(),
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"广播通知","content":"正文"}`,
		mustOperatorHeaders(t, "notify-broadcast-operator"),
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := atomic.LoadInt32(defaultHits); got != 1 {
		t.Fatalf("默认推送渠道应收到 1 条广播，实际 %d", got)
	}
	if got := atomic.LoadInt32(boundHits); got != 0 {
		t.Fatalf("绑定推送渠道不应参与广播，实际收到 %d 条", got)
	}
}

// TestNotificationSendTargetsBoundChannelExplicitly 断言定向分支完全忽略 push_scope。
// 少了这条，「绑定推送」渠道就成了永远发不出去的死渠道。
func TestNotificationSendTargetsBoundChannelExplicitly(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultURL, defaultHits := countingWebhook(t)
	boundURL, boundHits := countingWebhook(t)

	mustCreatePushScopeChannel(t, "广播渠道", defaultURL, model.NotifyPushScopeDefault)
	bound := mustCreatePushScopeChannel(t, "脚本专用渠道", boundURL, model.NotifyPushScopeBound)

	rec := performJSONRequest(
		newProtectedRouter(),
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"定向通知","content":"正文","channel_id":`+jsonNumber(bound.ID)+`}`,
		mustOperatorHeaders(t, "notify-targeted-operator"),
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if got := atomic.LoadInt32(boundHits); got != 1 {
		t.Fatalf("被显式指定的绑定推送渠道应收到 1 条，实际 %d", got)
	}
	if got := atomic.LoadInt32(defaultHits); got != 0 {
		t.Fatalf("定向发送不应波及其它渠道，默认推送渠道实际收到 %d 条", got)
	}
}

// TestNotificationTestButtonWorksForBoundChannel：渠道测试按钮走 SendNotificationToChannel，
// 完全绕过筛选（连 enabled 都不看），所以 push_scope 也不该拦它 —— 否则用户根本没法验证配置。
func TestNotificationTestButtonWorksForBoundChannel(t *testing.T) {
	testutil.SetupTestEnv(t)

	boundURL, boundHits := countingWebhook(t)
	bound := mustCreatePushScopeChannel(t, "脚本专用渠道", boundURL, model.NotifyPushScopeBound)

	rec := performRequest(
		newProtectedRouter(),
		http.MethodPost,
		"/api/v1/notifications/"+jsonNumber(bound.ID)+"/test",
		mustNotificationAdminHeaders(t, "notify-bound-test-admin"),
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(boundHits); got != 1 {
		t.Fatalf("绑定推送渠道的测试按钮应能发出 1 条，实际 %d", got)
	}
}

// TestNotificationBroadcastIncludesLegacyBlankPushScopeRow 锁住过滤条件必须是
// 「不等于 bound」而不是「等于 default」。
//
// 写成等值比较时，push_scope 为空串 / NULL 的历史行会静默退出广播，
// 表现成「升级之后突然一条通知都收不到」，而且没有任何报错可查。
func TestNotificationBroadcastIncludesLegacyBlankPushScopeRow(t *testing.T) {
	testutil.SetupTestEnv(t)

	legacyURL, legacyHits := countingWebhook(t)
	legacy := mustCreatePushScopeChannel(t, "历史渠道", legacyURL, model.NotifyPushScopeDefault)

	// 直接用原生 SQL 把这一列打回空串：GORM 的 default tag 会把零值替换成 'default'，
	// 走 Create 造不出这种历史形态。
	if err := database.DB.Exec("UPDATE notify_channels SET push_scope = '' WHERE id = ?", legacy.ID).Error; err != nil {
		t.Fatalf("blank out push_scope: %v", err)
	}
	var stored model.NotifyChannel
	if err := database.DB.First(&stored, legacy.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if stored.PushScope != "" {
		t.Fatalf("前置条件不成立：push_scope 应为空串，实际 %q", stored.PushScope)
	}

	rec := performJSONRequest(
		newProtectedRouter(),
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"广播通知","content":"正文"}`,
		mustOperatorHeaders(t, "notify-legacy-operator"),
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(legacyHits); got != 1 {
		t.Fatalf("空串 push_scope 的历史行必须仍参与广播，实际收到 %d 条", got)
	}

	// NULL 行在两条建表路径下都不该出现（AutoMigrate 与 ALTER TABLE 都声明了 NOT NULL），
	// 但 SQL 里 `NULL <> 'bound'` 求值为 NULL（不成立），所以过滤条件仍用 COALESCE 兜了一层。
	// 这里试着写一次 NULL：写得进去就顺带验证，被约束挡住同样说明结果是安全的。
	if err := database.DB.Exec("UPDATE notify_channels SET push_scope = NULL WHERE id = ?", legacy.ID).Error; err != nil {
		return
	}
	rec = performJSONRequest(
		newProtectedRouter(),
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"广播通知","content":"正文"}`,
		mustOperatorHeaders(t, "notify-legacy-null-operator"),
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(legacyHits); got != 2 {
		t.Fatalf("NULL push_scope 的历史行必须仍参与广播，实际累计收到 %d 条", got)
	}
}

// TestNotificationSendRejectsBlankChannelTargets 锁住「点名了渠道却一个有效 ID 都没有」时返回 400。
//
// 老行为是静默退化成广播：uniqueNotificationChannelIDs 丢掉 id==0，响应里 used_all 仍是 false、
// requested_ids 仍是 [0]，接口在说谎。加了绑定推送后更危险 ——
// 本想只发一个专用渠道，结果所有默认推送渠道都收到了。
func TestNotificationSendRejectsBlankChannelTargets(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultURL, defaultHits := countingWebhook(t)
	mustCreatePushScopeChannel(t, "广播渠道", defaultURL, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-zero-id-operator")

	for _, body := range []string{
		`{"title":"通知","content":"正文","channel_ids":[0]}`,
		`{"title":"通知","content":"正文","channel_id":0}`,
		`{"title":"通知","content":"正文","channel_id":0,"channel_ids":[0,0]}`,
	} {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send", body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 期望 400，实际 %d: %s", body, rec.Code, rec.Body.String())
		}
	}

	if got := atomic.LoadInt32(defaultHits); got != 0 {
		t.Fatalf("被拒绝的请求不应退化成广播，默认推送渠道实际收到 %d 条", got)
	}

	// 空数组不算「点名」，仍按广播处理：它字面上没指定任何渠道，且这是升级前的既有行为。
	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"通知","content":"正文","channel_ids":[]}`,
		headers,
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("空 channel_ids 应按广播处理，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if got := atomic.LoadInt32(defaultHits); got != 1 {
		t.Fatalf("空 channel_ids 应广播到默认推送渠道，实际收到 %d 条", got)
	}
}

// TestUpdateNotificationChannelKeepsPushScopeWhenFieldAbsent 是兼容性关键用例。
//
// 独立发版的 Flutter APP 编辑渠道时根本不带 push_scope，
// 一旦 Update 改成「缺省即 default」，用户在 Web 上设的「绑定推送」会被 APP 的一次保存悄悄清掉。
func TestUpdateNotificationChannelKeepsPushScopeWhenFieldAbsent(t *testing.T) {
	testutil.SetupTestEnv(t)

	bound := mustCreatePushScopeChannel(t, "脚本专用渠道", "https://example.com/webhook", model.NotifyPushScopeBound)

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-scope-update-admin")

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(bound.ID),
		`{"name":"改个名字"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var reloaded model.NotifyChannel
	if err := database.DB.First(&reloaded, bound.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if reloaded.Name != "改个名字" {
		t.Fatalf("expected name updated, got %q", reloaded.Name)
	}
	if reloaded.PushScope != model.NotifyPushScopeBound {
		t.Fatalf("不带 push_scope 的更新不能清掉已有标记，实际 %q", reloaded.PushScope)
	}

	// 显式传 null 与「不带这个键」等价：独立发版的 APP 很可能把未填字段序列化成 null，
	// 按类型错误返回 400 会让它一升级就全线保存失败。
	rec = performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(bound.ID),
		`{"name":"再改个名字","push_scope":null}`,
		headers,
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("显式传 null 应视为未提供并返回 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if err := database.DB.First(&reloaded, bound.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if reloaded.Name != "再改个名字" {
		t.Fatalf("同一请求里的其它键仍应生效，实际 %q", reloaded.Name)
	}
	if reloaded.PushScope != model.NotifyPushScopeBound {
		t.Fatalf("显式传 null 不能清掉已有标记，实际 %q", reloaded.PushScope)
	}

	// 显式传值时才改。
	rec = performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(bound.ID),
		`{"push_scope":"default"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := database.DB.First(&reloaded, bound.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if reloaded.PushScope != model.NotifyPushScopeDefault {
		t.Fatalf("显式传 default 应生效，实际 %q", reloaded.PushScope)
	}

	// 非法取值必须 400，不能就近纠正成 default —— 那等于把用户的隔离意图反着执行。
	rec = performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(bound.ID),
		`{"push_scope":"bind"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 push_scope 应返回 400，实际 %d: %s", rec.Code, rec.Body.String())
	}

	// 放行 null 是针对「未填字段」这一种形态的兜底，其余非字符串类型仍要 400，校验能力不能丢。
	for _, body := range []string{
		`{"push_scope":1}`,
		`{"push_scope":true}`,
		`{"push_scope":["bound"]}`,
	} {
		rec = performJSONRequest(engine, http.MethodPut, "/api/v1/notifications/"+jsonNumber(bound.ID), body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 期望 400，实际 %d: %s", body, rec.Code, rec.Body.String())
		}
	}
}

// TestCreateNotificationChannelHandlesPushScope 覆盖创建口：
// 不带该字段（老客户端）落成 default，带 bound 就落成 bound，拼错则 400。
func TestCreateNotificationChannelHandlesPushScope(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-scope-create-admin")

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications",
		`{"name":"老客户端渠道","type":"webhook","config":"{\"url\":\"https://example.com/a\"}"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var legacy model.NotifyChannel
	if err := database.DB.Where("name = ?", "老客户端渠道").First(&legacy).Error; err != nil {
		t.Fatalf("load created channel: %v", err)
	}
	if legacy.EffectivePushScope() != model.NotifyPushScopeDefault {
		t.Fatalf("不带 push_scope 的创建应落成 default，实际 %q", legacy.PushScope)
	}

	rec = performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications",
		`{"name":"专用渠道","type":"webhook","config":"{\"url\":\"https://example.com/b\"}","push_scope":"bound"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"push_scope":"bound"`) {
		t.Fatalf("创建响应应回显 push_scope，实际 %s", rec.Body.String())
	}

	var bound model.NotifyChannel
	if err := database.DB.Where("name = ?", "专用渠道").First(&bound).Error; err != nil {
		t.Fatalf("load created channel: %v", err)
	}
	if bound.PushScope != model.NotifyPushScopeBound {
		t.Fatalf("expected push_scope=bound, got %q", bound.PushScope)
	}

	rec = performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications",
		`{"name":"拼错渠道","type":"webhook","config":"{}","push_scope":"bind"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 push_scope 应返回 400，实际 %d: %s", rec.Code, rec.Body.String())
	}

	var count int64
	database.DB.Model(&model.NotifyChannel{}).Where("name = ?", "拼错渠道").Count(&count)
	if count != 0 {
		t.Fatalf("被拒绝的请求不应落库，实际存在 %d 条", count)
	}
}
