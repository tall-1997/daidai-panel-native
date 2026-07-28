package handler

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type PlatformTokenHandler struct{}

type sealedPlatformTokenPayload struct {
	Provider string `json:"provider"`
	Cipher   string `json:"cipher"`
}

func NewPlatformTokenHandler() *PlatformTokenHandler {
	return &PlatformTokenHandler{}
}

func sealPlatformToken(ctx context.Context, tokenID string, token string) (string, string, error) {
	sealed, err := service.RuntimeSecretStoreInstance().Seal(ctx, "platform-token:"+tokenID, []byte(token))
	if err != nil {
		return "", "", err
	}
	payload := sealedPlatformTokenPayload{
		Provider: sealed.Provider,
		Cipher:   base64.StdEncoding.EncodeToString(sealed.Cipher),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(token))
	return string(encoded), hex.EncodeToString(sum[:]), nil
}

func platformTokenKeyParts(platformName, serviceName, serviceUser string) string {
	parts := []string{strings.TrimSpace(platformName), strings.TrimSpace(serviceName), strings.TrimSpace(serviceUser)}
	for i := range parts {
		if parts[i] == "" {
			parts[i] = "default"
		}
	}
	return strings.Join(parts, ":")
}

func (h *PlatformTokenHandler) Platforms(c *gin.Context) {
	var platforms []model.Platform
	database.DB.Order("name ASC").Find(&platforms)

	data := make([]map[string]interface{}, len(platforms))
	for i, p := range platforms {
		data[i] = p.ToDict()
	}

	response.Success(c, gin.H{"data": data})
}

