package handler

import (
	"strconv"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type SSHKeyHandler struct{}

func NewSSHKeyHandler() *SSHKeyHandler {
	return &SSHKeyHandler{}
}

func (h *SSHKeyHandler) List(c *gin.Context) {
	var keys []model.SSHKey
	database.DB.Order("created_at DESC").Find(&keys)

	data := make([]map[string]interface{}, len(keys))
	for i, k := range keys {
		data[i] = k.ToDict()
	}

	response.Success(c, gin.H{"data": data})
}

func (h *SSHKeyHandler) Create(c *gin.Context) {
	var req struct {
		Name       string `json:"name" binding:"required"`
		PrivateKey string `json:"private_key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	key := model.SSHKey{
		Name:       req.Name,
		PrivateKey: req.PrivateKey,
	}

	tx := database.DB.Begin()
	if tx.Error != nil {
		response.InternalError(c, "创建 SSH 密钥失败")
		return
	}
	if err := tx.Create(&key).Error; err != nil {
		tx.Rollback()
		response.BadRequest(c, "同名 SSH 密钥已存在")
		return
	}
	// Android 端密钥不落明文：交给运行时 SecretStore 密封，库里只留密文与 provider 标记。
	if err := service.SealSSHKeyPrivateKey(c.Request.Context(), &key, req.PrivateKey); err != nil {
		tx.Rollback()
		response.InternalError(c, "创建 SSH 密钥失败")
		return
	}
	if err := tx.Model(&key).Updates(map[string]interface{}{
		"private_key":                 key.PrivateKey,
		"private_key_secret_provider": key.PrivateKeySecretProvider,
		"private_key_secret_cipher":   key.PrivateKeySecretCipher,
	}).Error; err != nil {
		tx.Rollback()
		response.InternalError(c, "创建 SSH 密钥失败")
		return
	}
	tx.Commit()

	response.Created(c, gin.H{"message": "创建成功", "data": key.ToDict()})
}

func (h *SSHKeyHandler) Update(c *gin.Context) {
	keyID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var key model.SSHKey
	if err := database.DB.First(&key, keyID).Error; err != nil {
		response.NotFound(c, "SSH 密钥不存在")
		return
	}

	var req struct {
		Name       string `json:"name"`
		PrivateKey string `json:"private_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	updates := make(map[string]interface{})
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.PrivateKey != "" {
		if err := service.SealSSHKeyPrivateKey(c.Request.Context(), &key, req.PrivateKey); err != nil {
			response.InternalError(c, "更新 SSH 密钥失败")
			return
		}
		updates["private_key"] = key.PrivateKey
		updates["private_key_secret_provider"] = key.PrivateKeySecretProvider
		updates["private_key_secret_cipher"] = key.PrivateKeySecretCipher
	}

	if len(updates) > 0 {
		// 改名撞上别的密钥会在这里报错；不接 .Error 就会出现「提示更新成功、刷新又变回去」。
		if err := database.DB.Model(&key).Updates(updates).Error; err != nil {
			response.BadRequest(c, "同名 SSH 密钥已存在")
			return
		}
	}

	database.DB.First(&key, keyID)
	response.Success(c, gin.H{"message": "更新成功", "data": key.ToDict()})
}

func (h *SSHKeyHandler) Delete(c *gin.Context) {
	keyID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("id = ?", keyID).Delete(&model.SSHKey{})
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *SSHKeyHandler) Detail(c *gin.Context) {
	keyID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var key model.SSHKey
	if err := database.DB.First(&key, keyID).Error; err != nil {
		response.NotFound(c, "SSH 密钥不存在")
		return
	}

	response.Success(c, gin.H{"data": key.ToDictWithKey()})
}

func (h *SSHKeyHandler) RegisterRoutes(r *gin.RouterGroup) {
	keys := r.Group("/ssh-keys", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireAdmin())
	{
		keys.GET("", h.List)
		keys.POST("", h.Create)
		keys.PUT("/:id", h.Update)
		keys.DELETE("/:id", h.Delete)
		keys.GET("/:id", h.Detail)
	}
}
