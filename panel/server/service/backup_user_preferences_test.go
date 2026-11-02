package service

import (
	"encoding/json"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestSnapshotConfigBundleKeepsUserPreferenceColumns 锁住用户偏好的备份导出（以及成对的恢复函数）。
//
// BackupUserPreference 是手写平铺的结构体，TestBackupPayloadModelsHaveNoJSONHiddenFields 那道反射护栏
// 只查嵌入的 model，管不到它 —— model.UserPreference 新加一列（这次是 list），
// 导出那边漏拷一个字段，编译、其余测试全绿，只有用户哪天真去翻备份才发现这一组偏好没了。
// 这里刻意经过一次真实的 JSON 往返，json tag 写错同样会挂。
//
// 注意：restoreUserPreferences 目前没有调用点（用户、2FA、偏好都只导出不恢复），
// 这里直接调它只是确认它与导出成对，不代表恢复链路已经接上。
func TestSnapshotConfigBundleKeepsUserPreferenceColumns(t *testing.T) {
	testutil.SetupTestEnv(t)

	const (
		editorJSON = `{"word_wrap":"off","minimap":true,"indent_guides":true,"whitespace":"all","indent_width":"4"}`
		listJSON   = `{"envs_page_size":"all","tasks_page_size":50,"tasks_view_all_hidden":true}`
	)
	if err := database.DB.Create(&model.UserPreference{UserID: 7, Editor: editorJSON, List: listJSON}).Error; err != nil {
		t.Fatalf("seed user preference: %v", err)
	}
	// 只存过编辑器偏好的用户：list 为空串，导出时整键省略，与老备份的形态一致。
	if err := database.DB.Create(&model.UserPreference{UserID: 8, Editor: editorJSON}).Error; err != nil {
		t.Fatalf("seed editor-only user preference: %v", err)
	}

	bundle, err := snapshotConfigBundle()
	if err != nil {
		t.Fatalf("snapshot config bundle: %v", err)
	}

	raw, err := json.Marshal(bundle.UserPreferences)
	if err != nil {
		t.Fatalf("marshal user preferences: %v", err)
	}
	var decoded []BackupUserPreference
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal user preferences: %v", err)
	}

	byUser := map[uint]BackupUserPreference{}
	for _, item := range decoded {
		byUser[item.UserID] = item
	}
	withList, ok := byUser[7]
	if !ok {
		t.Fatalf("user 7 preference missing from backup: %s", raw)
	}
	if withList.List != listJSON {
		t.Fatalf("备份导出必须原样保留 list 列\nwant %q\ngot  %q", listJSON, withList.List)
	}
	if withList.Editor != editorJSON {
		t.Fatalf("备份导出必须原样保留 editor 列\nwant %q\ngot  %q", editorJSON, withList.Editor)
	}
	if editorOnly, ok := byUser[8]; !ok || editorOnly.List != "" || editorOnly.Editor != editorJSON {
		t.Fatalf("editor-only preference exported wrongly: %#v", editorOnly)
	}
	if !strings.Contains(string(raw), `"list":`) {
		t.Fatalf("导出的 JSON 里应当带 list 键: %s", raw)
	}

	// 老备份（v3.3.1 之前）没有 list 键：反序列化成空串 = 一个键都没存过。
	var legacy []BackupUserPreference
	if err := json.Unmarshal([]byte(`[{"user_id":7,"editor":"{}"}]`), &legacy); err != nil {
		t.Fatalf("unmarshal legacy user preferences: %v", err)
	}
	if len(legacy) != 1 || legacy[0].List != "" {
		t.Fatalf("legacy backup without list key should decode to empty list, got %#v", legacy)
	}

	// 恢复函数与导出成对：用户主键按 userIDMap 重新映射，两列都要带过去。
	if err := database.DB.Exec("DELETE FROM user_preferences").Error; err != nil {
		t.Fatalf("clear user preferences: %v", err)
	}
	if err := restoreUserPreferences(database.DB, decoded, map[uint]uint{7: 70}); err != nil {
		t.Fatalf("restore user preferences: %v", err)
	}
	var restored []model.UserPreference
	if err := database.DB.Order("user_id ASC").Find(&restored).Error; err != nil {
		t.Fatalf("load restored user preferences: %v", err)
	}
	// user 8 在 userIDMap 里映射不到，应当整条跳过。
	if len(restored) != 1 || restored[0].UserID != 70 {
		t.Fatalf("expected only the mapped user to be restored, got %#v", restored)
	}
	if restored[0].List != listJSON || restored[0].Editor != editorJSON {
		t.Fatalf("restoreUserPreferences must carry both columns, got %#v", restored[0])
	}
}
