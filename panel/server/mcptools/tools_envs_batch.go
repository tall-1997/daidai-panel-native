package mcptools

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 环境变量的批量操作、导入与导出（#139），对应 /envs/batch/*、POST /envs/import、GET /envs/export-all。
// 单条增删改仍在 tools_write.go。

func (t *toolset) registerEnvBatchReadTools(s *mcp.Server) {
	addReadTool(s, "export_envs", "导出环境变量",
		"不分页导出环境变量（名称、值、备注、分组、启用状态、是否重要），格式与 import_envs 的 envs 参数一致。"+
			"名称像凭据的变量值会被遮蔽（带 value_masked: true），遮蔽后的值不能再拿去导入。变量很多时输出可能超过 64KB 被截断，可用 ids 分批导出。",
		t.exportEnvs)
}

func (t *toolset) registerEnvBatchWriteTools(s *mcp.Server) {
	addWriteTool(s, "batch_env_action", "批量操作环境变量",
		"对多个环境变量执行同一操作：enable（启用）、disable（禁用）、delete（删除，不可恢复）。",
		writeHints{destructive: true, idempotent: true}, t.batchEnvAction)
	addWriteTool(s, "import_envs", "导入环境变量",
		"批量导入环境变量。mode=merge（默认）：名称与备注都相同的已有变量会被覆盖值、分组与启用状态，其余新增；"+
			"mode=merge_value：名称与值都相同的视为同一条，更新其备注（给了才写）、分组与启用状态，其余新增；"+
			"mode=replace：先删除面板上的全部环境变量再导入，原有变量不可恢复。导入前会先检查变量名格式，并拒绝 export_envs / list_envs 输出的遮蔽值。"+
			"条目可带 important：merge 只会把已有变量标为重要、不会取消标记；replace 会连重要变量一起删除。",
		writeHints{destructive: true}, t.importEnvs)
}

// ---- 导出 --------------------------------------------------------------------

type exportEnvsInput struct {
	IDs         []int64 `json:"ids,omitempty" jsonschema:"只导出这些 ID 的变量（ID 可从 list_envs 获得），默认导出全部"`
	EnabledOnly bool    `json:"enabled_only,omitempty" jsonschema:"为 true 时只导出启用的变量"`
}

func (t *toolset) exportEnvs(ctx context.Context, in exportEnvsInput) (any, error) {
	query := url.Values{}
	if len(in.IDs) > 0 {
		ids := make([]string, 0, len(in.IDs))
		for _, id := range in.IDs {
			if err := requireID("ids 中的变量 ID", id); err != nil {
				return nil, err
			}
			ids = append(ids, strconv.FormatInt(id, 10))
		}
		query.Set("ids", strings.Join(ids, ","))
	}
	result, err := t.call(ctx, http.MethodGet, "/envs/export-all", query, nil)
	if err != nil {
		return nil, err
	}

	envs := make([]map[string]any, 0)
	masked := 0
	for _, item := range itemsOf(result) {
		if enabled, _ := item["enabled"].(bool); in.EnabledOnly && !enabled {
			continue
		}
		// 与 list_envs 同一套遮蔽：导出同样是 MCP 的输出，凭据不能因为换了个工具就进了 AI 的上下文。
		out := pickFields(item, "name", "remarks", "group", "enabled", "important")
		value := stringValue(item["value"])
		if IsSensitiveEnvName(stringValue(item["name"])) {
			out["value"] = MaskEnvValue(value)
			out["value_masked"] = true
			masked++
		} else {
			out["value"] = value
		}
		envs = append(envs, out)
	}
	out := map[string]any{"total": len(envs), "envs": envs}
	if masked > 0 {
		out["note"] = fmt.Sprintf("%d 个名称像凭据的变量值已遮蔽，需要明文请在面板网页端导出", masked)
	}
	return out, nil
}

// ---- 批量与导入 ----------------------------------------------------------------

type batchEnvActionInput struct {
	IDs    []int64 `json:"ids" jsonschema:"环境变量 ID 列表（1 到 100 个），可从 list_envs 获得"`
	Action string  `json:"action" jsonschema:"操作：enable（启用）、disable（禁用）、delete（删除，不可恢复）"`
}

type importEnvItem struct {
	Name    string `json:"name" jsonschema:"变量名（字母、数字、下划线，不能以数字开头）"`
	Value   string `json:"value" jsonschema:"变量值"`
	Remarks string `json:"remarks,omitempty" jsonschema:"备注；merge 模式下按「名称 + 备注」判断是否已存在"`
	Group   string `json:"group,omitempty" jsonschema:"分组（多个分组用英文逗号分隔）"`
	Enabled *bool  `json:"enabled,omitempty" jsonschema:"是否启用，默认 true"`
	// 「重要」标记（APP #16）：只有 true 会转发给面板，见 importEnvs。
	Important bool `json:"important,omitempty" jsonschema:"是否标为重要变量；merge 模式下只会把已有变量标为重要，不会取消标记"`
}

