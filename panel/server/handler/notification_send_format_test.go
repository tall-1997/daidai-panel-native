package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// issue #135：/notifications/send 的 content_type、channel_type(s)、channel_name(s)。
// 全部是可选的加法字段；不传时的行为由 notification_send_regression_test.go 与 notification_push_scope_test.go 守着。

func mustCreateTypedChannel(t *testing.T, name, channelType, webhookURL, pushScope string) *model.NotifyChannel {
	t.Helper()

	channel := &model.NotifyChannel{
		Name:      name,
		Type:      channelType,
		Config:    `{"url":"` + webhookURL + `"}`,
		PushScope: pushScope,
		Enabled:   true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel %q: %v", name, err)
	}
	return channel
}

func TestNotificationSendRejectsInvalidContentType(t *testing.T) {
	testutil.SetupTestEnv(t)

	url, hits := countingWebhook(t)
	mustCreatePushScopeChannel(t, "广播渠道", url, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-bad-content-type")
	for _, contentType := range []string{"application/json", "json"} {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send",
			`{"title":"通知","content":"正文","content_type":"`+contentType+`"}`, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非法 content_type %q 应返回 400，实际 %d: %s", contentType, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "content_type") {
			t.Fatalf("错误信息应点名 content_type，实际 %s", rec.Body.String())
		}
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("被拒绝的请求不应发出通知，实际 %d 条", got)
	}
}

// TestNotificationSendAcceptsMIMEContentTypeAndAnyCaseChannelType：content_type 认 text/html 这类 MIME 写法（带 charset 参数也行），
// channel_type 与 content_type 一样大小写不敏感，归一成注册表里的类型名再过滤、回显。
func TestNotificationSendAcceptsMIMEContentTypeAndAnyCaseChannelType(t *testing.T) {
	testutil.SetupTestEnv(t)

	webhookURL, webhookHits := countingWebhook(t)
	customURL, customHits := countingWebhook(t)
	mustCreatePushScopeChannel(t, "广播 webhook", webhookURL, model.NotifyPushScopeDefault)
	mustCreateTypedChannel(t, "广播 custom", "custom", customURL, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-mime-any-case")

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send",
		`{"title":"通知","content":"<b>正文</b>","content_type":"Text/HTML; charset=UTF-8","channel_type":"WebHook"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if data["content_type"] != "html" || !reflect.DeepEqual(data["channel_types"], []interface{}{"webhook"}) {
		t.Fatalf("响应应回显归一后的 content_type 与 channel_types，实际 %#v", data)
	}
	if atomic.LoadInt32(webhookHits) != 1 || atomic.LoadInt32(customHits) != 0 {
		t.Fatalf("channel_type=WebHook 应只命中 webhook，实际 webhook=%d custom=%d", atomic.LoadInt32(webhookHits), atomic.LoadInt32(customHits))
	}

	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send",
		`{"title":"通知","content":"正文","content_type":"text/plain","channel_types":["CUSTOM"," Webhook "]}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data, _ = decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if data["content_type"] != "text" || !reflect.DeepEqual(data["channel_types"], []interface{}{"custom", "webhook"}) {
		t.Fatalf("响应应回显归一后的 content_type 与 channel_types，实际 %#v", data)
	}
	if atomic.LoadInt32(webhookHits) != 2 || atomic.LoadInt32(customHits) != 1 {
		t.Fatalf("channel_types 应命中两种渠道各一次，实际 webhook=%d custom=%d", atomic.LoadInt32(webhookHits), atomic.LoadInt32(customHits))
	}
}

// TestNotifySendDocsListEveryChannelType：脚本令牌调不了 GET /notifications/types，脚本文档和接口文档只能直接列出渠道类型名。
// 注册表加了渠道、文档没跟上时这里会红。
func TestNotifySendDocsListEveryChannelType(t *testing.T) {
	docs := []struct {
		path   string
		anchor string // 列出类型名的那一行里一定有的文字
	}{
		{path: filepath.Join("..", "..", "docs", "script-api.md"), anchor: "渠道类型名"},
		{path: filepath.Join("..", "..", "web", "src", "views", "api-docs", "apiData.ts"), anchor: "name: 'channel_type',"},
	}
	for _, doc := range docs {
		raw, err := os.ReadFile(doc.path)
		if os.IsNotExist(err) {
			t.Skipf("%s 不在（只拷了 server 目录），跳过文档检查", doc.path)
		}
		if err != nil {
			t.Fatalf("read %s: %v", doc.path, err)
		}
		var line string
		for _, candidate := range strings.Split(string(raw), "\n") {
			if strings.Contains(candidate, doc.anchor) {
				line = candidate
				break
			}
		}
		if line == "" {
			t.Fatalf("%s 里找不到含 %q 的那一行", doc.path, doc.anchor)
		}
		if strings.Contains(line, "notifications/types") {
			t.Errorf("%s 的渠道类型说明不应再指向管理员才能调的 /notifications/types", doc.path)
		}
		for _, definition := range model.NotifyChannelDefinitions() {
			if !regexp.MustCompile(`(^|[^a-z_])` + regexp.QuoteMeta(definition.Type) + `([^a-z_]|$)`).MatchString(line) {
				t.Errorf("%s 的渠道类型说明漏了 %q", doc.path, definition.Type)
			}
		}
	}
}

func TestNotificationSendRejectsUnknownOrBlankChannelType(t *testing.T) {
	testutil.SetupTestEnv(t)

	url, hits := countingWebhook(t)
	mustCreatePushScopeChannel(t, "广播渠道", url, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-bad-channel-type")
	cases := []struct {
		body string
		want string
	}{
		{body: `{"title":"通知","content":"正文","channel_type":"weixin"}`, want: "weixin"},
		// 脚本令牌调不了 GET /notifications/types，报错里要直接列出可选类型。
		{body: `{"title":"通知","content":"正文","channel_type":"weixin"}`, want: "wxpusher"},
		{body: `{"title":"通知","content":"正文","channel_types":["webhook","nope"]}`, want: "nope"},
		// 显式传了却是空白：和 channel_ids:[0] 一样直接 400，不能静默放宽成「不过滤」的广播。
		{body: `{"title":"通知","content":"正文","channel_type":"  "}`, want: "channel_type"},
		{body: `{"title":"通知","content":"正文","channel_types":[""]}`, want: "channel_type"},
	}
	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send", tc.body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 期望 400，实际 %d: %s", tc.body, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("body=%s 错误信息应包含 %q，实际 %s", tc.body, tc.want, rec.Body.String())
		}
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("被拒绝的请求不应发出通知，实际 %d 条", got)
	}
}

