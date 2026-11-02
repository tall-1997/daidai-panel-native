package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 登录会话撤销缺口的回归用例，契约见 .trellis/spec/backend/quality-guidelines.md「登录会话号与撤销契约」。
//
// 写法约束：除日志流那条外，都只用 HTTP 与改动前就有的 API。把这些用例（日志流复查那条除外）放到改动前的代码上，
// Guard 与「老登录令牌照常可用」几条是绿的，其余是红的，红的正是缺口。
// 注意：本文件在改动前的代码上并不能直接编译——TestRVLogStreamEndsWithReconnectAfterRevoke 引用了改动后才有的
// 包级变量 logStreamHeartbeatInterval，要先补一行编译垫片（var logStreamHeartbeatInterval = 30 * time.Second）才编得过；
// 而且日志流那条复查行为在旧代码上本来就是红的。
//
// 记号：A1 = 登录时签发的 access，A2 = 续期得来的 access，R1 = refresh。

const rvPassword = "Password123!"

// rvEnv 是这组用例共用的面板：真实的登录、续期、退出、用户管理、会话管理、日志流与 MCP 路由。
type rvEnv struct {
	t      *testing.T
	engine *gin.Engine
	nextIP int
}

func newRVEnv(t *testing.T) *rvEnv {
	t.Helper()
	testutil.SetupTestEnv(t)
	// 有效期取生产默认值：testutil 默认的 1h / 2h 会掩盖「兜底只写 24 小时」这类问题
	config.C.JWT.AccessTokenExpire = 480 * time.Hour
	config.C.JWT.RefreshTokenExpire = 1440 * time.Hour

	engine := gin.New()
	api := engine.Group("/api/v1")
	NewAuthHandler().RegisterRoutes(api)
	NewUserHandler().RegisterRoutes(api)
	NewSecurityHandler().RegisterRoutes(api)
	NewSystemHandler().RegisterRoutes(api)
	NewTaskHandler().RegisterRoutes(api)
	NewLogHandler().RegisterRoutes(api)
	NewOpenAPIHandler().RegisterRoutes(api)
	NewMCPHandler(engine).RegisterRoutes(api)
	// 只挂 JWTAuth 的探针：数「一次鉴权查了几次库」时不掺业务接口自己的查询
	engine.GET("/rv-probe", middleware.JWTAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return &rvEnv{t: t, engine: engine}
}

func (e *rvEnv) do(method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// loginRequest 走真实的 /auth/login。每次换一个来源 IP 避开登录限流；app 为 true 时带 APP 的请求头，落成 APP 会话。
func (e *rvEnv) loginRequest(username string, app bool) *httptest.ResponseRecorder {
	e.nextIP++
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q}`, username, rvPassword)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = fmt.Sprintf("198.51.100.%d:12345", e.nextIP)
	if app {
		req.Header.Set("User-Agent", "Dart/3.11 (dart:io)")
		req.Header.Set("X-Client-Type", "app")
		req.Header.Set("X-Client-App", "daidai-panel-app")
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	}
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec
}

// login 登录成功并返回 A1 与 R1。
func (e *rvEnv) login(username string, app bool) (string, string) {
	e.t.Helper()
	rec := e.loginRequest(username, app)
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &payload) != nil || payload.AccessToken == "" {
		e.t.Fatalf("login %s: expected 200 with tokens, got %d body=%s", username, rec.Code, rec.Body.String())
	}
	return payload.AccessToken, payload.RefreshToken
}

// refresh 走真实的 /auth/refresh，返回状态码与新 access（失败时为空串）。
func (e *rvEnv) refresh(refreshToken string) (int, string) {
	rec := e.do(http.MethodPost, "/api/v1/auth/refresh", refreshToken, "")
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return rec.Code, payload.AccessToken
}

// mustRefresh 续期一次并断言成功，返回这个会话续期出的 access（A2）。
func (e *rvEnv) mustRefresh(refreshToken string) string {
	e.t.Helper()
	code, access := e.refresh(refreshToken)
	if code != http.StatusOK || access == "" {
		e.t.Fatalf("refresh: expected 200 with a new access token, got %d", code)
	}
	return access
}

// status 拿 token 调一个只要求登录的业务接口，返回状态码。
func (e *rvEnv) status(token string) int {
	return e.do(http.MethodGet, "/api/v1/system/version", token, "").Code
}

// mcp 拿 token 走 MCP 的 Bearer 鉴权发一次 initialize，返回状态码。
func (e *rvEnv) mcp(token string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"rv-test","version":"1.0"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	e.engine.ServeHTTP(rec, req)
	return rec.Code
}

// appToken 走真实的 POST /open-api/token 换一枚应用令牌：username = app:<key>，没有 jti。
// 不用 testutil.MustCreateAppToken：它经 GenerateAccessToken 签发、带着 jti，与线上应用令牌的形状不同。
func (e *rvEnv) appToken(appKey, scopes string) string {
	e.t.Helper()
	app := testutil.MustCreateOpenApp(e.t, appKey, scopes)
	rec := e.do(http.MethodPost, "/api/v1/open-api/token", "",
		fmt.Sprintf(`{"app_key":%q,"app_secret":%q}`, app.AppKey, app.AppSecret))
	var payload struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &payload) != nil || payload.Data.AccessToken == "" {
		e.t.Fatalf("exchange open api token: got %d body=%s", rec.Code, rec.Body.String())
	}
	return payload.Data.AccessToken
}

// adminToken 建一个管理员并直接签一枚令牌，当作「操作者」（撤销、改角色、禁用、删除……）。
func (e *rvEnv) adminToken(username string) string {
	e.t.Helper()
	admin := testutil.MustCreateUser(e.t, username, "admin")
	return testutil.MustCreateAccessToken(e.t, admin.Username, admin.Role)
}

// sessionOf 取某个用户最新的一行登录会话。
func (e *rvEnv) sessionOf(userID uint) model.UserSession {
	e.t.Helper()
	var session model.UserSession
	if err := database.DB.Where("user_id = ?", userID).Order("id DESC").First(&session).Error; err != nil {
		e.t.Fatalf("load session of user %d: %v", userID, err)
	}
	return session
}

// rvBodyError 取 401 响应体里的 error 与 code。
func rvBodyError(rec *httptest.ResponseRecorder) (string, string) {
	var payload struct {
		Error string `json:"error"`
		Code  string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &payload)
	return payload.Error, payload.Code
}

// rvCleanupAsOf 按 CleanExpiredTokenBlocklist 的条件删黑名单，只是把「现在」换成 at：
// 模拟 6 小时一次的清理任务在 at 那一刻跑过。token 此刻并没有过期，正好看它会不会复活。
func rvCleanupAsOf(t *testing.T, at time.Time) {
	t.Helper()
	if err := database.DB.Where("expires_at < ?", at).Delete(&model.TokenBlocklist{}).Error; err != nil {
		t.Fatalf("simulate blocklist cleanup: %v", err)
	}
}

func rvTokenExpiry(t *testing.T, token string) time.Time {
	t.Helper()
	claims, err := middleware.ParseToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	return claims.ExpiresAt.Time
}

// rvClaim 读 token 里的一个字符串字段。不走 middleware.Claims：改动前它还没有 sid 字段。
func rvClaim(t *testing.T, token, key string) string {
	t.Helper()
	parsed, err := jwt.Parse(token, func(*jwt.Token) (interface{}, error) {
		return []byte(config.C.JWT.Secret), nil
	})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	value, _ := parsed.Claims.(jwt.MapClaims)[key].(string)
	return value
}

// rvSignLegacyAccess 按升级前的形状手签一枚用户 access：有 jti，没有 sid。
func rvSignLegacyAccess(t *testing.T, username, role, jti string) string {
	t.Helper()
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username":   username,
		"role":       role,
		"token_type": "access",
		"exp":        now.Add(config.C.JWT.AccessTokenExpire).Unix(),
		"iat":        now.Unix(),
		"jti":        jti,
	}).SignedString([]byte(config.C.JWT.Secret))
	if err != nil {
		t.Fatalf("sign legacy access token: %v", err)
	}
	return token
}

// rvSeedLegacySession 造一个升级前的登录会话：会话行记着登录那枚 access 的 jti 与 refresh 的 jti，返回 R1。
func rvSeedLegacySession(t *testing.T, user *model.User, accessJTI string) string {
	t.Helper()
	refresh, err := middleware.GenerateRefreshTokenInfo(user.Username, user.Role)
	if err != nil {
		t.Fatalf("generate refresh token: %v", err)
	}
	session := model.UserSession{
		UserID:           user.ID,
		Username:         user.Username,
		JTI:              accessJTI,
		RefreshJTI:       refresh.JTI,
		ClientType:       "web",
		ExpiresAt:        time.Now().Add(config.C.JWT.AccessTokenExpire),
		RefreshExpiresAt: &refresh.ExpiresAt,
	}
	if err := database.DB.Create(&session).Error; err != nil {
		t.Fatalf("seed legacy session: %v", err)
	}
	return refresh.Token
}

// rvSQLCounter 数 GORM 发出的 SQL：logger 的 Trace 对每条语句调一次。
type rvSQLCounter struct {
	logger.Interface
	statements int
}

func (r *rvSQLCounter) LogMode(logger.LogLevel) logger.Interface { return r }

func (r *rvSQLCounter) Trace(context.Context, time.Time, func() (string, int64), error) {
	r.statements++
}

func rvCountStatements(fn func()) int {
	counter := &rvSQLCounter{Interface: logger.Discard}
	previous := database.DB.Logger
	database.DB.Logger = counter
	defer func() { database.DB.Logger = previous }()
	fn()
	return counter.statements
}

// V1：管理员撤销一个会话后，这个会话续期出的 A2 也必须失效；清理任务在 A2 到期前跑过之后也不能复活。
// MCP 的 Bearer 鉴权必须与 JWTAuth 同一个撤销判定（评审必做 3）。
func TestRVAdminRevokeKillsRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	adminToken := env.adminToken("rv-admin")
	target := testutil.MustCreateLoginUser(t, "rv_target", "operator", rvPassword)
	a1, r1 := env.login(target.Username, false)
	a2 := env.mustRefresh(r1)

	if err := model.SetConfig(model.MCPEnabledConfigKey, "true"); err != nil {
		t.Fatalf("enable mcp: %v", err)
	}
	// 撤销前 A2 要能过 MCP 的 Bearer 鉴权，后面的 401 才说明问题
	if code := env.mcp(a2); code != http.StatusOK {
		t.Fatalf("A2 on MCP before revoke = %d, want 200", code)
	}

	session := env.sessionOf(target.ID)
	if rec := env.do(http.MethodDelete, fmt.Sprintf("/api/v1/security/sessions/%d", session.ID), adminToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke session: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a1); code != http.StatusUnauthorized {
		t.Errorf("A1 after revoke = %d, want 401", code)
	}
	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2(refreshed) after revoke = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after revoke = %d, want 401", code)
	}
	if code := env.mcp(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 on MCP Bearer after revoke = %d, want 401", code)
	}

	rvCleanupAsOf(t, rvTokenExpiry(t, a2).Add(-time.Minute))
	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after cleanup run just before its expiry = %d, want 401", code)
	}
}

// V2 + V3：用续期得来的 A2 退出登录，同一会话的 A1（另一个标签页）与 R1 必须一起失效；
// 清理任务在 A2 到期前跑过之后，A2 与 R1 也不能复活（旧代码只给 A2 写一条 24 小时的黑名单）。
// 末尾：会话行已经不在的令牌退出时，拉黑会话号那一行也要盖住令牌自己的到期，不能再写死 24 小时。
func TestRVLogoutWithRefreshedAccessEndsWholeSession(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_logout", "operator", rvPassword)
	a1, r1 := env.login(user.Username, false)
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodPost, "/api/v1/auth/logout", a2, ""); rec.Code != http.StatusOK {
		t.Fatalf("logout with A2: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after logout = %d, want 401", code)
	}
	if code := env.status(a1); code != http.StatusUnauthorized {
		t.Errorf("A1 (other tab) after logout = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after logout = %d, want 401", code)
	}

	rvCleanupAsOf(t, rvTokenExpiry(t, a2).Add(-time.Minute))
	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after cleanup = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after cleanup = %d, want 401", code)
	}

	// 会话行已经不在（这里直接删行模拟），令牌本身还有效：退出走「直接拉黑会话号」那条兜底
	other := testutil.MustCreateLoginUser(t, "rv_logout_orphan", "operator", rvPassword)
	orphanA1, _ := env.login(other.Username, false)
	if err := database.DB.Where("user_id = ?", other.ID).Delete(&model.UserSession{}).Error; err != nil {
		t.Fatalf("delete session row: %v", err)
	}
	if rec := env.do(http.MethodPost, "/api/v1/auth/logout", orphanA1, ""); rec.Code != http.StatusOK {
		t.Fatalf("logout without a session row: got %d body=%s", rec.Code, rec.Body.String())
	}
	rvCleanupAsOf(t, rvTokenExpiry(t, orphanA1).Add(-time.Minute))
	if code := env.status(orphanA1); code != http.StatusUnauthorized {
		t.Errorf("token logged out without a session row, after cleanup = %d, want 401", code)
	}
}

// V4（降权）：把一个管理员降为 viewer 后，他续期出的 A2（令牌里的 role 还是 admin）不能再调管理员接口。
// 改角色等于要求重新登录（撤销全部会话），维持现有语义。
func TestRVRoleChangeRevokesRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")
	target := testutil.MustCreateLoginUser(t, "rv_demoted", "admin", rvPassword)
	_, r1 := env.login(target.Username, false)
	a2 := env.mustRefresh(r1)

	if code := env.do(http.MethodGet, "/api/v1/users", a2, "").Code; code != http.StatusOK {
		t.Fatalf("A2 on admin route before demote = %d, want 200", code)
	}
	if rec := env.do(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", target.ID), actorToken, `{"role":"viewer"}`); rec.Code != http.StatusOK {
		t.Fatalf("demote: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.do(http.MethodGet, "/api/v1/users", a2, "").Code; code != http.StatusUnauthorized {
		t.Errorf("A2 on admin route after demote = %d, want 401", code)
	}
}

// V4（禁用）：禁用用户后，他续期出的 A2 必须失效（旧代码 JWTAuth 不查 enabled，只拉黑登录那一枚）。
func TestRVDisableRevokesRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")
	target := testutil.MustCreateLoginUser(t, "rv_disabled", "operator", rvPassword)
	_, r1 := env.login(target.Username, false)
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", target.ID), actorToken, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after disable = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after disable = %d, want 401", code)
	}
}

// P4：删除用户后，他的 A1、A2 都必须失效（旧代码删号时什么都不撤销，被删的人还能一直调业务接口）。
func TestRVDeleteUserRevokesAllTokens(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")
	target := testutil.MustCreateLoginUser(t, "rv_deleted", "operator", rvPassword)
	a1, r1 := env.login(target.Username, false)
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", target.ID), actorToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete user: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a1); code != http.StatusUnauthorized {
		t.Errorf("A1 after delete = %d, want 401", code)
	}
	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after delete = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after delete = %d, want 401", code)
	}
}

// V5：改自己的密码后，另一台设备续期出的 A2 也必须失效（旧代码只拉黑各会话登录时那一枚）。
// 当前会话一起撤销，维持现有语义（网页改密后自己会退出）。
func TestRVOwnPasswordChangeEndsOtherDevices(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_own_password", "operator", rvPassword)
	webA1, _ := env.login(user.Username, false)
	_, appR1 := env.login(user.Username, true)
	appA2 := env.mustRefresh(appR1)

	body := fmt.Sprintf(`{"old_password":%q,"new_password":%q}`, rvPassword, "NewPassword456!")
	if rec := env.do(http.MethodPut, "/api/v1/auth/password", webA1, body); rec.Code != http.StatusOK {
		t.Fatalf("change password: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(appA2); code != http.StatusUnauthorized {
		t.Errorf("other device A2 after password change = %d, want 401", code)
	}
	if code, _ := env.refresh(appR1); code != http.StatusUnauthorized {
		t.Errorf("other device R1 after password change = %d, want 401", code)
	}
	if code := env.status(webA1); code != http.StatusUnauthorized {
		t.Errorf("own A1 after password change = %d, want 401", code)
	}
}

// 管理员重置他人密码后，对方续期出的 A2 必须失效。
func TestRVAdminResetPasswordRevokesRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")
	target := testutil.MustCreateLoginUser(t, "rv_reset_target", "operator", rvPassword)
	_, r1 := env.login(target.Username, false)
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodPut, fmt.Sprintf("/api/v1/users/%d/reset-password", target.ID), actorToken, `{"password":"NewPassword456!"}`); rec.Code != http.StatusOK {
		t.Fatalf("reset password: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after admin reset = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after admin reset = %d, want 401", code)
	}
}

// 改用户名后，续期出的 A2（令牌里还是旧用户名）必须失效。
func TestRVChangeUsernameRevokesRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_rename_me", "operator", rvPassword)
	a1, r1 := env.login(user.Username, false)
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodPut, "/api/v1/auth/username", a1, `{"username":"rv_renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("change username: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after username change = %d, want 401", code)
	}
	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("R1 after username change = %d, want 401", code)
	}
}

