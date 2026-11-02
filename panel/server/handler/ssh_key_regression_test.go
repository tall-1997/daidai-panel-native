package handler_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/handler"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// handlerSecretStore 是 SSH 密钥密封/开启链路回归测试用的内存密钥存储替身。
type handlerSecretStore struct{}

func (handlerSecretStore) Seal(_ context.Context, _ string, payload []byte) (service.SealedValue, error) {
	return service.SealedValue{Provider: "test", Cipher: []byte("sealed:" + base64.StdEncoding.EncodeToString(payload))}, nil
}

func (handlerSecretStore) Open(_ context.Context, _ string, value service.SealedValue) ([]byte, error) {
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(string(value.Cipher), "sealed:"))
}

func (handlerSecretStore) Status() service.SecretStoreStatus {
	return service.SecretStoreStatus{Provider: "test", Ready: true}
}

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

func TestSSHKeyCreateSealsPrivateKeyAndRedactsResponses(t *testing.T) {
	testutil.SetupTestEnv(t)
	restore := service.SetRuntimeSecretStoreForTest(handlerSecretStore{})
	defer restore()

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewSSHKeyHandler().RegisterRoutes(api)

	admin := testutil.MustCreateUser(t, "ssh-secret-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	body := `{"name":"deploy-key","private_key":"-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/ssh-keys", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "OPENSSH PRIVATE KEY") || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("create response leaked private key: %s", rec.Body.String())
	}

	var key model.SSHKey
	if err := database.DB.Where("name = ?", "deploy-key").First(&key).Error; err != nil {
		t.Fatalf("query ssh key: %v", err)
	}
	if strings.TrimSpace(key.PrivateKey) != "" {
		t.Fatalf("expected plaintext private key to be empty, got %q", key.PrivateKey)
	}
	if !key.HasPrivateKeySecret() {
		t.Fatalf("expected private key SecretStore fields to be populated")
	}
	if strings.Contains(string(key.PrivateKeySecretCipher), "OPENSSH PRIVATE KEY") {
		t.Fatalf("expected sealed private key payload, got %q", string(key.PrivateKeySecretCipher))
	}

	detailRec := performRequest(engine, http.MethodGet, "/api/v1/ssh-keys/"+strconv.FormatUint(uint64(key.ID), 10), map[string]string{
		"Authorization": "Bearer " + token,
	})
	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d, body=%s", detailRec.Code, detailRec.Body.String())
	}
	if strings.Contains(detailRec.Body.String(), "OPENSSH PRIVATE KEY") || strings.Contains(detailRec.Body.String(), "secret") {
		t.Fatalf("detail response leaked private key: %s", detailRec.Body.String())
	}
}
