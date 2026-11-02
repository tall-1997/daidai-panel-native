package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (t *toolset) registerReadTools(s *mcp.Server) {
	addReadTool(s, "list_tasks", "查询任务列表",
		"分页查询定时任务，可按关键词、运行状态、分组、标签筛选。返回精简字段：enabled 是启用开关（与是否正在运行无关），status_text 是运行状态。",
		t.listTasks)
	addReadTool(s, "get_task", "查看任务详情",
		"按 ID 查看一个定时任务的完整配置与状态。",
		t.getTask)
	addReadTool(s, "get_task_log", "查看任务最近一次日志",
		"查看某个任务最近一次执行的日志正文；日志过长时只保留末尾（报错通常在最后）。",
		t.getTaskLog)
	addReadTool(s, "list_task_labels", "查询任务标签",
		"列出全部任务用过的自定义标签，以及各自挂在多少个任务上（count），按名称升序。"+
			"只含用户自己加的标签：「分组:名称」与「subscription:ID」两类内部标签不会出现，分组请用 list_tasks 的 group 筛选。"+
			"标签名可以传给 list_tasks 的 label 参数筛选任务（那是模糊匹配）。",
		t.listTaskLabels)
	addReadTool(s, "list_logs", "查询执行记录",
		"分页查询任务执行记录，可按任务、结果（成功 / 失败 / 运行中 / 已终止）筛选，适合巡检失败任务。不含日志正文，正文用 get_log 查看。",
		t.listLogs)
	addReadTool(s, "get_log", "查看一条执行日志",
		"按执行记录 ID 查看日志正文；过长时只保留末尾。",
		t.getLog)
	addReadTool(s, "list_envs", "查询环境变量",
		"分页查询环境变量。名称像凭据的变量（含 TOKEN、SECRET、PASSWORD、COOKIE 等，或以 _KEY、_PWD、_CK 等结尾）的值会被遮蔽，并带 value_masked: true。"+
			"每条带 important：用户标为「重要」的变量，删除或替换导入前务必先向用户确认。",
		t.listEnvs)
	addReadTool(s, "list_scripts", "查询脚本文件",
		"列出脚本目录里的文件（默认扁平列表，可按路径关键词过滤），或返回目录树。",
		t.listScripts)
	addReadTool(s, "read_script", "读取脚本内容",
		"按字节分段读取一个脚本文件：offset 是起始字节（默认 0），limit 是本段最多字节数（默认且最多 49152）。"+
			"返回 total_bytes（文件总字节数）、offset、next_offset 与 truncated；truncated 为 true 表示还没读完，用 next_offset 作为下一次的 offset 继续读，"+
			"直到 truncated 为 false。分段一定落在 UTF-8 字符边界上，各段按顺序拼起来就是完整文件。二进制文件不返回内容。",
		t.readScript)
	addReadTool(s, "list_subscriptions", "查询订阅",
		"分页查询订阅（Git 仓库 / 单文件），包括白名单、黑名单、依赖规则与最近拉取时间。",
		t.listSubscriptions)
	addReadTool(s, "get_system_info", "查看系统信息",
		"查看面板所在机器的资源占用、面板版本与部署形态。",
		t.getSystemInfo)
	addReadTool(s, "get_dashboard", "查看概览统计",
		"查看面板概览页的统计数据（任务数量、执行成功与失败次数等）。",
		t.getDashboard)

	// #139 对齐开放 API 补上的查询工具，按领域分在各自的文件里。
	t.registerScriptReadTools(s)
	t.registerEnvBatchReadTools(s)
	t.registerNotifyBackupReadTools(s)
}

// ---- 任务 ------------------------------------------------------------------

// taskGroupLabelPrefix 与 handler/task_query.go 的同名常量同一口径：分组存成「分组:名称」标签。
const taskGroupLabelPrefix = "分组:"

var taskStatusAliases = map[string]string{
	"disabled": "0", "禁用": "0",
	"queued": "0.5", "排队": "0.5", "排队中": "0.5",
	"idle": "1", "enabled": "1", "空闲": "1",
	"running": "2", "运行中": "2",
	"0": "0", "0.5": "0.5", "1": "1", "2": "2",
}

