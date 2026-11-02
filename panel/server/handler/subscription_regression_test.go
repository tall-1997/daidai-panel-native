package handler_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestSubscriptionCreatePersistsForceOverwriteFalse(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"demo-sub","type":"git-repo","url":"https://github.com/example/demo.git","force_overwrite":false}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["force_overwrite"].(bool); got {
		t.Fatalf("expected response force_overwrite false, got %v", data["force_overwrite"])
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "demo-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.ForceOverwrite == nil || *sub.ForceOverwrite {
		t.Fatalf("expected force_overwrite persisted false, got %#v", sub.ForceOverwrite)
	}
}

func TestSubscriptionUpdateKeepsForceOverwriteFalseAfterReload(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	forceOverwrite := true
	sub := model.Subscription{
		Name:           "editable-sub",
		Type:           model.SubTypeGitRepo,
		URL:            "https://github.com/example/editable.git",
		Enabled:        true,
		ForceOverwrite: &forceOverwrite,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	updateBody := `{"force_overwrite":false,"alias":"edited-sub"}`
	updateRec := performJSONRequest(engine, http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10), updateBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.ForceOverwrite == nil || *updated.ForceOverwrite {
		t.Fatalf("expected force_overwrite updated false, got %#v", updated.ForceOverwrite)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions?keyword=editable-sub", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listRec.Code, listRec.Body.String())
	}

	listPayload := decodeJSONMap(t, listRec)
	items, ok := listPayload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected subscription list, got %T", listPayload["data"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected list item map, got %T", items[0])
	}
	if got, _ := item["force_overwrite"].(bool); got {
		t.Fatalf("expected list force_overwrite false after reload, got %v", item["force_overwrite"])
	}
}

// TestSubscriptionCreatePersistsOverwriteMode 守住「设了能存住」：
// v2.2.15 删的正是这条链路（后端字段还在、前端和 handler 没了），漏一处就是设完刷新又回到跟随全局。
func TestSubscriptionCreatePersistsOverwriteMode(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-overwrite-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"overwrite-sub","type":"git-repo","url":"https://github.com/example/overwrite.git","overwrite_mode":"preserve"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["overwrite_mode"].(string); got != model.SubOverwritePreserve {
		t.Fatalf("expected response overwrite_mode=%q, got %#v", model.SubOverwritePreserve, data["overwrite_mode"])
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "overwrite-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.OverwriteMode != model.SubOverwritePreserve {
		t.Fatalf("expected overwrite_mode persisted %q, got %q", model.SubOverwritePreserve, sub.OverwriteMode)
	}
}

// TestSubscriptionCreateNormalizesDirtyOverwriteMode 守住脏值归一：
// 不传 / 传非法值都必须落成 inherit（跟随全局），不能让脏字符串进库把判定搞成未定义行为。
func TestSubscriptionCreateNormalizesDirtyOverwriteMode(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-overwrite-dirty", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	cases := []struct {
		name string
		body string
	}{
		{"dirty-mode-sub", `{"name":"dirty-mode-sub","type":"git-repo","url":"https://github.com/example/a.git","overwrite_mode":"not-a-mode"}`},
		{"missing-mode-sub", `{"name":"missing-mode-sub","type":"git-repo","url":"https://github.com/example/b.git"}`},
	}

	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", tc.body, map[string]string{
			"Authorization": "Bearer " + token,
		}, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("[%s] expected 201, got %d, body=%s", tc.name, rec.Code, rec.Body.String())
		}

		var sub model.Subscription
		if err := database.DB.Where("name = ?", tc.name).First(&sub).Error; err != nil {
			t.Fatalf("[%s] query subscription: %v", tc.name, err)
		}
		if sub.OverwriteMode != model.SubOverwriteInherit {
			t.Fatalf("[%s] expected overwrite_mode normalized to %q, got %q", tc.name, model.SubOverwriteInherit, sub.OverwriteMode)
		}
	}
}

