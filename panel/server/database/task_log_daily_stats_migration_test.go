package database_test

import (
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestTaskLogDailyStatsAutoMigrateDoesNotRebuildTable 守住 #158 新表 task_log_daily_stats 的迁移稳定性：
// AutoMigrate 跑在每次启动和每条 ddp 命令里，表结构与 model tag 只要有一点对不上（比如 default 写法不一致），
// 就会每次都整表重建（建 __temp → 拷数据 → 删表 → 改名），见 database-guidelines.md 的 #156 那一节。
//
// SetupTestEnv 之后表就该在：证明 testutil 那份建表清单登记了它（漏登记时只有别的包碰到这张表才会炸）。
func TestTaskLogDailyStatsAutoMigrateDoesNotRebuildTable(t *testing.T) {
	testutil.SetupTestEnv(t)

	if !database.DB.Migrator().HasTable(&model.TaskLogDailyStat{}) {
		t.Fatal("SetupTestEnv 之后应已有 task_log_daily_stats：testutil.SetupTestEnv 的建表清单要登记 model.TaskLogDailyStat")
	}

	var ddl string
	if err := database.DB.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'task_log_daily_stats'").Scan(&ddl).Error; err != nil {
		t.Fatalf("read task_log_daily_stats DDL: %v", err)
	}
	// 主键是 day（一天一行，归档靠 ON CONFLICT(day) 累加）；四个计数列都是 NOT NULL DEFAULT 0。
	if !strings.Contains(ddl, "PRIMARY KEY (`day`)") || !strings.Contains(ddl, "NOT NULL DEFAULT 0") {
		t.Fatalf("task_log_daily_stats 的建表语句应含 PRIMARY KEY (`day`) 与 NOT NULL DEFAULT 0，实际：%s", ddl)
	}

	// 再连跑两次 AutoMigrate（相当于又启动两次）：除了探测表结构的 SELECT / PRAGMA，不该有任何写语句，更不能出现 __temp 重建。
	for round := 1; round <= 2; round++ {
		recorder := &migrationSQLRecorder{Interface: logger.Discard}
		if err := database.DB.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&model.TaskLogDailyStat{}); err != nil {
			t.Fatalf("第 %d 次 AutoMigrate task_log_daily_stats: %v", round, err)
		}
		var writes []string
		for _, statement := range recorder.statements {
			if strings.Contains(statement, "task_log_daily_stats__temp") {
				t.Fatalf("第 %d 次 AutoMigrate 整表重建了 task_log_daily_stats：%s（全部语句：%v）", round, statement, recorder.statements)
			}
			head := strings.ToUpper(strings.TrimSpace(statement))
			if strings.HasPrefix(head, "SELECT") || strings.HasPrefix(head, "PRAGMA") {
				continue
			}
			writes = append(writes, statement)
		}
		if len(writes) != 0 {
			t.Fatalf("第 %d 次 AutoMigrate 不应有任何写语句，实际：%v", round, writes)
		}
	}
}
