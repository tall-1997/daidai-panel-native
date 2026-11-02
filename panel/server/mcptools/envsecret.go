package mcptools

import "strings"

// 敏感环境变量的判定规则，与网页端 web/src/utils/envSecret.ts（issue #127）是同一份定义。
// 两边的测试向量必须一致：改任何一边都要同步另一边，并同时更新 envsecret_test.go 与前端的两组名单。
//
// 规则：名称转大写后
//   - 任意位置包含 sensitiveEnvNameSubstrings 中的一项 → 敏感；
//   - 或者按 _ 切段后，某一段整段等于 sensitiveEnvNameSegments 中的一项 → 敏感。
//
// KEY / PASS / AUTH 这类短词只做整段匹配，是为了不误伤 SEARCH_KEYWORD、BYPASS_PROXY、AUTHOR_NAME。
var (
	sensitiveEnvNameSubstrings = []string{
		"TOKEN", "SECRET", "PASSWORD", "PASSWD", "COOKIE",
		"CREDENTIAL", "WSKEY", "PRIVATE", "APIKEY", "ACCESSKEY",
	}
	sensitiveEnvNameSegments = map[string]struct{}{
		"KEY": {}, "PWD": {}, "PASS": {}, "AUTH": {}, "SK": {},
		"AK": {}, "SID": {}, "CK": {}, "WSCK": {},
	}
)

// IsSensitiveEnvName 判断环境变量名是否像凭据。
func IsSensitiveEnvName(name string) bool {
	upper := strings.ToUpper(name)
	if upper == "" {
		return false
	}
	for _, needle := range sensitiveEnvNameSubstrings {
		if strings.Contains(upper, needle) {
			return true
		}
	}
	for _, segment := range strings.Split(upper, "_") {
		if _, hit := sensitiveEnvNameSegments[segment]; hit {
			return true
		}
	}
	return false
}

// MaskEnvValue 遮蔽敏感值：超过 8 个字符时保留前 3 后 3、中间固定 6 个星号；
// 否则整段换成固定 8 个星号。遮罩长度固定，不泄露原值长度。
//
// 按字符（rune）计数：网页端按 UTF-16 码元计数，两者只在罕见的补充平面字符上有差别，
// 这里优先保证输出是合法 UTF-8。
func MaskEnvValue(value string) string {
	runes := []rune(value)
	if len(runes) > 8 {
		return string(runes[:3]) + "******" + string(runes[len(runes)-3:])
	}
	return "********"
}
