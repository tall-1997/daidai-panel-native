package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 脚本文件的目录树、版本、删改移复制与代码片段执行（#139），对应开放 API 的 /scripts/* 接口。
// 路径校验全部交给面板：它拒绝 ..、绝对路径，以及 .git / .svn / .hg / .bzr、node_modules、__pycache__
// 这类被隔离的目录（任意一段命中都拒，返回「该路径不可访问」），工具层不再重复一套规则。

const scriptPathRulesHint = "路径相对脚本目录；.git 等版本库元数据、node_modules、__pycache__ 这类被隔离的目录不能通过脚本接口访问，会返回「该路径不可访问」。"

func (t *toolset) registerScriptReadTools(s *mcp.Server) {
	addReadTool(s, "get_script_tree", "查看脚本目录树",
		"返回脚本目录的完整目录树（含空目录），每个节点给出 name、path、type（directory / file），文件另有 size 与 mtime。"+
			"可用 path 只看某个子目录，用 max_depth 限制展开层数；目录很大时建议先限制层数再逐级展开。",
		t.getScriptTree)
	addReadTool(s, "list_script_versions", "查看脚本历史版本",
		"列出某个脚本最近 50 个历史版本（每次通过面板保存、回滚都会记一版），最新的在前。返回的 id 可传给 rollback_script。",
		t.listScriptVersions)
}

func (t *toolset) registerScriptWriteTools(s *mcp.Server) {
	addWriteTool(s, "delete_script", "删除脚本文件或目录",
		"删除脚本目录里的一个文件，或整个目录（type=directory，连同里面的全部内容），不可恢复。"+scriptPathRulesHint,
		writeHints{destructive: true, idempotent: true}, t.deleteScript)
	addWriteTool(s, "rename_script", "重命名脚本",
		"在原目录内重命名一个文件或目录；同目录下已有同名文件时会被覆盖。改名后引用它的任务命令不会自动更新，需要用 update_task 同步。"+scriptPathRulesHint,
		writeHints{destructive: true}, t.renameScript)
	addWriteTool(s, "move_script", "移动脚本",
		"把一个文件或目录移动到另一个目录（target_dir 留空表示脚本根目录），目标目录必须已存在；目标位置已有同名文件时会被覆盖。引用它的任务命令不会自动更新。"+scriptPathRulesHint,
		writeHints{destructive: true}, t.moveScript)
	addWriteTool(s, "copy_script", "复制脚本",
		"复制一个文件或目录到 target_dir（留空表示脚本根目录），可用 new_name 改名，默认沿用原名；目标已存在同名文件时会被覆盖。"+scriptPathRulesHint,
		writeHints{destructive: true}, t.copyScript)
	addWriteTool(s, "batch_delete_scripts", "批量删除脚本",
		"一次删除多个文件或目录（最多 100 项），不可恢复。逐项执行，某一项失败不影响其它项，结果里列出失败的路径。"+scriptPathRulesHint,
		writeHints{destructive: true, idempotent: true}, t.batchDeleteScripts)
	addWriteTool(s, "run_code", "执行代码片段",
		"直接执行一段代码并等待输出（最多约 50 秒），不需要先保存成脚本文件，适合 ls、md5sum、git status 这类一次性命令。"+
			"language 可选 shell（或 bash）、python、javascript、node、typescript、go。工作目录是一个临时目录而不是脚本目录，"+
			"脚本目录的绝对路径在环境变量 DAIDAI_SCRIPTS_DIR 里。到时仍在运行会返回 run_id，用 run_script 传同一个 run_id 可继续取输出或停止。代码以面板的权限执行。",
		writeHints{destructive: true, openWorld: true}, t.runCode)
	addWriteTool(s, "rollback_script", "回滚脚本版本",
		"把脚本内容恢复成某个历史版本（id 来自 list_script_versions），当前内容被覆盖；回滚本身也会记一个新版本，可以再回滚回来。",
		writeHints{destructive: true, idempotent: true}, t.rollbackScript)
}

// ---- 只读 --------------------------------------------------------------------

type scriptTreeInput struct {
	Path     string `json:"path,omitempty" jsonschema:"只返回这个子目录下的树（相对脚本目录），默认整个脚本目录"`
	MaxDepth int    `json:"max_depth,omitempty" jsonschema:"最多展开几层目录，默认不限；超出层数的目录只给出 child_count，不展开"`
}

type scriptPathInput struct {
	Path string `json:"path" jsonschema:"脚本相对路径（相对脚本目录），例如 demo/test.py"`
}

// scriptTreeStats 统计整棵子树的规模（含没展开的部分），让调用方知道目录有多大。
type scriptTreeStats struct {
	directories int
	files       int
	collapsed   bool
}

