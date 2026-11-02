package handler

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/pkg/validator"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	LoginCodeAccountLocked      = "account_locked"
	LoginCodeCaptchaRequired    = "captcha_required"
	LoginCodeInvalidCredentials = "invalid_credentials"
	LoginCodeTwoFactorRequired  = "two_factor_required"
	LoginCodeInvalidTOTP        = "invalid_totp"
)

type AuthHandler struct {
	authService        *service.AuthService
	loginLimiter       gin.HandlerFunc
	loginNotifications bool
}

var dispatchLoginNotification = service.SendNotification

func NewAuthHandler() *AuthHandler {
	return &AuthHandler{
		authService:        service.NewAuthService(),
		loginLimiter:       middleware.RateLimit(5, time.Minute),
		loginNotifications: true,
	}
}

func NewManagementAuthHandler() *AuthHandler {
	handler := NewAuthHandler()
	handler.loginNotifications = false
	return handler
}

func (h *AuthHandler) CheckInit(c *gin.Context) {
	response.Success(c, gin.H{"need_init": h.authService.NeedInit()})
}

func (h *AuthHandler) Init(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	user, err := h.authService.InitAdmin(req.Username, req.Password)
	if err != nil {
		switch err {
		case service.ErrInvalidUsername:
			response.BadRequest(c, "用户名需 1-32 位，支持中文、字母、数字和下划线")
		case service.ErrPasswordTooShort:
			response.BadRequest(c, "密码长度需 6-128 位")
		default:
			response.BadRequest(c, err.Error())
		}
		return
	}

	response.Success(c, gin.H{
		"message": "初始化成功",
		"user":    user.ToDict(),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Username string                 `json:"username" binding:"required"`
		Password string                 `json:"password" binding:"required"`
		TOTPCode string                 `json:"totp_code"`
		Captcha  service.CaptchaPayload `json:"captcha"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	ip := middleware.ResolveClientIP(c)
	ua := c.GetHeader("User-Agent")
	clientInfo := service.DetectSessionClientInfo(
		c.GetHeader("X-Client-Type"),
		c.GetHeader("X-Client-App"),
		c.GetHeader("X-Client-Platform"),
		c.GetHeader("X-Device-Model"),
		c.GetHeader("X-Device-Name"),
		c.GetHeader("X-OS-Version"),
		ua,
	)
	clientType := clientInfo.Type
	clientName := service.SessionClientDisplayName(clientInfo)

	locked, remaining := service.CheckLoginLock(ip, req.Username)
	if locked {
		service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "账号已锁定")
		remainSec := int(remaining.Seconds())
		c.JSON(429, gin.H{
			"error":             fmt.Sprintf("账号已锁定，请 %.0f 分钟后重试", remaining.Minutes()),
			"code":              LoginCodeAccountLocked,
			"locked":            true,
			"remaining_seconds": remainSec,
		})
		return
	}

	if !service.IsIPWhitelisted(ip) {
		service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "IP 不在白名单")
		response.Forbidden(c, "当前 IP 不在登录白名单中")
		return
	}

	captchaCfg := service.GetCaptchaRuntimeConfig()
	if service.IsCaptchaRequired(ip, req.Username) {
		verifyResult, err := service.VerifyLoginCaptcha(req.Captcha)
		if err != nil {
			switch err {
			case service.ErrCaptchaRequired:
				c.JSON(401, gin.H{
					"error":                  err.Error(),
					"code":                   LoginCodeCaptchaRequired,
					"captcha_required":       true,
					"captcha_id":             captchaCfg.CaptchaID,
					"captcha_threshold":      captchaCfg.RequireAfterFailures,
					"require_after_failures": captchaCfg.RequireAfterFailures,
				})
			default:
				response.InternalError(c, "验证码校验失败，请稍后重试")
			}
			return
		}
		if !verifyResult.Passed {
			if verifyResult.UpstreamError {
				c.JSON(503, gin.H{
					"error":                       "验证码服务暂时不可用，请稍后重试",
					"captcha_required":            true,
					"captcha_service_unavailable": true,
					"captcha_id":                  captchaCfg.CaptchaID,
					"captcha_reason":              verifyResult.Reason,
					"captcha_fail_mode":           captchaCfg.FailMode,
					"captcha_threshold":           captchaCfg.RequireAfterFailures,
					"require_after_failures":      captchaCfg.RequireAfterFailures,
				})
				return
			}

			c.JSON(401, gin.H{
				"error":                  "验证码校验失败，请重新完成人机验证",
				"code":                   LoginCodeCaptchaRequired,
				"captcha_required":       true,
				"captcha_invalid":        true,
				"captcha_id":             captchaCfg.CaptchaID,
				"captcha_reason":         verifyResult.Reason,
				"captcha_threshold":      captchaCfg.RequireAfterFailures,
				"require_after_failures": captchaCfg.RequireAfterFailures,
			})
			return
		}
	}

	user, accessToken, refreshToken, accessInfo, refreshInfo, err := h.authService.Login(req.Username, req.Password, req.TOTPCode)
	if err == nil {
		// 先落会话行，落成了才算登录成功。这枚 access 的会话号就是会话行的 jti，撤销全部会话、改密、禁用、删除
		// 都靠遍历会话行找到它；会话行写不进去还照发 token，签出的就是一枚永远撤销不掉的登录令牌。
		if sessionErr := service.CreateSessionWithRefresh(user.ID, user.Username, accessInfo.JTI, refreshInfo.JTI, clientType, clientName, ip, ua, accessInfo.ExpiresAt, refreshInfo.ExpiresAt); sessionErr != nil {
			response.InternalError(c, "登录失败")
			return
		}

		// 会话行落库之后，按 id 重读一次用户，与这次校验时读到的那份比：启用状态、角色、密码哈希、用户名。
		// 为什么要这一步：管理员的禁用 / 降权 / 重置密码 / 改名 / 删号会调 RevokeAllUserSessions，它只能撤掉
		// 「当时已经在库里」的会话行。改动如果恰好落在「登录校验完、会话行还没落库」之间，这次的会话行就不在
		// 那次撤销的范围里，之后也没有路径会再撤它；新令牌每请求只查黑名单、不查用户状态，
		// 这枚令牌就能在账号已被禁用的情况下一直用到自然过期。
		// 为什么这样就关住了窗口：管理员的更新早于这次重读，这里看得见，当场撤掉刚建的会话；
		// 晚于这次重读，会话行已经在库里，管理员随后的 RevokeAllUserSessions 一定遍历得到它。
		// 本推理依赖「先把用户状态落库、再调 RevokeAllUserSessions」的顺序（删号就是先删用户行、再撤销）：
		// 反过来先撤销后落库，登录能落在撤销的 Find 之后、落库之前，重读看不到变化，会话行也躲过了那次撤销。
		// 新增改用户状态的入口必须遵守这个顺序。
		// 代价是每次登录多一条查询，不在每请求路径上。
		// 有变化就走下面原有的失败分支：被禁用回 403；其余回 401——密码被重置、改名、删号时这份凭据本来就登不上了，
		// 角色变了也按 401 处理：这次校验用的账号快照已经过时，重新登录一次就按新角色签发。
		var latest model.User
		if database.DB.First(&latest, user.ID).Error != nil || latest.Username != user.Username {
			err = service.ErrUserNotFound
		} else if !latest.Enabled {
			err = service.ErrUserDisabled
		} else if latest.Password != user.Password || latest.Role != user.Role {
			err = service.ErrInvalidPassword
		}
		if err != nil {
			service.RevokeSession(accessInfo.JTI, accessInfo.ExpiresAt)
		}
	}
	if err != nil {
		switch err {
		case service.ErrUserNotFound, service.ErrInvalidPassword:
			failedAttempts := service.RecordFailedLogin(ip, req.Username)
			service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "登录失败")
			c.JSON(401, gin.H{
				"error":                  "用户名或密码错误",
				"code":                   LoginCodeInvalidCredentials,
				"failed_attempts":        failedAttempts,
				"captcha_required":       captchaCfg.Enabled,
				"captcha_id":             captchaCfg.CaptchaID,
				"captcha_threshold":      captchaCfg.RequireAfterFailures,
				"require_after_failures": captchaCfg.RequireAfterFailures,
			})
		case service.ErrUserDisabled:
			service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "登录失败")
			response.Forbidden(c, "账号已被禁用")
		case service.ErrTOTPRequired:
			c.JSON(401, gin.H{
				"error":                  "请输入两步验证码",
				"code":                   LoginCodeTwoFactorRequired,
				"two_factor_required":    true,
				"captcha_required":       captchaCfg.Enabled,
				"captcha_id":             captchaCfg.CaptchaID,
				"captcha_threshold":      captchaCfg.RequireAfterFailures,
				"require_after_failures": captchaCfg.RequireAfterFailures,
			})
		case service.ErrInvalidTOTP:
			failedAttempts := service.RecordFailedLogin(ip, req.Username)
			service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "登录失败")
			c.JSON(401, gin.H{
				"error":                  "两步验证码错误",
				"code":                   LoginCodeInvalidTOTP,
				"two_factor_required":    true,
				"failed_attempts":        failedAttempts,
				"captcha_required":       captchaCfg.Enabled,
				"captcha_id":             captchaCfg.CaptchaID,
				"captcha_threshold":      captchaCfg.RequireAfterFailures,
				"require_after_failures": captchaCfg.RequireAfterFailures,
			})
		default:
			service.RecordFailedLogin(ip, req.Username)
			service.RecordLoginLog(0, req.Username, ip, clientName, ua, 1, "登录失败")
			response.InternalError(c, "登录失败")
		}
		return
	}

	service.ClearLoginAttempts(ip, req.Username)
	service.RecordLoginLog(user.ID, user.Username, ip, clientName, ua, 0, "登录成功")
	if h.loginNotifications && model.GetRegisteredConfigBool("notify_on_login") {
		go dispatchLoginNotification(
			"登录成功通知",
			fmt.Sprintf("用户 %s 于 %s 从 IP %s 登录成功。",
				user.Username, time.Now().Format("2006-01-02 15:04:05"), ip),
		)
	}

	response.Success(c, gin.H{
		"message":       "登录成功",
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"user":          user.ToDict(),
	})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	// 按会话号撤销整个会话，而不是只拉黑发起退出的这一枚：两端退出时手里的几乎总是续期得来的 access，
	// 同一会话的其它 access（别的标签页）与 refresh 必须一起作废。
	service.RevokeSession(c.GetString("sid"), c.GetTime("token_expires_at"))
	response.Success(c, gin.H{"message": "已退出登录"})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	tokenStr := middleware.ExtractBearerToken(c.GetHeader("Authorization"))

	newToken, err := h.authService.RefreshToken(tokenStr)
	if err != nil {
		response.Unauthorized(c, "令牌无效或已过期")
		return
	}

	response.Success(c, gin.H{"access_token": newToken})
}

func (h *AuthHandler) GetUser(c *gin.Context) {
	username, _ := c.Get("username")
	user, err := h.authService.GetUser(username.(string))
	if err != nil {
		response.NotFound(c, "用户不存在")
		return
	}
	response.Success(c, gin.H{"user": user.ToDict()})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	username, _ := c.Get("username")
	if err := h.authService.ChangePassword(username.(string), req.OldPassword, req.NewPassword); err != nil {
		switch err {
		case service.ErrInvalidPassword:
			response.BadRequest(c, "当前密码不正确")
		case service.ErrPasswordTooShort:
			response.BadRequest(c, "新密码长度需 6-128 位")
		default:
			response.InternalError(c, "修改密码失败")
		}
		return
	}

	user, _ := h.authService.GetUser(username.(string))
	if user != nil {
		service.RevokeAllUserSessions(user.ID)
	}

	response.Success(c, gin.H{"message": "密码修改成功，请重新登录"})
}

func (h *AuthHandler) ChangeUsername(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	newUsername := strings.TrimSpace(req.Username)
	if !validator.ValidateUsername(newUsername) {
		response.BadRequest(c, "用户名需 1-32 位，支持中文、字母、数字和下划线")
		return
	}

	currentUsername, _ := c.Get("username")
	if newUsername == currentUsername.(string) {
		response.BadRequest(c, "新用户名与当前用户名相同")
		return
	}

	var existing model.User
	if err := database.DB.Where("username = ?", newUsername).First(&existing).Error; err == nil {
		response.BadRequest(c, "该用户名已被使用")
		return
	}

	var user model.User
	if err := database.DB.Where("username = ?", currentUsername).First(&user).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	if err := database.DB.Model(&user).Update("username", newUsername).Error; err != nil {
		response.InternalError(c, "修改用户名失败")
		return
	}

	service.RevokeAllUserSessions(user.ID)
	response.Success(c, gin.H{"message": "用户名修改成功，请重新登录", "user": user.ToDict()})
}

func (h *AuthHandler) CaptchaConfig(c *gin.Context) {
	cfg := service.GetCaptchaRuntimeConfig()
	username := c.Query("username")

	response.Success(c, gin.H{
		"enabled":                cfg.Enabled,
		"captcha_id":             cfg.CaptchaID,
		"configured":             cfg.Configured,
		"implemented":            true,
		"required":               service.IsCaptchaRequired(middleware.ResolveClientIP(c), username),
		"captcha_fail_mode":      cfg.FailMode,
		"require_after_failures": cfg.RequireAfterFailures,
		"message":                service.BuildCaptchaStatusMessage(cfg),
	})
}

const avatarMaxBytes = 2 * 1024 * 1024

var avatarAllowedExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

func avatarDir() string {
	return filepath.Join(config.C.Data.Dir, "avatars")
}

func (h *AuthHandler) UploadAvatar(c *gin.Context) {
	username, _ := c.Get("username")
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		response.BadRequest(c, "请选择要上传的头像文件")
		return
	}
	defer file.Close()

	if header.Size > avatarMaxBytes {
		response.BadRequest(c, "头像文件不能超过 2MB")
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !avatarAllowedExts[ext] {
		response.BadRequest(c, "仅支持 JPG、PNG、GIF、WebP 格式")
		return
	}

	if ext != ".webp" {
		if _, _, err := image.DecodeConfig(file); err != nil {
			response.BadRequest(c, "文件不是有效的图片")
			return
		}
		file.Seek(0, io.SeekStart)
	}

	dir := avatarDir()
	os.MkdirAll(dir, 0755)

	filename := uuid.New().String() + ext
	savePath := filepath.Join(dir, filename)

	dst, err := os.Create(savePath)
	if err != nil {
		response.InternalError(c, "保存头像失败")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(savePath)
		response.InternalError(c, "保存头像失败")
		return
	}

	avatarURL := "/api/v1/auth/avatar/" + filename

	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		os.Remove(savePath)
		response.NotFound(c, "用户不存在")
		return
	}

	if user.AvatarURL != "" {
		oldFile := strings.TrimPrefix(user.AvatarURL, "/api/v1/auth/avatar/")
		oldFile = strings.TrimPrefix(oldFile, "/api/auth/avatar/")
		if oldFile != "" {
			os.Remove(filepath.Join(dir, filepath.Base(oldFile)))
		}
	}

	database.DB.Model(&user).Update("avatar_url", avatarURL)

	response.Success(c, gin.H{
		"message":    "头像上传成功",
		"avatar_url": avatarURL,
	})
}

func (h *AuthHandler) DeleteAvatar(c *gin.Context) {
	username, _ := c.Get("username")

	var user model.User
	if err := database.DB.Where("username = ?", username).First(&user).Error; err != nil {
		response.NotFound(c, "用户不存在")
		return
	}

	if user.AvatarURL == "" {
		response.Success(c, gin.H{"message": "当前没有设置头像"})
		return
	}

	oldFile := strings.TrimPrefix(user.AvatarURL, "/api/v1/auth/avatar/")
	oldFile = strings.TrimPrefix(oldFile, "/api/auth/avatar/")
	if oldFile != "" {
		os.Remove(filepath.Join(avatarDir(), filepath.Base(oldFile)))
	}

	database.DB.Model(&user).Update("avatar_url", "")

	response.Success(c, gin.H{"message": "头像已删除"})
}

func (h *AuthHandler) ServeAvatar(c *gin.Context) {
	filename := filepath.Base(c.Param("filename"))
	if filename == "" || filename == "." {
		response.BadRequest(c, "无效的文件名")
		return
	}

	filePath := filepath.Join(avatarDir(), filename)
	if _, err := os.Stat(filePath); err != nil {
		response.NotFound(c, "头像不存在")
		return
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.File(filePath)
}

func (h *AuthHandler) RegisterRoutes(r *gin.RouterGroup) {
	// /api and /api/v1 receive independent rate-limit buckets.
	loginLimiter := middleware.RateLimit(5, time.Minute)
	auth := r.Group("/auth")
	{
		auth.GET("/check-init", h.CheckInit)
		auth.POST("/init", h.Init)
		auth.POST("/login", loginLimiter, h.Login)
		auth.POST("/logout", middleware.JWTAuth(), h.Logout)
		auth.POST("/refresh", h.Refresh)
		auth.GET("/user", middleware.JWTAuth(), h.GetUser)
		auth.PUT("/password", middleware.JWTAuth(), h.ChangePassword)
		auth.PUT("/username", middleware.JWTAuth(), h.ChangeUsername)
		// 编辑器偏好是 per-user 的，只要登录就能读写自己那份，刻意不加 RequireAdmin：
		// 脚本页 operator 就能打开编辑器，不能要求管理员权限才允许改自己的开关。
		auth.GET("/preferences", middleware.JWTAuth(), h.GetPreferences)
		auth.PUT("/preferences", middleware.JWTAuth(), h.UpdatePreferences)
		auth.GET("/captcha-config", h.CaptchaConfig)
		auth.POST("/avatar", middleware.JWTAuth(), h.UploadAvatar)
		auth.DELETE("/avatar", middleware.JWTAuth(), h.DeleteAvatar)
		auth.GET("/avatar/:filename", h.ServeAvatar)
	}
}

func (h *AuthHandler) RegisterManagementRoutes(r *gin.RouterGroup) {
	h.RegisterRoutes(r)
}
