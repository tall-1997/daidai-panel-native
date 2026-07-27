package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func createTaskView(t *testing.T, name string) model.TaskView {
	t.Helper()
	view := model.TaskView{
		Name:      name,
		Filters:   `[{"field":"name","operator":"contains","value":"` + name + `"}]`,
		SortRules: `[]`,
	}
	if err := database.DB.Create(&view).Error; err != nil {
		t.Fatalf("seed task view %q: %v", name, err)
	}
	return view
}

func TestListViewsHonorsSortOrderAndReturnsHidden(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "view-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	alpha := createTaskView(t, "alpha")
	beta := createTaskView(t, "beta")
	gamma := createTaskView(t, "gamma")

	// alpha → 2, beta → 0 (tie-break by id), gamma → 1, beta hidden.
	database.DB.Model(&model.TaskView{}).Where("id = ?", alpha.ID).Updates(map[string]interface{}{"sort_order": 2})
	database.DB.Model(&model.TaskView{}).Where("id = ?", beta.ID).Updates(map[string]interface{}{"sort_order": 0, "hidden": true})
	database.DB.Model(&model.TaskView{}).Where("id = ?", gamma.ID).Updates(map[string]interface{}{"sort_order": 1})

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/views", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list views status: %d, body=%s", rec.Code, rec.Body.String())
	}

	var raw []interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode list response: %v (body=%s)", err, rec.Body.String())
	}
	if len(raw) != 3 {
		t.Fatalf("expected 3 views including hidden, got %d", len(raw))
	}

	names := make([]string, len(raw))
	hiddenFor := make(map[string]bool)
	for i, item := range raw {
		entry, _ := item.(map[string]interface{})
		names[i], _ = entry["name"].(string)
		hiddenFor[names[i]], _ = entry["hidden"].(bool)
	}

	wantOrder := []string{"beta", "gamma", "alpha"}
	for i, want := range wantOrder {
		if names[i] != want {
			t.Fatalf("order mismatch at position %d: want %q, got %q (names=%v)", i, want, names[i], names)
		}
	}
	if !hiddenFor["beta"] {
		t.Fatalf("expected beta to be hidden, got map=%v", hiddenFor)
	}
	if hiddenFor["alpha"] || hiddenFor["gamma"] {
		t.Fatalf("expected alpha/gamma visible, got map=%v", hiddenFor)
	}
}

func TestReorderViewsUpdatesOrderAndHiddenFlag(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "view-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	alpha := createTaskView(t, "alpha")
	beta := createTaskView(t, "beta")
	gamma := createTaskView(t, "gamma")

	body := fmt.Sprintf(`{
		"views": [
			{"id": %d, "sort_order": 10, "hidden": false},
			{"id": %d, "sort_order": 20, "hidden": true},
			{"id": %d, "sort_order": 5, "hidden": false}
		]
	}`, alpha.ID, beta.ID, gamma.ID)

	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/views/reorder", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status: %d, body=%s", rec.Code, rec.Body.String())
	}

	loadView := func(id uint) model.TaskView {
		t.Helper()
		var out model.TaskView
		if err := database.DB.Where("id = ?", id).First(&out).Error; err != nil {
			t.Fatalf("load view %d: %v", id, err)
		}
		return out
	}

	alphaStored := loadView(alpha.ID)
	if alphaStored.SortOrder != 10 {
		t.Fatalf("alpha sort_order: want 10, got %d", alphaStored.SortOrder)
	}
	if alphaStored.Hidden {
		t.Fatalf("alpha should remain visible")
	}

	betaStored := loadView(beta.ID)
	if !betaStored.Hidden {
		t.Fatalf("beta should be hidden, got visible (body=%s)", rec.Body.String())
	}
	if betaStored.SortOrder != 20 {
		t.Fatalf("beta sort_order: want 20, got %d", betaStored.SortOrder)
	}

	gammaStored := loadView(gamma.ID)
	if gammaStored.SortOrder != 5 {
		t.Fatalf("gamma sort_order: want 5, got %d", gammaStored.SortOrder)
	}

	// Response echoes the new ordering; top-level is not wrapped in {data:}.
	var payload map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	respViews, _ := payload["views"].([]interface{})
	if len(respViews) != 3 {
		t.Fatalf("expected 3 views in response, got %d (body=%s)", len(respViews), rec.Body.String())
	}
	first, _ := respViews[0].(map[string]interface{})
	if name, _ := first["name"].(string); name != "gamma" {
		t.Fatalf("expected gamma first after reorder, got %q", name)
	}
}

func TestReorderViewsLeavesOmittedRowsUntouched(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "view-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	alpha := createTaskView(t, "alpha")
	beta := createTaskView(t, "beta")
	database.DB.Model(&model.TaskView{}).Where("id = ?", beta.ID).Updates(map[string]interface{}{"sort_order": 99, "hidden": true})

	body := fmt.Sprintf(`{"views": [{"id": %d, "sort_order": 3}]}`, alpha.ID)
	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/tasks/views/reorder", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reorder status: %d, body=%s", rec.Code, rec.Body.String())
	}

	var stored model.TaskView
	if err := database.DB.First(&stored, beta.ID).Error; err != nil {
		t.Fatalf("load beta: %v", err)
	}
	if stored.SortOrder != 99 || !stored.Hidden {
		t.Fatalf("expected beta untouched, got sort=%d hidden=%v", stored.SortOrder, stored.Hidden)
	}
}

func TestUpdateViewAcceptsPointerFields(t *testing.T) {
	testutil.SetupTestEnv(t)

	admin := testutil.MustCreateUser(t, "view-admin", "admin")
	token := testutil.MustCreateAccessToken(t, admin.Username, admin.Role)

	view := createTaskView(t, "alpha")

	// Toggle hidden without touching other fields.
	body := `{"hidden": true, "sort_order": 42}`
	engine := newProtectedRouter()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/tasks/views/%d", view.ID), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update status: %d, body=%s", rec.Code, rec.Body.String())
	}

	var stored model.TaskView
	if err := database.DB.First(&stored, view.ID).Error; err != nil {
		t.Fatalf("load stored: %v", err)
	}
	if !stored.Hidden || stored.SortOrder != 42 {
		t.Fatalf("expected hidden=true sort_order=42, got hidden=%v sort_order=%d", stored.Hidden, stored.SortOrder)
	}
	if stored.Name != "alpha" {
		t.Fatalf("expected name unchanged, got %q", stored.Name)
	}

	// Raw body with an empty filters string must not overwrite existing JSON.
	body = `{"filters": ""}`
	req = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/v1/tasks/views/%d", view.ID), bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	_ = json.Unmarshal(rec.Body.Bytes(), &map[string]interface{}{})
	if err := database.DB.First(&stored, view.ID).Error; err != nil {
		t.Fatalf("reload stored: %v", err)
	}
	if stored.Filters == "" {
		t.Fatalf("expected filters preserved when body sets empty string, got empty")
	}
}
