package service

import (
	"context"
	"log"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

var (
	logCleanupMu      sync.Mutex
	logCleanupStop    chan struct{}
	logCleanupDone    chan struct{}
	logCleanupRunning bool
)

func StartLogCleanup(ctx context.Context) error {
	_ = ctx
	logCleanupMu.Lock()
	defer logCleanupMu.Unlock()

	if logCleanupRunning {
		return nil
	}

	logCleanupStop = make(chan struct{})
	logCleanupDone = make(chan struct{})
	logCleanupRunning = true
	go logCleanupLoop(logCleanupStop, logCleanupDone)
	log.Println("log cleanup worker started (interval: 6h)")
	return nil
}

func StopLogCleanup(ctx context.Context) error {
	logCleanupMu.Lock()
	if !logCleanupRunning {
		logCleanupMu.Unlock()
		return nil
	}
	stop := logCleanupStop
	done := logCleanupDone
	logCleanupRunning = false
	logCleanupStop = nil
	logCleanupDone = nil
	logCleanupMu.Unlock()

	close(stop)
	if ctx == nil {
		<-done
	} else {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func LogCleanupWorkerRunning() bool {
	logCleanupMu.Lock()
	defer logCleanupMu.Unlock()
	return logCleanupRunning
}

// StartLogCleanupWorker 启动日志自动清理后台 worker：
// 启动后延迟一小段时间先清一次，之后每 6 小时清理一次。
// 同时清理「数据库 TaskLog 旧记录」与「磁盘旧 .log 文件」，按 log_retention_days 判定，无开关。
func StartLogCleanupWorker() {
	if err := StartLogCleanup(context.Background()); err != nil {
		log.Printf("log cleanup worker start failed: %v", err)
	}
}

func StopLogCleanupWorker() {
	if err := StopLogCleanup(context.Background()); err != nil {
		log.Printf("log cleanup worker stop failed: %v", err)
	}
}

func logCleanupLoop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	// 启动延迟，避免与启动迁移争抢
	select {
	case <-time.After(60 * time.Second):
		runPeriodicCleanup()
	case <-stop:
		return
	}

	for {
		select {
		case <-ticker.C:
			runPeriodicCleanup()
		case <-stop:
			return
		}
	}
}

func runPeriodicCleanup() {
	cleanupOldLogs()
	if removed, err := CleanExpiredTokenBlocklist(); err != nil {
		log.Printf("token blocklist cleanup failed: %v", err)
	} else if removed > 0 {
		log.Printf("token blocklist cleanup: removed %d expired rows", removed)
	}
}

// cleanupOldLogs 按 log_retention_days 清理过期日志（DB 记录 + 磁盘文件）。
func cleanupOldLogs() {
	days := model.GetRegisteredConfigInt("log_retention_days")
	if days < 1 {
		days = 1
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	var deletedRecords int64
	if database.DB != nil {
		result := database.DB.Where("started_at < ?", cutoff).Delete(&model.TaskLog{})
		if result.Error != nil {
			log.Printf("log cleanup: delete TaskLog records failed: %v", result.Error)
		} else {
			deletedRecords = result.RowsAffected
		}
	}

	deletedFiles := 0
	if config.C != nil {
		deletedFiles = CleanOldLogs(config.C.Data.LogDir, days)
	}

	log.Printf("log cleanup: removed %d TaskLog records and %d log files (retention: %d days)", deletedRecords, deletedFiles, days)
}
