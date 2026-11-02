package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/handler"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// ddp reset-password / reset-username 也要撤销该用户的全部登录会话（含续期出的令牌），与网页改密、改名一致。
// 命令行是独立进程、直接写同一个库，运行中的面板下一个请求就能看到；这里用同一个库模拟「面板在跑、命令行改密」。
// 只用改动前就有的 API，同一份文件放到改动前的代码上也能编译（在那里两条都是红的）。

const rvCLIPassword = "Password123!"

func newRVCLIPanel(t *testing.T) *gin.Engine {
	t.Helper()
	testutil.SetupTestEnv(t)
	// 有效期取生产默认值
	config.C.JWT.AccessTokenExpire = 480 * time.Hour
	config.C.JWT.RefreshTokenExpire = 1440 * time.Hour

	engine := gin.New()
	api := engine.Group("/api/v1")
	handler.NewAuthHandler().RegisterRoutes(api)
	handler.NewSystemHandler().RegisterRoutes(api)
	return engine
}

func rvCLIRequest(engine *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// rvCLILogin 走面板的真实登录并续期一次，返回登录时的 A1、续期出的 A2 与 R1。
func rvCLILogin(t *testing.T, engine *gin.Engine, username string) (string, string, string) {
	t.Helper()
	rec := rvCLIRequest(engine, http.MethodPost, "/api/v1/auth/login", "",
		fmt.Sprintf(`{"username":%q,"password":%q}`, username, rvCLIPassword))
	var login struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &login) != nil || login.AccessToken == "" {
		t.Fatalf("login: got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = rvCLIRequest(engine, http.MethodPost, "/api/v1/auth/refresh", login.RefreshToken, "")
	var refreshed struct {
		AccessToken string `json:"access_token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &refreshed) != nil || refreshed.AccessToken == "" {
		t.Fatalf("refresh: got %d body=%s", rec.Code, rec.Body.String())
	}
	return login.AccessToken, refreshed.AccessToken, login.RefreshToken
}

func TestRVCLIResetPasswordRevokesSessions(t *testing.T) {
	engine := newRVCLIPanel(t)
	user := testutil.MustCreateLoginUser(t, "rv_cli_password", "admin", rvCLIPassword)
	a1, a2, r1 := rvCLILogin(t, engine, user.Username)

	rt := &cliRuntime{cfg: config.C}
	if err := runResetPassword(rt, []string{user.Username, "NewPassword456!"}); err != nil {
		t.Fatalf("ddp reset-password: %v", err)
	}

	if code := rvCLIRequest(engine, http.MethodGet, "/api/v1/system/version", a1, "").Code; code != http.StatusUnauthorized {
		t.Errorf("A1 after ddp reset-password = %d, want 401", code)
	}
	if code := rvCLIRequest(engine, http.MethodGet, "/api/v1/system/version", a2, "").Code; code != http.StatusUnauthorized {
		t.Errorf("A2 after ddp reset-password = %d, want 401", code)
	}
	if code := rvCLIRequest(engine, http.MethodPost, "/api/v1/auth/refresh", r1, "").Code; code != http.StatusUnauthorized {
		t.Errorf("R1 after ddp reset-password = %d, want 401", code)
	}
}

func TestRVCLIResetUsernameRevokesSessions(t *testing.T) {
	engine := newRVCLIPanel(t)
	user := testutil.MustCreateLoginUser(t, "rv_cli_username", "admin", rvCLIPassword)
	a1, a2, r1 := rvCLILogin(t, engine, user.Username)

	rt := &cliRuntime{cfg: config.C}
	if err := runResetUsername(rt, []string{user.Username, "rv_cli_renamed"}); err != nil {
		t.Fatalf("ddp reset-username: %v", err)
	}

	if code := rvCLIRequest(engine, http.MethodGet, "/api/v1/system/version", a1, "").Code; code != http.StatusUnauthorized {
		t.Errorf("A1 after ddp reset-username = %d, want 401", code)
	}
	if code := rvCLIRequest(engine, http.MethodGet, "/api/v1/system/version", a2, "").Code; code != http.StatusUnauthorized {
		t.Errorf("A2 after ddp reset-username = %d, want 401", code)
	}
	if code := rvCLIRequest(engine, http.MethodPost, "/api/v1/auth/refresh", r1, "").Code; code != http.StatusUnauthorized {
		t.Errorf("R1 after ddp reset-username = %d, want 401", code)
	}
}
