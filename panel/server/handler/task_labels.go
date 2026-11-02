package handler

import (
	"fmt"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

// ListLabels 列出全部任务用过的自定义标签及各自的任务数（#157 契约 L1），
// 给网页任务表单与「批量添加标签」弹窗的「已有标签」候选用。
//
// 查询与口径都在 service.ListTaskLabelCounts（trim、跳过内部前缀、同任务去重、字节序升序），这里只管调用与响应：
// 返回裸数组 [{name, count}]，一个标签都没有时是 []，与 GET /tasks/groups 同形。
func (h *TaskHandler) ListLabels(c *gin.Context) {
	labels, err := service.ListTaskLabelCounts()
	if err != nil {
		response.InternalError(c, "加载任务标签失败")
		return
	}
	response.Success(c, labels)
}

// BatchAddLabels 批量给任务追加标签。
// 语义为「追加」：保留任务原有全部标签（含 分组:/subscription: 等内部标签），
// 只把新标签并进去并去重，不删除任何原标签。
// 带内部前缀（分组: / subscription:）的输入一律忽略，避免用户注入保留标签。
func (h *TaskHandler) BatchAddLabels(c *gin.Context) {
	var req struct {
		TaskIDs []uint   `json:"task_ids" binding:"required"`
		Labels  []string `json:"labels" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 清洗待追加的标签：trim、跳过空、忽略内部前缀、去重。
	newLabels := sanitizeIncomingLabels(req.Labels)
	if len(newLabels) == 0 {
		response.BadRequest(c, "没有可添加的有效标签")
		return
	}

	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}

		merged := mergeLabels(task.GetLabels(), newLabels)
		task.SetLabelsFromSlice(merged)
		if database.DB.Model(&task).Update("labels", task.Labels).Error != nil {
			continue
		}
		count++
	}

	response.Success(c, gin.H{
		"message":       fmt.Sprintf("已为 %d 个任务添加标签", count),
		"success_count": count,
	})
}

// sanitizeIncomingLabels 清洗用户输入的待追加标签：
// trim 空白、跳过空串、忽略带内部前缀的输入、去重（保持输入顺序）。
func sanitizeIncomingLabels(labels []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		// 与 GET /tasks/labels 的候选共用同一个判定，两边口径不会漂开。
		if service.IsInternalTaskLabel(label) {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	return result
}

// mergeLabels 把 newLabels 追加进 existing，保留 existing 原有全部标签（含内部标签），去重。
func mergeLabels(existing, newLabels []string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(existing)+len(newLabels))
	for _, raw := range existing {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	for _, label := range newLabels {
		if _, ok := seen[label]; ok {
			continue
		}
		seen[label] = struct{}{}
		result = append(result, label)
	}
	return result
}
