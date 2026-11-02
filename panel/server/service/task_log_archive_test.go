package service

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件覆盖 #158 的 service.DeleteTaskLogs（删 task_logs 的唯一入口：先按天并进 task_log_daily_stats 再删）、
// 订阅自动删失效任务（D8）的计数，以及两道护栏（源码语法扫描 + testutil 的测试期 Delete 回调）。

// archivedDailyStats 按 day 升序取出 task_log_daily_stats 的全部行（backup_task_log_daily_stats_test.go 也用它）。
func archivedDailyStats(t *testing.T) []model.TaskLogDailyStat {
	t.Helper()
	var rows []model.TaskLogDailyStat
	if err := database.DB.Order("day ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load task_log_daily_stats: %v", err)
	}
	return rows
}

// deleteTaskLogsInTx 开一个事务调 DeleteTaskLogs，与生产调用点同一个形状。
func deleteTaskLogsInTx(t *testing.T, where string, args ...interface{}) (int64, []string, error) {
	t.Helper()
	var deleted int64
	var paths []string
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		deleted, paths, err = DeleteTaskLogs(tx, where, args...)
		return err
	})
	return deleted, paths, err
}

// archiveCreateLog 造一行日志，started_at 与 created_at 都取 at（归档按 created_at 的存储日期分桶）。
func archiveCreateLog(t *testing.T, taskID uint, at time.Time, status *int, logPath *string) uint {
	t.Helper()
	entry := &model.TaskLog{TaskID: taskID, Status: status, LogPath: logPath, StartedAt: at, CreatedAt: at}
	if err := database.DB.Create(entry).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	return entry.ID
}

// S1：按存储日期分桶、按状态分列（运行中与 NULL 进 other）；返回删除行数与非空的 log_path；
// 同一天再删一批走 ON CONFLICT 累加；一行都没命中时不插计数行。
func TestDeleteTaskLogsArchivesEachStatusIntoItsStoredDay(t *testing.T) {
	testutil.SetupTestEnv(t)
	taskID := createTaskForLog(t)

	success, failed, running, aborted := model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusRunning, model.LogStatusAborted
	cst := time.FixedZone("CST", 8*60*60)
	// 两天各一行 0 / 1 / 2 / 3 / NULL；部分带 log_path，另有一行是空串 log_path（老数据），只有非空的才会被取回。
	pathA, pathB, emptyPath := "task_1_demo/a.log", "task_1_demo/b.log", ""
	rows := []struct {
		at     time.Time
		status *int
		path   *string
	}{
		{time.Date(2026, 10, 1, 0, 5, 0, 0, cst), &success, &pathA},
		{time.Date(2026, 10, 1, 9, 0, 0, 0, cst), &failed, nil},
		{time.Date(2026, 10, 1, 12, 0, 0, 0, cst), &running, &emptyPath},
		{time.Date(2026, 10, 1, 18, 0, 0, 0, cst), &aborted, nil},
		{time.Date(2026, 10, 1, 23, 59, 0, 0, cst), nil, &pathB},
		{time.Date(2026, 10, 2, 7, 30, 0, 0, cst), &success, nil},
		{time.Date(2026, 10, 2, 8, 0, 0, 0, cst), &failed, nil},
		{time.Date(2026, 10, 2, 9, 0, 0, 0, cst), &running, nil},
		{time.Date(2026, 10, 2, 10, 0, 0, 0, cst), &aborted, nil},
		{time.Date(2026, 10, 2, 11, 0, 0, 0, cst), nil, nil},
	}
	for _, row := range rows {
		archiveCreateLog(t, taskID, row.at, row.status, row.path)
	}

	deleted, paths, err := deleteTaskLogsInTx(t, "task_id = ?", taskID)
	if err != nil {
		t.Fatalf("delete task logs: %v", err)
	}
	if deleted != int64(len(rows)) {
		t.Fatalf("应删掉 %d 行，实际 %d 行", len(rows), deleted)
	}
	sort.Strings(paths)
	if want := []string{pathA, pathB}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("只应取回非空的 log_path %v，实际 %v", want, paths)
	}
	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("日志应全部删掉，还剩 %d 行", n)
	}
	want := []model.TaskLogDailyStat{
		{Day: "2026-10-01", Success: 1, Failed: 1, Aborted: 1, Other: 2},
		{Day: "2026-10-02", Success: 1, Failed: 1, Aborted: 1, Other: 2},
	}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("计数应为 %+v，实际 %+v", want, got)
	}

	// 同一天再删一批：ON CONFLICT 累加到已有的那一行，不另起一行。
	archiveCreateLog(t, taskID, time.Date(2026, 10, 1, 6, 0, 0, 0, cst), &success, nil)
	archiveCreateLog(t, taskID, time.Date(2026, 10, 1, 7, 0, 0, 0, cst), &success, nil)
	archiveCreateLog(t, taskID, time.Date(2026, 10, 1, 8, 0, 0, 0, cst), nil, nil)
	if deleted, _, err := deleteTaskLogsInTx(t, "task_id = ?", taskID); err != nil || deleted != 3 {
		t.Fatalf("第二批应删掉 3 行，实际 %d 行，err=%v", deleted, err)
	}
	want[0] = model.TaskLogDailyStat{Day: "2026-10-01", Success: 3, Failed: 1, Aborted: 1, Other: 3}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("同一天再删一批应累加，期望 %+v，实际 %+v", want, got)
	}

	// 一行都没命中：GROUP BY 遇到空集不产出行，计数表原样不动，也不会多出全 0 的一行。
	deleted, paths, err = deleteTaskLogsInTx(t, "id = ?", 999999)
	if err != nil || deleted != 0 || len(paths) != 0 {
		t.Fatalf("删不存在的行应返回 0 行、无路径、无错误，实际 %d 行、%v、err=%v", deleted, paths, err)
	}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("一行都没命中时计数表不该变，期望 %+v，实际 %+v", want, got)
	}
}

