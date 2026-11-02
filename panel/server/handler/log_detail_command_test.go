package handler_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// 日志详情必须带上任务的 command。
//
// APP 的日志详情页要靠它反推「这条日志跑的是哪个脚本」，再跳到脚本编辑页
// （Dumb-Panel-APP issue #5）。这条信息在别处一律拿不到：
//   - 面板没有 GET /tasks/:id 这条路由；
//   - 日志正文的头部只有 `=== 开始执行 [时间] ===`，不含命令行；
//   - log_path 是 task_<id>_<任务名>/<时间戳>.log，脚本路径不在里面。
//
// 所以删掉这个字段等于把 APP 那个入口一起删掉。
func TestLogDetailIncludesTaskCommand(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "log-detail-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	task := model.Task{
		Name:           "京东签到任务",
		Command:        "task jd/sign.py",
		CronExpression: "0 0 * * *",
		Status:         model.TaskStatusEnabled,
	}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	successStatus := model.LogStatusSuccess
	taskLog := model.TaskLog{
		TaskID:    task.ID,
		Content:   "=== 开始执行 ===",
		Status:    &successStatus,
		StartedAt: time.Now(),
	}
	if err := database.DB.Create(&taskLog).Error; err != nil {
		t.Fatalf("create log: %v", err)
	}

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/logs/%d", taskLog.ID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	// 日志详情走的是 response.Success(c, result)，body 就是 ToDict() 那张 map 本身，
	// 没有再套一层 data。
	data := decodeJSONMap(t, rec)

	if data["command"] != task.Command {
		t.Fatalf("expected command %q, got %#v", task.Command, data["command"])
	}
	if data["task_name"] != task.Name {
		t.Fatalf("expected task_name %q, got %#v", task.Name, data["task_name"])
	}
}
