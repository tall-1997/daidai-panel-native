package handler

import (
	"net/http"

	"daidai-panel/mcptools"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCPHandler 是内置 MCP 服务（issue #128）的 HTTP 入口：POST /api/v1/mcp（Streamable HTTP）。
//
// 一次请求的处理顺序：总开关 mcp_enabled → 鉴权（mcp_auth.go）→ 按 mcp_allow_mutations 现建一个
// MCP Server → 交给官方 SDK 处理 JSON-RPC。工具调用不走 service，而是经 engineDispatcher
// 在进程内回放到面板自己的 /api/v1 接口（mcp_dispatch.go），scope 校验、限流、调用审计全部照旧生效。
//
// Origin 校验不在这里重写：router.Setup 在所有路由之前挂了全局 CORS 中间件，
// 带 Origin 且不在放行口径内的请求在进入本 handler 之前就被它回了 403（防 DNS 重绑定）。
type MCPHandler struct {
	engine http.Handler
}

// NewMCPHandler 需要拿到 engine 本身：工具调用在请求期回放到 engine 上已注册的接口。
func NewMCPHandler(engine http.Handler) *MCPHandler {
	// mcptools 不能反过来 import handler，版本号由这里写进去（initialize 响应的 serverInfo.version）。
	mcptools.ServerVersion = Version
	return &MCPHandler{engine: engine}
}

func (h *MCPHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.POST("/mcp", h.Serve)
	// 无状态模式不支持独立 SSE 流与会话删除，GET / DELETE 交给 SDK 按规范回 405（带 Allow: POST）。
	r.GET("/mcp", h.Serve)
	r.DELETE("/mcp", h.Serve)
}

func (h *MCPHandler) Serve(c *gin.Context) {
	if !model.GetRegisteredConfigBool(model.MCPEnabledConfigKey) {
		response.Forbidden(c, "MCP 服务未开启，请管理员在「系统设置 → MCP 服务」中启用")
		return
	}

	authorization, ok := authenticateMCPRequest(c)
	if !ok {
		return
	}

	// 两个开关都是每次请求现读，管理员在设置页改完立即生效，不需要重启。
	server := mcptools.BuildServer(
		newEngineDispatcher(h.engine, c.Request, authorization),
		model.GetRegisteredConfigBool(model.MCPAllowMutationsConfigKey),
	)
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		// 无状态：每个 POST 自带完整上下文，不维护会话。
		Stateless: true,
		// 直接回 application/json 而不是 SSE：工具都是一问一答，也免得经过反代时被缓冲。
		JSONResponse: true,
		// SDK 默认拒绝「连接落在 127.0.0.1、Host 却不是 localhost」的请求（防 DNS 重绑定）。
		// Docker 镜像里 nginx 恰好从 127.0.0.1 反代到后端并透传原始 Host，开着这项会把所有经 nginx 的
		// MCP 请求一律 403。面板这里的防线是：每个请求都必须带凭据（不认 Cookie，重绑定页面拿不到），
		// 再加全局 CORS 中间件对跨站 Origin 回 403，所以关掉 SDK 这一层。
		DisableLocalhostProtection: true,
	})
	sdkHandler.ServeHTTP(c.Writer, c.Request)
}
