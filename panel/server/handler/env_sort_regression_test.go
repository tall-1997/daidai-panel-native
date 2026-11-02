package handler_test

import (
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// mustCreateEnvVars 按顺序落库。Position 别给 0：字段带 default:10000，
// GORM 遇到零值会改用库默认值，排序断言就对不上了。
func mustCreateEnvVars(t *testing.T, envs ...*model.EnvVar) {
	t.Helper()

	for _, env := range envs {
		if err := database.DB.Create(env).Error; err != nil {
			t.Fatalf("create env %q: %v", env.Name, err)
		}
	}
}

func reloadEnvVar(t *testing.T, id uint) model.EnvVar {
	t.Helper()

	var env model.EnvVar
	if err := database.DB.First(&env, id).Error; err != nil {
		t.Fatalf("reload env %d: %v", id, err)
	}
	return env
}

// assertEnvListOrder 逐个比对列表顺序 —— 这个顺序同时就是运行时同名多账号用 & 拼接的顺序。
func assertEnvListOrder(t *testing.T, engine *gin.Engine, token string, want ...string) {
	t.Helper()

	got := envNamesFromListRequest(t, engine, token, "/api/v1/envs?all=1")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected env order %v, got %v", want, got)
	}
}

// envSortBody 拼 PUT /envs/sort 的请求体。position 为空串表示不带这个字段（老客户端、v1.3.9 及以前的 APP 的写法）。
func envSortBody(sourceID, targetID uint, position string) string {
	if position == "" {
		return fmt.Sprintf(`{"source_id":%d,"target_id":%d}`, sourceID, targetID)
	}
	return fmt.Sprintf(`{"source_id":%d,"target_id":%d,"position":%q}`, sourceID, targetID, position)
}

func mustSortEnv(t *testing.T, engine *gin.Engine, token string, sourceID, targetID uint, position string) {
	t.Helper()

	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/envs/sort", envSortBody(sourceID, targetID, position),
		map[string]string{"Authorization": "Bearer " + token}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected sort 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
}

// #131：先置顶的排在前面。置顶改成追加到置顶区末尾（置顶区最大 position + 1000）；
// 存量里旧算法「最小值 - 1000」产生的负数原样保留（不迁移），新置顶的排在它们后面。
func TestEnvMoveTopAppendsToEndOfPinnedArea(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-move-top-order", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	legacy := &model.EnvVar{Name: "LEGACY_PINNED", Value: "0", Enabled: true, SortOrder: 1, Position: -2000}
	alpha := &model.EnvVar{Name: "ALPHA", Value: "1", Enabled: true, Position: 1000}
	beta := &model.EnvVar{Name: "BETA", Value: "2", Enabled: true, Position: 2000}
	gamma := &model.EnvVar{Name: "GAMMA", Value: "3", Enabled: true, Position: 3000}
	mustCreateEnvVars(t, legacy, alpha, beta, gamma)

	for _, env := range []*model.EnvVar{alpha, beta} {
		rec := performRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d/move-top", env.ID), map[string]string{
			"Authorization": "Bearer " + token,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected move-top %s 200, got %d, body=%s", env.Name, rec.Code, rec.Body.String())
		}
	}

	assertEnvListOrder(t, engine, token, "LEGACY_PINNED", "ALPHA", "BETA", "GAMMA")

	for _, env := range []*model.EnvVar{alpha, beta} {
		if stored := reloadEnvVar(t, env.ID); stored.SortOrder != 1 {
			t.Fatalf("expected %s pinned (sort_order=1), got %d", env.Name, stored.SortOrder)
		}
	}
	if stored := reloadEnvVar(t, legacy.ID); stored.Position != -2000 {
		t.Fatalf("存量置顶项的 position 不该被迁移，expected -2000, got %v", stored.Position)
	}
}

// 对已置顶项再调一次 move-top 必须原样返回：Web 菜单按状态互斥不会发出这种请求，
// 但 Open API 可以直调，再追加一次会把它从置顶区中间挪到末尾，等于偷偷改了用户排好的顺序。
func TestEnvMoveTopIsIdempotentForPinnedItem(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-move-top-idempotent", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	first := &model.EnvVar{Name: "FIRST_PINNED", Value: "1", Enabled: true, SortOrder: 1, Position: 1000}
	second := &model.EnvVar{Name: "SECOND_PINNED", Value: "2", Enabled: true, SortOrder: 1, Position: 2000}
	normal := &model.EnvVar{Name: "NORMAL", Value: "3", Enabled: true, Position: 1000}
	mustCreateEnvVars(t, first, second, normal)

	rec := performRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d/move-top", first.ID), map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected move-top on pinned env 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	stored := reloadEnvVar(t, first.ID)
	if stored.SortOrder != 1 || stored.Position != 1000 {
		t.Fatalf("expected pinned env untouched (sort_order=1, position=1000), got sort_order=%d position=%v", stored.SortOrder, stored.Position)
	}
	assertEnvListOrder(t, engine, token, "FIRST_PINNED", "SECOND_PINNED", "NORMAL")
}

