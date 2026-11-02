package middleware

import (
	"net/http"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ScriptTokenUsername 是注入脚本环境的那枚面板凭据（notify.py / sendNotify.js / DAIDAI_TOKEN / ddp 交互会话）的用户名。
// 它不是真实用户：库里没有这一行，也没有登录会话。用户名规则不允许 "-"，真实用户不可能与它同名。
const ScriptTokenUsername = "internal-script-notify"

type Claims struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	TokenType string `json:"token_type"`
	// SessionID 是签出这枚 access 的登录会话号，等于 user_sessions.jti（也就是登录那枚 access 的 jti）。
	// 续期得来的 access 沿用同一个会话号；撤销会话时把会话号写进黑名单，这条会话签出的所有 access 一起作废。
	// refresh、应用令牌、脚本令牌都不带（omitempty），这几类 token 的形状与加这个字段之前一模一样；
	// 旧版本服务端会忽略这个未知字段，回退安全。
	SessionID string `json:"sid,omitempty"`
	// NotifyOnly 只出现在「任务限定了可用环境变量」（#168）时注入脚本的那枚令牌上：
	// 它只能调发通知接口，其余接口一律 403（JWTAuth 与 MCP 的 Bearer 鉴权两处都拦）。
	// omitempty：其它令牌（登录、续期、refresh、应用令牌、普通脚本令牌）的形状与加字段之前一模一样。
	NotifyOnly bool `json:"notify_only,omitempty"`
	jwt.RegisteredClaims
}

// NotifyOnlyTokenForbiddenMessage 是 notify-only 脚本令牌调了发通知以外接口时的 403 文案，
// JWTAuth 与 MCP 鉴权共用；docs/script-api.md 里也写了这句，改动要同步。
const NotifyOnlyTokenForbiddenMessage = "该任务限定了可用环境变量，脚本令牌只能用来发通知"

// notifyOnlyAllowedPaths 是 notify-only 令牌唯一能用的接口：发通知路由在 /api/v1 与旧的 /api 各注册了一份。
// 按 c.FullPath()（路由模板）判断，不按原始 URL：不会被多余斜杠、大小写、查询串之类的写法绕过。
var notifyOnlyAllowedPaths = map[string]bool{
	"/api/v1/notifications/send": true,
	"/api/notifications/send":    true,
}

type TokenInfo struct {
	Token     string
	JTI       string
	ExpiresAt time.Time
}

func GenerateAccessToken(username, role string) (string, error) {
	info, err := GenerateAccessTokenInfo(username, role)
	if err != nil {
		return "", err
	}
	return info.Token, nil
}

// GenerateAccessTokenInfo 给登录签发 access：它开启一个新会话，会话号就是它自己的 jti
// （登录时这个 jti 同时写进 user_sessions.jti）。
func GenerateAccessTokenInfo(username, role string) (*TokenInfo, error) {
	jti := generateJTI()
	return generateAccessTokenInfoWithTTL(username, role, jti, jti, config.C.JWT.AccessTokenExpire, false)
}

// GenerateSessionAccessTokenInfo 只给续期用：新 jti，沿用原会话号。
// 同一会话同时存在多枚有效 access 是正常的（多标签页、SSE 与普通请求各自续期），
// 所以撤销只能按会话号整体作废，不能只认「最新那一枚」。
func GenerateSessionAccessTokenInfo(username, role, sessionID string) (*TokenInfo, error) {
	return generateAccessTokenInfoWithTTL(username, role, generateJTI(), sessionID, config.C.JWT.AccessTokenExpire, false)
}

func GenerateTemporaryAccessToken(username, role string, ttl time.Duration) (string, error) {
	info, err := GenerateTemporaryAccessTokenInfo(username, role, ttl)
	if err != nil {
		return "", err
	}
	return info.Token, nil
}

// GenerateTemporaryAccessTokenInfo 与 GenerateTemporaryAccessToken 等价，但保留 jti 与到期时间。
// 签发方拿到 jti 才能在凭据用完后主动拉黑（IsTokenBlocked 按 jti 单条命中），
// 否则临时 token 只能等自然过期，面板自己都不知道它是谁。
// 临时令牌（应用令牌、脚本令牌）不属于任何登录会话，不带会话号。
func GenerateTemporaryAccessTokenInfo(username, role string, ttl time.Duration) (*TokenInfo, error) {
	return generateAccessTokenInfoWithTTL(username, role, generateJTI(), "", ttl, false)
}

