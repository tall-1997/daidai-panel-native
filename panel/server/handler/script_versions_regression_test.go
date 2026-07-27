package handler_test

import (
	"net/http"
	"net/url"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestScriptClearVersionsOnlyRemovesSelectedScriptHistory(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-version-clear", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	records := []model.ScriptVersion{
		{ScriptPath: "folder/demo.js", Version: 1, Message: "v1", Content: "console.log('a')\n"},
		{ScriptPath: "folder/demo.js", Version: 2, Message: "v2", Content: "console.log('b')\n"},
		{ScriptPath: "folder/keep.js", Version: 1, Message: "keep", Content: "console.log('keep')\n"},
	}
	if err := database.DB.Create(&records).Error; err != nil {
		t.Fatalf("seed script versions: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodDelete,
		"/api/v1/scripts/versions?path="+url.QueryEscape("folder/demo.js"),
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	if got, _ := payload["cleared_count"].(float64); got != 2 {
		t.Fatalf("expected cleared_count=2, got %v", payload["cleared_count"])
	}

	var clearedCount int64
	if err := database.DB.Model(&model.ScriptVersion{}).
		Where("script_path = ?", "folder/demo.js").
		Count(&clearedCount).Error; err != nil {
		t.Fatalf("count cleared versions: %v", err)
	}
	if clearedCount != 0 {
		t.Fatalf("expected cleared versions to be removed, got %d", clearedCount)
	}

	var keptCount int64
	if err := database.DB.Model(&model.ScriptVersion{}).
		Where("script_path = ?", "folder/keep.js").
		Count(&keptCount).Error; err != nil {
		t.Fatalf("count kept versions: %v", err)
	}
	if keptCount != 1 {
		t.Fatalf("expected unrelated versions to remain, got %d", keptCount)
	}
}
