package mcptools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// ---- read_script 分段 ----------------------------------------------------------

func scriptContentResponder(content string) func(recordedCall) (int, any, error) {
	return okResponder(map[string]any{"data": map[string]any{"path": "big.js", "content": content, "binary": false}})
}

// readAllSegments 按 next_offset 一段段读到 truncated=false，返回拼接结果与各段输出。
func readAllSegments(t *testing.T, content string, limit int) (string, []map[string]any) {
	t.Helper()
	fake := &fakeDispatcher{respond: scriptContentResponder(content)}
	session := connectTestClient(t, fake, false)

	var joined strings.Builder
	var segments []map[string]any
	offset := 0
	for round := 0; ; round++ {
		if round > len(content)+1 {
			t.Fatal("分段读取没有前进，陷入死循环")
		}
		args := map[string]any{"path": "big.js", "offset": offset}
		if limit > 0 {
			args["limit"] = limit
		}
		result, text := callTool(t, session, "read_script", args)
		if result.IsError {
			t.Fatalf("read_script 不应失败: %s", text)
		}
		if len(text) > MaxOutputBytes {
			t.Fatalf("单段输出 %d 字节，超过上限 %d（会被截成不合法的 JSON）", len(text), MaxOutputBytes)
		}
		out := decodeObject(t, text)
		segments = append(segments, out)
		chunk, _ := out["content"].(string)
		if !utf8.ValidString(chunk) {
			t.Fatalf("第 %d 段切在了字符中间", round+1)
		}
		joined.WriteString(chunk)
		if out["total_bytes"] != float64(len(content)) {
			t.Fatalf("total_bytes 应为 %d，实际 %v", len(content), out["total_bytes"])
		}
		if out["truncated"] != true {
			if out["next_offset"] != float64(len(content)) {
				t.Fatalf("最后一段的 next_offset 应等于总字节数，实际 %v", out["next_offset"])
			}
			return joined.String(), segments
		}
		if _, ok := out["hint"].(string); !ok {
			t.Fatalf("未读完的段应当带续读提示: %v", out)
		}
		next, _ := out["next_offset"].(float64)
		offset = int(next)
	}
}

func TestReadScriptPagesReassembleToOriginal(t *testing.T) {
	// 与 issue #139 的例子同一量级（约 14 万字节），混入多字节字符、4 字节 emoji、引号与换行。
	var builder strings.Builder
	for i := 0; builder.Len() < 141980; i++ {
		fmt.Fprintf(&builder, "const s%d = \"京东签到😀\\n\"; // 第 %d 行 \t\n", i, i)
	}
	content := builder.String()

	joined, segments := readAllSegments(t, content, 0)
	if joined != content {
		t.Fatal("各段按顺序拼起来必须与原文逐字节相同")
	}
	if len(segments) < 3 {
		t.Fatalf("14 万字节的文件默认上限下应当分成至少 3 段，实际 %d 段", len(segments))
	}
	if segments[0]["offset"] != float64(0) || segments[0]["truncated"] != true {
		t.Fatalf("第一段应当从 0 开始并明确标记未读完: %v", segments[0]["truncated"])
	}
	if _, legacy := segments[0]["content_truncated"]; legacy {
		t.Fatal("不应再只靠附加字段提示截断")
	}

	// limit 比一个字符还小时也要前进（至少给一个完整字符），不能原地打转。
	small := "中文😀abc"
	joined, segments = readAllSegments(t, small, 1)
	if joined != small || len(segments) != 6 {
		t.Fatalf("limit=1 时应逐字符前进并拼回原文，实际 %d 段: %q", len(segments), joined)
	}
}