// #131：PUT /envs/sort 支持 position:"after"（契约 C4，与 /tasks/sort 同名同义）；
// 不带或写成别的值一律按 before，老客户端不带这个键，行为不变。
func TestEnvSortSupportsPositionAfter(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-sort-after", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	a := &model.EnvVar{Name: "A", Value: "1", Enabled: true, Position: 1000}
	b := &model.EnvVar{Name: "B", Value: "2", Enabled: true, Position: 2000}
	c := &model.EnvVar{Name: "C", Value: "3", Enabled: true, Position: 3000}
	d := &model.EnvVar{Name: "D", Value: "4", Enabled: true, Position: 4000}
	mustCreateEnvVars(t, a, b, c, d)

	mustSortEnv(t, engine, token, a.ID, c.ID, "after")
	assertEnvListOrder(t, engine, token, "B", "C", "A", "D")

	mustSortEnv(t, engine, token, d.ID, b.ID, "")
	assertEnvListOrder(t, engine, token, "D", "B", "C", "A")

	mustSortEnv(t, engine, token, a.ID, d.ID, "before")
	assertEnvListOrder(t, engine, token, "A", "D", "B", "C")

	// 大小写与首尾空白按 /tasks/sort 的口径放宽。
	mustSortEnv(t, engine, token, b.ID, c.ID, " AFTER ")
	assertEnvListOrder(t, engine, token, "A", "D", "C", "B")
}

