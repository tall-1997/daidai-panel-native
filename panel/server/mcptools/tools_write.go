package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerWriteTools 只在 mcp_allow_mutations 开启时调用（见 BuildServer）。
// 这些工具能改数据、跑代码：run_script / save_script 依赖 scripts 权限，而 scripts 权限本身就等于任意代码执行。
func (t *toolset) registerWriteTools(s *mcp.Server) {
	addWriteTool(s, "run_task", "运行任务",
		"立即运行一个定时任务（与面板上点「运行」相同，禁用的任务也可以手动运行）。任务在后台执行，结果用 get_task_log 查看。",
		writeHints{}, t.runTask)
	addWriteTool(s, "stop_task", "停止任务",
		"停止一个正在运行或排队中的任务，本次执行记为「已终止」。",
		writeHints{idempotent: true}, t.stopTask)
	addWriteTool(s, "set_task_enabled", "启用或禁用任务",
		"打开或关闭任务的启用开关（禁用后不再按定时规则自动运行；正在运行的任务会在本次结束后生效）。",
		writeHints{idempotent: true}, t.setTaskEnabled)
	addWriteTool(s, "batch_task_action", "批量操作任务",
		"对多个任务执行同一操作：enable、disable、run、stop、pin、unpin、delete。delete 会删除任务及其执行记录（不删脚本文件），不可恢复。",
		writeHints{destructive: true}, t.batchTaskAction)
	addWriteTool(s, "create_env", "新建环境变量",
		"新建一条环境变量。同名变量允许多条（多账号场景），运行时按名称合并。",
		writeHints{}, t.createEnv)
	addWriteTool(s, "update_env", "修改环境变量",
		"按 ID 修改环境变量，只改传入的字段。",
		writeHints{destructive: true, idempotent: true}, t.updateEnv)
	addWriteTool(s, "delete_env", "删除环境变量",
		"按 ID 删除一条环境变量，不可恢复。",
		writeHints{destructive: true, idempotent: true}, t.deleteEnv)
	addWriteTool(s, "save_script", "保存脚本",
		"写入脚本文件：不存在则新建，存在则整体覆盖（面板会保留历史版本，可在网页端回滚）。",
		writeHints{destructive: true, idempotent: true}, t.saveScript)
	addWriteTool(s, "run_script", "运行脚本",
		"调试运行一个脚本并等待输出（最多约 50 秒）；到时仍在运行会返回 run_id，用同一个 run_id 再调用可继续取输出，加 stop: true 可停止。run_code 返回的 run_id 也可以在这里续取或停止。",
		writeHints{destructive: true, openWorld: true}, t.runScript)
	addWriteTool(s, "pull_subscription", "拉取订阅",
		"立即拉取一个订阅（按订阅设置同步脚本、增删任务）。拉取在后台进行，结果用 list_subscriptions 查看。",
		writeHints{destructive: true, openWorld: true}, t.pullSubscription)

	// #139 对齐开放 API 补上的写入工具，按领域分在各自的文件里。
	t.registerTaskWriteTools(s)
	t.registerScriptWriteTools(s)
	t.registerSubscriptionWriteTools(s)
	t.registerEnvBatchWriteTools(s)
	t.registerNotifyBackupWriteTools(s)
}

// ---- 任务 ------------------------------------------------------------------

type setTaskEnabledInput struct {
	ID      int64 `json:"id" jsonschema:"任务 ID"`
	Enabled bool  `json:"enabled" jsonschema:"true 启用，false 禁用"`
}

type batchTaskActionInput struct {
	IDs    []int64 `json:"ids" jsonschema:"任务 ID 列表（1 到 100 个）"`
	Action string  `json:"action" jsonschema:"操作：enable（启用）、disable（禁用）、run（运行）、stop（停止）、pin（置顶）、unpin（取消置顶）、delete（删除任务及其执行记录，不删脚本）"`
}

