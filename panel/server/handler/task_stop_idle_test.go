package handler_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// 停止一个没在排队、也没在运行的任务，什么都不能改（S3 复查的 PLAUSIBLE）。
// 原来不管任务在不在跑都会把 last_run_status 写成「已终止」：网页只在运行中才显示停止按钮，
// 可 MCP 的 stop_task 能对任意 id 调，一调空闲任务的「上次结果」就被改掉了。
// 排队中 / 运行中的停止语义由 TestStopQueuedTaskRestoresItsSwitchState、TestStopTaskMarksRunningLogAborted 钉住。
func TestStopIdleTaskChangesNothing(t *testing.T) {
	testutil.SetupTestEnv(t)
	service.ShutdownSchedulerV2()

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "stop-idle-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	success := model.RunSuccess
	failed := model.RunFailed
	cases := []struct {
		name          string
		status        float64
		lastRunStatus *int
	}{
		{name: "空闲的启用任务-上次成功", status: model.TaskStatusEnabled, lastRunStatus: &success},
		{name: "空闲的禁用任务-上次失败", status: model.TaskStatusDisabled, lastRunStatus: &failed},
		{name: "空闲的启用任务-从未运行", status: model.TaskStatusEnabled, lastRunStatus: nil},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			task := mustCreateTaskWithStatus(t, testCase.name, model.TaskTypeCron, "0 0 * * *", testCase.status)
			updates := map[string]interface{}{
				"last_run_at":       time.Now().Add(-time.Hour),
				"last_running_time": 12.5,
			}
			if testCase.lastRunStatus != nil {
				updates["last_run_status"] = *testCase.lastRunStatus
			}
			if err := database.DB.Model(&model.Task{}).Where("id = ?", task.ID).Updates(updates).Error; err != nil {
				t.Fatalf("seed last run: %v", err)
			}
			before := reloadTaskRow(t, task.ID)

			rec := performRequest(engine, http.MethodPut, taskActionPath(task, "stop"), headers)
			if rec.Code != http.StatusOK {
				t.Fatalf("停止空闲任务应返回 200，实际 %d: %s", rec.Code, rec.Body.String())
			}

			// 库里的状态是契约本身，先核对它们；提示文案放在最后。
			after := reloadTaskRow(t, task.ID)
			if after.Status != before.Status {
				t.Errorf("停止空闲任务不能改 status：之前 %v，之后 %v", before.Status, after.Status)
			}
			if !sameIntPointer(after.LastRunStatus, before.LastRunStatus) {
				t.Errorf("停止空闲任务不能改「上次结果」：之前 %s，之后 %s",
					describeIntPointer(before.LastRunStatus), describeIntPointer(after.LastRunStatus))
			}
			if after.LastRunningTime == nil || *after.LastRunningTime != 12.5 {
				t.Errorf("停止空闲任务不能改上次耗时，实际 %#v", after.LastRunningTime)
			}
			if message, _ := decodeJSONMap(t, rec)["message"].(string); message != "任务未在运行" {
				t.Errorf("停止空闲任务应提示「任务未在运行」，实际 %q", message)
			}
		})
	}
}

func sameIntPointer(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func describeIntPointer(value *int) string {
	if value == nil {
		return "<nil>"
	}
	return strconv.Itoa(*value)
}