// max_web_sessions 顶替：同一用户在第二台设备登录网页端，被顶掉那台续期出的 A2 也必须失效。
//
// 末尾守住评审必做 5：会话行落不下去时登录必须回 500、不发 token。否则签出的令牌带着会话号却没有会话行，
// 撤销全部会话、改密、禁用、删除都遍历不到它，永远撤销不掉。
func TestRVDisplacedDeviceLosesRefreshedAccess(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_displaced", "operator", rvPassword)
	_, firstR1 := env.login(user.Username, false)
	firstA2 := env.mustRefresh(firstR1)

	// 默认 max_web_sessions = 1：第二台网页端登录会把第一台顶下线
	secondA1, _ := env.login(user.Username, false)

	if code := env.status(firstA2); code != http.StatusUnauthorized {
		t.Errorf("displaced device A2 = %d, want 401", code)
	}
	if code, _ := env.refresh(firstR1); code != http.StatusUnauthorized {
		t.Errorf("displaced device R1 = %d, want 401", code)
	}
	if code := env.status(secondA1); code != http.StatusOK {
		t.Errorf("new device A1 = %d, want 200", code)
	}

	if err := database.DB.Migrator().DropTable(&model.UserSession{}); err != nil {
		t.Fatalf("drop user_sessions to make the session insert fail: %v", err)
	}
	rec := env.loginRequest(user.Username, false)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "access_token") {
		t.Errorf("login whose session row cannot be stored = %d body=%s, want 500 without tokens", rec.Code, rec.Body.String())
	}
}