func (h *PlatformTokenHandler) CreatePlatform(c *gin.Context) {
	var req struct {
		Name  string `json:"name" binding:"required"`
		Label string `json:"label"`
		Icon  string `json:"icon"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	platform := model.Platform{
		Name:  req.Name,
		Label: req.Label,
		Icon:  req.Icon,
	}
	if platform.Label == "" {
		platform.Label = platform.Name
	}

	if err := database.DB.Create(&platform).Error; err != nil {
		response.BadRequest(c, "平台已存在")
		return
	}

	response.Created(c, gin.H{"message": "创建成功", "data": platform.ToDict()})
}

func (h *PlatformTokenHandler) DeletePlatform(c *gin.Context) {
	platformID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("platform_id = ?", platformID).Delete(&model.PlatformToken{})
	database.DB.Where("id = ?", platformID).Delete(&model.Platform{})
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *PlatformTokenHandler) List(c *gin.Context) {
	platformID := c.Query("platform_id")

	query := database.DB.Model(&model.PlatformToken{}).Preload("Platform")
	if platformID != "" {
		query = query.Where("platform_id = ?", platformID)
	}

	var tokens []model.PlatformToken
	query.Order("created_at DESC").Find(&tokens)

	data := make([]map[string]interface{}, len(tokens))
	for i, t := range tokens {
		data[i] = t.ToDict()
	}

	response.Success(c, gin.H{"data": data})
}

func (h *PlatformTokenHandler) Create(c *gin.Context) {
	var req struct {
		PlatformID  uint   `json:"platform_id" binding:"required"`
		Name        string `json:"name" binding:"required"`
		Token       string `json:"token" binding:"required"`
		Service     string `json:"service"`
		ServiceUser string `json:"service_user"`
		Remarks     string `json:"remarks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var platform model.Platform
	if err := database.DB.First(&platform, req.PlatformID).Error; err != nil {
		response.BadRequest(c, "平台不存在")
		return
	}
	sealed, tokenHash, err := sealPlatformToken(c.Request.Context(), platformTokenKeyParts(platform.Name, req.Service, req.ServiceUser), req.Token)
	if err != nil {
		response.InternalError(c, "封存令牌失败")
		return
	}

	token := model.PlatformToken{
		PlatformID:  req.PlatformID,
		Name:        req.Name,
		Token:       "",
		TokenHash:   tokenHash,
		Sealed:      sealed,
		Service:     strings.TrimSpace(req.Service),
		ServiceUser: strings.TrimSpace(req.ServiceUser),
		Remarks:     req.Remarks,
		Enabled:     true,
	}

	if err := database.DB.Create(&token).Error; err != nil {
		response.InternalError(c, "创建令牌失败")
		return
	}

	database.DB.Preload("Platform").First(&token, token.ID)
	response.Created(c, gin.H{"message": "创建成功", "data": token.ToDict()})
}

func (h *PlatformTokenHandler) Update(c *gin.Context) {
	tokenID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var token model.PlatformToken
	if err := database.DB.First(&token, tokenID).Error; err != nil {
		response.NotFound(c, "令牌不存在")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Token       string `json:"token"`
		Service     string `json:"service"`
		ServiceUser string `json:"service_user"`
		Remarks     string `json:"remarks"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Token != "" {
		if strings.TrimSpace(req.Token) != "********" {
			var platform model.Platform
			if err := database.DB.First(&platform, token.PlatformID).Error; err != nil {
				response.BadRequest(c, "平台不存在")
				return
			}
			serviceName := firstNonEmpty(req.Service, token.Service)
			serviceUser := firstNonEmpty(req.ServiceUser, token.ServiceUser)
			sealed, tokenHash, err := sealPlatformToken(c.Request.Context(), platformTokenKeyParts(platform.Name, serviceName, serviceUser), req.Token)
			if err != nil {
				response.InternalError(c, "封存令牌失败")
				return
			}
			updates["token"] = ""
			updates["token_hash"] = tokenHash
			updates["sealed"] = sealed
		}
	}
	if req.Service != "" {
		updates["service"] = strings.TrimSpace(req.Service)
	}
	if req.ServiceUser != "" {
		updates["service_user"] = strings.TrimSpace(req.ServiceUser)
	}
	if req.Remarks != "" {
		updates["remarks"] = req.Remarks
	}

	if len(updates) > 0 {
		database.DB.Model(&token).Updates(updates)
	}

	database.DB.Preload("Platform").First(&token, tokenID)
	response.Success(c, gin.H{"message": "更新成功", "data": token.ToDict()})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func ResolvePlatformTokenCredential(platformName, serviceName, serviceUser string) (*model.PlatformToken, error) {
	query := database.DB.Model(&model.PlatformToken{}).
		Joins("JOIN platforms ON platforms.id = platform_tokens.platform_id").
		Where("platforms.name = ? AND platform_tokens.enabled = ?", strings.TrimSpace(platformName), true)
	if strings.TrimSpace(serviceName) != "" {
		query = query.Where("platform_tokens.service = ? OR platform_tokens.service = ''", strings.TrimSpace(serviceName))
	}
	if strings.TrimSpace(serviceUser) != "" {
		query = query.Where("platform_tokens.service_user = ? OR platform_tokens.service_user = ''", strings.TrimSpace(serviceUser))
	}
	var token model.PlatformToken
	if err := query.Order("platform_tokens.service_user DESC, platform_tokens.service DESC, platform_tokens.created_at DESC").First(&token).Error; err != nil {
		return nil, fmt.Errorf("未配置服务凭据")
	}
	return &token, nil
}

func (h *PlatformTokenHandler) Delete(c *gin.Context) {
	tokenID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("id = ?", tokenID).Delete(&model.PlatformToken{})
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *PlatformTokenHandler) Enable(c *gin.Context) {
	tokenID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Model(&model.PlatformToken{}).Where("id = ?", tokenID).Update("enabled", true)
	response.Success(c, gin.H{"message": "已启用"})
}

func (h *PlatformTokenHandler) Disable(c *gin.Context) {
	tokenID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Model(&model.PlatformToken{}).Where("id = ?", tokenID).Update("enabled", false)
	response.Success(c, gin.H{"message": "已禁用"})
}

func (h *PlatformTokenHandler) RegisterRoutes(r *gin.RouterGroup) {
	pt := r.Group("/platform-tokens", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireAdmin())
	{
		pt.GET("/platforms", h.Platforms)
		pt.POST("/platforms", h.CreatePlatform)
		pt.DELETE("/platforms/:id", h.DeletePlatform)
		pt.GET("", h.List)
		pt.POST("", h.Create)
		pt.PUT("/:id", h.Update)
		pt.DELETE("/:id", h.Delete)
		pt.PUT("/:id/enable", h.Enable)
		pt.PUT("/:id/disable", h.Disable)
	}
}