var logStatusAliases = map[string]string{
	"success": "0", "成功": "0",
	"failed": "1", "失败": "1",
	"running": "2", "运行中": "2",
	"aborted": "3", "已终止": "3", "终止": "3",
	"0": "0", "1": "1", "2": "2", "3": "3",
}

type listTasksInput struct {
	Keyword  string `json:"keyword,omitempty" jsonschema:"按任务名称或命令模糊搜索"`
	Status   string `json:"status,omitempty" jsonschema:"按运行状态筛选：disabled（禁用）、idle（空闲）、queued（排队中）、running（运行中），也可以直接填 0、1、0.5、2"`
	Group    string `json:"group,omitempty" jsonschema:"按分组筛选（任务标签里的「分组:名称」，精确匹配名称）"`
	Label    string `json:"label,omitempty" jsonschema:"按标签模糊匹配"`
	Page     int    `json:"page,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"每页条数，默认 20，最大 100"`
}

type taskIDInput struct {
	ID int64 `json:"id" jsonschema:"任务 ID，可从 list_tasks 获得"`
}

func (t *toolset) listTasks(ctx context.Context, in listTasksInput) (any, error) {
	query := url.Values{}
	if keyword := strings.TrimSpace(in.Keyword); keyword != "" {
		query.Set("keyword", keyword)
	}
	if raw := strings.TrimSpace(in.Status); raw != "" {
		status, ok := taskStatusAliases[strings.ToLower(raw)]
		if !ok {
			return nil, fmt.Errorf("status 只能是 disabled、idle、queued、running（或 0、1、0.5、2），收到 %q", raw)
		}
		query.Set("status", status)
	}
	if label := strings.TrimSpace(in.Label); label != "" {
		query.Set("label", label)
	}
	if group := strings.TrimSpace(in.Group); group != "" {
		// 分组筛选走列表接口的 filters（field=group），与网页端顶栏分组标签是同一个参数。
		filters, _ := json.Marshal([]map[string]string{{"field": "group", "operator": "equals", "value": group}})
		query.Set("filters", string(filters))
	}
	setPageQuery(query, in.Page, in.PageSize)

	result, err := t.call(ctx, http.MethodGet, "/tasks", query, nil)
	if err != nil {
		return nil, err
	}
	items := itemsOf(result)
	slim := make([]map[string]any, 0, len(items))
	for _, item := range items {
		// env_scope 是可用环境变量范围（#168，all / selected）。列表只给这一个键保持精简，分组与变量名要看 get_task
		out := pickFields(item, "id", "name", "command", "cron_expression", "task_type", "labels",
			"is_pinned", "last_run_at", "last_running_time", "next_run_at", "schedule_hint", "env_scope")
		decorateTask(out, item)
		slim = append(slim, out)
	}
	return paginated(result, slim), nil
}

func (t *toolset) getTask(ctx context.Context, in taskIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	// 面板没有 GET /tasks/:id（APP 也是这样取详情），这里用列表接口的 all=1 取全量再按 ID 挑。
	result, err := t.call(ctx, http.MethodGet, "/tasks", url.Values{"all": {"1"}}, nil)
	if err != nil {
		return nil, err
	}
	for _, item := range itemsOf(result) {
		if id, ok := numberValue(item["id"]); ok && int64(id) == in.ID {
			return taskDetail(item), nil
		}
	}
	return nil, fmt.Errorf("任务 %d 不存在", in.ID)
}

func (t *toolset) getTaskLog(ctx context.Context, in taskIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d/latest-log", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	return formatLogDetail(result), nil
}

// taskLabelItem 是 GET /tasks/labels 的一项（#157 契约 L1）：自定义标签名 + 带这个标签的任务数。
type taskLabelItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// listTaskLabels 转发到 GET /tasks/labels（#157）。
// 这个接口与 /tasks/groups 一样返回裸数组 [{name, count}]，而 call 只会把响应解成对象，
// 裸数组到它手里会变成「无法解析的内容」，所以这里直接经 Dispatcher 取原始响应、单独解码；
// 非 2xx 照旧交给 apiError，应用缺 tasks 权限时的提示与其它工具一致。
func (t *toolset) listTaskLabels(ctx context.Context, _ emptyInput) (any, error) {
	status, payload, err := t.d.Do(ctx, http.MethodGet, "/tasks/labels", nil, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, apiError(status, payload)
	}
	var labels []taskLabelItem
	if err := json.Unmarshal(payload, &labels); err != nil {
		return nil, fmt.Errorf("面板接口返回了无法解析的内容（HTTP %d）", status)
	}
	return map[string]any{"total": len(labels), "labels": labels}, nil
}

