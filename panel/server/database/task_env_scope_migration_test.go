package database_test

import (
	"reflect"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// taskEnvScopeColumns 是 #168 给 tasks 加的三列。
var taskEnvScopeColumns = []string{"env_scope", "env_groups", "env_names"}

// migrateTasksCapturingStatements 对 tasks 跑一次 AutoMigrate（生产启动、每条 ddp 命令走的同一个迁移器），
// 返回期间发出的全部 SQL，以及其中会改库的那部分（探测表结构的 SELECT / PRAGMA 不算写）。
func migrateTasksCapturingStatements(t *testing.T) (all []string, writes []string) {
	t.Helper()

	recorder := &migrationSQLRecorder{Interface: logger.Discard}
	if err := database.DB.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&model.Task{}); err != nil {
		t.Fatalf("auto migrate tasks: %v", err)
	}
	for _, statement := range recorder.statements {
		head := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(head, "SELECT") || strings.HasPrefix(head, "PRAGMA") {
			continue
		}
		writes = append(writes, statement)
	}
	return recorder.statements, writes
}

// assertTasksNotRebuilt：glebarez 的迁移器重建表就是「建 tasks__temp → 拷数据 → 删原表 → 改名」。
func assertTasksNotRebuilt(t *testing.T, stage string, all, writes []string) {
	t.Helper()

	for _, statement := range all {
		if strings.Contains(statement, "tasks__temp") {
			t.Fatalf("%s：AutoMigrate 不应整表重建 tasks，却发出了：%s", stage, statement)
		}
	}
	if len(writes) != 0 {
		t.Fatalf("%s：AutoMigrate 不应有任何写语句，实际：%v", stage, writes)
	}
}

// snapshotTaskRows 按存储原样取出每一行的几列（quote() 保住 NULL 与原文），用来证明迁移没动存量数据。
func snapshotTaskRows(t *testing.T) []string {
	t.Helper()

	var rows []string
	if err := database.DB.Raw(`SELECT quote(id) || '|' || quote(name) || '|' || quote(command) || '|' || quote(cron_expression) || '|' ||
		quote(task_type) || '|' || quote(status) || '|' || quote(labels) || '|' || quote(created_at) || '|' || quote(updated_at)
		FROM tasks ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("snapshot tasks rows: %v", err)
	}
	return rows
}

func tasksRootPage(t *testing.T) int64 {
	t.Helper()

	var rootPage int64
	if err := database.DB.Raw("SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'tasks'").Scan(&rootPage).Error; err != nil {
		t.Fatalf("read tasks rootpage: %v", err)
	}
	return rootPage
}

// #168：model.Task 三列的 tag 默认值与 GORM 写进 DDL 的值一致，之后每次 AutoMigrate（每次启动、每条 ddp 命令）都不整表重建 tasks。
func TestTasksAutoMigrateDoesNotRebuildTable(t *testing.T) {
	testutil.SetupTestEnv(t)

	for i := 0; i < 2; i++ {
		all, writes := migrateTasksCapturingStatements(t)
		assertTasksNotRebuilt(t, "SetupTestEnv 之后再次迁移", all, writes)
	}
}

// #168：v3.3.9 老库升级只给 tasks 补三列（3 条 ALTER，不整表重建），存量任务一律落「全部」，之后再启动 0 写。
func TestTasksEnvScopeColumnsUpgradeAddsColumnsWithoutRebuild(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 存量任务趁列还在时用 model 建（建完再删列，存量行就是「升级前没有这三列」的样子）
	for _, name := range []string{"老任务A", "老任务B"} {
		task := &model.Task{Name: name, Command: "task demo.py", CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
		task.SetLabelsFromSlice([]string{"分组:日常"})
		if err := database.DB.Create(task).Error; err != nil {
			t.Fatalf("create task %s: %v", name, err)
		}
	}
	// 造 v3.3.9 原样的 tasks：SQLite 原生 DROP COLUMN 只摘掉这三列的定义，其余 GORM 建出来的 DDL 原样保留
	for _, column := range taskEnvScopeColumns {
		if err := database.DB.Exec("ALTER TABLE tasks DROP COLUMN " + column).Error; err != nil {
			t.Fatalf("drop %s to simulate v3.3.9 tasks table: %v", column, err)
		}
	}
	rowsBefore := snapshotTaskRows(t)
	rootPageBefore := tasksRootPage(t)

	all, writes := migrateTasksCapturingStatements(t)
	for _, statement := range all {
		if strings.Contains(statement, "tasks__temp") {
			t.Fatalf("升级不应整表重建 tasks，却发出了：%s", statement)
		}
	}
	if len(writes) != len(taskEnvScopeColumns) {
		t.Fatalf("升级应当只发 %d 条补列的 ALTER，实际：%v", len(taskEnvScopeColumns), writes)
	}
	for _, column := range taskEnvScopeColumns {
		found := false
		for _, statement := range writes {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "ALTER TABLE") && strings.Contains(statement, "ADD `"+column+"`") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("升级应当补上 %s 列，实际写语句：%v", column, writes)
		}
	}

	// 生产启动顺序：AutoMigrate → EnsureColumns（列已在，跳过）；之后再启动一次不该再写库。
	database.EnsureColumns()
	all, writes = migrateTasksCapturingStatements(t)
	assertTasksNotRebuilt(t, "补列之后的下一次启动", all, writes)

	if rowsAfter := snapshotTaskRows(t); !reflect.DeepEqual(rowsAfter, rowsBefore) {
		t.Fatalf("升级不应改动存量数据\nbefore=%v\nafter=%v", rowsBefore, rowsAfter)
	}
	if rootPage := tasksRootPage(t); rootPage != rootPageBefore {
		t.Fatalf("tasks 的根页从 %d 变成了 %d：升级重建了整张表", rootPageBefore, rootPage)
	}

	// 存量任务补列后一律是「全部」、两个列表为空：升级后每个任务的运行环境与升级前一致。
	var tasks []model.Task
	if err := database.DB.Order("id ASC").Find(&tasks).Error; err != nil {
		t.Fatalf("load migrated tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 migrated tasks, got %d", len(tasks))
	}
	for _, task := range tasks {
		if task.EnvScope != model.TaskEnvScopeAll || task.EnvGroups != "" || task.EnvNames != "" {
			t.Fatalf("存量任务 %s 补列后应为 all / 空 / 空，实际 %q / %q / %q", task.Name, task.EnvScope, task.EnvGroups, task.EnvNames)
		}
		dict := task.ToDict()
		if dict["env_scope"] != "all" {
			t.Fatalf("ToDict 的 env_scope 应为 all，实际 %#v", dict["env_scope"])
		}
		if groups, ok := dict["env_groups"].([]string); !ok || groups == nil || len(groups) != 0 {
			t.Fatalf("ToDict 的 env_groups 应为空数组（不是 null），实际 %#v", dict["env_groups"])
		}
		if names, ok := dict["env_names"].([]string); !ok || names == nil || len(names) != 0 {
			t.Fatalf("ToDict 的 env_names 应为空数组（不是 null），实际 %#v", dict["env_names"])
		}
	}
}
