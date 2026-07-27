package service

import (
	"log"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

var (
	logCleanupOnce sync.Once
	logCleanupStop chan struct{}
)

// StartLogCleanupWorker 启动日志自动清理后台 worker：
// 启动后延迟一小段时间先清一次，之后每 6 小时清理一次。
// 同时清理「数据库 TaskLog 旧记录」与「磁盘旧 .log 文件」，按 log_retention_days 判定，无开关。
func StartLogCleanupWorker() {
	logCleanupOnce.Do(func() {
		logCleanupStop = make(chan struct{})
		go logCleanupLoop()
		log.Println("log cleanup worker started (interval: 6h)")
	})
}

func StopLogCleanupWorker() {
	if logCleanupStop != nil {
		close(logCleanupStop)
	}
}

func logCleanupLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	// 启动延迟，避免与启动迁移争抢
	time.Sleep(60 * time.Second)
	cleanupOldLogs()

	for {
		select {
		case <-ticker.C:
			cleanupOldLogs()
		case <-logCleanupStop:
			return
		}
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
