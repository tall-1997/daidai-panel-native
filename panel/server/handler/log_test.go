package handler_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// mustCreateStreamTask 建一条任务并把状态改成指定值。
// 状态单独用 Update 写：Create 时带上 status 会和其它用例的写法分叉，
// 现有 task_query_regression_test.go 也是先 Create 再 Update 状态。
func mustCreateStreamTask(t *testing.T, name string, status float64) *model.Task {
	t.Helper()

	task := &model.Task{Name: name, Command: "echo " + name, CronExpression: "0 0 * * *"}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	if err := database.DB.Model(task).Update("status", status).Error; err != nil {
		t.Fatalf("set status for %q: %v", name, err)
	}

	return task
}

// TestLogStreamFinishedTaskReturnsWithoutFixedDelay 锁住 issue #109-1 的核心契约：
// 「点日志看上次跑完的结果」不能再吃那条无条件的 1.5s sleep。
//
// 这类任务的状态一定是 Disabled(0) 或 Enabled(1)——它们根本没有执行在启动，
// 等再久也不会有 TinyLog 出现，所以 handler 必须 0 等待直接收口。
func TestLogStreamFinishedTaskReturnsWithoutFixedDelay(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "log-stream-viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	cases := []struct {
		name   string
		status float64
	}{
		{"已启用（上次已跑完）", model.TaskStatusEnabled},
		{"已禁用", model.TaskStatusDisabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := mustCreateStreamTask(t, "finished-"+tc.name, tc.status)
			path := "/api/v1/logs/" + strconv.FormatUint(uint64(task.ID), 10) + "/stream"

			started := time.Now()
			rec := performRequest(engine, http.MethodGet, path, map[string]string{
				"Authorization": "Bearer " + accessToken,
			})
			elapsed := time.Since(started)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
			}
			// 阈值取 500ms：真实开销只有一次 JWT 校验 + 两三条 SQLite 查询（毫秒级），
			// 而回归后的表现是 1500ms，两者之间留足了余量，不会因为机器慢就误报。
			if elapsed >= 500*time.Millisecond {
				t.Fatalf("已结束任务的日志流不应再有固定等待，实际耗时 %v", elapsed)
			}

			body := rec.Body.String()
			if !strings.Contains(body, "event: done\ndata: finished") {
				t.Fatalf("expected done:finished event, got %q", body)
			}
		})
	}
}

// TestLogStreamFlushesHeaderBeforeQuerying 锁住「进 handler 立刻 flush 响应头」这条改动。
//
// 响应体的第一段必须是那条 SSE 注释行：它是让前端 fetch 立刻 resolve、onOpen 立刻触发的唯一手段，
// 否则最坏要等到 1.5s 轮询结束才发头，日志弹窗全程白屏转圈。
// 注释行本身对前端是 no-op（sse.ts 对 ":" 开头的行直接 continue），所以放在最前面是安全的。
func TestLogStreamFlushesHeaderBeforeQuerying(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "log-stream-open-viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateStreamTask(t, "flush-header", model.TaskStatusEnabled)
	path := "/api/v1/logs/" + strconv.FormatUint(uint64(task.ID), 10) + "/stream"

	rec := performRequest(engine, http.MethodGet, path, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Body.String(), ": open\n\n") {
		t.Fatalf("expected stream to open with an SSE comment line, got %q", rec.Body.String())
	}
}

// TestLogStreamQueuedTaskStillWaitsForTinyLog 守住「竞态覆盖范围不变」这一半。
//
// 排队中(0.5) 的任务是真有可能马上建出 TinyLog 的（点运行 → 入队 → runTask 建实时日志），
// 这条路径必须继续等，不能被上面那个「0 等待」的优化顺手一起砍掉。
// 这里不造 TinyLog（TinyLogManager 是进程级单例，留下残留会污染同包其它用例），
// 只验证 handler 确实等满了轮询窗口。
func TestLogStreamQueuedTaskStillWaitsForTinyLog(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "log-stream-queued-viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := mustCreateStreamTask(t, "queued", model.TaskStatusQueued)
	path := "/api/v1/logs/" + strconv.FormatUint(uint64(task.ID), 10) + "/stream"

	started := time.Now()
	rec := performRequest(engine, http.MethodGet, path, map[string]string{
		"Authorization": "Bearer " + accessToken,
	})
	elapsed := time.Since(started)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	// 封顶是 1.5s（10 × 150ms），这里只断言「明显等过」，不卡死上限，避免慢机器上抖成 flaky。
	if elapsed < time.Second {
		t.Fatalf("排队中的任务应保留启动竞态的轮询窗口，实际只耗时 %v", elapsed)
	}
}
