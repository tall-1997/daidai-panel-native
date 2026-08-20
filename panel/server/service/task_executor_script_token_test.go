package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// stubRunCommandWithPlan 替换子进程执行入口，让用例可以稳定构造
// “执行中 / 执行崩溃 / 执行成功” 三种状态，而不依赖机器上是否装了 node/python。
func stubRunCommandWithPlan(t *testing.T, fn func(envVars map[string]string) (*ScriptResult, error)) {
	t.Helper()

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		result, err := fn(envVars)
		return result, nil, err
	}
	t.Cleanup(func() {
		runCommandWithPlanFunc = previous
	})
}

func copyScriptEnv(envVars map[string]string) map[string]string {
	copied := make(map[string]string, len(envVars))
	for key, value := range envVars {
		copied[key] = value
	}
	return copied
}

// runTaskForScriptTokenTest 跑一次最小任务，走的是真实的 runTask 路径。
func runTaskForScriptTokenTest(t *testing.T, timeout int) {
	t.Helper()

	task := &model.Task{
		Name:     "script-token-probe",
		Command:  "node probe.js",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusRunning,
		Timeout:  timeout,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	runningStatus := model.LogStatusRunning
	taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}

	plan := &CommandExecutionPlan{
		Interpreter: "node",
		FullPath:    filepath.Join(config.C.Data.ScriptsDir, "probe.js"),
		Mode:        commandModeNormal,
	}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan}

	NewTaskExecutor().runTask(req, taskLog, nil)
}

func mustParseScriptToken(t *testing.T, envVars map[string]string) *middleware.Claims {
	t.Helper()

	token := envVars["DAIDAI_TOKEN"]
	if token == "" {
		t.Fatalf("expected DAIDAI_TOKEN in task env, got %#v", envVars)
	}
	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse injected script token: %v", err)
	}
	if claims.ID == "" {
		t.Fatalf("expected jti on injected script token, got %#v", claims)
	}
	return claims
}

// 守卫 F3：task.Timeout 的 DB 默认值就是 0，历史实现在这条分支上签发了整整一年期的
// operator token。上界必须显著收窄，否则「面板被 kill -9」时凭据会长期游荡。
func TestUntimedTaskScriptTokenTTLIsBounded(t *testing.T) {
	const oneYear = 365 * 24 * time.Hour

	if UntimedTaskScriptTokenTTL <= 0 {
		t.Fatalf("expected positive fallback TTL, got %v", UntimedTaskScriptTokenTTL)
	}
	if UntimedTaskScriptTokenTTL > 7*24*time.Hour {
		t.Fatalf("expected fallback TTL to stay within 7 days, got %v", UntimedTaskScriptTokenTTL)
	}
	if oneYear/UntimedTaskScriptTokenTTL < 50 {
		t.Fatalf("expected fallback TTL to be far below a year, got %v", UntimedTaskScriptTokenTTL)
	}
}

func TestRunTaskWithoutTimeoutIssuesShortLivedScriptToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	var envVars map[string]string
	stubRunCommandWithPlan(t, func(taskEnv map[string]string) (*ScriptResult, error) {
		envVars = copyScriptEnv(taskEnv)
		return &ScriptResult{ReturnCode: 0}, nil
	})

	before := time.Now()
	runTaskForScriptTokenTest(t, 0)
	claims := mustParseScriptToken(t, envVars)

	lifetime := claims.ExpiresAt.Time.Sub(before)
	if lifetime > UntimedTaskScriptTokenTTL+time.Minute {
		t.Fatalf("expected untimed task token to live at most %v, got %v", UntimedTaskScriptTokenTTL, lifetime)
	}
	if lifetime < UntimedTaskScriptTokenTTL-time.Minute {
		t.Fatalf("expected untimed task token to live about %v, got %v", UntimedTaskScriptTokenTTL, lifetime)
	}
}

// timeout > 0 是用户显式声明的运行时长，不该被兜底上限截断。
func TestRunTaskWithTimeoutKeepsTimeoutPlusOneHourScriptToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	var envVars map[string]string
	stubRunCommandWithPlan(t, func(taskEnv map[string]string) (*ScriptResult, error) {
		envVars = copyScriptEnv(taskEnv)
		return &ScriptResult{ReturnCode: 0}, nil
	})

	const timeoutSeconds = 7200

	before := time.Now()
	runTaskForScriptTokenTest(t, timeoutSeconds)
	claims := mustParseScriptToken(t, envVars)

	want := timeoutSeconds*time.Second + time.Hour
	lifetime := claims.ExpiresAt.Time.Sub(before)
	if lifetime > want+time.Minute || lifetime < want-time.Minute {
		t.Fatalf("expected token lifetime about %v, got %v", want, lifetime)
	}
}

