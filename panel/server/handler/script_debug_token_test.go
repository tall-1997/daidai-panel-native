package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/middleware"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// waitFor 轮询等待条件成立；调试运行的收尾发生在后台 goroutine 里，不能读一次就下结论。
func waitFor(t *testing.T, timeout time.Duration, describe string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, describe)
}

// 守卫缺口 2：脚本编辑器的调试运行同样会注入一枚 operator 凭据，
// 历史实现只给它 2 小时 TTL 却从不吊销。跑完必须立刻作废。
func TestDebugRunRevokesScriptTokenAfterCompletion(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	const scriptName = "debug-token-probe.sh"
	const outputName = "debug-token-probe.out"

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, scriptName)
	outputPath := filepath.Join(config.C.Data.ScriptsDir, outputName)
	// 把注入的凭据落到文件，测试才能拿到它的 jti —— 生产脚本绝不该这么写。
	script := "printf '%s' \"$DAIDAI_TOKEN\" > " + outputName + "\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	gin.SetMode(gin.TestMode)
	h := NewScriptHandler()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/scripts/run",
		strings.NewReader(`{"path":"`+scriptName+`"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	h.DebugRun(c)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201 from debug run, got %d body=%s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode debug run response: %v body=%s", err, recorder.Body.String())
	}
	if created.RunID == "" {
		t.Fatalf("expected run_id in debug run response, got %s", recorder.Body.String())
	}

	waitFor(t, 30*time.Second, "debug run to finish", func() bool {
		run, ok := h.loadRun(created.RunID)
		if !ok {
			return false
		}
		_, done, _, _ := run.snapshot()
		return done
	})

	tokenBytes, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read injected token: %v", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		t.Fatal("expected DAIDAI_TOKEN to be injected into the debug run environment")
	}

	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse injected debug token: %v", err)
	}
	// TTL 维持 2 小时不变：调试运行是人在页面上点的，短窗口足够。
	lifetime := time.Until(claims.ExpiresAt.Time)
	if lifetime > scriptDebugEnvTTL+time.Minute {
		t.Fatalf("expected debug token to live at most %v, got %v", scriptDebugEnvTTL, lifetime)
	}

	// 吊销挂在 goroutine 的 defer 上，跑在 run.finish 之后，所以要轮询而不是读一次。
	waitFor(t, 10*time.Second, "debug run token to be revoked", func() bool {
		return middleware.IsTokenBlocked(claims.ID)
	})
}

// buildScriptExecEnv 必须把 jti 一路交回给调用方，否则启动失败那几条分支根本没法吊销。
// （本用例不依赖 bash，所以在没有解释器的机器上也会跑。）
func TestBuildScriptExecEnvReturnsRevocableToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	envMap, scriptToken := buildScriptExecEnv(config.C.Data.ScriptsDir)
	if scriptToken == nil || scriptToken.JTI == "" {
		t.Fatalf("expected buildScriptExecEnv to return a revocable token, got %#v", scriptToken)
	}
	if envMap["DAIDAI_TOKEN"] == "" {
		t.Fatalf("expected DAIDAI_TOKEN in debug env, got %#v", envMap)
	}

	claims, err := middleware.ParseToken(envMap["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse debug token: %v", err)
	}
	if claims.ID != scriptToken.JTI {
		t.Fatalf("expected returned token jti %q to match the injected one %q", scriptToken.JTI, claims.ID)
	}
	if middleware.IsTokenBlocked(claims.ID) {
		t.Fatal("freshly issued debug token must not be blocked")
	}
}