// taskDetail 返回任务的全部字段外加 decorateTask 的派生字段（get_task、create_task、update_task 共用）。
func taskDetail(item map[string]any) map[string]any {
	out := make(map[string]any, len(item)+4)
	for key, value := range item {
		out[key] = value
	}
	decorateTask(out, item)
	return out
}

// decorateTask 给任务补上 AI 读得懂的状态文字、启用开关与分组名。
func decorateTask(out, item map[string]any) {
	status, _ := numberValue(item["status"])
	out["status"] = item["status"]
	out["status_text"] = taskStatusText(status)
	out["enabled"] = taskEnabled(item, status)
	out["last_run_status"] = item["last_run_status"]
	out["last_run_status_text"] = runStatusText(item["last_run_status"])
	if group := taskGroupName(item["labels"]); group != "" {
		out["group"] = group
	}
}

// taskEnabled 读列表项的 enabled（v3.2.8 起服务端下发，只表示启用开关、与运行态无关）；
// 老面板没有这个字段时退回 status != 0，与网页端 isTaskSwitchOn 同一口径。
func taskEnabled(item map[string]any, status float64) bool {
	if enabled, ok := item["enabled"].(bool); ok {
		return enabled
	}
	return status != 0
}

// taskGroupName 与 handler/task_query.go 同一口径：trim 后取第一个「分组:」标签的名称。
func taskGroupName(raw any) string {
	labels, _ := raw.([]any)
	for _, entry := range labels {
		label := strings.TrimSpace(stringValue(entry))
		if !strings.HasPrefix(label, taskGroupLabelPrefix) {
			continue
		}
		if name := strings.TrimSpace(strings.TrimPrefix(label, taskGroupLabelPrefix)); name != "" {
			return name
		}
	}
	return ""
}

func taskStatusText(status float64) string {
	switch status {
	case 0:
		return "禁用"
	case 0.5:
		return "排队中"
	case 2:
		return "运行中"
	default:
		return "空闲"
	}
}

func runStatusText(value any) string {
	status, ok := numberValue(value)
	if !ok {
		return "未运行"
	}
	switch status {
	case 0:
		return "成功"
	case 1:
		return "失败"
	case 2:
		return "已终止"
	default:
		return "未知"
	}
}

func logStatusText(value any) string {
	status, ok := numberValue(value)
	if !ok {
		return "未知"
	}
	switch status {
	case 0:
		return "成功"
	case 1:
		return "失败"
	case 2:
		return "运行中"
	case 3:
		return "已终止"
	default:
		return "未知"
	}
}

// ---- 执行记录 ----------------------------------------------------------------

type listLogsInput struct {
	TaskID   int64  `json:"task_id,omitempty" jsonschema:"只看某个任务的执行记录"`
	Status   string `json:"status,omitempty" jsonschema:"按结果筛选：success（成功）、failed（失败）、running（运行中）、aborted（已终止），也可以填 0、1、2、3"`
	Keyword  string `json:"keyword,omitempty" jsonschema:"按任务名称模糊搜索"`
	Page     int    `json:"page,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"每页条数，默认 20，最大 100"`
}

type logIDInput struct {
	ID int64 `json:"id" jsonschema:"执行记录（日志）ID，可从 list_logs 获得"`
}

func (t *toolset) listLogs(ctx context.Context, in listLogsInput) (any, error) {
	query := url.Values{}
	if in.TaskID > 0 {
		query.Set("task_id", strconv.FormatInt(in.TaskID, 10))
	}
	if raw := strings.TrimSpace(in.Status); raw != "" {
		status, ok := logStatusAliases[strings.ToLower(raw)]
		if !ok {
			return nil, fmt.Errorf("status 只能是 success、failed、running、aborted（或 0、1、2、3），收到 %q", raw)
		}
		query.Set("status", status)
	}
	if keyword := strings.TrimSpace(in.Keyword); keyword != "" {
		query.Set("keyword", keyword)
	}
	setPageQuery(query, in.Page, in.PageSize)

	result, err := t.call(ctx, http.MethodGet, "/logs", query, nil)
	if err != nil {
		return nil, err
	}
	items := itemsOf(result)
	slim := make([]map[string]any, 0, len(items))
	for _, item := range items {
		// 列表里的 content 是压缩后的 base64，对 AI 没用还占篇幅，一律不给。
		out := pickFields(item, "id", "task_id", "task_name", "status", "duration", "started_at", "ended_at")
		out["status_text"] = logStatusText(item["status"])
		slim = append(slim, out)
	}
	return paginated(result, slim), nil
}