// S2：归档键是 created_at 存储文本的前 10 位，不是换算后的日期。面板时区从东八区改成 UTC 前后各写一行：
// 东八区早上 7:30 那行仍记在当天（要是用 date() 换算成 UTC，就会落到前一天）。
func TestDeleteTaskLogsKeysByStoredPrefixAfterTimezoneChange(t *testing.T) {
	restorePanelTimezoneForTest(t)
	testutil.SetupTestEnv(t)
	taskID := createTaskForLog(t)
	success, failed := model.LogStatusSuccess, model.LogStatusFailed

	if err := ApplyPanelTimezone("Asia/Shanghai"); err != nil {
		t.Fatalf("apply Asia/Shanghai: %v", err)
	}
	archiveCreateLog(t, taskID, time.Date(2026, 10, 5, 7, 30, 0, 0, time.Local), &success, nil)
	if err := ApplyPanelTimezone("UTC"); err != nil {
		t.Fatalf("apply UTC: %v", err)
	}
	// 改成 UTC 之后写的一行：按东八区看已经是 10-06 凌晨，存储文本上仍是 10-05。
	archiveCreateLog(t, taskID, time.Date(2026, 10, 5, 20, 0, 0, 0, time.Local), &failed, nil)

	// 前提：两行存储文本的前 10 位都是 2026-10-05（库里存的是带偏移的文本，按写入时的时区）。
	var prefixes []string
	if err := database.DB.Raw("SELECT substr(created_at, 1, 10) FROM task_logs ORDER BY id").Scan(&prefixes).Error; err != nil {
		t.Fatalf("read created_at prefixes: %v", err)
	}
	if want := []string{"2026-10-05", "2026-10-05"}; !reflect.DeepEqual(prefixes, want) {
		t.Fatalf("前提不成立：created_at 存储文本的前 10 位应为 %v，实际 %v", want, prefixes)
	}

	if _, _, err := deleteTaskLogsInTx(t, "task_id = ?", taskID); err != nil {
		t.Fatalf("delete task logs: %v", err)
	}
	want := []model.TaskLogDailyStat{{Day: "2026-10-05", Success: 1, Failed: 1}}
	if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("归档键应取删前的存储前缀，期望 %+v，实际 %+v", want, got)
	}
}

