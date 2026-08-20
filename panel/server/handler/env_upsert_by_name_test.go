package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func upsertEnvByName(t *testing.T, token string, payload map[string]interface{}) (int, map[string]interface{}) {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal upsert payload: %v", err)
	}

	rec := performJSONRequest(newProtectedRouter(), http.MethodPut, "/api/v1/envs/by-name", string(body),
		map[string]string{"Authorization": "Bearer " + token}, "")
	return rec.Code, decodeJSONMap(t, rec)
}

func mustOperatorToken(t *testing.T, username string) string {
	t.Helper()

	user := testutil.MustCreateUser(t, username, "operator")
	return testutil.MustCreateAccessToken(t, user.Username, user.Role)
}

// 验收 A6（0 条分支）：不存在时创建。
func TestUpsertByNameCreatesWhenMissing(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-create")

	code, payload := upsertEnvByName(t, token, map[string]interface{}{
		"name":    "JD_COOKIE",
		"value":   "pt_key=abc;pt_pin=demo;",
		"remarks": "account-A",
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201 on create, got %d payload=%#v", code, payload)
	}
	if created, _ := payload["created"].(bool); !created {
		t.Fatalf("expected created=true, got %#v", payload["created"])
	}

	var rows []model.EnvVar
	if err := database.DB.Where("name = ?", "JD_COOKIE").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly one row after create, got %d", len(rows))
	}
	if rows[0].Value != "pt_key=abc;pt_pin=demo;" {
		t.Fatalf("unexpected stored value %q", rows[0].Value)
	}
	if rows[0].Remarks != "account-A" {
		t.Fatalf("unexpected stored remarks %q", rows[0].Remarks)
	}
	if !rows[0].Enabled {
		t.Fatalf("expected new env to default to enabled")
	}
}