func (t *toolset) getScriptTree(ctx context.Context, in scriptTreeInput) (any, error) {
	if in.MaxDepth < 0 {
		return nil, errors.New("max_depth 不能是负数")
	}
	result, err := t.call(ctx, http.MethodGet, "/scripts/tree", nil, nil)
	if err != nil {
		return nil, err
	}
	nodes, _ := result["data"].([]any)

	root := strings.Trim(strings.ReplaceAll(strings.TrimSpace(in.Path), "\\", "/"), "/")
	if root != "" {
		dir, found := findScriptTreeDir(nodes, root)
		if !found {
			return nil, fmt.Errorf("目录不存在：%s（注意 path 要指向目录，不是文件）", root)
		}
		nodes, _ = dir["children"].([]any)
	}

	stats := &scriptTreeStats{}
	out := map[string]any{
		"path":        root,
		"tree":        slimScriptTree(nodes, 1, in.MaxDepth, stats),
		"directories": stats.directories,
		"files":       stats.files,
	}
	if stats.collapsed {
		out["note"] = "部分目录超过 max_depth 没有展开（只给出 child_count），可以用 path 指定子目录继续查看"
	}
	return out, nil
}

// findScriptTreeDir 按面板目录树的 key（相对路径，/ 分隔）找目录节点。
func findScriptTreeDir(nodes []any, target string) (map[string]any, bool) {
	for _, entry := range nodes {
		node, ok := entry.(map[string]any)
		if !ok || stringValue(node["type"]) != "directory" {
			continue
		}
		key := stringValue(node["key"])
		if key == target {
			return node, true
		}
		if strings.HasPrefix(target, key+"/") {
			children, _ := node["children"].([]any)
			return findScriptTreeDir(children, target)
		}
	}
	return nil, false
}

// slimScriptTree 把面板给前端树组件用的节点（key / title / isLeaf …）换成对 AI 更直白的字段。
func slimScriptTree(nodes []any, depth, maxDepth int, stats *scriptTreeStats) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, entry := range nodes {
		node, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		item := map[string]any{"name": node["title"], "path": node["key"]}
		if stringValue(node["type"]) == "directory" {
			stats.directories++
			item["type"] = "directory"
			children, _ := node["children"].([]any)
			if maxDepth > 0 && depth >= maxDepth {
				item["child_count"] = len(children)
				stats.collapsed = true
				// 没展开的部分也要计入规模统计，丢弃输出即可。
				slimScriptTree(children, depth+1, 0, stats)
			} else {
				item["children"] = slimScriptTree(children, depth+1, maxDepth, stats)
			}
		} else {
			stats.files++
			item["type"] = "file"
			item["size"] = node["size"]
			item["mtime"] = node["mtime"]
		}
		out = append(out, item)
	}
	return out
}

func (t *toolset) listScriptVersions(ctx context.Context, in scriptPathInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	result, err := t.call(ctx, http.MethodGet, "/scripts/versions", url.Values{"path": {path}}, nil)
	if err != nil {
		return nil, err
	}
	items := itemsOf(result)
	versions := make([]map[string]any, 0, len(items))
	for _, item := range items {
		versions = append(versions, pickFields(item, "id", "version", "message", "content_length", "created_at"))
	}
	return map[string]any{"path": path, "total": len(versions), "versions": versions}, nil
}

// ---- 写入 --------------------------------------------------------------------

type deleteScriptInput struct {
	Path string `json:"path" jsonschema:"要删除的文件或目录（相对脚本目录）"`
	Type string `json:"type,omitempty" jsonschema:"file（文件，默认）或 directory（目录，连同里面的全部内容）；删非空目录必须传 directory"`
}

type renameScriptInput struct {
	Path    string `json:"path" jsonschema:"要重命名的文件或目录（相对脚本目录）"`
	NewName string `json:"new_name" jsonschema:"新名称（只是名字，不含目录），例如 sign_v2.py"`
}

type moveScriptInput struct {
	Path      string `json:"path" jsonschema:"要移动的文件或目录（相对脚本目录）"`
	TargetDir string `json:"target_dir,omitempty" jsonschema:"目标目录（相对脚本目录，必须已存在），留空表示脚本根目录"`
}

type copyScriptInput struct {
	Path      string `json:"path" jsonschema:"要复制的文件或目录（相对脚本目录）"`
	TargetDir string `json:"target_dir,omitempty" jsonschema:"复制到哪个目录（相对脚本目录，不存在会自动创建），留空表示脚本根目录"`
	NewName   string `json:"new_name,omitempty" jsonschema:"副本的名称，默认沿用原名"`
}

type scriptDeleteItem struct {
	Path string `json:"path" jsonschema:"文件或目录（相对脚本目录）"`
	Type string `json:"type,omitempty" jsonschema:"file（默认）或 directory"`
}

type batchDeleteScriptsInput struct {
	Paths []scriptDeleteItem `json:"paths" jsonschema:"要删除的文件或目录，1 到 100 项"`
}

type runCodeInput struct {
	Code        string `json:"code" jsonschema:"要执行的代码"`
	Language    string `json:"language" jsonschema:"语言：shell（或 bash）、python、javascript、node（按 ES 模块执行）、typescript、go"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"最多等待的秒数（1 到 50，默认 50），到时仍未结束会返回 run_id 与已有输出"`
}

