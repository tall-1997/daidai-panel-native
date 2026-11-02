package model

// TaskLogDailyStat 是「已经删掉的执行日志」按天留下的计数（#158，v3.3.6）。
//
// 仪表板的今日 / 昨日 / 按天趋势 = task_logs 里现存的行（现算）+ 这张表（已删掉的行）。
// 一行日志要么还在 task_logs、要么已经并进这里且只并一次：并入和删除在同一个事务里完成，
// 唯一入口是 service.DeleteTaskLogs，别处不要直接删 task_logs。
//
// 各列含义：
//   - Day：被删行 created_at 存储文本的前 10 个字符（YYYY-MM-DD，即写入时面板时区的日期），
//     和仪表板现算时「拿带偏移的文本按字典序比较」落到的是同一天，面板时区改过也一样；
//   - Success / Failed / Aborted：被删时 status 为 0 / 1 / 3 的行数；
//   - Other：被删时 status 为 NULL（老数据）或还在运行中的行，只计入「今日执行 / 昨日执行」总数，不进三条折线。
//
// 一天一行、不按任务细分。表名刻意不含 task_logs 子串：#153 的执行计划护栏按这个子串截查询。
type TaskLogDailyStat struct {
	Day     string `gorm:"primaryKey;size:10" json:"day"`
	Success int64  `gorm:"not null;default:0" json:"success"`
	Failed  int64  `gorm:"not null;default:0" json:"failed"`
	Aborted int64  `gorm:"not null;default:0" json:"aborted"`
	Other   int64  `gorm:"not null;default:0" json:"other"`
}

func (TaskLogDailyStat) TableName() string {
	return "task_log_daily_stats"
}

// TaskLogDeleteGuardKey 是 service.DeleteTaskLogs 给自己那条 Delete 打的标记（GORM Statement Settings 的键）。
// testutil.SetupTestEnv 挂了一个测试期 Delete 回调：删 task_logs 却没带这个标记就报错、一行不删，
// 防止以后新增的删除路径忘了先并入计数（#158）。
// 放在 model 包是因为 service 与 testutil 都要引用它：testutil 不能 import service（service 自己的测试也 import testutil，会成环）。
const TaskLogDeleteGuardKey = "daidai:task_logs_delete_archived"
