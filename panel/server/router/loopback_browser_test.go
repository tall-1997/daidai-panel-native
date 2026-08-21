package router

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/middleware"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestLoopbackBrowserTicketCookieAndReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("panel"), 0o600); err != nil {
		t.Fatal(err)
	}
	access, err := NewLoopbackBrowserAccess("127.0.0.1:5700", webDir)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(managementSecurityMiddleware(ManagementSecurity{LocalToken: "local-token", Host: "127.0.0.1:5700", Browser: access}))
	access.Register(engine)

	browserURL, err := access.CreateURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(browserURL)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.TrimPrefix(parsed.Fragment, "ticket=")) < 43 || parsed.RawQuery != "" {
		t.Fatalf("ticket must remain in the fragment: %q", browserURL)
	}

	exchange := requestLocalBrowserExchange(engine, strings.TrimPrefix(parsed.Fragment, "ticket="), "127.0.0.1:5700", "")
	if exchange.Code != http.StatusFound {
		t.Fatalf("exchange status=%d body=%s", exchange.Code, exchange.Body.String())
	}
	cookies := exchange.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%d want=1", len(cookies))
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 900 {
		t.Fatalf("insecure cookie: %#v", cookie)
	}
	replay := requestLocalBrowserExchange(engine, strings.TrimPrefix(parsed.Fragment, "ticket="), "127.0.0.1:5700", "")
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d want=401", replay.Code)
	}

	page := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:5700/local-ui/", nil)
	page.Host = "127.0.0.1:5700"
	page.AddCookie(cookie)
	pageResult := httptest.NewRecorder()
	engine.ServeHTTP(pageResult, page)
	if pageResult.Code != http.StatusOK || pageResult.Body.String() != "panel" {
		t.Fatalf("page status=%d body=%q", pageResult.Code, pageResult.Body.String())
	}
	for _, header := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if pageResult.Header().Get(header) == "" {
			t.Fatalf("missing %s", header)
		}
	}
}

func TestLoopbackBrowserRejectsExpiredHostAndOrigin(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("panel"), 0o600); err != nil {
		t.Fatal(err)
	}
	access, err := NewLoopbackBrowserAccess("127.0.0.1:5700", webDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	access.now = func() time.Time { return now }
	engine := gin.New()
	engine.Use(managementSecurityMiddleware(ManagementSecurity{LocalToken: "local-token", Host: "127.0.0.1:5700", Browser: access}))
	access.Register(engine)
	browserURL, err := access.CreateURL()
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(browserURL)

	ticket := strings.TrimPrefix(parsed.Fragment, "ticket=")
	wrongHost := requestLocalBrowserExchange(engine, ticket, "localhost:5700", "")
	if wrongHost.Code != http.StatusBadRequest {
		t.Fatalf("host status=%d want=400", wrongHost.Code)
	}
	wrongOrigin := requestLocalBrowserExchange(engine, ticket, "127.0.0.1:5700", "http://evil.invalid")
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatalf("origin status=%d want=403", wrongOrigin.Code)
	}
	now = now.Add(31 * time.Second)
	expired := requestLocalBrowserExchange(engine, ticket, "127.0.0.1:5700", "")
	if expired.Code != http.StatusUnauthorized {
		t.Fatalf("expired status=%d want=401", expired.Code)
	}
}

func TestBrowserCookieReplacesOnlyLocalTokenAndKeepsJWT(t *testing.T) {
	testutil.SetupTestEnv(t)
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("panel"), 0o600); err != nil {
		t.Fatal(err)
	}
	access, err := NewLoopbackBrowserAccess("127.0.0.1:5700", webDir)
	if err != nil {
		t.Fatal(err)
	}
	engine := gin.New()
	engine.Use(managementSecurityMiddleware(ManagementSecurity{LocalToken: "local-token", Host: "127.0.0.1:5700", Browser: access}))
	engine.GET("/api/protected", middleware.JWTAuth(), middleware.RequireRole("admin"), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	access.Register(engine)
	browserURL, _ := access.CreateURL()
	parsed, _ := url.Parse(browserURL)
	exchange := requestLocalBrowserExchange(engine, strings.TrimPrefix(parsed.Fragment, "ticket="), "127.0.0.1:5700", "")
	cookie := exchange.Result().Cookies()[0]

	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:5700/api/protected", nil)
	request.Host = "127.0.0.1:5700"
	request.Header.Set("Origin", "http://127.0.0.1:5700")
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "授权令牌") {
		t.Fatalf("cookie bypassed JWT: status=%d body=%s", response.Code, response.Body.String())
	}
}

func requestLocalBrowser(engine http.Handler, path, host, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:5700"+path, nil)
	request.Host = host
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func requestLocalBrowserExchange(engine http.Handler, ticket, host, origin string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:5700/local-ui/session", bytes.NewBufferString(ticket))
	request.Host = host
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}
