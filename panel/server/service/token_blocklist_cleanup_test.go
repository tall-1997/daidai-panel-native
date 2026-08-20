package service

import (
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func seedBlocklistRow(t *testing.T, jti string, expiresAt time.Time) {
	t.Helper()

	row := model.TokenBlocklist{
		JTI:       jti,
		TokenType: "access",
		RevokedAt: time.Now().Add(-time.Hour),
		ExpiresAt: expiresAt,
	}
	if err := database.DB.Create(&row).Error; err != nil {
		t.Fatalf("seed blocklist row %q: %v", jti, err)
	}
}

func blocklistJTIs(t *testing.T) []string {
	t.Helper()

	var rows []model.TokenBlocklist
	if err := database.DB.Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load blocklist rows: %v", err)
	}
	jtis := make([]string, 0, len(rows))
	for _, row := range rows {
		jtis = append(jtis, row.JTI)
	}
	return jtis
}

// 守卫缺口 3：D1 之后每跑一次任务 / 一次 ddp 会话 / 一次调试运行都会写一行黑名单，
// 不清理就是无界增长。清理必须只删已过期的行 —— 未过期的行还在真正拦截 token。
func TestCleanExpiredTokenBlocklistOnlyRemovesExpiredRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	now := time.Now()
	seedBlocklistRow(t, "expired-long-ago", now.Add(-7*24*time.Hour))
	seedBlocklistRow(t, "expired-just-now", now.Add(-time.Second))
	seedBlocklistRow(t, "still-valid-1h", now.Add(time.Hour))
	seedBlocklistRow(t, "still-valid-7d", now.Add(7*24*time.Hour))

	removed, err := CleanExpiredTokenBlocklist()
	if err != nil {
		t.Fatalf("clean expired blocklist: %v", err)
	}
	if removed != 2 {
		t.Fatalf("expected 2 expired rows removed, got %d", removed)
	}

	remaining := blocklistJTIs(t)
	if len(remaining) != 2 || remaining[0] != "still-valid-1h" || remaining[1] != "still-valid-7d" {
		t.Fatalf("expected only unexpired rows to survive, got %#v", remaining)
	}
}

// 没有过期行时必须是 no-op，不能顺手清掉别的东西。
func TestCleanExpiredTokenBlocklistIsNoopWithoutExpiredRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	seedBlocklistRow(t, "still-valid", time.Now().Add(time.Hour))

	removed, err := CleanExpiredTokenBlocklist()
	if err != nil {
		t.Fatalf("clean expired blocklist: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected no rows removed, got %d", removed)
	}
	if got := blocklistJTIs(t); len(got) != 1 {
		t.Fatalf("expected the unexpired row to stay, got %#v", got)
	}
}

// 清理挂在后台 worker 上，可能在 DB 尚未初始化时被触发；不能 panic。
func TestCleanExpiredTokenBlocklistHandlesNilDB(t *testing.T) {
	previous := database.DB
	database.DB = nil
	t.Cleanup(func() {
		database.DB = previous
	})

	removed, err := CleanExpiredTokenBlocklist()
	if err != nil {
		t.Fatalf("expected nil DB to be a safe no-op, got error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("expected 0 removed rows with nil DB, got %d", removed)
	}
}

// 清理函数光存在没用，必须真的挂在 6 小时 ticker 的那条清理链上。
func TestPeriodicCleanupRemovesExpiredTokenBlocklistRows(t *testing.T) {
	testutil.SetupTestEnv(t)

	seedBlocklistRow(t, "periodic-expired", time.Now().Add(-time.Hour))
	seedBlocklistRow(t, "periodic-valid", time.Now().Add(time.Hour))

	runPeriodicCleanup()

	remaining := blocklistJTIs(t)
	if len(remaining) != 1 || remaining[0] != "periodic-valid" {
		t.Fatalf("expected periodic cleanup to drop only the expired row, got %#v", remaining)
	}
}

// 吊销后立刻清理不能把还在生效的拦截记录删掉：
// 凭据的 exp 还没到，黑名单行就必须留着。
func TestRevokedScriptTokenSurvivesCleanupUntilItExpires(t *testing.T) {
	testutil.SetupTestEnv(t)

	info := &ScriptTokenInfo{JTI: "live-script-token", ExpiresAt: time.Now().Add(UntimedTaskScriptTokenTTL)}
	RevokeScriptToken(info)

	if _, err := CleanExpiredTokenBlocklist(); err != nil {
		t.Fatalf("clean expired blocklist: %v", err)
	}

	var count int64
	database.DB.Model(&model.TokenBlocklist{}).Where("jti = ?", info.JTI).Count(&count)
	if count != 1 {
		t.Fatalf("expected the revoked-but-unexpired token to stay blocked, got %d rows", count)
	}
}
