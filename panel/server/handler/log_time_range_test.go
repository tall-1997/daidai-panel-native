package handler_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestLogListFiltersByTimeRange 守住日志列表的执行时间范围筛选。
//
// 三条边界都要覆盖，因为日期范围筛选最容易错的就是边界：
//  1. 只给 start_time / 只给 end_time / 两个都给，三种组合都要生效；
//  2. 落在区间【端点当天】的记录必须被选中——前端把结束日收拢到 23:59:59.999
//     就是为了这个，少了它「筛到今天」会把今天整天漏掉；
//  3. 畸形参数不能让整个接口 500 或 400，只能被忽略。
func TestLogListFiltersByTimeRange(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "log-time-range", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:     "time range task",
		Command:  "echo ok",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	now := time.Now()
	// 直接在 Create 时把 CreatedAt 写成过去的时间。
	// GORM 的 autoCreateTime 只在该字段为【零值】时才填充当前时间，显式给了非零值就会原样保留，
	// 所以不需要建完再 UpdateColumn 回写一次（那样反而更绕）。
	makeLog := func(daysAgo int) uint {
		t.Helper()
		status := model.LogStatusSuccess
		duration := 1.0
		at := now.AddDate(0, 0, -daysAgo)
		logRecord := &model.TaskLog{
			TaskID:    task.ID,
			Status:    &status,
			Duration:  &duration,
			StartedAt: at,
			EndedAt:   &at,
			CreatedAt: at,
			UpdatedAt: at,
		}
		if err := database.DB.Create(logRecord).Error; err != nil {
			t.Fatalf("create log: %v", err)
		}
		// 断言回写确实落库了。少了这一步，一旦 GORM 的行为变化导致 created_at 被顶成"此刻"，
		// 三条日志会全部落在近 7 天内，测试会以一种很难看懂的方式失败。
		var stored model.TaskLog
		if err := database.DB.First(&stored, logRecord.ID).Error; err != nil {
			t.Fatalf("reload log: %v", err)
		}
		if diff := stored.CreatedAt.Sub(at); diff > time.Minute || diff < -time.Minute {
			t.Fatalf("created_at not backdated: want ~%s, got %s", at, stored.CreatedAt)
		}
		return logRecord.ID
	}

	todayID := makeLog(0)
	threeDaysAgoID := makeLog(3)
	tenDaysAgoID := makeLog(10)

	fetchIDs := func(t *testing.T, query string) map[uint]bool {
		t.Helper()
		rec := performJSONRequest(
			engine,
			http.MethodGet,
			"/api/v1/logs"+query,
			`{}`,
			map[string]string{"Authorization": "Bearer " + token},
			"",
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("query %q: expected 200, got %d body=%s", query, rec.Code, rec.Body.String())
		}
		payload := decodeJSONMap(t, rec)
		rows, ok := payload["data"].([]interface{})
		if !ok {
			t.Fatalf("query %q: expected data array, got %#v", query, payload["data"])
		}
		ids := make(map[uint]bool, len(rows))
		for _, row := range rows {
			item, ok := row.(map[string]interface{})
			if !ok {
				continue
			}
			if id, ok := item["id"].(float64); ok {
				ids[uint(id)] = true
			}
		}
		return ids
	}

	// 正确的调用方式：percent-encode。RFC3339 的东八区偏移是 `+08:00`，
	// 而 `+` 在 query string 里是空格的转义 —— 不编码的话服务端会拿到
	// `2026-08-17T00:00:00 08:00`。前端走 axios params 会自动编码，这里对齐它。
	rfc := func(t time.Time) string { return url.QueryEscape(t.Format(time.RFC3339)) }
	// 不编码的原始写法，用来验证服务端对手工调用方的容错（parseQueryTime 会把空格换回 +）
	rfcRaw := func(t time.Time) string { return t.Format(time.RFC3339) }

	// 无筛选：三条都在
	all := fetchIDs(t, "")
	for _, id := range []uint{todayID, threeDaysAgoID, tenDaysAgoID} {
		if !all[id] {
			t.Fatalf("expected log %d in unfiltered result, got %v", id, all)
		}
	}

	// 近 7 天（含今天）：今天与 3 天前在，10 天前不在。
	// 起点取 6 天前的零点，与前端 DATE_RANGE_SHORTCUTS 的 last7 一致。
	start := time.Date(now.AddDate(0, 0, -6).Year(), now.AddDate(0, 0, -6).Month(), now.AddDate(0, 0, -6).Day(), 0, 0, 0, 0, now.Location())
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	last7 := fetchIDs(t, fmt.Sprintf("?start_time=%s&end_time=%s", rfc(start), rfc(end)))
	if !last7[todayID] {
		t.Fatalf("expected today's log in last-7-days result, got %v", last7)
	}
	if !last7[threeDaysAgoID] {
		t.Fatalf("expected 3-days-ago log in last-7-days result, got %v", last7)
	}
	if last7[tenDaysAgoID] {
		t.Fatalf("10-days-ago log must not appear in last-7-days result, got %v", last7)
	}

	// 只给 end_time：早于它的都在，今天的不在。
	// 用 4 天前的当天末尾做上界，验证「端点当天算在内」——如果服务端把上界当成
	// 当天零点，3 天前那条会被正确排除，但 4 天前那天的记录也会整天漏掉。
	onlyEnd := fetchIDs(t, "?end_time="+rfc(time.Date(now.AddDate(0, 0, -4).Year(), now.AddDate(0, 0, -4).Month(), now.AddDate(0, 0, -4).Day(), 23, 59, 59, 0, now.Location())))
	if onlyEnd[todayID] || onlyEnd[threeDaysAgoID] {
		t.Fatalf("end_time must exclude newer logs, got %v", onlyEnd)
	}
	if !onlyEnd[tenDaysAgoID] {
		t.Fatalf("expected 10-days-ago log with end_time only, got %v", onlyEnd)
	}

	// 只给 start_time：晚于它的都在
	onlyStart := fetchIDs(t, "?start_time="+rfc(now.AddDate(0, 0, -5)))
	if !onlyStart[todayID] || !onlyStart[threeDaysAgoID] {
		t.Fatalf("expected recent logs with start_time only, got %v", onlyStart)
	}
	if onlyStart[tenDaysAgoID] {
		t.Fatalf("start_time must exclude older logs, got %v", onlyStart)
	}

	// 未编码的 `+08:00`（手工 curl / 拼 URL 的脚本 / Open API 第三方调用方最容易这样写）
	// 到服务端会变成 `2026-08-17T00:00:00 08:00`。parseQueryTime 要把空格换回加号再解析，
	// 否则筛选条件会被静默忽略：接口照样 200、照样返回全量数据，只是过滤没生效。
	// 这个用例就是在守那条容错——去掉它，下面的断言会红。
	rawEncoded := fetchIDs(t, fmt.Sprintf("?start_time=%s&end_time=%s", rfcRaw(start), rfcRaw(end)))
	if rawEncoded[tenDaysAgoID] {
		t.Fatalf("unencoded RFC3339 offset must still filter, got %v", rawEncoded)
	}
	if !rawEncoded[todayID] || !rawEncoded[threeDaysAgoID] {
		t.Fatalf("unencoded RFC3339 offset dropped in-range logs, got %v", rawEncoded)
	}

	// 畸形参数被忽略而不是报错：日期筛选是附加能力，
	// 不该因为一个坏参数就让整个日志页打不开。
	garbage := fetchIDs(t, "?start_time=not-a-time&end_time=2026-13-45")
	for _, id := range []uint{todayID, threeDaysAgoID, tenDaysAgoID} {
		if !garbage[id] {
			t.Fatalf("malformed time params must be ignored, expected log %d, got %v", id, garbage)
		}
	}
}
