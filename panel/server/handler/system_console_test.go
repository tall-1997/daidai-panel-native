package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 这一组用例守住网页版系统命令行的三道护栏：超时、admin-only、审计留痕之外的可观测行为。
// 命令行等价于「在容器里开一个 root shell」，任何一道护栏塌了都不会有报错，
// 只会安静地把面板变成一个谁都能用的远程执行入口，所以必须钉死。

// newConsoleTestEngine 起一个只挂了 ConsoleHandler 的引擎，避免受其它 handler 的路由影响。
func newConsoleTestEngine(t *testing.T) *gin.Engine {
	t.Helper()

	engine := gin.New()
	NewConsoleHandler().RegisterRoutes(engine.Group("/api/v1"))
	return engine
}

func consoleRequest(t *testing.T, engine *gin.Engine, method, path, token string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func decodeConsoleJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()

	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return payload
}

// requireBash 在没有 bash 的机器（例如 Windows 开发机）上跳过真正起进程的用例。
// 面板的 Docker 镜像与模块版都自带 bash，CI 上这些用例照跑。
func requireBash(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("当前环境没有 bash，跳过需要真正起进程的命令行用例")
	}
}

// mustStartConsoleCommand 起一条命令并返回 run_id。
func mustStartConsoleCommand(t *testing.T, engine *gin.Engine, token, command string) string {
	t.Helper()

	rec := consoleRequest(t, engine, http.MethodPost, "/api/v1/system/console/run", token, map[string]string{"command": command})
	if rec.Code != http.StatusCreated {
		t.Fatalf("启动命令应返回 201，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	runID, _ := decodeConsoleJSON(t, rec)["run_id"].(string)
	if runID == "" {
		t.Fatalf("响应里缺少 run_id，body=%s", rec.Body.String())
	}
	return runID
}

// fetchConsoleLogs 拉一次日志快照，返回 DebugLogs 同形结构里的 data 对象。
func fetchConsoleLogs(t *testing.T, engine *gin.Engine, token, runID string) map[string]interface{} {
	t.Helper()

	rec := consoleRequest(t, engine, http.MethodGet, "/api/v1/system/console/run/"+runID+"/logs", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("拉日志应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	data, ok := decodeConsoleJSON(t, rec)["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("日志响应缺少 data 对象，body=%s", rec.Body.String())
	}
	return data
}

// awaitConsoleRunDone 轮询到命令结束为止。
func awaitConsoleRunDone(t *testing.T, engine *gin.Engine, token, runID string) map[string]interface{} {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data := fetchConsoleLogs(t, engine, token, runID)
		if done, _ := data["done"].(bool); done {
			return data
		}
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("命令 %s 在 30 秒内没有结束", runID)
	return nil
}

func consoleLogText(data map[string]interface{}) string {
	lines, _ := data["logs"].([]interface{})
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if text, ok := line.(string); ok {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

// mustAdminToken 造一个管理员令牌。
func mustConsoleAdminToken(t *testing.T, username string) string {
	t.Helper()

	admin := testutil.MustCreateUser(t, username, "admin")
	return testutil.MustCreateAccessToken(t, admin.Username, admin.Role)
}

// 空命令与超长命令都必须在起进程之前被挡掉。
// 这两条校验刻意排在 bash 探测之前，所以在没有 bash 的机器上也能跑。
func TestConsoleRunRejectsEmptyAndOversizedCommand(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newConsoleTestEngine(t)
	token := mustConsoleAdminToken(t, "console-validate-admin")

	empty := consoleRequest(t, engine, http.MethodPost, "/api/v1/system/console/run", token, map[string]string{"command": "   \n\t "})
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("空命令应返回 400，实际 %d，body=%s", empty.Code, empty.Body.String())
	}

	oversized := consoleRequest(t, engine, http.MethodPost, "/api/v1/system/console/run", token, map[string]string{
		"command": strings.Repeat("a", maxConsoleCommandBytes+1),
	})
	if oversized.Code != http.StatusBadRequest {
		t.Fatalf("超长命令应返回 400，实际 %d，body=%s", oversized.Code, oversized.Body.String())
	}

	// 刚好卡在上限上的命令不该被这条校验误伤（没 bash 的机器上会被下一道 bash 探测拦下，
	// 那时返回的也是 400，所以只在有 bash 时断言放行）。
	if _, err := exec.LookPath("bash"); err == nil {
		atLimit := consoleRequest(t, engine, http.MethodPost, "/api/v1/system/console/run", token, map[string]string{
			"command": "echo " + strings.Repeat("a", maxConsoleCommandBytes-len("echo ")),
		})
		if atLimit.Code != http.StatusCreated {
			t.Fatalf("恰好等于上限的命令应放行，实际 %d，body=%s", atLimit.Code, atLimit.Body.String())
		}
		// ⚠️ 必须等它跑完再返回。命令的工作目录就是 SetupTestEnv 建的临时脚本目录，
		// Windows 上「某个进程正把这个目录当 cwd」会让 t.TempDir() 的清理直接失败
		// （unlinkat ...\data\scripts: 被另一进程占用），表现为这条用例随机变红。
		runID, _ := decodeConsoleJSON(t, atLimit)["run_id"].(string)
		if runID == "" {
			t.Fatalf("响应里缺少 run_id，body=%s", atLimit.Body.String())
		}
		awaitConsoleRunDone(t, engine, token, runID)
	}
}

// 正常命令：能起来、能轮询到输出、退出码正确。
func TestConsoleRunExecutesCommandAndReportsExitCode(t *testing.T) {
	requireBash(t)
	testutil.SetupTestEnv(t)

	engine := newConsoleTestEngine(t)
	token := mustConsoleAdminToken(t, "console-run-admin")

	successID := mustStartConsoleCommand(t, engine, token, "echo daidai-console-ok")
	success := awaitConsoleRunDone(t, engine, token, successID)
	if status, _ := success["status"].(string); status != "success" {
		t.Fatalf("正常命令的状态应为 success，实际 %v，logs=%s", success["status"], consoleLogText(success))
	}
	if exitCode, _ := success["exit_code"].(float64); exitCode != 0 {
		t.Fatalf("正常命令的退出码应为 0，实际 %v", success["exit_code"])
	}
	if !strings.Contains(consoleLogText(success), "daidai-console-ok") {
		t.Fatalf("日志里应能看到命令输出，实际=%s", consoleLogText(success))
	}

	failedID := mustStartConsoleCommand(t, engine, token, "exit 7")
	failed := awaitConsoleRunDone(t, engine, token, failedID)
	if status, _ := failed["status"].(string); status != "failed" {
		t.Fatalf("失败命令的状态应为 failed，实际 %v，logs=%s", failed["status"], consoleLogText(failed))
	}
	if exitCode, _ := failed["exit_code"].(float64); exitCode != 7 {
		t.Fatalf("失败命令的退出码应原样透出 7，实际 %v", failed["exit_code"])
	}
}

// 停止：状态要变成 stopped，收尾文案必须是命令行的措辞。
// 直接复用 debugRun.stop() 会写「[调试运行已停止]」，用户在命令行里看到会以为自己在调试脚本。
func TestConsoleStopUsesConsoleWording(t *testing.T) {
	requireBash(t)
	testutil.SetupTestEnv(t)

	engine := newConsoleTestEngine(t)
	token := mustConsoleAdminToken(t, "console-stop-admin")

	runID := mustStartConsoleCommand(t, engine, token, "sleep 30")

	stop := consoleRequest(t, engine, http.MethodPut, "/api/v1/system/console/run/"+runID+"/stop", token, nil)
	if stop.Code != http.StatusOK {
		t.Fatalf("停止应返回 200，实际 %d，body=%s", stop.Code, stop.Body.String())
	}

	data := fetchConsoleLogs(t, engine, token, runID)
	if status, _ := data["status"].(string); status != "stopped" {
		t.Fatalf("停止后的状态应为 stopped，实际 %v", data["status"])
	}
	if done, _ := data["done"].(bool); !done {
		t.Fatalf("停止后 done 应为 true，实际 %v", data["done"])
	}

	text := consoleLogText(data)
	if !strings.Contains(text, "[命令已停止]") {
		t.Fatalf("收尾日志应是命令行的措辞，实际=%s", text)
	}
	if strings.Contains(text, "调试运行") {
		t.Fatalf("命令行的收尾日志里不许出现「调试运行」，实际=%s", text)
	}

	// 停止之后那条后台 goroutine 还会跑一遍收尾，不能反过来把 stopped 覆盖成 failed。
	time.Sleep(300 * time.Millisecond)
	after := fetchConsoleLogs(t, engine, token, runID)
	if status, _ := after["status"].(string); status != "stopped" {
		t.Fatalf("收尾 goroutine 不得覆盖已停止的状态，实际 %v", after["status"])
	}
}

// newConsoleTestRun 造一条「已结束」或「仍在跑」的运行记录，用来验证注册表的淘汰策略。
func newConsoleTestRun(done bool) *debugRun {
	run := newDebugRun()
	if done {
		exitCode := 0
		run.Done = true
		run.Status = "success"
		run.ExitCode = &exitCode
	}
	return run
}

func consoleRunCount(h *ConsoleHandler) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.runs)
}

// 注册表必须有上限：命令输出全在内存里，长期运行的面板不设上限会一直堆到 OOM。
func TestConsoleRunRegistryEvictsOldestFinishedRuns(t *testing.T) {
	h := NewConsoleHandler()

	finished := make([]string, 0, 45)
	for i := 0; i < 45; i++ {
		finished = append(finished, h.storeConsoleRun(newConsoleTestRun(true)))
	}
	running := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		running = append(running, h.storeConsoleRun(newConsoleTestRun(false)))
	}
	if got := consoleRunCount(h); got != maxConsoleRuns {
		t.Fatalf("刚好塞满时不该淘汰任何记录，期望 %d 条，实际 %d 条", maxConsoleRuns, got)
	}

	extra := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		extra = append(extra, h.storeConsoleRun(newConsoleTestRun(true)))
	}

	if got := consoleRunCount(h); got != maxConsoleRuns {
		t.Fatalf("超出上限后应被压回 %d 条，实际 %d 条", maxConsoleRuns, got)
	}
	if _, exists := h.loadConsoleRun(finished[0]); exists {
		t.Fatalf("最老的已结束记录 %s 应当被淘汰", finished[0])
	}
	for _, runID := range running {
		if _, exists := h.loadConsoleRun(runID); !exists {
			t.Fatalf("仍在运行的记录 %s 不许被淘汰——用户正盯着它的输出，进程也会失去停止入口", runID)
		}
	}
	if _, exists := h.loadConsoleRun(extra[len(extra)-1]); !exists {
		t.Fatalf("最新的记录 %s 必须还在", extra[len(extra)-1])
	}
}

