package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// issue #116-5：编辑器偏好要跟着人走（换设备 / 换浏览器也记得住），
// 所以它落在 per-user 的 user_preferences 表，而不是全局的 system_configs。

// testutil.SetupTestEnv 自己维护了一份 AutoMigrate 清单（没有走 appboot.allModels），
// user_preferences 现在**已经在**那份清单里（见 testutil/testenv.go），
// 所以这里不再补建表 —— 早先那次 AutoMigrate 已经是纯冗余。
// 函数留着不内联：偏好用例全走这一个入口，将来这张表再掉出清单只用改这一处。
func setupEditorPrefsEnv(t *testing.T) {
	t.Helper()

	testutil.SetupTestEnv(t)
}

func editorPrefsRequest(t *testing.T, engine *gin.Engine, method, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, "/api/v1/auth/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// editorPrefsDecode 把响应体解成 map，这样除了值还能顺带断言 JSON 类型：
// 前端 ensureEditorPreferencesLoaded() 用 `typeof x === 'boolean'` 判定 minimap /
// indent_guides 要不要写回本地缓存，下发成字符串它会整项忽略、且不报任何错。
func editorPrefsDecode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Editor map[string]any `json:"editor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preferences body %q: %v", rec.Body.String(), err)
	}
	if payload.Editor == nil {
		t.Fatalf("response has no editor object: %s", rec.Body.String())
	}
	return payload.Editor
}

// editorPrefsDecodeStored 取响应体顶层的 stored —— 「这套值是用户存过的，还是服务端现编的默认值」。
// 没有它，前端分不出「从没存过」和「用户主动把 5 项都设成默认值」，
// 于是升级用户存在浏览器本地（dd:editor:*）的老偏好会被默认值无条件冲掉、刷新也回不来。
// 字段缺失直接失败：漏下发和下发 false 对前端是两回事（缺失会被当成 undefined，
// 走不进「stored !== true 就把本机那份迁上去」那条分支）。
func editorPrefsDecodeStored(t *testing.T, rec *httptest.ResponseRecorder) bool {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preferences body %q: %v", rec.Body.String(), err)
	}
	raw, exists := payload["stored"]
	if !exists {
		t.Fatalf("response has no stored field: %s", rec.Body.String())
	}
	stored, ok := raw.(bool)
	if !ok {
		t.Fatalf("stored must be a JSON boolean, got %T: %s", raw, rec.Body.String())
	}
	return stored
}

// editorPrefsAssertDefaults 钉死那 5 个默认值。
// 它们必须与 web/src/utils/editorPreferences.ts 的 EDITOR_PREFERENCES_DEFAULTS 逐字相同，
// 否则「从没存过偏好的用户」打开编辑器时观感会跳一下（本地缓存先按前端默认渲染，
// 服务端值拉回来又改一次）。
func editorPrefsAssertDefaults(t *testing.T, editor map[string]any) {
	t.Helper()

	if editor["word_wrap"] != "on" {
		t.Fatalf("word_wrap default should be on, got %#v", editor["word_wrap"])
	}
	if editor["minimap"] != false {
		t.Fatalf("minimap default should be false, got %#v", editor["minimap"])
	}
	if editor["indent_guides"] != true {
		t.Fatalf("indent_guides default should be true, got %#v", editor["indent_guides"])
	}
	if editor["whitespace"] != "selection" {
		t.Fatalf("whitespace default should be selection, got %#v", editor["whitespace"])
	}
	if editor["indent_width"] != "auto" {
		t.Fatalf("indent_width default should be auto, got %#v", editor["indent_width"])
	}
}

func TestGetEditorPreferencesReturnsDefaultsForFreshUser(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-fresh", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	rec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	editor := editorPrefsDecode(t, rec)
	editorPrefsAssertDefaults(t, editor)

	// 这套默认值是服务端现编的，不是用户存的 —— stored 必须是 false，
	// 否则前端会拿它去覆盖本机（dd:editor:*）那份老偏好。
	if editorPrefsDecodeStored(t, rec) {
		t.Fatalf("从没存过偏好的用户 stored 必须为 false: %s", rec.Body.String())
	}

	// 类型也要对：minimap / indent_guides 是 JSON 布尔，indent_width 是字符串。
	if _, ok := editor["minimap"].(bool); !ok {
		t.Fatalf("minimap must be a JSON boolean, got %T", editor["minimap"])
	}
	if _, ok := editor["indent_width"].(string); !ok {
		t.Fatalf("indent_width must be a JSON string, got %T", editor["indent_width"])
	}
}

// 只提交一个键时，其余键必须保持原值 —— 前端每次只 PUT 被点的那一个开关，
// 两个标签页各改各的，谁也不能把对方的改动盖回去。
func TestUpdateEditorPreferencesMergesPerField(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-merge", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// 第一次：只改 word_wrap 与 indent_width。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"word_wrap":"off"}}`))
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"indent_width":"4"}}`))

	// 第二次：只改 whitespace，前两项不能被带回默认。
	mergedRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"whitespace":"all"}}`)
	merged := editorPrefsDecode(t, mergedRec)
	// PUT 的响应恒为 stored=true：写完必然是存过了。
	if !editorPrefsDecodeStored(t, mergedRec) {
		t.Fatalf("PUT 响应的 stored 必须为 true: %s", mergedRec.Body.String())
	}
	if merged["word_wrap"] != "off" || merged["indent_width"] != "4" || merged["whitespace"] != "all" {
		t.Fatalf("PUT response should contain merged values, got %#v", merged)
	}
	// 没碰过的两项保持默认。
	if merged["minimap"] != false || merged["indent_guides"] != true {
		t.Fatalf("untouched fields should keep defaults, got %#v", merged)
	}

	// GET 回来仍然是合并后的完整值。
	reloadedRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	reloaded := editorPrefsDecode(t, reloadedRec)
	// 存过之后 GET 的 stored 必须翻成 true，前端这时才允许拿服务端值写回本地。
	if !editorPrefsDecodeStored(t, reloadedRec) {
		t.Fatalf("PUT 之后 GET 的 stored 必须为 true: %s", reloadedRec.Body.String())
	}
	if reloaded["word_wrap"] != "off" || reloaded["indent_width"] != "4" || reloaded["whitespace"] != "all" {
		t.Fatalf("GET should return merged values, got %#v", reloaded)
	}
	if reloaded["minimap"] != false || reloaded["indent_guides"] != true {
		t.Fatalf("GET untouched fields should keep defaults, got %#v", reloaded)
	}
}

