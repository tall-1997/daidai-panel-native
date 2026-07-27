package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

func recordScriptVersion(scriptPath, content, message string) int {
	var maxVersion int
	database.DB.Model(&model.ScriptVersion{}).
		Where("script_path = ?", scriptPath).
		Select("COALESCE(MAX(version), 0)").
		Scan(&maxVersion)

	newVersion := maxVersion + 1
	if message == "" {
		message = fmt.Sprintf("v%d", newVersion)
	}

	sv := model.ScriptVersion{
		ScriptPath: scriptPath,
		Content:    content,
		Version:    newVersion,
		Message:    message,
	}
	database.DB.Create(&sv)

	return newVersion
}

func (h *ScriptHandler) ListVersions(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		response.BadRequest(c, "路径不能为空")
		return
	}

	var versions []model.ScriptVersion
	database.DB.Where("script_path = ?", path).
		Order("version DESC").Limit(50).Find(&versions)

	data := make([]map[string]interface{}, len(versions))
	for i, v := range versions {
		data[i] = v.ToDict()
	}

	response.Success(c, gin.H{"data": data})
}

func (h *ScriptHandler) ClearVersions(c *gin.Context) {
	path, err := normalizeScriptRelativePath(c.Query("path"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	result := database.DB.Where("script_path = ?", path).Delete(&model.ScriptVersion{})
	if result.Error != nil {
		response.InternalError(c, "清空版本历史失败")
		return
	}

	response.Success(c, gin.H{
		"message":       "版本历史已清空",
		"cleared_count": result.RowsAffected,
	})
}

func (h *ScriptHandler) GetVersion(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var version model.ScriptVersion
	if err := database.DB.First(&version, id).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	response.Success(c, gin.H{"data": version.ToDictWithContent()})
}

func (h *ScriptHandler) Rollback(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var version model.ScriptVersion
	if err := database.DB.First(&version, id).Error; err != nil {
		response.NotFound(c, "版本不存在")
		return
	}

	full, err := safePath(version.ScriptPath, false)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	os.MkdirAll(filepath.Dir(full), 0755)
	if err := os.WriteFile(full, []byte(version.Content), 0644); err != nil {
		response.InternalError(c, "写入文件失败")
		return
	}

	newVersion := recordScriptVersion(
		version.ScriptPath,
		version.Content,
		fmt.Sprintf("回滚到 v%d", version.Version),
	)

	response.Success(c, gin.H{
		"message": fmt.Sprintf("已回滚到 v%d", version.Version),
		"version": newVersion,
	})
}
