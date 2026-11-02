package service

import (
	"daidai-panel/model"

	"gorm.io/gorm"
)

// DeleteTaskLogs 是删除 task_logs 行的唯一入口（#158，v3.3.6）：先把要删的行按天并进 task_log_daily_stats，
// 再取出这些行里非空的 log_path，最后删行。返回（删掉的行数, 这些行的 log_path, 错误）；
// 任何一步出错都立即返回，由调用方的事务回滚，计数不会多出来。
//
// 调用方必须遵守的约定：
//   - tx 必须是事务（database.DB.Transaction 的 tx，或 database.DB.Begin() 的 tx）。传 database.DB 会让三条语句各自提交，
//     中途崩溃就会出现「同一批行既在计数里、又还在 task_logs 里」。
//   - 🔴 本函数必须是这个事务里的第一条语句：WAL 下事务「先读后写」，中间若有别的进程（ddp）提交过写，
//     随后的写会直接报 database is locked (517)，busy_timeout 也救不了。归档那条 INSERT … SELECT 是写语句，
//     打头执行就一开始拿到写锁，后面的取路径、删行都在同一把锁里。
//   - where 只能是代码里写死的 SQL 片段（用 ? 占位），禁止拼用户输入：它会被原样拼进下面的原生 SQL。
//   - 磁盘文件等事务提交之后再删（DeleteLogFilesForRecords(paths, …)）；回滚时文件不动。
//
// 唯一的例外是恢复备份勾了「日志」时的整表 deleteAll：计数整体换成备份里的，不需要并入（见 restoreBackupManifest）。
//
// 删除一律先并入、趋势只增不减。被删时还在运行中的行计入 other，之后结算的 UPDATE 影响 0 行，不会再算一次；
// 已知例外（接受）：删掉运行中的那行之后、执行结束之前面板重启，启动恢复找不到运行中的行会另建一行失败，
// 同一次执行在趋势里算两次（other 1 + 失败 1），修法（删日志接口拒删运行中的行）在 backlog。
func DeleteTaskLogs(tx *gorm.DB, where string, args ...interface{}) (int64, []string, error) {
	// 归档：status 用 IS 比较而不是 =：status = 0 遇到 NULL 得 NULL，SUM 会少算；IS 0 遇到 NULL 得 0，NULL 行全部进 other。
	// 键取 created_at 存储文本的前 10 位：substr 只出现在 SELECT / GROUP BY 里，WHERE 仍是调用方的条件，照常走索引。
	// GROUP BY 遇到空集不产出行，一行都没命中时不会插出全 0 的计数行；同一天已有计数时 ON CONFLICT 累加。
	if err := tx.Exec(`INSERT INTO task_log_daily_stats (day, success, failed, aborted, other)
SELECT COALESCE(substr(created_at, 1, 10), ''), SUM(status IS 0), SUM(status IS 1), SUM(status IS 3),
       SUM(status IS NULL OR status NOT IN (0, 1, 3))
FROM task_logs WHERE (`+where+`) GROUP BY 1
ON CONFLICT(day) DO UPDATE SET success = success + excluded.success, failed = failed + excluded.failed,
aborted = aborted + excluded.aborted, other = other + excluded.other`, args...).Error; err != nil {
		return 0, nil, err
	}

	// 取路径必须排在删行之前：行一删，log_path 就再也查不回来了，磁盘上的 .log 会变成没人认领的垃圾（#144）。
	var paths []string
	if err := tx.Model(&model.TaskLog{}).Where(where, args...).
		Where("log_path IS NOT NULL AND log_path <> ''").Pluck("log_path", &paths).Error; err != nil {
		return 0, nil, err
	}

	// 带上标记再删：testutil 的测试期护栏只放行带这个标记的 task_logs 删除。标记只挂在这一条语句上，不会串到事务里的下一条。
	result := tx.Set(model.TaskLogDeleteGuardKey, true).Where(where, args...).Delete(&model.TaskLog{})
	if result.Error != nil {
		return 0, nil, result.Error
	}
	return result.RowsAffected, paths, nil
}
