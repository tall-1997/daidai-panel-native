package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 本文件覆盖 #158「执行趋势保留历史」的仪表板侧：任何删日志的入口都先把要删的行按天并进 task_log_daily_stats，
// 仪表板的今日 / 昨日 / 按天 = 现存 + 计数表，所以删除前后这些数字逐字段不变。

// trendDailyStat 是仪表板 daily_stats 的一项。
type trendDailyStat struct {
	Date    string `json:"date"`
	Success int64  `json:"success"`
	Failed  int64  `json:"failed"`
	Aborted int64  `json:"aborted"`
}

// trendDashboard 只取仪表板里和执行趋势有关的字段：today_* / yesterday_* / daily_stats。
// 任务数、最近 10 条等字段删任务时本来就会变，不在比较范围内。
type trendDashboard struct {
	TodayLogs        int64            `json:"today_logs"`
	SuccessLogs      int64            `json:"success_logs"`
	FailedLogs       int64            `json:"failed_logs"`
	AbortedLogs      int64            `json:"aborted_logs"`
	YesterdayLogs    int64            `json:"yesterday_logs"`
	YesterdaySuccess int64            `json:"yesterday_success"`
	YesterdayFailed  int64            `json:"yesterday_failed"`
	YesterdayAborted int64            `json:"yesterday_aborted"`
	DailyStats       []trendDailyStat `json:"daily_stats"`
}

// trendPinPanelTimezone 把面板时区钉到默认的东八区（没有夏令时），用例结束还原，写法照 system_dashboard_counts_test.go。
// 仪表板按天那一桶是 [day, day+24h)，本机时区有夏令时的话，切换那天的桶和「按存储日期归档」的计数就对不齐了。
func trendPinPanelTimezone(t *testing.T) {
	t.Helper()
	oldLocal := time.Local
	oldTZ, hadTZ := os.LookupEnv("TZ")
	oldName := service.CurrentPanelTimezone()
	t.Cleanup(func() {
		_ = service.ApplyPanelTimezone(oldName)
		time.Local = oldLocal
		if hadTZ {
			_ = os.Setenv("TZ", oldTZ)
		} else {
			_ = os.Unsetenv("TZ")
		}
	})
	if err := service.ApplyPanelTimezone(model.DefaultPanelTimezone); err != nil {
		t.Fatalf("apply panel timezone: %v", err)
	}
}

