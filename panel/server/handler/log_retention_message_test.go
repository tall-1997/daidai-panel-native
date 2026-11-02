package handler_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// latestLogPath 拼「最近一次日志」接口的地址，三条用例共用。
func latestLogPath(taskID uint64) string {
	return "/api/v1/tasks/" + strconv.FormatUint(taskID, 10) + "/latest-log"
}

// TestLatestLogMissingRowMessageTellsWhetherTaskEverRan 锁住 issue #115 里那句误导文案的修法。
//
// task_logs 行查不到有两种完全不同的成因：
//   - 任务确实一次都没跑过 → 「该任务还没有日志记录」，前端「执行一次后就会出现日志」的引导是对的；
//   - 跑过、但日志被保留策略物理删掉了（log_cleanup 直接删行，last_run_at 还留在任务上）
//     → 再说「还没有日志记录」就和列表「上次结果」列自相矛盾，必须换成清理文案 + 最近一次运行时间。
//
// 两种情况都只换 message，状态码仍是 404、响应体仍是 {"error": ...}，前端不用改契约。
func TestLatestLogMissingRowMessageTellsWhetherTaskEverRan(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "latest-log-viewer", "viewer")
	accessToken := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 固定一个本地时间，避免用 time.Now() 让断言依赖当下时刻。
	lastRun := time.Date(2026, 9, 1, 8, 0, 0, 0, time.Local)

	neverRan := &model.Task{Name: "never-ran", Command: "echo never", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(neverRan).Error; err != nil {
		t.Fatalf("create never-ran task: %v", err)
	}

	logsCleaned := &model.Task{Name: "logs-cleaned", Command: "echo cleaned", CronExpression: "0 0 * * *"}
	if err := database.DB.Create(logsCleaned).Error; err != nil {
		t.Fatalf("create logs-cleaned task: %v", err)
	}
	// 只写 last_run_at、不建 task_logs 行，正好复刻「日志已被清理」的现场。
	if err := database.DB.Model(logsCleaned).Update("last_run_at", lastRun).Error; err != nil {
		t.Fatalf("set last_run_at: %v", err)
	}

	cases := []struct {
		name   string
		taskID uint64
		want   string
	}{
		// 前端会直接把这句 message 显示在日志抽屉里，所以它必须与抽屉一直以来的说法逐字一致。
		{"从来没跑过", uint64(neverRan.ID), "该任务还没有日志记录"},
		{"跑过但日志被清理", uint64(logsCleaned.ID), "该任务的日志已按保留策略清理（最近一次运行 2026-09-01 08:00:00），保留天数可在设置里调整"},
		// 任务本身查不到时不额外报错，沿用「还没有日志记录」，避免把一个不存在的 id 说成「日志被清理」。
		{"任务不存在", 999999, "该任务还没有日志记录"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performRequest(engine, http.MethodGet, latestLogPath(tc.taskID), map[string]string{
				"Authorization": "Bearer " + accessToken,
			})

			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
			}

			payload := decodeJSONMap(t, rec)
			if got, _ := payload["error"].(string); got != tc.want {
				t.Fatalf("message want %q got %q", tc.want, got)
			}
		})
	}
}
