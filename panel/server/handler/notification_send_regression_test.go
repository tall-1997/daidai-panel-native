package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestNotificationSendUsesSelectedChannelAndSupportsAppToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("decode webhook body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := &model.NotifyChannel{
		Name:    "Webhook 通知",
		Type:    "webhook",
		Config:  `{"url":"` + server.URL + `"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	engine := newProtectedRouter()
	appToken := testutil.MustCreateAppToken(t, "notify-app", "notifications")

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"脚本通知","content":"执行成功","channel_id":`+jsonNumber(channel.ID)+`}`,
		map[string]string{"Authorization": "Bearer " + appToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if received["title"] != "脚本通知" {
		t.Fatalf("expected title to be forwarded, got %#v", received)
	}
	if received["content"] != "执行成功" {
		t.Fatalf("expected content to be forwarded, got %#v", received)
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %#v", payload["data"])
	}
	if got, ok := data["sent_count"].(float64); !ok || got != 1 {
		t.Fatalf("expected sent_count=1, got %#v", data["sent_count"])
	}
}

func TestNotificationSendRejectsAppTokenWithoutScope(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	appToken := testutil.MustCreateAppToken(t, "tasks-app", "tasks")

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"脚本通知","content":"执行成功"}`,
		map[string]string{"Authorization": "Bearer " + appToken},
		"",
	)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationSendReturnsFailureWhenNoEnabledChannelMatches(t *testing.T) {
	testutil.SetupTestEnv(t)

	channel := &model.NotifyChannel{
		Name:    "已禁用通道",
		Type:    "webhook",
		Config:  `{"url":"https://example.com/webhook"}`,
		Enabled: true,
	}
	if err := database.DB.Select("*").Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}
	if err := database.DB.Model(channel).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable notification channel: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "notify-operator", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"脚本通知","content":"执行成功","channel_id":`+jsonNumber(channel.ID)+`}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "未找到已启用的通知渠道") {
		t.Fatalf("expected disabled channel error, got %s", rec.Body.String())
	}
}

func TestNotificationSendUpdatesTodaySendCount(t *testing.T) {
	testutil.SetupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := &model.NotifyChannel{
		Name:    "统计通道",
		Type:    "webhook",
		Config:  `{"url":"` + server.URL + `"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "notify-counter", "operator")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/notifications/send",
		`{"title":"脚本通知","content":"执行成功","channel_id":`+jsonNumber(channel.ID)+`}`,
		map[string]string{"Authorization": "Bearer " + accessToken},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var reloaded model.NotifyChannel
	if err := database.DB.First(&reloaded, channel.ID).Error; err != nil {
		t.Fatalf("reload notification channel: %v", err)
	}

	if reloaded.TodaySendDate != time.Now().Format("2006-01-02") {
		t.Fatalf("expected today_send_date to be updated, got %q", reloaded.TodaySendDate)
	}
	if reloaded.TodaySendCount != 1 {
		t.Fatalf("expected today_send_count=1, got %d", reloaded.TodaySendCount)
	}
}

func TestNotificationTestPersistsLastTestState(t *testing.T) {
	testutil.SetupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := &model.NotifyChannel{
		Name:    "测试通道",
		Type:    "webhook",
		Config:  `{"url":"` + server.URL + `"}`,
		Enabled: true,
	}
	if err := database.DB.Create(channel).Error; err != nil {
		t.Fatalf("create notification channel: %v", err)
	}

	engine := newProtectedRouter()
	admin := testutil.MustCreateUser(t, "notify-admin", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	rec := performRequest(engine, http.MethodPost, "/api/v1/notifications/"+jsonNumber(channel.ID)+"/test", map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var reloaded model.NotifyChannel
	if err := database.DB.First(&reloaded, channel.ID).Error; err != nil {
		t.Fatalf("reload notification channel: %v", err)
	}

	if reloaded.LastTestStatus != "success" {
		t.Fatalf("expected last_test_status=success, got %q", reloaded.LastTestStatus)
	}
	if reloaded.LastTestAt == nil || reloaded.LastTestAt.IsZero() {
		t.Fatalf("expected last_test_at to be recorded, got %#v", reloaded.LastTestAt)
	}
}

func jsonNumber(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
