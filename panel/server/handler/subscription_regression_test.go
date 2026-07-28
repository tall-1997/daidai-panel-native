package handler_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

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

func TestSubscriptionCreatePersistsForceOverwriteFalse(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"demo-sub","type":"git-repo","url":"https://github.com/example/demo.git","force_overwrite":false}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["force_overwrite"].(bool); got {
		t.Fatalf("expected response force_overwrite false, got %v", data["force_overwrite"])
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "demo-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.ForceOverwrite == nil || *sub.ForceOverwrite {
		t.Fatalf("expected force_overwrite persisted false, got %#v", sub.ForceOverwrite)
	}
}

func TestSubscriptionUpdateKeepsForceOverwriteFalseAfterReload(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	forceOverwrite := true
	sub := model.Subscription{
		Name:           "editable-sub",
		Type:           model.SubTypeGitRepo,
		URL:            "https://github.com/example/editable.git",
		Enabled:        true,
		ForceOverwrite: &forceOverwrite,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	updateBody := `{"force_overwrite":false,"alias":"edited-sub"}`
	updateRec := performJSONRequest(engine, http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10), updateBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.ForceOverwrite == nil || *updated.ForceOverwrite {
		t.Fatalf("expected force_overwrite updated false, got %#v", updated.ForceOverwrite)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions?keyword=editable-sub", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listRec.Code, listRec.Body.String())
	}

	listPayload := decodeJSONMap(t, listRec)
	items, ok := listPayload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected subscription list, got %T", listPayload["data"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected list item map, got %T", items[0])
	}
	if got, _ := item["force_overwrite"].(bool); got {
		t.Fatalf("expected list force_overwrite false after reload, got %v", item["force_overwrite"])
	}
}

func TestSubscriptionCreatePersistsTokenAuthWithoutLeakingToken(t *testing.T) {
	testutil.SetupTestEnv(t)
	restore := service.SetRuntimeSecretStoreForTest(handlerSecretStore{})
	defer restore()

	operator := testutil.MustCreateUser(t, "subscription-token-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"token-sub","type":"git-repo","url":"https://github.com/example/private.git","auth_type":"token","auth_token":"ghp_demo_token"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["auth_type"].(string); got != model.SubAuthTypeToken {
		t.Fatalf("expected auth_type=%q, got %#v", model.SubAuthTypeToken, data["auth_type"])
	}
	if got, _ := data["has_auth_token"].(bool); !got {
		t.Fatalf("expected has_auth_token=true, got %#v", data["has_auth_token"])
	}
	if _, exists := data["auth_token"]; exists {
		t.Fatalf("did not expect auth_token in response payload: %#v", data)
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "token-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.EffectiveAuthType() != model.SubAuthTypeToken {
		t.Fatalf("expected stored auth type token, got %q", sub.EffectiveAuthType())
	}
	if strings.TrimSpace(sub.AuthToken) != "" {
		t.Fatalf("expected plaintext auth token to be empty, got %q", sub.AuthToken)
	}
	if !sub.HasAuthTokenSecret() {
		t.Fatalf("expected auth token SecretStore fields to be populated")
	}
	if strings.Contains(string(sub.AuthTokenSecretCipher), "ghp_demo_token") {
		t.Fatalf("expected sealed auth token payload, got %q", string(sub.AuthTokenSecretCipher))
	}
	if sub.SSHKeyID != nil {
		t.Fatalf("expected ssh_key_id cleared for token auth, got %#v", sub.SSHKeyID)
	}
}

func TestSubscriptionUpdateKeepsExistingTokenWhenAuthTokenOmitted(t *testing.T) {
	testutil.SetupTestEnv(t)
	restore := service.SetRuntimeSecretStoreForTest(handlerSecretStore{})
	defer restore()

	operator := testutil.MustCreateUser(t, "subscription-token-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{
		Name:      "editable-token-sub",
		Type:      model.SubTypeGitRepo,
		URL:       "https://github.com/example/private.git",
		Enabled:   true,
		AuthType:  model.SubAuthTypeToken,
		AuthToken: "ghp_keep_me",
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	updateBody := `{"auth_type":"token","auth_token":"","alias":"token-edited"}`
	updateRec := performJSONRequest(engine, http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10), updateBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if strings.TrimSpace(updated.AuthToken) != "" {
		t.Fatalf("expected legacy plaintext auth token to be cleared, got %q", updated.AuthToken)
	}
	if !updated.HasAuthTokenSecret() {
		t.Fatalf("expected legacy auth token to migrate into SecretStore fields")
	}
	if updated.EffectiveAuthType() != model.SubAuthTypeToken {
		t.Fatalf("expected auth type token after update, got %q", updated.EffectiveAuthType())
	}
}

func TestSubscriptionPullStreamResumesPersistedLogFromCursor(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-stream-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{Name: "stream-sub", Type: model.SubTypeGitRepo, URL: "https://github.com/example/stream.git", Enabled: true}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	log := model.SubLog{SubscriptionID: sub.ID, OperationID: "subop-1", Status: 1, Content: "line-1\nline-2\nline-3\n", LogCursor: 3}
	if err := database.DB.Create(&log).Error; err != nil {
		t.Fatalf("create subscription log: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10)+"/pull-stream?operation_id=subop-1&cursor=1", map[string]string{
		"Authorization": "Bearer " + token,
	})
	body, _ := io.ReadAll(rec.Result().Body)
	text := string(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, text)
	}
	if strings.Contains(text, "line-1") || !strings.Contains(text, "id: 2\ndata: line-2") || !strings.Contains(text, "id: 3\ndata: line-3") {
		t.Fatalf("unexpected resumed SSE body: %s", text)
	}
}
