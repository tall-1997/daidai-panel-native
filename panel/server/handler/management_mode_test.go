package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestManagementAuthLoginDoesNotDispatchNotification(t *testing.T) {
	testutil.SetupTestEnv(t)
	if err := model.SetConfig("notify_on_login", "true"); err != nil {
		t.Fatal(err)
	}
	testutil.MustCreateLoginUser(t, "mobilelogin", "admin", "secret123")
	count := 0
	oldNotify := dispatchLoginNotification
	dispatchLoginNotification = func(string, string) { count++ }
	t.Cleanup(func() { dispatchLoginNotification = oldNotify })

	engine := gin.New()
	handler := NewManagementAuthHandler()
	engine.POST("/login", handler.Login)
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"mobilelogin","password":"secret123"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if count != 0 {
		t.Fatalf("management login dispatched %d notification(s)", count)
	}
}

func TestManagementTaskCreateSkipsSchedulerRegistration(t *testing.T) {
	testutil.SetupTestEnv(t)
	count := 0
	oldAdd := addTaskToScheduler
	addTaskToScheduler = func(*model.Task) { count++ }
	t.Cleanup(func() { addTaskToScheduler = oldAdd })

	engine := gin.New()
	handler := NewManagementTaskHandler()
	engine.POST("/tasks", handler.Create)
	request := httptest.NewRequest(http.MethodPost, "/tasks", strings.NewReader(`{"name":"safe","command":"echo safe","task_type":"manual"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if count != 0 {
		t.Fatalf("management task registered scheduler %d time(s)", count)
	}
}

func TestManagementTaskUpdateSkipsSchedulerRegistration(t *testing.T) {
	testutil.SetupTestEnv(t)
	task := model.Task{Name: "before", Command: "echo before", TaskType: model.TaskTypeManual, Status: model.TaskStatusDisabled}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	count := 0
	oldUpdate := updateTaskInScheduler
	updateTaskInScheduler = func(*model.Task) { count++ }
	t.Cleanup(func() { updateTaskInScheduler = oldUpdate })

	engine := gin.New()
	engine.PUT("/tasks/:id", NewManagementTaskHandler().Update)
	request := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/tasks/%d", task.ID), strings.NewReader(`{"name":"after"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || count != 0 {
		t.Fatalf("update status=%d scheduler calls=%d body=%s", recorder.Code, count, recorder.Body.String())
	}
}

func TestManagementTaskDeleteSkipsSchedulerRegistration(t *testing.T) {
	testutil.SetupTestEnv(t)
	task := model.Task{Name: "delete", Command: "echo delete", TaskType: model.TaskTypeManual, Status: model.TaskStatusDisabled}
	if err := database.DB.Create(&task).Error; err != nil {
		t.Fatal(err)
	}
	count := 0
	oldRemove := removeTaskFromScheduler
	removeTaskFromScheduler = func(uint) { count++ }
	t.Cleanup(func() { removeTaskFromScheduler = oldRemove })

	engine := gin.New()
	engine.DELETE("/tasks/:id", NewManagementTaskHandler().Delete)
	request := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/tasks/%d", task.ID), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || count != 0 {
		t.Fatalf("delete status=%d scheduler calls=%d body=%s", recorder.Code, count, recorder.Body.String())
	}
}
