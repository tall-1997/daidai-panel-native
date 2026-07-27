package handler_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestEnvBatchSetGroupUpdatesSelectedRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "FOO", Value: "1", Enabled: true, Position: 1000},
		{Name: "BAR", Value: "2", Enabled: true, Position: 2000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/envs/batch/group",
		fmt.Sprintf(`{"ids":[%d,%d],"group":"release"}`, envs[0].ID, envs[1].ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	for _, env := range envs {
		var current model.EnvVar
		if err := database.DB.First(&current, env.ID).Error; err != nil {
			t.Fatalf("reload env %d: %v", env.ID, err)
		}
		if current.Group != "release" {
			t.Fatalf("expected env %d group release, got %q", env.ID, current.Group)
		}
	}
}

func TestEnvBatchSetGroupAcceptsMultipleGroups(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-multi-group-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	env := &model.EnvVar{Name: "FOO", Value: "1", Enabled: true, Position: 1000}
	if err := database.DB.Create(env).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/envs/batch/group",
		fmt.Sprintf(`{"ids":[%d],"groups":["release","prod","release"]}`, env.ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var current model.EnvVar
	if err := database.DB.First(&current, env.ID).Error; err != nil {
		t.Fatalf("reload env: %v", err)
	}
	if current.Group != "release,prod" {
		t.Fatalf("expected normalized group release,prod, got %q", current.Group)
	}
}

func TestEnvListSupportsMultipleGroupFilters(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-multi-filter-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "PROD_ONLY", Value: "1", Group: "prod", Enabled: true, Position: 1000},
		{Name: "DEV_AND_PROD", Value: "2", Group: "dev,prod", Enabled: true, Position: 2000},
		{Name: "STAGE_ONLY", Value: "3", Group: "stage", Enabled: true, Position: 3000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/envs?groups=dev,prod", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 envs for dev/prod filter, got %#v", payload["data"])
	}

	gotNames := make(map[string]struct{}, len(items))
	for _, item := range items {
		env, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("expected env object, got %#v", item)
		}
		gotNames[env["name"].(string)] = struct{}{}
		groups, ok := env["groups"].([]interface{})
		if !ok || len(groups) == 0 {
			t.Fatalf("expected groups array in response, got %#v", env["groups"])
		}
	}
	if _, exists := gotNames["PROD_ONLY"]; !exists {
		t.Fatalf("expected PROD_ONLY in filter result, got %v", gotNames)
	}
	if _, exists := gotNames["DEV_AND_PROD"]; !exists {
		t.Fatalf("expected DEV_AND_PROD in filter result, got %v", gotNames)
	}
	if _, exists := gotNames["STAGE_ONLY"]; exists {
		t.Fatalf("did not expect STAGE_ONLY in filter result, got %v", gotNames)
	}
}

func TestEnvGroupsSplitsStoredMultiGroups(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-group-list-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "A", Value: "1", Group: "prod,dev", Enabled: true, Position: 1000},
		{Name: "B", Value: "2", Group: "prod", Enabled: true, Position: 2000},
		{Name: "C", Value: "3", Group: "stage", Enabled: true, Position: 3000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/envs/groups", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok {
		t.Fatalf("expected group list, got %#v", payload["data"])
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected string group, got %#v", item)
		}
		got = append(got, text)
	}
	expected := []string{"dev", "prod", "stage"}
	if strings.Join(got, ",") != strings.Join(expected, ",") {
		t.Fatalf("expected groups %v, got %v", expected, got)
	}
}