func (t *toolset) getLog(ctx context.Context, in logIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodGet, fmt.Sprintf("/logs/%d", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	return formatLogDetail(result), nil
}

func formatLogDetail(result map[string]any) map[string]any {
	out := pickFields(result, "id", "task_id", "task_name", "command", "status", "duration", "started_at", "ended_at")
	out["status_text"] = logStatusText(result["status"])
	content := stringValue(result["content"])
	text, cut := tailText(content, contentBudget)
	out["content"] = text
	if cut {
		out["content_truncated"] = fmt.Sprintf("日志共 %d 字节，只保留了末尾 %d 字节", len(content), len(text))
	}
	return out
}

// ---- 环境变量 ----------------------------------------------------------------

type listEnvsInput struct {
	Keyword  string `json:"keyword,omitempty" jsonschema:"按变量名、备注、分组或值模糊搜索（敏感变量只按名称、备注、分组匹配）"`
	Group    string `json:"group,omitempty" jsonschema:"按分组筛选"`
	Enabled  *bool  `json:"enabled,omitempty" jsonschema:"只看启用（true）或禁用（false）的变量"`
	Page     int    `json:"page,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"每页条数，默认 20，最大 100"`
}

func (t *toolset) listEnvs(ctx context.Context, in listEnvsInput) (any, error) {
	query := url.Values{}
	if group := strings.TrimSpace(in.Group); group != "" {
		query.Set("group", group)
	}
	if in.Enabled != nil {
		query.Set("enabled", strconv.FormatBool(*in.Enabled))
	}

	keyword := strings.TrimSpace(in.Keyword)
	if keyword == "" {
		setPageQuery(query, in.Page, in.PageSize)
		result, err := t.call(ctx, http.MethodGet, "/envs", query, nil)
		if err != nil {
			return nil, err
		}
		items := itemsOf(result)
		slim := make([]map[string]any, 0, len(items))
		for _, item := range items {
			slim = append(slim, slimEnv(item))
		}
		return paginated(result, slim), nil
	}

	// 关键词不交给面板：面板的 keyword 同时匹配变量值。遮蔽了值、却让面板按值筛，
	// 那么「这一行在不在」「total 是几」「翻页落在哪」都会告诉 AI 某个凭据里含不含这段字符，
	// 拿关键词一段段试就能拼出凭据（只把命中的行藏起来也没用，条数和分页照样漏）。
	// 所以取全量（all=1，只带分组 / 启用筛选）在本地筛：敏感变量只认名称、备注、分组命中，
	// 普通变量照旧也按值匹配；分页与 total 在本地算，输出与敏感变量的值完全无关。
	query.Set("all", "1")
	result, err := t.call(ctx, http.MethodGet, "/envs", query, nil)
	if err != nil {
		return nil, err
	}
	lowerKeyword := strings.ToLower(keyword)
	items := itemsOf(result)
	matched := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if envMatchesKeyword(item, lowerKeyword) {
			matched = append(matched, item)
		}
	}

	// 与面板列表接口同一口径：页码从 1 开始，每页默认 20、最多 100。
	page, pageSize := in.Page, in.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	start := len(matched)
	if page-1 <= len(matched)/pageSize {
		start = min((page-1)*pageSize, len(matched))
	}
	end := min(start+pageSize, len(matched))
	slim := make([]map[string]any, 0, end-start)
	for _, item := range matched[start:end] {
		slim = append(slim, slimEnv(item))
	}

	out := map[string]any{"items": slim, "total": len(matched), "page": page, "page_size": pageSize}
	// 面板取全量有 5000 条的硬上限；这里的 total 只受分组 / 启用筛选影响，与变量值无关。
	if total, ok := numberValue(result["total"]); ok && int(total) > len(items) {
		out["note"] = fmt.Sprintf("变量共 %d 条，关键词搜索只覆盖了前 %d 条", int(total), len(items))
	}
	return out, nil
}

