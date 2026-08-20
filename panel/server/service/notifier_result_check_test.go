package service

import (
	"strings"
	"testing"
)

// 这组用例锁住 notifyResultCheck 的保守口径：
// 只有能【确定】是业务失败时才报错，其余一律放行。
// 口径一旦被改宽，本来能正常发送的渠道就会集体变红，所以这里逐条钉死。
func TestNotifyResultChecksPassWhenUndeterminable(t *testing.T) {
	checks := map[string]notifyResultCheck{
		"pushplus":   checkPushplusResult,
		"serverchan": checkServerchanResult,
		"bark":       checkBarkResult,
		"wecom":      checkWecomStyleResult,
		"feishu":     checkFeishuResult,
		"telegram":   checkTelegramResult,
	}

	undeterminable := map[string]string{
		"空响应":       "",
		"纯文本":       "success",
		"HTML":      "<html><body>ok</body></html>",
		"JSON 数组":   `[{"code":999}]`,
		"非法 JSON":   `{"code":`,
		"缺少业务码字段":   `{"data":{"id":"abc"}}`,
		"业务码是对象":    `{"code":{"nested":1}}`,
		"业务码是不可解析串": `{"code":"not-a-number"}`,
	}

	for name, check := range checks {
		for caseName, body := range undeterminable {
			if err := check([]byte(body)); err != nil {
				t.Fatalf("%s 校验器在「%s」场景不应报错，却返回：%v", name, caseName, err)
			}
		}
	}
}

func TestNotifyResultChecksAcceptSuccessResponses(t *testing.T) {
	cases := []struct {
		name  string
		check notifyResultCheck
		body  string
	}{
		{"pushplus", checkPushplusResult, `{"code":200,"msg":"请求成功","data":"xxx"}`},
		{"serverchan", checkServerchanResult, `{"code":0,"message":"","data":{"pushid":"1"}}`},
		{"bark", checkBarkResult, `{"code":200,"message":"success","timestamp":1}`},
		{"dingtalk", checkWecomStyleResult, `{"errcode":0,"errmsg":"ok"}`},
		{"feishu 新版", checkFeishuResult, `{"code":0,"msg":"success"}`},
		{"feishu 老版", checkFeishuResult, `{"StatusCode":0,"StatusMessage":"success"}`},
		{"telegram", checkTelegramResult, `{"ok":true,"result":{"message_id":1}}`},
		// 业务码写成字符串的情况也要认。
		{"业务码为字符串", checkPushplusResult, `{"code":"200","msg":"ok"}`},
	}

	for _, tc := range cases {
		if err := tc.check([]byte(tc.body)); err != nil {
			t.Fatalf("%s 成功响应不应报错，却返回：%v", tc.name, err)
		}
	}
}

func TestNotifyResultChecksRejectBusinessFailures(t *testing.T) {
	cases := []struct {
		name        string
		check       notifyResultCheck
		body        string
		wantMessage string
	}{
		{"pushplus token 无效", checkPushplusResult, `{"code":999,"msg":"token为空"}`, "token为空"},
		{"serverchan key 错误", checkServerchanResult, `{"code":40001,"message":"bad pushkey"}`, "bad pushkey"},
		{"bark 设备无效", checkBarkResult, `{"code":400,"message":"device token invalid"}`, "device token invalid"},
		{"钉钉加签失败", checkWecomStyleResult, `{"errcode":310000,"errmsg":"sign not match"}`, "sign not match"},
		{"飞书新版失败", checkFeishuResult, `{"code":19021,"msg":"sign match fail"}`, "sign match fail"},
		{"飞书老版失败", checkFeishuResult, `{"StatusCode":19021,"StatusMessage":"sign match fail"}`, "sign match fail"},
		{"telegram 失败", checkTelegramResult, `{"ok":false,"description":"chat not found"}`, "chat not found"},
	}

	for _, tc := range cases {
		err := tc.check([]byte(tc.body))
		if err == nil {
			t.Fatalf("%s 应判定为失败，却放行了", tc.name)
		}
		if !strings.Contains(err.Error(), tc.wantMessage) {
			t.Fatalf("%s 的错误信息应包含 %q，实际为：%v", tc.name, tc.wantMessage, err)
		}
	}
}

// 业务码字段缺失但存在同名的另一代字段时，组合校验器不能互相干扰。
func TestFeishuCheckIgnoresAbsentFieldForm(t *testing.T) {
	if err := checkFeishuResult([]byte(`{"StatusCode":0,"StatusMessage":"success","Extra":null}`)); err != nil {
		t.Fatalf("只有老版字段的成功响应不应报错：%v", err)
	}
	if err := checkFeishuResult([]byte(`{"code":0,"msg":"success","data":{}}`)); err != nil {
		t.Fatalf("只有新版字段的成功响应不应报错：%v", err)
	}
}