// TestSubscriptionUpdateNormalizesOverwriteMode 覆盖编辑链路：
// 合法值要存住，脏值要归一回 inherit，且列表接口读出来的也是归一后的值。
func TestSubscriptionUpdateNormalizesOverwriteMode(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-overwrite-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{
		Name:          "overwrite-editable-sub",
		Type:          model.SubTypeGitRepo,
		URL:           "https://github.com/example/overwrite-editable.git",
		Enabled:       true,
		OverwriteMode: model.SubOverwriteInherit,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	subPath := "/api/v1/subscriptions/" + strconv.FormatUint(uint64(sub.ID), 10)

	updateRec := performJSONRequest(engine, http.MethodPut, subPath, `{"overwrite_mode":"force"}`, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.OverwriteMode != model.SubOverwriteForce {
		t.Fatalf("expected overwrite_mode updated to %q, got %q", model.SubOverwriteForce, updated.OverwriteMode)
	}

	dirtyRec := performJSONRequest(engine, http.MethodPut, subPath, `{"overwrite_mode":"whatever"}`, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if dirtyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for dirty value, got %d, body=%s", dirtyRec.Code, dirtyRec.Body.String())
	}
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription after dirty update: %v", err)
	}
	if updated.OverwriteMode != model.SubOverwriteInherit {
		t.Fatalf("expected dirty overwrite_mode normalized to %q, got %q", model.SubOverwriteInherit, updated.OverwriteMode)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions?keyword=overwrite-editable-sub", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listRec.Code, listRec.Body.String())
	}
	listPayload := decodeJSONMap(t, listRec)
	items, ok := listPayload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected subscription list, got %T", listPayload["data"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected list item map, got %T", items[0])
	}
	if got, _ := item["overwrite_mode"].(string); got != model.SubOverwriteInherit {
		t.Fatalf("expected list overwrite_mode %q, got %#v", model.SubOverwriteInherit, item["overwrite_mode"])
	}
}

// TestSubscriptionCreatePersistsTaskSyncModes 守住「设了能存住」：
// 订阅级的自动添加/删除任务三态开关一旦哪一层漏接，用户设完刷新就又回到跟随全局，
// 而且因为全局默认是 true，表现是「关不掉」——正是 issue #119 第 1 条要解决的问题。
func TestSubscriptionCreatePersistsTaskSyncModes(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-task-sync-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"task-sync-sub","type":"git-repo","url":"https://github.com/example/task-sync.git","auto_add_task_mode":"disabled","auto_del_task_mode":"enabled"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["auto_add_task_mode"].(string); got != model.SubTaskSyncDisabled {
		t.Fatalf("expected response auto_add_task_mode=%q, got %#v", model.SubTaskSyncDisabled, data["auto_add_task_mode"])
	}
	if got, _ := data["auto_del_task_mode"].(string); got != model.SubTaskSyncEnabled {
		t.Fatalf("expected response auto_del_task_mode=%q, got %#v", model.SubTaskSyncEnabled, data["auto_del_task_mode"])
	}
	// 旧布尔字段必须继续下发，否则老客户端 / APP 解析会炸。
	if _, exists := data["auto_add_task"]; !exists {
		t.Fatal("expected legacy auto_add_task to stay in the response payload")
	}
	if _, exists := data["auto_del_task"]; !exists {
		t.Fatal("expected legacy auto_del_task to stay in the response payload")
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "task-sync-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.AutoAddTaskMode != model.SubTaskSyncDisabled {
		t.Fatalf("expected auto_add_task_mode persisted %q, got %q", model.SubTaskSyncDisabled, sub.AutoAddTaskMode)
	}
	if sub.AutoDelTaskMode != model.SubTaskSyncEnabled {
		t.Fatalf("expected auto_del_task_mode persisted %q, got %q", model.SubTaskSyncEnabled, sub.AutoDelTaskMode)
	}
}