// trendGet 以 token 发一个 GET，要求 200，返回响应体原文。
func trendGet(t *testing.T, engine *gin.Engine, token, path string) string {
	t.Helper()
	rec := performJSONRequest(engine, http.MethodGet, path, `{}`, map[string]string{"Authorization": "Bearer " + token}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d body=%s", path, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// trendFetchDashboard 取一份仪表板的执行趋势字段。query 形如 "?range=7"，空串表示不带 range（APP 就不带）。
func trendFetchDashboard(t *testing.T, engine *gin.Engine, token, query string) trendDashboard {
	t.Helper()
	var payload struct {
		Data trendDashboard `json:"data"`
	}
	if err := json.Unmarshal([]byte(trendGet(t, engine, token, "/api/v1/system/dashboard"+query)), &payload); err != nil {
		t.Fatalf("dashboard%s: decode response: %v", query, err)
	}
	return payload.Data
}

// trendCountLogs 按条件数 task_logs 里现存的行。
func trendCountLogs(t *testing.T, where string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := database.DB.Model(&model.TaskLog{}).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatalf("count task logs where %s: %v", where, err)
	}
	return n
}

// trendArchivedTotal 是计数表里所有行、所有列（成功 + 失败 + 终止 + 其它）之和。
func trendArchivedTotal(t *testing.T) int64 {
	t.Helper()
	var total int64
	if err := database.DB.Raw("SELECT COALESCE(SUM(success + failed + aborted + other), 0) FROM task_log_daily_stats").Scan(&total).Error; err != nil {
		t.Fatalf("sum task_log_daily_stats: %v", err)
	}
	return total
}

// trendCreateLog 造一行日志，started_at 与 created_at 都取 at（仪表板按 created_at 分桶）。
func trendCreateLog(t *testing.T, taskID uint, at time.Time, status *int) uint {
	t.Helper()
	entry := &model.TaskLog{TaskID: taskID, Status: status, StartedAt: at, CreatedAt: at}
	if err := database.DB.Create(entry).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	return entry.ID
}

// trendFixture 是 H1 每个子用例各自的一套新环境。
type trendFixture struct {
	engine          *gin.Engine
	token           string
	taskA           uint
	taskB           uint
	yesterdayNullID uint
	batchIDs        []uint
}

// trendSeed 建一套新环境：任务 A（任务级保留 1 天）、任务 B；今天和前 7 天每天 A 成功 / 失败、B 成功 / 终止各一行，
// 外加昨天 B 一行 status 为 NULL、今天 A 一行运行中。
func trendSeed(t *testing.T) trendFixture {
	t.Helper()
	testutil.SetupTestEnv(t)
	user := testutil.MustCreateUser(t, "trend-history-admin", "admin")
	f := trendFixture{engine: newProtectedRouter(), token: testutil.MustCreateAccessToken(t, user.Username, user.Role)}

	oneDay := 1
	taskA := &model.Task{Name: "trend-a", Command: "echo a", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled, LogRetentionDays: &oneDay}
	taskB := &model.Task{Name: "trend-b", Command: "echo b", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	for _, task := range []*model.Task{taskA, taskB} {
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %s: %v", task.Name, err)
		}
	}
	f.taskA, f.taskB = taskA.ID, taskB.ID

	success, failed, running, aborted := model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusRunning, model.LogStatusAborted
	today := settledLocalToday(t)
	for daysAgo := 0; daysAgo <= 7; daysAgo++ {
		day := today.AddDate(0, 0, -daysAgo)
		ids := []uint{
			trendCreateLog(t, f.taskA, day.Add(1*time.Hour), &success),
			trendCreateLog(t, f.taskA, day.Add(2*time.Hour), &failed),
			trendCreateLog(t, f.taskB, day.Add(3*time.Hour), &success),
			trendCreateLog(t, f.taskB, day.Add(4*time.Hour), &aborted),
		}
		// 批量删的那批跨天、跨状态、跨任务：3 天前的四行全要，再加今天 B 终止的那行。
		if daysAgo == 3 {
			f.batchIDs = append(f.batchIDs, ids...)
		}
		if daysAgo == 0 {
			f.batchIDs = append(f.batchIDs, ids[3])
		}
	}
	f.yesterdayNullID = trendCreateLog(t, f.taskB, today.AddDate(0, 0, -1).Add(5*time.Hour), nil)
	f.batchIDs = append(f.batchIDs, f.yesterdayNullID)
	trendCreateLog(t, f.taskA, today.Add(30*time.Minute), &running)
	return f
}

// trendDo 以管理员身份发请求，要求 200。
func trendDo(t *testing.T, f trendFixture, method, path, body string) {
	t.Helper()
	rec := performJSONRequest(f.engine, method, path, body, map[string]string{"Authorization": "Bearer " + f.token}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s: expected 200, got %d body=%s", method, path, rec.Code, rec.Body.String())
	}
}

// H1：D1~D7 每个删日志的入口（D8 订阅自动删没有 HTTP 入口，见 service 包 S7）都先并入计数：
// 每个入口真的删掉了行（D1~D3 的期望数在删前按同一条件现数，且 > 0），计数表各列之和 = 删除数，
// range=1 / 7 / 30 三份仪表板的 today_* / yesterday_* / daily_stats 删前删后逐字段相等。
func TestDashboardTrendSurvivesEveryLogDeletionEntry(t *testing.T) {
	trendPinPanelTimezone(t)
	// []uint 序列化不会出错，这里不在子用例里碰父用例的 t。
	idsJSON := func(ids []uint) string {
		raw, _ := json.Marshal(ids)
		return string(raw)
	}

	cases := []struct {
		name   string
		remove func(t *testing.T, f trendFixture) int64
	}{
		{"D1 自动清理 worker", func(t *testing.T, f trendFixture) int64 {
			// 与 CleanLogsByRetentionPolicy 同一套条件：A 设了 1 天按 1 天算，其余跟随全局 7 天，运行中的不动。
			want := trendCountLogs(t, "task_id = ? AND started_at < ? AND (status IS NULL OR status <> ?)", f.taskA, time.Now().AddDate(0, 0, -1), model.LogStatusRunning) +
				trendCountLogs(t, "task_id NOT IN ? AND started_at < ? AND (status IS NULL OR status <> ?)", []uint{f.taskA}, time.Now().AddDate(0, 0, -7), model.LogStatusRunning)
			if records, _ := service.CleanLogsByRetentionPolicy(7); records != want {
				t.Fatalf("自动清理应删掉 %d 行，实际返回 %d 行", want, records)
			}
			return want
		}},
		{"D2 DELETE /logs/clean", func(t *testing.T, f trendFixture) int64 {
			want := trendCountLogs(t, "started_at < ? AND (status IS NULL OR status <> ?)", time.Now().AddDate(0, 0, -1), model.LogStatusRunning)
			trendDo(t, f, http.MethodDelete, "/api/v1/logs/clean?days=1", `{}`)
			return want
		}},
		{"D3 DELETE /tasks/clean-logs", func(t *testing.T, f trendFixture) int64 {
			want := trendCountLogs(t, "started_at < ? AND (status IS NULL OR status <> ?)", time.Now().AddDate(0, 0, -1), model.LogStatusRunning)
			trendDo(t, f, http.MethodDelete, "/api/v1/tasks/clean-logs?days=1", `{}`)
			return want
		}},
		{"D4 DELETE /logs/:id", func(t *testing.T, f trendFixture) int64 {
			trendDo(t, f, http.MethodDelete, "/api/v1/logs/"+strconv.FormatUint(uint64(f.yesterdayNullID), 10), `{}`)
			return 1
		}},
		{"D5a DELETE /logs/batch", func(t *testing.T, f trendFixture) int64 {
			trendDo(t, f, http.MethodDelete, "/api/v1/logs/batch", `{"ids":`+idsJSON(f.batchIDs)+`}`)
			return int64(len(f.batchIDs))
		}},
		{"D5b POST /logs/batch-delete", func(t *testing.T, f trendFixture) int64 {
			trendDo(t, f, http.MethodPost, "/api/v1/logs/batch-delete", `{"ids":`+idsJSON(f.batchIDs)+`}`)
			return int64(len(f.batchIDs))
		}},
		{"D6 DELETE /tasks/:id", func(t *testing.T, f trendFixture) int64 {
			want := trendCountLogs(t, "task_id = ?", f.taskB)
			trendDo(t, f, http.MethodDelete, "/api/v1/tasks/"+strconv.FormatUint(uint64(f.taskB), 10), `{}`)
			return want
		}},
		{"D7a PUT /tasks/batch delete", func(t *testing.T, f trendFixture) int64 {
			want := trendCountLogs(t, "task_id = ?", f.taskB)
			trendDo(t, f, http.MethodPut, "/api/v1/tasks/batch", fmt.Sprintf(`{"ids":[%d],"action":"delete"}`, f.taskB))
			return want
		}},
		{"D7b DELETE /tasks/batch/delete", func(t *testing.T, f trendFixture) int64 {
			want := trendCountLogs(t, "task_id = ?", f.taskB)
			trendDo(t, f, http.MethodDelete, "/api/v1/tasks/batch/delete", fmt.Sprintf(`{"task_ids":[%d]}`, f.taskB))
			return want
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := trendSeed(t)
			ranges := []string{"?range=1", "?range=7", "?range=30"}
			before := make(map[string]trendDashboard, len(ranges))
			for _, query := range ranges {
				before[query] = trendFetchDashboard(t, f.engine, f.token, query)
			}
			totalBefore := trendCountLogs(t, "1 = 1")

			want := tc.remove(t, f)
			if want <= 0 {
				t.Fatalf("这个入口应真的删掉行（期望删除数 > 0），否则用例是空跑：want=%d", want)
			}
			if removed := totalBefore - trendCountLogs(t, "1 = 1"); removed != want {
				t.Fatalf("应删掉 %d 行，实际少了 %d 行", want, removed)
			}
			if archived := trendArchivedTotal(t); archived != want {
				t.Fatalf("计数表各列之和应等于删除数 %d，实际 %d", want, archived)
			}
			for _, query := range ranges {
				if after := trendFetchDashboard(t, f.engine, f.token, query); !reflect.DeepEqual(after, before[query]) {
					t.Fatalf("dashboard%s 的今日 / 昨日 / 按天应与删除前逐字段相等\nbefore=%+v\n after=%+v", query, before[query], after)
				}
			}
		})
	}
}

// H2：直接往计数表里插行，仪表板的今日 / 昨日 / 按天都要加回来：
//   - today_logs 连 other 一起加；range=1（昨天不在趋势里）时「昨日执行」照样要加（核实 M1），不带 range、range=7 也一样；
//   - range=7 看不到 7 天前那行，range=30 看得到；
//   - 只看现存的三个接口不加计数表：侧栏角标、/system/stats、/tasks/:id/stats 与插入前逐字节相同。
func TestDashboardAddsArchivedCountsIncludingRangeOne(t *testing.T) {
	trendPinPanelTimezone(t)
	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "trend-archived-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "trend-archived", Command: "echo ok", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	today := settledLocalToday(t)
	success, failed := model.LogStatusSuccess, model.LogStatusFailed
	// 现存日志只造在今天（成功 1、失败 1）：角标与系统概况要有非零的底数；昨天一行现存都没有，昨日的数字全来自计数表。
	trendCreateLog(t, task.ID, today.Add(time.Hour), &success)
	trendCreateLog(t, task.ID, today.Add(2*time.Hour), &failed)

	badgesBefore := trendGet(t, engine, token, "/api/v1/system/badges")
	statsBefore := trendGet(t, engine, token, "/api/v1/system/stats")
	taskStatsPath := "/api/v1/tasks/" + strconv.FormatUint(uint64(task.ID), 10) + "/stats"
	taskStatsBefore := trendGet(t, engine, token, taskStatsPath)

	// 今天那行四列都非零、各不相同（失败也非零，角标「今日失败」不加计数表这一条才有牙齿）；昨天那行带 other。
	day := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format("2006-01-02") }
	for _, stat := range []model.TaskLogDailyStat{
		{Day: day(0), Success: 2, Failed: 1, Aborted: 3, Other: 1},
		{Day: day(1), Success: 5, Failed: 1, Other: 1},
		{Day: day(6), Success: 4},
		{Day: day(7), Failed: 2},
	} {
		row := stat
		if err := database.DB.Create(&row).Error; err != nil {
			t.Fatalf("insert task_log_daily_stats %+v: %v", stat, err)
		}
	}

	// 按天的期望（现存 + 计数表），键是「距今天几天」，其余天全是 0；三项依次是成功 / 失败 / 终止。
	wantByDaysAgo := map[int][3]int64{
		0: {1 + 2, 1 + 1, 0 + 3},
		1: {5, 1, 0},
		6: {4, 0, 0},
		7: {0, 2, 0},
	}
	for _, tc := range []struct {
		query    string
		wantDays int
	}{
		{"", 7},
		{"?range=1", 1},
		{"?range=7", 7},
		{"?range=30", 30},
	} {
		got := trendFetchDashboard(t, engine, token, tc.query)
		// 今日：现存 2 行 + 计数表 2+1+3+1（总数连 other 一起加）。
		if got.TodayLogs != 9 || got.SuccessLogs != 3 || got.FailedLogs != 2 || got.AbortedLogs != 3 {
			t.Fatalf("dashboard%s：今日应为 总数 9、成功 3、失败 2、终止 3，实际 总数 %d、成功 %d、失败 %d、终止 %d",
				tc.query, got.TodayLogs, got.SuccessLogs, got.FailedLogs, got.AbortedLogs)
		}
		// 昨日：全来自计数表 5+1+0+1。range=1 时也必须加上（读计数表的下界要取 min(趋势第一天, 昨天)）。
		if got.YesterdayLogs != 7 || got.YesterdaySuccess != 5 || got.YesterdayFailed != 1 || got.YesterdayAborted != 0 {
			t.Fatalf("dashboard%s：昨日应为 总数 7、成功 5、失败 1、终止 0，实际 总数 %d、成功 %d、失败 %d、终止 %d",
				tc.query, got.YesterdayLogs, got.YesterdaySuccess, got.YesterdayFailed, got.YesterdayAborted)
		}
		want := make([]trendDailyStat, 0, tc.wantDays)
		for daysAgo := tc.wantDays - 1; daysAgo >= 0; daysAgo-- {
			counts := wantByDaysAgo[daysAgo]
			want = append(want, trendDailyStat{
				Date:    today.AddDate(0, 0, -daysAgo).Format("01-02"),
				Success: counts[0],
				Failed:  counts[1],
				Aborted: counts[2],
			})
		}
		if !reflect.DeepEqual(got.DailyStats, want) {
			t.Fatalf("dashboard%s：按天统计应为 %+v，实际 %+v", tc.query, want, got.DailyStats)
		}
	}

	if after := trendGet(t, engine, token, "/api/v1/system/badges"); after != badgesBefore {
		t.Fatalf("侧栏角标只看现存日志，不该加计数表\nbefore=%s\n after=%s", badgesBefore, after)
	}
	if after := trendGet(t, engine, token, "/api/v1/system/stats"); after != statsBefore {
		t.Fatalf("/system/stats 只看现存日志，不该加计数表\nbefore=%s\n after=%s", statsBefore, after)
	}
	if after := trendGet(t, engine, token, taskStatsPath); after != taskStatsBefore {
		t.Fatalf("/tasks/:id/stats 只看现存日志，不该加计数表\nbefore=%s\n after=%s", taskStatsBefore, after)
	}
}

