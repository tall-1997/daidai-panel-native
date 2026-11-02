package handler

import (
	"fmt"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 整桶重编号的步长。写成 (idx+1)*10 是为了留出空档，以后想改成「只改被拖那一条」时有余量；
// 当前实现每次都把整桶重写一遍，简单且不会因为空档用尽出现顺序错乱。
const taskListOrderStep = 10

// 跨桶拖动的提示文案。事务里返回这条错误、外层按它判 400，两处必须是同一个字面量。
const taskSortCrossBucketMessage = "置顶任务与普通任务、启用与禁用任务请分别排序，跨区移动请用置顶 / 启用按钮"

// Sort 处理任务列表的拖拽排序：PUT /tasks/sort。
// 只写 list_order（列表展示顺序），绝不碰 sort_order —— 后者是开机任务的串行执行顺序契约，
// 混用会在用户毫无察觉的情况下改写开机编排。
func (h *TaskHandler) Sort(c *gin.Context) {
	var req struct {
		SourceID uint   `json:"source_id" binding:"required"`
		TargetID *uint  `json:"target_id"`
		Position string `json:"position"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// position 只认 "after"，空串和拼错的值一律按 "before"。
	// 这样前端漏传或传了别的写法时，落点是「插到目标前面」这个更符合直觉的默认，而不是报错。
	insertAfter := strings.EqualFold(strings.TrimSpace(req.Position), "after")

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var source model.Task
		if err := tx.First(&source, req.SourceID).Error; err != nil {
			return fmt.Errorf("源任务不存在")
		}

		// 拖到自己身上（前端抖一下就可能发出来）当没拖过，直接返回，
		// 别为此把整桶白重编号一遍 —— 那会把还没拖过的任务的 list_order 从 0 改成 10/20/30。
		if req.TargetID != nil && *req.TargetID == source.ID {
			return nil
		}

		if req.TargetID != nil {
			var target model.Task
			if err := tx.First(&target, *req.TargetID).Error; err != nil {
				return fmt.Errorf("目标任务不存在")
			}
			// 桶 = 置顶与否 + 状态分组。默认排序里这两层都排在 list_order 前面，
			// 跨桶拖动写进去也会被弹回原区，等于「拖了个寂寞」，所以直接拒绝并告诉用户该按哪个按钮。
			if target.IsPinned != source.IsPinned || taskSortGroup(target.Status) != taskSortGroup(source.Status) {
				return fmt.Errorf("%s", taskSortCrossBucketMessage)
			}
		}

		// 兄弟列表从数据库取【整桶】而不是当前页：跨页拖拽（把第 3 页的任务拖到第 1 页）
		// 只有拿到整桶才能算出正确的插入位置。
		// 状态分组不下推 SQL，是为了直接复用 taskSortGroup 这一份口径 ——
		// 在这里再写一遍 CASE WHEN status IN (...) 就多了一处会各自漂移的判定。
		var siblings []model.Task
		if err := tx.Where("is_pinned = ?", source.IsPinned).
			Order("list_order ASC, sort_order ASC, created_at DESC, id DESC").
			Find(&siblings).Error; err != nil {
			return err
		}

		filtered := make([]model.Task, 0, len(siblings))
		for _, item := range siblings {
			if item.ID == source.ID {
				continue
			}
			if taskSortGroup(item.Status) != taskSortGroup(source.Status) {
				continue
			}
			filtered = append(filtered, item)
		}

		// target_id 省略 = 移到本桶末尾。
		insertIndex := len(filtered)
		if req.TargetID != nil {
			insertIndex = -1
			for idx, item := range filtered {
				if item.ID == *req.TargetID {
					insertIndex = idx
					break
				}
			}
			if insertIndex == -1 {
				return fmt.Errorf("目标任务不存在")
			}
			if insertAfter {
				insertIndex++
			}
		}

		// 用独立的底层数组拼装，避免 append 回写到 filtered 上把后半段覆盖掉。
		ordered := make([]model.Task, 0, len(filtered)+1)
		ordered = append(ordered, filtered[:insertIndex]...)
		ordered = append(ordered, source)
		ordered = append(ordered, filtered[insertIndex:]...)

		// 整桶重编号，只 UPDATE 本桶：其它桶（置顶区、禁用区）的 list_order 保持原值，
		// 没拖过的桶就一直是存量的 0，行为与升级前一致。
		for idx, item := range ordered {
			if err := tx.Model(&model.Task{}).
				Where("id = ?", item.ID).
				Update("list_order", (idx+1)*taskListOrderStep).Error; err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		switch err.Error() {
		case "源任务不存在", "目标任务不存在":
			response.NotFound(c, err.Error())
		case taskSortCrossBucketMessage:
			response.BadRequest(c, err.Error())
		default:
			// 剩下只可能是数据库读写失败，不该把 SQL 错误原文当成用户输入问题回 400。
			response.InternalError(c, "排序更新失败")
		}
		return
	}

	response.Success(c, gin.H{"message": "排序更新成功"})
}
