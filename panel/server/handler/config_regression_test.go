package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

func TestConfigListIncludesRegistryMetadata(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "config-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	database.DB.Where("`key` = ?", "proxy_url").Delete(&model.SystemConfig{})

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}

	autoInstall, ok := data["auto_install_deps"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected auto_install_deps entry, got %T", data["auto_install_deps"])
	}
	if got, _ := autoInstall["value"].(string); got != "true" {
		t.Fatalf("expected auto_install_deps default true, got %q", got)
	}
	if got, _ := autoInstall["value_type"].(string); got != string(model.SystemConfigTypeBool) {
		t.Fatalf("expected auto_install_deps value_type bool, got %q", got)
	}
	if got, _ := autoInstall["registered"].(bool); !got {
		t.Fatalf("expected auto_install_deps to be marked registered")
	}

	proxyCfg, ok := data["proxy_url"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected proxy_url entry, got %T", data["proxy_url"])
	}
	if got, _ := proxyCfg["registered"].(bool); !got {
		t.Fatalf("expected proxy_url to be marked registered")
	}
	if got, _ := proxyCfg["group"].(string); got != "network" {
		t.Fatalf("expected proxy_url group network, got %q", got)
	}

	updateMirrorCfg, ok := data["update_image_mirror"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected update_image_mirror entry, got %T", data["update_image_mirror"])
	}
	if got, _ := updateMirrorCfg["registered"].(bool); !got {
		t.Fatalf("expected update_image_mirror to be marked registered")
	}
	if got, _ := updateMirrorCfg["group"].(string); got != "network" {
		t.Fatalf("expected update_image_mirror group network, got %q", got)
	}
}

func TestConfigBatchSetUsesRegistryValidation(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "config-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newProtectedRouter()

	body := `{"configs":{"auto_install_deps":"0","captcha_fail_mode":" strict ","update_image_mirror":"https://docker.1ms.run/","binary_update_proxy":"gh-proxy.org"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := model.GetRegisteredConfigBool("auto_install_deps"); got {
		t.Fatalf("expected auto_install_deps false after batch set")
	}
	if got := model.GetRegisteredConfig("captcha_fail_mode"); got != "strict" {
		t.Fatalf("expected captcha_fail_mode strict, got %q", got)
	}
	if got := model.GetRegisteredConfig("update_image_mirror"); got != "docker.1ms.run" {
		t.Fatalf("expected update_image_mirror docker.1ms.run, got %q", got)
	}
	if got := model.GetRegisteredConfig("binary_update_proxy"); got != "https://gh-proxy.org/" {
		t.Fatalf("expected binary_update_proxy https://gh-proxy.org/, got %q", got)
	}

	trustedProxyBody := `{"configs":{"trusted_proxy_cidrs":"127.0.0.1,203.0.113.0/24"}}`
	trustedProxyReq := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(trustedProxyBody))
	trustedProxyReq.Header.Set("Authorization", "Bearer "+token)
	trustedProxyReq.Header.Set("Content-Type", "application/json")
	trustedProxyRec := httptest.NewRecorder()
	engine.ServeHTTP(trustedProxyRec, trustedProxyReq)

	if trustedProxyRec.Code != http.StatusOK {
		t.Fatalf("expected trusted proxy request to return 200, got %d, body=%s", trustedProxyRec.Code, trustedProxyRec.Body.String())
	}
	if got := model.GetRegisteredConfig("trusted_proxy_cidrs"); got != "127.0.0.1/32\n203.0.113.0/24" {
		t.Fatalf("expected canonical trusted_proxy_cidrs, got %q", got)
	}

	invalidBody := `{"configs":{"default_cron_rule":"invalid cron"}}`
	invalidReq := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(invalidBody))
	invalidReq.Header.Set("Authorization", "Bearer "+token)
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	engine.ServeHTTP(invalidRec, invalidReq)

	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid config request to return 400, got %d", invalidRec.Code)
	}

	var invalidPayload map[string]interface{}
	if err := json.Unmarshal(invalidRec.Body.Bytes(), &invalidPayload); err != nil {
		t.Fatalf("decode invalid response: %v", err)
	}
	if got, _ := invalidPayload["error"].(string); got == "" {
		t.Fatalf("expected validation error message, got %v", invalidPayload)
	}

	invalidMirrorBody := `{"configs":{"update_image_mirror":"https://docker.1ms.run/path"}}`
	invalidMirrorReq := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(invalidMirrorBody))
	invalidMirrorReq.Header.Set("Authorization", "Bearer "+token)
	invalidMirrorReq.Header.Set("Content-Type", "application/json")
	invalidMirrorRec := httptest.NewRecorder()
	engine.ServeHTTP(invalidMirrorRec, invalidMirrorReq)

	if invalidMirrorRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid update_image_mirror request to return 400, got %d", invalidMirrorRec.Code)
	}
}

func TestConfigBatchSetTimezoneAppliesImmediately(t *testing.T) {
	oldLocal := time.Local
	oldTZ, hadTZ := os.LookupEnv("TZ")
	oldName := service.CurrentPanelTimezone()
	t.Cleanup(func() {
		_ = service.ApplyPanelTimezone(oldName)
		time.Local = oldLocal
		if hadTZ {
			_ = os.Setenv("TZ", oldTZ)
		} else {
			_ = os.Unsetenv("TZ")
		}
	})

	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "timezone-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
	engine := newProtectedRouter()

	body := `{"configs":{"timezone":"UTC"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := service.CurrentPanelTimezone(); got != "UTC" {
		t.Fatalf("expected runtime timezone UTC after save, got %q", got)
	}
	if got := os.Getenv("TZ"); got != "UTC" {
		t.Fatalf("expected process TZ=UTC after save, got %q", got)
	}
	if got := time.Local.String(); got != "UTC" {
		t.Fatalf("expected time.Local UTC after save, got %q", got)
	}

	invalidBody := `{"configs":{"timezone":"Bad/Zone"}}`
	invalidReq := httptest.NewRequest(http.MethodPut, "/api/v1/configs/batch", bytes.NewBufferString(invalidBody))
	invalidReq.Header.Set("Authorization", "Bearer "+token)
	invalidReq.Header.Set("Content-Type", "application/json")
	invalidRec := httptest.NewRecorder()
	engine.ServeHTTP(invalidRec, invalidReq)

	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid timezone request to return 400, got %d", invalidRec.Code)
	}
}