// TestNotificationSendChannelTypeRespectsPushScope：按类型选渠道不算点名，广播时仍只命中「默认推送」渠道；
// 与 ID / 名称点名同时出现时取交集。
func TestNotificationSendChannelTypeRespectsPushScope(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultWebhookURL, defaultWebhookHits := countingWebhook(t)
	boundWebhookURL, boundWebhookHits := countingWebhook(t)
	customURL, customHits := countingWebhook(t)

	mustCreatePushScopeChannel(t, "广播 webhook", defaultWebhookURL, model.NotifyPushScopeDefault)
	boundWebhook := mustCreatePushScopeChannel(t, "绑定 webhook", boundWebhookURL, model.NotifyPushScopeBound)
	custom := mustCreateTypedChannel(t, "广播 custom", "custom", customURL, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-type-scope")
	send := func(body string) *httptest.ResponseRecorder {
		return performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send", body, headers, "")
	}
	counts := func() [3]int32 {
		return [3]int32{atomic.LoadInt32(defaultWebhookHits), atomic.LoadInt32(boundWebhookHits), atomic.LoadInt32(customHits)}
	}

	rec := send(`{"title":"通知","content":"正文","channel_type":"webhook"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := counts(); got != [3]int32{1, 0, 0} {
		t.Fatalf("按类型广播只该命中默认推送的 webhook，实际 [默认 webhook, 绑定 webhook, custom] = %v", got)
	}
	data, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if !reflect.DeepEqual(data["channel_types"], []interface{}{"webhook"}) || data["used_all"] != true {
		t.Fatalf("响应应回显 channel_types 且仍是广播，实际 %#v", data)
	}

	rec = send(`{"title":"通知","content":"正文","channel_types":["custom"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := counts(); got != [3]int32{1, 0, 1} {
		t.Fatalf("channel_types=[custom] 只该命中 custom，实际 %v", got)
	}

	// 点名 + 类型：取交集。点名忽略 push_scope，所以绑定推送的 webhook 能收到。
	rec = send(`{"title":"通知","content":"正文","channel_ids":[` + jsonNumber(boundWebhook.ID) + `,` + jsonNumber(custom.ID) + `],"channel_type":"webhook"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := counts(); got != [3]int32{1, 1, 1} {
		t.Fatalf("点名 + 类型应只命中绑定 webhook，实际 %v", got)
	}

	// 交集为空：点名场景沿用「未找到已启用的通知渠道」，不退化成广播。
	rec = send(`{"title":"通知","content":"正文","channel_id":` + jsonNumber(custom.ID) + `,"channel_type":"webhook"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "未找到已启用的通知渠道") {
		t.Fatalf("交集为空应 400 并提示未找到已启用的通知渠道，实际 %d: %s", rec.Code, rec.Body.String())
	}
	// 广播按类型无命中（库里没有 email 渠道）。
	rec = send(`{"title":"通知","content":"正文","channel_type":"email"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "暂无参与广播的默认推送渠道") {
		t.Fatalf("按类型广播无命中应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if got := counts(); got != [3]int32{1, 1, 1} {
		t.Fatalf("失败的请求不应发出任何通知，实际 %v", got)
	}
}

func TestNotificationSendByChannelNameTargetsBoundChannel(t *testing.T) {
	testutil.SetupTestEnv(t)

	defaultURL, defaultHits := countingWebhook(t)
	boundURL, boundHits := countingWebhook(t)
	mustCreatePushScopeChannel(t, "广播渠道", defaultURL, model.NotifyPushScopeDefault)
	bound := mustCreatePushScopeChannel(t, "脚本专用渠道", boundURL, model.NotifyPushScopeBound)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-by-name")
	send := func(body string) *httptest.ResponseRecorder {
		return performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send", body, headers, "")
	}

	rec := send(`{"title":"通知","content":"正文","channel_name":"脚本专用渠道"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(boundHits) != 1 || atomic.LoadInt32(defaultHits) != 0 {
		t.Fatalf("按名称点名应只命中绑定推送渠道，实际 bound=%d default=%d", atomic.LoadInt32(boundHits), atomic.LoadInt32(defaultHits))
	}
	data, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if !reflect.DeepEqual(data["requested_ids"], []interface{}{float64(bound.ID)}) || data["used_all"] != false {
		t.Fatalf("按名称点名解析出的 ID 应计入 requested_ids，实际 %#v", data)
	}

	// 名称查不到：400 并列出查不到的名称，绝不退化成广播。
	for _, body := range []string{
		`{"title":"通知","content":"正文","channel_names":["广播渠道","不存在的渠道"]}`,
		`{"title":"通知","content":"正文","channel_name":"不存在的渠道"}`,
	} {
		rec = send(body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "不存在的渠道") {
			t.Fatalf("body=%s 期望 400 并列出名称，实际 %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	for _, body := range []string{
		`{"title":"通知","content":"正文","channel_name":""}`,
		`{"title":"通知","content":"正文","channel_names":[" "]}`,
	} {
		rec = send(body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s 空白名称应 400，实际 %d: %s", body, rec.Code, rec.Body.String())
		}
	}

	// 名称 + 类型不匹配：交集为空。
	rec = send(`{"title":"通知","content":"正文","channel_name":"脚本专用渠道","channel_type":"custom"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "未找到已启用的通知渠道") {
		t.Fatalf("名称与类型不匹配应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}

	if atomic.LoadInt32(boundHits) != 1 || atomic.LoadInt32(defaultHits) != 0 {
		t.Fatalf("被拒绝的请求不应发出任何通知，实际 bound=%d default=%d", atomic.LoadInt32(boundHits), atomic.LoadInt32(defaultHits))
	}
}

// TestNotificationSendForwardsContentTypeToWebhook：传了 content_type 时 webhook 多带一个键；
// 不传时请求体的键集合必须仍然只有 title / content（接收方可能按固定结构解析）。
func TestNotificationSendForwardsContentTypeToWebhook(t *testing.T) {
	testutil.SetupTestEnv(t)

	var (
		mu     sync.Mutex
		bodies []map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode webhook body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	mustCreatePushScopeChannel(t, "Webhook 通知", server.URL, model.NotifyPushScopeDefault)

	engine := newProtectedRouter()
	headers := mustOperatorHeaders(t, "notify-content-type-webhook")

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send",
		`{"title":"日报","content":"<b>正文</b>"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/notifications/send",
		`{"title":"日报","content":"<b>正文</b>","content_type":"HTML"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	data, _ := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if data["content_type"] != "html" {
		t.Fatalf("响应应回显归一后的 content_type，实际 %#v", data["content_type"])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("expected 2 webhook requests, got %d", len(bodies))
	}
	keysOf := func(m map[string]interface{}) []string {
		keys := make([]string, 0, len(m))
		for key := range m {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}
	if got := keysOf(bodies[0]); !reflect.DeepEqual(got, []string{"content", "title"}) {
		t.Fatalf("不传 content_type 时 webhook 请求体键集合必须不变，实际 %v", got)
	}
	want := map[string]interface{}{"title": "日报", "content": "<b>正文</b>", "content_type": "html"}
	if !reflect.DeepEqual(bodies[1], want) {
		t.Fatalf("webhook body = %#v, want %#v", bodies[1], want)
	}
}