type importEnvsInput struct {
	Envs []importEnvItem `json:"envs" jsonschema:"要导入的变量，1 到 1000 条"`
	Mode string          `json:"mode,omitempty" jsonschema:"merge（默认，覆盖同名同备注的变量、其余新增）、merge_value（名称与值都相同的视为同一条、其余新增）或 replace（先删除全部现有变量再导入，不可恢复）"`
}

const (
	maxBatchEnvIDs   = 100
	maxImportEnvRows = 1000
)

// importEnvNamePattern 与 handler/env.go 的 envNamePattern 同一口径。
// 在这里先校验一遍不是多余：replace 模式下面板是「先清空、再逐条导入」，
// 如果每一条都因名称不合法被拒，原有变量已经删光了，接口却只回一句「没有成功导入任何环境变量」。
var importEnvNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (t *toolset) batchEnvAction(ctx context.Context, in batchEnvActionInput) (any, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	var method, path string
	switch action {
	case "enable":
		method, path = http.MethodPut, "/envs/batch/enable"
	case "disable":
		method, path = http.MethodPut, "/envs/batch/disable"
	case "delete":
		method, path = http.MethodDelete, "/envs/batch"
	default:
		return nil, fmt.Errorf("action 只能是 enable、disable、delete，收到 %q", in.Action)
	}
	if len(in.IDs) == 0 || len(in.IDs) > maxBatchEnvIDs {
		return nil, fmt.Errorf("ids 需要 1 到 %d 个环境变量 ID", maxBatchEnvIDs)
	}
	for _, id := range in.IDs {
		if err := requireID("ids 中的变量 ID", id); err != nil {
			return nil, err
		}
	}
	result, err := t.call(ctx, method, path, nil, map[string]any{"ids": in.IDs})
	if err != nil {
		return nil, err
	}
	return map[string]any{"message": result["message"], "action": action}, nil
}

func (t *toolset) importEnvs(ctx context.Context, in importEnvsInput) (any, error) {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "merge_value" && mode != "replace" {
		return nil, fmt.Errorf("mode 只能是 merge、merge_value 或 replace，收到 %q", in.Mode)
	}
	if len(in.Envs) == 0 || len(in.Envs) > maxImportEnvRows {
		return nil, fmt.Errorf("envs 需要 1 到 %d 条", maxImportEnvRows)
	}

	items := make([]map[string]any, 0, len(in.Envs))
	for i, env := range in.Envs {
		name := strings.TrimSpace(env.Name)
		if !importEnvNamePattern.MatchString(name) {
			return nil, fmt.Errorf("第 %d 条的变量名 %q 格式无效（只能用字母、数字、下划线，不能以数字开头），没有导入任何变量", i+1, env.Name)
		}
		if IsSensitiveEnvName(name) && looksLikeMaskedEnvValue(env.Value) {
			return nil, fmt.Errorf("第 %d 条 %s 的值像是 export_envs / list_envs 输出的遮蔽值，导入会把真实凭据覆盖掉，没有导入任何变量", i+1, name)
		}
		item := map[string]any{"name": name, "value": env.Value, "remarks": env.Remarks}
		// merge_value 命中时「有 remarks 键才写备注」：没给备注就不带这个键，免得把已有备注清空
		if mode == "merge_value" && env.Remarks == "" {
			delete(item, "remarks")
		}
		if group := strings.TrimSpace(env.Group); group != "" {
			item["group"] = group
		}
		if env.Enabled != nil {
			item["enabled"] = *env.Enabled
		}
		// 只转发 true：面板 merge 时只升不降、新建默认 false，转发 false 没有任何效果。
		if env.Important {
			item["important"] = true
		}
		items = append(items, item)
	}

	result, err := t.call(ctx, http.MethodPost, "/envs/import", nil, map[string]any{"envs": items, "mode": mode})
	if err != nil {
		return nil, err
	}
	out := map[string]any{"message": result["message"], "mode": mode}
	if problems, ok := result["errors"].([]any); ok && len(problems) > 0 {
		out["errors"] = problems
	}
	return out, nil
}

// looksLikeMaskedEnvValue 判断一个值是不是 MaskEnvValue 的输出形态：整段 8 个星号，
// 或「3 个字符 + 6 个星号 + 3 个字符」。真实凭据恰好长这样的概率可以忽略。
func looksLikeMaskedEnvValue(value string) bool {
	if value == "********" {
		return true
	}
	return utf8.RuneCountInString(value) == 12 && string([]rune(value)[3:9]) == "******"
}