// S3：删行那一步失败时整个事务回滚——计数表 0 行、日志还在；
// D1~D3 共用的 cleanExpiredTaskLogRows 在这种情况下返回 (0, 0)，也不按 log_path 删文件。
// 这里刻意直接调 cleanExpiredTaskLogRows 而不是 CleanLogsOlderThan：后者随后还会无条件按 ModTime 扫盘（CleanOldLogs），
// 文件时间一旦调老就会被扫走，混淆「删行失败不按路径删文件」这一条。
func TestDeleteTaskLogsRollsBackArchiveWhenDeleteFails(t *testing.T) {
	testutil.SetupTestEnv(t)
	taskID := createTaskForLog(t)
	success := model.LogStatusSuccess
	rel := writeTaskLogFile(t, "task_1_demo/blocked.log")
	logID := createTaskLogWithPath(t, taskID, time.Now().AddDate(0, 0, -3), &success, &rel)

	if err := database.DB.Exec(`CREATE TRIGGER archive_test_block_log_delete BEFORE DELETE ON task_logs BEGIN SELECT RAISE(ABORT, 'blocked by test'); END;`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if _, _, err := deleteTaskLogsInTx(t, "id = ?", logID); err == nil {
		t.Fatal("删行被触发器拦下时应返回错误")
	}
	if got := archivedDailyStats(t); len(got) != 0 {
		t.Fatalf("事务回滚后计数表应为空，实际 %+v", got)
	}
	if n := countTaskLogs(t); n != 1 {
		t.Fatalf("日志行应还在，实际 %d 行", n)
	}

	records, files := cleanExpiredTaskLogRows("", nil, 1, config.C.Data.LogDir)
	if records != 0 || files != 0 {
		t.Fatalf("删行失败时应返回 (0, 0)，实际 (%d, %d)", records, files)
	}
	if _, err := os.Stat(filepath.Join(config.C.Data.LogDir, filepath.FromSlash(rel))); err != nil {
		t.Fatalf("删行失败时不应按 log_path 删文件，stat err=%v", err)
	}
	if got := archivedDailyStats(t); len(got) != 0 {
		t.Fatalf("清理失败后计数表应为空，实际 %+v", got)
	}
	if n := countTaskLogs(t); n != 1 {
		t.Fatalf("清理失败后日志行应还在，实际 %d 行", n)
	}
}

// archiveSQLRecorder 记下 GORM 发出的每一条 SQL（logger 的 Trace 对每条语句调一次），
// 写法照 database/task_log_indexes_migration_test.go 的 migrationSQLRecorder。
type archiveSQLRecorder struct {
	logger.Interface
	statements []string
}

func (r *archiveSQLRecorder) LogMode(logger.LogLevel) logger.Interface { return r }

func (r *archiveSQLRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.statements = append(r.statements, sql)
}

// S4：事务里依次是「归档 INSERT → 取 log_path → 删行」：归档那条写语句必须打头（WAL 下先读后写会撞 517，
// 见 DeleteTaskLogs 的注释），取路径必须排在删行之前。
func TestDeleteTaskLogsArchiveIsFirstStatementInTransaction(t *testing.T) {
	testutil.SetupTestEnv(t)
	taskID := createTaskForLog(t)
	success := model.LogStatusSuccess
	rel := "task_1_demo/order.log"
	createTaskLogWithPath(t, taskID, time.Now(), &success, &rel)

	recorder := &archiveSQLRecorder{Interface: logger.Discard}
	if err := database.DB.Session(&gorm.Session{Logger: recorder}).Transaction(func(tx *gorm.DB) error {
		_, _, err := DeleteTaskLogs(tx, "task_id = ?", taskID)
		return err
	}); err != nil {
		t.Fatalf("delete task logs: %v", err)
	}

	wantPrefixes := []string{"INSERT INTO task_log_daily_stats", "SELECT log_path FROM task_logs", "DELETE FROM task_logs"}
	if len(recorder.statements) != len(wantPrefixes) {
		t.Fatalf("事务里应正好 %d 条语句，实际 %d 条：%v", len(wantPrefixes), len(recorder.statements), recorder.statements)
	}
	for i, prefix := range wantPrefixes {
		// GORM 生成的语句会给表名、列名加反引号，去掉再比。
		got := strings.ReplaceAll(strings.TrimSpace(recorder.statements[i]), "`", "")
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("第 %d 条语句应以 %q 开头，实际：%s\n全部语句：%v", i+1, prefix, recorder.statements[i], recorder.statements)
		}
	}
}

