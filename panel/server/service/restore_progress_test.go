package service

import (
	"errors"
	"testing"
)

func resetRestoreProgressStateForTest() {
	restoreProgressState.mu.Lock()
	defer restoreProgressState.mu.Unlock()
	restoreProgressState.progress = RestoreProgress{
		Status: RestoreProgressStatusIdle,
	}
}

func TestRestoreProgressLifecycle(t *testing.T) {
	resetRestoreProgressStateForTest()
	t.Cleanup(resetRestoreProgressStateForTest)

	if err := BeginRestoreProgress("backup_20260323.tgz"); err != nil {
		t.Fatalf("begin restore progress: %v", err)
	}

	running := CurrentRestoreProgress()
	if !running.Active {
		t.Fatalf("expected restore progress to be active")
	}
	if running.Status != RestoreProgressStatusRunning {
		t.Fatalf("expected running status, got %q", running.Status)
	}
	if running.Filename != "backup_20260323.tgz" {
		t.Fatalf("expected filename to be tracked, got %q", running.Filename)
	}

	UpdateRestoreProgress("extracting", "正在解包...", 28)
	BindRestoreProgressPlan("daidai-panel", BackupSelection{
		Configs:      true,
		Tasks:        true,
		Dependencies: true,
	})
	updated := CurrentRestoreProgress()
	if updated.Stage != "extracting" {
		t.Fatalf("expected stage extracting, got %q", updated.Stage)
	}
	if updated.Percent != 28 {
		t.Fatalf("expected percent 28, got %d", updated.Percent)
	}
	if updated.Source != "daidai-panel" {
		t.Fatalf("expected source daidai-panel, got %q", updated.Source)
	}
	if updated.Selection == nil || !updated.Selection.Configs || !updated.Selection.Tasks || !updated.Selection.Dependencies {
		t.Fatalf("expected restore selection to be bound, got %+v", updated.Selection)
	}

	CompleteRestoreProgress("恢复完成")
	completed := CurrentRestoreProgress()
	if completed.Active {
		t.Fatalf("expected completed restore to be inactive")
	}
	if completed.Status != RestoreProgressStatusCompleted {
		t.Fatalf("expected completed status, got %q", completed.Status)
	}
	if completed.Percent != 100 {
		t.Fatalf("expected percent 100, got %d", completed.Percent)
	}
	if completed.Stage != "completed" {
		t.Fatalf("expected completed stage, got %q", completed.Stage)
	}
}

func TestRestoreProgressRejectsConcurrentRestore(t *testing.T) {
	resetRestoreProgressStateForTest()
	t.Cleanup(resetRestoreProgressStateForTest)

	if err := BeginRestoreProgress("first.tgz"); err != nil {
		t.Fatalf("begin restore progress: %v", err)
	}
	if err := BeginRestoreProgress("second.tgz"); err == nil {
		t.Fatalf("expected concurrent restore to be rejected")
	}
}

func TestRestoreProgressFailureCapturesError(t *testing.T) {
	resetRestoreProgressStateForTest()
	t.Cleanup(resetRestoreProgressStateForTest)

	if err := BeginRestoreProgress("failed.tgz"); err != nil {
		t.Fatalf("begin restore progress: %v", err)
	}

	FailRestoreProgress(errors.New("恢复备份失败"))
	progress := CurrentRestoreProgress()
	if progress.Active {
		t.Fatalf("expected failed restore to be inactive")
	}
	if progress.Status != RestoreProgressStatusFailed {
		t.Fatalf("expected failed status, got %q", progress.Status)
	}
	if progress.Error != "恢复备份失败" {
		t.Fatalf("expected failure error to be recorded, got %q", progress.Error)
	}
}