// envMatchesKeyword 不区分大小写地匹配名称、备注、分组；只有非敏感变量才按值匹配。
func envMatchesKeyword(item map[string]any, lowerKeyword string) bool {
	for _, key := range []string{"name", "remarks", "group"} {
		if strings.Contains(strings.ToLower(stringValue(item[key])), lowerKeyword) {
			return true
		}
	}
	if IsSensitiveEnvName(stringValue(item["name"])) {
		return false
	}
	return strings.Contains(strings.ToLower(stringValue(item["value"])), lowerKeyword)
}

// slimEnv 输出环境变量的精简字段，敏感变量的值一律遮蔽（增删改的返回值也走这里）。
// important 是「重要」标记（APP #16）：只提醒 AI 删除前先问用户，面板本身不拦删除。
func slimEnv(item map[string]any) map[string]any {
	out := pickFields(item, "id", "name", "remarks", "enabled", "group", "important", "updated_at")
	value := stringValue(item["value"])
	if IsSensitiveEnvName(stringValue(item["name"])) {
		out["value"] = MaskEnvValue(value)
		out["value_masked"] = true
	} else {
		out["value"] = value
	}
	return out
}

// ---- 脚本 --------------------------------------------------------------------

type listScriptsInput struct {
	Keyword string `json:"keyword,omitempty" jsonschema:"只列出路径包含该关键词的文件（不区分大小写，仅扁平列表模式有效）"`
	Tree    bool   `json:"tree,omitempty" jsonschema:"为 true 时返回目录树（含空目录），默认返回扁平文件列表"`
}

type readScriptInput struct {
	Path   string `json:"path" jsonschema:"脚本相对路径（相对脚本目录），例如 demo/test.py，可从 list_scripts 获得"`
	Offset int    `json:"offset,omitempty" jsonschema:"从第几个字节开始读（从 0 开始），续读时填上一次返回的 next_offset"`
	Limit  int    `json:"limit,omitempty" jsonschema:"本段最多读取的字节数，默认且最多 49152"`
}

// readScriptMaxLimit 是 read_script 单段的字节上限，与其它大字段共用 contentBudget。
const readScriptMaxLimit = contentBudget

func (t *toolset) listScripts(ctx context.Context, in listScriptsInput) (any, error) {
	if in.Tree {
		result, err := t.call(ctx, http.MethodGet, "/scripts/tree", nil, nil)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tree": result["data"]}, nil
	}

	result, err := t.call(ctx, http.MethodGet, "/scripts", nil, nil)
	if err != nil {
		return nil, err
	}
	keyword := strings.ToLower(strings.TrimSpace(in.Keyword))
	files := make([]map[string]any, 0)
	for _, item := range itemsOf(result) {
		path := stringValue(item["path"])
		if keyword != "" && !strings.Contains(strings.ToLower(path), keyword) {
			continue
		}
		files = append(files, pickFields(item, "path", "size", "mtime"))
	}
	return map[string]any{"total": len(files), "files": files}, nil
}

// readScript 分段读取（#139）：以前超过 48KB 只返回开头、靠一个附加字段提示截断，
// 调用方只看 content 就会把前三分之一当成整个文件。现在每段都明确给出 truncated 与 next_offset。
func (t *toolset) readScript(ctx context.Context, in readScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	if in.Offset < 0 || in.Limit < 0 {
		return nil, errors.New("offset 与 limit 不能是负数")
	}
	limit := in.Limit
	if limit == 0 || limit > readScriptMaxLimit {
		limit = readScriptMaxLimit
	}

	result, err := t.call(ctx, http.MethodGet, "/scripts/content", url.Values{"path": {path}}, nil)
	if err != nil {
		return nil, err
	}
	data := objectOf(result)
	if binary, _ := data["binary"].(bool); binary {
		return map[string]any{"path": path, "binary": true, "note": "二进制文件，不返回内容"}, nil
	}
	content := stringValue(data["content"])
	if in.Offset > len(content) {
		return nil, fmt.Errorf("offset %d 超出文件大小（共 %d 字节）", in.Offset, len(content))
	}

	start, end := scriptChunk(content, in.Offset, limit)
	out := map[string]any{
		"path":        path,
		"content":     content[start:end],
		"offset":      start,
		"next_offset": end,
		"total_bytes": len(content),
		"truncated":   end < len(content),
	}
	if end < len(content) {
		out["hint"] = fmt.Sprintf("还有 %d 字节未读，用 offset=%d 继续读取", len(content)-end, end)
	}
	return out, nil
}

