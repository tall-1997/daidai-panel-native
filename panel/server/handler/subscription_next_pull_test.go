package handler_test

import (
	"net/http"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// TestSubscriptionListNextPullAt 守住订阅列表的「下次拉取」（issue #163）：
// 启用且有定时 → 下发 next_pull_at，且时间在未来；禁用、或定时为空 → 不带这个键（前端显示 -）。
func TestSubscriptionListNextPullAt(t *testing.T) {
	testutil.SetupTestEnv(t)

	operator := testutil.MustCreateUser(t, "subscription-next-pull", "operator")
	token := testutil.MustCreateAccessToken(t, operator.Username, operator.Role)
	engine := newProtectedRouter()

	subs := []model.Subscription{
		// 6 段（带秒）和 5 段都要能算，调度器两种都认。
		{Name: "next-pull-enabled-6", Type: model.SubTypeGitRepo, URL: "https://github.com/example/a.git", Schedule: "0 0 3 * * *"},
		{Name: "next-pull-enabled-5", Type: model.SubTypeGitRepo, URL: "https://github.com/example/b.git", Schedule: "30 4 * * *"},
		{Name: "next-pull-disabled", Type: model.SubTypeGitRepo, URL: "https://github.com/example/c.git", Schedule: "0 0 3 * * *"},
		{Name: "next-pull-no-schedule", Type: model.SubTypeGitRepo, URL: "https://github.com/example/d.git", Schedule: ""},
	}
	for i := range subs {
		if err := database.DB.Create(&subs[i]).Error; err != nil {
			t.Fatalf("create subscription %s: %v", subs[i].Name, err)
		}
	}
	// enabled 列带 default:true，Create 时的 false 会被当零值忽略，只能建完再改。
	if err := database.DB.Model(&model.Subscription{}).Where("name = ?", "next-pull-disabled").Update("enabled", false).Error; err != nil {
		t.Fatalf("disable subscription: %v", err)
	}

	rec := performRequest(engine, http.MethodGet, "/api/v1/subscriptions?keyword=next-pull-", map[string]string{
		"Authorization": "Bearer " + token,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	items, ok := decodeJSONMap(t, rec)["data"].([]interface{})
	if !ok || len(items) != len(subs) {
		t.Fatalf("expected %d subscriptions, got %#v", len(subs), items)
	}

	byName := map[string]map[string]interface{}{}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("expected list item map, got %T", raw)
		}
		name, _ := item["name"].(string)
		byName[name] = item
	}

	for _, name := range []string{"next-pull-enabled-6", "next-pull-enabled-5"} {
		value, ok := byName[name]["next_pull_at"].(string)
		if !ok {
			t.Fatalf("%s: expected next_pull_at string, got %#v", name, byName[name]["next_pull_at"])
		}
		next, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			t.Fatalf("%s: parse next_pull_at %q: %v", name, value, err)
		}
		if !next.After(time.Now()) {
			t.Fatalf("%s: expected next_pull_at in the future, got %s", name, value)
		}
	}
	for _, name := range []string{"next-pull-disabled", "next-pull-no-schedule"} {
		if _, exists := byName[name]["next_pull_at"]; exists {
			t.Fatalf("%s: expected no next_pull_at, got %#v", name, byName[name]["next_pull_at"])
		}
	}
}
