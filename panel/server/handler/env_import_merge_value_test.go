package handler_test

import (
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// #161：merge_value 按「名称 + 值」认同一条。备注相同、值不同的要各自新建（merge 会把它们合成一条）；
// 命中时备注有键才写、重要只升不降、启用状态按文件写；同一文件里重复的「名称 + 值」只落一条。
func TestImportMergeValueMatchesOnNameAndValue(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-import-merge-value", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	mustCreateEnvVars(t,
		&model.EnvVar{Name: "TOKEN", Value: "a", Remarks: "old", Group: "g0", Enabled: true, Position: 1000, Important: true},
		&model.EnvVar{Name: "KEEP_REMARK", Value: "k", Remarks: "keep", Enabled: true, Position: 2000},
	)

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import",
		`{"mode":"merge_value","envs":[`+
			`{"name":"TOKEN","value":"a","remarks":"new","group":"g1","enabled":false,"important":false},`+
			`{"name":"TOKEN","value":"b","remarks":"new"},`+
			`{"name":"TOKEN","value":"c","remarks":"new"},`+
			`{"name":"TOKEN","value":"c","remarks":"dup"},`+
			`{"name":"KEEP_REMARK","value":"k","important":true},`+
			`{"name":"NEW_OFF","value":"x","status":1}]}`, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["created"] != float64(3) || body["updated"] != float64(3) {
		t.Fatalf("应新建 3 条（TOKEN=b、TOKEN=c、NEW_OFF）、更新 3 条，实际 %s", rec.Body.String())
	}
	if body["message"] != "成功导入 6 个环境变量（新建 3 个，更新 3 个）" {
		t.Fatalf("message 不对，实际 %v", body["message"])
	}

	var envs []model.EnvVar
	if err := database.DB.Order("id ASC").Find(&envs).Error; err != nil {
		t.Fatalf("list envs: %v", err)
	}
	if len(envs) != 5 {
		t.Fatalf("库里应有 5 条（同文件重复的 TOKEN=c 只落一条），实际 %d: %+v", len(envs), envs)
	}
	byKey := map[string]model.EnvVar{}
	for _, env := range envs {
		byKey[env.Name+"="+env.Value] = env
	}

	hit := byKey["TOKEN=a"]
	if hit.Remarks != "new" || hit.Group != "g1" || hit.Enabled || !hit.Important {
		t.Fatalf("命中时应更新备注 / 分组 / 禁用，重要不能被降级，实际 %+v", hit)
	}
	if dup := byKey["TOKEN=c"]; dup.Remarks != "dup" {
		t.Fatalf("同文件重复的第二条应命中第一条并更新备注，实际 %+v", dup)
	}
	if keep := byKey["KEEP_REMARK=k"]; keep.Remarks != "keep" || !keep.Important {
		t.Fatalf("没有 remarks 键不能清空备注，important:true 应升级，实际 %+v", keep)
	}
	if _, ok := byKey["TOKEN=b"]; !ok {
		t.Fatalf("值不同应新建 TOKEN=b")
	}
	if off := byKey["NEW_OFF=x"]; off.Enabled {
		t.Fatalf("新建行应保住禁用状态，实际 %+v", off)
	}
}

// merge 有命中时 message 同样带计数；全是新建时 message 保持原样，不追加括号。
func TestImportMessageAppendsCountsOnlyWhenUpdated(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-import-counts", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import",
		`{"envs":[{"name":"A","value":"1"},{"name":"B","value":"2"}]}`, headers, "")
	body := decodeJSONMap(t, rec)
	if rec.Code != http.StatusCreated || body["message"] != "成功导入 2 个环境变量" || body["created"] != float64(2) || body["updated"] != float64(0) {
		t.Fatalf("全新建时 message 不追加计数，实际 %d %s", rec.Code, rec.Body.String())
	}

	// mode 为空默认 merge：A 按「名称 + 备注」命中更新值，C 新建
	rec = performJSONRequest(engine, http.MethodPost, "/api/v1/envs/import",
		`{"envs":[{"name":"A","value":"9"},{"name":"C","value":"3"}]}`, headers, "")
	body = decodeJSONMap(t, rec)
	if rec.Code != http.StatusCreated || body["message"] != "成功导入 2 个环境变量（新建 1 个，更新 1 个）" {
		t.Fatalf("merge 命中时 message 应带计数，实际 %d %s", rec.Code, rec.Body.String())
	}
}