// 这条用例覆盖的是**面板之外**的客户端：APP / 第三方脚本 / 把这两项当字符串开关发的历史客户端。
// 面板前端自己发的是 JSON 布尔（web/src/utils/editorPreferences.ts 的 setEditorPreference 里
// minimap / indent_guides 走 `value as boolean`，web/src/api/auth.ts 的 updatePreferences
// 签名也是 Record<string, string | boolean>），所以它并不依赖 "on"/"off" 这条路径。
// 但这类客户端对同步失败往往是静默的：只认布尔的话它们每次点开关都会 400，
// 表现为这两项跨端永远不生效、一行报错都看不到。
// 下发方向两边一致：GET 回去的永远是 JSON 布尔。
func TestUpdateEditorPreferencesAcceptsOnOffFlags(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-flags", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	editor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"editor":{"minimap":"on","indent_guides":"off"}}`))
	if editor["minimap"] != true || editor["indent_guides"] != false {
		t.Fatalf("on/off strings should map to booleans, got %#v", editor)
	}

	// JSON 布尔同样要认（APP / 第三方脚本更可能直接发布尔）。
	editor = editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"editor":{"minimap":false,"indent_guides":true}}`))
	if editor["minimap"] != false || editor["indent_guides"] != true {
		t.Fatalf("JSON booleans should be accepted, got %#v", editor)
	}
}

func TestUpdateEditorPreferencesRejectsInvalidValues(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-invalid", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	cases := map[string]string{
		// "boundary" 是 CodeMirror 的档位名，不在我们这三档里。
		"whitespace":   `{"editor":{"whitespace":"boundary"}}`,
		"indent_width": `{"editor":{"indent_width":"3"}}`,
		"word_wrap":    `{"editor":{"word_wrap":"wrap"}}`,
		"minimap":      `{"editor":{"minimap":"maybe"}}`,
	}
	for name, body := range cases {
		rec := editorPrefsRequest(t, engine, http.MethodPut, token, body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d body=%s", name, rec.Code, rec.Body.String())
		}
	}

	// 非法请求一个都不能落库，GET 仍然是默认值。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")))
}

// 走 per-user 表的全部理由：A 改了不该让 B 跟着变。
func TestEditorPreferencesAreIsolatedPerUser(t *testing.T) {
	setupEditorPrefsEnv(t)

	alice := testutil.MustCreateUser(t, "prefs-alice", "admin")
	bob := testutil.MustCreateUser(t, "prefs-bob", "operator")
	aliceToken := testutil.MustCreateAccessToken(t, alice.Username, alice.Role)
	bobToken := testutil.MustCreateAccessToken(t, bob.Username, bob.Role)
	engine := newProtectedRouter()

	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, aliceToken,
		`{"editor":{"word_wrap":"off","whitespace":"all","indent_width":"8","minimap":"on"}}`))

	// Bob 完全没被影响，仍然是默认值。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, "")))

	// Bob 自己改一项，也不能反过来污染 Alice。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, bobToken, `{"editor":{"indent_width":"2"}}`))

	aliceEditor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, aliceToken, ""))
	if aliceEditor["indent_width"] != "8" || aliceEditor["whitespace"] != "all" || aliceEditor["minimap"] != true {
		t.Fatalf("alice preferences were affected by bob: %#v", aliceEditor)
	}

	bobEditor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, ""))
	if bobEditor["indent_width"] != "2" || bobEditor["whitespace"] != "selection" {
		t.Fatalf("bob preferences are wrong: %#v", bobEditor)
	}

	// 每人一行，不是共用一行被反复覆盖。
	var count int64
	if err := database.DB.Model(&model.UserPreference{}).Count(&count).Error; err != nil {
		t.Fatalf("count user preferences: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected one row per user, got %d", count)
	}
}

// 脏数据不该让用户打不开编辑器：解析不出来就整体回落默认，绝不 500。
func TestGetEditorPreferencesFallsBackOnCorruptedRow(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-corrupt", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// wantStored 是这一轮的重点：读不出任何可用选择的行一律当「没存过」（false），
	// 好让前端把本机那份重新迁上来，而不是拿默认值把它盖掉。
	cases := []struct {
		editor     string
		wantStored bool
	}{
		{editor: `{"word_wrap":`, wantStored: false},                 // 截断的 JSON
		{editor: `not json at all`, wantStored: false},               // 压根不是 JSON
		{editor: ``, wantStored: false},                              // 空串（从没改过任何开关）
		{editor: `null`, wantStored: false},                          // 合法 JSON、但不是对象；解到结构体上不报错也不改字段
		{editor: `[1,2]`, wantStored: false},                         // 合法 JSON、是数组不是对象
		{editor: `{"word_wrap":123,"minimap":1}`, wantStored: false}, // 是对象、但类型全错，一项也读不出来
		// 合法 JSON 对象、只有单项是枚举外的脏值：用户确实存过，
		// 那一项回落默认、其余照用，所以 stored 仍然是 true。
		{editor: `{"whitespace":"boundary"}`, wantStored: true},
	}
	for _, tc := range cases {
		if err := database.DB.Where("user_id = ?", user.ID).Delete(&model.UserPreference{}).Error; err != nil {
			t.Fatalf("reset preference row: %v", err)
		}
		if err := database.DB.Create(&model.UserPreference{UserID: user.ID, Editor: tc.editor}).Error; err != nil {
			t.Fatalf("seed dirty preference %q: %v", tc.editor, err)
		}

		rec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("dirty row %q should still return 200, got %d body=%s", tc.editor, rec.Code, rec.Body.String())
		}
		editorPrefsAssertDefaults(t, editorPrefsDecode(t, rec))
		if got := editorPrefsDecodeStored(t, rec); got != tc.wantStored {
			t.Fatalf("dirty row %q: stored want %v got %v, body=%s", tc.editor, tc.wantStored, got, rec.Body.String())
		}
	}
}

// stored 的完整生命周期：没有行 → 空串 → 脏 JSON 一路都是 false，
// 存过一次之后翻成 true 并且一直是 true。
//
// 这条用例钉的是 issue #116 的续集：v3.2.2/3.2.3 的用户把编辑器偏好存在浏览器本地
// （dd:editor:*），升级后第一次打开编辑器时，前端会拿服务端下发的值逐项写回本地缓存。
// 服务端这边一行记录都没有、下发的全是默认值，本机那份老偏好就被无条件冲掉、刷新也回不来。
// stored=false 是前端唯一能识破「这是默认值不是你的选择」的信号，
// 它在那种情况下会反过来把本机那 5 项 PUT 上去（首次上行迁移）。
func TestEditorPreferencesStoredFlagTracksPersistence(t *testing.T) {
	setupEditorPrefsEnv(t)

	user := testutil.MustCreateUser(t, "prefs-stored", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	engine := newProtectedRouter()

	// 1) 一行都没有。
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("没有 user_preferences 行时 stored 必须为 false")
	}

	// 2) 有行、但 Editor 是空串（比如别的字段先把行建出来了）。
	if err := database.DB.Create(&model.UserPreference{UserID: user.ID, Editor: ""}).Error; err != nil {
		t.Fatalf("seed empty preference row: %v", err)
	}
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("Editor 为空串时 stored 必须为 false")
	}

	// 3) 有行、Editor 是解不开的脏 JSON。
	if err := database.DB.Model(&model.UserPreference{}).
		Where("user_id = ?", user.ID).
		Update("editor", `{"word_wrap":`).Error; err != nil {
		t.Fatalf("seed corrupted preference row: %v", err)
	}
	if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
		t.Fatalf("Editor 是脏 JSON 时 stored 必须为 false")
	}

	// 4) 存一次 —— 哪怕存的值恰好等于默认值，也必须能和「没存过」区分开。
	putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"word_wrap":"on"}}`)
	if !editorPrefsDecodeStored(t, putRec) {
		t.Fatalf("PUT 响应的 stored 必须为 true: %s", putRec.Body.String())
	}

	getRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	if !editorPrefsDecodeStored(t, getRec) {
		t.Fatalf("存过之后 GET 的 stored 必须为 true: %s", getRec.Body.String())
	}
	// 值本身仍然是完整的一套（stored 只是多带的标记，不改变 editor 的下发口径）。
	editorPrefsAssertDefaults(t, editorPrefsDecode(t, getRec))
}

