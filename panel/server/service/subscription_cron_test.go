package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCronForSubscriptionTaskSupportsDocstringCronFilenameHeader(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "bili_task_get_cookie.py")
	content := "'''\n1 9 11 11 1 bili_task_get_cookie.py\n手动运行，查看日志\n'''\nprint('hello')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "1 9 11 11 1" {
		t.Fatalf("expected cron from docstring header, got %q", got)
	}
}

func TestResolveCronForSubscriptionTaskIgnoresDocstringCronForOtherFile(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "actual_task.py")
	content := "'''\n1 9 11 11 1 other_task.py\n手动运行，查看日志\n'''\nprint('hello')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "0 0 * * *")
	if got != "0 0 * * *" {
		t.Fatalf("expected fallback cron for mismatched filename, got %q", got)
	}
}

func TestResolveSubscriptionTaskNamePrefersNewEnvTitle(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "main.py")
	content := "\"\"\"\nnew Env('华星电信999答题');\ncron: 1 1 1 1 1\n\"\"\"\nprint('hello')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveSubscriptionTaskName(scriptPath, "main")
	if got != "华星电信999答题" {
		t.Fatalf("expected task name from new Env title, got %q", got)
	}
}

func TestResolveSubscriptionTaskNameFallsBackToFilenameWhenNoNewEnvTitle(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "main.py")
	content := "print('hello')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveSubscriptionTaskName(scriptPath, "main")
	if got != "main" {
		t.Fatalf("expected fallback task name, got %q", got)
	}
}

// 覆盖 JS 块注释 `/* ... */` 中 `<cron> <filename>` 形式（jd_OnceApply.js 风格）。
func TestResolveCronForSubscriptionTaskSupportsBlockCommentCronFilenameHeader(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jd_OnceApply.js")
	content := "/*\n价格保护\n55 11 * * * jd_OnceApply.js\n */\nconst $ = new Env('一键价保');\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "55 11 * * *" {
		t.Fatalf("expected cron from block comment header, got %q", got)
	}
}

// 覆盖 Python docstring 中 `<cron> <filename>` 形式（jd_beans_7days.py 风格）。
func TestResolveCronForSubscriptionTaskSupportsPythonDocstringCronFilenameHeader(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jd_beans_7days.py")
	content := "# !/usr/bin/env python3\n# -*- coding: utf-8 -*-\n'''\nnew Env('豆子7天统计');\n8 8 29 2 * jd_beans_7days.py\n'''\nprint('hello')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "8 8 29 2 *" {
		t.Fatalf("expected cron from python docstring header, got %q", got)
	}
}

// 覆盖青龙单行声明 `cron "EXPR" filename, tag:xxx`（jd_CheckCK.js 风格）。
func TestResolveCronForSubscriptionTaskSupportsCronDirectiveLine(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jd_CheckCK.js")
	content := "/*\ncron \"6 6 6 6 *\" jd_CheckCK.js, tag:京东CK检测by-ccwav\n */\nconsole.log('hi');\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "6 6 6 6 *" {
		t.Fatalf("expected cron from cron directive line, got %q", got)
	}
}

// 青龙单行声明的 cron 与脚本文件名不一致时应忽略，避免误抓邻接脚本的声明。
func TestResolveCronForSubscriptionTaskCronDirectiveIgnoresOtherFile(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jd_OnceApply.js")
	content := "/*\ncron \"6 6 6 6 *\" jd_CheckCK.js, tag:京东CK检测\n */\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "0 0 * * *")
	if got != "0 0 * * *" {
		t.Fatalf("expected fallback cron when directive points to other file, got %q", got)
	}
}

// 真实场景：B 站 cookie 脚本，docstring 中含 cron 行 + 多行中文说明 + 含 = 的代码片段，
// 不应被中文说明 / 含 = 的代码行误识别为 cron。
func TestResolveCronForSubscriptionTaskBilibiliDocstringScenario(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "bili_task_get_cookie.py")
	content := `'''
1 9 11 11 1 bili_task_get_cookie.py
手动运行，查看日志，并使用手机B站app扫描日志中二维码，注意，只能修改第一个cookie
如果产生错误，重新运行并用手机扫描二维码
有可能识别不出来二维码，我测试了几次都能识别

默认环境变量存放位置为/ql/data/config/env.sh
可以自己通过docker命令进入容器查找这个文件位置。docker exec -it qinglong /bin/bash,进入青龙容器，然后查找一下这个文件位置
filename = '../config/env.sh'
'''
print('hello')
`
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "1 9 11 11 1" {
		t.Fatalf("expected cron from docstring header, got %q", got)
	}
}

// QLScriptPublic 真实样例：JSDoc 块注释每行 `*` 前缀 + 紧跟同名文件，
// 例：`* cron 11 8 * * *  sysxc.js`（backup/sysxc.js）。
func TestResolveCronForSubscriptionTaskSupportsJSDocStarCronWithFilename(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "sysxc.js")
	content := "/**\n * 书亦烧仙草\n * cron 11 8 * * *  sysxc.js\n * 23/04/15 内部使用\n */\nconst $ = new Env(\"书亦烧仙草\");\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "11 8 * * *" {
		t.Fatalf("expected cron from JSDoc star header, got %q", got)
	}
}