// S5：语法护栏——生产代码里删 task_logs 只能经 DeleteTaskLogs（#158）。遍历 server/ 下全部非 _test.go 的 .go 文件：
//  1. X.Delete(&model.TaskLog{...}) 恰好 1 处，且在 DeleteTaskLogs 里；
//  2. deleteAll(_, "task_logs") 恰好 1 处，且在 restoreBackupManifest 里（勾「日志」时计数整体换成备份里的，不需要并入）；
//  3. 字符串字面量里出现 DELETE FROM task_logs（合并空白、忽略大小写与引号）一律报错；
//  4. X.Table("task_logs") 一律报错（读也不行）。
//
// 「恰好 1 处」防的是扫描规则失灵（比如目录走错、一个文件都没扫到）之后空跑通过。
// 运行时的兜底在 testutil.SetupTestEnv 的测试期 Delete 回调里（见 TestTaskLogDeleteGuardRejectsUnmarkedDelete）。
// 注意：这条用例按相对路径 .. 读源码，在 WSL 里跑交叉编译的测试二进制时要放进整个工作区的副本里跑，否则扫不到文件。
func TestTaskLogDeletesOnlyGoThroughDeleteTaskLogs(t *testing.T) {
	// stringValue 取字符串字面量的内容（双引号、反引号都认），不是字符串字面量时返回空串。
	stringValue := func(expr ast.Expr) string {
		lit, ok := expr.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return ""
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return ""
		}
		return value
	}
	// isModelTaskLogAddr 判断实参是不是 &model.TaskLog{...}。
	isModelTaskLogAddr := func(expr ast.Expr) bool {
		unary, ok := expr.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return false
		}
		composite, ok := unary.X.(*ast.CompositeLit)
		if !ok {
			return false
		}
		sel, ok := composite.Type.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "TaskLog" {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		return ok && pkg.Name == "model"
	}

	deleteCalls, restoreDeleteAll, files := 0, 0, 0
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// server/data 里可能放着用户脚本；testdata、node_modules、隐藏目录也都不是面板源码。根目录 .. 自己不能跳过。
			if path != ".." && (name == "data" || name == "testdata" || name == "node_modules" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range parsed.Decls {
			// 闭包（比如 database.DB.Transaction(func(tx) {...})）里的调用算在外层函数头上。
			funcName := ""
			if fn, ok := decl.(*ast.FuncDecl); ok {
				funcName = fn.Name.Name
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.BasicLit:
					if n.Kind != token.STRING {
						return true
					}
					// 先解码字面量（"…\n…" 里的 \n、\t 要变回真的空白，否则合并不了），再合并空白、去掉引号与反引号、忽略大小写：
					// 多行原生 SQL、"DELETE FROM\n task_logs"、DELETE FROM `task_logs` 都算。
					text := strings.ToLower(strings.Join(strings.Fields(strings.NewReplacer("`", "", "\"", "").Replace(stringValue(n))), " "))
					if strings.Contains(text, "delete from task_logs") {
						t.Errorf("%s: 字符串里直接写了 DELETE FROM task_logs，删日志一律经 service.DeleteTaskLogs：%s", fset.Position(n.Pos()), n.Value)
					}
				case *ast.CallExpr:
					switch fun := n.Fun.(type) {
					case *ast.SelectorExpr:
						if fun.Sel.Name == "Table" && len(n.Args) > 0 && stringValue(n.Args[0]) == "task_logs" {
							t.Errorf("%s: 不要用 Table(\"task_logs\")，用 Model(&model.TaskLog{})；删日志一律经 service.DeleteTaskLogs", fset.Position(n.Pos()))
						}
						if fun.Sel.Name == "Delete" && len(n.Args) > 0 && isModelTaskLogAddr(n.Args[0]) {
							deleteCalls++
							if funcName != "DeleteTaskLogs" {
								t.Errorf("%s: Delete(&model.TaskLog{}) 只能出现在 service.DeleteTaskLogs 里（实际在 %s 里）", fset.Position(n.Pos()), funcName)
							}
						}
					case *ast.Ident:
						if fun.Name == "deleteAll" && len(n.Args) == 2 && stringValue(n.Args[1]) == "task_logs" {
							restoreDeleteAll++
							if funcName != "restoreBackupManifest" {
								t.Errorf("%s: deleteAll(…, \"task_logs\") 只允许出现在 restoreBackupManifest 里（实际在 %s 里）", fset.Position(n.Pos()), funcName)
							}
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk server tree: %v", err)
	}
	if deleteCalls != 1 || restoreDeleteAll != 1 {
		t.Fatalf("扫了 %d 个文件：Delete(&model.TaskLog{}) 应恰好 1 处、deleteAll(…, \"task_logs\") 应恰好 1 处，实际 %d 处、%d 处（扫描规则可能失灵了）",
			files, deleteCalls, restoreDeleteAll)
	}
}

// S6：测试期护栏（testutil.SetupTestEnv 挂的 Delete 回调）自测。护栏坏了这一条会红，否则别的用例都会空跑：
// 没带标记的几种删法都报护栏错误且一行不删；DeleteTaskLogs 照常通过；标记不会串到同一事务里的下一条；别的表不受影响。
func TestTaskLogDeleteGuardRejectsUnmarkedDelete(t *testing.T) {
	testutil.SetupTestEnv(t)
	taskID := createTaskForLog(t)
	success := model.LogStatusSuccess
	for i := 0; i < 4; i++ {
		createTaskLogWithPath(t, taskID, time.Now().Add(-time.Duration(i)*time.Hour), &success, nil)
	}
	assertGuardRejected := func(label string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "service.DeleteTaskLogs") {
			t.Fatalf("%s：应被护栏拦下（报「只能经 service.DeleteTaskLogs 删除」），实际 err=%v", label, err)
		}
		if n := countTaskLogs(t); n != 4 {
			t.Fatalf("%s：被拦下时一行都不该删，实际剩 %d 行", label, n)
		}
	}

	assertGuardRejected("Where(...).Delete(&model.TaskLog{})", database.DB.Where("task_id = ?", taskID).Delete(&model.TaskLog{}).Error)

	var logs []model.TaskLog
	if err := database.DB.Where("task_id = ?", taskID).Order("id ASC").Find(&logs).Error; err != nil || len(logs) != 4 {
		t.Fatalf("load task logs: err=%v, got %d rows", err, len(logs))
	}
	assertGuardRejected("Delete(&logs)", database.DB.Delete(&logs).Error)
	assertGuardRejected(`Table("task_logs")…Delete`, database.DB.Table("task_logs").Where("task_id = ?", taskID).Delete(&model.TaskLog{}).Error)

	// 同一个事务里：DeleteTaskLogs 带标记的那条通过，紧跟着一条没带标记的仍被拦下（标记不串），整个事务回滚、计数也不留。
	markedPassed := false
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if _, _, err := DeleteTaskLogs(tx, "id = ?", logs[0].ID); err != nil {
			return err
		}
		markedPassed = true
		return tx.Where("id = ?", logs[1].ID).Delete(&model.TaskLog{}).Error
	})
	if !markedPassed {
		t.Fatalf("DeleteTaskLogs 带了标记，不该被护栏拦下：%v", err)
	}
	assertGuardRejected("同一事务里 DeleteTaskLogs 之后没带标记的那条", err)
	if got := archivedDailyStats(t); len(got) != 0 {
		t.Fatalf("事务回滚后计数表应为空，实际 %+v", got)
	}

	// DeleteTaskLogs 本身照常删得掉。
	deleted, _, err := deleteTaskLogsInTx(t, "task_id = ?", taskID)
	if err != nil || deleted != 4 {
		t.Fatalf("DeleteTaskLogs 应删掉 4 行，实际 %d 行，err=%v", deleted, err)
	}
	if n := countTaskLogs(t); n != 0 {
		t.Fatalf("DeleteTaskLogs 之后应一行不剩，实际 %d 行", n)
	}

	// 别的表不受影响：删一个不存在的任务照常返回，不报护栏错误。
	if err := database.DB.Where("id = ?", 999999).Delete(&model.Task{}).Error; err != nil {
		t.Fatalf("删别的表不该被护栏拦下：%v", err)
	}
}