// ---------------------------------------------------------------------------
// 列表页 / 日志查看等界面偏好（list 组，issue #143 桌面端第 1 条：每页条数、视图栏显隐跟随账户；
// #147：打开已结束的日志时定位到底部，见 ⑨）。
//
// 这一组与 editor 共用同一个接口、同一行记录，但各占一列、按组可选写入。
// 下面这些用例里最要紧的是第一条：只写 list 时绝不能碰 editor 列、不能把 editor 的 stored 翻成 true。
// ---------------------------------------------------------------------------

// listPrefsDecode 取响应体顶层的 list，要求它存在且是 JSON 对象。
// 缺失或是 null 直接失败：前端 ensureListPreferencesLoaded() 把「list 不是对象」当成老服务端、
// 整段迁移直接跳过，下发成 null 等于让所有用户的本机老值永远迁不上来。
func listPrefsDecode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode preferences body %q: %v", rec.Body.String(), err)
	}
	raw, exists := payload["list"]
	if !exists {
		t.Fatalf("response has no list field: %s", rec.Body.String())
	}
	list, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("list must be a JSON object, got %T: %s", raw, rec.Body.String())
	}
	return list
}

type preferenceRawColumns struct {
	Editor string
	List   string
}

// readPreferenceRawColumns 用 Raw SELECT 读 editor / list 两列的原始存储值。
// 刻意绕开 handler 的解析与回落：护栏要证明的是「库里那一列一个字节都没被碰过」，
// 而不是「读出来看着像默认值」—— 后者在 editor 被写成整套默认值时同样成立，恰好查不出那个 bug。
func readPreferenceRawColumns(t *testing.T, userID uint) (preferenceRawColumns, bool) {
	t.Helper()

	var rows []preferenceRawColumns
	if err := database.DB.Raw("SELECT editor, list FROM user_preferences WHERE user_id = ?", userID).
		Scan(&rows).Error; err != nil {
		t.Fatalf("raw select user_preferences: %v", err)
	}
	switch len(rows) {
	case 0:
		return preferenceRawColumns{}, false
	case 1:
		return rows[0], true
	default:
		t.Fatalf("user %d should have at most one preference row, got %d", userID, len(rows))
		return preferenceRawColumns{}, false
	}
}