// GenerateNotifyOnlyAccessTokenInfo 签一枚只能发通知的临时令牌（#168）：任务限定了可用环境变量时注入脚本用。
// 除了带 notify_only 之外与 GenerateTemporaryAccessTokenInfo 完全一样，同样按 jti 吊销。
func GenerateNotifyOnlyAccessTokenInfo(username, role string, ttl time.Duration) (*TokenInfo, error) {
	return generateAccessTokenInfoWithTTL(username, role, generateJTI(), "", ttl, true)
}

func generateAccessTokenInfoWithTTL(username, role, jti, sessionID string, ttl time.Duration, notifyOnly bool) (*TokenInfo, error) {
	if ttl <= 0 {
		ttl = config.C.JWT.AccessTokenExpire
	}

	expiresAt := time.Now().Add(ttl)
	claims := Claims{
		Username:   username,
		Role:       role,
		TokenType:  "access",
		SessionID:  sessionID,
		NotifyOnly: notifyOnly,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(config.C.JWT.Secret))
	if err != nil {
		return nil, err
	}
	return &TokenInfo{Token: tokenStr, JTI: jti, ExpiresAt: expiresAt}, nil
}

func GenerateRefreshToken(username, role string) (string, error) {
	info, err := GenerateRefreshTokenInfo(username, role)
	if err != nil {
		return "", err
	}
	return info.Token, nil
}

func GenerateRefreshTokenInfo(username, role string) (*TokenInfo, error) {
	jti := generateJTI()
	expiresAt := time.Now().Add(config.C.JWT.RefreshTokenExpire)
	claims := Claims{
		Username:  username,
		Role:      role,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(config.C.JWT.Secret))
	if err != nil {
		return nil, err
	}
	return &TokenInfo{Token: tokenStr, JTI: jti, ExpiresAt: expiresAt}, nil
}

func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(config.C.JWT.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

func IsTokenBlocked(jti string) bool {
	var count int64
	database.DB.Model(&model.TokenBlocklist{}).Where("jti = ?", jti).Count(&count)
	return count > 0
}

// IsUserSessionToken 判断一枚 access 是不是用户登录会话签出的令牌。
// 应用令牌（app: 前缀：开放 API、MCP、企业微信的临时令牌）与脚本令牌都没有用户行、也没有会话行，
// 只能按 jti 单条拉黑。把它们当用户令牌去查会话会一律 401，所有任务通知、开放 API、MCP 都会挂掉。
// 脚本令牌必须按用户名精确匹配，不能按前缀放宽豁免面。
func IsUserSessionToken(claims *Claims) bool {
	return !isAppToken(claims.Username, claims.Role) && claims.Username != ScriptTokenUsername
}

// AccessTokenRevoked 判定一枚 access 是否已作废，JWTAuth 与 MCP 的 Bearer 鉴权共用。
// 🔴 每个请求只允许查这一次库：数据库是单连接，多一条慢查询就堵全站（#153）。
func AccessTokenRevoked(claims *Claims) bool {
	// 应用令牌、脚本令牌：沿用老规则，只按 jti 单条拉黑
	if !IsUserSessionToken(claims) {
		return IsTokenBlocked(claims.ID)
	}
	// 新版签发的用户令牌：jti 或会话号任一在黑名单里就算作废。
	// 撤销会话写的是会话号那一行，于是这条会话续期出的 access 全部一起失效。
	if claims.SessionID != "" {
		var count int64
		database.DB.Model(&model.TokenBlocklist{}).
			Where("jti IN ?", []string{claims.ID, claims.SessionID}).
			Count(&count)
		return count > 0
	}
	// 升级前签发的用户令牌（没有会话号）：只放行「登录时签发的那一枚」——会话行还在、用户仍启用、用户名没改。
	// 旧版 APP（≤ v1.2.6）从不续期，手里只有这一枚，所以升级不会把它们踢下线；
	// 续期得来的老令牌在这里回 401，网页与新版 APP 会用老 refresh 自动换一枚带会话号的新令牌。
	// 两个子查询合在一条语句里，仍然只查一次库。
	var row struct {
		Blocked int64
		Alive   int64
	}
	err := database.DB.Raw(`SELECT
		(SELECT COUNT(*) FROM token_blocklist WHERE jti = ?) AS blocked,
		(SELECT COUNT(*) FROM user_sessions s JOIN users u ON u.id = s.user_id
			WHERE s.jti = ? AND u.username = ? AND u.enabled = ?) AS alive`,
		claims.ID, claims.ID, claims.Username, true).Scan(&row).Error
	if err != nil {
		return true
	}
	return row.Blocked > 0 || row.Alive == 0
}

func ExtractBearerToken(authHeader string) string {
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return ""
	}

	return strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
}

func JWTAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr := ExtractBearerToken(c.GetHeader("Authorization"))

		if tokenStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "缺少授权令牌"})
			c.Abort()
			return
		}

		claims, err := ParseToken(tokenStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "令牌无效或已过期"})
			c.Abort()
			return
		}

		if claims.TokenType != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "令牌类型错误"})
			c.Abort()
			return
		}

		if AccessTokenRevoked(claims) {
			if IsUserSessionToken(claims) {
				// 登录会话已失效：退出、撤销、改密、禁用、删除、改角色、被新登录顶替，或是升级前续期得来的老令牌。
				// 必须是 401：网页与 APP 遇 401 会先用 refresh 续一次，会话还在就无感续上，不在就回登录页；
				// 用 403 两端都不会登出。只「增加」code 字段，与登录失败的写法一致，两端都不读它。
				c.JSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录", "code": "session_revoked"})
			} else {
				// 脚本令牌、应用令牌：这句文案写进了 docs/script-api.md，保持不变
				c.JSON(http.StatusUnauthorized, gin.H{"error": "令牌已被撤销"})
			}
			c.Abort()
			return
		}

		// 任务限定了可用环境变量（#168）时，注入脚本的令牌只能发通知：
		// 否则脚本拿着它调 /envs 就能读到任务没给它的变量，或者新建一个任务再运行来绕开限制。
		// 放在撤销判定之后：已吊销的令牌照旧回 401「令牌已被撤销」，不因为这条改成 403。
		// 这只是减少不必要的暴露，不是沙箱——脚本与面板同一系统用户运行，仍能用 ddp 或直接读库拿到全部变量。
		if claims.NotifyOnly && !(c.Request.Method == http.MethodPost && notifyOnlyAllowedPaths[c.FullPath()]) {
			c.JSON(http.StatusForbidden, gin.H{"error": NotifyOnlyTokenForbiddenMessage})
			c.Abort()
			return
		}

		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Set("jti", claims.ID)
		// 会话号：退出登录、「撤销其他会话」、会话列表的 current 都按它认当前会话。
		// 升级前签发的老令牌没有会话号，能走到这里的只有「登录时那一枚」，它的 jti 就是会话号。
		sessionID := claims.SessionID
		if sessionID == "" {
			sessionID = claims.ID
		}
		c.Set("sid", sessionID)
		if claims.ExpiresAt != nil {
			c.Set("token_expires_at", claims.ExpiresAt.Time)
		}
		// 执行日志流在心跳时要拿它复查撤销（handler/log.go）
		c.Set("claims", claims)
		if isAppToken(claims.Username, claims.Role) {
			c.Set("token_kind", "app")
		} else {
			c.Set("token_kind", "user")
		}
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists || role.(string) != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func RequireRole(minRole string) gin.HandlerFunc {
	roleLevel := map[string]int{
		"viewer":   1,
		"operator": 2,
		"admin":    3,
	}

	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"error": "拒绝访问"})
			c.Abort()
			return
		}

		if c.GetString("token_kind") == "app" {
			if c.GetBool("app_scope_authorized") {
				c.Next()
				return
			}

			c.JSON(http.StatusForbidden, gin.H{"error": "应用令牌无权访问此接口"})
			c.Abort()
			return
		}

		if roleLevel[role.(string)] < roleLevel[minRole] {
			c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func generateJTI() string {
	return uuid.New().String()
}