// P5 / B6：用续期得来的 A2 点「撤销其他会话」，只能撤别的会话，不能把自己的会话一起撤掉。
// 会话列表按会话号标出 current；登录超过 access 有效期、还能续期的会话也要列出来（P9）。
func TestRVRevokeOthersKeepsCallersOwnSession(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_revoke_others", "admin", rvPassword)
	_, webR1 := env.login(user.Username, false)
	webA2 := env.mustRefresh(webR1)
	_, appR1 := env.login(user.Username, true)
	appA2 := env.mustRefresh(appR1)

	// 另一个用户：21 天前登录、之后没续过期——登录那枚 access 已过期，refresh 还有效
	idle := testutil.MustCreateLoginUser(t, "rv_idle_listed", "operator", rvPassword)
	env.login(idle.Username, false)
	idleSession := env.sessionOf(idle.ID)
	if err := database.DB.Model(&idleSession).Update("expires_at", time.Now().Add(-24*time.Hour)).Error; err != nil {
		t.Fatalf("age idle session: %v", err)
	}

	rec := env.do(http.MethodGet, "/api/v1/security/sessions", webA2, "")
	var listed struct {
		Data []struct {
			UserID     uint   `json:"user_id"`
			ClientType string `json:"client_type"`
			Current    bool   `json:"current"`
		} `json:"data"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &listed) != nil {
		t.Fatalf("list sessions: got %d body=%s", rec.Code, rec.Body.String())
	}
	current := map[string]bool{}
	for _, item := range listed.Data {
		current[fmt.Sprintf("%d/%s", item.UserID, item.ClientType)] = item.Current
	}
	if isCurrent, ok := current[fmt.Sprintf("%d/web", user.ID)]; !ok || !isCurrent {
		t.Errorf("caller's own web session should be listed with current=true, got %v", current)
	}
	if isCurrent, ok := current[fmt.Sprintf("%d/app", user.ID)]; !ok || isCurrent {
		t.Errorf("caller's app session should be listed with current=false, got %v", current)
	}
	if isCurrent, ok := current[fmt.Sprintf("%d/web", idle.ID)]; !ok || isCurrent {
		t.Errorf("a session whose refresh is still valid should be listed, got %v", current)
	}

	if rec := env.do(http.MethodDelete, "/api/v1/security/sessions/others", webA2, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke others: got %d body=%s", rec.Code, rec.Body.String())
	}

	if code, _ := env.refresh(webR1); code != http.StatusOK {
		t.Errorf("caller R1 = %d (own session was revoked), want 200", code)
	}
	if code := env.status(webA2); code != http.StatusOK {
		t.Errorf("caller A2 = %d, want 200", code)
	}
	if code := env.status(appA2); code != http.StatusUnauthorized {
		t.Errorf("other session A2 = %d, want 401", code)
	}
	if code, _ := env.refresh(appR1); code != http.StatusUnauthorized {
		t.Errorf("other session R1 = %d, want 401", code)
	}
}

// 守护：续期成功 ⇒ 新 access 立即可用；同一会话的多枚 access 同时有效（多标签页、SSE 与普通请求各自续期）。
func TestRVGuardRefreshedAccessTokensCoexist(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_coexist", "operator", rvPassword)
	a1, r1 := env.login(user.Username, false)
	a2 := env.mustRefresh(r1)
	a3 := env.mustRefresh(r1)
	if a2 == a3 {
		t.Fatal("expected every refresh to issue a distinct access token")
	}

	for name, token := range map[string]string{"A1": a1, "A2": a2, "A3": a3} {
		if code := env.status(token); code != http.StatusOK {
			t.Errorf("%s = %d, want 200", name, code)
		}
	}
}

// 守护（回退安全）：带 sid 的令牌在不认识 sid 的旧版本上照样可用。
// 这份用例放到改动前的代码上跑，验证的就是旧版本对这个未知字段的处理。
func TestRVGuardTokenWithUnknownSIDClaimIsAccepted(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateUser(t, "rv_sid_token", "operator")
	now := time.Now()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username":   user.Username,
		"role":       user.Role,
		"token_type": "access",
		"sid":        uuid.NewString(),
		"exp":        now.Add(time.Hour).Unix(),
		"iat":        now.Unix(),
		"jti":        uuid.NewString(),
	}).SignedString([]byte(config.C.JWT.Secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if code := env.status(token); code != http.StatusOK {
		t.Errorf("token carrying an sid claim = %d, want 200", code)
	}
}

// 守护（评审必做 1）：应用令牌与脚本令牌没有用户行、也没有会话行，行为必须与改动前一致——只按 jti 拉黑，
// 被拉黑时仍回「令牌已被撤销」。它们要是落进用户会话的判定，所有任务通知、开放 API、MCP、企业微信触发都会 401。
func TestRVGuardNonUserTokensUnaffected(t *testing.T) {
	env := newRVEnv(t)

	// POST /open-api/token 签的应用令牌：username = app:<key>，没有 jti
	appToken := env.appToken("rv-app", "tasks")
	if jti := rvClaim(t, appToken, "jti"); jti != "" {
		t.Fatalf("expected the open api token to carry no jti, got %q", jti)
	}
	if code := env.do(http.MethodGet, "/api/v1/tasks", appToken, "").Code; code != http.StatusOK {
		t.Errorf("open api app token = %d, want 200", code)
	}

	// MCP / 企业微信在进程内现铸的临时应用令牌：有 jti
	tempAppToken, err := middleware.GenerateTemporaryAccessToken("app:rv-app", "app:tasks", time.Minute)
	if err != nil {
		t.Fatalf("mint temporary app token: %v", err)
	}
	if code := env.do(http.MethodGet, "/api/v1/tasks", tempAppToken, "").Code; code != http.StatusOK {
		t.Errorf("temporary app token = %d, want 200", code)
	}

	// 注入脚本环境的令牌（任务通知、DAIDAI_TOKEN、ddp 交互会话）；用户名写字面量，改动前还没有对应常量
	script, err := middleware.GenerateTemporaryAccessTokenInfo("internal-script-notify", "operator", time.Hour)
	if err != nil {
		t.Fatalf("mint script token: %v", err)
	}
	if code := env.do(http.MethodGet, "/api/v1/tasks", script.Token, "").Code; code != http.StatusOK {
		t.Errorf("script token = %d, want 200", code)
	}

	service.RevokeScriptToken(&service.ScriptTokenInfo{JTI: script.JTI, ExpiresAt: script.ExpiresAt})
	rec := env.do(http.MethodGet, "/api/v1/tasks", script.Token, "")
	message, code := rvBodyError(rec)
	if rec.Code != http.StatusUnauthorized || message != "令牌已被撤销" || code != "" {
		t.Errorf("revoked script token = %d body=%s, want 401 {\"error\":\"令牌已被撤销\"} without code", rec.Code, rec.Body.String())
	}
}

// 守护（#153 承重项）：每个带鉴权的请求只查一次库。数据库是单连接，JWTAuth 里多一条查询就是全站多排一次队。
// 续期不在每请求路径上，只记录条数（改动前 2 条，现在 4 条）。
func TestRVGuardAuthCostIsOneStatementPerRequest(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_cost", "operator", rvPassword)
	a1, r1 := env.login(user.Username, false)
	a2 := env.mustRefresh(r1)
	appToken := env.appToken("rv-cost-app", "tasks")
	script, err := middleware.GenerateTemporaryAccessTokenInfo("internal-script-notify", "operator", time.Hour)
	if err != nil {
		t.Fatalf("mint script token: %v", err)
	}

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"script", script.Token},
		{"login A1", a1},
		{"refreshed A2", a2},
		{"app", appToken},
	} {
		var code int
		statements := rvCountStatements(func() { code = env.do(http.MethodGet, "/rv-probe", tc.token, "").Code })
		if code != http.StatusNoContent {
			t.Errorf("%s: probe = %d, want 204", tc.name, code)
		}
		if statements != 1 {
			t.Errorf("%s: %d statement(s) per authenticated request, want 1", tc.name, statements)
		}
	}

	var refreshCode int
	statements := rvCountStatements(func() { refreshCode, _ = env.refresh(r1) })
	t.Logf("refresh: %d statement(s) per /auth/refresh (status %d)", statements, refreshCode)
}

// 升级兼容：升级前登录签发的 A1（没有 sid）只要会话行还在、用户启用、用户名没改就照常可用，鉴权仍只查一次库。
// 旧版 APP（≤ v1.2.6）从不续期，手里只有这一枚。
func TestRVUpgradeLegacyLoginAccessKeepsWorking(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateUser(t, "rv_legacy_login", "operator")
	loginJTI := uuid.NewString()
	legacyA1 := rvSignLegacyAccess(t, user.Username, user.Role, loginJTI)
	rvSeedLegacySession(t, user, loginJTI)

	if code := env.status(legacyA1); code != http.StatusOK {
		t.Errorf("legacy login A1 = %d, want 200", code)
	}
	var code int
	statements := rvCountStatements(func() { code = env.do(http.MethodGet, "/rv-probe", legacyA1, "").Code })
	if code != http.StatusNoContent || statements != 1 {
		t.Errorf("legacy login A1 on probe = %d with %d statement(s), want 204 with 1", code, statements)
	}
}

// 升级兼容（新口径）：升级前续期得来的老 A2 回 401 + session_revoked；网页与新版 APP 随即用老 R1 续一次，
// 拿到带会话号的新 access，无感续上。
// 顺带守住评审必做 1 的「精确」：用户名只是以 internal-script-notify 开头的老形状令牌，不能蹭脚本令牌的豁免。
func TestRVUpgradeLegacyRefreshedAccessRecoversWithOneRefresh(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateUser(t, "rv_legacy_refreshed", "operator")
	loginJTI := uuid.NewString()
	legacyA1 := rvSignLegacyAccess(t, user.Username, user.Role, loginJTI)
	legacyA2 := rvSignLegacyAccess(t, user.Username, user.Role, uuid.NewString())
	legacyR1 := rvSeedLegacySession(t, user, loginJTI)

	rec := env.do(http.MethodGet, "/api/v1/system/version", legacyA2, "")
	if _, code := rvBodyError(rec); rec.Code != http.StatusUnauthorized || code != "session_revoked" {
		t.Errorf("legacy refreshed A2 = %d body=%s, want 401 with code session_revoked", rec.Code, rec.Body.String())
	}

	refreshCode, fresh := env.refresh(legacyR1)
	if refreshCode != http.StatusOK {
		t.Fatalf("legacy R1 refresh = %d, want 200", refreshCode)
	}
	if sid := rvClaim(t, fresh, "sid"); sid != loginJTI {
		t.Errorf("new access sid = %q, want the session id %q", sid, loginJTI)
	}
	if code := env.status(fresh); code != http.StatusOK {
		t.Errorf("new access from legacy R1 = %d, want 200", code)
	}
	if code := env.status(legacyA1); code != http.StatusOK {
		t.Errorf("legacy login A1 = %d, want 200", code)
	}

	lookalike := rvSignLegacyAccess(t, "internal-script-notify-lookalike", "operator", uuid.NewString())
	if code := env.status(lookalike); code != http.StatusUnauthorized {
		t.Errorf("legacy token named like the script token = %d, want 401", code)
	}
}

// 升级兼容：升级前「删号不撤销」留下的孤儿会话，老 A1 与 R1 升级后都要失效。
func TestRVUpgradeOrphanSessionOfDeletedUserIsRejected(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateUser(t, "rv_orphan", "operator")
	loginJTI := uuid.NewString()
	legacyA1 := rvSignLegacyAccess(t, user.Username, user.Role, loginJTI)
	legacyR1 := rvSeedLegacySession(t, user, loginJTI)
	// 模拟旧版本的删号：只删 users 行，不撤销任何会话
	if err := database.DB.Delete(user).Error; err != nil {
		t.Fatalf("delete user row: %v", err)
	}

	if code := env.status(legacyA1); code != http.StatusUnauthorized {
		t.Errorf("orphan legacy A1 = %d, want 401", code)
	}
	if code, _ := env.refresh(legacyR1); code != http.StatusUnauthorized {
		t.Errorf("orphan legacy R1 = %d, want 401", code)
	}
}

// V7：删号后再建一个同名账号，旧设备手里的 R1 不能续到新账号上。
// 用直接删 users 行模拟旧版本的删号（不撤销会话），单独守住续期时「会话属于这个用户」这道校验。
func TestRVRecreatedUsernameCannotReuseOldRefresh(t *testing.T) {
	env := newRVEnv(t)
	old := testutil.MustCreateLoginUser(t, "rv_recreated", "admin", rvPassword)
	_, r1 := env.login(old.Username, false)
	if err := database.DB.Delete(old).Error; err != nil {
		t.Fatalf("delete user row: %v", err)
	}
	testutil.MustCreateLoginUser(t, "rv_recreated", "viewer", "OtherPassword789!")

	if code, _ := env.refresh(r1); code != http.StatusUnauthorized {
		t.Errorf("old R1 against recreated username = %d, want 401", code)
	}
}

// V3：撤销写下的会话号那行黑名单，到期必须 ≥ 这个会话签出的最晚一枚 access，否则清理任务一跑，那枚 access 就复活了。
//
// 后半段守住评审必做 2：refresh 已过期、但它之前续期出的 access 还有效时，会话清理不能删掉会话行，
// 否则之后的禁用 / 改密遍历不到它，那枚 access 漏拉黑。
func TestRVRevocationRowOutlivesEveryIssuedAccess(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")

	// 前半段：10 天前登录、今天续期。登录那枚 A1 比今天续期出的 A2 早 10 天到期。
	user := testutil.MustCreateLoginUser(t, "rv_row_expiry", "operator", rvPassword)
	_, r1 := env.login(user.Username, false)
	session := env.sessionOf(user.ID)
	if err := database.DB.Model(&session).Update("expires_at", session.ExpiresAt.Add(-10*24*time.Hour)).Error; err != nil {
		t.Fatalf("age session: %v", err)
	}
	a2 := env.mustRefresh(r1)

	if rec := env.do(http.MethodDelete, fmt.Sprintf("/api/v1/security/sessions/%d", session.ID), actorToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("revoke session: got %d body=%s", rec.Code, rec.Body.String())
	}
	var row model.TokenBlocklist
	if err := database.DB.Where("jti = ?", session.JTI).First(&row).Error; err != nil {
		t.Fatalf("expected a blocklist row for the session id: %v", err)
	}
	if latest := rvTokenExpiry(t, a2); row.ExpiresAt.Before(latest) {
		t.Errorf("session-id row expires at %s, before the latest access issued by the session (%s)",
			row.ExpiresAt.Format(time.RFC3339), latest.Format(time.RFC3339))
	}

	// 后半段：只改会话行模拟时间流逝——约 50 天前登录（登录那枚 access 早已过期），那时续期出 idleA2；
	// 又过了 10 多天，refresh 也到期了，而 idleA2 还在有效期内。
	idle := testutil.MustCreateLoginUser(t, "rv_refresh_expired", "operator", rvPassword)
	_, idleR1 := env.login(idle.Username, false)
	idleSession := env.sessionOf(idle.ID)
	if err := database.DB.Model(&idleSession).Update("expires_at", time.Now().Add(-24*time.Hour)).Error; err != nil {
		t.Fatalf("expire login access of idle session: %v", err)
	}
	idleA2 := env.mustRefresh(idleR1)
	if err := database.DB.Model(&idleSession).Update("refresh_expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("expire refresh of idle session: %v", err)
	}
	// 打开会话管理页会先跑一次会话清理
	if rec := env.do(http.MethodGet, "/api/v1/security/sessions", actorToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("list sessions: got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec := env.do(http.MethodPut, fmt.Sprintf("/api/v1/users/%d", idle.ID), actorToken, `{"enabled":false}`); rec.Code != http.StatusOK {
		t.Fatalf("disable: got %d body=%s", rec.Code, rec.Body.String())
	}
	if code := env.status(idleA2); code != http.StatusUnauthorized {
		t.Errorf("refreshed access after session cleanup + disable = %d, want 401", code)
	}
}

// rvOnceAfterUserRead 注册一个一次性的查询回调：某次 First 读到目标用户之后立刻触发 action。
// 用来确定性地把「管理员的并发改动」塞进「登录读用户」与「登录写 last_login_at / 落会话行」之间那个窗口，
// 不靠并发抢时序。回调只触发一次，action 里再访问 database.DB 不会重入（fired 已置位）；
// After("gorm:query") 时那条查询的连接已经归还连接池，单连接下嵌套读写不会自锁。
func rvOnceAfterUserRead(t *testing.T, username string, action func()) {
	t.Helper()
	const name = "rv:after_user_read"
	fired := false
	err := database.DB.Callback().Query().After("gorm:query").Register(name, func(db *gorm.DB) {
		if fired {
			return
		}
		user, ok := db.Statement.Dest.(*model.User)
		if !ok || user.Username != username {
			return
		}
		fired = true
		action()
	})
	if err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Callback().Query().Remove(name) })
}

// F1：登录只更新 last_login_at 这一列，不能把开头读到的整行写回。
// 用查询回调在「Login 读完用户」之后塞进管理员的禁用 + 降权 + 重置密码，登录结束后库里必须仍是管理员改过的值。
// 旧代码的 database.DB.Save(&user) 会拿登录开始时那份整行覆盖回去，把这三项一起改回原样。
// 删号恰好落在这个窗口时更糟：Save 更新到 0 行会退化成插入，把刚删掉的账号又建回来。
func TestRVLoginUpdatesOnlyLastLoginColumn(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_login_writeback", "operator", rvPassword)
	const resetHash = "admin-reset-hash-value"
	rvOnceAfterUserRead(t, user.Username, func() {
		// 禁用、降权、重置密码一次覆盖到：登录写回若发生，这三项都会被打回原样
		if err := database.DB.Model(&model.User{}).Where("id = ?", user.ID).
			Updates(map[string]interface{}{"enabled": false, "role": "viewer", "password": resetHash}).Error; err != nil {
			t.Errorf("apply concurrent admin change: %v", err)
		}
	})

	// 登录本身成不成功不重要（禁用后会被拒），这里只看它有没有踩掉管理员的改动
	env.loginRequest(user.Username, false)

	var stored model.User
	if err := database.DB.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.Enabled {
		t.Error("login wrote the whole row back and re-enabled a concurrently disabled account")
	}
	if stored.Role != "viewer" {
		t.Errorf("role = %q, want viewer: a concurrent demotion must survive the login write", stored.Role)
	}
	if stored.Password != resetHash {
		t.Error("password hash was overwritten by the login write-back, want the admin reset value")
	}
}

// F2：登录校验完、会话行还没落库时，并发的禁用执行了 RevokeAllUserSessions；那次撤销遍历不到还没建的会话行。
// 登录落会话后必须重读用户、发现被禁用就撤掉刚建的会话并拒登，否则这枚会话的令牌在账号已禁用时还能一直用到过期。
func TestRVLoginRevokesSessionWhenUserDisabledMidLogin(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_login_race_disable", "operator", rvPassword)
	rvOnceAfterUserRead(t, user.Username, func() {
		if err := database.DB.Model(&model.User{}).Where("id = ?", user.ID).Update("enabled", false).Error; err != nil {
			t.Errorf("disable mid-login: %v", err)
		}
		// 此刻会话行还没建，这次撤销遍历不到它——正是要靠登录落会话后的重读来兜住
		service.RevokeAllUserSessions(user.ID)
	})

	rec := env.loginRequest(user.Username, false)
	if rec.Code != http.StatusForbidden {
		t.Errorf("login of an account disabled mid-login = %d, want 403", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "access_token") {
		t.Errorf("login must not hand out a token when the account was disabled mid-login: %s", rec.Body.String())
	}
	// 没有留下能用的 token：中途建出的会话行必须被撤掉（RevokeSession 会拉黑会话号并删行）
	var remaining int64
	database.DB.Model(&model.UserSession{}).Where("user_id = ?", user.ID).Count(&remaining)
	if remaining != 0 {
		t.Errorf("the session created mid-login must be revoked, %d row(s) left", remaining)
	}
}

// F4：撤销写下的会话号那行黑名单，到期必须盖住这个会话续期出的 access——上界取「refresh 到期 + access 有效期」，
// 不能只用会话行的 expires_at。构造：老会话（登录那枚 access 早已临近过期）→ 取推后前的快照 → 续期把 expires_at 推后
// → 用旧快照拉黑并删会话行 → 在 A2 到期前 1 分钟跑清理。上界正确时黑名单行还在、A2 仍 401；
// 退化成只用 session.ExpiresAt 时这行会被提前删掉，A2 复活（用 go test -overlay 改掉上界即可看到这条变红）。
func TestRVRevocationRowCoversRefreshedAccessAfterCleanup(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_row_bound", "operator", rvPassword)
	_, r1 := env.login(user.Username, false)

	// 把登录那枚 access 的到期挪到 10 天前的位置：这样续期出的 A2 到期明显晚于「会话行 expires_at」，
	// 两种上界算法才拉得开差距，测试才有区分度
	session := env.sessionOf(user.ID)
	if err := database.DB.Model(&session).Update("expires_at", session.ExpiresAt.Add(-10*24*time.Hour)).Error; err != nil {
		t.Fatalf("age session: %v", err)
	}
	// 快照取「推后前」的会话行：撤销侧拿到的常常就是续期前的那一份
	var snapshot model.UserSession
	if err := database.DB.First(&snapshot, session.ID).Error; err != nil {
		t.Fatalf("snapshot session: %v", err)
	}

	a2 := env.mustRefresh(r1) // 把 DB 里的 expires_at 推到 A2 的到期

	// 用旧快照拉黑并删掉会话行：会话号那行黑名单的到期由结构上界（含 refresh 到期 + access 有效期）决定
	service.BlockSessionTokens(&snapshot)
	if err := database.DB.Delete(&model.UserSession{}, snapshot.ID).Error; err != nil {
		t.Fatalf("delete session row: %v", err)
	}

	rvCleanupAsOf(t, rvTokenExpiry(t, a2).Add(-time.Minute))
	if code := env.status(a2); code != http.StatusUnauthorized {
		t.Errorf("A2 after cleanup = %d, want 401: the session-id blocklist row must outlive the refreshed access", code)
	}
}

// F3：撤销时先 Find 再逐个拉黑、最后整批删。若「Find 之后、删除之前」并发落下一行新会话，
// 整批按 user_id 删会把它一起删掉，但它没进过黑名单，之后任何撤销都遍历不到它——那枚令牌就再也撤不掉了。
// 只删「这次真的拉黑过」的那几行才不误伤。用查询回调在 RevokeAllUserSessions 的 Find 之后插一行新会话，
// 断言撤销结束后这行还在（旧代码按 user_id 整批删会把它删掉）。
func TestRVRevokeAllKeepsSessionInsertedDuringRevoke(t *testing.T) {
	env := newRVEnv(t)
	user := testutil.MustCreateLoginUser(t, "rv_revoke_window", "operator", rvPassword)
	env.login(user.Username, false) // 先有一行已存在的会话，撤销的 Find 才非空

	// 只在 RevokeAllUserSessions 的那次 []UserSession Find 之后触发一次：模拟一个并发登录恰好在此刻落库。
	// 注册必须在 login 之后（login 内部的 revokeExcessSessionsByClientType 也会 Find []UserSession）。
	const cbName = "rv:after_sessions_find"
	fired := false
	var insertedID uint
	err := database.DB.Callback().Query().After("gorm:query").Register(cbName, func(db *gorm.DB) {
		if fired {
			return
		}
		if _, ok := db.Statement.Dest.(*[]model.UserSession); !ok {
			return
		}
		fired = true
		future := time.Now().Add(config.C.JWT.AccessTokenExpire)
		refreshFuture := time.Now().Add(config.C.JWT.RefreshTokenExpire)
		newSess := model.UserSession{
			UserID:           user.ID,
			Username:         user.Username,
			JTI:              uuid.NewString(),
			RefreshJTI:       uuid.NewString(),
			ClientType:       "web",
			ExpiresAt:        future,
			RefreshExpiresAt: &refreshFuture,
		}
		if createErr := database.DB.Create(&newSess).Error; createErr != nil {
			t.Errorf("insert session during revoke window: %v", createErr)
			return
		}
		insertedID = newSess.ID
	})
	if err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Callback().Query().Remove(cbName) })

	service.RevokeAllUserSessions(user.ID)

	if insertedID == 0 {
		t.Fatal("callback did not run: no session was inserted during the revoke window")
	}
	var cnt int64
	database.DB.Model(&model.UserSession{}).Where("id = ?", insertedID).Count(&cnt)
	if cnt != 1 {
		t.Errorf("session inserted during the Find→delete window was collaterally deleted; it must be kept so a later revoke can still reach it")
	}
}

// F5：升级前签发、不带 sid 的老登录令牌，只放行「会话行还在、用户启用、用户名没改」的那一枚——
// 老 token 分支那条查询里的 u.username = ? 与 u.enabled = ? 两个条件都要在。
// 用户名被改：令牌里的用户名与库里对不上 → 401；用户被禁用：enabled=false → 401。
// 删掉任一条件，对应那半就会放行到 200（用 go test -overlay 删掉可看到变红）。
func TestRVUpgradeLegacyAccessRejectedAfterUsernameChangeOrDisable(t *testing.T) {
	env := newRVEnv(t)

	// 改用户名：老 A1 的用户名不再匹配 users 行
	renamed := testutil.MustCreateUser(t, "rv_legacy_rename", "operator")
	renameJTI := uuid.NewString()
	renamedA1 := rvSignLegacyAccess(t, renamed.Username, renamed.Role, renameJTI)
	rvSeedLegacySession(t, renamed, renameJTI)
	if err := database.DB.Model(&renamed).Update("username", "rv_legacy_renamed").Error; err != nil {
		t.Fatalf("rename user: %v", err)
	}
	if code := env.status(renamedA1); code != http.StatusUnauthorized {
		t.Errorf("legacy A1 after username change = %d, want 401", code)
	}

	// 禁用：老 A1 对应的用户 enabled=false
	disabled := testutil.MustCreateUser(t, "rv_legacy_disable", "operator")
	disableJTI := uuid.NewString()
	disabledA1 := rvSignLegacyAccess(t, disabled.Username, disabled.Role, disableJTI)
	rvSeedLegacySession(t, disabled, disableJTI)
	if err := database.DB.Model(&disabled).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if code := env.status(disabledA1); code != http.StatusUnauthorized {
		t.Errorf("legacy A1 after disable = %d, want 401", code)
	}
}

// 删号顺序：UserHandler.Delete 必须先删用户行、再 RevokeAllUserSessions，F2 的重读才兜得住并发登录。
// 用查询回调在 DELETE /users/:id 第一次 Find []UserSession（撤销那一步的 Find）时，塞进一次该用户的完整登录。
// 先撤销后删行时：这次登录的会话行落在撤销的 Find 之后，重读又早于删行、看不到变化，token 照发；
// 用户行随后被删，这行孤儿会话再没有哪条改用户状态的路径会撤它，被删的管理员拿着这枚 access（role 仍是 admin）能一直调管理员接口。
// 先删行后撤销时：登录读不到用户，直接拒登。用 go test -overlay 把顺序改回「先撤销后删行」即可看到这条变红。
func TestRVLoginRacingUserDeleteLeavesNoUsableToken(t *testing.T) {
	env := newRVEnv(t)
	actorToken := env.adminToken("rv-actor")
	target := testutil.MustCreateLoginUser(t, "rv_delete_race", "admin", rvPassword)

	// 只在删号请求里的第一次 []UserSession Find 触发一次；登录内部的 revokeExcessSessionsByClientType
	// 也会 Find []UserSession，fired 先置位，嵌套的那次不会重入
	const cbName = "rv:login_during_delete"
	fired := false
	var raced *httptest.ResponseRecorder
	err := database.DB.Callback().Query().After("gorm:query").Register(cbName, func(db *gorm.DB) {
		if fired {
			return
		}
		if _, ok := db.Statement.Dest.(*[]model.UserSession); !ok {
			return
		}
		fired = true
		raced = env.loginRequest(target.Username, false)
	})
	if err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() { _ = database.DB.Callback().Query().Remove(cbName) })

	if rec := env.do(http.MethodDelete, fmt.Sprintf("/api/v1/users/%d", target.ID), actorToken, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete user: got %d body=%s", rec.Code, rec.Body.String())
	}
	if raced == nil {
		t.Fatal("callback did not run: no login was attempted during the delete")
	}

	// 这次登录要么没拿到 token，要么拿到的 token 已经用不了
	var payload struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(raced.Body.Bytes(), &payload)
	if payload.AccessToken != "" {
		if code := env.status(payload.AccessToken); code != http.StatusUnauthorized {
			t.Errorf("access issued to a login racing the delete = %d, want 401", code)
		}
		if code := env.do(http.MethodGet, "/api/v1/users", payload.AccessToken, "").Code; code != http.StatusUnauthorized {
			t.Errorf("access issued to a login racing the delete, on an admin route = %d, want 401", code)
		}
	}
	// 被删用户不能留下会话行：用户已经不在，改用户状态的路径再也撤不到它，留下的就是孤儿会话
	var remaining int64
	database.DB.Model(&model.UserSession{}).Where("user_id = ?", target.ID).Count(&remaining)
	if remaining != 0 {
		t.Errorf("deleted user still has %d orphan session row(s)", remaining)
	}
}

// rvReadSSE 在后台逐行读一条 SSE 响应，按顺序推进 channel；服务端收流（EOF）时关闭 channel。
func rvReadSSE(body io.Reader) <-chan string {
	lines := make(chan string, 4096)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return lines
}

// rvNextMatch 一直读到第一条满足 match 的行。found：找到了；ended：流在找到之前就结束了；两者都是 false 就是超时。
func rvNextMatch(lines <-chan string, timeout time.Duration, match func(string) bool) (found bool, ended bool) {
	deadline := time.After(timeout)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return false, true
			}
			if match(line) {
				return true, false
			}
		case <-deadline:
			return false, false
		}
	}
}

// 日志流复查：执行日志流只在连上时过一次 JWTAuth，却能一直推到任务结束。登录会话被撤销后，
// 流要在一个心跳周期内先发 done:reconnect 再收流（不发 done 直接断开，网页的日志弹窗会卡在「运行中」）。
// 两条流一静一动：一直有输出的任务等不到「静默心跳」，复查也得按周期照跑。
func TestRVLogStreamEndsWithReconnectAfterRevoke(t *testing.T) {
	env := newRVEnv(t)
	previousInterval := logStreamHeartbeatInterval
	logStreamHeartbeatInterval = 100 * time.Millisecond
	t.Cleanup(func() { logStreamHeartbeatInterval = previousInterval })

	user := testutil.MustCreateLoginUser(t, "rv_log_stream", "viewer", rvPassword)
	_, r1 := env.login(user.Username, false)
	a2 := env.mustRefresh(r1)

	// TinyLogManager 是进程级单例，用完必须摘掉，免得留给同包的其它用例
	manager := service.GetTinyLogManager()
	quiet, err := manager.Create("987601_rv-quiet")
	if err != nil {
		t.Fatalf("create quiet tiny log: %v", err)
	}
	chatty, err := manager.Create("987602_rv-chatty")
	if err != nil {
		t.Fatalf("create chatty tiny log: %v", err)
	}
	t.Cleanup(func() {
		quiet.Close()
		manager.Remove("987601_rv-quiet")
		chatty.Close()
		manager.Remove("987602_rv-chatty")
	})
	stopWriting := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopWriting:
				return
			case <-ticker.C:
				chatty.Write([]byte("tick\n"))
			}
		}
	}()
	t.Cleanup(func() { close(stopWriting) })

	server := httptest.NewServer(env.engine)
	t.Cleanup(server.Close)
	open := func(taskID int) <-chan string {
		req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/logs/%d/stream", server.URL, taskID), nil)
		if err != nil {
			t.Fatalf("build stream request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+a2)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("open stream: %v", err)
		}
		// Cleanup 后进先出：先于 server.Close 断开客户端，服务端的流才会因 ctx.Done 退出，Close 不会卡住
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("open stream: got %d", resp.StatusCode)
		}
		return rvReadSSE(resp.Body)
	}
	streams := []struct {
		name  string
		lines <-chan string
	}{
		{"quiet", open(987601)},
		{"chatty", open(987602)},
	}
	isDone := func(line string) bool { return line == "event: done" }

	for _, s := range streams {
		if found, _ := rvNextMatch(s.lines, 5*time.Second, func(line string) bool { return line == ": open" }); !found {
			t.Fatalf("%s stream: expected the open comment", s.name)
		}
	}
	if found, _ := rvNextMatch(streams[1].lines, 5*time.Second, func(line string) bool { return line == "data: tick" }); !found {
		t.Fatal("chatty stream: expected live output before revoke")
	}
	// 撤销之前复查不能误伤：等满 3 个心跳周期，两条流都必须还开着
	for _, s := range streams {
		if found, ended := rvNextMatch(s.lines, 300*time.Millisecond, isDone); found || ended {
			t.Fatalf("%s stream ended before the session was revoked", s.name)
		}
	}

	service.RevokeAllUserSessions(user.ID)

	for _, s := range streams {
		if found, _ := rvNextMatch(s.lines, 5*time.Second, isDone); !found {
			t.Errorf("%s stream: expected event: done within 5s after revoke", s.name)
			continue
		}
		select {
		case line := <-s.lines:
			if line != "data: reconnect" {
				t.Errorf("%s stream: expected data: reconnect right after done, got %q", s.name, line)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("%s stream: expected data: reconnect right after done", s.name)
		}
		if _, ended := rvNextMatch(s.lines, 5*time.Second, func(string) bool { return false }); !ended {
			t.Errorf("%s stream: expected the server to close the stream after done:reconnect", s.name)
		}
	}
}
