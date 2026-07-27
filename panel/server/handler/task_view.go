package handler

import (
	"strconv"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *TaskHandler) ListViews(c *gin.Context) {
	var views []model.TaskView
	database.DB.Order("sort_order ASC, id ASC").Find(&views)
	response.Success(c, views)
}

func (h *TaskHandler) CreateView(c *gin.Context) {
	var view model.TaskView
	if err := c.ShouldBindJSON(&view); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	if view.Name == "" {
		response.BadRequest(c, "视图名称不能为空")
		return
	}
	if view.Filters == "" {
		view.Filters = "[]"
	}
	if view.SortRules == "" {
		view.SortRules = "[]"
	}

	// Append new views to the end of the current order unless the caller
	// already specified a value.
	if view.SortOrder == 0 {
		var maxOrder int
		database.DB.Model(&model.TaskView{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maxOrder)
		view.SortOrder = maxOrder + 1
	}

	database.DB.Create(&view)
	response.Success(c, view)
}

type updateTaskViewRequest struct {
	Name      *string `json:"name"`
	Filters   *string `json:"filters"`
	SortRules *string `json:"sort_rules"`
	Hidden    *bool   `json:"hidden"`
	SortOrder *int    `json:"sort_order"`
}

func (h *TaskHandler) UpdateView(c *gin.Context) {
	viewID, _ := strconv.ParseUint(c.Param("viewId"), 10, 32)
	var view model.TaskView
	if err := database.DB.First(&view, viewID).Error; err != nil {
		response.NotFound(c, "视图不存在")
		return
	}

	var input updateTaskViewRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}

	updates := map[string]interface{}{}
	if input.Name != nil && *input.Name != "" {
		updates["name"] = *input.Name
	}
	if input.Filters != nil && *input.Filters != "" {
		updates["filters"] = *input.Filters
	}
	if input.SortRules != nil && *input.SortRules != "" {
		updates["sort_rules"] = *input.SortRules
	}
	if input.Hidden != nil {
		updates["hidden"] = *input.Hidden
	}
	if input.SortOrder != nil {
		updates["sort_order"] = *input.SortOrder
	}

	if len(updates) > 0 {
		database.DB.Model(&view).Updates(updates)
	}
	database.DB.First(&view, viewID)
	response.Success(c, view)
}

func (h *TaskHandler) DeleteView(c *gin.Context) {
	viewID, _ := strconv.ParseUint(c.Param("viewId"), 10, 32)
	var view model.TaskView
	if err := database.DB.First(&view, viewID).Error; err != nil {
		response.NotFound(c, "视图不存在")
		return
	}
	database.DB.Delete(&view)
	response.Success(c, gin.H{"message": "已删除"})
}

type reorderTaskViewItem struct {
	ID        uint  `json:"id" binding:"required"`
	SortOrder int   `json:"sort_order"`
	Hidden    *bool `json:"hidden"`
}

type reorderTaskViewsRequest struct {
	Views []reorderTaskViewItem `json:"views" binding:"required"`
}

// ReorderViews applies bulk sort_order + hidden updates in a single
// transaction. Any id missing from the payload is left untouched so the caller
// can submit either the full list or just a subset.
func (h *TaskHandler) ReorderViews(c *gin.Context) {
	var req reorderTaskViewsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "参数错误")
		return
	}
	if len(req.Views) == 0 {
		response.Success(c, gin.H{"updated": 0})
		return
	}

	updated := 0
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		for _, item := range req.Views {
			if item.ID == 0 {
				continue
			}
			updates := map[string]interface{}{"sort_order": item.SortOrder}
			if item.Hidden != nil {
				updates["hidden"] = *item.Hidden
			}
			res := tx.Model(&model.TaskView{}).Where("id = ?", item.ID).Updates(updates)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				updated++
			}
		}
		return nil
	})
	if err != nil {
		response.InternalError(c, "保存视图顺序失败")
		return
	}

	var views []model.TaskView
	database.DB.Order("sort_order ASC, id ASC").Find(&views)
	response.Success(c, gin.H{"updated": updated, "views": views})
}