// 一条都淘汰不掉时（全都在跑）宁可暂时超限，也不能删掉正在运行的记录，更不能死循环。
func TestConsoleRunRegistryKeepsEverythingWhenAllRunsAreBusy(t *testing.T) {
	h := NewConsoleHandler()

	total := maxConsoleRuns + 3
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		ids = append(ids, h.storeConsoleRun(newConsoleTestRun(false)))
	}

	if got := consoleRunCount(h); got != total {
		t.Fatalf("全部在跑时应保留全部 %d 条，实际 %d 条", total, got)
	}
	for _, runID := range ids {
		if _, exists := h.loadConsoleRun(runID); !exists {
			t.Fatalf("运行中的记录 %s 丢了", runID)
		}
	}
}

// setConsoleTimeoutConfigRaw 绕过 SetConfig 的区间校验直接写库，
// 模拟「数据库里存着历史越界值」——这正是 resolveConsoleTimeout 那道兜底要防的场景。
func setConsoleTimeoutConfigRaw(t *testing.T, value string) {
	t.Helper()

	result := database.DB.Model(&model.SystemConfig{}).
		Where("`key` = ?", "console_timeout_minutes").
		Update("value", value)
	if result.Error != nil {
		t.Fatalf("update console_timeout_minutes: %v", result.Error)
	}
	if result.RowsAffected == 0 {
		if err := database.DB.Create(&model.SystemConfig{Key: "console_timeout_minutes", Value: value}).Error; err != nil {
			t.Fatalf("create console_timeout_minutes: %v", err)
		}
	}
}