// TestSubscriptionCreateNormalizesDirtyTaskSyncModes 守住脏值归一：
// 不传 / 传非法值都必须落成 inherit（跟随全局默认），不能让脏字符串进库把判定搞成未定义行为。
func TestSubscriptionCreateNormalizesDirtyTaskSyncModes(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-task-sync-dirty", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	cases := []struct {
		name string
		body string
	}{
		{"dirty-task-sync-sub", `{"name":"dirty-task-sync-sub","type":"git-repo","url":"https://github.com/example/c.git","auto_add_task_mode":"not-a-mode","auto_del_task_mode":"force"}`},
		{"missing-task-sync-sub", `{"name":"missing-task-sync-sub","type":"git-repo","url":"https://github.com/example/d.git"}`},
	}

	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", tc.body, map[string]string{
			"Authorization": "Bearer " + token,
		}, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("[%s] expected 201, got %d, body=%s", tc.name, rec.Code, rec.Body.String())
		}

		var sub model.Subscription
		if err := database.DB.Where("name = ?", tc.name).First(&sub).Error; err != nil {
			t.Fatalf("[%s] query subscription: %v", tc.name, err)
		}
		if sub.AutoAddTaskMode != model.SubTaskSyncInherit {
			t.Fatalf("[%s] expected auto_add_task_mode normalized to %q, got %q", tc.name, model.SubTaskSyncInherit, sub.AutoAddTaskMode)
		}
		// force 是覆盖拉取那套三态的词，在这里同样是脏值，必须归 inherit 而不是被当成开启。
		if sub.AutoDelTaskMode != model.SubTaskSyncInherit {
			t.Fatalf("[%s] expected auto_del_task_mode normalized to %q, got %q", tc.name, model.SubTaskSyncInherit, sub.AutoDelTaskMode)
		}
	}
}

// TestSubscriptionUpdateNormalizesTaskSyncModes 覆盖编辑链路：
// 合法值要存住，脏值要归一回 inherit，且列表接口读出来的也是归一后的值。
func TestSubscriptionUpdateNormalizesTaskSyncModes(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-task-sync-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{
		Name:            "task-sync-editable-sub",
		Type:            model.SubTypeGitRepo,
		URL:             "https://github.com/example/task-sync-editable.git",
		Enabled:         true,
		AutoAddTaskMode: model.SubTaskSyncInherit,
		AutoDelTaskMode: model.SubTaskSyncInherit,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	subPath := "/api/v1/subscriptions/" + strconv.FormatUint(uint64(sub.ID), 10)

	updateRec := performJSONRequest(engine, http.MethodPut, subPath,
		`{"auto_add_task_mode":"disabled","auto_del_task_mode":"enabled"}`, map[string]string{
			"Authorization": "Bearer " + token,
		}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.AutoAddTaskMode != model.SubTaskSyncDisabled {
		t.Fatalf("expected auto_add_task_mode updated to %q, got %q", model.SubTaskSyncDisabled, updated.AutoAddTaskMode)
	}
	if updated.AutoDelTaskMode != model.SubTaskSyncEnabled {
		t.Fatalf("expected auto_del_task_mode updated to %q, got %q", model.SubTaskSyncEnabled, updated.AutoDelTaskMode)
	}

	dirtyRec := performJSONRequest(engine, http.MethodPut, subPath,
		`{"auto_add_task_mode":"whatever","auto_del_task_mode":null}`, map[string]string{
			"Authorization": "Bearer " + token,
		}, "")
	if dirtyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for dirty value, got %d, body=%s", dirtyRec.Code, dirtyRec.Body.String())
	}
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription after dirty update: %v", err)
	}
	if updated.AutoAddTaskMode != model.SubTaskSyncInherit {
		t.Fatalf("expected dirty auto_add_task_mode normalized to %q, got %q", model.SubTaskSyncInherit, updated.AutoAddTaskMode)
	}
	if updated.AutoDelTaskMode != model.SubTaskSyncInherit {
		t.Fatalf("expected null auto_del_task_mode normalized to %q, got %q", model.SubTaskSyncInherit, updated.AutoDelTaskMode)
	}

	listRec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions?keyword=task-sync-editable-sub", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", listRec.Code, listRec.Body.String())
	}
	listPayload := decodeJSONMap(t, listRec)
	items, ok := listPayload["data"].([]interface{})
	if !ok || len(items) == 0 {
		t.Fatalf("expected subscription list, got %T", listPayload["data"])
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected list item map, got %T", items[0])
	}
	if got, _ := item["auto_add_task_mode"].(string); got != model.SubTaskSyncInherit {
		t.Fatalf("expected list auto_add_task_mode %q, got %#v", model.SubTaskSyncInherit, item["auto_add_task_mode"])
	}
	if got, _ := item["auto_del_task_mode"].(string); got != model.SubTaskSyncInherit {
		t.Fatalf("expected list auto_del_task_mode %q, got %#v", model.SubTaskSyncInherit, item["auto_del_task_mode"])
	}
}

