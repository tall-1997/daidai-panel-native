package service

import (
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/model"
)

// 用户实际在用的三种注释头写法（js 带空格、mjs 不带空格、sh 用 ##），
// 以及 py/JSDoc/HTML 的常见变体。这些以前全都识别不到，任务名一路回退成文件名。
func TestResolveSubscriptionTaskNameSupportsCommentHeaders(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  string
		want     string
	}{
		{
			name:     "mjs 无空格 //name:",
			filename: "wol.mjs",
			content:  "//name: 远程开机\n//cron: 0 7,14 * * *\nexport default 1\n",
			want:     "远程开机",
		},
		{
			name:     "js 有空格 // name:",
			filename: "nestle.js",
			content:  "// name: 雀巢会员\n// cron: 30 12 * * *\nconsole.log(1)\n",
			want:     "雀巢会员",
		},
		{
			name:     "sh 双井号 ## name:",
			filename: "ddns.sh",
			content:  "#!/bin/bash\n## name: DDNS IP更新\n## cron: */5 * * * *\necho hi\n",
			want:     "DDNS IP更新",
		},
		{
			name:     "py 单井号 # name:",
			filename: "sign.py",
			content:  "# name: 每日签到\n# cron: 0 8 * * *\nprint(1)\n",
			want:     "每日签到",
		},
		{
			name:     "JSDoc 星号前缀",
			filename: "jsdoc.js",
			content:  "/**\n * name: 京东比价\n * cron: 0 9 * * *\n */\n",
			want:     "京东比价",
		},
		{
			name:     "@name 标签",
			filename: "tag.js",
			content:  "// @name: 标签写法\n",
			want:     "标签写法",
		},
		{
			name:     "HTML 注释要去掉收尾标记",
			filename: "page.js",
			content:  "<!-- name: 网页任务 -->\n",
			want:     "网页任务",
		},
		{
			name:     "带引号的名称去引号",
			filename: "quoted.js",
			content:  "// name: \"引号任务\"\n",
			want:     "引号任务",
		},
		{
			name:     "全角冒号",
			filename: "fullwidth.js",
			content:  "// name： 全角冒号\n",
			want:     "全角冒号",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scriptPath := filepath.Join(root, tc.filename)
			if err := os.WriteFile(scriptPath, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("write script: %v", err)
			}

			if got := resolveSubscriptionTaskName(scriptPath, "fallback"); got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

// 关键的误判防线：JS/TS 对象字面量里的 name 字段不能被当成任务名。
// 这也是「注释头必须带注释标记」这条规则存在的唯一理由。
func TestResolveSubscriptionTaskNameIgnoresObjectLiteralNameField(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "config.js")
	content := "const config = {\n  name: '不是任务名',\n  url: 'https://example.com',\n}\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if got := resolveSubscriptionTaskName(scriptPath, "config"); got != "config" {
		t.Fatalf("expected fallback to filename, got %q", got)
	}
}

// 同名字段的另一种常见误判来源：filename / username 之类以 name 结尾的键。
func TestResolveSubscriptionTaskNameIgnoresSuffixedNameKeys(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "upload.js")
	content := "// filename: a.txt\n// username: bob\nconsole.log(1)\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if got := resolveSubscriptionTaskName(scriptPath, "upload"); got != "upload" {
		t.Fatalf("expected fallback to filename, got %q", got)
	}
}

// 优先级：new Env 必须压过注释头，否则老用户升级后一批任务会被静默改名。
// 注意注释头刻意写在 new Env 之前，验证的是「扫完再决定」而不是「谁先出现用谁」。
func TestResolveSubscriptionTaskNamePrefersNewEnvOverCommentHeader(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "both.js")
	content := "// name: 注释里的名字\nconst $ = new Env('EnvName');\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if got := resolveSubscriptionTaskName(scriptPath, "both"); got != "EnvName" {
		t.Fatalf("expected new Env name to win, got %q", got)
	}
}

// 两种声明都没有时仍回退到文件名（保持既有行为）。
func TestResolveSubscriptionTaskNameFallsBackToFilename(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "plain.py")
	if err := os.WriteFile(scriptPath, []byte("print('hi')\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	if got := resolveSubscriptionTaskName(scriptPath, "plain"); got != "plain" {
		t.Fatalf("expected fallback to filename, got %q", got)
	}
}

// mjs 必须在默认扫描后缀里，否则 .mjs 脚本连候选都进不去。
func TestDefaultSubscriptionAllowedExtensionsCoverMjs(t *testing.T) {
	// 配置为空时走代码硬兜底
	if exts := getSubscriptionAllowedExtensions(""); !exts[".mjs"] {
		t.Fatalf("hardcoded fallback extensions must contain .mjs, got %v", exts)
	}
	// 注册表默认值同样要含 mjs（这才是实际生效的那份）
	if exts := getSubscriptionAllowedExtensions(model.DefaultRepoFileExtensions); !exts[".mjs"] {
		t.Fatalf("registry default extensions must contain .mjs, got %v", exts)
	}
	// 旧默认值不含 mjs —— 这条断言是「存量实例迁移」判定的前提，改动默认值时不能把它改没了
	if exts := getSubscriptionAllowedExtensions(model.LegacyRepoFileExtensions); exts[".mjs"] {
		t.Fatalf("legacy default extensions must NOT contain .mjs, got %v", exts)
	}
}
