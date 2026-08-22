package handler_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

type sealedPlatformTokenForTest struct {
	Provider string `json:"provider"`
	Cipher   string `json:"cipher"`
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func sealPlatformTokenForTest(key, value string) (string, string, error) {
	sealed, err := service.RuntimeSecretStoreInstance().Seal(context.Background(), key, []byte(value))
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(sealedPlatformTokenForTest{
		Provider: sealed.Provider,
		Cipher:   base64.StdEncoding.EncodeToString(sealed.Cipher),
	})
	if err != nil {
		return "", "", err
	}
	return string(payload), sha256Hex(value), nil
}

func newOpenAPIRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewOpenAPIHandler().RegisterRoutes(api)
	return engine
}

func TestOpenAPITokenSupportsPerServiceUserCredentials(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newOpenAPIRouter()
	rawSecret := "service-secret"
	app := &model.OpenApp{
		Name:      "service-app",
		AppKey:    "service-app-key",
		AppSecret: "sha256:" + sha256Hex(rawSecret),
		Scopes:    "notifications",
		Enabled:   true,
		RateLimit: 0,
	}
	if err := database.DB.Create(app).Error; err != nil {
		t.Fatalf("create app: %v", err)
	}
	platform := &model.Platform{Name: app.AppKey, Label: "Service App"}
	if err := database.DB.Create(platform).Error; err != nil {
		t.Fatalf("create platform: %v", err)
	}
	sealed, tokenHash, err := sealPlatformTokenForTest("platform-token:"+app.AppKey+":notify:alice", "plain-token")
	if err != nil {
		t.Fatalf("seal token: %v", err)
	}
	if err := database.DB.Create(&model.PlatformToken{PlatformID: platform.ID, Name: "notify alice", Sealed: sealed, TokenHash: tokenHash, Service: "notify", ServiceUser: "alice", Enabled: true}).Error; err != nil {
		t.Fatalf("create platform token: %v", err)
	}

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/open-api/token", `{"app_key":"service-app-key","app_secret":"service-secret","service":"notify","service_user":"alice"}`, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected token success, got %d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeJSONMap(t, rec)
	data := payload["data"].(map[string]interface{})
	accessToken, ok := data["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatalf("expected access token, got %#v", data)
	}
	if strings.Contains(rec.Body.String(), "plain-token") {
		t.Fatalf("token response leaked service credential: %s", rec.Body.String())
	}
}

func TestOpenAPIManagementResponsesUseTodayCallCount(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "open-api-admin", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newOpenAPIRouter()

	app := &model.OpenApp{
		Name:      "demo-app",
		AppKey:    "demo-key",
		AppSecret: "demo-secret",
		Scopes:    "tasks",
		Enabled:   true,
		RateLimit: 0,
		CallCount: 88,
	}
	if err := database.DB.Create(app).Error; err != nil {
		t.Fatalf("create app: %v", err)
	}

	now := time.Now()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayLog := &model.ApiCallLog{
		AppID:     app.ID,
		AppName:   app.Name,
		Endpoint:  "/api/v1/tasks",
		Method:    http.MethodGet,
		Status:    http.StatusOK,
		CreatedAt: startOfToday.Add(2 * time.Hour),
	}
	yesterdayLog := &model.ApiCallLog{
		AppID:     app.ID,
		AppName:   app.Name,
		Endpoint:  "/api/v1/tasks",
		Method:    http.MethodGet,
		Status:    http.StatusOK,
		CreatedAt: startOfToday.Add(-2 * time.Hour),
	}
	if err := database.DB.Create(todayLog).Error; err != nil {
		t.Fatalf("create today log: %v", err)
	}
	if err := database.DB.Create(yesterdayLog).Error; err != nil {
		t.Fatalf("create yesterday log: %v", err)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/open-api/apps", map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list to succeed, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	listPayload := decodeJSONMap(t, listRec)
	apps, ok := listPayload["data"].([]interface{})
	if !ok || len(apps) != 1 {
		t.Fatalf("expected a single app in list response, got %#v", listPayload["data"])
	}
	listItem := apps[0].(map[string]interface{})
	if got := int(listItem["call_count"].(float64)); got != 1 {
		t.Fatalf("expected list today call_count to be 1, got %d", got)
	}

	updateRec := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/open-api/apps/%d", app.ID), `{"name":"renamed-app"}`, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update to succeed, got %d body=%s", updateRec.Code, updateRec.Body.String())
	}
	updatePayload := decodeJSONMap(t, updateRec)
	updateData := updatePayload["data"].(map[string]interface{})
	if got := int(updateData["call_count"].(float64)); got != 1 {
		t.Fatalf("expected update response today call_count to be 1, got %d", got)
	}

	resetRec := performRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/open-api/apps/%d/reset-secret", app.ID), map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected reset secret to succeed, got %d body=%s", resetRec.Code, resetRec.Body.String())
	}
	resetPayload := decodeJSONMap(t, resetRec)
	resetData := resetPayload["data"].(map[string]interface{})
	if got := int(resetData["call_count"].(float64)); got != 1 {
		t.Fatalf("expected reset response today call_count to be 1, got %d", got)
	}
	if _, ok := resetData["app_secret"].(string); !ok {
		t.Fatalf("expected reset response to include app_secret, got %#v", resetData)
	}
}

func TestOpenAPICreateResponseStartsWithZeroTodayCallCount(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "open-api-create-admin", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newOpenAPIRouter()

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/open-api/apps", `{"name":"new-app","scopes":"tasks","rate_limit":0}`, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create to succeed, got %d body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data := payload["data"].(map[string]interface{})
	if got := int(data["call_count"].(float64)); got != 0 {
		t.Fatalf("expected new app today call_count to start at 0, got %d", got)
	}
}

func TestOpenAPISecretCanBeViewedAfterPasswordVerification(t *testing.T) {
	testutil.SetupTestEnv(t)
	admin := testutil.MustCreateLoginUser(t, "open-api-secret-admin", "admin", "correct-password")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newOpenAPIRouter()

	create := performJSONRequest(engine, http.MethodPost, "/api/v1/open-api/apps", `{"name":"secret-app","scopes":"tasks"}`, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	created := decodeJSONMap(t, create)["data"].(map[string]interface{})
	id := uint(created["id"].(float64))
	want := created["app_secret"].(string)

	view := performJSONRequest(engine, http.MethodPost, fmt.Sprintf("/api/v1/open-api/apps/%d/view-secret", id), `{"password":"correct-password"}`, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if view.Code != http.StatusOK {
		t.Fatalf("view status=%d body=%s", view.Code, view.Body.String())
	}
	got := decodeJSONMap(t, view)["data"].(map[string]interface{})["app_secret"]
	if got != want {
		t.Fatalf("viewed secret=%v want=%q", got, want)
	}
}