// 验收 A2：任务正常结束后凭据立刻失效。
func TestRunTaskRevokesScriptTokenAfterCompletion(t *testing.T) {
	testutil.SetupTestEnv(t)

	var envVars map[string]string
	stubRunCommandWithPlan(t, func(taskEnv map[string]string) (*ScriptResult, error) {
		envVars = copyScriptEnv(taskEnv)
		return &ScriptResult{ReturnCode: 0}, nil
	})

	runTaskForScriptTokenTest(t, 0)
	claims := mustParseScriptToken(t, envVars)

	if envVars["DAIDAI_NOTIFY_TOKEN"] != envVars["DAIDAI_TOKEN"] {
		t.Fatalf("expected DAIDAI_NOTIFY_TOKEN and DAIDAI_TOKEN to share one credential")
	}

	var blocked model.TokenBlocklist
	if err := database.DB.Where("jti = ?", claims.ID).First(&blocked).Error; err != nil {
		t.Fatalf("expected script token jti %q in blocklist: %v", claims.ID, err)
	}
	if blocked.TokenType != "access" {
		t.Fatalf("expected blocklist entry token_type=access, got %q", blocked.TokenType)
	}
	// 拉黑记录不能比 token 本身先过期，否则清理后 token 会“复活”。
	if blocked.ExpiresAt.Before(claims.ExpiresAt.Time.Add(-time.Minute)) {
		t.Fatalf("blocklist entry expires %v before token expiry %v", blocked.ExpiresAt, claims.ExpiresAt.Time)
	}
	if !middleware.IsTokenBlocked(claims.ID) {
		t.Fatalf("expected IsTokenBlocked to report the finished task token as revoked")
	}
}

// 验收 A4：任务崩溃也必须吊销，不能依赖正常结算路径。
func TestRunTaskRevokesScriptTokenAfterPanic(t *testing.T) {
	testutil.SetupTestEnv(t)

	var envVars map[string]string
	stubRunCommandWithPlan(t, func(taskEnv map[string]string) (*ScriptResult, error) {
		envVars = copyScriptEnv(taskEnv)
		panic("boom inside task execution")
	})

	runTaskForScriptTokenTest(t, 0)
	claims := mustParseScriptToken(t, envVars)

	if !middleware.IsTokenBlocked(claims.ID) {
		t.Fatalf("expected panicked task token to be revoked")
	}
}

// 验收 A3：未设超时的长任务在运行期间凭据必须始终有效。
func TestRunTaskKeepsScriptTokenValidWhileRunning(t *testing.T) {
	testutil.SetupTestEnv(t)

	var (
		envVars            map[string]string
		blockedDuringRun   bool
		remainingDuringRun time.Duration
	)

	stubRunCommandWithPlan(t, func(taskEnv map[string]string) (*ScriptResult, error) {
		envVars = copyScriptEnv(taskEnv)

		claims, err := middleware.ParseToken(envVars["DAIDAI_TOKEN"])
		if err != nil {
			return nil, err
		}
		blockedDuringRun = middleware.IsTokenBlocked(claims.ID)
		remainingDuringRun = time.Until(claims.ExpiresAt.Time)
		return &ScriptResult{ReturnCode: 0}, nil
	})

	runTaskForScriptTokenTest(t, 0)
	claims := mustParseScriptToken(t, envVars)

	if blockedDuringRun {
		t.Fatalf("script token must stay usable while the task is still running")
	}
	// 长任务不能跑到一半丢权限：运行中剩余寿命至少还有一天。
	if remainingDuringRun < 24*time.Hour {
		t.Fatalf("expected at least 24h of remaining lifetime mid-run, got %v", remainingDuringRun)
	}
	if !middleware.IsTokenBlocked(claims.ID) {
		t.Fatalf("expected token to be revoked once the task finished")
	}
}

// 重复吊销必须幂等：blockToken 内部按 jti 去重。
func TestRevokeScriptTokenIsIdempotent(t *testing.T) {
	testutil.SetupTestEnv(t)

	info := &ScriptTokenInfo{JTI: "script-token-idempotent", ExpiresAt: time.Now().Add(time.Hour)}
	RevokeScriptToken(info)
	RevokeScriptToken(info)
	RevokeScriptToken(nil)
	RevokeScriptToken(&ScriptTokenInfo{})

	var count int64
	database.DB.Model(&model.TokenBlocklist{}).Where("jti = ?", info.JTI).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly one blocklist row after repeated revokes, got %d", count)
	}
}
