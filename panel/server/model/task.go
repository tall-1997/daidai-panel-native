package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	TaskStatusDisabled = 0
	TaskStatusQueued   = 0.5
	TaskStatusEnabled  = 1
	TaskStatusRunning  = 2

	TaskTypeCron    = "cron"
	TaskTypeManual  = "manual"
	TaskTypeStartup = "startup"

	RunSuccess = 0
	RunFailed  = 1
	RunAborted = 2

	DefaultSuccessExitCodes = "0"

	// 任务级「可用环境变量」范围（#168）：all = 注入全部已启用变量（默认，与升级前一致），
	// selected = 只注入命中 EnvGroups / EnvNames 的已启用变量，且脚本令牌只能用来发通知。
	TaskEnvScopeAll      = "all"
	TaskEnvScopeSelected = "selected"
)

type Task struct {
	ID             uint    `gorm:"primarykey" json:"id"`
	Name           string  `gorm:"size:128;not null" json:"name"`
	Command        string  `gorm:"type:text;not null" json:"command"`
	PythonVersion  string  `gorm:"size:16;default:''" json:"python_version"`
	CronExpression string  `gorm:"type:text;not null" json:"cron_expression"`
	TaskType       string  `gorm:"size:16;not null;default:'cron'" json:"task_type"`
	Status         float64 `gorm:"not null" json:"status"`
	// LastStartupAutoRunDate 只记录“开机运行”的自动触发日期，手动运行不受它限制。
	LastStartupAutoRunDate string     `gorm:"size:10;default:''" json:"last_startup_auto_run_date"`
	Labels                 string     `gorm:"size:256;default:''" json:"-"`
	LastRunAt              *time.Time `json:"last_run_at"`
	LastRunStatus          *int       `json:"last_run_status"`
	Timeout                int        `gorm:"default:0" json:"timeout"`
	SuccessExitCodes       string     `gorm:"size:128;not null;default:'0'" json:"success_exit_codes"`
	RandomDelaySeconds     *int       `json:"random_delay_seconds"`
	// LogRetentionDays 任务级日志保留天数：nil = 跟随全局 log_retention_days，>0 = 只保留这么多天。
	// 每分钟跑一次的任务几天就能堆出上万个日志文件，全局天数对它来说太长（issue #144 / v3.3.2）。
	//
	// 🔴 刻意用指针而不是 int + 0 表示「跟随全局」：SQLite 给存量行补列后的取值就是 0/NULL，
	// 一旦用 0 承载「跟随全局」，就和「用户真的填了 0」撞车——而把 0 解读成「保留 0 天」，
	// 升级后第一次清理 tick 就会把全站日志清空。所以补列必须可空、这里必须是 *int。
	LogRetentionDays      *int  `json:"log_retention_days"`
	MaxRetries            int   `json:"max_retries"`
	RetryInterval         int   `json:"retry_interval"`
	NotifyOnFailure       bool  `json:"notify_on_failure"`
	NotifyOnSuccess       bool  `json:"notify_on_success"`
	NotifyOnAbort         bool  `gorm:"default:0" json:"notify_on_abort"`
	NotificationChannelID *uint `gorm:"index" json:"notification_channel_id"`
	DependsOn             *uint `gorm:"index" json:"depends_on"`
	SortOrder             int   `json:"sort_order"`
	// ListOrder 只管任务列表里拖拽出来的展示顺序，刻意不复用上面的 SortOrder：
	// SortOrder 是「开机任务串行执行顺序」的契约（service/scheduler_v2.go 按它排队跑），
	// 拿它承载列表拖拽会在用户毫无察觉的情况下改写开机编排。
	// 补列后存量任务一律为 0，默认排序整体行为与升级前逐字节一致。
	ListOrder int  `gorm:"not null;default:0" json:"list_order"`
	IsPinned  bool `json:"is_pinned"`
	// 这里原有订阅锁字段，#125 起下线：订阅同步从不修改已有任务，锁不再有意义。
	// 库里那一列保留、不再读写（也不 DROP），旧版本回退不受影响。
	// LastSkipAt / LastSkipReason 记录「到点了但这次没执行」的最近一次原因。
	// 目前只有一种成因：上一次执行还没结束、且任务没开「允许多实例」。
	// 刻意不为这种情况建 task_logs 行 —— 它压根没执行，建成日志会混进「已终止」与耗时统计，
	// 还会因为时间更晚顶掉「最近一次日志」，让用户看不到真正的执行输出。
	LastSkipAt             *time.Time `json:"last_skip_at"`
	LastSkipReason         string     `gorm:"type:text;default:''" json:"last_skip_reason"`
	PID                    *int       `gorm:"column:pid" json:"pid"`
	LogPath                *string    `gorm:"size:256" json:"log_path"`
	LastRunningTime        *float64   `json:"last_running_time"`
	TaskBefore             *string    `gorm:"type:text" json:"task_before"`
	TaskAfter              *string    `gorm:"type:text" json:"task_after"`
	AllowMultipleInstances bool       `json:"allow_multiple_instances"`
	// SchedulePolicy 是本机版（Android）扩展：任务到点时的并发策略（skip=跳过/queue=排队/parallel=并行）。上游无此字段，合并时保留。
	SchedulePolicy         string    `gorm:"size:16;not null;default:'skip'" json:"schedule_policy"`
	StopSchedule           string     `gorm:"type:text;default:''" json:"stop_schedule"`
	// 任务级「可用环境变量」（#168）。tag 与 database.go 补列 SQL 逐字一致（见 database-guidelines.md），
	// 否则每次 AutoMigrate 都整表重建 tasks。json 刻意保留字符串形态：备份清单直接序列化 model.Task，
	// 这样原样带上、恢复原样读回；老备份没有这三个键 → 落库默认 all，恢复成「全部」。
	// EnvGroups / EnvNames 都是逗号拼接：分组用 JoinEnvGroups 规范化，变量名受 envNamePattern 约束不含逗号。
	EnvScope  string    `gorm:"size:16;not null;default:'all'" json:"env_scope"`
	EnvGroups string    `gorm:"type:text;default:''" json:"env_groups"`
	EnvNames  string    `gorm:"type:text;default:''" json:"env_names"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Task) TableName() string {
	return "tasks"
}

func (t *Task) ToDict() map[string]interface{} {
	labels := []string{}
	if t.Labels != "" {
		labels = strings.Split(t.Labels, ",")
	}
	cronExpressions := splitTaskCronExpressions(t.CronExpression)

	return map[string]interface{}{
		"id":                       t.ID,
		"name":                     t.Name,
		"command":                  t.Command,
		"python_version":           t.PythonVersion,
		"cron_expression":          t.CronExpression,
		"cron_expressions":         cronExpressions,
		"task_type":                t.GetTaskType(),
		"status":                   t.Status,
		"labels":                   labels,
		"last_run_at":              t.LastRunAt,
		"last_run_status":          t.LastRunStatus,
		"timeout":                  t.Timeout,
		"success_exit_codes":       t.GetSuccessExitCodes(),
		"random_delay_seconds":     t.RandomDelaySeconds,
		"log_retention_days":       t.LogRetentionDays,
		"max_retries":              t.MaxRetries,
		"retry_interval":           t.RetryInterval,
		"notify_on_failure":        t.NotifyOnFailure,
		"notify_on_success":        t.NotifyOnSuccess,
		"notify_on_abort":          t.NotifyOnAbort,
		"notification_channel_id":  t.NotificationChannelID,
		"depends_on":               t.DependsOn,
		"sort_order":               t.SortOrder,
		"list_order":               t.ListOrder,
		"is_pinned":                t.IsPinned,
		"pid":                      t.PID,
		"last_skip_at":             t.LastSkipAt,
		"last_skip_reason":         t.LastSkipReason,
		"log_path":                 t.LogPath,
		"last_running_time":        t.LastRunningTime,
		"task_before":              t.TaskBefore,
		"task_after":               t.TaskAfter,
		"allow_multiple_instances": t.AllowMultipleInstances,
		"stop_schedule":            t.StopSchedule,
		"env_scope":                t.GetEnvScope(),
		"env_groups":               t.GetEnvGroups(),
		"env_names":                t.GetEnvNames(),
		"created_at":               t.CreatedAt,
		"updated_at":               t.UpdatedAt,
	}
}

// NormalizeSuccessExitCodes 统一任务表单、导入文件和旧数据中的成功退出码格式。
// Shell 退出码只接受 0-255；负数保留给超时、信号退出等面板失败状态，不能配置为成功。
func NormalizeSuccessExitCodes(raw string) (string, error) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || unicode.IsSpace(r)
	})
	if len(parts) == 0 {
		return DefaultSuccessExitCodes, nil
	}

	seen := make(map[int]struct{}, len(parts))
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		code, err := strconv.Atoi(part)
		if err != nil || code < 0 || code > 255 {
			return "", fmt.Errorf("成功退出码只能填写 0-255 的整数，多个值请用逗号分隔")
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		normalized = append(normalized, strconv.Itoa(code))
	}
	return strings.Join(normalized, ","), nil
}

func (t *Task) GetSuccessExitCodes() string {
	if t == nil {
		return DefaultSuccessExitCodes
	}
	normalized, err := NormalizeSuccessExitCodes(t.SuccessExitCodes)
	if err != nil {
		// 旧库或手工改库出现异常值时回退到标准退出码 0，不能扩大成功范围。
		return DefaultSuccessExitCodes
	}
	return normalized
}

func (t *Task) IsSuccessExitCode(exitCode int) bool {
	if t == nil || exitCode < 0 {
		return false
	}
	target := strconv.Itoa(exitCode)
	for _, code := range strings.Split(t.GetSuccessExitCodes(), ",") {
		if code == target {
			return true
		}
	}
	return false
}

func splitTaskCronExpressions(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func NormalizeTaskType(taskType string) string {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case "", TaskTypeCron:
		return TaskTypeCron
	case TaskTypeManual:
		return TaskTypeManual
	case TaskTypeStartup:
		return TaskTypeStartup
	default:
		return ""
	}
}

func IsValidTaskType(taskType string) bool {
	return NormalizeTaskType(taskType) != ""
}

func (t *Task) GetTaskType() string {
	if t == nil {
		return TaskTypeCron
	}
	return NormalizeTaskType(t.TaskType)
}

func (t *Task) UsesCronSchedule() bool {
	return t.GetTaskType() == TaskTypeCron
}

func (t *Task) SetLabelsFromSlice(labels []string) {
	t.Labels = strings.Join(labels, ",")
}

func (t *Task) GetLabels() []string {
	if t.Labels == "" {
		return []string{}
	}
	return strings.Split(t.Labels, ",")
}

// GetEnvScope 把库里的取值规范成 all / selected 两态（#168）：
// 只有明确是 selected 才算「指定」，空串、老数据、手工改坏的值一律当 all，注入行为与升级前一致。
func (t *Task) GetEnvScope() string {
	if t != nil && strings.TrimSpace(t.EnvScope) == TaskEnvScopeSelected {
		return TaskEnvScopeSelected
	}
	return TaskEnvScopeAll
}

// GetEnvGroups / GetEnvNames 把逗号拼接的列拆成数组，永远不返回 nil（ToDict 输出 [] 而不是 null）。
func (t *Task) GetEnvGroups() []string {
	if t == nil {
		return []string{}
	}
	return SplitEnvGroups(t.EnvGroups)
}

func (t *Task) GetEnvNames() []string {
	if t == nil {
		return []string{}
	}
	// 变量名不含逗号，复用分组的拆分（去空、去重、去首尾空白）即可
	return SplitEnvGroups(t.EnvNames)
}