// batchTaskActions 沿用 PUT /tasks/batch 支持的动作；删除脚本（delete_scripts）刻意不开放。
var batchTaskActions = map[string]bool{
	"enable": true, "disable": true, "run": true, "stop": true,
	"pin": true, "unpin": true, "delete": true,
}

const maxBatchTaskIDs = 100

func (t *toolset) runTask(ctx context.Context, in taskIDInput) (any, error) {
	return t.taskAction(ctx, in.ID, "run")
}

func (t *toolset) stopTask(ctx context.Context, in taskIDInput) (any, error) {
	return t.taskAction(ctx, in.ID, "stop")
}

func (t *toolset) setTaskEnabled(ctx context.Context, in setTaskEnabledInput) (any, error) {
	action := "disable"
	if in.Enabled {
		action = "enable"
	}
	return t.taskAction(ctx, in.ID, action)
}

func (t *toolset) taskAction(ctx context.Context, id int64, action string) (any, error) {
	if err := requireID("id", id); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/tasks/%d/%s", id, action), nil, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"task_id": id, "message": result["message"]}
	if task, ok := result["data"].(map[string]any); ok {
		slim := pickFields(task, "id", "name")
		decorateTask(slim, task)
		out["task"] = slim
	}
	return out, nil
}

func (t *toolset) batchTaskAction(ctx context.Context, in batchTaskActionInput) (any, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if !batchTaskActions[action] {
		return nil, fmt.Errorf("action 只能是 enable、disable、run、stop、pin、unpin、delete，收到 %q", in.Action)
	}
	if len(in.IDs) == 0 || len(in.IDs) > maxBatchTaskIDs {
		return nil, fmt.Errorf("ids 需要 1 到 %d 个任务 ID", maxBatchTaskIDs)
	}
	for _, id := range in.IDs {
		if err := requireID("ids 中的任务 ID", id); err != nil {
			return nil, err
		}
	}
	result, err := t.call(ctx, http.MethodPut, "/tasks/batch", nil, map[string]any{"ids": in.IDs, "action": action})
	if err != nil {
		return nil, err
	}
	return pickFields(result, "message", "count"), nil
}

// ---- 环境变量 ----------------------------------------------------------------

type createEnvInput struct {
	Name    string `json:"name" jsonschema:"变量名（字母、数字、下划线）"`
	Value   string `json:"value" jsonschema:"变量值"`
	Remarks string `json:"remarks,omitempty" jsonschema:"备注"`
	Group   string `json:"group,omitempty" jsonschema:"分组（多个分组用英文逗号分隔）"`
}

type updateEnvInput struct {
	ID      int64   `json:"id" jsonschema:"环境变量 ID，可从 list_envs 获得"`
	Name    *string `json:"name,omitempty" jsonschema:"新的变量名"`
	Value   *string `json:"value,omitempty" jsonschema:"新的值"`
	Remarks *string `json:"remarks,omitempty" jsonschema:"新的备注"`
	Group   *string `json:"group,omitempty" jsonschema:"新的分组（多个分组用英文逗号分隔）"`
	Enabled *bool   `json:"enabled,omitempty" jsonschema:"启用（true）或禁用（false）"`
}

type envIDInput struct {
	ID int64 `json:"id" jsonschema:"环境变量 ID，可从 list_envs 获得"`
}

func (t *toolset) createEnv(ctx context.Context, in createEnvInput) (any, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("name 不能为空")
	}
	body := map[string]any{"name": name, "value": in.Value}
	if remarks := strings.TrimSpace(in.Remarks); remarks != "" {
		body["remarks"] = remarks
	}
	if group := strings.TrimSpace(in.Group); group != "" {
		body["group"] = group
	}
	result, err := t.call(ctx, http.MethodPost, "/envs", nil, body)
	if err != nil {
		return nil, err
	}
	// 单条创建失败时接口回 200 + errors（为了兼容批量写法），这里翻译成工具错误。
	if problems, ok := result["errors"].([]any); ok && len(problems) > 0 {
		messages := make([]string, 0, len(problems))
		for _, problem := range problems {
			messages = append(messages, stringValue(problem))
		}
		return nil, fmt.Errorf("创建失败：%s", strings.Join(messages, "；"))
	}
	out := map[string]any{"message": result["message"]}
	if env, ok := result["data"].(map[string]any); ok {
		out["env"] = slimEnv(env)
	}
	return out, nil
}