// ① 🔴 核心护栏：只 PUT list 时，editor 列一个字节都不能动，editor 的 stored 也不能被翻成 true。
//
// 背景：以前 PUT 的入参是值类型、upsert 的 DoUpdates 写死 editor，
// 不带 editor 的请求也会把一整套默认值写进 editor 列、stored 从 false 翻成 true。
// 前端的列表偏好迁移恰好就是「只发 list」，它一上线，每个升级用户存在本机 dd:editor:* 的
// 编辑器偏好都会在下次打开编辑器时被默认值静默冲掉 —— 不报错、测试全绿，只有用户看得见。
// 三种起点都要测：没有行（走 INSERT）、有行但 editor 为空串（走 DO UPDATE）、editor 已存过（走 DO UPDATE）。
func TestUpdateListPreferencesNeverTouchesEditorColumn(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	t.Run("fresh user without a row", func(t *testing.T) {
		user := testutil.MustCreateUser(t, "list-guard-fresh", "operator")
		token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

		putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_page_size":50}}`)
		if editorPrefsDecodeStored(t, putRec) {
			t.Fatalf("只写 list 的 PUT 响应 stored 必须仍为 false: %s", putRec.Body.String())
		}
		editorPrefsAssertDefaults(t, editorPrefsDecode(t, putRec))
		if got := listPrefsDecode(t, putRec)["tasks_page_size"]; got != float64(50) {
			t.Fatalf("PUT response should carry the stored list key, got %#v", got)
		}

		raw, found := readPreferenceRawColumns(t, user.ID)
		if !found {
			t.Fatal("PUT list should create the preference row")
		}
		if raw.Editor != "" {
			t.Fatalf("只写 list 时新建行的 editor 必须落空串，got %q", raw.Editor)
		}
		if raw.List == "" {
			t.Fatal("list column should be written")
		}

		getRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
		if editorPrefsDecodeStored(t, getRec) {
			t.Fatalf("只写过 list 后 GET 的 stored 必须仍为 false: %s", getRec.Body.String())
		}
	})

	t.Run("existing row with empty editor", func(t *testing.T) {
		user := testutil.MustCreateUser(t, "list-guard-empty", "operator")
		token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
		if err := database.DB.Create(&model.UserPreference{UserID: user.ID, Editor: ""}).Error; err != nil {
			t.Fatalf("seed empty preference row: %v", err)
		}

		putRec := editorPrefsRequest(t, engine, http.MethodPut, token,
			`{"list":{"envs_page_size":"all","tasks_view_groups_hidden":true}}`)
		if editorPrefsDecodeStored(t, putRec) {
			t.Fatalf("只写 list 的 PUT 响应 stored 必须仍为 false: %s", putRec.Body.String())
		}

		raw, _ := readPreferenceRawColumns(t, user.ID)
		if raw.Editor != "" {
			t.Fatalf("冲突更新路径也不能碰 editor 列，got %q", raw.Editor)
		}
		if editorPrefsDecodeStored(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")) {
			t.Fatal("只写过 list 后 GET 的 stored 必须仍为 false")
		}
	})

	t.Run("editor already stored", func(t *testing.T) {
		user := testutil.MustCreateUser(t, "list-guard-stored", "operator")
		token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

		editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
			`{"editor":{"word_wrap":"off","indent_width":"4","minimap":true}}`))
		before, _ := readPreferenceRawColumns(t, user.ID)
		if before.Editor == "" {
			t.Fatal("editor column should be written by the editor PUT")
		}

		putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_view_all_hidden":false}}`)
		if !editorPrefsDecodeStored(t, putRec) {
			t.Fatalf("editor 已存过时，只写 list 的 PUT 响应 stored 应如实为 true: %s", putRec.Body.String())
		}

		after, _ := readPreferenceRawColumns(t, user.ID)
		if after.Editor != before.Editor {
			t.Fatalf("只写 list 时 editor 列必须逐字节不变\nbefore=%q\nafter =%q", before.Editor, after.Editor)
		}

		editor := editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, ""))
		if editor["word_wrap"] != "off" || editor["indent_width"] != "4" || editor["minimap"] != true {
			t.Fatalf("editor preferences were changed by a list-only PUT: %#v", editor)
		}
	})
}

