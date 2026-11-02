package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func postRunCode(t *testing.T, h *ScriptHandler, body string) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/scripts/run-code", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.RunCode(c)
	return rec
}

// run-code 接受 language=bash（#139），且与 shell 走同一条执行路径（.sh 交给 bash）。
// 不依赖 bash 是否安装，Windows 上也会跑。
func TestRunCodeAcceptsBashLanguageAsShell(t *testing.T) {
	ext, ok := scriptLanguageExtMap["bash"]
	if !ok {
		t.Fatal("run-code 应接受 language=bash")
	}
	if shellExt := scriptLanguageExtMap["shell"]; ext != shellExt {
		t.Fatalf("bash 应与 shell 落成同一种文件，bash=%q shell=%q", ext, shellExt)
	}
	interpreter, err := scriptRuntimeInterpreter(ext)
	if err != nil {
		t.Fatalf("bash 对应的扩展名 %q 应有解释器: %v", ext, err)
	}
	if interpreter != "bash" {
		t.Fatalf("language=bash 应交给 bash 执行，实际解释器 %q", interpreter)
	}
}

// 放开 bash 不能顺带放开别的：未知语言仍在校验处被拦下。
func TestRunCodeRejectsUnknownLanguage(t *testing.T) {
	rec := postRunCode(t, NewScriptHandler(), `{"code":"puts 1","language":"ruby"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知语言应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "不支持的语言类型") {
		t.Fatalf("未知语言应提示不支持的语言类型，实际 body=%s", rec.Body.String())
	}
}

// 端到端：language=bash 的代码片段真的由 bash 执行（[[ ]] 是 bash 专有语法，sh 下会报错）。
func TestRunCodeRunsBashLanguageThroughShell(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	h := NewScriptHandler()
	rec := postRunCode(t, h, `{"code":"[[ 1 -eq 1 ]] && echo run-code-bash-ok\n","language":"bash"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("language=bash 应启动成功（201），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.RunID == "" {
		t.Fatalf("响应应带 run_id，err=%v body=%s", err, rec.Body.String())
	}

	var run *debugRun
	waitFor(t, 30*time.Second, "run-code to finish", func() bool {
		loaded, ok := h.loadRun(created.RunID)
		if !ok {
			return false
		}
		run = loaded
		_, done, _, _ := loaded.snapshot()
		return done
	})

	logs, _, exitCode, _ := run.snapshot()
	if exitCode == nil || *exitCode != 0 {
		t.Fatalf("bash 片段应正常退出，exitCode=%v logs=%v", exitCode, logs)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "run-code-bash-ok") {
		t.Fatalf("bash 片段输出缺失，logs=%v", logs)
	}
}