// 引号、反斜杠、控制字符在 JSON 里会变长。只按原始字节切，一段 48KB 的引号会撑破 64KB 的输出上限。
func TestReadScriptKeepsEscapeHeavyChunksWithinOutputCap(t *testing.T) {
	content := strings.Repeat(`"\`, 30*1024) + strings.Repeat("\x01", 20*1024)
	joined, segments := readAllSegments(t, content, 0)
	if joined != content {
		t.Fatal("转义很多的内容也必须能拼回原文")
	}
	if len(segments) < 2 {
		t.Fatalf("转义后超过预算的内容应当被分段，实际 %d 段", len(segments))
	}
}

func TestReadScriptOffsetHandling(t *testing.T) {
	fake := &fakeDispatcher{respond: scriptContentResponder("中文abc")}
	session := connectTestClient(t, fake, false)

	// offset 落在「中」的第 2 个字节：退回字符开头，不丢字符。
	result, text := callTool(t, session, "read_script", map[string]any{"path": "big.js", "offset": 1})
	out := decodeObject(t, text)
	if result.IsError || out["offset"] != float64(0) || out["content"] != "中文abc" || out["truncated"] != false {
		t.Fatalf("落在字符中间的 offset 应当退回字符开头，实际 %s", text)
	}

	result, text = callTool(t, session, "read_script", map[string]any{"path": "big.js", "offset": 9})
	if out := decodeObject(t, text); result.IsError || out["content"] != "" || out["truncated"] != false {
		t.Fatalf("offset 恰好等于总长时应返回空段并标记读完，实际 %s", text)
	}

	for _, args := range []map[string]any{
		{"path": "big.js", "offset": 10},
		{"path": "big.js", "offset": -1},
		{"path": "big.js", "limit": -5},
		{"path": " "},
	} {
		if result, text := callTool(t, session, "read_script", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
}

// ---- 目录树与版本 ----------------------------------------------------------------

func scriptTreeFixture() map[string]any {
	return map[string]any{"data": []any{
		map[string]any{"key": "a", "title": "a", "type": "directory", "isLeaf": false, "children": []any{
			map[string]any{"key": "a/b", "title": "b", "type": "directory", "isLeaf": false, "children": []any{
				map[string]any{"key": "a/b/c.py", "title": "c.py", "type": "file", "isLeaf": true, "size": 10, "mtime": 1700000000, "extension": ".py"},
			}},
			map[string]any{"key": "a/x.js", "title": "x.js", "type": "file", "isLeaf": true, "size": 20, "mtime": 1700000001, "extension": ".js"},
		}},
		map[string]any{"key": "root.py", "title": "root.py", "type": "file", "isLeaf": true, "size": 30, "mtime": 1700000002, "extension": ".py"},
	}}
}

func TestGetScriptTreeSlimsNodesAndLimitsDepth(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(scriptTreeFixture())}
	session := connectTestClient(t, fake, false)

	result, text := callTool(t, session, "get_script_tree", map[string]any{"max_depth": 1})
	if result.IsError {
		t.Fatalf("get_script_tree 不应失败: %s", text)
	}
	if call := fake.recorded()[0]; call.method != http.MethodGet || call.path != "/scripts/tree" {
		t.Fatalf("应当调用 GET /scripts/tree，实际 %s %s", call.method, call.path)
	}
	out := decodeObject(t, text)
	tree, _ := out["tree"].([]any)
	if len(tree) != 2 {
		t.Fatalf("顶层应当有 2 个节点，实际 %v", tree)
	}
	dir := tree[0].(map[string]any)
	if dir["type"] != "directory" || dir["child_count"] != float64(2) || dir["children"] != nil || dir["path"] != "a" {
		t.Fatalf("超过 max_depth 的目录只给 child_count，实际 %v", dir)
	}
	file := tree[1].(map[string]any)
	if file["type"] != "file" || file["size"] != float64(30) || file["isLeaf"] != nil {
		t.Fatalf("文件节点应当换成精简字段，实际 %v", file)
	}
	// 规模统计覆盖整棵树（含没展开的部分）。
	if out["directories"] != float64(2) || out["files"] != float64(3) || out["note"] == nil {
		t.Fatalf("统计应当覆盖整棵树并注明有目录未展开，实际 %v", out)
	}

	result, text = callTool(t, session, "get_script_tree", map[string]any{"path": "/a/"})
	out = decodeObject(t, text)
	sub, _ := out["tree"].([]any)
	if result.IsError || out["path"] != "a" || len(sub) != 2 || sub[0].(map[string]any)["path"] != "a/b" {
		t.Fatalf("path 应当只返回该子目录下的树，实际 %s", text)
	}
	inner, _ := sub[0].(map[string]any)["children"].([]any)
	if len(inner) != 1 || inner[0].(map[string]any)["name"] != "c.py" {
		t.Fatalf("不限层数时应当展开到底，实际 %v", sub[0])
	}

	for _, args := range []map[string]any{{"path": "a/x.js"}, {"path": "nope"}, {"max_depth": -1}} {
		if result, text := callTool(t, session, "get_script_tree", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
}

func TestListScriptVersionsForwardsPath(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"data": []any{
		map[string]any{"id": 12, "script_path": "demo/a.py", "version": 3, "message": "v3", "content_length": 99, "created_at": "2026-09-18T10:00:00Z"},
	}})}
	session := connectTestClient(t, fake, false)

	result, text := callTool(t, session, "list_script_versions", map[string]any{"path": " demo/a.py "})
	if result.IsError {
		t.Fatalf("list_script_versions 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	if call.method != http.MethodGet || call.path != "/scripts/versions" || call.query.Get("path") != "demo/a.py" {
		t.Fatalf("应当以 path 查询 GET /scripts/versions，实际 %s %s %v", call.method, call.path, call.query)
	}
	out := decodeObject(t, text)
	versions, _ := out["versions"].([]any)
	if out["total"] != float64(1) || len(versions) != 1 {
		t.Fatalf("应当返回 1 个版本，实际 %s", text)
	}
	if first := versions[0].(map[string]any); first["id"] != float64(12) || first["version"] != float64(3) || first["script_path"] != nil {
		t.Fatalf("版本条目应当是精简字段，实际 %v", first)
	}
}

// ---- 删改移复制 ------------------------------------------------------------------

func TestDeleteScriptMapsTypeAndSurfacesIsolatedDirGuard(t *testing.T) {
	fake := &fakeDispatcher{respond: func(call recordedCall) (int, any, error) {
		if strings.Contains(call.query.Get("path"), "__pycache__") {
			return http.StatusBadRequest, map[string]any{"error": "该路径不可访问"}, nil
		}
		return http.StatusOK, map[string]any{"message": "删除成功"}, nil
	}}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "delete_script", map[string]any{"path": "demo/old.py"})
	if result.IsError {
		t.Fatalf("delete_script 不应失败: %s", text)
	}
	call := fake.recorded()[0]
	if call.method != http.MethodDelete || call.path != "/scripts" || call.query.Get("path") != "demo/old.py" || call.query.Get("type") != "file" {
		t.Fatalf("应当以 DELETE /scripts?path=&type=file 转发，实际 %s %s %v", call.method, call.path, call.query)
	}

	if result, text = callTool(t, session, "delete_script", map[string]any{"path": "demo", "type": "DIRECTORY"}); result.IsError {
		t.Fatalf("删除目录不应失败: %s", text)
	}
	if got := fake.recorded()[1].query.Get("type"); got != "directory" {
		t.Fatalf("type 应当归一成 directory，实际 %q", got)
	}

	result, text = callTool(t, session, "delete_script", map[string]any{"path": "demo/__pycache__", "type": "directory"})
	if !result.IsError || !strings.Contains(text, "该路径不可访问") {
		t.Fatalf("隔离目录应当把面板的拒绝原样带回，实际 isError=%v: %s", result.IsError, text)
	}

	calls := len(fake.recorded())
	if result, _ := callTool(t, session, "delete_script", map[string]any{"path": "demo", "type": "folder"}); !result.IsError {
		t.Fatal("未知 type 应当报错")
	}
	if len(fake.recorded()) != calls {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestRenameMoveCopyScriptMapBodies(t *testing.T) {
	fake := &fakeDispatcher{respond: func(call recordedCall) (int, any, error) {
		switch {
		case call.method == http.MethodPut && call.path == "/scripts/rename":
			return http.StatusOK, map[string]any{"message": "重命名成功", "new_path": "demo/new.py"}, nil
		case call.method == http.MethodPut && call.path == "/scripts/move":
			return http.StatusOK, map[string]any{"message": "移动成功", "new_path": "lib/new.py"}, nil
		case call.method == http.MethodPost && call.path == "/scripts/copy":
			return http.StatusCreated, map[string]any{"message": "复制成功", "new_path": "backup/copy.py"}, nil
		}
		return http.StatusTeapot, map[string]any{"error": "unexpected " + call.method + " " + call.path}, nil
	}}
	session := connectTestClient(t, fake, true)

	steps := []struct {
		tool     string
		args     map[string]any
		body     map[string]any
		wantPath string
	}{
		{"rename_script", map[string]any{"path": "demo/old.py", "new_name": " new.py "},
			map[string]any{"old_path": "demo/old.py", "new_name": "new.py"}, "demo/new.py"},
		{"move_script", map[string]any{"path": "demo/new.py", "target_dir": "lib"},
			map[string]any{"source_path": "demo/new.py", "target_dir": "lib"}, "lib/new.py"},
		{"move_script", map[string]any{"path": "lib/new.py"},
			map[string]any{"source_path": "lib/new.py", "target_dir": ""}, "lib/new.py"},
		{"copy_script", map[string]any{"path": "lib/new.py", "target_dir": "backup", "new_name": "copy.py"},
			map[string]any{"source_path": "lib/new.py", "target_dir": "backup", "new_name": "copy.py"}, "backup/copy.py"},
		{"copy_script", map[string]any{"path": "lib/new.py", "target_dir": "backup"},
			map[string]any{"source_path": "lib/new.py", "target_dir": "backup"}, "backup/copy.py"},
	}
	for i, step := range steps {
		result, text := callTool(t, session, step.tool, step.args)
		if result.IsError {
			t.Fatalf("%s 不应失败: %s", step.tool, text)
		}
		if body := fake.recorded()[i].body; !reflect.DeepEqual(body, step.body) {
			t.Errorf("%s 的请求体不对，实际 %#v，期望 %#v", step.tool, body, step.body)
		}
		if out := decodeObject(t, text); out["new_path"] != step.wantPath {
			t.Errorf("%s 应当返回 new_path=%s，实际 %v", step.tool, step.wantPath, out)
		}
	}

	for _, check := range []struct {
		tool string
		args map[string]any
	}{
		{"rename_script", map[string]any{"path": "a.py", "new_name": " "}},
		{"move_script", map[string]any{"path": " "}},
		{"copy_script", map[string]any{"path": ""}},
	} {
		if result, text := callTool(t, session, check.tool, check.args); !result.IsError {
			t.Errorf("%s 参数 %v 应当报错，实际 %s", check.tool, check.args, text)
		}
	}
	if len(fake.recorded()) != len(steps) {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestBatchDeleteScriptsValidatesAndReportsFailures(t *testing.T) {
	allFailed := false
	fake := &fakeDispatcher{respond: func(recordedCall) (int, any, error) {
		if allFailed {
			return http.StatusOK, map[string]any{"message": "删除完成: 成功 0, 失败 1", "success_count": 0, "failed_count": 1, "failed_items": []any{"x.py"}}, nil
		}
		return http.StatusOK, map[string]any{"message": "删除完成: 成功 1, 失败 1", "success_count": 1, "failed_count": 1, "failed_items": []any{"gone.py"}}, nil
	}}
	session := connectTestClient(t, fake, true)

	tooMany := make([]map[string]any, maxBatchDeleteScripts+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{"path": fmt.Sprintf("f%d.py", i)}
	}
	for _, args := range []map[string]any{
		{"paths": []map[string]any{}},
		{"paths": tooMany},
		{"paths": []map[string]any{{"path": " "}}},
		{"paths": []map[string]any{{"path": "a.py", "type": "link"}}},
	} {
		if result, _ := callTool(t, session, "batch_delete_scripts", args); !result.IsError {
			t.Errorf("非法参数应当报错: %v", args["paths"])
		}
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("参数校验失败时不应调用面板接口")
	}

	result, text := callTool(t, session, "batch_delete_scripts", map[string]any{"paths": []map[string]any{
		{"path": "a.py"}, {"path": "old", "type": "directory"},
	}})
	if result.IsError {
		t.Fatalf("部分成功不应报错: %s", text)
	}
	call := fake.recorded()[0]
	wantBody := map[string]any{"paths": []map[string]any{{"path": "a.py", "type": "file"}, {"path": "old", "type": "directory"}}}
	if call.method != http.MethodDelete || call.path != "/scripts/batch" || !reflect.DeepEqual(call.body, wantBody) {
		t.Fatalf("应当以 DELETE /scripts/batch 转发（type 补全为 file），实际 %s %s %#v", call.method, call.path, call.body)
	}
	if out := decodeObject(t, text); out["success_count"] != float64(1) || out["failed_count"] != float64(1) {
		t.Fatalf("应当返回成功与失败的数量，实际 %s", text)
	}

	allFailed = true
	if result, text := callTool(t, session, "batch_delete_scripts", map[string]any{"paths": []map[string]any{{"path": "x.py"}}}); !result.IsError || !strings.Contains(text, "全部删除失败") {
		t.Fatalf("一项都没删成时（接口仍回 200）应当翻译成工具错误，实际 %s", text)
	}
}

func TestRunCodeNormalizesLanguageAndPollsOutput(t *testing.T) {
	useFastScriptPolling(t)
	fake := &fakeDispatcher{respond: func(call recordedCall) (int, any, error) {
		switch {
		case call.method == http.MethodPost && call.path == "/scripts/run-code":
			return http.StatusCreated, map[string]any{"message": "代码已启动", "run_id": "code_1"}, nil
		case call.method == http.MethodGet && call.path == "/scripts/run/code_1/logs":
			return http.StatusOK, map[string]any{"data": map[string]any{"logs": []any{"a.py", "b.py"}, "done": true, "exit_code": 0, "status": "success"}}, nil
		case call.method == http.MethodDelete && call.path == "/scripts/run/code_1":
			return http.StatusOK, map[string]any{"message": "已清除"}, nil
		}
		return http.StatusTeapot, map[string]any{"error": "unexpected " + call.method + " " + call.path}, nil
	}}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "run_code", map[string]any{"code": "ls $DAIDAI_SCRIPTS_DIR", "language": " Bash "})
	if result.IsError {
		t.Fatalf("run_code 不应失败: %s", text)
	}
	first := fake.recorded()[0]
	if !reflect.DeepEqual(first.body, map[string]any{"code": "ls $DAIDAI_SCRIPTS_DIR", "language": "bash"}) {
		t.Fatalf("语言应当归一成小写后转发，代码原样发送，实际 %#v", first.body)
	}
	out := decodeObject(t, text)
	if out["done"] != true || out["output"] != "a.py\nb.py" || out["run_id"] != "code_1" || out["exit_code"] != float64(0) {
		t.Fatalf("运行结束后的输出不对: %v", out)
	}
	calls := fake.recorded()
	if last := calls[len(calls)-1]; last.method != http.MethodDelete {
		t.Fatalf("取完结果应当清掉运行记录，最后一次调用是 %s %s", last.method, last.path)
	}

	before := len(fake.recorded())
	for _, args := range []map[string]any{{"code": " ", "language": "shell"}, {"code": "ls", "language": " "}} {
		if result, text := callTool(t, session, "run_code", args); !result.IsError {
			t.Errorf("参数 %v 应当报错，实际 %s", args, text)
		}
	}
	if len(fake.recorded()) != before {
		t.Fatal("参数校验失败时不应调用面板接口")
	}
}

func TestRollbackScriptUsesVersionID(t *testing.T) {
	fake := &fakeDispatcher{respond: okResponder(map[string]any{"message": "已回滚到 v3", "version": 5})}
	session := connectTestClient(t, fake, true)

	result, text := callTool(t, session, "rollback_script", map[string]any{"id": 12})
	if result.IsError {
		t.Fatalf("rollback_script 不应失败: %s", text)
	}
	if call := fake.recorded()[0]; call.method != http.MethodPut || call.path != "/scripts/versions/12/rollback" {
		t.Fatalf("应当调用 PUT /scripts/versions/12/rollback，实际 %s %s", call.method, call.path)
	}
	if out := decodeObject(t, text); out["version_id"] != float64(12) || out["new_version"] != float64(5) {
		t.Fatalf("应当返回回滚后新记下的版本号，实际 %s", text)
	}
	if result, _ := callTool(t, session, "rollback_script", map[string]any{"id": 0}); !result.IsError {
		t.Fatal("id 为 0 应当报错")
	}
}

func TestJSONEscapedLenNeverUnderestimates(t *testing.T) {
	samples := []string{"a", "中", "😀", "\"", "\\", "\n", "\t", "\x01", "\x7f", " ", "<", "&", string([]byte{0xff})}
	for _, sample := range samples {
		r, size := utf8.DecodeRuneInString(sample)
		encoded, _ := json.Marshal(sample)
		// json.Marshal 会转义 < > &，renderOutput 不会；这里只要求估算值不小于关闭 HTML 转义后的真实长度。
		actual := len(renderOutput(sample)) - 2
		if estimate := jsonEscapedLen(r, size); estimate < actual {
			t.Errorf("%q 估算 %d 字节，实际编码 %d 字节（json.Marshal: %s）", sample, estimate, actual, encoded)
		}
	}
}
