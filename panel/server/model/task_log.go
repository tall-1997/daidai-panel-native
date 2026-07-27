package model

import (
	"time"
)

const (
	LogStatusSuccess = 0
	LogStatusFailed  = 1
	LogStatusRunning = 2
	LogStatusAborted = 3
)

type TaskLog struct {
	ID        uint       `gorm:"primarykey" json:"id"`
	TaskID    uint       `gorm:"index;not null" json:"task_id"`
	Content   string     `gorm:"type:text;default:''" json:"content"`
	Status    *int       `json:"status"`
	Duration  *float64   `json:"duration"`
	LogPath   *string    `gorm:"size:256" json:"log_path"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Task      *Task      `gorm:"foreignKey:TaskID" json:"-"`
}

func (TaskLog) TableName() string {
	return "task_logs"
}

func (l *TaskLog) ToDict() map[string]interface{} {
	result := map[string]interface{}{
		"id":         l.ID,
		"task_id":    l.TaskID,
		"content":    l.Content,
		"status":     l.Status,
		"duration":   l.Duration,
		"log_path":   l.LogPath,
		"started_at": l.StartedAt,
		"ended_at":   l.EndedAt,
		"created_at": l.CreatedAt,
		"updated_at": l.UpdatedAt,
	}
	if l.Task != nil {
		result["task_name"] = l.Task.Name
		result["task_type"] = l.Task.GetTaskType()
		result["labels"] = l.Task.GetLabels()
		result["task"] = map[string]interface{}{
			"task_type": l.Task.GetTaskType(),
			"labels":    l.Task.GetLabels(),
		}
	}
	return result
}
