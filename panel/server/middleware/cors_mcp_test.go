package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daidai-panel/config"

	"github.com/gin-gonic/gin"
)

// 浏览器里的 MCP 客户端（例如网页版 Inspector）每个请求都带 MCP-Protocol-Version，
// 有会话时还会带 Mcp-Session-Id。预检回的 Access-Control-Allow-Headers 里没有它们，
// 浏览器就把请求拦在预检这一步，放行名单里的来源也连不上。原生客户端和 curl 不走预检，不受影响。
func TestCORSPreflightAllowsMCPRequestHeaders(t *testing.T) {
	prev := config.C
	t.Cleanup(func() { config.C = prev })
	config.C = &config.Config{CORS: config.CORSConfig{Origins: []string{"https://inspector.example.com"}}}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CORS())
	engine.POST("/api/v1/mcp", func(c *gin.Context) { c.Status(http.StatusOK) })

	requested := []string{"authorization", "content-type", "mcp-protocol-version", "mcp-session-id"}
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/mcp", nil)
		req.Host = "panel.example.com"
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", strings.Join(requested, ","))
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	rec := preflight("https://inspector.example.com")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for allowed MCP preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://inspector.example.com" {
		t.Fatalf("expected allowed origin echoed back, got %q", got)
	}
	// 浏览器按不区分大小写比对请求头名。
	allowed := map[string]bool{}
	for _, name := range strings.Split(rec.Header().Get("Access-Control-Allow-Headers"), ",") {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	for _, name := range requested {
		if !allowed[name] {
			t.Fatalf("预检的 Access-Control-Allow-Headers 缺少 %q，实际 %q", name, rec.Header().Get("Access-Control-Allow-Headers"))
		}
	}

	// Origin 放行口径不变：多放行两个请求头不等于放开来源，跨站来源带着同样的请求头照旧 403。
	foreign := preflight("https://evil.example.com")
	if foreign.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for foreign origin MCP preflight, got %d", foreign.Code)
	}
	if got := foreign.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow-origin for foreign origin, got %q", got)
	}
}