func (t *toolset) updateEnv(ctx context.Context, in updateEnvInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	body := map[string]any{}
	if in.Name != nil {
		body["name"] = *in.Name
	}
	if in.Value != nil {
		body["value"] = *in.Value
	}
	if in.Remarks != nil {
		body["remarks"] = *in.Remarks
	}
	if in.Group != nil {
		body["group"] = *in.Group
	}
	if in.Enabled != nil {
		body["enabled"] = *in.Enabled
	}
	if len(body) == 0 {
		return nil, errors.New("没有要修改的字段：name、value、remarks、group、enabled 至少传一个")
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/envs/%d", in.ID), nil, body)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"message": result["message"]}
	if env, ok := result["data"].(map[string]any); ok {
		out["env"] = slimEnv(env)
	}
	return out, nil
}

func (t *toolset) deleteEnv(ctx context.Context, in envIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	// 删除接口对不存在的 ID 也回成功；先查一次，AI 删错 ID 时能拿到明确的「不存在」。
	existing, err := t.call(ctx, http.MethodGet, fmt.Sprintf("/envs/%d", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodDelete, fmt.Sprintf("/envs/%d", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"message": result["message"], "id": in.ID}
	if name := stringValue(objectOf(existing)["name"]); name != "" {
		out["name"] = name
	}
	return out, nil
}

// ---- 脚本 --------------------------------------------------------------------

type saveScriptInput struct {
	Path    string `json:"path" jsonschema:"脚本相对路径（相对脚本目录），不存在会新建，存在会整体覆盖"`
	Content string `json:"content" jsonschema:"完整的文件内容"`
	Message string `json:"message,omitempty" jsonschema:"本次修改说明，记入脚本历史版本"`
}

type runScriptInput struct {
	Path        string `json:"path,omitempty" jsonschema:"要运行的脚本相对路径（相对脚本目录），与 run_id 二选一"`
	RunID       string `json:"run_id,omitempty" jsonschema:"之前调用返回的 run_id：继续等待那次运行并取最新输出，与 path 二选一"`
	Stop        bool   `json:"stop,omitempty" jsonschema:"与 run_id 一起使用：停止那次运行"`
	WaitSeconds int    `json:"wait_seconds,omitempty" jsonschema:"最多等待的秒数（1 到 50，默认 50），到时仍未结束会返回 run_id 与已有输出"`
}

const (
	defaultScriptWaitSeconds = 50
	maxScriptWaitSeconds     = 50
	stopScriptWaitSeconds    = 10
)

// 轮询间隔从 1 秒逐步放宽到 3 秒：每次轮询都是一次开放接口调用，会计入应用的调用频率限制。
// 最长等待取 50 秒而不是整 60 秒，给反代的默认 60 秒读超时留余量。测试里会调小这两个值。
var (
	scriptPollInitialDelay = time.Second
	scriptPollMaxDelay     = 3 * time.Second
)

func (t *toolset) saveScript(ctx context.Context, in saveScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errors.New("path 不能为空")
	}
	body := map[string]any{"path": path, "content": in.Content}
	if message := strings.TrimSpace(in.Message); message != "" {
		body["message"] = message
	}
	result, err := t.call(ctx, http.MethodPut, "/scripts/content", nil, body)
	if err != nil {
		return nil, err
	}
	out := pickFields(result, "message", "version")
	out["path"] = path
	return out, nil
}

func (t *toolset) runScript(ctx context.Context, in runScriptInput) (any, error) {
	path := strings.TrimSpace(in.Path)
	runID := strings.TrimSpace(in.RunID)
	if (path == "") == (runID == "") {
		return nil, errors.New("path 与 run_id 必须且只能填一个")
	}
	if in.Stop && runID == "" {
		return nil, errors.New("stop 需要和 run_id 一起使用")
	}

	wait := normalizeScriptWait(in.WaitSeconds)

	if runID == "" {
		started, err := t.call(ctx, http.MethodPost, "/scripts/run", nil, map[string]any{"path": path})
		if err != nil {
			return nil, err
		}
		runID = stringValue(started["run_id"])
		if runID == "" {
			return nil, errors.New("面板没有返回 run_id，无法跟踪这次运行")
		}
	} else if in.Stop {
		if _, err := t.call(ctx, http.MethodPut, "/scripts/run/"+url.PathEscape(runID)+"/stop", nil, nil); err != nil {
			return nil, err
		}
		if wait > stopScriptWaitSeconds {
			wait = stopScriptWaitSeconds
		}
	}
	return t.waitScriptRun(ctx, runID, wait)
}

func normalizeScriptWait(seconds int) int {
	if seconds <= 0 {
		return defaultScriptWaitSeconds
	}
	return min(seconds, maxScriptWaitSeconds)
}

// waitScriptRun 轮询一次调试运行，直到结束或等满 wait 秒。run_script 与 run_code 共用：
// 两者的运行记录在面板内存里是同一份（都经 /scripts/run/:run_id 读写），run_id 可以互相续上。
func (t *toolset) waitScriptRun(ctx context.Context, runID string, wait int) (any, error) {
	deadline := time.Now().Add(time.Duration(wait) * time.Second)
	delay := scriptPollInitialDelay
	for {
		snapshot, err := t.call(ctx, http.MethodGet, "/scripts/run/"+url.PathEscape(runID)+"/logs", nil, nil)
		if err != nil {
			return nil, err
		}
		data := objectOf(snapshot)
		if done, _ := data["done"].(bool); done {
			// 调试运行记录只存在面板内存里；面板只在下一次启动调试 / 运行代码时，清理结束超过 30 分钟、或超出 20 条的旧记录。
			// 取完结果顺手清掉，内存回收得更早。
			_, _, _ = t.d.Do(ctx, http.MethodDelete, "/scripts/run/"+url.PathEscape(runID), nil, nil)
			return scriptRunOutput(runID, data, true), nil
		}
		if !time.Now().Before(deadline) {
			return scriptRunOutput(runID, data, false), nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > scriptPollMaxDelay {
			delay = scriptPollMaxDelay
		}
	}
}

func scriptRunOutput(runID string, data map[string]any, done bool) map[string]any {
	lines, _ := data["logs"].([]any)
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, stringValue(line))
	}
	output := strings.Join(parts, "\n")
	text, cut := tailText(output, contentBudget)

	out := map[string]any{
		"run_id":    runID,
		"done":      done,
		"status":    data["status"],
		"exit_code": data["exit_code"],
		"output":    text,
	}
	if cut {
		out["output_truncated"] = fmt.Sprintf("输出共 %d 字节，只保留了末尾 %d 字节", len(output), len(text))
	}
	if !done {
		out["hint"] = "脚本仍在运行：稍后用同一个 run_id 再调用 run_script 可继续获取输出，加 stop: true 可停止"
	}
	return out
}

// ---- 订阅 --------------------------------------------------------------------

type subscriptionIDInput struct {
	ID int64 `json:"id" jsonschema:"订阅 ID，可从 list_subscriptions 获得"`
}

func (t *toolset) pullSubscription(ctx context.Context, in subscriptionIDInput) (any, error) {
	if err := requireID("id", in.ID); err != nil {
		return nil, err
	}
	result, err := t.call(ctx, http.MethodPut, fmt.Sprintf("/subscriptions/%d/pull", in.ID), nil, nil)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"subscription_id": in.ID,
		"message":         result["message"],
		"hint":            "拉取在后台进行，稍后用 list_subscriptions 查看 last_pull_at 与状态",
	}, nil
}
