package service

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

func PersistLogCursor(logID uint, cursor int64) error {
	if database.DB == nil || logID == 0 || cursor < 0 {
		return nil
	}
	return database.DB.Model(&model.TaskLog{}).Where("id = ?", logID).Update("log_cursor", cursor).Error
}

func AppendLogCursor(logID uint, delta int) int64 {
	if database.DB == nil || logID == 0 || delta <= 0 {
		return 0
	}
	var taskLog model.TaskLog
	if err := database.DB.Select("id", "log_cursor").First(&taskLog, logID).Error; err != nil {
		return 0
	}
	cursor := taskLog.LogCursor + int64(delta)
	_ = PersistLogCursor(logID, cursor)
	return cursor
}

func ReadTaskLogFromCursor(taskLog *model.TaskLog, cursor int64) (string, int64, error) {
	if taskLog == nil {
		return "", cursor, fmt.Errorf("task log is nil")
	}
	content, err := TaskLogPlainContent(taskLog)
	if err != nil {
		return "", cursor, err
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor > int64(len(content)) {
		cursor = int64(len(content))
	}
	return content[cursor:], int64(len(content)), nil
}

func TaskLogPlainContent(taskLog *model.TaskLog) (string, error) {
	if taskLog == nil {
		return "", nil
	}
	if taskLog.Content != "" {
		if decompressed, err := DecompressFromBase64(taskLog.Content); err == nil {
			return decompressed, nil
		}
		if decoded, err := base64.StdEncoding.DecodeString(taskLog.Content); err == nil {
			return string(decoded), nil
		}
		return taskLog.Content, nil
	}
	if taskLog.LogPath != nil {
		logDir := ""
		if config.C != nil {
			logDir = config.C.Data.LogDir
		}
		return ReadLogFile(*taskLog.LogPath, logDir)
	}
	return "", nil
}

func ParseLogCursor(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		return 0
	}
	return cursor
}
