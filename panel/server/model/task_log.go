package model

import (
	"time"
)

const (
	LogStatusSuccess = 0
	LogStatusFailed  = 1
	LogStatusRunning = 2
	LogStatusAborted = 3
)

// TaskLog 是一次执行的记录。
//
// 【为什么有三个查询索引（issue #153，v3.3.4）】
// content（整段执行日志，压缩后再 base64）在表里排在 status / started_at / created_at 前面，
// 一行常常超过一页（约 4 KB），超出的部分放在溢出页链表上。按这几列过滤时如果不走索引，
// SQLite 要顺着每一行的溢出页一页页读到这几列，等于把整库日志正文读一遍。
// 仪表板一次请求要这样扫 29 次（7 天视图）/ 98 次（30 天视图），这就是 #153「仪表板很慢、偶尔超时报失败」的根因。
// 所以补了三个复合索引，status 放第二列，让「某天 + 某状态」的计数只读索引、不用回表：
//   - idx_task_logs_created_at_status  (created_at, status)：仪表板今日 / 昨日 / 按天计数与最近 10 条、侧栏角标、日志页按日期筛
//   - idx_task_logs_started_at_status  (started_at, status)：日志列表默认页与 5 秒自动刷新、自动清理、Stats 按状态计数
//   - idx_task_logs_task_id_started_at (task_id, started_at)：latest-log、停止任务与中断收口时找运行中的日志、按任务筛选与清理
//
// 旧的 idx_task_logs_task_id 是第三个索引的前缀，功能上多余，但刻意保留：删它要改 TaskID 的 tag，
// 而 GORM 不会删老库里已有的索引，新老库的结构就对不上了；无筛选的 count(*) 也正好用它做覆盖扫描。
// 三个索引名在 appboot.TaskLogQueryIndexNames 里还有一份（启动时据此判断要不要打「补建索引」日志），
// 改名要两边一起改，database/task_log_indexes_migration_test.go 兜底。
//
// 新增按 created_at / started_at / status 过滤或排序的查询时，要看一眼 EXPLAIN QUERY PLAN 确认用上了索引。
// 不能对这几列包 date() / strftime() 之类的函数：包了索引就失效；而且库里存的是带偏移的文本
// （如 2026-09-29 21:54:39.3+08:00），date() 会先换算成 UTC，东八区每天 0～8 点的执行会被算到前一天。
type TaskLog struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	TaskID    uint       `gorm:"index;index:idx_task_logs_task_id_started_at,priority:1;not null" json:"task_id"`
	Content   string     `gorm:"type:text;default:''" json:"content"`
	Status    *int       `gorm:"index:idx_task_logs_created_at_status,priority:2;index:idx_task_logs_started_at_status,priority:2" json:"status"`
	Duration  *float64   `json:"duration"`
	LogPath   *string    `gorm:"size:256" json:"log_path"`
	StartedAt time.Time  `gorm:"index:idx_task_logs_started_at_status,priority:1;index:idx_task_logs_task_id_started_at,priority:2" json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `gorm:"index:idx_task_logs_created_at_status,priority:1" json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	// --- 本机版（Android）扩展：日志游标（SSE 断线续传用） ---
	LogCursor int64      `gorm:"default:0" json:"log_cursor"`
	Task      *Task      `gorm:"foreignKey:TaskID" json:"-"`
}

func (TaskLog) TableName() string {
	return "task_logs"
}

func (l *TaskLog) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":         l.ID,
		"task_id":    l.TaskID,
		"content":    l.Content,
		"status":     l.Status,
		"duration":   l.Duration,
		"log_path":   l.LogPath,
		"started_at": l.StartedAt,
		"ended_at":   l.EndedAt,
		"created_at": l.CreatedAt,
		"updated_at": l.UpdatedAt,
	}
	if l.Task != nil {
		result["task_name"] = l.Task.Name
		// APP 的日志详情页要用它反推「这条日志跑的是哪个脚本」，再跳到脚本编辑页
		// （Dumb-Panel-APP issue #5）。面板没有 GET /tasks/:id，日志正文和 log_path
		// 里也都没有脚本路径，所以只能从这里下发。
		result["command"] = l.Task.Command
		result["task_type"] = l.Task.GetTaskType()
		result["labels"] = l.Task.GetLabels()
		result["task"] = map[string]interface{}{
			"task_type": l.Task.GetTaskType(),
			"labels":    l.Task.GetLabels(),
		}
	}
	return result
}
