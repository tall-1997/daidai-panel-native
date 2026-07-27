package handler

import (
	"log"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type ConfigHandler struct{}

func NewConfigHandler() *ConfigHandler {
	return &ConfigHandler{}
}

func reloadRuntimeConfigKeys(keys ...string) {
	for _, key := range keys {
		switch key {
		case model.PanelTimezoneConfigKey:
			if err := service.ApplyRegisteredPanelTimezone(); err != nil {
				log.Printf("warn: apply panel timezone failed: %v", err)
			}
		case "trusted_proxy_cidrs":
			_ = middleware.ConfigureTrustedProxyCIDRs(model.GetRegisteredConfig(key))
		case "backup_schedule_enabled",
			"backup_schedule_frequency",
			"backup_schedule_time",
			"backup_schedule_weekday",
			"backup_schedule_monthday",
			"backup_schedule_name",
			"backup_schedule_password",
			"backup_schedule_selection":
			service.ReloadBackupScheduler()
		}
	}
}

func buildConfigResponseItem(cfg *model.SystemConfig, def *model.SystemConfigDefinition) gin.H {
	item := gin.H{
		"registered": false,
		"updated_at": nil,
	}

	if cfg != nil {
		item["value"] = cfg.Value
		item["description"] = cfg.Description
		item["updated_at"] = cfg.UpdatedAt
	} else {
		item["value"] = ""
		item["description"] = ""
	}

	if def != nil {
		item["registered"] = true
		item["default_value"] = def.DefaultValue
		item["value_type"] = def.ValueType
		item["group"] = def.Group
		item["description"] = def.Description
		if cfg == nil || cfg.Value == "" {
			item["value"] = def.DefaultValue
		}
		if len(def.Options) > 0 {
			item["options"] = def.Options
		}
	}

	return item
}

func (h *ConfigHandler) List(c *gin.Context) {
	var configs []model.SystemConfig
	database.DB.Order("key ASC").Find(&configs)

	configMap := make(map[string]model.SystemConfig, len(configs))
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg
	}

	data := make(map[string]interface{})
	for _, def := range model.SystemConfigDefinitions() {
		cfg, exists := configMap[def.Key]
		if exists {
			cfgCopy := cfg
			data[def.Key] = buildConfigResponseItem(&cfgCopy, &def)
			delete(configMap, def.Key)
			continue
		}
		defCopy := def
		data[def.Key] = buildConfigResponseItem(nil, &defCopy)
	}

	for key, cfg := range configMap {
		cfgCopy := cfg
		data[key] = buildConfigResponseItem(&cfgCopy, nil)
	}

	response.Success(c, gin.H{"data": data})
}

func (h *ConfigHandler) Get(c *gin.Context) {
	key := c.Param("key")

	var cfg model.SystemConfig
	cfgExists := database.DB.Where("`key` = ?", key).First(&cfg).Error == nil

	if def, exists := model.GetSystemConfigDefinition(key); exists {
		var cfgPtr *model.SystemConfig
		if cfgExists {
			cfgPtr = &cfg
		}
		item := buildConfigResponseItem(cfgPtr, &def)
		response.Success(c, gin.H{"data": gin.H{
			"key":    key,
			"value":  item["value"],
			"config": item,
		}})
		return
	}

	if !cfgExists {
		response.NotFound(c, "配置不存在")
		return
	}

	item := buildConfigResponseItem(&cfg, nil)
	response.Success(c, gin.H{"data": gin.H{
		"key":    key,
		"value":  item["value"],
		"config": item,
	}})
}

func (h *ConfigHandler) Set(c *gin.Context) {
	var req struct {
		Key         string `json:"key" binding:"required"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	if err := model.SetConfig(req.Key, req.Value); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	reloadRuntimeConfigKeys(req.Key)

	var cfg model.SystemConfig
	if err := database.DB.Where("`key` = ?", req.Key).First(&cfg).Error; err == nil && req.Description != "" {
		database.DB.Model(&cfg).Update("description", req.Description)
	}

	response.Success(c, gin.H{"message": "配置已更新"})
}

func (h *ConfigHandler) BatchSet(c *gin.Context) {
	var req struct {
		Configs map[string]string `json:"configs" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	for key, value := range req.Configs {
		if err := model.SetConfig(key, value); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}
	for key := range req.Configs {
		reloadRuntimeConfigKeys(key)
	}

	response.Success(c, gin.H{"message": "配置已更新"})
}

func (h *ConfigHandler) Delete(c *gin.Context) {
	key := c.Param("key")
	database.DB.Where("`key` = ?", key).Delete(&model.SystemConfig{})
	response.Success(c, gin.H{"message": "配置已删除"})
}

func (h *ConfigHandler) RegisterRoutes(r *gin.RouterGroup) {
	cfgs := r.Group("/configs", middleware.JWTAuth(), middleware.RequireAdmin())
	{
		cfgs.GET("", h.List)
		cfgs.GET("/:key", h.Get)
		cfgs.POST("", h.Set)
		cfgs.PUT("/batch", h.BatchSet)
		cfgs.DELETE("/:key", h.Delete)
	}
}
