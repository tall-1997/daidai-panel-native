package handler_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// OpenAPI 的 scope 清单此前在三处各存一份，而且已经漂了：
// 服务端强制 8 个、Web 只列 7 个（缺 notifications）、APP 只列 6 个（缺 notifications 与 backup）。
// 漂移的表现不是报错，而是「这个权限永远勾不上」—— 建出来的应用调该类接口一律 403，
// 界面上却看不出少了什么，排查起来毫无线索。
//
// 本测试把服务端实际强制的 scope 与 Web 的下拉选项双向绑死：
// 服务端加了 Web 没加、或 Web 列了服务端根本不认，两个方向都让 CI 红。
//
// APP 那份（open_api_page.dart 的 _apiScopeOptions）在另一个仓库，这里管不到，
// 只能靠 docs/script-api.md 与发布说明提醒。要根治得让服务端下发 scope 字典。

var (
	openAPIAccessCallPattern = regexp.MustCompile(`OpenAPIAccess\("([a-z_]+)"\)`)
	webScopeOptionsPattern   = regexp.MustCompile(`(?s)const scopeOptions = \[(.*?)\];`)
	webScopeValuePattern     = regexp.MustCompile(`value:\s*"([a-z_]+)"`)
)

func collectServerEnforcedScopes(t *testing.T, serverRoot string) []string {
	t.Helper()

	found := map[string]bool{}
	err := filepath.Walk(serverRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, match := range openAPIAccessCallPattern.FindAllStringSubmatch(string(content), -1) {
			found[match[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk server tree: %v", err)
	}

	scopes := make([]string, 0, len(found))
	for scope := range found {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	return scopes
}

func collectWebScopeOptions(t *testing.T, webFile string) []string {
	t.Helper()

	content, err := os.ReadFile(webFile)
	if err != nil {
		// Web 目录被裁掉的场景（例如只打后端产物）不该让后端测试红。
		if os.IsNotExist(err) {
			t.Skipf("web source not present: %s", webFile)
		}
		t.Fatalf("read web open-api page: %v", err)
	}

	block := webScopeOptionsPattern.FindSubmatch(content)
	if block == nil {
		t.Fatalf("未能在 %s 里找到 const scopeOptions = [...]，选项清单可能被改名或换了写法", webFile)
	}

	scopes := make([]string, 0, 8)
	for _, match := range webScopeValuePattern.FindAllSubmatch(block[1], -1) {
		scopes = append(scopes, string(match[1]))
	}
	sort.Strings(scopes)
	return scopes
}

func TestOpenAPIScopeOptionsMatchServerEnforcement(t *testing.T) {
	serverScopes := collectServerEnforcedScopes(t, "..")
	if len(serverScopes) == 0 {
		t.Fatal("没有扫到任何 OpenAPIAccess 调用，正则可能已经失配")
	}

	webScopes := collectWebScopeOptions(t, filepath.Join("..", "..", "web", "src", "views", "open-api", "index.vue"))

	serverSet := map[string]bool{}
	for _, s := range serverScopes {
		serverSet[s] = true
	}
	webSet := map[string]bool{}
	for _, s := range webScopes {
		webSet[s] = true
	}

	for _, s := range serverScopes {
		if !webSet[s] {
			t.Errorf("服务端强制了 scope %q，但 Web 的 scopeOptions 里没有 —— 用户永远勾不上这个权限", s)
		}
	}
	for _, s := range webScopes {
		if !serverSet[s] {
			t.Errorf("Web 列了 scope %q，但服务端没有任何接口用 OpenAPIAccess(%q) —— 勾了也没用", s, s)
		}
	}
}
