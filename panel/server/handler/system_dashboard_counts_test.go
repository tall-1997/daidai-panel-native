package handler_test

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// settledLocalToday 返回「本地今天零点」。离下一个零点不到 30 秒时先睡过零点再算：
// 造数与发请求之间一旦跨天，handler 算出的「今天」就和用例算的不是同一天，用例会随机变红。
func settledLocalToday(t *testing.T) time.Time {
	t.Helper()

	now := time.Now()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	if wait := nextMidnight.Sub(now); wait < 30*time.Second {
		time.Sleep(wait + time.Second)
		now = time.Now()
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// TestSystemDashboardCountBoundariesStayUnchanged 锁住仪表板的计数口径（issue #153）。
//
// #153 只给 task_logs 补了索引、SQL 一条没改。这条用例保证以后谁想「顺手合并查询」或改写条件，口径一变就红。
// 容易被改坏的几处：
//   - today_* 只有下界 created_at >= 今天零点，时间在未来的行也算今天；
//   - 按天那一桶是 [day, day+24h)，上界不含：今天那一桶不含「明天零点整」那行，today_logs 却含；
//   - today_logs / yesterday_logs 是全部行，含 status 为 NULL 与运行中的，分状态的三项不含它们；
//   - range 只接受 1~90，不带、0、91、非数字一律回落 7 天；
//   - recent_logs 最多 10 条，按 created_at 倒序。
//
// 必须走真实路由、真实驱动，时间一律交给驱动按 time.Time 绑定：库里存的是带偏移的文本，比较按文本字典序，
// 手写日期字符串（比如照抄 GORM 慢日志里那种去掉了偏移的时间）会让零点边界的语义和生产不一样。
func TestSystemDashboardCountBoundariesStayUnchanged(t *testing.T) {
	// 钉到面板默认时区（没有夏令时），生产启动时同样会先应用面板时区。按天那一桶是 day+24h，
	// 本机时区如果有夏令时，切换那天会多出或少掉一小时，零点边界上的几行就会漂到别的桶里。
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

	testutil.SetupTestEnv(t)
	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "dashboard-counts", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:     "dashboard counts task",
		Command:  "echo ok",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	today := settledLocalToday(t)
	yesterday := today.AddDate(0, 0, -1)
	success, failed, running, aborted := model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusRunning, model.LogStatusAborted
	// 按 created_at 从新到旧排列，前 10 行就是 recent_logs 的期望顺序。
	// 「零点前 1 ns」就是「昨天 23:59:59.999999999」，同一个时刻只造一行；
	// 另补一行「昨天零点前 1 ns」（前天 23:59:59.999999999），锁住昨日区间的下界。
	rows := []struct {
		createdAt time.Time
		status    *int
	}{
		{today.Add(24 * time.Hour), &failed},                    // 明天零点整（未来行）：算进 today_*，但落在今天那一桶的上界上、不进按天统计
		{today.Add(3 * time.Hour), &aborted},                    // 今天 03:00
		{today.Add(2 * time.Hour), &running},                    // 今天 02:00，运行中
		{today.Add(time.Hour), nil},                             // 今天 01:00，status 为 NULL
		{today, &success},                                       // 今天零点整
		{today.Add(-time.Nanosecond), &failed},                  // 零点前 1 ns：属于昨天
		{yesterday.Add(5 * time.Hour), &success},                // 昨天 05:00
		{yesterday.Add(4 * time.Hour), &running},                // 昨天 04:00，运行中
		{yesterday.Add(3 * time.Hour), nil},                     // 昨天 03:00，status 为 NULL
		{yesterday, &aborted},                                   // 昨天零点整
		{yesterday.Add(-time.Nanosecond), &success},             // 昨天零点前 1 ns：属于前天，不算昨日
		{today.AddDate(0, 0, -6).Add(12 * time.Hour), &failed},  // 6 天前：7 天视图最早的那一桶
		{today.AddDate(0, 0, -7).Add(12 * time.Hour), &success}, // 7 天前：落在 7 天窗口外，只有 30 天视图看得到
	}
	// 刻意打乱插入顺序，让 id 顺序与 created_at 顺序对不上：recent_logs 一旦被改成按 id 排，这里才抓得到。
	ids := make([]uint, len(rows))
	for _, i := range []int{1, 3, 5, 7, 9, 11, 0, 2, 4, 6, 8, 10, 12} {
		logRecord := &model.TaskLog{
			TaskID:    task.ID,
			Status:    rows[i].status,
			StartedAt: rows[i].createdAt,
			CreatedAt: rows[i].createdAt,
		}
		if err := database.DB.Create(logRecord).Error; err != nil {
			t.Fatalf("create task log %d: %v", i, err)
		}
		ids[i] = logRecord.ID
	}

	type dashboardData struct {
		TodayLogs        int64 `json:"today_logs"`
		SuccessLogs      int64 `json:"success_logs"`
		FailedLogs       int64 `json:"failed_logs"`
		AbortedLogs      int64 `json:"aborted_logs"`
		YesterdayLogs    int64 `json:"yesterday_logs"`
		YesterdaySuccess int64 `json:"yesterday_success"`
		YesterdayFailed  int64 `json:"yesterday_failed"`
		YesterdayAborted int64 `json:"yesterday_aborted"`
		RangeDays        int   `json:"range_days"`
		DailyStats       []struct {
			Date    string `json:"date"`
			Success int64  `json:"success"`
			Failed  int64  `json:"failed"`
			Aborted int64  `json:"aborted"`
		} `json:"daily_stats"`
		RecentLogs []struct {
			ID uint `json:"id"`
		} `json:"recent_logs"`
	}
	fetchDashboard := func(t *testing.T, query string) dashboardData {
		t.Helper()
		rec := performJSONRequest(
			engine,
			http.MethodGet,
			"/api/v1/system/dashboard"+query,
			`{}`,
			map[string]string{"Authorization": "Bearer " + token},
			"",
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("dashboard%s: expected 200, got %d body=%s", query, rec.Code, rec.Body.String())
		}
		var payload struct {
			Data dashboardData `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("dashboard%s: decode response: %v", query, err)
		}
		return payload.Data
	}

	// 按天统计的期望值，键是「距今天几天」，其余天全是 0。三项依次是成功 / 失败 / 终止。
	wantByDaysAgo := map[int][3]int64{
		0: {1, 0, 1}, // 今天零点整（成功）、今天 03:00（终止）；明天零点整那条失败落在上界上，不算
		1: {1, 1, 1}, // 昨天 05:00（成功）、零点前 1 ns（失败）、昨天零点整（终止）；NULL 与运行中不进任何一桶
		2: {1, 0, 0}, // 昨天零点前 1 ns
		6: {0, 1, 0}, // 6 天前
		7: {1, 0, 0}, // 7 天前，只有 30 天视图有这一桶
	}
	assertDailyStats := func(t *testing.T, query string, got dashboardData, wantDays int) {
		t.Helper()
		if got.RangeDays != wantDays || len(got.DailyStats) != wantDays {
			t.Fatalf("dashboard%s: 应返回 %d 天，实际 range_days=%d、daily_stats %d 条", query, wantDays, got.RangeDays, len(got.DailyStats))
		}
		for k, stat := range got.DailyStats {
			daysAgo := wantDays - 1 - k
			want := wantByDaysAgo[daysAgo]
			wantDate := today.AddDate(0, 0, -daysAgo).Format("01-02")
			if stat.Date != wantDate || stat.Success != want[0] || stat.Failed != want[1] || stat.Aborted != want[2] {
				t.Fatalf("dashboard%s: 第 %d 桶（%d 天前）应为 %s 成功/失败/终止=%v，实际 %+v", query, k, daysAgo, wantDate, want, stat)
			}
		}
	}

	got := fetchDashboard(t, "?range=7")
	// 今日：明天零点整、今天 03:00、02:00（运行中）、01:00（NULL）、零点整，共 5 行。
	if got.TodayLogs != 5 || got.SuccessLogs != 1 || got.FailedLogs != 1 || got.AbortedLogs != 1 {
		t.Fatalf("今日计数应为 总数 5、成功 1、失败 1（未来行）、终止 1，实际 总数 %d、成功 %d、失败 %d、终止 %d",
			got.TodayLogs, got.SuccessLogs, got.FailedLogs, got.AbortedLogs)
	}
	// 昨日：[昨天零点, 今天零点)，含零点前 1 ns 与昨天零点整，不含昨天零点前 1 ns，共 5 行。
	if got.YesterdayLogs != 5 || got.YesterdaySuccess != 1 || got.YesterdayFailed != 1 || got.YesterdayAborted != 1 {
		t.Fatalf("昨日计数应为 总数 5、成功 1、失败 1、终止 1，实际 总数 %d、成功 %d、失败 %d、终止 %d",
			got.YesterdayLogs, got.YesterdaySuccess, got.YesterdayFailed, got.YesterdayAborted)
	}
	assertDailyStats(t, "?range=7", got, 7)

	// 13 行里取最新的 10 行，按 created_at 倒序。
	gotRecent := make([]uint, len(got.RecentLogs))
	for i, item := range got.RecentLogs {
		gotRecent[i] = item.ID
	}
	if wantRecent := ids[:10]; !reflect.DeepEqual(gotRecent, wantRecent) {
		t.Fatalf("recent_logs 应为最新 10 条、按 created_at 倒序，期望 id %v，实际 %v", wantRecent, gotRecent)
	}

	assertDailyStats(t, "?range=30", fetchDashboard(t, "?range=30"), 30)

	// 不带、越界、非数字一律回落 7 天（APP 请求仪表板时就不带 range）。
	for _, query := range []string{"", "?range=0", "?range=91", "?range=abc"} {
		assertDailyStats(t, query, fetchDashboard(t, query), 7)
	}
}
