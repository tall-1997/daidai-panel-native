package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func mustNotificationAdminHeaders(t *testing.T, username string) map[string]string {
	t.Helper()

	admin := testutil.MustCreateUser(t, username, "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	return map[string]string{"Authorization": "Bearer " + token}
}

// TestNotificationTypesExposesFieldSchema 断言 /notifications/types 下发字段定义，
// 且老客户端依赖的 type / name 两个键一个没少（纯可加）。
func TestNotificationTypesExposesFieldSchema(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	rec := performRequest(engine, http.MethodGet, "/api/v1/notifications/types",
		mustNotificationAdminHeaders(t, "notify-schema-admin"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected non-empty data array, got %#v", payload["data"])
	}

	expectedCount := len(model.NotifyChannelDefinitions())
	if len(items) != expectedCount {
		t.Fatalf("渠道数量与注册表不一致：接口 %d 个，注册表 %d 个", len(items), expectedCount)
	}

	totalSlots := 0
	for _, item := range items {
		channel, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("渠道项应当是对象，实际 %#v", item)
		}
		channelType, _ := channel["type"].(string)
		if channelType == "" {
			t.Errorf("渠道项缺少 type: %#v", channel)
		}
		if name, _ := channel["name"].(string); name == "" {
			t.Errorf("渠道 %s 缺少 name", channelType)
		}

		fields, ok := channel["fields"].([]interface{})
		if !ok || len(fields) == 0 {
			t.Errorf("渠道 %s 没有下发 fields，APP 会回落本地冻结快照", channelType)
			continue
		}
		totalSlots += len(fields)
	}

	if totalSlots == 0 {
		t.Fatal("所有渠道加起来一个字段都没有")
	}
}

// TestNotificationTypesCoversWecomConditionalFields 单独盯住条件字段这块，
// 它是整份 schema 里唯一有分支语义的部分，也是最容易在翻译时漏掉的部分。
func TestNotificationTypesCoversWecomConditionalFields(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	rec := performRequest(engine, http.MethodGet, "/api/v1/notifications/types",
		mustNotificationAdminHeaders(t, "notify-wecom-admin"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, _ := payload["data"].([]interface{})

	var wecom map[string]interface{}
	for _, item := range items {
		channel, _ := item.(map[string]interface{})
		if channel["type"] == "wecom" {
			wecom = channel
			break
		}
	}
	if wecom == nil {
		t.Fatal("响应里没有 wecom 渠道")
	}

	fields, _ := wecom["fields"].([]interface{})
	conditions := make(map[string][]string)
	for _, item := range fields {
		field, _ := item.(map[string]interface{})
		showWhen, ok := field["show_when"].(map[string]interface{})
		if !ok {
			continue
		}
		if showWhen["key"] != "msg_type" {
			t.Errorf("wecom 的条件字段应当都按 msg_type 分支，实际 %#v", showWhen["key"])
		}
		values, _ := showWhen["values"].([]interface{})
		key, _ := field["key"].(string)
		for _, value := range values {
			text, _ := value.(string)
			conditions[key] = append(conditions[key], text)
		}
	}

	for _, key := range []string{
		"content_template", "mentioned_list", "mentioned_mobile_list",
		"image_base64", "image_md5", "news_articles", "template_card_payload",
	} {
		if len(conditions[key]) == 0 {
			t.Errorf("wecom 的条件字段 %s 缺少 show_when", key)
		}
	}

	// content_template 声明了两次（text 一次、markdown 系一次），三个值都要能命中。
	joined := strings.Join(conditions["content_template"], ",")
	for _, value := range []string{"text", "markdown", "markdown_v2"} {
		if !strings.Contains(joined, value) {
			t.Errorf("content_template 应当在 msg_type=%s 时显示，实际条件为 %s", value, joined)
		}
	}
}

// TestCreateNotificationChannelCoercesNonStringConfigValues 是 §2.3 的核心回归：
// APP 曾把 smtp_ssl 写成 JSON 布尔，导致整个渠道的通知全挂。
func TestCreateNotificationChannelCoercesNonStringConfigValues(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-coerce-admin")

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications",
		`{"name":"邮件通知","type":"email","config":"{\"smtp_ssl\":false,\"smtp_port\":465,\"smtp_host\":\"smtp.qq.com\"}"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var stored model.NotifyChannel
	if err := database.DB.Where("name = ?", "邮件通知").First(&stored).Error; err != nil {
		t.Fatalf("load created channel: %v", err)
	}

	// 关键断言：落库的 config 必须能被 service.sendToChannel 的读法解出来。
	var cfg map[string]string
	if err := json.Unmarshal([]byte(stored.Config), &cfg); err != nil {
		t.Fatalf("落库的 config 仍然无法反序列化成 map[string]string: %v\n  config: %s", err, stored.Config)
	}
	if cfg["smtp_ssl"] != "false" {
		t.Errorf("smtp_ssl 应归一成字符串 \"false\"，实际 %q", cfg["smtp_ssl"])
	}
	if cfg["smtp_port"] != "465" {
		t.Errorf("smtp_port 应归一成字符串 \"465\"，实际 %q", cfg["smtp_port"])
	}
	if cfg["smtp_host"] != "smtp.qq.com" {
		t.Errorf("smtp_host 应原样保留，实际 %q", cfg["smtp_host"])
	}
}

// TestCreateNotificationChannelRejectsNestedConfigValues 断言不可逆的值被 400 拦掉，
// 而不是被 fmt.Sprint 成 Go 语法垃圾静默存进库。
func TestCreateNotificationChannelRejectsNestedConfigValues(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-reject-admin")

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications",
		`{"name":"自定义通知","type":"custom","config":"{\"headers\":{\"Authorization\":\"Bearer xxx\"}}"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "headers") {
		t.Errorf("错误信息应当指出是哪个键出了问题，实际: %s", body)
	}

	var count int64
	database.DB.Model(&model.NotifyChannel{}).Where("name = ?", "自定义通知").Count(&count)
	if count != 0 {
		t.Errorf("被拒绝的请求不应落库，实际存在 %d 条", count)
	}
}

// TestUpdateNotificationChannelHealsLegacyBrokenConfig 是向后兼容的关键用例。
//
// 库里可能已经存在被老客户端写坏的记录（smtp_ssl 是 JSON 布尔）。加了校验之后，
// 用户编辑这条记录时提交的仍然是那份坏 config —— 如果直接 400，这条记录就永远改不了。
// 这里断言：坏值走的是「归一自愈」而不是「拒绝」。
func TestUpdateNotificationChannelHealsLegacyBrokenConfig(t *testing.T) {
	testutil.SetupTestEnv(t)

	broken := &model.NotifyChannel{
		Name: "历史坏记录",
		Type: "email",
		// 模拟老 APP 直接写进库的形态：smtp_ssl 是 JSON 布尔。
		Config:  `{"smtp_host":"smtp.qq.com","smtp_port":"465","smtp_ssl":false}`,
		Enabled: true,
	}
	if err := database.DB.Create(broken).Error; err != nil {
		t.Fatalf("create legacy channel: %v", err)
	}

	// 先确认这条记录在修复前确实是发不出去的。
	var probe map[string]string
	if err := json.Unmarshal([]byte(broken.Config), &probe); err == nil {
		t.Fatal("前置条件不成立：这份 config 本应无法反序列化成 map[string]string")
	}

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-heal-admin")

	// 用户原样把读回来的坏 config 再提交一次（Web 和 APP 都是这个行为）。
	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(broken.ID),
		`{"name":"历史坏记录","config":"{\"smtp_host\":\"smtp.qq.com\",\"smtp_port\":\"465\",\"smtp_ssl\":false}"}`,
		headers,
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("坏记录必须能被编辑保存，否则用户永远改不了它。got %d: %s", rec.Code, rec.Body.String())
	}

	var healed model.NotifyChannel
	if err := database.DB.First(&healed, broken.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}

	var cfg map[string]string
	if err := json.Unmarshal([]byte(healed.Config), &cfg); err != nil {
		t.Fatalf("保存后 config 仍然是坏的: %v\n  config: %s", err, healed.Config)
	}
	if cfg["smtp_ssl"] != "false" {
		t.Errorf("smtp_ssl 应被自愈成字符串 \"false\"，实际 %q", cfg["smtp_ssl"])
	}
}

// TestUpdateNotificationChannelRejectsNonStringConfigField 断言 config 字段本身
// 必须是 JSON 字符串。Update 收的是 map[string]interface{}，客户端把 config 写成
// 对象会被 GORM 写成一段无法解析的内容。
func TestUpdateNotificationChannelRejectsNonStringConfigField(t *testing.T) {
	testutil.SetupTestEnv(t)

	channel := &model.NotifyChannel{
		Name:    "形态检查",
		Type:    "webhook",
		Config:  `{"url":"https://example.com/webhook"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}

	engine := newProtectedRouter()
	headers := mustNotificationAdminHeaders(t, "notify-shape-admin")

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/notifications/"+jsonNumber(channel.ID),
		`{"config":{"url":"https://example.com/webhook"}}`,
		headers,
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var reloaded model.NotifyChannel
	if err := database.DB.First(&reloaded, channel.ID).Error; err != nil {
		t.Fatalf("reload channel: %v", err)
	}
	if reloaded.Config != channel.Config {
		t.Errorf("被拒绝的请求不应改动已有 config，实际变成了 %s", reloaded.Config)
	}
}
