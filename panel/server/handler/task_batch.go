package handler

import (
	"fmt"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func (h *TaskHandler) Batch(c *gin.Context) {
	var req struct {
		IDs    []uint `json:"ids" binding:"required"`
		Action string `json:"action" binding:"required"`

		// 可选开关（#124）：只在 action=delete 时生效，其它 action 带了也忽略。
		// delete_scripts 必须是 JSON bool，传成字符串会让整个请求绑定失败（400，任务也不会被删）。
		// confirm_script_paths 为 nil 表示没传（不收窄）；传了则只删列表里的路径，[] 表示一个都不删。
		DeleteScripts      bool      `json:"delete_scripts"`
		ConfirmScriptPaths *[]string `json:"confirm_script_paths"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 删除前拍快照；不带开关时整段不进入，下面的循环与响应和改动前逐字节一致。
	var scriptCleanup *taskScriptCleanupCtx
	if req.Action == "delete" && req.DeleteScripts {
		var ok bool
		if scriptCleanup, ok = beginTaskScriptCleanup(c, req.IDs, req.ConfirmScriptPaths); !ok {
			return
		}
	}

	scheduler := service.GetSchedulerV2()
	count := 0

	for _, id := range req.IDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}

		switch req.Action {
		case "enable":
			if err := validateAndEnableTask(&task); err != nil {
				continue
			}
		case "disable":
			disableTaskAndRemoveSchedule(&task)
		case "delete":
			if scheduler != nil {
				scheduler.RemoveJob(id)
			}
			// 同单个删除：历史日志先经 DeleteTaskLogs 并入执行趋势计数再删（#158），出错不拦流程。
			_ = database.DB.Transaction(func(tx *gorm.DB) error {
				_, _, err := service.DeleteTaskLogs(tx, "task_id = ?", id)
				return err
			})
			// 同单个删除：任务没了，logs/task_<ID>_* 目录也要一起收走（issue #144 / v3.3.2）。
			service.RemoveTaskLogDirs(id, config.C.Data.LogDir)
			database.DB.Delete(&task)
		case "run":
			if task.Status != model.TaskStatusRunning {
				if err := scheduler.RunNow(id); err == nil {
					count++
				}
				continue
			}
		case "stop":
			if task.Status == model.TaskStatusRunning {
				stopped := false
				if executor := service.GetTaskExecutor(); executor != nil {
					stopped = executor.StopTask(id)
				}
				// #159 修复 B：执行器认领了停止（已对整组发过 TERM、安排了 KILL）就不再按 PID 补刀，
				// 理由同单个停止（task_control.go 的 Stop）；只有执行器不认识这次执行时才按 PID 兜底，同样先 TERM。
				if !stopped && task.PID != nil && *task.PID > 0 {
					// 按 PID 兜底时显式打一次停止标记（幂等）。
					service.MarkManualStop(id)
					service.TerminateProcessByPid(*task.PID)
				}
				stopLogStatus := model.LogStatusAborted
				var runningLog model.TaskLog
				if err := database.DB.Where("task_id = ? AND status = ?", id, model.LogStatusRunning).
					Order("started_at DESC").First(&runningLog).Error; err == nil {
					now := time.Now()
					duration := now.Sub(runningLog.StartedAt).Seconds()
					if duration < 0 {
						duration = 0
					}
					// 批量停止也统一标记为 Aborted，避免进入成功/失败统计。
					database.DB.Model(&runningLog).Updates(map[string]interface{}{
						"status":   stopLogStatus,
						"ended_at": now,
						"duration": duration,
					})
					database.DB.Model(&task).Updates(map[string]interface{}{
						"last_run_status":   model.RunAborted,
						"last_running_time": duration,
					})
				}
				inactiveStatus := service.ResolveTaskInactiveStatus(&task)
				database.DB.Model(&task).Updates(map[string]interface{}{
					"status":          inactiveStatus,
					"last_run_status": model.RunAborted,
					"pid":             gorm.Expr("NULL"),
					"log_path":        gorm.Expr("NULL"),
				})
			} else {
				continue
			}
		case "pin":
			database.DB.Model(&task).Update("is_pinned", true)
		case "unpin":
			database.DB.Model(&task).Update("is_pinned", false)
		}
		count++
	}

	// gin.H 序列化时 key 按字母排序，追加 scripts 不会改变原有字段的字节。
	resp := gin.H{"message": fmt.Sprintf("批量%s: %d 个任务", req.Action, count), "count": count}
	if scriptCleanup != nil {
		resp["scripts"] = finishTaskScriptCleanup(c, scriptCleanup)
	}
	response.Success(c, resp)
}

func (h *TaskHandler) BatchEnable(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		if err := validateAndEnableTask(&task); err != nil {
			continue
		}
		count++
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已启用 %d 个任务", count), "success_count": count})
}

func (h *TaskHandler) BatchDisable(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		disableTaskAndRemoveSchedule(&task)
		count++
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已禁用 %d 个任务", count), "success_count": count})
}

func (h *TaskHandler) BatchDelete(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`

		// 可选开关（#124），语义同 Batch：delete_scripts 必须是 JSON bool；
		// confirm_script_paths 为 nil 不收窄，传了则只删列表里的路径，[] 表示一个都不删。
		DeleteScripts      bool      `json:"delete_scripts"`
		ConfirmScriptPaths *[]string `json:"confirm_script_paths"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 删除前拍快照；不带开关时整段不进入，下面的循环与响应和改动前逐字节一致。
	var scriptCleanup *taskScriptCleanupCtx
	if req.DeleteScripts {
		var ok bool
		if scriptCleanup, ok = beginTaskScriptCleanup(c, req.TaskIDs, req.ConfirmScriptPaths); !ok {
			return
		}
	}

	scheduler := service.GetSchedulerV2()
	count := 0
	for _, id := range req.TaskIDs {
		if scheduler != nil {
			scheduler.RemoveJob(id)
		}
		// 同单个删除：历史日志先经 DeleteTaskLogs 并入执行趋势计数再删（#158），出错不拦流程。
		_ = database.DB.Transaction(func(tx *gorm.DB) error {
			_, _, err := service.DeleteTaskLogs(tx, "task_id = ?", id)
			return err
		})
		// 同单个删除：任务没了，logs/task_<ID>_* 目录也要一起收走（issue #144 / v3.3.2）。
		// 这里连不存在的 id 也会走一遭，listTaskLogDirs 匹配不到目录时是空转，没有副作用。
		service.RemoveTaskLogDirs(id, config.C.Data.LogDir)
		database.DB.Where("id = ?", id).Delete(&model.Task{})
		count++
	}
	// count 仍等于 len(task_ids)（连不存在的 id 也计入，现有怪癖保留）；脚本只针对真实存在的任务处理。
	resp := gin.H{"message": fmt.Sprintf("已删除 %d 个任务", count), "count": count}
	if scriptCleanup != nil {
		resp["scripts"] = finishTaskScriptCleanup(c, scriptCleanup)
	}
	response.Success(c, resp)
}

func (h *TaskHandler) BatchRun(c *gin.Context) {
	var req struct {
		TaskIDs []uint `json:"task_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	if len(req.TaskIDs) > 10 {
		response.BadRequest(c, "批量运行最多 10 个任务")
		return
	}

	scheduler := service.GetSchedulerV2()
	count := 0
	for _, id := range req.TaskIDs {
		var task model.Task
		if database.DB.First(&task, id).Error != nil {
			continue
		}
		if task.Status != model.TaskStatusRunning {
			if scheduler != nil && scheduler.RunNow(id) == nil {
				count++
			}
		}
	}
	response.Success(c, gin.H{"message": fmt.Sprintf("已启动 %d 个任务", count), "count": count})
}

// BatchSetNotify 批量改任务的三个通知开关（issue #149）。
// 只动 notify_on_failure / notify_on_success / notify_on_abort 三列，传了哪项改哪项；
// 通知渠道不在这里改，各任务原来绑定的渠道照旧生效。
// all=true 时忽略 task_ids、改全部任务：网页任务列表只能勾当前页，拿不到全部任务的 id。
//
// 刻意单开一个接口，不往 PUT /tasks/batch 里加 action：那个接口对不认识的 action 什么都不做、照样回 200 并计数，
// 新客户端连老面板时会把「什么都没改」当成功展示。这里老面板没有路由，客户端拿到的是 404。
//
// 不用通知调度器：定时触发与手动运行都会现读任务（scheduler_v2.go 的 AddJob 回调与 RunNow），
// 只有已经入队的那一次仍用入队时的快照，下一次执行就用上新开关，和单个 PUT /tasks/:id 一致。
func (h *TaskHandler) BatchSetNotify(c *gin.Context) {
	var req struct {
		TaskIDs         []uint `json:"task_ids"`
		All             bool   `json:"all"`
		NotifyOnFailure *bool  `json:"notify_on_failure"`
		NotifyOnSuccess *bool  `json:"notify_on_success"`
		NotifyOnAbort   *bool  `json:"notify_on_abort"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 用指针区分「没传」和「传了 false」：false 也是一次有效的修改（批量关闭）。
	updates := map[string]interface{}{}
	if req.NotifyOnFailure != nil {
		updates["notify_on_failure"] = *req.NotifyOnFailure
	}
	if req.NotifyOnSuccess != nil {
		updates["notify_on_success"] = *req.NotifyOnSuccess
	}
	if req.NotifyOnAbort != nil {
		updates["notify_on_abort"] = *req.NotifyOnAbort
	}
	if len(updates) == 0 {
		response.BadRequest(c, "请至少设置一项通知开关")
		return
	}

	query := database.DB.Model(&model.Task{})
	if req.All {
		// GORM 默认拒绝不带条件的整表更新，这里是有意改全部任务（写法同 handler/env.go 清空环境变量）。
		query = query.Where("1 = 1")
	} else {
		if len(req.TaskIDs) == 0 {
			response.BadRequest(c, "请先选择任务")
			return
		}
		query = query.Where("id IN ?", req.TaskIDs)
	}
	// 只改三列布尔值，不用校验 cron、也不用重建调度，一条 UPDATE 就够，不像 Batch 那样逐个 First。
	result := query.Updates(updates)
	if result.Error != nil {
		response.InternalError(c, "批量设置通知失败")
		return
	}
	// RowsAffected 是命中的行数：值本来就相同的任务也算在内，所以它就是「选中 / 全部任务里实际存在的数量」。
	// 一个都没命中时不回 200：批量接口用 200 表达「全军覆没」，客户端会当成成功。
	if result.RowsAffected == 0 {
		response.NotFound(c, "没有找到要修改的任务")
		return
	}
	response.Success(c, gin.H{
		"message":       fmt.Sprintf("已更新 %d 个任务的通知设置", result.RowsAffected),
		"success_count": result.RowsAffected,
	})
}
