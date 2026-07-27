package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestIsCaptchaRequiredWhenEnabled(t *testing.T) {
	testutil.SetupTestEnv(t)

	if IsCaptchaRequired("198.51.100.10", "admin") {
		t.Fatalf("expected captcha to be disabled by default")
	}

	model.SetConfig("captcha_enabled", "true")
	model.SetConfig("captcha_id", "captcha-id")
	model.SetConfig("captcha_key", "secret-key")

	if !IsCaptchaRequired("198.51.100.10", "") {
		t.Fatalf("expected enabled captcha to be required before any failed login")
	}
}

func TestValidateGeeTestSuccess(t *testing.T) {
	payload := CaptchaPayload{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1711000000",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Query().Get("captcha_id"); got != "captcha-id" {
			t.Fatalf("unexpected captcha_id: %s", got)
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if got := r.Form.Get("lot_number"); got != payload.LotNumber {
			t.Fatalf("unexpected lot_number: %s", got)
		}
		if got := r.Form.Get("captcha_output"); got != payload.CaptchaOutput {
			t.Fatalf("unexpected captcha_output: %s", got)
		}
		if got := r.Form.Get("pass_token"); got != payload.PassToken {
			t.Fatalf("unexpected pass_token: %s", got)
		}
		if got := r.Form.Get("gen_time"); got != payload.GenTime {
			t.Fatalf("unexpected gen_time: %s", got)
		}

		expectedSign := expectedSignToken("secret-key", payload.LotNumber)
		if got := r.Form.Get("sign_token"); got != expectedSign {
			t.Fatalf("unexpected sign_token: %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success","reason":"success"}`))
	}))
	defer server.Close()

	oldURL := geetestValidateURL
	oldClient := geetestHTTPClient
	geetestValidateURL = server.URL
	geetestHTTPClient = &http.Client{Timeout: time.Second}
	defer func() {
		geetestValidateURL = oldURL
		geetestHTTPClient = oldClient
	}()

	result, err := validateGeeTest("captcha-id", "secret-key", CaptchaFailModeOpen, payload)
	if err != nil {
		t.Fatalf("validate geetest returned error: %v", err)
	}
	if !result.Passed || result.FailOpen {
		t.Fatalf("expected captcha validation success, got %+v", result)
	}
}

func TestValidateGeeTestRejectsInvalidResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"fail","reason":"forbidden"}`))
	}))
	defer server.Close()

	oldURL := geetestValidateURL
	oldClient := geetestHTTPClient
	geetestValidateURL = server.URL
	geetestHTTPClient = &http.Client{Timeout: time.Second}
	defer func() {
		geetestValidateURL = oldURL
		geetestHTTPClient = oldClient
	}()

	result, err := validateGeeTest("captcha-id", "secret-key", CaptchaFailModeOpen, CaptchaPayload{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1711000000",
	})
	if err != nil {
		t.Fatalf("validate geetest returned error: %v", err)
	}
	if result.Passed {
		t.Fatalf("expected captcha validation to fail, got %+v", result)
	}
	if result.Reason != "forbidden" {
		t.Fatalf("unexpected fail reason: %s", result.Reason)
	}
}

func TestValidateGeeTestFailOpenOnUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream error", http.StatusBadGateway)
	}))
	defer server.Close()

	oldURL := geetestValidateURL
	oldClient := geetestHTTPClient
	geetestValidateURL = server.URL
	geetestHTTPClient = &http.Client{Timeout: time.Second}
	defer func() {
		geetestValidateURL = oldURL
		geetestHTTPClient = oldClient
	}()

	result, err := validateGeeTest("captcha-id", "secret-key", CaptchaFailModeOpen, CaptchaPayload{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1711000000",
	})
	if err != nil {
		t.Fatalf("validate geetest returned error: %v", err)
	}
	if !result.Passed || !result.FailOpen || !result.UpstreamError {
		t.Fatalf("expected fail-open result on upstream failure, got %+v", result)
	}
}

func TestValidateGeeTestStrictOnUpstreamFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream error", http.StatusBadGateway)
	}))
	defer server.Close()

	oldURL := geetestValidateURL
	oldClient := geetestHTTPClient
	geetestValidateURL = server.URL
	geetestHTTPClient = &http.Client{Timeout: time.Second}
	defer func() {
		geetestValidateURL = oldURL
		geetestHTTPClient = oldClient
	}()

	result, err := validateGeeTest("captcha-id", "secret-key", CaptchaFailModeStrict, CaptchaPayload{
		LotNumber:     "lot-123",
		CaptchaOutput: "captcha-output",
		PassToken:     "pass-token",
		GenTime:       "1711000000",
	})
	if err != nil {
		t.Fatalf("validate geetest returned error: %v", err)
	}
	if result.Passed || result.FailOpen || !result.UpstreamError {
		t.Fatalf("expected strict failure result on upstream failure, got %+v", result)
	}
	if result.Reason != "upstream_5xx" {
		t.Fatalf("unexpected strict fail reason: %s", result.Reason)
	}
}

func TestNormalizeCaptchaFailMode(t *testing.T) {
	cases := map[string]string{
		"":         CaptchaFailModeOpen,
		"open":     CaptchaFailModeOpen,
		" strict ": CaptchaFailModeStrict,
		"unknown":  CaptchaFailModeOpen,
	}

	for input, want := range cases {
		if got := NormalizeCaptchaFailMode(input); got != want {
			t.Fatalf("NormalizeCaptchaFailMode(%q) = %q, want %q", input, got, want)
		}
	}

	if !IsCaptchaFailModeValid("") {
		t.Fatal("expected empty value to be accepted as default-open")
	}
	if !IsCaptchaFailModeValid("open") {
		t.Fatal("expected open to be valid")
	}
	if !IsCaptchaFailModeValid("strict") {
		t.Fatal("expected strict to be valid")
	}
	if IsCaptchaFailModeValid("invalid") {
		t.Fatal("expected invalid mode to be rejected")
	}
}

func expectedSignToken(secret, lotNumber string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(lotNumber))
	return hex.EncodeToString(mac.Sum(nil))
}
