package main

import (
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/middleware"
	"daidai-panel/service"
	"daidai-panel/testutil"
)

// 守卫缺口 1：ddp python / ddp shell 历史上自己定义了一个 365 天的 TTL，
// 与任务侧的 7 天兜底分叉。现在必须是同一个常量、同一个数字。
func TestBuildRuntimeEnvReusesUntimedTaskScriptTokenTTL(t *testing.T) {
	testutil.SetupTestEnv(t)
	rt := &cliRuntime{cfg: config.C}

	before := time.Now()
	envMap, scriptToken := buildRuntimeEnv(rt, config.C.Data.ScriptsDir)

	token := envMap["DAIDAI_TOKEN"]
	if token == "" {
		t.Fatalf("expected DAIDAI_TOKEN in ddp runtime env, got %#v", envMap)
	}
	if envMap["DAIDAI_NOTIFY_TOKEN"] != token {
		t.Fatalf("expected DAIDAI_NOTIFY_TOKEN and DAIDAI_TOKEN to share one credential")
	}

	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse ddp runtime token: %v", err)
	}

	lifetime := claims.ExpiresAt.Time.Sub(before)
	if lifetime > service.UntimedTaskScriptTokenTTL+time.Minute {
		t.Fatalf("expected ddp runtime token to live at most %v, got %v", service.UntimedTaskScriptTokenTTL, lifetime)
	}
	if lifetime < service.UntimedTaskScriptTokenTTL-time.Minute {
		t.Fatalf("expected ddp runtime token to live about %v, got %v", service.UntimedTaskScriptTokenTTL, lifetime)
	}

	if scriptToken == nil || scriptToken.JTI != claims.ID {
		t.Fatalf("expected returned script token to carry the injected jti %q, got %#v", claims.ID, scriptToken)
	}
}

// 会话结束后凭据必须立刻作废，不能只靠 TTL 兜底。
func TestBuildRuntimeEnvTokenIsRevocable(t *testing.T) {
	testutil.SetupTestEnv(t)
	rt := &cliRuntime{cfg: config.C}

	envMap, scriptToken := buildRuntimeEnv(rt, config.C.Data.ScriptsDir)
	claims, err := middleware.ParseToken(envMap["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse ddp runtime token: %v", err)
	}

	if middleware.IsTokenBlocked(claims.ID) {
		t.Fatal("ddp runtime token must stay usable while the session is running")
	}

	service.RevokeScriptToken(scriptToken)

	if !middleware.IsTokenBlocked(claims.ID) {
		t.Fatalf("expected ddp runtime token %q to be revoked after the session ends", claims.ID)
	}
}
