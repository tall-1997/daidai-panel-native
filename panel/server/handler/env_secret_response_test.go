package handler_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

type failingHandlerSecretStore struct{}

func (failingHandlerSecretStore) Seal(context.Context, string, []byte) (service.SealedValue, error) {
	return service.SealedValue{}, errors.New("seal unavailable")
}

func (failingHandlerSecretStore) Open(context.Context, string, service.SealedValue) ([]byte, error) {
	return nil, errors.New("open unavailable")
}

func (failingHandlerSecretStore) Status() service.SecretStoreStatus {
	return service.SecretStoreStatus{Provider: "failing", Ready: false}
}

func TestEnvResponsesRedactSecretValuesByDefault(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-secret-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	createRec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs", `{"name":"SECRET_TOKEN","value":"super-secret"}`, headers, "")
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	created := decodeJSONMap(t, createRec)["data"].(map[string]interface{})
	if got := created["value"]; got != service.RedactedEnvSecretValue {
		t.Fatalf("create response leaked secret: %#v", got)
	}
	id := uint(created["id"].(float64))
	var beforeUpdate model.EnvVar
	if err := database.DB.First(&beforeUpdate, id).Error; err != nil {
		t.Fatalf("load created env: %v", err)
	}

	paths := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/envs"},
		{name: "get", method: http.MethodGet, path: fmt.Sprintf("/api/v1/envs/%d", id)},
		{name: "update", method: http.MethodPut, path: fmt.Sprintf("/api/v1/envs/%d", id), body: `{"remarks":"updated"}`},
		{name: "disable", method: http.MethodPut, path: fmt.Sprintf("/api/v1/envs/%d/disable", id)},
		{name: "enable", method: http.MethodPut, path: fmt.Sprintf("/api/v1/envs/%d/enable", id)},
	}

	for _, tt := range paths {
		t.Run(tt.name, func(t *testing.T) {
			var rec = performRequest(engine, tt.method, tt.path, headers)
			if tt.body != "" {
				rec = performJSONRequest(engine, tt.method, tt.path, tt.body, headers, "")
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			payload := decodeJSONMap(t, rec)
			data := payload["data"]
			if tt.name == "list" {
				items := data.([]interface{})
				if len(items) != 1 {
					t.Fatalf("expected one item, got %#v", items)
				}
				data = items[0]
			}
			env := data.(map[string]interface{})
			if got := env["value"]; got != service.RedactedEnvSecretValue {
				t.Fatalf("%s response leaked secret: %#v", tt.name, got)
			}
		})
	}

	redactedUpdate := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", id), `{"value":"********","remarks":"keep-secret"}`, headers, "")
	if redactedUpdate.Code != http.StatusOK {
		t.Fatalf("expected redacted update 200, got %d body=%s", redactedUpdate.Code, redactedUpdate.Body.String())
	}
	updated := decodeJSONMap(t, redactedUpdate)["data"].(map[string]interface{})
	if got := updated["value"]; got != service.RedactedEnvSecretValue {
		t.Fatalf("redacted update response leaked secret: %#v", got)
	}
	var afterUpdate model.EnvVar
	if err := database.DB.First(&afterUpdate, id).Error; err != nil {
		t.Fatalf("reload env after redacted update: %v", err)
	}
	if afterUpdate.Sealed != beforeUpdate.Sealed || afterUpdate.Value != "" || !afterUpdate.Secret {
		t.Fatalf("redacted update should preserve sealed secret, before=%+v after=%+v", beforeUpdate, afterUpdate)
	}
}

func TestEnvResponsesRedactLegacyPlaintextValuesByDefault(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-legacy-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	env := &model.EnvVar{Name: "LEGACY_TOKEN", Value: "legacy-secret", Enabled: true, Position: 1000}
	if err := database.DB.Create(env).Error; err != nil {
		t.Fatalf("create legacy env: %v", err)
	}

	for _, path := range []string{"/api/v1/envs", fmt.Sprintf("/api/v1/envs/%d", env.ID)} {
		rec := performRequest(engine, http.MethodGet, path, headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}
		payload := decodeJSONMap(t, rec)
		data := payload["data"]
		if path == "/api/v1/envs" {
			items := data.([]interface{})
			if len(items) != 1 {
				t.Fatalf("expected one item, got %#v", items)
			}
			data = items[0]
		}
		item := data.(map[string]interface{})
		if got := item["value"]; got != service.RedactedEnvSecretValue {
			t.Fatalf("%s response leaked legacy plaintext: %#v", path, got)
		}
	}
}

func TestEnvCreatePropagatesSealFailure(t *testing.T) {
	testutil.SetupTestEnv(t)
	restore := service.SetRuntimeSecretStoreForTest(failingHandlerSecretStore{})
	defer restore()

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-seal-failure-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs", `{"name":"BROKEN_SECRET","value":"plain-secret"}`, headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected seal failure 500, got %d body=%s", rec.Code, rec.Body.String())
	}

	var count int64
	if err := database.DB.Model(&model.EnvVar{}).Where("name = ? OR value = ?", "BROKEN_SECRET", "plain-secret").Count(&count).Error; err != nil {
		t.Fatalf("count env rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("seal failure must not persist env row or plaintext, count=%d", count)
	}
}

func TestEnvExportRedactsSecretsByDefault(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := service.InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-export-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	env := &model.EnvVar{Name: "EXPORT_SECRET", Enabled: true, Position: 1000}
	if err := service.SealEnvVarValue(nil, env, "export-secret"); err != nil {
		t.Fatalf("seal env: %v", err)
	}
	if err := database.DB.Create(env).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}

	for _, path := range []string{"/api/v1/envs/export", "/api/v1/envs/export-all"} {
		rec := performRequest(engine, http.MethodGet, path, headers)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}
		if body := rec.Body.String(); body == "" || body == "export-secret" || containsSecret(body, "export-secret") {
			t.Fatalf("export response leaked secret for %s: %s", path, body)
		}
	}
}

func containsSecret(body, secret string) bool {
	for i := 0; i+len(secret) <= len(body); i++ {
		if body[i:i+len(secret)] == secret {
			return true
		}
	}
	return false
}