// TestSubscriptionUpdateIgnoresLegacyTaskFlags 守住「旧布尔列没有写入路径」：
//
// 复现路径全程只用面板自己的功能：导入青龙备份（老写法会写 auto_add_task=true）→
// 用户在网页里把这条订阅改成「跟随全局设置」并保存（老前端把 auto_add_task=true 原样回传）→
// 重启时启动回填看到 legacy=1 且 mode='inherit'，把它提回「强制开启」，用户的选择静默消失。
// 修法是把 auto_add_task / auto_del_task 从 Update 白名单里删掉：它们只做只读输出，
// ToDict 继续下发就够老客户端不炸了。
func TestSubscriptionUpdateIgnoresLegacyTaskFlags(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-legacy-flag-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{
		Name:            "legacy-flag-sub",
		Type:            model.SubTypeGitRepo,
		URL:             "https://github.com/example/legacy-flag.git",
		Enabled:         true,
		AutoAddTaskMode: model.SubTaskSyncInherit,
		AutoDelTaskMode: model.SubTaskSyncInherit,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	// 老前端的提交体形状：三态选了 inherit，旧布尔字段被原样回传。
	updateBody := `{"auto_add_task":true,"auto_del_task":true,"auto_add_task_mode":"inherit","auto_del_task_mode":"inherit"}`
	updateRec := performJSONRequest(engine, http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10), updateBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if updated.AutoAddTask || updated.AutoDelTask {
		t.Fatalf("expected legacy flags to stay false, got auto_add_task=%v auto_del_task=%v",
			updated.AutoAddTask, updated.AutoDelTask)
	}
	if updated.AutoAddTaskMode != model.SubTaskSyncInherit || updated.AutoDelTaskMode != model.SubTaskSyncInherit {
		t.Fatalf("expected task sync modes to stay %q, got add=%q del=%q",
			model.SubTaskSyncInherit, updated.AutoAddTaskMode, updated.AutoDelTaskMode)
	}
}

// TestSubscriptionCreateTranslatesLegacyTaskFlags 守住写入点翻译：
// 老客户端只发旧布尔 true、不发三态时，语义「开就是开、不看全局」必须原地翻译成 mode='enabled'，
// 而源列恒写 false —— 信息一点不丢，同时不给启动回填留下 legacy=1 的行。
// 反过来，显式给了 inherit 时旧布尔不许说话，否则就是本次修的那个 bug。
func TestSubscriptionCreateTranslatesLegacyTaskFlags(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-legacy-flag-creator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	cases := []struct {
		name    string
		body    string
		wantAdd string
		wantDel string
	}{
		{
			// 老客户端 / APP：只有布尔字段。
			name:    "legacy-only-sub",
			body:    `{"name":"legacy-only-sub","type":"git-repo","url":"https://github.com/example/legacy-only.git","auto_add_task":true,"auto_del_task":true}`,
			wantAdd: model.SubTaskSyncEnabled,
			wantDel: model.SubTaskSyncEnabled,
		},
		{
			// 显式选了「跟随全局设置」，旧布尔不能把它顶成强制开启。
			name:    "explicit-inherit-sub",
			body:    `{"name":"explicit-inherit-sub","type":"git-repo","url":"https://github.com/example/explicit-inherit.git","auto_add_task":true,"auto_del_task":true,"auto_add_task_mode":"inherit","auto_del_task_mode":"inherit"}`,
			wantAdd: model.SubTaskSyncInherit,
			wantDel: model.SubTaskSyncInherit,
		},
	}

	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", tc.body, map[string]string{
			"Authorization": "Bearer " + token,
		}, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("[%s] expected 201, got %d, body=%s", tc.name, rec.Code, rec.Body.String())
		}

		var sub model.Subscription
		if err := database.DB.Where("name = ?", tc.name).First(&sub).Error; err != nil {
			t.Fatalf("[%s] query subscription: %v", tc.name, err)
		}
		if sub.AutoAddTaskMode != tc.wantAdd {
			t.Fatalf("[%s] expected auto_add_task_mode %q, got %q", tc.name, tc.wantAdd, sub.AutoAddTaskMode)
		}
		if sub.AutoDelTaskMode != tc.wantDel {
			t.Fatalf("[%s] expected auto_del_task_mode %q, got %q", tc.name, tc.wantDel, sub.AutoDelTaskMode)
		}
		if sub.AutoAddTask || sub.AutoDelTask {
			t.Fatalf("[%s] expected legacy flags persisted false, got auto_add_task=%v auto_del_task=%v",
				tc.name, sub.AutoAddTask, sub.AutoDelTask)
		}
	}
}

