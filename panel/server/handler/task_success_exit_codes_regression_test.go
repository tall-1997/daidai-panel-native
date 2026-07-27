package handler_test

import (
	"net/http"
	"strconv"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestTaskSuccessExitCodesCreateUpdateCopyAndExport(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "success-exit-codes-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	createRec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"legacy exit task","command":"task legacy.js","task_type":"manual","success_exit_codes":"0，1,1"}`,
		headers,
		"",
	)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d: %s", createRec.Code, createRec.Body.String())
	}
	created := decodeJSONMap(t, createRec)["data"].(map[string]interface{})
	if got := created["success_exit_codes"]; got != "0,1" {
		t.Fatalf("expected normalized success_exit_codes=0,1, got %#v", got)
	}
	taskID := uint(created["id"].(float64))

	resetRec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/"+strconv.FormatUint(uint64(taskID), 10),
		`{"success_exit_codes":null}`,
		headers,
		"",
	)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected null reset 200, got %d: %s", resetRec.Code, resetRec.Body.String())
	}
	resetTask := decodeJSONMap(t, resetRec)["data"].(map[string]interface{})
	if got := resetTask["success_exit_codes"]; got != "0" {
		t.Fatalf("expected null success_exit_codes to reset to 0, got %#v", got)
	}

	updateRec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/tasks/"+strconv.FormatUint(uint64(taskID), 10),
		`{"success_exit_codes":"0 2"}`,
		headers,
		"",
	)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d: %s", updateRec.Code, updateRec.Body.String())
	}
	updated := decodeJSONMap(t, updateRec)["data"].(map[string]interface{})
	if got := updated["success_exit_codes"]; got != "0,2" {
		t.Fatalf("expected updated success_exit_codes=0,2, got %#v", got)
	}

	copyRec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks/"+strconv.FormatUint(uint64(taskID), 10)+"/copy",
		`{}`,
		headers,
		"",
	)
	if copyRec.Code != http.StatusCreated {
		t.Fatalf("expected copy 201, got %d: %s", copyRec.Code, copyRec.Body.String())
	}
	copied := decodeJSONMap(t, copyRec)["data"].(map[string]interface{})
	if got := copied["success_exit_codes"]; got != "0,2" {
		t.Fatalf("expected copied success_exit_codes=0,2, got %#v", got)
	}

	exportRec := performRequest(engine, http.MethodGet, "/api/v1/tasks/export", headers)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("expected export 200, got %d: %s", exportRec.Code, exportRec.Body.String())
	}
	exportedTasks := decodeJSONMap(t, exportRec)["data"].([]interface{})
	foundExportedTask := false
	for _, item := range exportedTasks {
		exported := item.(map[string]interface{})
		if exported["name"] != "legacy exit task" {
			continue
		}
		foundExportedTask = true
		if exported["success_exit_codes"] != "0,2" {
			t.Fatalf("expected exported success_exit_codes=0,2, got %#v", exported["success_exit_codes"])
		}
	}
	if !foundExportedTask {
		t.Fatal("expected exported task to be present")
	}

	var stored model.Task
	if err := database.DB.First(&stored, taskID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if stored.SuccessExitCodes != "0,2" {
		t.Fatalf("expected stored success_exit_codes=0,2, got %q", stored.SuccessExitCodes)
	}
}

func TestTaskSuccessExitCodesRejectInvalidValueAndImportValidValue(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "success-exit-codes-importer", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	invalidRec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks",
		`{"name":"invalid exit task","command":"task invalid.js","task_type":"manual","success_exit_codes":"0,-1"}`,
		headers,
		"",
	)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid create 400, got %d: %s", invalidRec.Code, invalidRec.Body.String())
	}

	importRec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/tasks/import",
		`{"tasks":[{"name":"imported exit task","command":"task imported.js","task_type":"manual","success_exit_codes":"0,1"}]}`,
		headers,
		"",
	)
	if importRec.Code != http.StatusCreated {
		t.Fatalf("expected import 201, got %d: %s", importRec.Code, importRec.Body.String())
	}

	var imported model.Task
	if err := database.DB.Where("name = ?", "imported exit task").First(&imported).Error; err != nil {
		t.Fatalf("load imported task: %v", err)
	}
	if imported.SuccessExitCodes != "0,1" {
		t.Fatalf("expected imported success_exit_codes=0,1, got %q", imported.SuccessExitCodes)
	}
}
