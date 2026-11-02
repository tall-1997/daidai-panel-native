package handler

import (
	"crypto/subtle"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

// mcpDispatchTokenTTL 是 Basic 凭据现铸的应用令牌有效期。这枚令牌只在本次 HTTP 请求内部回放接口时使用，
// 从不下发给客户端；给 10 分钟只是为了盖住 run_script 最长约 50 秒的等待。
const mcpDispatchTokenTTL = 10 * time.Minute

// authenticateMCPRequest 校验 MCP 请求的凭据，返回回放接口时要带的 Authorization 头。
//
// 支持两种写法：
//   - Authorization: Bearer <JWT>：任何能过 JWTAuth 的令牌（开放 API 应用令牌或登录令牌）；
//   - Authorization: Basic base64(app_key:app_secret)：直接用应用凭据，省去客户端先换令牌这一步。
//
// 失败时这里已写好 401（响应里不带任何工具信息），调用方直接 return。
func authenticateMCPRequest(c *gin.Context) (string, bool) {
	if appKey, appSecret, ok := c.Request.BasicAuth(); ok {
		return authenticateMCPBasic(c, appKey, appSecret)
	}

	tokenStr := middleware.ExtractBearerToken(c.GetHeader("Authorization"))
	if tokenStr == "" {
		response.Unauthorized(c, "缺少授权凭据：请使用 Authorization: Bearer <令牌>，或 Basic base64(app_key:app_secret)")
		return "", false
	}
	return authenticateMCPBearer(c, tokenStr)
}

func authenticateMCPBasic(c *gin.Context, appKey, appSecret string) (string, bool) {
	appKey = strings.TrimSpace(appKey)

	var app model.OpenApp
	found := appKey != "" && database.DB.Where("app_key = ?", appKey).First(&app).Error == nil
	expected := app.AppSecret
	if !found {
		// 查不到应用时也做一次比较，不让「这个 key 存不存在」从响应耗时上看出来。
		expected = strings.Repeat("0", 64)
	}
	secretMatches := subtle.ConstantTimeCompare([]byte(expected), []byte(appSecret)) == 1
	if !found || !secretMatches {
		response.Unauthorized(c, "应用凭据无效")
		return "", false
	}
	if !app.Enabled {
		response.Unauthorized(c, "应用已被禁用")
		return "", false
	}

	// 与 POST /open-api/token 签发的应用令牌同一形态（username=app:<key>，role=app:<scopes>），
	// 回放时照常经过 JWTAuth + OpenAPIAccess，scope、限流、审计都记在这个应用名下。
	token, err := middleware.GenerateTemporaryAccessToken("app:"+app.AppKey, "app:"+app.Scopes, mcpDispatchTokenTTL)
	if err != nil {
		response.InternalError(c, "生成令牌失败")
		return "", false
	}
	return "Bearer " + token, true
}

func authenticateMCPBearer(c *gin.Context, tokenStr string) (string, bool) {
	// 与 middleware.JWTAuth 同一套判定。这里没法直接挂 JWTAuth：Basic 凭据要走另一条路。
	claims, err := middleware.ParseToken(tokenStr)
	if err != nil {
		response.Unauthorized(c, "令牌无效或已过期")
		return "", false
	}
	if claims.TokenType != "access" {
		response.Unauthorized(c, "令牌类型错误")
		return "", false
	}
	// 与 JWTAuth 同一个撤销判定：只按 jti 查的话，被撤销会话续期出的登录令牌还能在这里列出工具清单
	if middleware.AccessTokenRevoked(claims) {
		response.Unauthorized(c, "令牌已被撤销")
		return "", false
	}
	// 只能发通知的脚本令牌（#168）在 MCP 这里直接拒绝：MCP 没有「只发通知」的用法，
	// 放进来至少能列出工具清单。工具回放时 JWTAuth 也会再拦一次，两处都挡住。
	if claims.NotifyOnly {
		response.Forbidden(c, middleware.NotifyOnlyTokenForbiddenMessage)
		return "", false
	}

	if strings.HasPrefix(claims.Username, "app:") || strings.HasPrefix(claims.Role, "app:") {
		// 应用令牌固定 24 小时有效、没有 jti 没法单条吊销，禁用应用是唯一的止血手段。
		// 回放时 OpenAPIAccess 也会拦，但这里要提前拦：否则被禁用应用的旧令牌还能列出工具清单。
		var app model.OpenApp
		appKey := strings.TrimPrefix(claims.Username, "app:")
		if database.DB.Where("app_key = ?", appKey).First(&app).Error != nil || !app.Enabled {
			response.Unauthorized(c, "应用不存在或已被禁用")
			return "", false
		}
	}
	return "Bearer " + tokenStr, true
}
