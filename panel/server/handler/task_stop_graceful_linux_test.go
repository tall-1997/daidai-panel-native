//go:build linux

package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 这一组锁的是 #159 修复 B 落在 handler 的两条停止路径（进程信号只在 Linux 上成立，要在 WSL / CI 里跑）：
//   - PUT /tasks/:id/stop：执行器认领了停止（StopTask 返回 true）就不再按库里的 PID 补一刀，
//     否则紧跟在 TERM 后面的 SIGKILL 会让宽限形同虚设，trap 了 TERM 的脚本来不及收尾；
//   - PUT /scripts/run/:run_id/stop：调试运行 / 运行代码的停止同样先 TERM、接口立即返回。
// 两条都要等脚本写出就绪标记再停：trap 装上之前发 TERM，bash 按默认动作直接退出、trap 不执行。

// waitScriptTokenRevoked 读脚本落盘的 DAIDAI_TOKEN，等到它被吊销。吊销挂在结算 / 收尾协程的最后一步，
// 吊销了就说明这次执行彻底收完尾，用例结束时不会再有协程去写已经关掉的库。
func waitScriptTokenRevoked(t *testing.T, tokenFile string) {
	t.Helper()
	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read injected token: %v", err)
	}
	claims, err := middleware.ParseToken(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse injected token: %v", err)
	}
	waitFor(t, 10*time.Second, "script token to be revoked after the run settled", func() bool {
		return middleware.IsTokenBlocked(claims.ID)
	})
}

// 停止接口：1 秒内返回 200；进程组先收到 TERM，trap 里的收尾（先 sleep 0.3 再写标记）能做完。
// 突变：恢复 handler 里 StopTask 之后无条件的 KillProcessByPid，这条应变红（收尾被紧跟的 SIGKILL 打断）。
// trap 里先 sleep 0.3 再写标记：突变下 TERM 与 SIGKILL 只隔几微秒，trap 若只写一行文件，偶尔能抢在 SIGKILL 之前写完，突变就测不出来。
func TestStopTaskEndpointLetsScriptHandleSigterm(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)
	service.ShutdownSchedulerV2()
	service.InitSchedulerV2()
	t.Cleanup(service.ShutdownSchedulerV2)

	scriptsDir := config.C.Data.ScriptsDir
	// sleep 30 放在就绪标记之前起、并重定向输出：标记出现时它已经在组里，TERM 一到就跟着退出
	script := "printf '%s' \"$DAIDAI_TOKEN\" > endpoint-token.out\n" +
		"trap 'sleep 0.3; echo done > endpoint-term.flag; exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > endpoint-ready.flag\n" +
		"wait\n"
	if err := os.WriteFile(filepath.Join(scriptsDir, "endpoint_stop.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	task := &model.Task{Name: "停止接口让脚本收尾", Command: "task endpoint_stop.sh", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := service.GetSchedulerV2().RunNow(task.ID); err != nil {
		t.Fatalf("run task: %v", err)
	}

	readyFlag := filepath.Join(scriptsDir, "endpoint-ready.flag")
	waitFor(t, 15*time.Second, "task to record its pid and write the ready flag", func() bool {
		var stored model.Task
		if err := database.DB.First(&stored, task.ID).Error; err != nil {
			return false
		}
		_, statErr := os.Stat(readyFlag)
		return stored.PID != nil && *stored.PID > 0 && statErr == nil
	})

	engine := gin.New()
	engine.PUT("/tasks/:id/stop", NewTaskHandler().Stop)
	start := time.Now()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/tasks/%d/stop", task.ID), nil))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("停止接口不应等宽限期，应在 1 秒内返回，实际 %s", elapsed)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("停止接口应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	termFlag := filepath.Join(scriptsDir, "endpoint-term.flag")
	waitFor(t, 3*time.Second, "the trapped script to finish its cleanup after SIGTERM (no SIGKILL right behind it)", func() bool {
		_, err := os.Stat(termFlag)
		return err == nil
	})
	waitScriptTokenRevoked(t, filepath.Join(scriptsDir, "endpoint-token.out"))
}

// 调试运行 / 运行代码的停止：很快返回 200、状态为 stopped；进程组先收到 TERM，trap 里的收尾能做完。
func TestDebugStopSendsSigtermFirst(t *testing.T) {
	testutil.SetupTestEnv(t)
	requireUsableBash(t)

	// run-code 的工作目录是系统临时目录下的 daidai-debug，标记文件一律用 t.TempDir() 下的绝对路径
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "debug-token.out")
	readyFlag := filepath.Join(dir, "debug-ready.flag")
	termFlag := filepath.Join(dir, "debug-term.flag")
	code := "printf '%s' \"$DAIDAI_TOKEN\" > \"" + tokenFile + "\"\n" +
		"trap 'echo done > \"" + termFlag + "\"; exit 0' TERM\n" +
		"sleep 30 >/dev/null 2>&1 &\n" +
		"echo ready > \"" + readyFlag + "\"\n" +
		"wait\n"
	body, err := json.Marshal(map[string]string{"code": code, "language": "bash"})
	if err != nil {
		t.Fatalf("marshal run-code body: %v", err)
	}

	h := NewScriptHandler()
	rec := postRunCode(t, h, string(body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("run-code 应启动成功（201），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.RunID == "" {
		t.Fatalf("响应应带 run_id，err=%v body=%s", err, rec.Body.String())
	}
	waitFor(t, 15*time.Second, "debug run to write its ready flag", func() bool {
		_, err := os.Stat(readyFlag)
		return err == nil
	})

	engine := gin.New()
	engine.PUT("/scripts/run/:run_id/stop", h.DebugStop)
	start := time.Now()
	stopRec := httptest.NewRecorder()
	engine.ServeHTTP(stopRec, httptest.NewRequest(http.MethodPut, "/scripts/run/"+created.RunID+"/stop", nil))
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("调试运行的停止不应等宽限期，应在 1 秒内返回，实际 %s", elapsed)
	}
	if stopRec.Code != http.StatusOK {
		t.Fatalf("停止应返回 200，实际 %d，body=%s", stopRec.Code, stopRec.Body.String())
	}
	run, ok := h.loadRun(created.RunID)
	if !ok {
		t.Fatalf("运行记录 %s 不该在停止后消失", created.RunID)
	}
	if _, done, _, status := run.snapshot(); !done || status != "stopped" {
		t.Fatalf("停止后应是 done=true、status=stopped，实际 done=%v status=%q", done, status)
	}

	waitFor(t, 3*time.Second, "the trapped debug script to finish its cleanup after SIGTERM", func() bool {
		_, err := os.Stat(termFlag)
		return err == nil
	})
	waitScriptTokenRevoked(t, tokenFile)
}