func TestEnvListSupportsEnabledFilter(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-filter-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "ENABLED_ENV", Value: "1", Enabled: true, Position: 1000},
		{Name: "DISABLED_ENV", Value: "2", Enabled: true, Position: 2000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}
	if err := database.DB.Model(envs[1]).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable env %q: %v", envs[1].Name, err)
	}

	enabledRec := performRequest(engine, http.MethodGet, "/api/v1/envs?enabled=true", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if enabledRec.Code != http.StatusOK {
		t.Fatalf("expected enabled filter 200, got %d, body=%s", enabledRec.Code, enabledRec.Body.String())
	}

	enabledPayload := decodeJSONMap(t, enabledRec)
	enabledItems, ok := enabledPayload["data"].([]interface{})
	if !ok || len(enabledItems) != 1 {
		t.Fatalf("expected 1 enabled env, got %#v", enabledPayload["data"])
	}
	enabledItem, ok := enabledItems[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected enabled env object, got %#v", enabledItems[0])
	}
	if got, _ := enabledItem["name"].(string); got != "ENABLED_ENV" {
		t.Fatalf("expected ENABLED_ENV, got %q", got)
	}
	if got, _ := enabledPayload["total"].(float64); got != 1 {
		t.Fatalf("expected enabled total 1, got %v", got)
	}

	disabledRec := performRequest(engine, http.MethodGet, "/api/v1/envs?enabled=false", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if disabledRec.Code != http.StatusOK {
		t.Fatalf("expected disabled filter 200, got %d, body=%s", disabledRec.Code, disabledRec.Body.String())
	}

	disabledPayload := decodeJSONMap(t, disabledRec)
	disabledItems, ok := disabledPayload["data"].([]interface{})
	if !ok || len(disabledItems) != 1 {
		t.Fatalf("expected 1 disabled env, got %#v", disabledPayload["data"])
	}
	disabledItem, ok := disabledItems[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected disabled env object, got %#v", disabledItems[0])
	}
	if got, _ := disabledItem["name"].(string); got != "DISABLED_ENV" {
		t.Fatalf("expected DISABLED_ENV, got %q", got)
	}
	if got, _ := disabledPayload["total"].(float64); got != 1 {
		t.Fatalf("expected disabled total 1, got %v", got)
	}
}

func TestEnvBatchRenameUpdatesSelectedRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-rename-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "JD_COOKIE_ALPHA", Value: "1", Enabled: true, Position: 1000},
		{Name: "JD_COOKIE_BETA", Value: "2", Enabled: true, Position: 2000},
		{Name: "JD_COOKIE_GAMMA", Value: "3", Enabled: true, Position: 3000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/envs/batch/rename",
		fmt.Sprintf(`{"ids":[%d,%d],"search":"COOKIE","replace":"TOKEN"}`, envs[0].ID, envs[1].ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	expectedNames := map[uint]string{
		envs[0].ID: "JD_TOKEN_ALPHA",
		envs[1].ID: "JD_TOKEN_BETA",
		envs[2].ID: "JD_COOKIE_GAMMA",
	}
	for _, env := range envs {
		var current model.EnvVar
		if err := database.DB.First(&current, env.ID).Error; err != nil {
			t.Fatalf("reload env %d: %v", env.ID, err)
		}
		if current.Name != expectedNames[env.ID] {
			t.Fatalf("expected env %d name %q, got %q", env.ID, expectedNames[env.ID], current.Name)
		}
	}
}

func TestEnvBatchRenameRejectsInvalidReplacement(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-rename-invalid", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	env := &model.EnvVar{Name: "VALID_NAME", Value: "1", Enabled: true, Position: 1000}
	if err := database.DB.Create(env).Error; err != nil {
		t.Fatalf("create env: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/envs/batch/rename",
		fmt.Sprintf(`{"ids":[%d],"search":"VALID","replace":"INVALID-NAME"}`, env.ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "格式无效") {
		t.Fatalf("expected invalid format error, got %s", rec.Body.String())
	}

	var current model.EnvVar
	if err := database.DB.First(&current, env.ID).Error; err != nil {
		t.Fatalf("reload env: %v", err)
	}
	if current.Name != "VALID_NAME" {
		t.Fatalf("expected env name to remain unchanged, got %q", current.Name)
	}
}

func TestEnvSortToFirstKeepsItemUnpinned(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "ALPHA", Value: "1", Enabled: true, SortOrder: 0, Position: 1000},
		{Name: "BETA", Value: "2", Enabled: true, SortOrder: 0, Position: 2000},
		{Name: "GAMMA", Value: "3", Enabled: true, SortOrder: 0, Position: 3000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/envs/sort",
		fmt.Sprintf(`{"source_id":%d,"target_id":%d}`, envs[2].ID, envs[0].ID),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	var moved model.EnvVar
	if err := database.DB.First(&moved, envs[2].ID).Error; err != nil {
		t.Fatalf("reload moved env: %v", err)
	}
	if moved.SortOrder != 0 {
		t.Fatalf("expected moved env to remain unpinned, got sort_order=%d", moved.SortOrder)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/envs", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listRec.Code, listRec.Body.String())
	}

	payload := decodeJSONMap(t, listRec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) < 3 {
		t.Fatalf("expected env list with at least 3 items, got %#v", payload["data"])
	}

	first, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected first env object, got %#v", items[0])
	}
	if got, _ := first["name"].(string); got != "GAMMA" {
		t.Fatalf("expected GAMMA to be first after sort, got %q", got)
	}
	if sortOrder, _ := first["sort_order"].(float64); sortOrder != 0 {
		t.Fatalf("expected first env to be unpinned after drag, got sort_order=%v", sortOrder)
	}
}

func TestEnvMoveTopAndCancelTopUseExplicitPinnedState(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "LEFT", Value: "1", Enabled: true, SortOrder: 0, Position: 1000},
		{Name: "RIGHT", Value: "2", Enabled: true, SortOrder: 0, Position: 2000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	moveTopRec := performRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d/move-top", envs[1].ID), map[string]string{
		"Authorization": "Bearer " + token,
	})
	if moveTopRec.Code != http.StatusOK {
		t.Fatalf("expected move-top 200, got %d, body=%s", moveTopRec.Code, moveTopRec.Body.String())
	}

	var pinned model.EnvVar
	if err := database.DB.First(&pinned, envs[1].ID).Error; err != nil {
		t.Fatalf("reload pinned env: %v", err)
	}
	if pinned.SortOrder != 1 {
		t.Fatalf("expected sort_order=1 after move-top, got %d", pinned.SortOrder)
	}

	cancelTopRec := performRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d/cancel-top", envs[1].ID), map[string]string{
		"Authorization": "Bearer " + token,
	})
	if cancelTopRec.Code != http.StatusOK {
		t.Fatalf("expected cancel-top 200, got %d, body=%s", cancelTopRec.Code, cancelTopRec.Body.String())
	}

	if err := database.DB.First(&pinned, envs[1].ID).Error; err != nil {
		t.Fatalf("reload unpinned env: %v", err)
	}
	if pinned.SortOrder != 0 {
		t.Fatalf("expected sort_order=0 after cancel-top, got %d", pinned.SortOrder)
	}
}