type scriptVersionIDInput struct {
	ID int64 `json:"id" jsonschema:"版本 ID（list_script_versions 返回的 id，不是版本号 version）"`
}

const maxBatchDeleteScripts = 100

// normalizeScriptDeleteType 与 DELETE /scripts 的 type 参数同一口径：只认 file 与 directory。
func normalizeScriptDeleteType(raw string) (string, error) {
	switch value := strings.ToLower(strings.TrimSpace(raw)); value {
	case "":
		return "file", nil
	case "file", "directory":
		return value, nil
	default:
		return "", fmt.Errorf("type 只能是 file 或 directory，收到 %q", raw)
	}
}

func (t *toolset) deleteScript(ctx context.Context, in deleteScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	fileType, err := normalizeScriptDeleteType(in.Type)
	if err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodDelete, "/scripts", url.Values{"path": {path}, "type": {fileType}}, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "path": path, "type": fileType}, nil
}

func (t *toolset) renameScript(ctx context.Context, in renameScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	newName := strings.TrimSpace(in.NewName)
	if path == "" || newName == "" {
		return nil, errors.New("path 与 new_name 都不能为空")
	}
	result, err := t.call(ctx, http.MethodPut, "/scripts/rename", nil, map[string]any{"old_path": path, "new_name": newName})
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "path": path, "new_path": result["new_path"]}, nil
}

func (t *toolset) moveScript(ctx context.Context, in moveScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	body := map[string]any{"source_path": path, "target_dir": strings.TrimSpace(in.TargetDir)}
	result, err := t.call(ctx, http.MethodPut, "/scripts/move", nil, body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "path": path, "new_path": result["new_path"]}, nil
}

func (t *toolset) copyScript(ctx context.Context, in copyScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	body := map[string]any{"source_path": path, "target_dir": strings.TrimSpace(in.TargetDir)}
	if newName := strings.TrimSpace(in.NewName); newName != "" {
		body["new_name"] = newName
	}
	result, err := t.call(ctx, http.MethodPost, "/scripts/copy", nil, body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "path": path, "new_path": result["new_path"]}, nil
}

func (t *toolset) batchDeleteScripts(ctx context.Context, in batchDeleteScriptsInput) (any, error) {
	if len(in.Paths) == 0 || len(in.Paths) > maxBatchDeleteScripts {
		return nil, fmt.Errorf("paths 需要 1 到 %d 项", maxBatchDeleteScripts)
	}
	items := make([]map[string]any, 0, len(in.Paths))
	for i, entry := range in.Paths {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			return nil, fmt.Errorf("paths 第 %d 项的 path 不能为空", i+1)
		}
		fileType, err := normalizeScriptDeleteType(entry.Type)
		if err != nil {
			return nil, fmt.Errorf("paths 第 %d 项：%w", i+1, err)
		}
		items = append(items, map[string]any{"path": path, "type": fileType})
	}
	result, err := t.call(ctx, http.MethodDelete, "/scripts/batch", nil, map[string]any{"paths": items})
	if err != nil {
		return nil, err
	}
	// 接口逐项执行、一律回 200；一项都没删成时翻译成工具错误，免得调用方把全部失败当成功。
	succeeded, _ := numberValue(result["success_count"])
	failed, _ := numberValue(result["failed_count"])
	if succeeded == 0 && failed > 0 {
		return nil, fmt.Errorf("全部删除失败（路径不存在、不可访问，或删非空目录时 type 没传 directory）：%v", result["failed_items"])
	}
	return pickFields(result, "message", "success_count", "failed_count", "failed_items"), nil
}

func (t *toolset) runCode(ctx context.Context, in runCodeInput) (any, error) {
	if strings.TrimSpace(in.Code) == "" {
		return nil, errors.New("code 不能为空")
	}
	// 面板按原样查语言表（区分大小写），这里统一成小写，Bash、Python 这类写法也能用。
	language := strings.ToLower(strings.TrimSpace(in.Language))
	if language == "" {
		return nil, errors.New("language 不能为空：可选 shell、bash、python、javascript、node、typescript、go")
	}
	started, err := t.call(ctx, http.MethodPost, "/scripts/run-code", nil, map[string]any{"code": in.Code, "language": language})
	if err != nil {
		return nil, err
	}
	runID := stringValue(started["run_id"])
	if runID == "" {
		return nil, errors.New("面板没有返回 run_id，无法跟踪这次运行")
	}
	return t.waitScriptRun(ctx, runID, normalizeScriptWait(in.WaitSeconds))
}

func (t *toolset) rollbackScript(ctx context.Context, in scriptVersionIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/scripts/versions/%d/rollback", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	// version 是回滚后新记下的版本号，想撤销这次回滚就再回滚到回滚前那一版。
	return map[string]any{"message": result["message"], "version_id": in.ID, "new_version": result["version"]}, nil
}
