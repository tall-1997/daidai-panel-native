package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daidai-panel/model"
)

const (
	defaultGeeTestValidateURL = "https://gcaptcha4.geetest.com/validate"
	CaptchaFailModeOpen       = "open"
	CaptchaFailModeStrict     = "strict"
)

var (
	ErrCaptchaRequired = errors.New("请先完成人机验证")
	ErrCaptchaInvalid  = errors.New("验证码校验失败")

	geetestHTTPClient  = &http.Client{Timeout: 5 * time.Second}
	geetestValidateURL = defaultGeeTestValidateURL
)

// SetGeeTestValidationForTesting temporarily overrides the GeeTest validation client and URL.
// It is intended for automated tests only.
func SetGeeTestValidationForTesting(client *http.Client, validateURL string) func() {
	oldClient := geetestHTTPClient
	oldURL := geetestValidateURL

	if client != nil {
		geetestHTTPClient = client
	}
	if strings.TrimSpace(validateURL) != "" {
		geetestValidateURL = strings.TrimSpace(validateURL)
	}

	return func() {
		geetestHTTPClient = oldClient
		geetestValidateURL = oldURL
	}
}

type CaptchaPayload struct {
	LotNumber     string `json:"lot_number"`
	CaptchaOutput string `json:"captcha_output"`
	PassToken     string `json:"pass_token"`
	GenTime       string `json:"gen_time"`
}

type CaptchaRuntimeConfig struct {
	SwitchOn             bool
	Configured           bool
	Enabled              bool
	CaptchaID            string
	CaptchaKey           string
	FailMode             string
	RequireAfterFailures int
}

type CaptchaVerificationResult struct {
	Passed        bool
	FailOpen      bool
	UpstreamError bool
	Reason        string
}

type geetestValidateResponse struct {
	Result string `json:"result"`
	Reason string `json:"reason"`
}

func GetCaptchaRuntimeConfig() CaptchaRuntimeConfig {
	switchOn := model.GetRegisteredConfigBool("captcha_enabled")
	captchaID := strings.TrimSpace(model.GetRegisteredConfig("captcha_id"))
	captchaKey := strings.TrimSpace(model.GetRegisteredConfig("captcha_key"))
	failMode := NormalizeCaptchaFailMode(model.GetRegisteredConfig("captcha_fail_mode"))
	configured := captchaID != "" && captchaKey != ""

	return CaptchaRuntimeConfig{
		SwitchOn:             switchOn,
		Configured:           configured,
		Enabled:              switchOn && configured,
		CaptchaID:            captchaID,
		CaptchaKey:           captchaKey,
		FailMode:             failMode,
		RequireAfterFailures: 0,
	}
}

func BuildCaptchaStatusMessage(cfg CaptchaRuntimeConfig) string {
	modeText := "上游异常时默认放行，避免验证码平台短时异常把正常登录完全阻塞"
	if cfg.FailMode == CaptchaFailModeStrict {
		modeText = "上游异常时严格拦截，验证码服务异常期间登录会被阻止"
	}

	switch {
	case !cfg.SwitchOn:
		return "验证码未启用"
	case !cfg.Configured:
		return "验证码已开启，但 Captcha ID 或 Captcha Key 未配置完整，当前不会生效"
	default:
		return fmt.Sprintf("验证码已启用：每次登录都需要先完成人机验证；当前策略：%s", modeText)
	}
}

func NormalizeCaptchaFailMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case CaptchaFailModeStrict:
		return CaptchaFailModeStrict
	default:
		return CaptchaFailModeOpen
	}
}

func IsCaptchaFailModeValid(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", CaptchaFailModeOpen, CaptchaFailModeStrict:
		return true
	default:
		return false
	}
}

func IsCaptchaRequired(ip, username string) bool {
	cfg := GetCaptchaRuntimeConfig()
	return cfg.Enabled
}

func VerifyLoginCaptcha(payload CaptchaPayload) (*CaptchaVerificationResult, error) {
	cfg := GetCaptchaRuntimeConfig()
	if !cfg.Enabled {
		return &CaptchaVerificationResult{Passed: true}, nil
	}

	return validateGeeTest(cfg.CaptchaID, cfg.CaptchaKey, cfg.FailMode, payload)
}

func validateGeeTest(captchaID, captchaKey, failMode string, payload CaptchaPayload) (*CaptchaVerificationResult, error) {
	payload = normalizeCaptchaPayload(payload)
	if payload.LotNumber == "" || payload.CaptchaOutput == "" || payload.PassToken == "" || payload.GenTime == "" {
		return nil, ErrCaptchaRequired
	}

	form := url.Values{}
	form.Set("lot_number", payload.LotNumber)
	form.Set("captcha_output", payload.CaptchaOutput)
	form.Set("pass_token", payload.PassToken)
	form.Set("gen_time", payload.GenTime)
	form.Set("sign_token", buildGeeTestSignToken(captchaKey, payload.LotNumber))

	endpoint := geetestValidateURL + "?captcha_id=" + url.QueryEscape(strings.TrimSpace(captchaID))
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := geetestHTTPClient.Do(req)
	if err != nil {
		log.Printf("geetest upstream request failed: %v", err)
		return captchaUpstreamFailureResult(failMode, "request_error"), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		log.Printf("geetest upstream read body failed: %v", err)
		return captchaUpstreamFailureResult(failMode, "read_error"), nil
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		log.Printf("geetest upstream returned HTTP %d: %s", resp.StatusCode, string(body))
		return captchaUpstreamFailureResult(failMode, "upstream_5xx"), nil
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("geetest upstream returned HTTP %d: %s", resp.StatusCode, string(body))
		return &CaptchaVerificationResult{Passed: false, Reason: "upstream_rejected"}, nil
	}

	var result geetestValidateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("geetest upstream response decode failed: %s", string(body))
		return captchaUpstreamFailureResult(failMode, "decode_error"), nil
	}

	switch strings.ToLower(strings.TrimSpace(result.Result)) {
	case "success":
		return &CaptchaVerificationResult{Passed: true, Reason: strings.TrimSpace(result.Reason)}, nil
	case "fail":
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "invalid"
		}
		return &CaptchaVerificationResult{Passed: false, Reason: reason}, nil
	default:
		return captchaUpstreamFailureResult(failMode, "unexpected_result"), nil
	}
}

func captchaUpstreamFailureResult(failMode, reason string) *CaptchaVerificationResult {
	result := &CaptchaVerificationResult{
		Passed:        false,
		FailOpen:      false,
		UpstreamError: true,
		Reason:        reason,
	}

	if NormalizeCaptchaFailMode(failMode) == CaptchaFailModeOpen {
		result.Passed = true
		result.FailOpen = true
	}

	return result
}

func normalizeCaptchaPayload(payload CaptchaPayload) CaptchaPayload {
	payload.LotNumber = strings.TrimSpace(payload.LotNumber)
	payload.CaptchaOutput = strings.TrimSpace(payload.CaptchaOutput)
	payload.PassToken = strings.TrimSpace(payload.PassToken)
	payload.GenTime = strings.TrimSpace(payload.GenTime)
	return payload
}

func buildGeeTestSignToken(captchaKey, lotNumber string) string {
	mac := hmac.New(sha256.New, []byte(strings.TrimSpace(captchaKey)))
	mac.Write([]byte(strings.TrimSpace(lotNumber)))
	return hex.EncodeToString(mac.Sum(nil))
}