// H3（核实 M2）：仪表板的全部读取在同一个只读事务里。在「读计数表」那条查询的回调里起一个 goroutine 去清理日志：
// 读事务还开着时清理拿不到唯一的连接，只能等事务结束，所以交错的那次请求、清理之后的请求都和清理之前一样。
// 去掉事务的话，清理会插进「读计数表」与「按天现算」之间，被删的行两边都数不到，这条会红（设计期实测 12→0→12）。
// 回调里只能等 goroutine 起来，不能等清理真的进了事务：正确实现下清理要等仪表板事务提交才拿得到连接，在回调里等它会死锁。
func TestDashboardReadsOneSnapshotWhileLogsAreDeleted(t *testing.T) {
	trendPinPanelTimezone(t)
	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "trend-snapshot-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "trend-snapshot", Command: "echo ok", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	today := settledLocalToday(t)
	success := model.LogStatusSuccess
	// 1~6 天前每天两条成功：都在 7 天窗口里，合计 12；清理（保留 1 天）会删掉其中绝大部分。
	for daysAgo := 1; daysAgo <= 6; daysAgo++ {
		for i := 0; i < 2; i++ {
			trendCreateLog(t, task.ID, today.AddDate(0, 0, -daysAgo).Add(time.Duration(10+i)*time.Hour), &success)
		}
	}
	trendSum := func() int64 {
		var sum int64
		for _, stat := range trendFetchDashboard(t, engine, token, "?range=7").DailyStats {
			sum += stat.Success
		}
		return sum
	}

	before := trendSum()

	fired := false
	done := make(chan struct{})
	if err := database.DB.Callback().Query().After("gorm:query").Register("test:trend_interleave_cleanup", func(db *gorm.DB) {
		// 只在第一次看到读计数表的查询时触发；清理 goroutine 里的查询也会走到这里，fired 之后一律放过。
		if fired || !strings.Contains(db.Statement.SQL.String(), "task_log_daily_stats") {
			return
		}
		fired = true
		started := make(chan struct{})
		go func() {
			defer close(done)
			close(started)
			service.CleanLogsOlderThan(1)
		}()
		<-started
		time.Sleep(150 * time.Millisecond)
	}); err != nil {
		t.Fatalf("register interleave callback: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Callback().Query().Remove("test:trend_interleave_cleanup") })

	during := trendSum()
	if !fired {
		t.Fatal("回调没有触发：仪表板没有读计数表，或者截获条件失灵了，用例在空跑")
	}
	<-done
	after := trendSum()
	if before != 12 || during != before || after != before {
		t.Fatalf("7 天趋势合计应始终是 12（删除前 = 交错那次 = 清理之后），实际 %d → %d → %d", before, during, after)
	}
}

// H4（核实 M3，已接受的例外，用例钉住契约）：用 DELETE /logs/:id 删掉运行中的那行之后、执行结束之前面板重启，
// 启动恢复（RecoverAbandonedActiveTasks → markTaskLogInterrupted）找不到运行中的行，会另建一行失败——
// 同一次执行在趋势里算两次（other 1 + 失败 1）。修法（删日志接口拒删运行中的行）在 backlog，修的时候要同步改这条。
func TestDeletingRunningLogThenRecoveryCountsTwice(t *testing.T) {
	trendPinPanelTimezone(t)
	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "trend-running-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	settledLocalToday(t)
	now := time.Now()
	task := &model.Task{Name: "trend-running", Command: "echo ok", TaskType: model.TaskTypeManual, Status: model.TaskStatusRunning, LastRunAt: &now}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	running := model.LogStatusRunning
	logID := trendCreateLog(t, task.ID, now, &running)

	rec := performJSONRequest(engine, http.MethodDelete, "/api/v1/logs/"+strconv.FormatUint(uint64(logID), 10), `{}`, map[string]string{"Authorization": "Bearer " + token}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete running log: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := trendFetchDashboard(t, engine, token, ""); got.TodayLogs != 1 || got.FailedLogs != 0 {
		t.Fatalf("删掉运行中的那行后：今日应为 1（计入 other）、失败 0，实际 今日 %d、失败 %d", got.TodayLogs, got.FailedLogs)
	}

	// 模拟执行结束之前面板重启：启动恢复找不到运行中的行，另建一行失败。
	if n := service.RecoverAbandonedActiveTasks("面板重启"); n != 1 {
		t.Fatalf("应恢复 1 个运行中的任务，实际 %d 个", n)
	}
	if got := trendFetchDashboard(t, engine, token, ""); got.TodayLogs != 2 || got.FailedLogs != 1 {
		t.Fatalf("重启恢复后同一次执行算两次（已接受的例外）：今日应为 2、失败 1，实际 今日 %d、失败 %d", got.TodayLogs, got.FailedLogs)
	}
}

// H5：删日志的事务出错（这里用触发器拦下删行）：DELETE /logs/:id、DELETE /logs/batch 都回 500「删除日志失败」，
// 计数表一行不留、日志行还在。改动前前者会误报 404「日志不存在」、后者回 200「已删除 0 条日志」。
func TestLogDeleteFailureReturns500AndKeepsCounts(t *testing.T) {
	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "trend-delete-fail-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{Name: "trend-delete-fail", Command: "echo ok", TaskType: model.TaskTypeManual, Status: model.TaskStatusEnabled}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	success := model.LogStatusSuccess
	logID := trendCreateLog(t, task.ID, time.Now(), &success)
	if err := database.DB.Exec(`CREATE TRIGGER trend_test_block_log_delete BEFORE DELETE ON task_logs BEGIN SELECT RAISE(ABORT, 'blocked by test'); END;`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	for _, req := range []struct {
		method, path, body string
	}{
		{http.MethodDelete, "/api/v1/logs/" + strconv.FormatUint(uint64(logID), 10), `{}`},
		{http.MethodDelete, "/api/v1/logs/batch", fmt.Sprintf(`{"ids":[%d]}`, logID)},
	} {
		rec := performJSONRequest(engine, req.method, req.path, req.body, map[string]string{"Authorization": "Bearer " + token}, "")
		var body struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != http.StatusInternalServerError || body.Error != "删除日志失败" {
			t.Fatalf("%s %s：数据库出错时应回 500「删除日志失败」，实际 %d %s", req.method, req.path, rec.Code, rec.Body.String())
		}
		var archivedRows int64
		if err := database.DB.Model(&model.TaskLogDailyStat{}).Count(&archivedRows).Error; err != nil {
			t.Fatalf("count task_log_daily_stats: %v", err)
		}
		if archivedRows != 0 {
			t.Fatalf("%s %s：事务回滚后计数表应为空，实际 %d 行", req.method, req.path, archivedRows)
		}
		if n := trendCountLogs(t, "id = ?", logID); n != 1 {
			t.Fatalf("%s %s：日志行应还在，实际 %d 行", req.method, req.path, n)
		}
	}
}