// ② 只 PUT editor 时，list 列同样一个字节不动；从没存过 list 的用户拿到的是 {}。
func TestUpdateEditorPreferencesNeverTouchesListColumn(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-editor-only", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"whitespace":"all"}}`)
	if list := listPrefsDecode(t, putRec); len(list) != 0 {
		t.Fatalf("只写 editor 后 PUT 响应的 list 必须为 {}，got %#v", list)
	}
	if list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")); len(list) != 0 {
		t.Fatalf("只写 editor 后 GET 的 list 必须为 {}，got %#v", list)
	}
	raw, _ := readPreferenceRawColumns(t, user.ID)
	if raw.List != "" {
		t.Fatalf("只写 editor 时 list 列必须保持空串，got %q", raw.List)
	}

	// 反方向：list 存过之后再只改 editor，list 列逐字节不变。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_page_size":100}}`))
	before, _ := readPreferenceRawColumns(t, user.ID)
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"editor":{"word_wrap":"off"}}`))
	after, _ := readPreferenceRawColumns(t, user.ID)
	if after.List != before.List {
		t.Fatalf("只写 editor 时 list 列必须逐字节不变\nbefore=%q\nafter =%q", before.List, after.List)
	}
}

// ③ 稀疏存储 + 类型契约：只下发存过的键，而且每个键的 JSON 类型要对。
// 前端按类型判定服务端值是否合法（tasks_page_size 是 number、envs_page_size 是 string、两个 hidden 是 bool），
// 类型下发错了它会当作「服务端没有这个键」，转头把本机老值又迁上来，两端永远对不齐。
func TestListPreferencesSparseStorageAndTypes(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-types", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 从没存过：{}，服务端不编造任何默认值。
	if list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")); len(list) != 0 {
		t.Fatalf("新用户的 list 必须为 {}，got %#v", list)
	}

	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_page_size":50}}`))
	list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, ""))
	if len(list) != 1 {
		t.Fatalf("只存了一个键，list 只能下发这一个，got %#v", list)
	}
	if size, ok := list["tasks_page_size"].(float64); !ok || size != 50 {
		t.Fatalf("tasks_page_size must be JSON number 50, got %T %#v", list["tasks_page_size"], list["tasks_page_size"])
	}

	// 逐键合并：后续只带别的键，前面存过的保持原值。
	// tasks_view_groups_hidden 刻意存 false：显式存的 false 必须照常下发，不能被当成「没存过」省略掉。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"list":{"envs_page_size":"all","tasks_view_all_hidden":true,"tasks_view_groups_hidden":false}}`))
	putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_page_size":100}}`)
	for name, current := range map[string]map[string]any{
		"PUT": listPrefsDecode(t, putRec),
		"GET": listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")),
	} {
		if len(current) != 4 {
			t.Fatalf("%s: expected 4 stored keys, got %#v", name, current)
		}
		if size, ok := current["tasks_page_size"].(float64); !ok || size != 100 {
			t.Fatalf("%s: tasks_page_size must be JSON number 100, got %T %#v", name, current["tasks_page_size"], current["tasks_page_size"])
		}
		if envs, ok := current["envs_page_size"].(string); !ok || envs != "all" {
			t.Fatalf("%s: envs_page_size must be JSON string \"all\", got %T %#v", name, current["envs_page_size"], current["envs_page_size"])
		}
		if hidden, ok := current["tasks_view_all_hidden"].(bool); !ok || !hidden {
			t.Fatalf("%s: tasks_view_all_hidden must be JSON bool true, got %T %#v", name, current["tasks_view_all_hidden"], current["tasks_view_all_hidden"])
		}
		if hidden, ok := current["tasks_view_groups_hidden"].(bool); !ok || hidden {
			t.Fatalf("%s: tasks_view_groups_hidden must be JSON bool false, got %T %#v", name, current["tasks_view_groups_hidden"], current["tasks_view_groups_hidden"])
		}
	}
}

