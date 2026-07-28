package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func newPlatformTokenRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewPlatformTokenHandler().RegisterRoutes(api)
	return engine
}

func TestPlatformTokenCreateSealsAndRedactsToken(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	admin := testutil.MustCreateUser(t, "platform-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	platform := &model.Platform{Name: "open-app-key", Label: "Open App"}
	if err := database.DB.Create(platform).Error; err != nil {
		t.Fatalf("create platform: %v", err)
	}

	rec := performJSONRequest(newPlatformTokenRouter(), http.MethodPost, "/api/v1/platform-tokens", fmt.Sprintf(`{"platform_id":%d,"name":"svc-token","token":"plain-token","service":"notify","service_user":"alice"}`, platform.ID), map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create success, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "plain-token") {
		t.Fatalf("create response leaked token: %s", rec.Body.String())
	}

	var saved model.PlatformToken
	if err := database.DB.First(&saved).Error; err != nil {
		t.Fatalf("load saved token: %v", err)
	}
	if saved.Token != "" || saved.Sealed == "" || saved.TokenHash == "" {
		t.Fatalf("token should be sealed and hashed without plaintext: %+v", saved)
	}
	if saved.Service != "notify" || saved.ServiceUser != "alice" {
		t.Fatalf("unexpected service binding: %+v", saved)
	}
}
