package mcptools

import "testing"

// 这两组名单与 final-design §3 F2（网页端 web/src/utils/envSecret.ts 的测试向量）逐字一致。
// 改规则时两边必须一起改、一起跑测试，否则 MCP 与网页端对「哪些变量要遮蔽」会各说各话。
var (
	mustMaskEnvNames = []string{
		"JD_COOKIE", "S3_SECRET_ACCESS_KEY", "SMTP_PASSWORD", "API_TOKEN", "APP_SECRET",
		"DB_PASSWORD", "OPENAI_API_KEY", "APIKEY", "BARK_KEY", "PUSH_KEY", "JD_CK",
		"WSKEY", "TG_BOT_TOKEN", "X_AUTH", "DB_PWD", "MAIL_PASS",
	}
	mustNotMaskEnvNames = []string{
		"SEARCH_KEYWORD", "AUTHOR_NAME", "BYPASS_PROXY", "MONKEY_X", "QUICK_MODE", "KEYWORDS", "PASSPORT_URL",
	}
)

func TestIsSensitiveEnvNameMatchesSharedVectors(t *testing.T) {
	for _, name := range mustMaskEnvNames {
		if !IsSensitiveEnvName(name) {
			t.Errorf("%s 应当被判为敏感变量", name)
		}
	}
	for _, name := range mustNotMaskEnvNames {
		if IsSensitiveEnvName(name) {
			t.Errorf("%s 不应被判为敏感变量", name)
		}
	}
}

func TestIsSensitiveEnvNameIgnoresCase(t *testing.T) {
	for _, name := range []string{"jd_cookie", "Bark_Key", "db_pwd"} {
		if !IsSensitiveEnvName(name) {
			t.Errorf("%s 转大写后命中规则，应当被判为敏感变量", name)
		}
	}
	if IsSensitiveEnvName("") {
		t.Error("空名称不应被判为敏感变量")
	}
}

func TestMaskEnvValue(t *testing.T) {
	cases := []struct {
		value string
		want  string
	}{
		{"abcdefghi", "abc******ghi"},
		{"pt_key=abcdef;pt_pin=zz;", "pt_******zz;"},
		// 8 个字符及以下整段替换，遮罩长度固定，不泄露原值长度。
		{"12345678", "********"},
		{"abc", "********"},
		{"", "********"},
		// 按字符切，不把多字节字符切成半个。
		{"一二三四五六七八九", "一二三******七八九"},
	}
	for _, tc := range cases {
		if got := MaskEnvValue(tc.value); got != tc.want {
			t.Errorf("MaskEnvValue(%q) = %q，期望 %q", tc.value, got, tc.want)
		}
	}
}
