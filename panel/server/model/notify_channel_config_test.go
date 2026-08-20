package model_test

import (
	"encoding/json"
	"strings"
	"testing"

	"daidai-panel/model"
)

// TestNormalizeNotifyChannelConfigCoercesScalarValues 锁死这条修复的核心行为。
//
// 真实事故：APP 把 smtp_ssl 写成了 JSON 布尔 false，服务端 sendToChannel 里
// json.Unmarshal 到 map[string]string 直接失败，整个邮件渠道的所有通知（含测试按钮）
// 全挂，报的还是一句用户看不懂的 cannot unmarshal bool into Go value of type string。
func TestNormalizeNotifyChannelConfigCoercesScalarValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "布尔归一成字符串（就是 APP 写坏 smtp_ssl 的那种）",
			input: `{"smtp_ssl":false,"smtp_host":"smtp.qq.com"}`,
			want:  map[string]string{"smtp_ssl": "false", "smtp_host": "smtp.qq.com"},
		},
		{
			name:  "整数归一成字符串",
			input: `{"timeout":30,"priority":5}`,
			want:  map[string]string{"timeout": "30", "priority": "5"},
		},
		{
			name:  "大整数不能被科学计数法毁掉",
			input: `{"max_size":102400000}`,
			want:  map[string]string{"max_size": "102400000"},
		},
		{
			name:  "小数原样保留",
			input: `{"ratio":0.5}`,
			want:  map[string]string{"ratio": "0.5"},
		},
		{
			name:  "null 视为没填",
			input: `{"proxy":null}`,
			want:  map[string]string{"proxy": ""},
		},
		{
			name:  "本来就是字符串的原样保留",
			input: `{"url":"https://example.com/webhook?a=1&b=2"}`,
			want:  map[string]string{"url": "https://example.com/webhook?a=1&b=2"},
		},
		{
			name:  "空串补成空对象",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "只有空白也补成空对象",
			input: "   \n\t ",
			want:  map[string]string{},
		},
		{
			name:  "custom 渠道的 headers/body 是 JSON 文本字符串，必须原样保留",
			input: `{"headers":"{\"Authorization\":\"Bearer xxx\"}","body":"{\"title\":\"{{title}}\"}"}`,
			want: map[string]string{
				"headers": `{"Authorization":"Bearer xxx"}`,
				"body":    `{"title":"{{title}}"}`,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			normalized, err := model.NormalizeNotifyChannelConfig(tc.input)
			if err != nil {
				t.Fatalf("不应报错，实际: %v", err)
			}

			// 归一结果必须能被服务端真正的消费方式读出来，这才是这条修复的全部意义。
			var cfg map[string]string
			if err := json.Unmarshal([]byte(normalized), &cfg); err != nil {
				t.Fatalf("归一后仍无法反序列化成 map[string]string: %v\n  结果: %s", err, normalized)
			}

			if len(cfg) != len(tc.want) {
				t.Fatalf("键数量不符\n  期望: %#v\n  实际: %#v", tc.want, cfg)
			}
			for key, want := range tc.want {
				if cfg[key] != want {
					t.Errorf("键 %q\n  期望: %q\n  实际: %q", key, want, cfg[key])
				}
			}
		})
	}
}

// TestNormalizeNotifyChannelConfigRejectsUnrecoverableValues 断言不可逆的值必须报错，
// 而不是被 fmt.Sprint 成 map[a:1] 这种 Go 语法垃圾静默存下去。
//
// 这正是 custom 渠道原始 JSON 编辑框最容易踩的坑：headers 的值本身是 JSON 文本，
// 但在 config 里必须是字符串，写成嵌套对象就会命中这里。
func TestNormalizeNotifyChannelConfigRejectsUnrecoverableValues(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantInMsg string
	}{
		{
			name:      "值是嵌套对象",
			input:     `{"headers":{"Authorization":"Bearer xxx"}}`,
			wantInMsg: `"headers"`,
		},
		{
			name:      "值是数组",
			input:     `{"uids":["u1","u2"]}`,
			wantInMsg: `"uids"`,
		},
		{
			name:      "顶层不是对象",
			input:     `["a","b"]`,
			wantInMsg: "必须是 JSON 对象",
		},
		{
			name:      "顶层是裸字符串",
			input:     `"just a string"`,
			wantInMsg: "必须是 JSON 对象",
		},
		{
			name:      "非法 JSON",
			input:     `{"url":`,
			wantInMsg: "不是合法的 JSON",
		},
		{
			name:      "结尾有多余内容",
			input:     `{"url":"https://a"} {"url":"https://b"}`,
			wantInMsg: "不是合法的 JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := model.NormalizeNotifyChannelConfig(tc.input)
			if err == nil {
				t.Fatal("应当报错，实际通过了")
			}
			if !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("错误信息应包含 %q，实际: %s", tc.wantInMsg, err.Error())
			}
			// 错误信息是直接展示给用户的，必须是中文而不是 Go 的原始类型错误。
			if strings.Contains(err.Error(), "cannot unmarshal") {
				t.Errorf("不应把 Go 原始错误透给用户: %s", err.Error())
			}
		})
	}
}

// TestNormalizeNotifyChannelConfigIsIdempotent 保证「保存 -> 读回 -> 再保存」不会越改越歪。
// Web 和 APP 都是把读回来的 config 原样再提交一次，不幂等会导致值在多次编辑后漂移。
func TestNormalizeNotifyChannelConfigIsIdempotent(t *testing.T) {
	inputs := []string{
		`{"smtp_ssl":false,"smtp_port":465,"smtp_host":"smtp.qq.com"}`,
		`{"url":"https://example.com/hook?a=1&b=2","body":"{\"title\":\"{{title}}\"}"}`,
		`{}`,
		"",
	}

	for _, input := range inputs {
		once, err := model.NormalizeNotifyChannelConfig(input)
		if err != nil {
			t.Fatalf("首次归一失败: %v", err)
		}
		twice, err := model.NormalizeNotifyChannelConfig(once)
		if err != nil {
			t.Fatalf("二次归一失败: %v", err)
		}
		if once != twice {
			t.Errorf("归一不幂等\n  输入: %s\n  一次: %s\n  两次: %s", input, once, twice)
		}
	}
}

// TestNormalizeNotifyChannelConfigKeepsUrlCharactersReadable 断言不做 HTML 转义。
// 默认的 json.Marshal 会把 URL query 里的与号转成 Unicode 转义序列，用户下次打开
// 原始 JSON 编辑框会看到一串不认识的字符，误以为配置被改坏了。
func TestNormalizeNotifyChannelConfigKeepsUrlCharactersReadable(t *testing.T) {
	normalized, err := model.NormalizeNotifyChannelConfig(`{"url":"https://a.com/x?a=1&b=2"}`)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if strings.Contains(normalized, "\\u0026") {
		t.Errorf("URL 里的与号被转义了: %s", normalized)
	}
}