// S7：订阅同步自动删失效任务（D8）也先并入计数：删成功时计数 = 被删日志按天、按状态的分布；
// 删任务失败（事务回滚）时计数一行不留、日志还在。
// D8 没有 HTTP 入口、handler 测试里也没有现成的订阅自动删场景，所以只在 service 层验证（design DD6）；
// 配合 handler 层已证明的「仪表板 = 现存 + 计数表」，等价于 D8 删除后仪表板不变。
func TestDeleteSubscriptionTaskIfUnchangedArchivesLogsWithTask(t *testing.T) {
	success, failed, running, aborted := model.LogStatusSuccess, model.LogStatusFailed, model.LogStatusRunning, model.LogStatusAborted
	cst := time.FixedZone("CST", 8*60*60)
	// 两天、各种状态；下面的期望值就是这批行按存储日期、按状态的分布（删前现算的结果）。
	seedLogs := func(t *testing.T, taskID uint) {
		t.Helper()
		for _, row := range []struct {
			at     time.Time
			status *int
		}{
			{time.Date(2026, 10, 1, 8, 0, 0, 0, cst), &success},
			{time.Date(2026, 10, 1, 9, 0, 0, 0, cst), &failed},
			{time.Date(2026, 10, 1, 10, 0, 0, 0, cst), nil},
			{time.Date(2026, 10, 2, 8, 0, 0, 0, cst), &aborted},
			{time.Date(2026, 10, 2, 9, 0, 0, 0, cst), &success},
			{time.Date(2026, 10, 2, 10, 0, 0, 0, cst), &running},
		} {
			archiveCreateLog(t, taskID, row.at, row.status, nil)
		}
	}

	t.Run("删成功时计数等于被删日志的分布", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		task := steNewTask(t, "stale-archive", "task repo/gone.js", "0 3 * * *", "subscription:1")
		seedLogs(t, task.ID)

		removed, err := deleteSubscriptionTaskIfUnchanged(task)
		if err != nil || !removed {
			t.Fatalf("快照与库一致时应删掉任务，实际 removed=%v err=%v", removed, err)
		}
		want := []model.TaskLogDailyStat{
			{Day: "2026-10-01", Success: 1, Failed: 1, Other: 1},
			{Day: "2026-10-02", Success: 1, Aborted: 1, Other: 1},
		}
		if got := archivedDailyStats(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("计数应为 %+v，实际 %+v", want, got)
		}
		if n := steLogCount(task.ID); n != 0 {
			t.Fatalf("任务的日志应删光，还剩 %d 行", n)
		}
	})

	t.Run("删任务失败时计数一起回滚", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		task := steNewTask(t, "blocked-archive", "task repo/gone.js", "0 3 * * *", "subscription:1")
		seedLogs(t, task.ID)
		steBlockTaskDeletes(t)

		removed, err := deleteSubscriptionTaskIfUnchanged(task)
		if err == nil || removed {
			t.Fatalf("删任务失败时应返回错误，实际 removed=%v err=%v", removed, err)
		}
		if got := archivedDailyStats(t); len(got) != 0 {
			t.Fatalf("事务回滚后计数表应为空，实际 %+v", got)
		}
		if n := steLogCount(task.ID); n != 6 {
			t.Fatalf("事务回滚后日志应原样还在（6 行），实际 %d 行", n)
		}
	})
}
