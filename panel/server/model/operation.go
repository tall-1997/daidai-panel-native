package model

import "time"

const (
	OperationKindTask         = "task"
	OperationKindScript       = "script"
	OperationKindDependency   = "dependency"
	OperationKindRuntime      = "runtime"
	OperationKindSubscription = "subscription"
	OperationKindBackup       = "backup"

	OperationStatePending  = "pending"
	OperationStateRunning  = "running"
	OperationStateSuccess  = "success"
	OperationStateFailed   = "failed"
	OperationStateAborted  = "aborted"
	OperationStateUnknown  = "unknown"
	OperationStateCanceled = "canceled"
)

type Operation struct {
	ID        string     `gorm:"primaryKey;size:64" json:"id"`
	Kind      string     `gorm:"size:32;index;not null" json:"kind"`
	State     string     `gorm:"size:32;index;not null" json:"state"`
	Phase     string     `gorm:"size:64;default:''" json:"phase"`
	Sequence  int64      `gorm:"index;not null" json:"sequence"`
	Progress  float64    `gorm:"default:0" json:"progress"`
	ExitCode  *int       `json:"exit_code"`
	ErrorCode string     `gorm:"size:128;default:''" json:"error_code"`
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	LogCursor int64      `gorm:"default:0" json:"log_cursor"`
}

func (Operation) TableName() string {
	return "operations"
}

func (o *Operation) ToDict() map[string]interface{} {
	return map[string]interface{}{
		"id":         o.ID,
		"kind":       o.Kind,
		"state":      o.State,
		"phase":      o.Phase,
		"sequence":   o.Sequence,
		"progress":   o.Progress,
		"exit_code":  o.ExitCode,
		"error_code": o.ErrorCode,
		"created_at": o.CreatedAt,
		"started_at": o.StartedAt,
		"ended_at":   o.EndedAt,
		"log_cursor": o.LogCursor,
	}
}
