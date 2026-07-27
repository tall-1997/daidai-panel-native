package handler_test

import (
	"fmt"
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestUserListIncludesTwoFactorEnabledState(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewUserHandler().RegisterRoutes(api)
	admin := testutil.MustCreateUser(t, "user-admin", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	securedUser := testutil.MustCreateUser(t, "user-with-2fa", "operator")
	plainUser := testutil.MustCreateUser(t, "user-without-2fa", "viewer")

	if err := database.DB.Create(&model.TwoFactorAuth{
		UserID:  securedUser.ID,
		Secret:  "SECRET",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create 2fa record: %v", err)
	}

	if err := database.DB.Create(&model.TwoFactorAuth{
		UserID:  plainUser.ID,
		Secret:  "DISABLED",
		Enabled: false,
	}).Error; err != nil {
		t.Fatalf("create disabled 2fa record: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/users", map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected user list array, got %#v", payload["data"])
	}

	stateByUsername := make(map[string]bool, len(items))
	for _, raw := range items {
		item := raw.(map[string]interface{})
		username, _ := item["username"].(string)
		enabled, _ := item["two_factor_enabled"].(bool)
		stateByUsername[username] = enabled
	}

	if !stateByUsername[securedUser.Username] {
		t.Fatalf("expected %s to expose two_factor_enabled=true, got %#v", securedUser.Username, stateByUsername)
	}
	if stateByUsername[plainUser.Username] {
		t.Fatalf("expected %s to expose two_factor_enabled=false, got %#v", plainUser.Username, stateByUsername)
	}
}

func TestUserUpdateRevokesExistingSessions(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewUserHandler().RegisterRoutes(api)
	handler.NewSystemHandler().RegisterRoutes(api)

	admin := testutil.MustCreateUser(t, "user-admin-update", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	target := testutil.MustCreateUser(t, "revoked-user", "operator")
	accessInfo, err := middleware.GenerateAccessTokenInfo(target.Username, target.Role)
	if err != nil {
		t.Fatalf("generate target access token: %v", err)
	}
	refreshInfo, err := middleware.GenerateRefreshTokenInfo(target.Username, target.Role)
	if err != nil {
		t.Fatalf("generate target refresh token: %v", err)
	}
	service.CreateSessionWithRefresh(
		target.ID,
		target.Username,
		accessInfo.JTI,
		refreshInfo.JTI,
		service.SessionClientWeb,
		"test-web",
		"198.51.100.50",
		"Mozilla/5.0",
		accessInfo.ExpiresAt,
		refreshInfo.ExpiresAt,
	)
	targetToken := accessInfo.Token

	beforeRec := performRequest(engine, http.MethodGet, "/api/v1/system/version", map[string]string{
		"Authorization": "Bearer " + targetToken,
	})
	if beforeRec.Code != http.StatusOK {
		t.Fatalf("expected target token to work before update, got %d, body=%s", beforeRec.Code, beforeRec.Body.String())
	}

	updateBody := `{"enabled":false,"role":"viewer"}`
	updateRec := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/users/%d", target.ID), updateBody, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected user update success, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	afterRec := performRequest(engine, http.MethodGet, "/api/v1/system/version", map[string]string{
		"Authorization": "Bearer " + targetToken,
	})
	if afterRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected target token to be revoked after update, got %d, body=%s", afterRec.Code, afterRec.Body.String())
	}
}

func TestUserResetPasswordRevokesExistingSessions(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewUserHandler().RegisterRoutes(api)
	handler.NewSystemHandler().RegisterRoutes(api)

	admin := testutil.MustCreateUser(t, "user-admin-reset", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	target := testutil.MustCreateUser(t, "reset-user", "operator")
	accessInfo, err := middleware.GenerateAccessTokenInfo(target.Username, target.Role)
	if err != nil {
		t.Fatalf("generate target access token: %v", err)
	}
	refreshInfo, err := middleware.GenerateRefreshTokenInfo(target.Username, target.Role)
	if err != nil {
		t.Fatalf("generate target refresh token: %v", err)
	}
	service.CreateSessionWithRefresh(
		target.ID,
		target.Username,
		accessInfo.JTI,
		refreshInfo.JTI,
		service.SessionClientWeb,
		"test-web",
		"198.51.100.60",
		"Mozilla/5.0",
		accessInfo.ExpiresAt,
		refreshInfo.ExpiresAt,
	)
	targetToken := accessInfo.Token

	beforeRec := performRequest(engine, http.MethodGet, "/api/v1/system/version", map[string]string{
		"Authorization": "Bearer " + targetToken,
	})
	if beforeRec.Code != http.StatusOK {
		t.Fatalf("expected target token to work before password reset, got %d, body=%s", beforeRec.Code, beforeRec.Body.String())
	}

	resetBody := `{"password":"NewPassword123!"}`
	resetRec := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/users/%d/reset-password", target.ID), resetBody, map[string]string{
		"Authorization": "Bearer " + adminToken,
	}, "")
	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected reset password success, got %d, body=%s", resetRec.Code, resetRec.Body.String())
	}

	afterRec := performRequest(engine, http.MethodGet, "/api/v1/system/version", map[string]string{
		"Authorization": "Bearer " + targetToken,
	})
	if afterRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected target token to be revoked after password reset, got %d, body=%s", afterRec.Code, afterRec.Body.String())
	}
}
