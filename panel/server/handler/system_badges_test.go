package handler_test

import (
	"net/http"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestSystemBadgesScopesCountsByRole 守住角标接口的两条契约：
//  1. 五项计数都算对（运行中任务 / 今日失败日志 / 失败订阅 / 失败依赖 / 忙碌依赖）；
//  2. 按角色裁剪——viewer 看不到订阅与依赖的数字，operator 看不到依赖的数字。
//
// 第 2 条是这个接口区别于 /system/stats 的关键：侧栏菜单本身按角色过滤，
// 给看不到菜单的用户返回计数没有落点，还会让低权限账号推断出无权模块的运行状态。
// 一旦有人「顺手」把裁剪去掉，这个用例会红。
func TestSystemBadgesScopesCountsByRole(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()

	// —— 造数据 ——
	runningTask := &model.Task{
		Name:     "badge running task",
		Command:  "sleep 1",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusRunning,
	}
	if err := database.DB.Create(runningTask).Error; err != nil {
		t.Fatalf("create running task: %v", err)
	}
	idleTask := &model.Task{
		Name:     "badge idle task",
		Command:  "echo ok",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(idleTask).Error; err != nil {
		t.Fatalf("create idle task: %v", err)
	}

	now := time.Now()
	failedStatus := model.LogStatusFailed
	successStatus := model.LogStatusSuccess
	duration := 1.0
	for _, status := range []*int{&failedStatus, &successStatus} {
		logRecord := &model.TaskLog{
			TaskID:    runningTask.ID,
			Status:    status,
			Duration:  &duration,
			StartedAt: now.Add(-time.Minute),
			EndedAt:   &now,
		}
		if err := database.DB.Create(logRecord).Error; err != nil {
			t.Fatalf("create task log: %v", err)
		}
	}

	// 订阅：一条失败(status=1)、一条成功(status=0)
	for _, status := range []int{1, 0} {
		sub := &model.Subscription{
			Name:   "badge sub",
			Type:   model.SubTypeGitRepo,
			URL:    "https://example.com/repo.git",
			Status: status,
		}
		if err := database.DB.Create(sub).Error; err != nil {
			t.Fatalf("create subscription: %v", err)
		}
	}

	// 依赖：1 失败、1 排队、1 安装中、1 已装好（已装好的不该被算进任何一项）
	for _, status := range []string{
		model.DepStatusFailed,
		model.DepStatusQueued,
		model.DepStatusInstalling,
		model.DepStatusInstalled,
	} {
		dep := &model.Dependency{
			Type:   model.DepTypeNodeJS,
			Name:   "badge-dep-" + status,
			Status: status,
		}
		if err := database.DB.Create(dep).Error; err != nil {
			t.Fatalf("create dependency: %v", err)
		}
	}

	fetchBadges := func(t *testing.T, role string) map[string]interface{} {
		t.Helper()
		user := testutil.MustCreateUser(t, "badge-"+role, role)
		token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
		rec := performJSONRequest(
			engine,
			http.MethodGet,
			"/api/v1/system/badges",
			`{}`,
			map[string]string{"Authorization": "Bearer " + token},
			"",
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("[%s] expected 200, got %d body=%s", role, rec.Code, rec.Body.String())
		}
		payload := decodeJSONMap(t, rec)
		data, ok := payload["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("[%s] expected data object, got %#v", role, payload["data"])
		}
		return data
	}

	assertField := func(t *testing.T, role string, data map[string]interface{}, key string, want float64) {
		t.Helper()
		got, ok := data[key]
		if !ok {
			t.Fatalf("[%s] field %q missing from response: %#v", role, key, data)
		}
		if got != want {
			t.Fatalf("[%s] expected %s=%v, got %#v", role, key, want, got)
		}
	}

	// viewer：只拿到任务与日志，订阅/依赖一律 0
	viewerData := fetchBadges(t, "viewer")
	assertField(t, "viewer", viewerData, "tasks_running", 1)
	assertField(t, "viewer", viewerData, "logs_failed_today", 1)
	assertField(t, "viewer", viewerData, "subs_failed", 0)
	assertField(t, "viewer", viewerData, "deps_failed", 0)
	assertField(t, "viewer", viewerData, "deps_installing", 0)

	// operator：多拿到订阅，依赖仍为 0
	operatorData := fetchBadges(t, "operator")
	assertField(t, "operator", operatorData, "tasks_running", 1)
	assertField(t, "operator", operatorData, "logs_failed_today", 1)
	assertField(t, "operator", operatorData, "subs_failed", 1)
	assertField(t, "operator", operatorData, "deps_failed", 0)
	assertField(t, "operator", operatorData, "deps_installing", 0)

	// admin：全都拿得到；deps_installing 合并 queued + installing（+ removing），
	// 已安装的那条不能被算进去，所以是 2 而不是 3。
	adminData := fetchBadges(t, "admin")
	assertField(t, "admin", adminData, "tasks_running", 1)
	assertField(t, "admin", adminData, "logs_failed_today", 1)
	assertField(t, "admin", adminData, "subs_failed", 1)
	assertField(t, "admin", adminData, "deps_failed", 1)
	assertField(t, "admin", adminData, "deps_installing", 2)
}

// TestSystemBadgesExcludesYesterdayFailures 锁住「今日失败」角标的下界（issue #153 补索引时一并补的口径锁）：
// 昨天 23:59:59.999999999 失败的那次不算今天，今天零点整失败的那次算。
// 口径与仪表板的 today_* 相同（created_at >= 本地今天零点），加索引只改执行计划，不该改这个数。
func TestSystemBadgesExcludesYesterdayFailures(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "badge-yesterday-failure", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	task := &model.Task{
		Name:     "badge yesterday failure task",
		Command:  "echo ok",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 时间一律交给驱动按 time.Time 绑定：库里存的是带偏移的文本，手写日期字符串的零点语义和生产不一样。
	today := settledLocalToday(t)
	failedStatus := model.LogStatusFailed
	for _, createdAt := range []time.Time{today.Add(-time.Nanosecond), today} {
		logRecord := &model.TaskLog{
			TaskID:    task.ID,
			Status:    &failedStatus,
			StartedAt: createdAt,
			CreatedAt: createdAt,
		}
		if err := database.DB.Create(logRecord).Error; err != nil {
			t.Fatalf("create task log: %v", err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodGet,
		"/api/v1/system/badges",
		`{}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	data, ok := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got %s", rec.Body.String())
	}
	if got := data["logs_failed_today"]; got != float64(1) {
		t.Fatalf("昨天的失败不该算进今日失败角标，期望 1（只有今天零点整那条），实际 %#v", got)
	}
}