func TestEnvExportAllHonorsSelectedIDs(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-operator", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "ALPHA", Value: "1", Enabled: true, Position: 1000},
		{Name: "BETA", Value: "2", Enabled: true, Position: 2000},
		{Name: "GAMMA", Value: "3", Enabled: false, Position: 3000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performRequest(
		engine,
		http.MethodGet,
		fmt.Sprintf("/api/v1/envs/export-all?ids=%d,%d", envs[0].ID, envs[2].ID),
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	items, ok := payload["data"].([]interface{})
	if !ok || len(items) != 2 {
		t.Fatalf("expected 2 exported envs, got %#v", payload["data"])
	}

	gotNames := make(map[string]struct{}, len(items))
	for _, item := range items {
		env, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("expected env object, got %#v", item)
		}
		gotNames[env["name"].(string)] = struct{}{}
	}

	if _, exists := gotNames["ALPHA"]; !exists {
		t.Fatalf("expected ALPHA in export, got %v", gotNames)
	}
	if _, exists := gotNames["GAMMA"]; !exists {
		t.Fatalf("expected GAMMA in export, got %v", gotNames)
	}
	if _, exists := gotNames["BETA"]; exists {
		t.Fatalf("did not expect BETA in selected export, got %v", gotNames)
	}
}

func TestEnvExportFilesPreservesEmbeddedAmpersands(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-exporter", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	envs := []*model.EnvVar{
		{Name: "JD_COOKIE", Value: "pt_key=one&a=1", Enabled: true, Position: 1000},
		{Name: "JD_COOKIE", Value: "pt_key=two&b=2", Enabled: true, Position: 2000},
	}
	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/envs/export-files",
		`{"format":"shell"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected export data map, got %#v", payload["data"])
	}

	shell, _ := data["shell"].(string)
	if !strings.Contains(shell, "export JD_COOKIE='pt_key=one&a=1&&pt_key=two&b=2'") {
		t.Fatalf("expected shell export to preserve embedded ampersands, got %s", shell)
	}
}

func TestEnvCreateRejectsOversizedBody(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-operator-limit", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	largeValue := strings.Repeat("a", (1<<20)+128)
	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/envs",
		fmt.Sprintf(`{"name":"BIG_ENV","value":"%s"}`, largeValue),
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "请求体过大") {
		t.Fatalf("expected oversized body message, got %s", rec.Body.String())
	}
}