// 验收 A6（1 条分支）：恰好一条时原地更新，不产生新行。
func TestUpsertByNameUpdatesSingleMatch(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-update")

	existing := &model.EnvVar{Name: "JD_COOKIE", Value: "old", Remarks: "account-A", Enabled: true, Position: 1000}
	if err := database.DB.Create(existing).Error; err != nil {
		t.Fatalf("seed env: %v", err)
	}

	code, payload := upsertEnvByName(t, token, map[string]interface{}{
		"name":  "JD_COOKIE",
		"value": "refreshed",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d payload=%#v", code, payload)
	}
	if created, _ := payload["created"].(bool); created {
		t.Fatalf("expected created=false on update, got %#v", payload["created"])
	}

	var rows []model.EnvVar
	if err := database.DB.Where("name = ?", "JD_COOKIE").Find(&rows).Error; err != nil {
		t.Fatalf("load rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected update to keep a single row, got %d", len(rows))
	}
	if rows[0].ID != existing.ID {
		t.Fatalf("expected same row id %d, got %d", existing.ID, rows[0].ID)
	}
	if rows[0].Value != "refreshed" {
		t.Fatalf("expected value refreshed, got %q", rows[0].Value)
	}
	// 没传的字段不许被清掉 —— 脚本只想换 value 时不该顺手抹掉 remarks。
	if rows[0].Remarks != "account-A" {
		t.Fatalf("expected remarks preserved, got %q", rows[0].Remarks)
	}
}

// 验收 A6（>1 条分支）：多账号场景必须报错，且报错时一条记录都不能被改。
// 这是数据安全防线：脚本读到的是合并且转义后的串，整段写回任意一条都会破坏结构。
func TestUpsertByNameRejectsAmbiguousMatchWithoutMutating(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-ambiguous")

	first := &model.EnvVar{Name: "JD_COOKIE", Value: "cookie-a", Remarks: "account-A", Enabled: true, Position: 1000}
	second := &model.EnvVar{Name: "JD_COOKIE", Value: "cookie-b", Remarks: "account-B", Enabled: true, Position: 2000}
	if err := database.DB.Create(first).Error; err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if err := database.DB.Create(second).Error; err != nil {
		t.Fatalf("seed second: %v", err)
	}

	var before []model.EnvVar
	if err := database.DB.Where("name = ?", "JD_COOKIE").Order("id ASC").Find(&before).Error; err != nil {
		t.Fatalf("snapshot rows: %v", err)
	}

	code, payload := upsertEnvByName(t, token, map[string]interface{}{
		"name":  "JD_COOKIE",
		"value": "cookie-a&cookie-b",
	})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 on ambiguous name, got %d payload=%#v", code, payload)
	}
	if message, _ := payload["error"].(string); message == "" {
		t.Fatalf("expected an actionable error message, got %#v", payload)
	}

	var after []model.EnvVar
	if err := database.DB.Where("name = ?", "JD_COOKIE").Order("id ASC").Find(&after).Error; err != nil {
		t.Fatalf("reload rows: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("expected row count to stay %d, got %d", len(before), len(after))
	}
	for i := range before {
		if after[i].ID != before[i].ID || after[i].Value != before[i].Value || after[i].Remarks != before[i].Remarks {
			t.Fatalf("row %d mutated on rejected upsert: before=%#v after=%#v", i, before[i], after[i])
		}
		if !after[i].UpdatedAt.Equal(before[i].UpdatedAt) {
			t.Fatalf("row %d updated_at moved on rejected upsert: before=%v after=%v",
				i, before[i].UpdatedAt, after[i].UpdatedAt)
		}
	}
}

// remarks 是同名多条时的消歧手段，与 `ddp env set --remarks` 一致。
func TestUpsertByNameDisambiguatesWithRemarks(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-remarks")

	first := &model.EnvVar{Name: "JD_COOKIE", Value: "cookie-a", Remarks: "account-A", Enabled: true, Position: 1000}
	second := &model.EnvVar{Name: "JD_COOKIE", Value: "cookie-b", Remarks: "account-B", Enabled: true, Position: 2000}
	if err := database.DB.Create(first).Error; err != nil {
		t.Fatalf("seed first: %v", err)
	}
	if err := database.DB.Create(second).Error; err != nil {
		t.Fatalf("seed second: %v", err)
	}

	code, payload := upsertEnvByName(t, token, map[string]interface{}{
		"name":    "JD_COOKIE",
		"value":   "cookie-b-refreshed",
		"remarks": "account-B",
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 when remarks disambiguates, got %d payload=%#v", code, payload)
	}

	var reloadedFirst, reloadedSecond model.EnvVar
	if err := database.DB.First(&reloadedFirst, first.ID).Error; err != nil {
		t.Fatalf("reload first: %v", err)
	}
	if err := database.DB.First(&reloadedSecond, second.ID).Error; err != nil {
		t.Fatalf("reload second: %v", err)
	}
	if reloadedFirst.Value != "cookie-a" {
		t.Fatalf("expected untargeted row untouched, got %q", reloadedFirst.Value)
	}
	if reloadedSecond.Value != "cookie-b-refreshed" {
		t.Fatalf("expected targeted row updated, got %q", reloadedSecond.Value)
	}
}

// 验收 A7：值逐字节原样写入。转义只发生在多条同名记录合并成环境变量时
// （joinTaskEnvValues），单条记录存的必须是原始串。
func TestUpsertByNameStoresValueVerbatim(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-verbatim")

	cases := []struct {
		name  string
		value string
	}{
		{name: "AMP_VALUE", value: "a&b&&c"},
		{name: "BACKSLASH_VALUE", value: `a\&b\\c`},
		{name: "JSON_ARRAY_VALUE", value: `["a","b"]`},
		{name: "MIXED_VALUE", value: "pt_key=a\\&b;pt_pin=x&y;"},
	}

	for _, tc := range cases {
		code, payload := upsertEnvByName(t, token, map[string]interface{}{
			"name":  tc.name,
			"value": tc.value,
		})
		if code != http.StatusCreated {
			t.Fatalf("%s: expected 201, got %d payload=%#v", tc.name, code, payload)
		}

		var created model.EnvVar
		if err := database.DB.Where("name = ?", tc.name).First(&created).Error; err != nil {
			t.Fatalf("%s: load created row: %v", tc.name, err)
		}
		if created.Value != tc.value {
			t.Fatalf("%s: expected verbatim value %q, got %q", tc.name, tc.value, created.Value)
		}

		// 二次 upsert（走更新分支）同样不得改写值。
		updated := tc.value + `&tail\`
		code, payload = upsertEnvByName(t, token, map[string]interface{}{
			"name":  tc.name,
			"value": updated,
		})
		if code != http.StatusOK {
			t.Fatalf("%s: expected 200 on update, got %d payload=%#v", tc.name, code, payload)
		}

		var reloaded model.EnvVar
		if err := database.DB.First(&reloaded, created.ID).Error; err != nil {
			t.Fatalf("%s: reload row: %v", tc.name, err)
		}
		if reloaded.Value != updated {
			t.Fatalf("%s: expected verbatim updated value %q, got %q", tc.name, updated, reloaded.Value)
		}
	}
}

// enabled 传什么就存什么：EnvVar.Enabled 带 `default:true`，稍不留神就会被 DB 默认值翻回 true。
func TestUpsertByNameHonoursExplicitEnabledFlag(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-enabled")

	code, payload := upsertEnvByName(t, token, map[string]interface{}{
		"name":    "DISABLED_ON_CREATE",
		"value":   "x",
		"enabled": false,
	})
	if code != http.StatusCreated {
		t.Fatalf("expected 201, got %d payload=%#v", code, payload)
	}

	var created model.EnvVar
	if err := database.DB.Where("name = ?", "DISABLED_ON_CREATE").First(&created).Error; err != nil {
		t.Fatalf("load created row: %v", err)
	}
	if created.Enabled {
		t.Fatalf("expected enabled=false to be honoured on create")
	}

	code, payload = upsertEnvByName(t, token, map[string]interface{}{
		"name":    "DISABLED_ON_CREATE",
		"value":   "x",
		"enabled": true,
	})
	if code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d payload=%#v", code, payload)
	}

	var reloaded model.EnvVar
	if err := database.DB.First(&reloaded, created.ID).Error; err != nil {
		t.Fatalf("reload row: %v", err)
	}
	if !reloaded.Enabled {
		t.Fatalf("expected enabled=true to be honoured on update")
	}
}

func TestUpsertByNameRejectsInvalidName(t *testing.T) {
	testutil.SetupTestEnv(t)
	token := mustOperatorToken(t, "env-by-name-invalid")

	for _, name := range []string{"", "  ", "1BAD", "has-dash"} {
		code, payload := upsertEnvByName(t, token, map[string]interface{}{"name": name, "value": "x"})
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for name %q, got %d payload=%#v", name, code, payload)
		}
	}

	var count int64
	database.DB.Model(&model.EnvVar{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected no rows created by rejected requests, got %d", count)
	}
}

// 验收 A8 回归：POST /envs 仍是纯 insert（青龙兼容），
// 两次同名产生两条；upsert 语义只存在于 by-name 入口。
func TestCreateStaysPureInsertAlongsideUpsertByName(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	token := mustOperatorToken(t, "env-pure-insert")

	for _, body := range []string{
		`{"name":"PURE_INSERT","value":"first"}`,
		`{"name":"PURE_INSERT","value":"second"}`,
	} {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/envs", body,
			map[string]string{"Authorization": "Bearer " + token}, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 for %s, got %d body=%s", body, rec.Code, rec.Body.String())
		}
	}

	var count int64
	database.DB.Model(&model.EnvVar{}).Where("name = ?", "PURE_INSERT").Count(&count)
	if count != 2 {
		t.Fatalf("expected POST /envs to stay pure insert (2 rows), got %d", count)
	}

	// 而 by-name 面对这两条同名记录必须拒绝，而不是随便挑一条改。
	code, payload := upsertEnvByName(t, token, map[string]interface{}{"name": "PURE_INSERT", "value": "third"})
	if code != http.StatusConflict {
		t.Fatalf("expected 409 from by-name upsert on duplicated rows, got %d payload=%#v", code, payload)
	}
}
