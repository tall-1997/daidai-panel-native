package model

import (
	"strings"
	"time"
)

const (
	ScheduleInstanceStatePending       = "pending"
	ScheduleInstanceStateLaunching     = "launching"
	ScheduleInstanceStateLaunched      = "launched"
	ScheduleInstanceStateSkipped       = "skipped"
	ScheduleInstanceStateResultUnknown = "result_unknown"

	SchedulePolicySkip     = "skip"
	SchedulePolicyQueue    = "queue"
	SchedulePolicyParallel = "parallel"
)

type ScheduleInstance struct {
	ID             uint       `gorm:"primarykey" json:"id"`
	TaskID         uint       `gorm:"not null;uniqueIndex:idx_schedule_instances_unique;index" json:"task_id"`
	ScheduledUTC   time.Time  `gorm:"not null;uniqueIndex:idx_schedule_instances_unique;index" json:"scheduled_utc"`
	ExpressionHash string     `gorm:"size:64;not null;uniqueIndex:idx_schedule_instances_unique" json:"expression_hash"`
	Expression     string     `gorm:"type:text;not null" json:"expression"`
	State          string     `gorm:"size:32;not null;index" json:"state"`
	Policy         string     `gorm:"size:16;not null;default:'skip'" json:"policy"`
	OperationID    string     `gorm:"size:64;default:'';index" json:"operation_id"`
	Reason         string     `gorm:"size:128;default:''" json:"reason"`
	CreatedAt      time.Time  `json:"created_at"`
	ClaimedAt      *time.Time `json:"claimed_at"`
	StartedAt      *time.Time `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
}

func (ScheduleInstance) TableName() string {
	return "schedule_instances"
}

func NormalizeSchedulePolicy(policy string) string {
	normalized := strings.ToLower(strings.TrimSpace(policy))
	switch normalized {
	case SchedulePolicySkip, SchedulePolicyQueue, SchedulePolicyParallel:
		return normalized
	default:
		return SchedulePolicySkip
	}
}

func (t *Task) EffectiveSchedulePolicy() string {
	if t == nil {
		return SchedulePolicySkip
	}
	if policy := NormalizeSchedulePolicy(t.SchedulePolicy); policy != SchedulePolicySkip || t.SchedulePolicy == SchedulePolicySkip {
		return policy
	}
	if t.AllowMultipleInstances {
		return SchedulePolicyParallel
	}
	return SchedulePolicySkip
}
