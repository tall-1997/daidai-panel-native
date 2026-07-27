package handler

import (
	"strconv"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

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

	if err := database.DB.Create(&key).Error; err != nil {
		response.InternalError(c, "创建 SSH 密钥失败")
		return
	}

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
		updates["private_key"] = req.PrivateKey
	}

	if len(updates) > 0 {
		database.DB.Model(&key).Updates(updates)
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
