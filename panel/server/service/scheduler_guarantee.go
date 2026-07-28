package service

import (
	"fmt"
	"strings"
	"sync"
)

const (
	SchedulerGuaranteeForegroundContinuous = "foreground_continuous"
	SchedulerGuaranteeSystemCompensation   = "system_compensation"
	SchedulerGuaranteeSystemStopped        = "system_stopped"
	SchedulerGuaranteeResourceLimited      = "resource_limited"
)

type SchedulerGuaranteeSnapshot struct {
	State        string `json:"state"`
	ReasonCode   string `json:"reasonCode,omitempty"`
	Intervention string `json:"intervention,omitempty"`
	Source       string `json:"source,omitempty"`
}

var schedulerGuarantee = struct {
	sync.RWMutex
	snapshot SchedulerGuaranteeSnapshot
}{snapshot: SchedulerGuaranteeSnapshot{State: SchedulerGuaranteeSystemCompensation, ReasonCode: "initial", Source: "core"}}

func ConfigureSchedulerGuarantee(snapshot SchedulerGuaranteeSnapshot) {
	normalized := normalizeSchedulerGuarantee(snapshot)
	schedulerGuarantee.Lock()
	schedulerGuarantee.snapshot = normalized
	schedulerGuarantee.Unlock()
}

func CurrentSchedulerGuarantee() SchedulerGuaranteeSnapshot {
	schedulerGuarantee.RLock()
	defer schedulerGuarantee.RUnlock()
	return schedulerGuarantee.snapshot
}

func ShouldPauseLowPriorityWork(triggerType string) (SchedulerGuaranteeSnapshot, bool) {
	snapshot := CurrentSchedulerGuarantee()
	if !isLowPriorityTrigger(triggerType) {
		return snapshot, false
	}
	switch snapshot.State {
	case SchedulerGuaranteeResourceLimited, SchedulerGuaranteeSystemStopped:
		return snapshot, true
	default:
		return snapshot, false
	}
}

func SchedulerPauseError(snapshot SchedulerGuaranteeSnapshot) error {
	reason := strings.TrimSpace(snapshot.ReasonCode)
	if reason == "" {
		reason = snapshot.State
	}
	return fmt.Errorf("scheduler paused low-priority work: %s", reason)
}

func normalizeSchedulerGuarantee(snapshot SchedulerGuaranteeSnapshot) SchedulerGuaranteeSnapshot {
	state := strings.TrimSpace(snapshot.State)
	switch state {
	case SchedulerGuaranteeForegroundContinuous, SchedulerGuaranteeSystemCompensation, SchedulerGuaranteeSystemStopped, SchedulerGuaranteeResourceLimited:
	default:
		state = SchedulerGuaranteeSystemCompensation
	}
	reason := strings.TrimSpace(snapshot.ReasonCode)
	if reason == "" {
		reason = "ok"
	}
	source := strings.TrimSpace(snapshot.Source)
	if source == "" {
		source = "android-host"
	}
	return SchedulerGuaranteeSnapshot{
		State:        state,
		ReasonCode:   reason,
		Intervention: strings.TrimSpace(snapshot.Intervention),
		Source:       source,
	}
}

func isLowPriorityTrigger(triggerType string) bool {
	switch strings.TrimSpace(triggerType) {
	case TriggerTypeManual:
		return false
	default:
		return true
	}
}