func TestResolveConsoleTimeoutFallsBackOnOutOfRangeConfig(t *testing.T) {
	testutil.SetupTestEnv(t)

	if got := resolveConsoleTimeout(); got != defaultConsoleTimeout {
		t.Fatalf("未改过配置时应取注册表默认值 %v，实际 %v", defaultConsoleTimeout, got)
	}

	cases := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "合法值原样生效", value: "45", want: 45 * time.Minute},
		{name: "下界外回退默认值", value: "0", want: defaultConsoleTimeout},
		{name: "上界外回退默认值", value: "5000", want: defaultConsoleTimeout},
		{name: "非法内容回退默认值", value: "abc", want: defaultConsoleTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setConsoleTimeoutConfigRaw(t, tc.value)
			if got := resolveConsoleTimeout(); got != tc.want {
				t.Fatalf("配置为 %q 时期望超时 %v，实际 %v", tc.value, tc.want, got)
			}
		})
	}
}

// 权限：命令行是 admin-only，且刻意没挂 OpenAPIAccess——应用令牌一律进不来。
func TestConsoleRoutesAreAdminOnlyAndRejectAppTokens(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newConsoleTestEngine(t)

	admin := testutil.MustCreateUser(t, "console-perm-admin", "admin")
	operator := testutil.MustCreateUser(t, "console-perm-operator", "operator")
	viewer := testutil.MustCreateUser(t, "console-perm-viewer", "viewer")

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{
			name:  "管理员放行",
			token: testutil.MustCreateAccessToken(t, admin.Username, admin.Role),
			want:  http.StatusOK,
		},
		{
			name:  "operator 拒绝",
			token: testutil.MustCreateAccessToken(t, operator.Username, operator.Role),
			want:  http.StatusForbidden,
		},
		{
			name:  "viewer 拒绝",
			token: testutil.MustCreateAccessToken(t, viewer.Username, viewer.Role),
			want:  http.StatusForbidden,
		},
		{
			// 这条是本次改动的核心：/scripts/run-code 那条老路挂着 OpenAPIAccess("scripts")，
			// 应用令牌就能跑任意 bash。命令行绝不能重蹈覆辙。
			name:  "带 system 授权域的应用令牌也拒绝",
			token: testutil.MustCreateAppToken(t, "console-perm-app", "system"),
			want:  http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := consoleRequest(t, engine, http.MethodGet, "/api/v1/system/console/meta", tc.token, nil)
			if rec.Code != tc.want {
				t.Fatalf("期望状态码 %d，实际 %d，body=%s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}

	// 没有令牌时是 401，不能因为路由拼错而变成 404。
	anonymous := consoleRequest(t, engine, http.MethodGet, "/api/v1/system/console/meta", "", nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("匿名访问应返回 401，实际 %d，body=%s", anonymous.Code, anonymous.Body.String())
	}
}

// meta 是前端渲染表头与禁用输入框的唯一依据，字段名与超时值都要稳。
func TestConsoleMetaExposesEnvironmentContract(t *testing.T) {
	testutil.SetupTestEnv(t)

	setConsoleTimeoutConfigRaw(t, "45")

	engine := newConsoleTestEngine(t)
	token := mustConsoleAdminToken(t, "console-meta-admin")

	rec := consoleRequest(t, engine, http.MethodGet, "/api/v1/system/console/meta", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("meta 应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeConsoleJSON(t, rec)
	for _, key := range []string{
		"available", "unavailable_reason", "shell", "work_dir",
		"is_root", "package_manager", "distribution", "timeout_minutes",
	} {
		if _, exists := payload[key]; !exists {
			t.Fatalf("meta 缺少字段 %s，body=%s", key, rec.Body.String())
		}
	}

	if shell, _ := payload["shell"].(string); shell != "bash" {
		t.Fatalf("meta.shell 应为 bash，实际 %v", payload["shell"])
	}
	if minutes, _ := payload["timeout_minutes"].(float64); minutes != 45 {
		t.Fatalf("meta.timeout_minutes 应跟随配置返回 45，实际 %v", payload["timeout_minutes"])
	}

	// bash 不可用时必须给出中文原因，前端据此禁用输入框；可用时该字段必须是空串。
	available, _ := payload["available"].(bool)
	reason, _ := payload["unavailable_reason"].(string)
	if available && reason != "" {
		t.Fatalf("bash 可用时 unavailable_reason 应为空，实际 %q", reason)
	}
	if !available && strings.TrimSpace(reason) == "" {
		t.Fatalf("bash 不可用时必须给出原因，否则前端只会显示一个没有解释的禁用输入框")
	}
}

// 命令里有后台进程（`nohup python3 bot.py &`）时，bash 毫秒级就以 0 退出，
// 但孙进程继承了 stdout/stderr 那对管道，cmd.Wait() 会一直等到孙进程结束。
// WaitDelay 负责在 2 秒后强制收管道，这条用例钉住随之而来的结算逻辑。
//
// 之所以对着纯函数测而不是起真进程：WaitDelay 只在 Linux 上有意义，
// Windows 开发机上 bash 多半解析到 WSL，转发器不会把 Windows 的管道句柄传给 Linux 侧进程，
// 这个场景在开发机上根本复现不出来。
func TestResolveConsoleRunOutcomeHandlesWaitDelayAndTimeoutKill(t *testing.T) {
	wrappedWaitDelay := fmt.Errorf("收管道超时: %w", exec.ErrWaitDelay)
	boom := errors.New("boom")

	cases := []struct {
		name            string
		waitErr         error
		processExitCode int
		timedOut        bool
		killIssued      bool
		wantExitCode    int
		wantErr         error
		wantTimedOut    bool
		wantBackground  bool
	}{
		{
			// 不认这条分支的话，resolveExitCode 会把 ErrWaitDelay 当成 1，
			// 于是每一条 `nohup ... &` 都被误报成 failed。
			name:            "ErrWaitDelay 按命令本体的真实退出码结算",
			waitErr:         exec.ErrWaitDelay,
			processExitCode: 0,
			wantExitCode:    0,
			wantErr:         nil,
			wantBackground:  true,
		},
		{
			name:            "被包了一层的 ErrWaitDelay 同样命中",
			waitErr:         wrappedWaitDelay,
			processExitCode: 3,
			wantExitCode:    3,
			wantErr:         nil,
			wantBackground:  true,
		},
		{
			name:         "普通错误仍走 resolveExitCode",
			waitErr:      boom,
			wantExitCode: 1,
			wantErr:      boom,
		},
		{
			// 核心回归：kill 已经下出去了，就不许再拿 bash 那次的 0 抹掉超时标记，
			// 否则一条被整组杀掉的运行会被写成 status=success、退出码 0、耗时 1800 秒。
			name:         "已下过 kill 时退出码 0 也不许抹掉超时",
			waitErr:      nil,
			timedOut:     true,
			killIssued:   true,
			wantTimedOut: true,
			wantExitCode: -1,
		},
		{
			// ⚠️ 这一条钉的是纯函数契约，不是当前接线下真实可达的路径。
			// Run 里 timedOut=true 与 killIssued=run.killIfRunning() 写在同一条分支里，
			// 而那一刻 run.Done 必然还是 false（只有 finish / stop / stopConsoleRun 会置位，
			// 进程自然退出不会），所以现实中进到超时分支就一定下过刀、killIssued 恒为 true。
			// 留着它是为了守住「压根没下过 kill 就不算超时」这条豁免语义：
			// 将来有人把 kill 与超时标记拆开（例如改成先标记再异步杀），这条会第一时间说话。
			name:         "没下过 kill 且退出码为 0：按抢在定时器前面跑完处理（纯函数契约，当前接线不可达）",
			waitErr:      nil,
			timedOut:     true,
			killIssued:   false,
			wantTimedOut: false,
			wantExitCode: 0,
		},
		{
			name:         "超时且进程被信号杀掉：退出码原样透出",
			waitErr:      boom,
			timedOut:     true,
			killIssued:   true,
			wantTimedOut: true,
			wantExitCode: 1,
			wantErr:      boom,
		},
		{
			name:            "超时又赶上 WaitDelay：仍按超时失败结算",
			waitErr:         exec.ErrWaitDelay,
			processExitCode: 0,
			timedOut:        true,
			killIssued:      true,
			wantTimedOut:    true,
			wantExitCode:    -1,
			wantBackground:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveConsoleRunOutcome(tc.waitErr, tc.processExitCode, tc.timedOut, tc.killIssued)
			if got.ExitCode != tc.wantExitCode {
				t.Fatalf("退出码期望 %d，实际 %d", tc.wantExitCode, got.ExitCode)
			}
			if !errors.Is(got.WaitErr, tc.wantErr) {
				t.Fatalf("错误期望 %v，实际 %v", tc.wantErr, got.WaitErr)
			}
			if got.TimedOut != tc.wantTimedOut {
				t.Fatalf("超时标记期望 %v，实际 %v", tc.wantTimedOut, got.TimedOut)
			}
			if got.BackgroundHeld != tc.wantBackground {
				t.Fatalf("后台持管道标记期望 %v，实际 %v", tc.wantBackground, got.BackgroundHeld)
			}
			// 超时一定要带着非 0 退出码进 finish，否则状态会被判成 success，
			// 出现「日志写着超时被终止、状态却是成功」的自相矛盾。
			if got.TimedOut && got.ExitCode == 0 {
				t.Fatal("超时结算不许留下 0 退出码：finish 会据此把状态记成 success")
			}
		})
	}
}

// 单行输出超过 1MB 时 scanner.Scan() 直接返回 false，如果不排空 reader，
// exec 内部往 io.Pipe 写侧灌数据的 io.Copy 会永久阻塞，cmd.Wait() 卡死在 awaitGoroutines——
// 阻塞点在父进程里，超时 kill 整个进程组也救不回来。
// 触发命令很日常：`base64 -w0 某个db`、`cat 压成一行的 min.js`、`jq -c . package-lock.json`。
func TestCollectRunLogsDrainsReaderAfterOverlongLine(t *testing.T) {
	run := newDebugRun()
	reader, writer := io.Pipe()
	done := collectRunLogs(reader, run)

	writeErr := make(chan error, 1)
	go func() {
		// 单行 2MB，超过 scanner 的 1MB 上限。
		if _, err := writer.Write([]byte(strings.Repeat("x", 2*1024*1024))); err != nil {
			writeErr <- err
			return
		}
		// 再灌一批：没人排空 reader 的话，这里会和 exec 的 copy goroutine 一样永久阻塞。
		if _, err := writer.Write([]byte(strings.Repeat("y", 1024*1024) + "\n")); err != nil {
			writeErr <- err
			return
		}
		writeErr <- writer.Close()
	}()

	select {
	case err := <-writeErr:
		if err != nil {
			t.Fatalf("写侧不该报错: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("写侧被永久阻塞：collectRunLogs 在单行超长之后没有排空 reader，真实场景里这会让 cmd.Wait() 永久卡死")
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("collectRunLogs 没有退出")
	}

	logs, _, _, _ := run.snapshot()
	if !strings.Contains(strings.Join(logs, "\n"), "输出行过长") {
		t.Fatalf("必须给用户留下截断提示，实际日志=%v", logs)
	}
}

// 单次运行的输出必须有上限：实测 WSL 里跑 2 秒 `yes` 就是 2500 万行、1.3GB 堆内存，
// 而默认超时是 30 分钟——容器几秒内会被 OOM Killer 干掉，整个面板（含所有定时任务）一起没。
func TestDebugRunLogsAreTrimmedInBlocksWithGlobalOffsets(t *testing.T) {
	run := newDebugRun()

	for i := 0; i < maxRunLogLines; i++ {
		run.appendLog(fmt.Sprintf("line-%d", i))
	}
	if got := len(run.Logs); got != maxRunLogLines {
		t.Fatalf("没到上限就不该丢日志，期望 %d 行，实际 %d 行", maxRunLogLines, got)
	}

	// 自动装依赖那套增量匹配的锚点：logOffset = run.logLen()。
	mark := run.logLen()
	if mark != maxRunLogLines {
		t.Fatalf("logLen 应等于已追加的行数 %d，实际 %d", maxRunLogLines, mark)
	}

	run.appendLog("after-mark")

	// ① 必须成块丢、让 len(Logs) 真的变小：逐行淘汰会让头部锚点每来一行就动一次，
	//    前端每一轮轮询都得整份重灌，成块丢才能几万行才发生一次。
	wantLen := maxRunLogLines - runLogTrimBatch + 2
	if got := len(run.Logs); got != wantLen {
		t.Fatalf("超限后应成块丢弃，期望 %d 行，实际 %d 行", wantLen, got)
	}

	// ② 头部要有省略提示，并且省略行数要对得上。
	wantHead := fmt.Sprintf("[前 %d 行输出已省略：单次运行最多保留 %d 行日志]", runLogTrimBatch, maxRunLogLines)
	if run.Logs[0] != wantHead {
		t.Fatalf("头部省略提示期望 %q，实际 %q", wantHead, run.Logs[0])
	}
	if want := fmt.Sprintf("line-%d", runLogTrimBatch); run.Logs[1] != want {
		t.Fatalf("提示行之后应紧接着现存最早一行 %q，实际 %q", want, run.Logs[1])
	}

	// ③ 全局序号只增不减：截断不能让水位倒退。
	if got := run.logLen(); got != maxRunLogLines+1 {
		t.Fatalf("logLen 应返回只增不减的全局序号 %d，实际 %d", maxRunLogLines+1, got)
	}

	// ④ logOutputSince 必须按全局序号解释 offset。若还按切片下标算，
	//    RunCode / DebugRun 的依赖探测要么重读旧日志、要么漏掉新日志。
	if got := run.logOutputSince(mark); got != "after-mark" {
		t.Fatalf("logOutputSince 应只返回锚点之后的新日志，期望 %q，实际 %q", "after-mark", got)
	}
	if got := run.logOutputSince(run.logLen()); got != "" {
		t.Fatalf("取当前水位之后的日志应为空，实际 %q", got)
	}

	// ⑤ 请求一个已经被丢掉的位置不该 panic，退化成「从现存最早一行开始」。
	if got := run.logOutputSince(0); !strings.HasPrefix(got, wantHead) {
		t.Fatal("请求已被丢弃的位置时应从现存最早一行开始")
	}

	// ⑥ 收尾之后全局序号仍然有效：finish 只写状态，不能把丢弃计数清零，
	//    否则收尾那一刻 logOutputSince 的锚点会整体错位。
	run.finish(0, nil, 0)
	if got := run.logOutputSince(mark); !strings.Contains(got, "after-mark") {
		t.Fatalf("finish 之后按全局序号取增量日志仍应有效，实际 %q", got)
	}
}

// 命令行的日志接口必须把「已丢弃行数」这个全局锚点一并下发。
//
// 前端是【增量追加】渲染的：没有锚点它只能拿数组长度猜下标，而「两次轮询之间既发生了
// 截断、又新增了更多行」时 logs.length 反而会大于已渲染行数，于是既不触发重灌、
// 切片下标又在整体左移过的新数组里指向完全不同的全局行 —— 实测每命中一次就静默吞掉
// 近 5 万行，且头部那行「[前 N 行输出已省略：…]」永远不会被渲染出来，
// 用户看到的是输出凭空跳号、稳态下甚至表现成「卡死不动」。
func TestConsoleLogsExposeDiscardedAnchor(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 这条用例要往注册表里塞自造的运行记录，所以不能用 newConsoleTestEngine（它不返回 handler）
	consoleHandler := NewConsoleHandler()
	engine := gin.New()
	consoleHandler.RegisterRoutes(engine.Group("/api/v1"))
	token := mustConsoleAdminToken(t, "console-discard-admin")

	// 刻意不起真进程：要灌满 20 万行才触发截断，直接把造好的运行记录塞进注册表更稳也更快。
	run := newDebugRun()
	runID := consoleHandler.storeConsoleRun(run)

	// ① 还没触顶时锚点必须是 0，而且字段要真的在（前端对缺字段按 0 兜底，
	//    但那样就永远发现不了服务端漏发）。
	run.appendLog("line-0")
	data := fetchConsoleLogs(t, engine, token, runID)
	discarded, ok := data["discarded"].(float64)
	if !ok {
		t.Fatalf("日志响应必须带 discarded 锚点，实际 data=%v", data)
	}
	if discarded != 0 {
		t.Fatalf("没截断过时 discarded 应为 0，实际 %v", discarded)
	}

	// ② 触发一次成块截断，锚点必须等于 logs[0] 的真实全局行号。
	for i := 1; i <= maxRunLogLines; i++ {
		run.appendLog(fmt.Sprintf("line-%d", i))
	}
	data = fetchConsoleLogs(t, engine, token, runID)
	discarded, _ = data["discarded"].(float64)
	if discarded <= 0 {
		t.Fatalf("成块截断之后 discarded 应大于 0，实际 %v", discarded)
	}

	logs, _ := data["logs"].([]interface{})
	if len(logs) == 0 {
		t.Fatal("截断之后 logs 不该为空")
	}
	// discarded + len(logs) 就是前端下一轮要用的全局水位，必须与服务端的 logLen 完全对齐，
	// 差一行都会让下一轮的切片下标整体错位。
	if want := run.logLen(); int(discarded)+len(logs) != want {
		t.Fatalf("discarded + len(logs) 应等于全局水位 %d，实际 %d + %d", want, int(discarded), len(logs))
	}
	if head, _ := logs[0].(string); !strings.Contains(head, "行输出已省略") {
		t.Fatalf("截断后 logs[0] 应是省略提示（前端重灌时靠它当断层提示），实际 %q", head)
	}
}

// 清除记录之后不能再被查到，同一个 run_id 的日志接口必须回 404。
func TestConsoleClearRemovesRunRecord(t *testing.T) {
	requireBash(t)
	testutil.SetupTestEnv(t)

	engine := newConsoleTestEngine(t)
	token := mustConsoleAdminToken(t, "console-clear-admin")

	runID := mustStartConsoleCommand(t, engine, token, "echo daidai-console-clear")
	awaitConsoleRunDone(t, engine, token, runID)

	clear := consoleRequest(t, engine, http.MethodDelete, "/api/v1/system/console/run/"+runID, token, nil)
	if clear.Code != http.StatusOK {
		t.Fatalf("清除应返回 200，实际 %d，body=%s", clear.Code, clear.Body.String())
	}

	logs := consoleRequest(t, engine, http.MethodGet, "/api/v1/system/console/run/"+runID+"/logs", token, nil)
	if logs.Code != http.StatusNotFound {
		t.Fatalf("清除后再查日志应返回 404，实际 %d，body=%s", logs.Code, logs.Body.String())
	}
}
