package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

type failingSecretStore struct{}

func (failingSecretStore) Seal(context.Context, string, []byte) (SealedValue, error) {
	return SealedValue{}, errors.New("seal unavailable")
}

func (failingSecretStore) Open(context.Context, string, SealedValue) ([]byte, error) {
	return nil, errors.New("open unavailable")
}

func (failingSecretStore) Status() SecretStoreStatus {
	return SecretStoreStatus{Provider: "failing", Ready: false}
}

func TestSealEnvVarValueFailsClosedWhenSecretStoreFails(t *testing.T) {
	restore := SetRuntimeSecretStoreForTest(failingSecretStore{})
	defer restore()

	env := &model.EnvVar{Name: "API_TOKEN"}
	err := SealEnvVarValue(context.Background(), env, "plain-secret")
	if err == nil {
		t.Fatal("expected seal error")
	}
	if env.Value != "" || env.Secret || env.Sealed != "" {
		t.Fatalf("secret store failure must not persist plaintext fallback: %+v", env)
	}
}

func TestOpenEnvVarsForRuntimeDecryptsOnlyRuntimePath(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	env := &model.EnvVar{Name: "RUNTIME_SECRET", Enabled: true, Position: 1000}
	if err := SealEnvVarValue(context.Background(), env, "runtime-value"); err != nil {
		t.Fatalf("seal env: %v", err)
	}
	if err := database.DB.Create(env).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}

	responsePayload := OpenEnvVarForResponse(env)
	if got := responsePayload["value"]; got != RedactedEnvSecretValue {
		t.Fatalf("expected response redaction, got %#v", got)
	}

	opened := OpenEnvVarsForRuntime(context.Background(), []model.EnvVar{*env})
	if len(opened) != 1 || opened[0].Value != "runtime-value" {
		t.Fatalf("expected runtime decryption, got %+v", opened)
	}
	if strings.Contains(responsePayload["value"].(string), "runtime-value") {
		t.Fatal("response payload leaked decrypted secret")
	}
}

func TestOpenEnvVarValueFailsClosedOnBadSealedPayload(t *testing.T) {
	env := &model.EnvVar{Name: "BROKEN_SECRET", Value: "plaintext", Secret: true, Sealed: "not-json"}

	if got := OpenEnvVarValue(context.Background(), env); got != "" {
		t.Fatalf("bad sealed payload must not fall back to plaintext, got %q", got)
	}
	if got, err := OpenEnvVarValueStrict(context.Background(), env); err == nil || got != "" {
		t.Fatalf("expected strict open error without plaintext fallback, got value=%q err=%v", got, err)
	}
}

func TestOpenEnvVarValueFailsClosedWhenSecretStoreOpenFails(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	env := &model.EnvVar{Name: "OPEN_FAIL_SECRET", Value: "plaintext"}
	if err := SealEnvVarValue(context.Background(), env, "runtime-value"); err != nil {
		t.Fatalf("seal env: %v", err)
	}
	env.Value = "plaintext"
	restore := SetRuntimeSecretStoreForTest(failingSecretStore{})
	defer restore()

	if got := OpenEnvVarValue(context.Background(), env); got != "" {
		t.Fatalf("secret store open failure must not fall back to plaintext, got %q", got)
	}
	if got, err := OpenEnvVarValueStrict(context.Background(), env); err == nil || got != "" {
		t.Fatalf("expected strict open error without plaintext fallback, got value=%q err=%v", got, err)
	}
}

func TestOpenEnvVarsForRuntimeSkipsSecretsThatFailToDecrypt(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	if err := InitializeRuntimeSecurity(root); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	good := &model.EnvVar{Name: "GOOD_SECRET", Enabled: true, Position: 1000}
	if err := SealEnvVarValue(context.Background(), good, "good-value"); err != nil {
		t.Fatalf("seal good env: %v", err)
	}
	bad := model.EnvVar{Name: "BAD_SECRET", Value: "plaintext", Secret: true, Sealed: "not-json", Enabled: true, Position: 2000}
	plain := model.EnvVar{Name: "PUBLIC", Value: "public-value", Enabled: true, Position: 3000}

	opened := OpenEnvVarsForRuntime(context.Background(), []model.EnvVar{*good, bad, plain})
	if len(opened) != 2 {
		t.Fatalf("expected failed secret to be skipped, got %+v", opened)
	}
	if opened[0].Name != "GOOD_SECRET" || opened[0].Value != "good-value" {
		t.Fatalf("expected good secret decrypted, got %+v", opened[0])
	}
	if opened[1].Name != "PUBLIC" || opened[1].Value != "public-value" {
		t.Fatalf("expected plaintext env preserved, got %+v", opened[1])
	}
}