// #131：置顶项要能拖到置顶区末尾。原来前端落在置顶区最后一格时下一行必然是普通项，
// 会被当成跨区拦下；现在发「插到上一个置顶项之后」，服务端必须照办且仍留在置顶区。
func TestEnvSortMovesPinnedItemToEndOfPinnedArea(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-sort-pinned-tail", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	p1 := &model.EnvVar{Name: "P1", Value: "1", Enabled: true, SortOrder: 1, Position: 1000}
	p2 := &model.EnvVar{Name: "P2", Value: "2", Enabled: true, SortOrder: 1, Position: 2000}
	p3 := &model.EnvVar{Name: "P3", Value: "3", Enabled: true, SortOrder: 1, Position: 3000}
	n1 := &model.EnvVar{Name: "N1", Value: "4", Enabled: true, Position: 1000}
	mustCreateEnvVars(t, p1, p2, p3, n1)

	mustSortEnv(t, engine, token, p1.ID, p3.ID, "after")
	assertEnvListOrder(t, engine, token, "P2", "P3", "P1", "N1")
	if stored := reloadEnvVar(t, p1.ID); stored.SortOrder != 1 {
		t.Fatalf("expected P1 to stay pinned after moving to the pinned tail, got sort_order=%d", stored.SortOrder)
	}

	// 跨区仍然拒绝：after 只是换了落点的表达方式，不放宽分桶规则。
	rec := performJSONRequest(engine, http.MethodPut, "/api/v1/envs/sort", envSortBody(p2.ID, n1.ID, "after"),
		map[string]string{"Authorization": "Bearer " + token}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-bucket sort 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
	assertEnvListOrder(t, engine, token, "P2", "P3", "P1", "N1")
}

// APP #16（契约 C3）：不带 target_id（或写成 null）= 把 source 移到它自己所在那个桶的末尾，此时不看 position。
// 这是对外文档化的契约（apiData.ts「不传 target_id 表示移到本区末尾」），APP v1.4.0 起在没有筛选时
// 把变量拖到可见列表末尾就发这种请求（永远带 "position":"after"），置顶项、普通项都会发；以前没有任何用例钉住它。
func TestEnvSortWithoutTargetMovesSourceToEndOfItsBucket(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-sort-null-target", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	p1 := &model.EnvVar{Name: "P1", Value: "1", Enabled: true, SortOrder: 1, Position: 1000}
	p2 := &model.EnvVar{Name: "P2", Value: "2", Enabled: true, SortOrder: 1, Position: 2000}
	a := &model.EnvVar{Name: "A", Value: "3", Enabled: true, Position: 1000}
	b := &model.EnvVar{Name: "B", Value: "4", Enabled: true, Position: 2000}
	c := &model.EnvVar{Name: "C", Value: "5", Enabled: true, Position: 3000}
	mustCreateEnvVars(t, p1, p2, a, b, c)

	steps := []struct {
		body string
		want []string
	}{
		// 不带 target_id 这个键：普通项移到普通区末尾
		{fmt.Sprintf(`{"source_id":%d}`, a.ID), []string{"P1", "P2", "B", "C", "A"}},
		// 显式写 null：同上
		{fmt.Sprintf(`{"source_id":%d,"target_id":null}`, b.ID), []string{"P1", "P2", "C", "A", "B"}},
		// 置顶项：移到置顶区末尾，仍是置顶（不会被甩进普通区）
		{fmt.Sprintf(`{"source_id":%d,"target_id":null}`, p1.ID), []string{"P2", "P1", "C", "A", "B"}},
		// target 为空时 position 不起作用：普通源带 after 也还是本桶末尾
		{fmt.Sprintf(`{"source_id":%d,"target_id":null,"position":"after"}`, c.ID), []string{"P2", "P1", "A", "B", "C"}},
		// 置顶源带 after 同理：APP v1.4.0 把置顶项拖到置顶区末尾时发的就是这个请求
		{fmt.Sprintf(`{"source_id":%d,"target_id":null,"position":"after"}`, p2.ID), []string{"P1", "P2", "A", "B", "C"}},
	}
	for _, step := range steps {
		rec := performJSONRequest(engine, http.MethodPut, "/api/v1/envs/sort", step.body, headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d, body=%s", step.body, rec.Code, rec.Body.String())
		}
		assertEnvListOrder(t, engine, token, step.want...)
	}
	for _, pinned := range []*model.EnvVar{p1, p2} {
		if stored := reloadEnvVar(t, pinned.ID); stored.SortOrder != 1 {
			t.Fatalf("%s 移到置顶区末尾后应仍是置顶，got sort_order=%d", pinned.Name, stored.SortOrder)
		}
	}
}

// #131：PUT /envs/:id 可写 position（契约 C5，桶内排序值）：值变化才写入，不传不动，非法值 400。
func TestEnvUpdateAcceptsPositionAndRejectsInvalidValues(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "env-update-position", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	headers := map[string]string{"Authorization": "Bearer " + token}

	a := &model.EnvVar{Name: "A", Value: "1", Enabled: true, Position: 1000}
	b := &model.EnvVar{Name: "B", Value: "2", Enabled: true, Position: 2000}
	c := &model.EnvVar{Name: "C", Value: "3", Enabled: true, Position: 3000}
	mustCreateEnvVars(t, a, b, c)

	rec := performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", c.ID), `{"position":1500.5}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected update position 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	data, ok := decodeJSONMap(t, rec)["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env payload, got %s", rec.Body.String())
	}
	if got, _ := data["position"].(float64); got != 1500.5 {
		t.Fatalf("expected response position 1500.5, got %#v", data["position"])
	}
	assertEnvListOrder(t, engine, token, "A", "C", "B")

	// 负数与小数都是合法排序值（存量里本来就有旧置顶算法留下的负数）。
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", b.ID), `{"position":-5}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected negative position 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	assertEnvListOrder(t, engine, token, "B", "A", "C")

	for _, body := range []string{`{"position":"abc"}`, `{"position":1e999}`, `{"position":true}`} {
		rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", a.ID), body, headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid position %s to be rejected with 400, got %d, body=%s", body, rec.Code, rec.Body.String())
		}
	}
	if stored := reloadEnvVar(t, a.ID); stored.Position != 1000 {
		t.Fatalf("rejected updates must not touch position, expected 1000, got %v", stored.Position)
	}

	// 不带 position 的更新（App 的写法）不能动排序值。
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", a.ID), `{"remarks":"只改备注"}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected remarks update 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if stored := reloadEnvVar(t, a.ID); stored.Position != 1000 || stored.Remarks != "只改备注" {
		t.Fatalf("expected remarks updated with position untouched, got position=%v remarks=%q", stored.Position, stored.Remarks)
	}

	// 值没变不算变更。
	rec = performJSONRequest(engine, http.MethodPut, fmt.Sprintf("/api/v1/envs/%d", a.ID), `{"position":1000}`, headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected unchanged position 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	if got, _ := decodeJSONMap(t, rec)["message"].(string); got != "未检测到字段变更" {
		t.Fatalf("expected unchanged position to report no change, got %q", got)
	}
}