// ④ 非法值一律 400，而且一个都不能落库 —— 包括同一个请求里合法的那部分（另一组、同组的其它键）。
func TestUpdateListPreferencesRejectsInvalidValues(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-invalid", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	cases := []struct {
		name string
		body string
		// wantMessage 非空时额外断言报错文案点名了是哪一项；类型错走通用文案，不断言。
		wantMessage string
	}{
		{name: "tasks_page_size out of whitelist", body: `{"list":{"tasks_page_size":30}}`, wantMessage: "tasks_page_size"},
		{name: "tasks_page_size as string", body: `{"list":{"tasks_page_size":"30"}}`},
		{name: "tasks_page_size as fraction", body: `{"list":{"tasks_page_size":50.5}}`},
		{name: "envs_page_size wrong case", body: `{"list":{"envs_page_size":"ALL"}}`, wantMessage: "envs_page_size"},
		{name: "envs_page_size out of whitelist", body: `{"list":{"envs_page_size":"1"}}`, wantMessage: "envs_page_size"},
		{name: "envs_page_size as number", body: `{"list":{"envs_page_size":50}}`},
		{name: "hidden as string", body: `{"list":{"tasks_view_all_hidden":"1"}}`},
		{name: "hidden as number", body: `{"list":{"tasks_view_groups_hidden":1}}`},
		// #147 的键同样只收 JSON bool：APP 等客户端照 editor 组发 "on" 或 1 都要 400，不能被悄悄吞掉。
		{name: "log_open_at_bottom as on", body: `{"list":{"log_open_at_bottom":"on"}}`},
		{name: "log_open_at_bottom as number", body: `{"list":{"log_open_at_bottom":1}}`},
		{name: "list is not an object", body: `{"list":"tasks_page_size=50"}`},
		// 同组里一个合法一个非法：合法的那个也不能落。
		{name: "one valid one invalid key", body: `{"list":{"envs_page_size":"50","tasks_page_size":30}}`, wantMessage: "tasks_page_size"},
		// 两组一起发、list 非法：editor 那组同样不能落，否则 stored 会被翻成 true。
		{name: "valid editor with invalid list", body: `{"editor":{"word_wrap":"off"},"list":{"tasks_page_size":30}}`, wantMessage: "tasks_page_size"},
	}
	for _, tc := range cases {
		rec := editorPrefsRequest(t, engine, http.MethodPut, token, tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d body=%s", tc.name, rec.Code, rec.Body.String())
		}
		if tc.wantMessage != "" && !strings.Contains(rec.Body.String(), tc.wantMessage) {
			t.Fatalf("%s: error message should name %q, got %s", tc.name, tc.wantMessage, rec.Body.String())
		}
	}

	if _, found := readPreferenceRawColumns(t, user.ID); found {
		t.Fatal("非法请求不能建出任何偏好行")
	}
	getRec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
	if list := listPrefsDecode(t, getRec); len(list) != 0 {
		t.Fatalf("非法请求一个键都不能落库，got %#v", list)
	}
	if editorPrefsDecodeStored(t, getRec) {
		t.Fatal("list 非法时同请求里的 editor 也不能落库")
	}
}

// ⑤ 与 editor 一样 per-user：A 改了不该让 B 跟着变。
func TestListPreferencesAreIsolatedPerUser(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	alice := testutil.MustCreateUser(t, "list-alice", "admin")
	bob := testutil.MustCreateUser(t, "list-bob", "viewer")
	aliceToken := testutil.MustCreateAccessToken(t, alice.Username, alice.Role)
	bobToken := testutil.MustCreateAccessToken(t, bob.Username, bob.Role)

	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, aliceToken,
		`{"list":{"tasks_page_size":50,"tasks_view_all_hidden":true}}`))

	if list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, "")); len(list) != 0 {
		t.Fatalf("bob should see an empty list, got %#v", list)
	}

	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, bobToken, `{"list":{"envs_page_size":"100"}}`))

	aliceList := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, aliceToken, ""))
	if len(aliceList) != 2 || aliceList["tasks_page_size"] != float64(50) || aliceList["tasks_view_all_hidden"] != true {
		t.Fatalf("alice list preferences were affected by bob: %#v", aliceList)
	}
	bobList := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, bobToken, ""))
	if len(bobList) != 1 || bobList["envs_page_size"] != "100" {
		t.Fatalf("bob list preferences are wrong: %#v", bobList)
	}
}

