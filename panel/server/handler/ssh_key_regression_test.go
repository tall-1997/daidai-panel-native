package handler_test

import (
	"net/http"
	"testing"

	"daidai-panel/handler"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestSSHKeyRoutesRequireAdminUserToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewSSHKeyHandler().RegisterRoutes(api)

	viewer := testutil.MustCreateUser(t, "ssh-viewer", "viewer")
	viewerToken := testutil.MustCreateAccessToken(t, viewer.Username, viewer.Role)
	admin := testutil.MustCreateUser(t, "ssh-admin", "admin")
	adminToken := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	appToken := testutil.MustCreateAppToken(t, "ssh-app", "*")

	viewerRec := performRequest(engine, http.MethodGet, "/api/v1/ssh-keys", map[string]string{
		"Authorization": "Bearer " + viewerToken,
	})
	if viewerRec.Code != http.StatusForbidden {
		t.Fatalf("expected viewer ssh key list to be forbidden, got %d, body=%s", viewerRec.Code, viewerRec.Body.String())
	}

	appRec := performRequest(engine, http.MethodGet, "/api/v1/ssh-keys", map[string]string{
		"Authorization": "Bearer " + appToken,
	})
	if appRec.Code != http.StatusForbidden {
		t.Fatalf("expected app token ssh key list to be forbidden, got %d, body=%s", appRec.Code, appRec.Body.String())
	}

	adminRec := performRequest(engine, http.MethodGet, "/api/v1/ssh-keys", map[string]string{
		"Authorization": "Bearer " + adminToken,
	})
	if adminRec.Code != http.StatusOK {
		t.Fatalf("expected admin ssh key list to succeed, got %d, body=%s", adminRec.Code, adminRec.Body.String())
	}
}