func TestSubscriptionCreatePersistsTokenAuthWithoutLeakingToken(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-token-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	body := `{"name":"token-sub","type":"git-repo","url":"https://github.com/example/private.git","auth_type":"token","auth_token":"ghp_demo_token"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", body, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rec.Code, rec.Body.String())
	}

	payload := decodeJSONMap(t, rec)
	data, ok := payload["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data map, got %T", payload["data"])
	}
	if got, _ := data["auth_type"].(string); got != model.SubAuthTypeToken {
		t.Fatalf("expected auth_type=%q, got %#v", model.SubAuthTypeToken, data["auth_type"])
	}
	if got, _ := data["has_auth_token"].(bool); !got {
		t.Fatalf("expected has_auth_token=true, got %#v", data["has_auth_token"])
	}
	if _, exists := data["auth_token"]; exists {
		t.Fatalf("did not expect auth_token in response payload: %#v", data)
	}

	var sub model.Subscription
	if err := database.DB.Where("name = ?", "token-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.EffectiveAuthType() != model.SubAuthTypeToken {
		t.Fatalf("expected stored auth type token, got %q", sub.EffectiveAuthType())
	}
	if strings.TrimSpace(sub.AuthToken) != "ghp_demo_token" {
		t.Fatalf("expected stored auth token, got %q", sub.AuthToken)
	}
	if sub.SSHKeyID != nil {
		t.Fatalf("expected ssh_key_id cleared for token auth, got %#v", sub.SSHKeyID)
	}
}

func TestSubscriptionUpdateKeepsExistingTokenWhenAuthTokenOmitted(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-token-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	sub := model.Subscription{
		Name:      "editable-token-sub",
		Type:      model.SubTypeGitRepo,
		URL:       "https://github.com/example/private.git",
		Enabled:   true,
		AuthType:  model.SubAuthTypeToken,
		AuthToken: "ghp_keep_me",
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	updateBody := `{"auth_type":"token","auth_token":"","alias":"token-edited"}`
	updateRec := performJSONRequest(engine, http.MethodPut, "/api/v1/subscriptions/"+strconv.FormatUint(uint64(sub.ID), 10), updateBody, map[string]string{
		"Authorization": "Bearer " + token,
	}, "")
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", updateRec.Code, updateRec.Body.String())
	}

	var updated model.Subscription
	if err := database.DB.First(&updated, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if strings.TrimSpace(updated.AuthToken) != "ghp_keep_me" {
		t.Fatalf("expected auth token to stay unchanged, got %q", updated.AuthToken)
	}
	if updated.EffectiveAuthType() != model.SubAuthTypeToken {
		t.Fatalf("expected auth type token after update, got %q", updated.EffectiveAuthType())
	}
}

// #129：白名单 / 黑名单 / 依赖规则里含正则触发字符的片段按正则解析，编译不过就在保存时 400，
// 文案点名字段与第几段，一条都不落库；合法正则原样保存（青龙 ql repo 一键识别填进来的就是这种值）。
func TestSubscriptionCreateRejectsInvalidRegexFilters(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-regex-creator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()
	headers := map[string]string{"Authorization": "Bearer " + token}

	cases := []struct {
		name         string
		body         string
		wantKeywords []string
	}{
		{
			name:         "regex-whitelist-sub",
			body:         `{"name":"regex-whitelist-sub","type":"git-repo","url":"https://github.com/example/regex-whitelist.git","whitelist":"jd_|^jd["}`,
			wantKeywords: []string{"白名单", "第 2 段", "^jd[", "转义"},
		},
		{
			name:         "regex-blacklist-sub",
			body:         `{"name":"regex-blacklist-sub","type":"git-repo","url":"https://github.com/example/regex-blacklist.git","blacklist":"(backUp"}`,
			wantKeywords: []string{"黑名单", "第 1 段", "(backUp"},
		},
		{
			name:         "regex-depend-sub",
			body:         `{"name":"regex-depend-sub","type":"git-repo","url":"https://github.com/example/regex-depend.git","depend_on":"sendNotify|utils|a{2,1}"}`,
			wantKeywords: []string{"依赖规则", "第 3 段", "a{2,1}"},
		},
	}
	for _, tc := range cases {
		rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", tc.body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("[%s] expected 400, got %d, body=%s", tc.name, rec.Code, rec.Body.String())
		}
		message, _ := decodeJSONMap(t, rec)["error"].(string)
		for _, keyword := range tc.wantKeywords {
			if !strings.Contains(message, keyword) {
				t.Errorf("[%s] error message should contain %q, got %q", tc.name, keyword, message)
			}
		}
		var count int64
		database.DB.Model(&model.Subscription{}).Where("name = ?", tc.name).Count(&count)
		if count != 0 {
			t.Fatalf("[%s] invalid regex should not be persisted, found %d rows", tc.name, count)
		}
	}

	okBody := `{"name":"regex-ok-sub","type":"git-repo","url":"https://github.com/example/regex-ok.git","whitelist":"jd_|jx_|jddj_","blacklist":"back[Uu]p","depend_on":"^jd[^_]|USER|JD|function|sendNotify|utils"}`
	rec := performJSONRequest(engine, http.MethodPost, "/api/v1/subscriptions", okBody, headers, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid regex, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var sub model.Subscription
	if err := database.DB.Where("name = ?", "regex-ok-sub").First(&sub).Error; err != nil {
		t.Fatalf("query subscription: %v", err)
	}
	if sub.DependOn != "^jd[^_]|USER|JD|function|sendNotify|utils" || sub.Blacklist != "back[Uu]p" {
		t.Fatalf("regex filters should be persisted verbatim, got depend_on=%q blacklist=%q", sub.DependOn, sub.Blacklist)
	}
}

// App 每次保存都把 whitelist / blacklist / depend_on 原样回传：库里的存量值（升级前保存的、青龙备份导入的）
// 就算不是合法正则，只要这次没改它，就不能挡住用户改别的字段。值与库里不同时才校验。
func TestSubscriptionUpdateValidatesOnlyChangedRegexFilters(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-regex-editor", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()
	headers := map[string]string{"Authorization": "Bearer " + token}

	sub := model.Subscription{
		Name:      "regex-dirty-sub",
		Type:      model.SubTypeGitRepo,
		URL:       "https://github.com/example/regex-dirty.git",
		Enabled:   true,
		Whitelist: "jd_",
		DependOn:  "^jd[",
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("create subscription: %v", err)
	}
	subPath := "/api/v1/subscriptions/" + strconv.FormatUint(uint64(sub.ID), 10)
	reload := func() model.Subscription {
		t.Helper()
		var current model.Subscription
		if err := database.DB.First(&current, sub.ID).Error; err != nil {
			t.Fatalf("reload subscription: %v", err)
		}
		return current
	}

	// 1) App 的保存形状：三个字段原样回传，只改名字 → 200，存量脏值原样保留。
	rec := performJSONRequest(engine, http.MethodPut, subPath,
		`{"name":"regex-dirty-renamed","whitelist":"jd_","blacklist":"","depend_on":"^jd["}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unchanged dirty value should not block the update, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if current := reload(); current.Name != "regex-dirty-renamed" || current.DependOn != "^jd[" {
		t.Fatalf("expected rename applied and depend_on kept, got name=%q depend_on=%q", current.Name, current.DependOn)
	}

	// 2) 依赖规则改成另一个非法值 → 400，同一请求里的其它字段也不落库。
	rec = performJSONRequest(engine, http.MethodPut, subPath, `{"name":"should-not-apply","depend_on":"^jd[|sendNotify"}`, headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("changed invalid depend_on should be rejected, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if message, _ := decodeJSONMap(t, rec)["error"].(string); !strings.Contains(message, "依赖规则第 1 段") {
		t.Fatalf("error should point at 依赖规则第 1 段, got %q", message)
	}
	if current := reload(); current.Name != "regex-dirty-renamed" || current.DependOn != "^jd[" {
		t.Fatalf("rejected update must not write anything, got name=%q depend_on=%q", current.Name, current.DependOn)
	}

	// 3) 白名单改成非法值 → 400。
	rec = performJSONRequest(engine, http.MethodPut, subPath, `{"whitelist":"(jd_"}`, headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("changed invalid whitelist should be rejected, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if message, _ := decodeJSONMap(t, rec)["error"].(string); !strings.Contains(message, "白名单第 1 段") {
		t.Fatalf("error should point at 白名单第 1 段, got %q", message)
	}

	// 4) 改成合法正则 → 200，原样落库。
	rec = performJSONRequest(engine, http.MethodPut, subPath, `{"depend_on":"^jd[^_]|sendNotify"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid regex should be accepted, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if current := reload(); current.DependOn != "^jd[^_]|sendNotify" {
		t.Fatalf("expected depend_on updated verbatim, got %q", current.DependOn)
	}
}

// 批量删除的空列表必须回 400：binding:"required" 放行 "ids": []，而空列表一条都不删却回「已删除 0 个订阅」，
// 前端据此提示「批量删除成功」，用户以为删掉了。非空列表照常删除。
func TestSubscriptionBatchDeleteRejectsEmptyIDs(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-batch-operator", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()
	headers := map[string]string{"Authorization": "Bearer " + token}

	keep := model.Subscription{Name: "batch-keep", Type: model.SubTypeGitRepo, URL: "https://github.com/example/keep.git", Enabled: true}
	drop := model.Subscription{Name: "batch-drop", Type: model.SubTypeGitRepo, URL: "https://github.com/example/drop.git", Enabled: true}
	for _, sub := range []*model.Subscription{&keep, &drop} {
		if err := database.DB.Create(sub).Error; err != nil {
			t.Fatalf("create subscription: %v", err)
		}
	}

	for _, body := range []string{`{"ids":[]}`, `{}`, `{"ids":null}`} {
		rec := performJSONRequest(engine, http.MethodDelete, "/api/v1/subscriptions/batch", body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d, body=%s", body, rec.Code, rec.Body.String())
		}
	}
	var count int64
	database.DB.Model(&model.Subscription{}).Count(&count)
	if count != 2 {
		t.Fatalf("rejected requests must not delete anything, got %d subscriptions", count)
	}

	rec := performJSONRequest(engine, http.MethodDelete, "/api/v1/subscriptions/batch",
		`{"ids":[`+strconv.FormatUint(uint64(drop.ID), 10)+`]}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("non-empty ids should succeed, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var remaining []model.Subscription
	database.DB.Find(&remaining)
	if len(remaining) != 1 || remaining[0].ID != keep.ID {
		t.Fatalf("only the listed subscription should be deleted, remaining %+v", remaining)
	}
}