// ⑥ list 列里是脏数据时 GET 仍回 200：整列解不开就当 {}，单个键脏了只丢那一个键。
// 这一列纯粹是观感偏好，让一条脏数据把任务页、环境变量页打不开，代价远大于悄悄回落前端默认。
func TestGetListPreferencesDropsCorruptedValues(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-corrupt", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	cases := []struct {
		list string
		want map[string]any
	}{
		{list: `{"tasks_page_size":`, want: map[string]any{}}, // 截断的 JSON
		{list: `not json at all`, want: map[string]any{}},     // 压根不是 JSON
		{list: `null`, want: map[string]any{}},                // 合法 JSON、但不是对象
		{list: `[1,2]`, want: map[string]any{}},               // 数组
		{list: `"all"`, want: map[string]any{}},               // 裸字符串
		// 逐键过白名单与类型：只留下干净的那一个。
		{
			list: `{"tasks_page_size":30,"envs_page_size":"ALL","tasks_view_all_hidden":"1","tasks_view_groups_hidden":true}`,
			want: map[string]any{"tasks_view_groups_hidden": true},
		},
		{list: `{"tasks_page_size":"50","envs_page_size":50}`, want: map[string]any{}}, // 类型对调
		{list: `{"tasks_page_size":50.5}`, want: map[string]any{}},                     // 小数
		// null 必须当「没有这个键」：解到 bool 值上不报错，一不小心就成了显式的 false。
		{list: `{"tasks_view_all_hidden":null,"tasks_page_size":20}`, want: map[string]any{"tasks_page_size": float64(20)}},
		// #147 的键同理：类型不对只丢它自己，null 当没有。
		{list: `{"log_open_at_bottom":"1","tasks_page_size":20}`, want: map[string]any{"tasks_page_size": float64(20)}},
		{list: `{"log_open_at_bottom":null}`, want: map[string]any{}},
		// 白名单外的键不下发。
		{list: `{"tasks_page_size":50,"unknown_key":1}`, want: map[string]any{"tasks_page_size": float64(50)}},
	}
	for _, tc := range cases {
		if err := database.DB.Where("user_id = ?", user.ID).Delete(&model.UserPreference{}).Error; err != nil {
			t.Fatalf("reset preference row: %v", err)
		}
		if err := database.DB.Create(&model.UserPreference{UserID: user.ID, List: tc.list}).Error; err != nil {
			t.Fatalf("seed dirty list %q: %v", tc.list, err)
		}

		rec := editorPrefsRequest(t, engine, http.MethodGet, token, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("dirty list %q should still return 200, got %d body=%s", tc.list, rec.Code, rec.Body.String())
		}
		if got := listPrefsDecode(t, rec); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("dirty list %q: want %#v got %#v", tc.list, tc.want, got)
		}
		// editor 列没存过，stored 不受 list 列脏不脏影响。
		if editorPrefsDecodeStored(t, rec) {
			t.Fatalf("dirty list %q must not affect editor stored flag", tc.list)
		}
	}

	// 在脏行上再写一次：合并以「清洗后的值」为底，脏键不会被原样写回去。
	if err := database.DB.Where("user_id = ?", user.ID).Delete(&model.UserPreference{}).Error; err != nil {
		t.Fatalf("reset preference row: %v", err)
	}
	if err := database.DB.Create(&model.UserPreference{UserID: user.ID, List: `{"tasks_page_size":30,"envs_page_size":"50"}`}).Error; err != nil {
		t.Fatalf("seed dirty list before merge: %v", err)
	}
	merged := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_view_all_hidden":true}}`))
	want := map[string]any{"envs_page_size": "50", "tasks_view_all_hidden": true}
	if !reflect.DeepEqual(merged, want) {
		t.Fatalf("merge on top of a dirty row: want %#v got %#v", want, merged)
	}
	raw, _ := readPreferenceRawColumns(t, user.ID)
	if strings.Contains(raw.List, `"tasks_page_size"`) {
		t.Fatalf("dirty key should be dropped when the list is rewritten, got %q", raw.List)
	}
}

// ⑦ 两组都不带时是 no-op：回 200、不落库、不建行，也不把 editor 的 stored 翻成 true。
// 以前 PUT {} 会顺手写进一套 editor 默认值 —— 正是 ① 那条数据丢失的另一个入口。
// {"list":{}} 与只含白名单外键的 list 同样是 no-op：稀疏存储下没有任何东西可存。
func TestUpdatePreferencesWithoutAnyGroupIsNoop(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "prefs-noop", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	bodies := []string{
		`{}`,
		`{"editor":null,"list":null}`,
		`{"list":{}}`,
		`{"list":{"unknown_key":1}}`,
	}
	for _, body := range bodies {
		rec := editorPrefsRequest(t, engine, http.MethodPut, token, body)
		editorPrefsAssertDefaults(t, editorPrefsDecode(t, rec))
		if editorPrefsDecodeStored(t, rec) {
			t.Fatalf("%s: no-op PUT must keep stored=false, body=%s", body, rec.Body.String())
		}
		if list := listPrefsDecode(t, rec); len(list) != 0 {
			t.Fatalf("%s: no-op PUT must return an empty list, got %#v", body, list)
		}
		if _, found := readPreferenceRawColumns(t, user.ID); found {
			t.Fatalf("%s: no-op PUT must not create a preference row", body)
		}
	}

	// 已经存过两组时，PUT {} 原样回当前值、两列都不动。
	editorPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
		`{"editor":{"word_wrap":"off"},"list":{"tasks_page_size":20}}`))
	before, _ := readPreferenceRawColumns(t, user.ID)

	rec := editorPrefsRequest(t, engine, http.MethodPut, token, `{}`)
	if !editorPrefsDecodeStored(t, rec) {
		t.Fatalf("no-op PUT should report the real stored flag (true here): %s", rec.Body.String())
	}
	if editor := editorPrefsDecode(t, rec); editor["word_wrap"] != "off" {
		t.Fatalf("no-op PUT should return current editor values, got %#v", editor)
	}
	if list := listPrefsDecode(t, rec); list["tasks_page_size"] != float64(20) {
		t.Fatalf("no-op PUT should return current list values, got %#v", list)
	}
	after, _ := readPreferenceRawColumns(t, user.ID)
	if after != before {
		t.Fatalf("no-op PUT must not touch either column\nbefore=%#v\nafter =%#v", before, after)
	}
}

