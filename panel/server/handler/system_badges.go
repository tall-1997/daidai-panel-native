package handler

import (
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

// Badges 汇总侧栏菜单角标需要的计数，供前端定时轮询。
//
// 【为什么单独开一个接口，而不是复用 /system/dashboard】
// dashboard 那个接口一次要跑 15+ 条查询、还要 Preload 最近 10 条日志、按 range 天数
// 循环查 7~90 组日统计，是为「进仪表板页看一眼」设计的。角标是全站每隔几十秒轮询一次，
// 复用它等于把最重的查询挂到最高频的调用上。这里刻意只做 COUNT，不取实体、不 Preload。
//
// 【为什么没有「有新版本」这一项】
// 检查更新走 fetchLatestPanelRelease()，每次都是一发真实的 GitHub 外网请求，没有缓存
// （见 CheckUpdate）。把它放进轮询接口，等于每个在线会话每隔几十秒替面板打一次外网，
// 既拖慢角标响应，也可能触发 GitHub 的匿名限流，把「检查更新」这个真正重要的功能拖垮。
// 前端的「有新版本」角标改为复用设置页/概览页已经发生的 check-update 调用结果，
// 由前端 store 缓存，本接口不参与。
//
// 【按角色裁剪】
// 路由挂的是 RequireRole("viewer")，但侧栏菜单本身是按角色过滤的：订阅/环境变量/脚本
// 要 operator，依赖/配置文件/管理后台要 admin。对看不到那些菜单的用户，这里返回的计数
// 没有任何落点，反而让低权限账号能从数字推断出无权模块的运行状态，所以统一置 0。
func (h *SystemHandler) Badges(c *gin.Context) {
	role := c.GetString("role")
	level := badgeRoleLevel(role)

	data := gin.H{
		"tasks_running":     int64(0),
		"logs_failed_today": int64(0),
		"subs_failed":       int64(0),
		"deps_failed":       int64(0),
		"deps_installing":   int64(0),
	}

	// viewer 及以上：定时任务与执行日志
	if level >= badgeRoleViewer {
		var tasksRunning int64
		database.DB.Model(&model.Task{}).
			Where("status = ?", model.TaskStatusRunning).
			Count(&tasksRunning)
		data["tasks_running"] = tasksRunning

		// 与 Dashboard 的「今日」口径保持一致：本地时区的自然日零点起。
		// 一致的只是时间边界：角标只数现存的日志，不加 #158 归档进 task_log_daily_stats 的计数
		// （角标点进去要能看到那几条日志），所以两边的数字可能不同。
		now := time.Now()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		var logsFailedToday int64
		database.DB.Model(&model.TaskLog{}).
			Where("created_at >= ? AND status = ?", today, model.LogStatusFailed).
			Count(&logsFailedToday)
		data["logs_failed_today"] = logsFailedToday
	}

	// operator 及以上：订阅管理
	if level >= badgeRoleOperator {
		// subscriptions.status 由 service.pullSubscription 在每次拉取结束时写入：
		// 0 = 成功，1 = 失败（见 server/service/subscription.go 的 status 赋值）。
		var subsFailed int64
		database.DB.Model(&model.Subscription{}).
			Where("status = ?", 1).
			Count(&subsFailed)
		data["subs_failed"] = subsFailed
	}

	// admin：依赖管理
	if level >= badgeRoleAdmin {
		var depsFailed int64
		database.DB.Model(&model.Dependency{}).
			Where("status = ?", model.DepStatusFailed).
			Count(&depsFailed)
		data["deps_failed"] = depsFailed

		// 安装中/排队中/卸载中合并成一个「正在忙」的计数：角标只需要表达
		// 「这个页面此刻有事情在跑」，区分三种中间态对侧栏没有意义。
		var depsInstalling int64
		database.DB.Model(&model.Dependency{}).
			Where("status IN ?", []string{
				model.DepStatusQueued,
				model.DepStatusInstalling,
				model.DepStatusRemoving,
			}).
			Count(&depsInstalling)
		data["deps_installing"] = depsInstalling
	}

	response.Success(c, gin.H{"data": data})
}

const (
	badgeRoleNone = iota
	badgeRoleViewer
	badgeRoleOperator
	badgeRoleAdmin
)

// badgeRoleLevel 把角色名映射成可比较的等级。
// 与 middleware.RequireRole 内部那张表同构，但那张表没有导出，
// 而这里只需要「够不够 operator/admin」这一个判断，不值得为它改动中间件的公开面。
func badgeRoleLevel(role string) int {
	switch role {
	case "admin":
		return badgeRoleAdmin
	case "operator":
		return badgeRoleOperator
	case "viewer":
		return badgeRoleViewer
	default:
		return badgeRoleNone
	}
}