// scriptChunk 从 offset 起切出一段 [start, end)：两端都落在 UTF-8 字符边界上，原始字节不超过 limit，
// 且这段序列化成 JSON 后不超过 contentBudget —— 引号、换行、控制字符转义后会变长，只按原始字节切，
// 整条输出可能撑破 MaxOutputBytes、被 truncateOutput 截成不合法的 JSON，分段也就拼不回原文了。
// 至少前进一个字符，limit 比一个字符还小时也不会原地打转。
func scriptChunk(content string, offset, limit int) (int, int) {
	start := offset
	// offset 落在多字节字符中间时退回字符开头：宁可多给几个字节，也不能丢字符。
	for back := 0; back < utf8.UTFMax-1 && start > 0 && start < len(content) && !utf8.RuneStart(content[start]); back++ {
		start--
	}

	end, encoded := start, 0
	for end < len(content) {
		r, size := utf8.DecodeRuneInString(content[end:])
		cost := jsonEscapedLen(r, size)
		if end > start && (end-start+size > limit || encoded+cost > contentBudget) {
			break
		}
		end += size
		encoded += cost
	}
	return start, end
}

// jsonEscapedLen 估算一个字符在 renderOutput（关闭 HTML 转义）里编码后的字节数，只会多估、不会少估。
func jsonEscapedLen(r rune, size int) int {
	switch {
	case r == utf8.RuneError && size == 1:
		return 6 // 非法字节编码成 �
	case r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '\t':
		return 2
	case r < 0x20 || r == ' ' || r == ' ':
		return 6 // \u00XX、  这类
	default:
		return size
	}
}

// ---- 订阅与系统 ----------------------------------------------------------------

type listSubscriptionsInput struct {
	Keyword  string `json:"keyword,omitempty" jsonschema:"按名称或地址模糊搜索"`
	Type     string `json:"type,omitempty" jsonschema:"按类型筛选：git-repo（Git 仓库）或 single-file（单文件）"`
	Enabled  *bool  `json:"enabled,omitempty" jsonschema:"只看启用（true）或禁用（false）的订阅"`
	Page     int    `json:"page,omitempty" jsonschema:"页码，从 1 开始，默认 1"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"每页条数，默认 20，最大 100"`
}

type emptyInput struct{}

func (t *toolset) listSubscriptions(ctx context.Context, in listSubscriptionsInput) (any, error) {
	query := url.Values{}
	if keyword := strings.TrimSpace(in.Keyword); keyword != "" {
		query.Set("keyword", keyword)
	}
	if subType := strings.TrimSpace(in.Type); subType != "" {
		query.Set("type", subType)
	}
	if in.Enabled != nil {
		query.Set("enabled", strconv.FormatBool(*in.Enabled))
	}
	setPageQuery(query, in.Page, in.PageSize)

	result, err := t.call(ctx, http.MethodGet, "/subscriptions", query, nil)
	if err != nil {
		return nil, err
	}
	items := itemsOf(result)
	slim := make([]map[string]any, 0, len(items))
	for _, item := range items {
		// 订阅接口本身不下发令牌明文（只有 has_auth_token），这里只是挑掉对 AI 没用的字段。
		slim = append(slim, pickFields(item, "id", "name", "alias", "type", "url", "branch", "schedule",
			"whitelist", "blacklist", "depend_on", "sub_path", "save_dir", "enabled", "status",
			"last_pull_at", "auth_type", "has_auth_token", "auto_add_task_mode", "auto_del_task_mode"))
	}
	return paginated(result, slim), nil
}

func (t *toolset) getSystemInfo(ctx context.Context, _ emptyInput) (any, error) {
	result, err := t.call(ctx, http.MethodGet, "/system/info", nil, nil)
	if err != nil {
		return nil, err
	}
	return objectOf(result), nil
}

func (t *toolset) getDashboard(ctx context.Context, _ emptyInput) (any, error) {
	result, err := t.call(ctx, http.MethodGet, "/system/dashboard", nil, nil)
	if err != nil {
		return nil, err
	}
	return objectOf(result), nil
}
