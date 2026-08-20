package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type notifyResultCheck func(body []byte) error

func httpPostChecked(url string, body interface{}, headers map[string]string, check notifyResultCheck) error {
	return httpPostCheckedWithClient(NewHTTPClient(10*time.Second), url, body, headers, check)
}

func httpPostCheckedWithClient(client *http.Client, url string, body interface{}, headers map[string]string, check notifyResultCheck) error {
	if err := validateWebhookURL(url); err != nil {
		return err
	}
	client = webhookHTTPClient(client)
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 限长读取：推送接口的响应体都很小，这里既避免异常大响应吃内存，
	// 也让连接能被正常复用（原来成功路径完全不读 body）。
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	if check != nil {
		return check(respBody)
	}
	return nil
}

// parseNotifyResponseObject 只接受 JSON 对象；纯文本、数组、空响应一律当作「无法判定」。
func parseNotifyResponseObject(body []byte) (map[string]interface{}, bool) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, false
	}
	payload := map[string]interface{}{}
	if err := json.Unmarshal(trimmed, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

// notifyResponseNumber 兼容错误码被写成字符串的情况（部分服务返回 "code": "0"）。
func notifyResponseNumber(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func notifyResponseMessage(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if text, ok := payload[key].(string); ok {
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// checkNotifyCodeField 适用于「HTTP 200 + JSON 里一个数字业务码」的响应。
func checkNotifyCodeField(codeKey string, successValues []float64, messageKeys ...string) notifyResultCheck {
	return func(body []byte) error {
		payload, ok := parseNotifyResponseObject(body)
		if !ok {
			return nil
		}
		raw, exists := payload[codeKey]
		if !exists {
			return nil
		}
		code, ok := notifyResponseNumber(raw)
		if !ok {
			return nil
		}
		for _, success := range successValues {
			if code == success {
				return nil
			}
		}
		if message := notifyResponseMessage(payload, messageKeys...); message != "" {
			return fmt.Errorf("推送服务返回失败（%s=%v）：%s", codeKey, raw, message)
		}
		return fmt.Errorf("推送服务返回失败（%s=%v）", codeKey, raw)
	}
}

// combineNotifyChecks 依次执行，返回第一个确定的失败。
// 用于同一个厂商存在多种响应体形态的情况（例如飞书的 code 与 StatusCode 两代字段）。
func combineNotifyChecks(checks ...notifyResultCheck) notifyResultCheck {
	return func(body []byte) error {
		for _, check := range checks {
			if err := check(body); err != nil {
				return err
			}
		}
		return nil
	}
}

// checkTelegramResult：Telegram Bot API 固定返回 {"ok":true|false,"description":"..."}。
func checkTelegramResult(body []byte) error {
	payload, ok := parseNotifyResponseObject(body)
	if !ok {
		return nil
	}
	okValue, exists := payload["ok"].(bool)
	if !exists || okValue {
		return nil
	}
	if message := notifyResponseMessage(payload, "description"); message != "" {
		return fmt.Errorf("Telegram 返回失败：%s", message)
	}
	return fmt.Errorf("Telegram 返回失败")
}

var (
	// errcode 是微信系（企业微信机器人 / 企业微信应用 / 钉钉）统一的业务码字段。
	checkWecomStyleResult = checkNotifyCodeField("errcode", []float64{0}, "errmsg")
	// 飞书自定义机器人新老两代响应字段并存，两个都查。
	checkFeishuResult = combineNotifyChecks(
		checkNotifyCodeField("code", []float64{0}, "msg"),
		checkNotifyCodeField("StatusCode", []float64{0}, "StatusMessage"),
	)
	checkPushplusResult   = checkNotifyCodeField("code", []float64{200}, "msg")
	checkServerchanResult = checkNotifyCodeField("code", []float64{0}, "message", "msg")
	checkBarkResult       = checkNotifyCodeField("code", []float64{200}, "message")
)
