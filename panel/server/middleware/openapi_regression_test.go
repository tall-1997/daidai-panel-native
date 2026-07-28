package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestAppTokenDefaultDenyWithoutOpenAPIAccess(t *testing.T) {
	testutil.SetupTestEnv(t)

	token := testutil.MustCreateAppToken(t, "deny-without-scope", "tasks")
	engine := gin.New()

	handlerReached := false
	engine.GET("/private", middleware.JWTAuth(), middleware.RequireRole("viewer"), func(c *gin.Context) {
		handlerReached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if handlerReached {
		t.Fatal("handler should not run when app token is not explicitly authorized")
	}
}

func TestAppTokenScopeCanPassOperatorRoute(t *testing.T) {
	testutil.SetupTestEnv(t)

	token := testutil.MustCreateAppToken(t, "operator-scope", "tasks")
	engine := gin.New()

	handlerReached := false
	engine.POST("/operator", middleware.JWTAuth(), middleware.OpenAPIAccess("tasks"), middleware.RequireRole("operator"), func(c *gin.Context) {
		handlerReached = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/operator", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !handlerReached {
		t.Fatal("handler should run when app token scope is explicitly authorized")
	}
}

func TestOpenAPIAccessLogsCallsWithoutIncrementingPersistentCallCount(t *testing.T) {
	testutil.SetupTestEnv(t)

	app := testutil.MustCreateOpenApp(t, "daily-count-app", "tasks")
	token := testutil.MustCreateAccessToken(t, "app:"+app.AppKey, "app:"+app.Scopes)
	engine := gin.New()

	engine.GET("/tasks", middleware.JWTAuth(), middleware.OpenAPIAccess("tasks"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var updated model.OpenApp
	if err := database.DB.First(&updated, app.ID).Error; err != nil {
		t.Fatalf("reload app: %v", err)
	}
	if updated.CallCount != 0 {
		t.Fatalf("expected persistent call_count to stay 0, got %d", updated.CallCount)
	}

	var logCount int64
	if err := database.DB.Model(&model.ApiCallLog{}).Where("app_id = ?", app.ID).Count(&logCount).Error; err != nil {
		t.Fatalf("count api logs: %v", err)
	}
	if logCount != 1 {
		t.Fatalf("expected 1 api call log, got %d", logCount)
	}
}

func TestOpenAPIAccessLogsPerServiceBinding(t *testing.T) {
	testutil.SetupTestEnv(t)

	app := testutil.MustCreateOpenApp(t, "binding-app", "notifications")
	token := testutil.MustCreateAccessToken(t, "app:"+app.AppKey+":notify:alice", "app:"+app.Scopes)
	engine := gin.New()

	engine.POST("/notifications/send", middleware.JWTAuth(), middleware.OpenAPIAccess("notifications"), func(c *gin.Context) {
		if got := c.GetString("openapi_service"); got != "notify" {
			t.Fatalf("expected context service notify, got %q", got)
		}
		if got := c.GetString("openapi_service_user"); got != "alice" {
			t.Fatalf("expected context service_user alice, got %q", got)
		}
		c.JSON(http.StatusAccepted, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/notifications/send", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	var log model.ApiCallLog
	if err := database.DB.Where("app_id = ?", app.ID).First(&log).Error; err != nil {
		t.Fatalf("load api call log: %v", err)
	}
	if log.Service != "notify" || log.ServiceUser != "alice" {
		t.Fatalf("expected service binding in api call log, got %+v", log)
	}
}
