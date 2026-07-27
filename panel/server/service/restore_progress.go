package service

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	RestoreProgressStatusIdle      = "idle"
	RestoreProgressStatusRunning   = "running"
	RestoreProgressStatusCompleted = "completed"
	RestoreProgressStatusFailed    = "failed"
)

type RestoreProgress struct {
	Active    bool             `json:"active"`
	Status    string           `json:"status"`
	Filename  string           `json:"filename,omitempty"`
	Source    string           `json:"source,omitempty"`
	Selection *BackupSelection `json:"selection,omitempty"`
	Stage     string           `json:"stage,omitempty"`
	Message   string           `json:"message,omitempty"`
	Percent   int              `json:"percent"`
	Error     string           `json:"error,omitempty"`
	StartedAt *time.Time       `json:"started_at,omitempty"`
	UpdatedAt time.Time        `json:"updated_at"`
}

var restoreProgressState = struct {
	mu       sync.RWMutex
	progress RestoreProgress
}{
	progress: RestoreProgress{
		Status: RestoreProgressStatusIdle,
	},
}

func BeginRestoreProgress(filename string) error {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()

	if restoreProgressState.progress.Active {
		return fmt.Errorf("已有恢复任务正在进行，请稍后再试")
	}

	now := time.Now()
	restoreProgressState.progress = RestoreProgress{
		Active:    true,
		Status:    RestoreProgressStatusRunning,
		Filename:  strings.TrimSpace(filename),
		Stage:     "preparing",
		Message:   "正在准备恢复环境...",
		Percent:   3,
		StartedAt: &now,
		UpdatedAt: now,
	}

	return nil
}

func UpdateRestoreProgress(stage, message string, percent int) {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()

	if !restoreProgressState.progress.Active && restoreProgressState.progress.Status != RestoreProgressStatusRunning {
		return
	}

	restoreProgressState.progress.Status = RestoreProgressStatusRunning
	restoreProgressState.progress.Stage = strings.TrimSpace(stage)
	restoreProgressState.progress.Message = strings.TrimSpace(message)
	restoreProgressState.progress.Percent = clampRestoreProgressPercent(percent)
	restoreProgressState.progress.Error = ""
	restoreProgressState.progress.UpdatedAt = time.Now()
}

func BindRestoreProgressPlan(source string, selection BackupSelection) {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()

	progress := &restoreProgressState.progress
	if progress.Status == RestoreProgressStatusIdle && !progress.Active {
		return
	}

	normalized := selection.NormalizeDefaults()
	progress.Source = strings.TrimSpace(source)
	progress.Selection = &normalized
	progress.UpdatedAt = time.Now()
}

func CompleteRestoreProgress(message string) {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()

	restoreProgressState.progress.Active = false
	restoreProgressState.progress.Status = RestoreProgressStatusCompleted
	restoreProgressState.progress.Stage = "completed"
	restoreProgressState.progress.Message = strings.TrimSpace(message)
	restoreProgressState.progress.Percent = 100
	restoreProgressState.progress.Error = ""
	restoreProgressState.progress.UpdatedAt = time.Now()
}

func FailRestoreProgress(err error) {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()

	restoreProgressState.progress.Active = false
	restoreProgressState.progress.Status = RestoreProgressStatusFailed
	restoreProgressState.progress.Stage = "failed"
	restoreProgressState.progress.Message = "恢复过程中出现异常"
	if err != nil {
		restoreProgressState.progress.Error = strings.TrimSpace(err.Error())
	}
	restoreProgressState.progress.UpdatedAt = time.Now()
}

func CurrentRestoreProgress() RestoreProgress {
	restoreProgressState.mu.RLock()
	defer restoreProgressState.mu.RUnlock()
	return restoreProgressState.progress
}

func clampRestoreProgressPercent(percent int) int {
	switch {
	case percent < 0:
		return 0
	case percent > 100:
		return 100
	default:
		return percent
	}
}
