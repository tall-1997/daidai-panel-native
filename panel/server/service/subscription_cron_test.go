package service

import (
	"os"
	"path/filepath"
	"strings"
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

// 标签行的表达式后面跟着说明文字、说明里带着 Node.js / sendNotify.js 这类像文件名的词时，照常识别 cron：
// 行尾的说明不参与判断（曾有过「行尾是别的脚本的文件名就不采用」的规则，会把这类说明连同照抄了别的脚本头部的脚本一起判成无效，已撤回）。
func TestResolveCronForSubscriptionTaskLabelAcceptsDescriptionMentioningFiles(t *testing.T) {
	cases := []struct {
		name, file, content, want string
	}{
		{"node_version", "daily.js", "// cron: 0 8 * * * 需要 Node.js 18 以上\nconst $ = new Env('daily');\n", "0 8 * * *"},
		{"send_notify_dependency", "sign.py", "# cron: 5 8 * * * 依赖 sendNotify.js\nprint('sign')\n", "5 8 * * *"},
		{"jsdoc_star", "checkin.js", "/**\n * cron 10 9 * * *  需要 Node.js 18 以上，依赖 sendNotify.js\n */\n", "10 9 * * *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSubscriptionCronScript(t, tc.file, tc.content)
			if got := resolveCronForSubscriptionTask(path, ""); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
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

// writeSubscriptionCronScript 把 content 写进临时目录下的 name，返回路径。
func writeSubscriptionCronScript(t *testing.T, name, content string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(scriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

// #134：Windows 编辑器保存的脚本常带 UTF-8 BOM。正则里的 \s 不匹配 U+FEFF，第 1 行的声明以前认不出，
// 脚本明明写了 cron 却被当成没写。三种声明写法（标签、文件名行、青龙指令行）都要认，CRLF 也一样。
func TestResolveCronForSubscriptionTaskStripsBOMOnFirstLine(t *testing.T) {
	cases := []struct {
		name, file, content, want string
	}{
		{"label", "bom_label.js", "\uFEFF// cron: 20 7 * * *\nconst $ = new Env('x');\n", "20 7 * * *"},
		{"label_crlf", "bom_crlf.py", "\uFEFF# cron: 5 6 * * *\r\nprint('x')\r\n", "5 6 * * *"},
		{"filename_line", "bom_fileline.js", "\uFEFF0 8 * * * bom_fileline.js\nconsole.log('x')\n", "0 8 * * *"},
		{"directive", "bom_directive.js", "\uFEFFcron \"6 6 6 6 *\" bom_directive.js, tag:x\n", "6 6 6 6 *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSubscriptionCronScript(t, tc.file, tc.content)
			if got := resolveCronForSubscriptionTask(path, ""); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}

	// BOM 只可能出现在文件开头：第 2 行行首的 U+FEFF 不是 BOM，不剥，保持认不出。
	path := writeSubscriptionCronScript(t, "mid_bom.js", "console.log('x')\n\uFEFF// cron: 20 7 * * *\n")
	if got := resolveCronForSubscriptionTask(path, ""); got != "" {
		t.Fatalf("U+FEFF in the middle of a file is not a BOM, got %q", got)
	}
}

// #134：`cron: "0 8 * * *"` / `cron: '0 8 * * *'` 值带引号时，青龙（grep + xargs）能认，面板以前认不出。
// 只剥「成对包住整个值」的引号，只在注释行或顶格行上剥（Python docstring 里常见顶格写法）。
func TestResolveCronForSubscriptionTaskAcceptsQuotedLabelValue(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"slash_double", "// cron: \"0 8 * * *\"\nconst $ = new Env('x');\n", "0 8 * * *"},
		{"hash_single", "# cron: '5 8 * * *'\nprint('x')\n", "5 8 * * *"},
		{"jsdoc_star", "/**\n * cron: \"1 2 * * *\"\n */\n", "1 2 * * *"},
		{"at_tag", "/*\n@cron: '30 8 * * *'\n*/\n", "30 8 * * *"},
		{"no_colon", "// cron \"10 8 * * *\"\n", "10 8 * * *"},
		{"docstring_top_level", "\"\"\"\ncron: \"10 9 * * *\"\nnew Env('x')\n\"\"\"\n", "10 9 * * *"},
		{"inner_spaces", "// cron: \" 0 8 * * * \"\n", "0 8 * * *"},
		{"six_fields", "// cron: '0 0 8 * * ?'\n", "0 0 8 * * ?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSubscriptionCronScript(t, "quoted.js", tc.content)
			if got := resolveCronForSubscriptionTask(path, ""); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

// 反例：剥引号不能把代码里的对象字面量认成 cron 声明。缩进的裸 `cron: '...'` 是 JS / TS 对象属性的典型形态，
// 不管末尾有没有逗号都不认；引号不成对的也不认。
func TestResolveCronForSubscriptionTaskIgnoresQuotedValueInCode(t *testing.T) {
	cases := []struct {
		name, content string
	}{
		{"object_literal_trailing_comma", "const job = {\n  name: 'x',\n  cron: '0 0 * * *',\n};\n"},
		{"object_literal_last_property", "const job = {\n  name: 'x',\n  cron: '0 0 * * *'\n};\n"},
		{"object_literal_tab_double", "const job = {\n\tcron: \"0 0 * * *\"\n};\n"},
		{"const_assignment", "const cron = '0 8 * * *'\n"},
		{"mismatched_quotes", "// cron: \"0 8 * * *'\n"},
		{"empty_quotes", "// cron: \"\"\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSubscriptionCronScript(t, "code.js", tc.content)
			if got := resolveCronForSubscriptionTask(path, ""); got != "" {
				t.Fatalf("must not be recognised as a cron declaration, got %q", got)
			}
		})
	}
}

// #134：cron 声明的扫描行数与任务名对齐到 120 行（以前只看 50 行，名字认得出、cron 却认不出）。第 121 行起不再看。
func TestResolveCronForSubscriptionTaskScansFirst120Lines(t *testing.T) {
	scriptWithCronAt := func(lineNo int) string {
		var b strings.Builder
		for i := 1; i < lineNo; i++ {
			b.WriteString("// 说明文字\n")
		}
		b.WriteString("// cron: 7 9 * * *\n")
		return b.String()
	}
	for _, tc := range []struct {
		line int
		want string
	}{{51, "7 9 * * *"}, {120, "7 9 * * *"}, {121, ""}} {
		path := writeSubscriptionCronScript(t, "long_header.js", scriptWithCronAt(tc.line))
		if got := resolveCronForSubscriptionTask(path, ""); got != tc.want {
			t.Errorf("cron at line %d: want %q, got %q", tc.line, tc.want, got)
		}
	}
	// 任务名与 cron 同一个上限：第 120 行的 name 注释头认得出，第 121 行的不认。
	for _, tc := range []struct {
		line int
		want string
	}{{120, "第120行的名字"}, {121, "fallback"}} {
		var b strings.Builder
		for i := 1; i < tc.line; i++ {
			b.WriteString("// 说明文字\n")
		}
		b.WriteString("// name: 第120行的名字\n")
		path := writeSubscriptionCronScript(t, "long_name.js", b.String())
		if got := resolveSubscriptionTaskName(path, "fallback"); got != tc.want {
			t.Errorf("name at line %d: want %q, got %q", tc.line, tc.want, got)
		}
	}
}

// #134：前面有一行超过 64KB 的代码（压缩 / 混淆过的单行代码很常见）时，bufio.Scanner 报 ErrTooLong 后静默停止，
// 后面的 cron 声明读不到。现在超长行只截取前一段参与匹配，照常往下读；行数照常计算。
func TestResolveCronForSubscriptionTaskSurvivesVeryLongLine(t *testing.T) {
	longLine := "var x = '" + strings.Repeat("a", 200*1024) + "';"
	path := writeSubscriptionCronScript(t, "minified.js", "// 混淆过的脚本\n"+longLine+"\n// cron: 3 4 * * *\nconst $ = new Env('长行脚本');\n")
	if got := resolveCronForSubscriptionTask(path, ""); got != "3 4 * * *" {
		t.Fatalf("cron after a very long line: want %q, got %q", "3 4 * * *", got)
	}
	if got := resolveSubscriptionTaskName(path, "minified"); got != "长行脚本" {
		t.Fatalf("task name after a very long line: want %q, got %q", "长行脚本", got)
	}

	// 声明本身就在超长行的开头：截断不影响匹配。
	path = writeSubscriptionCronScript(t, "head_long.js", "// cron: 8 9 * * * "+strings.Repeat("x", 100*1024)+"\n")
	if got := resolveCronForSubscriptionTask(path, ""); got != "8 9 * * *" {
		t.Fatalf("cron at the head of a very long line: want %q, got %q", "8 9 * * *", got)
	}

	// 超长行照样算一行：第 120 行之后的声明仍然不看。
	var b strings.Builder
	b.WriteString(longLine + "\n")
	for i := 2; i <= 120; i++ {
		b.WriteString("// 说明文字\n")
	}
	b.WriteString("// cron: 3 4 * * *\n")
	path = writeSubscriptionCronScript(t, "long_then_121.js", b.String())
	if got := resolveCronForSubscriptionTask(path, ""); got != "" {
		t.Fatalf("a very long line still counts as one line, cron at line 121 must be ignored, got %q", got)
	}
	// 反过来也要成立：超长行只算一行，第 120 行的声明照样认得出（多算行数的话会被挤出 120 行）。
	b.Reset()
	b.WriteString(longLine + "\r\n")
	for i := 2; i < 120; i++ {
		b.WriteString("// 说明文字\r\n")
	}
	b.WriteString("// cron: 3 4 * * *\r\n")
	path = writeSubscriptionCronScript(t, "long_then_120.js", b.String())
	if got := resolveCronForSubscriptionTask(path, ""); got != "3 4 * * *" {
		t.Fatalf("a very long line counts as exactly one line, cron at line 120 must be found, got %q", got)
	}

	// 没有换行结尾的最后一行照常读。
	path = writeSubscriptionCronScript(t, "no_trailing_newline.py", "print('x')\r\n# cron: 11 12 * * *")
	if got := resolveCronForSubscriptionTask(path, ""); got != "11 12 * * *" {
		t.Fatalf("last line without newline: want %q, got %q", "11 12 * * *", got)
	}

	// 比读缓冲长、但没到截断上限的行（几 KB 到几十 KB）：整行参与匹配，CRLF 照常去掉，行数照常计。
	mediumLine := "var y = '" + strings.Repeat("b", 20*1024) + "';"
	path = writeSubscriptionCronScript(t, "medium.py", mediumLine+"\r\n# name: 中等长行\r\n# cron: 13 14 * * *\r\n")
	if got := resolveCronForSubscriptionTask(path, ""); got != "13 14 * * *" {
		t.Fatalf("cron after a medium long line: want %q, got %q", "13 14 * * *", got)
	}
	if got := resolveSubscriptionTaskName(path, "medium"); got != "中等长行" {
		t.Fatalf("task name after a medium long line: want %q, got %q", "中等长行", got)
	}
	// 声明本身在中等长行里、值后面跟一长串说明：取前 5 段。
	path = writeSubscriptionCronScript(t, "medium_head.js", "// cron: 15 16 * * * "+strings.Repeat("说明", 4*1024)+"\r\n")
	if got := resolveCronForSubscriptionTask(path, ""); got != "15 16 * * *" {
		t.Fatalf("cron at the head of a medium long line: want %q, got %q", "15 16 * * *", got)
	}
	// 不锚行首的写法（混淆过的单行代码里的 new Env、青龙指令行）落在中等长行的中段（远超读缓冲的几 KB 处）也要认得出：
	// 改动前 Scanner 对 64KB 以内的行是整行匹配的，不能因为换了读法退化成只看行首一小段。
	padding := strings.Repeat("c", 12*1024)
	path = writeSubscriptionCronScript(t, "medium_env.js",
		"var z='"+padding+"';const $ = new Env('深处的名字');/* cron \"17 18 * * *\" medium_env.js */var w='"+padding+"';\n")
	if got := resolveSubscriptionTaskName(path, "medium_env"); got != "深处的名字" {
		t.Fatalf("new Env in the middle of a medium long line: want %q, got %q", "深处的名字", got)
	}
	if got := resolveCronForSubscriptionTask(path, ""); got != "17 18 * * *" {
		t.Fatalf("cron directive in the middle of a medium long line: want %q, got %q", "17 18 * * *", got)
	}
}
