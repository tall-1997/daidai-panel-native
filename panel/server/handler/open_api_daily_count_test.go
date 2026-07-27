package handler_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func newOpenAPIRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewOpenAPIHandler().RegisterRoutes(api)
	return engine
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