// ⑧ 同时到达、只改不同键的两个 PUT 不能互相覆盖。
//
// 视图管理一次保存勾两个隐藏项，就是同一拍发出两个各带一个键的 PUT；两个标签页各改各的也一样。
// PUT 是「读整行 → 合并 → 整列写回」，不串行化时两段会在语句之间交错：两边都先读到 false、再先后写回，
// 后写的那个把先写的那个键改回 false —— 服务端静默丢一个设置，下次加载前端还会拿它冲掉本机缓存。
// 测试库与生产一样是单连接池（testutil 走 database.Init 的 SetMaxOpenConns(1)），
// 这正是「单连接挡不住两段读-改-写交错」的场景，换成多连接反而测不出来。
// 每轮先把两个键复位成 false 再同时放出一对请求、逐轮断言：一次丢写都不能有。
func TestUpdateListPreferencesConcurrentDifferentKeysDoNotOverwrite(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-concurrent", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	bodies := []string{
		`{"list":{"tasks_view_all_hidden":true}}`,
		`{"list":{"tasks_view_groups_hidden":true}}`,
	}
	const rounds = 20
	for round := 0; round < rounds; round++ {
		listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token,
			`{"list":{"tasks_view_all_hidden":false,"tasks_view_groups_hidden":false}}`))

		start := make(chan struct{})
		codes := make([]int, len(bodies))
		var wg sync.WaitGroup
		for i, body := range bodies {
			wg.Add(1)
			go func(i int, body string) {
				defer wg.Done()
				<-start
				codes[i] = editorPrefsRequest(t, engine, http.MethodPut, token, body).Code
			}(i, body)
		}
		close(start)
		wg.Wait()

		for i, code := range codes {
			if code != http.StatusOK {
				t.Fatalf("round %d: PUT %s expected 200, got %d", round, bodies[i], code)
			}
		}
		list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, ""))
		if list["tasks_view_all_hidden"] != true || list["tasks_view_groups_hidden"] != true {
			t.Fatalf("round %d: 两个并发 PUT 各写一个键，两个键最后都应为 true，实际 %#v", round, list)
		}
	}
}

// ⑨ #147「打开已结束的日志时定位到底部」挂在 list 组上的往返。
// 最要紧的是第一次 PUT：empty() 漏加这个键时，只带它的请求会被判成空补丁、200 静默 no-op，
// 网页和 APP 上表现为「点了开关、刷新又回去了」，不报任何错。
func TestListPreferencesLogOpenAtBottom(t *testing.T) {
	setupEditorPrefsEnv(t)
	engine := newProtectedRouter()

	user := testutil.MustCreateUser(t, "list-log-bottom", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	// 没存过：服务端不下发这个键，各端按自己的默认处理。
	if list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodGet, token, "")); len(list) != 0 {
		t.Fatalf("新用户的 list 必须为 {}，got %#v", list)
	}

	// 先存一个别的键，再只带新键 PUT：两个键要共存，editor 组一个字节都不能动。
	listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"tasks_page_size":50}}`))
	putRec := editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"log_open_at_bottom":true}}`)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"PUT": putRec,
		"GET": editorPrefsRequest(t, engine, http.MethodGet, token, ""),
	} {
		list := listPrefsDecode(t, rec)
		if value, ok := list["log_open_at_bottom"].(bool); !ok || !value {
			t.Fatalf("%s: log_open_at_bottom must be JSON bool true, got %T %#v", name, list["log_open_at_bottom"], list["log_open_at_bottom"])
		}
		if list["tasks_page_size"] != float64(50) {
			t.Fatalf("%s: tasks_page_size should stay 50, got %#v", name, list)
		}
		if editorPrefsDecodeStored(t, rec) {
			t.Fatalf("%s: 只写 list 时 editor 的 stored 必须仍为 false: %s", name, rec.Body.String())
		}
	}
	raw, _ := readPreferenceRawColumns(t, user.ID)
	if raw.Editor != "" {
		t.Fatalf("只写 list 时 editor 列必须保持空串，got %q", raw.Editor)
	}

	// 改回 false：显式存的 false 照常下发，不能被省略成「没存过」——
	// APP 在账户没设过时按自己的默认（底部）走，省略掉等于把用户关掉的开关又打开。
	list := listPrefsDecode(t, editorPrefsRequest(t, engine, http.MethodPut, token, `{"list":{"log_open_at_bottom":false}}`))
	if value, ok := list["log_open_at_bottom"].(bool); !ok || value {
		t.Fatalf("log_open_at_bottom must be JSON bool false, got %T %#v", list["log_open_at_bottom"], list["log_open_at_bottom"])
	}
	if len(list) != 2 {
		t.Fatalf("expected tasks_page_size + log_open_at_bottom, got %#v", list)
	}
}
