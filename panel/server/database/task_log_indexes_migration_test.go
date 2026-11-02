package database_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"daidai-panel/appboot"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// taskLogQueryIndexColumns 是 issue #153 给 task_logs 补的三个查询索引与列序。
// status 必须排第二列：仪表板既要「今天全部」又要「今天某状态」，(created_at, status) 让两类计数都只读索引；
// 排反成 (status, created_at) 的话，最近 10 条的 ORDER BY created_at DESC 用不上它，又回到全表扫描 + 排序。
var taskLogQueryIndexColumns = map[string][]string{
	"idx_task_logs_created_at_status":  {"created_at", "status"},
	"idx_task_logs_started_at_status":  {"started_at", "status"},
	"idx_task_logs_task_id_started_at": {"task_id", "started_at"},
}

// migrationSQLRecorder 记下迁移期间 GORM 发出的每一条 SQL：logger 的 Trace 对每条语句都会调一次，
// 迁移器内部的 CREATE INDEX、重建表时的建临时表 / 拷数据 / 删表 / 改名也都走这里。
type migrationSQLRecorder struct {
	logger.Interface
	statements []string
}

func (r *migrationSQLRecorder) LogMode(logger.LogLevel) logger.Interface { return r }

func (r *migrationSQLRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

// migrateTaskLogsCapturingWrites 对 task_logs 跑一次 AutoMigrate（生产启动走的同一个迁移器），
// 返回其中所有会改库的语句。迁移器探测表结构发的 SELECT / PRAGMA 不算写，过滤掉。
func migrateTaskLogsCapturingWrites(t *testing.T) []string {
	t.Helper()

	recorder := &migrationSQLRecorder{Interface: logger.Discard}
	if err := database.DB.Session(&gorm.Session{Logger: recorder}).AutoMigrate(&model.TaskLog{}); err != nil {
		t.Fatalf("auto migrate task_logs: %v", err)
	}
	var writes []string
	for _, statement := range recorder.statements {
		head := strings.ToUpper(strings.TrimSpace(statement))
		if strings.HasPrefix(head, "SELECT") || strings.HasPrefix(head, "PRAGMA") {
			continue
		}
		writes = append(writes, statement)
	}
	return writes
}

// snapshotTaskLogRows 按存储原样取出每一行。用 quote() 是为了让时间列保持带偏移的原文、NULL 保持 NULL：
// 扫进 time.Time 再比，驱动解析会把原文上的差异抹平，证明不了「内容一个字节都没动」。
func snapshotTaskLogRows(t *testing.T) []string {
	t.Helper()

	var rows []string
	if err := database.DB.Raw(`SELECT quote(id) || '|' || quote(task_id) || '|' || quote(content) || '|' || quote(status) || '|' ||
		quote(duration) || '|' || quote(log_path) || '|' || quote(started_at) || '|' || quote(ended_at) || '|' ||
		quote(created_at) || '|' || quote(updated_at) FROM task_logs ORDER BY id`).Scan(&rows).Error; err != nil {
		t.Fatalf("snapshot task_logs rows: %v", err)
	}
	return rows
}

// taskLogsRootPage 读 task_logs 这张表在库文件里的根页号。
// glebarez 的迁移器重建表是「建 task_logs__temp → 拷数据 → 删原表 → 改名」，新表的根页必然换了位置
// （建临时表时原表还在，不可能占到原表的根页），所以根页没变就说明还是原来那张表。
func taskLogsRootPage(t *testing.T) int64 {
	t.Helper()

	var rootPage int64
	if err := database.DB.Raw("SELECT rootpage FROM sqlite_master WHERE type = 'table' AND name = 'task_logs'").Scan(&rootPage).Error; err != nil {
		t.Fatalf("read task_logs rootpage: %v", err)
	}
	return rootPage
}

// assertTaskLogQueryIndexes 从库里实读 task_logs 上的索引：除了原有的 idx_task_logs_task_id，
// 必须正好是这三个，而且列序逐一对上。库里的索引是按模型 tag 建出来的，所以这一步同时核对了模型 tag。
func assertTaskLogQueryIndexes(t *testing.T, stage string) {
	t.Helper()

	var names []string
	if err := database.DB.Raw("SELECT name FROM pragma_index_list('task_logs') ORDER BY name").Scan(&names).Error; err != nil {
		t.Fatalf("%s：list task_logs indexes: %v", stage, err)
	}
	want := []string{"idx_task_logs_task_id"}
	for name := range taskLogQueryIndexColumns {
		want = append(want, name)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("%s：task_logs 上的索引应为 %v，实际 %v", stage, want, names)
	}

	for name, wantColumns := range taskLogQueryIndexColumns {
		var columns []string
		if err := database.DB.Raw("SELECT name FROM pragma_index_info(?) ORDER BY seqno", name).Scan(&columns).Error; err != nil {
			t.Fatalf("%s：read columns of %s: %v", stage, name, err)
		}
		if !reflect.DeepEqual(columns, wantColumns) {
			t.Fatalf("%s：索引 %s 的列序应为 %v，实际 %v", stage, name, wantColumns, columns)
		}
	}
}

// TestAutoMigrateAddsTaskLogQueryIndexesToLegacyDatabase 验证老库升级时 AutoMigrate 能补上 #153 的三个索引，
// 而且只补索引：不重建表、不动任何一行，之后每次启动再跑什么都不做。
//
// 老库就是「task_logs 上只有 idx_task_logs_task_id」的库，这里用 DropIndex 摘掉三个新索引来模拟。
// 升级路径出错的代价很大：迁移在 HTTP 服务起来之前同步执行，一旦触发整表重建，
// 日志多的库要把全部日志正文复制一遍，面板会很久起不来。
func TestAutoMigrateAddsTaskLogQueryIndexesToLegacyDatabase(t *testing.T) {
	testutil.SetupTestEnv(t)

	// appboot 启动时按这份清单判断要不要打「正在补建索引」日志，它必须正好是这三个索引。
	// 它与模型 tag 是否一致，下面在真实库上用 appboot 同一组 HasIndex 调用核对。
	if len(appboot.TaskLogQueryIndexNames) != len(taskLogQueryIndexColumns) {
		t.Fatalf("appboot.TaskLogQueryIndexNames 应正好是 %d 个查询索引，实际 %v", len(taskLogQueryIndexColumns), appboot.TaskLogQueryIndexNames)
	}
	for _, name := range appboot.TaskLogQueryIndexNames {
		if _, ok := taskLogQueryIndexColumns[name]; !ok {
			t.Fatalf("appboot.TaskLogQueryIndexNames 里的 %q 不是 #153 的三个查询索引之一", name)
		}
	}

	// 全新库：建表时就带上三个索引。
	assertTaskLogQueryIndexes(t, "全新库")

	task := &model.Task{
		Name:     "索引迁移任务",
		Command:  "task demo.py",
		TaskType: model.TaskTypeManual,
		Status:   model.TaskStatusEnabled,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}

	// 造几行形态各异的日志：正文超过一页（会用到溢出页）、status 有成功 / 失败 / NULL / 运行中、时间带纳秒，
	// 迁移前后逐字节比较，才能说明「只加了索引、没动数据」。
	loc := time.FixedZone("CST", 8*60*60)
	base := time.Date(2026, 9, 29, 21, 54, 39, 309726300, loc)
	success, failed, running := model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusRunning
	duration := 12.5
	logPath := "task_1/20260929-215439.log"
	endedAt := base.Add(time.Minute)
	legacyRows := []*model.TaskLog{
		{TaskID: task.ID, Content: strings.Repeat("eJzLSM3JyVcozy/KSQEAGgQEXQ==", 200), Status: &success, Duration: &duration, LogPath: &logPath, StartedAt: base, EndedAt: &endedAt, CreatedAt: base},
		{TaskID: task.ID, Content: strings.Repeat("eJwrSS0uUcjMSy1KSVUoyU/OBQBGpwbv", 150), Status: &failed, Duration: &duration, LogPath: &logPath, StartedAt: base.Add(time.Hour), EndedAt: &endedAt, CreatedAt: base.Add(time.Hour)},
		{TaskID: task.ID, Content: "", Status: nil, StartedAt: base.Add(2 * time.Hour), CreatedAt: base.Add(2 * time.Hour)},
		{TaskID: task.ID, Status: &running, LogPath: &logPath, StartedAt: base.Add(3 * time.Hour), CreatedAt: base.Add(3 * time.Hour)},
	}
	for _, row := range legacyRows {
		if err := database.DB.Create(row).Error; err != nil {
			t.Fatalf("create task log: %v", err)
		}
	}

	// 模拟老库：摘掉三个新索引，只剩 idx_task_logs_task_id。
	for name := range taskLogQueryIndexColumns {
		if err := database.DB.Migrator().DropIndex(&model.TaskLog{}, name); err != nil {
			t.Fatalf("drop %s to simulate legacy database: %v", name, err)
		}
	}
	// appboot 启动时就是用这组调用判断「老库缺不缺索引」的：模拟出的老库上三个都得判成缺。
	for _, name := range appboot.TaskLogQueryIndexNames {
		if database.DB.Migrator().HasIndex(&model.TaskLog{}, name) {
			t.Fatalf("expected simulated legacy database to have no %s", name)
		}
	}

	rowsBefore := snapshotTaskLogRows(t)
	if len(rowsBefore) != len(legacyRows) {
		t.Fatalf("expected %d task_logs rows before migration, got %d", len(legacyRows), len(rowsBefore))
	}
	rootPageBefore := taskLogsRootPage(t)

	// 升级：只允许发出三条 CREATE INDEX，不能有建表、拷数据、删表、改名。
	writes := migrateTaskLogsCapturingWrites(t)
	if len(writes) != len(taskLogQueryIndexColumns) {
		t.Fatalf("老库升级只应发出 %d 条 CREATE INDEX，实际写语句：%v", len(taskLogQueryIndexColumns), writes)
	}
	for _, statement := range writes {
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(statement)), "CREATE INDEX") {
			t.Fatalf("老库升级只应补建索引，却发出了：%s（全部写语句：%v）", statement, writes)
		}
	}
	assertTaskLogQueryIndexes(t, "老库升级后")
	// appboot 那份清单与模型 tag 一致：升级后按清单逐个问，都得回「已存在」，
	// 否则以后每次启动都会误打「正在为执行日志补建查询索引」。
	for _, name := range appboot.TaskLogQueryIndexNames {
		if !database.DB.Migrator().HasIndex(&model.TaskLog{}, name) {
			t.Fatalf("升级后 HasIndex(%q) 仍为 false：appboot.TaskLogQueryIndexNames 与 model/task_log.go 的 tag 对不上", name)
		}
	}
	if rowsAfter := snapshotTaskLogRows(t); !reflect.DeepEqual(rowsAfter, rowsBefore) {
		t.Fatalf("迁移不应改动任何一行\nbefore=%v\nafter=%v", rowsBefore, rowsAfter)
	}
	if rootPage := taskLogsRootPage(t); rootPage != rootPageBefore {
		t.Fatalf("task_logs 的根页从 %d 变成了 %d：迁移重建了整张表", rootPageBefore, rootPage)
	}

	// 幂等：之后每次启动都会再跑 AutoMigrate，不应再有任何写语句，索引、数据、表都保持原样。
	if writes := migrateTaskLogsCapturingWrites(t); len(writes) != 0 {
		t.Fatalf("索引已齐时再跑 AutoMigrate 不应有任何写语句，实际：%v", writes)
	}
	assertTaskLogQueryIndexes(t, "第二次迁移后")
	if rowsAgain := snapshotTaskLogRows(t); !reflect.DeepEqual(rowsAgain, rowsBefore) {
		t.Fatalf("第二次迁移不应改动任何一行\nbefore=%v\nafter=%v", rowsBefore, rowsAgain)
	}
	if rootPage := taskLogsRootPage(t); rootPage != rootPageBefore {
		t.Fatalf("第二次迁移后 task_logs 的根页从 %d 变成了 %d：重建了整张表", rootPageBefore, rootPage)
	}
}