// QLScriptPublic 真实样例：JSDoc `*` 前缀但无尾随文件名，
// 例：`* cron 8 10 * * *`（daily/ydyp.js）。
func TestResolveCronForSubscriptionTaskSupportsJSDocStarCronWithoutFilename(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "ydyp.js")
	content := "/**\n * new Env(\"中国移动云盘\")\n * 变量名ydyp_ck\n * cron 8 10 * * *\n */\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "8 10 * * *" {
		t.Fatalf("expected cron from bare JSDoc star, got %q", got)
	}
}

// QLScriptPublic 真实样例：`#cron <expr>`（井号且无冒号），
// 例：`#cron 8 9,10,11 * * *`（daily/BREO.py）。
func TestResolveCronForSubscriptionTaskSupportsHashCronWithoutColon(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "BREO.py")
	content := "#by:哆啦A梦\n#cron 8 9,10,11 * * *\nprint('hi')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "8 9,10,11 * * *" {
		t.Fatalf("expected cron from #cron header without colon, got %q", got)
	}
}

func TestResolveCronForSubscriptionTaskSupportsSlashSlashCron(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "daily.js")
	content := "//cron: 15 12 * * *\nconst $ = new Env('daily');\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "15 12 * * *" {
		t.Fatalf("expected cron from //cron header, got %q", got)
	}
}

// QLScriptPublic 真实样例：Python docstring 中 `cron <expr>`（无注释符号、无冒号），
// 例：`cron 0 12 * * *`（daily/sfsy.py）。
func TestResolveCronForSubscriptionTaskSupportsBareCronWithoutColon(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "sfsy.py")
	content := "\"\"\"\n顺丰速运日常积分任务\ncron 0 12 * * *\n\"\"\"\nprint('hi')\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "0 12 * * *" {
		t.Fatalf("expected cron from bare `cron` header, got %q", got)
	}
}

// QLScriptPublic 真实样例：`@cron:` JSDoc 风格标签，
// 例：`@cron: 30 8 * * *`（daily/yht.js）。
func TestResolveCronForSubscriptionTaskSupportsAtCronTag(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "yht.js")
	content := "/*\n@Description:  益禾堂\n@cron: 30 8 * * *\n*/\nconst $ = new Env(\"益禾堂\");\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "30 8 * * *" {
		t.Fatalf("expected cron from @cron tag, got %q", got)
	}
}

// QLScriptPublic 真实样例：JSDoc 行尾跟随不匹配的文件名（脚本作者笔误），
// 例：jlld.js 内写着 `* cron 27 17 * * *  leidacar.js`，仍应识别 cron。
func TestResolveCronForSubscriptionTaskJSDocCronAcceptsMismatchedFilenameHint(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jlld.js")
	content := "/**\n * new Env('jlld')\n * cron 27 17 * * *  leidacar.js\n */\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "27 17 * * *" {
		t.Fatalf("expected cron from JSDoc header even when trailing filename mismatches, got %q", got)
	}
}

// 防御性回归：纯中文叙述中包含 "cron" 单词时不应被误判为 cron 表达式，
// 例：`2. cron 以防ocr识别出错每天运行两次左右`（backup/sysxc.py）。
func TestResolveCronForSubscriptionTaskIgnoresChineseProseMentioningCron(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "sysxc.py")
	content := "\"\"\"\n2. cron 以防ocr识别出错每天运行两次左右\n3. ddddocr搭建方法...\n\"\"\"\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "0 0 * * *")
	if got != "0 0 * * *" {
		t.Fatalf("expected fallback cron when only Chinese prose mentions cron, got %q", got)
	}
}

// jdpro 真实样例：`cron:` 后无空格紧贴数字，例：`cron:39 7 * * *`（jd_daka_bean.js）。
func TestResolveCronForSubscriptionTaskSupportsColonWithoutSpace(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "jd_daka_bean.js")
	content := "/*\n京豆打卡\ncron:39 7 * * *\n*/\nconst $ = new Env(\"京豆打卡\");\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "")
	if got != "39 7 * * *" {
		t.Fatalf("expected cron from colon-without-space header, got %q", got)
	}
}

// 防御性回归：不应被 `crontab` / `cron-utils` 等关键词误匹配。
func TestResolveCronForSubscriptionTaskIgnoresCronKeywordWithoutBoundary(t *testing.T) {
	root := t.TempDir()
	scriptPath := filepath.Join(root, "main.js")
	content := "// crontab is a tool, see https://crontab.guru\n// cron-utils 0 0 * * *\nconsole.log('hi');\n"
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	got := resolveCronForSubscriptionTask(scriptPath, "0 0 * * *")
	if got != "0 0 * * *" {
		t.Fatalf("expected fallback cron for non-cron keywords, got %q", got)
	}
}
