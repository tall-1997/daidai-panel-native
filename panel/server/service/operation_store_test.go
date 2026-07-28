package service

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestOperationStoreTerminalStateCannotBeOverwritten(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	op, err := store.Create(OperationCreateOptions{ID: "terminal_once", Kind: model.OperationKindTask})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := store.Start(op.ID, "running"); err != nil {
		t.Fatalf("start operation: %v", err)
	}
	if err := store.Unknown(op.ID, "stopped", 10); err != nil {
		t.Fatalf("unknown operation: %v", err)
	}
	if err := store.Finish(op.ID, 0, 20); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected terminal overwrite error, got %v", err)
	}
	code := 1
	if err := store.Fail(op.ID, &code, "late_failure", 30); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected terminal overwrite error for fail, got %v", err)
	}

	var current model.Operation
	if err := database.DB.First(&current, "id = ?", op.ID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if current.State != model.OperationStateUnknown || current.ErrorCode != "stopped" || current.LogCursor != 10 {
		t.Fatalf("terminal state was overwritten: %+v", current)
	}
}

func TestOperationStoreUnknownAndCancelCannotBeOverwritten(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	terminalCases := []struct {
		name      string
		terminal  func(string) error
		wantState string
	}{
		{
			name: "unknown",
			terminal: func(id string) error {
				return store.Unknown(id, "stopped", 11)
			},
			wantState: model.OperationStateUnknown,
		},
		{
			name: "canceled",
			terminal: func(id string) error {
				return store.Cancel(id, "aborted", 12)
			},
			wantState: model.OperationStateCanceled,
		},
	}

	for _, tc := range terminalCases {
		t.Run(tc.name, func(t *testing.T) {
			op, err := store.Create(OperationCreateOptions{ID: "terminal_" + tc.name, Kind: model.OperationKindTask})
			if err != nil {
				t.Fatalf("create operation: %v", err)
			}
			if err := store.Start(op.ID, "running"); err != nil {
				t.Fatalf("start operation: %v", err)
			}
			if err := tc.terminal(op.ID); err != nil {
				t.Fatalf("set terminal operation: %v", err)
			}
			if err := store.Finish(op.ID, 0, 99); !errors.Is(err, ErrOperationTerminal) {
				t.Fatalf("expected terminal overwrite error, got %v", err)
			}
			code := 1
			if err := store.Fail(op.ID, &code, "late_failure", 99); !errors.Is(err, ErrOperationTerminal) {
				t.Fatalf("expected terminal overwrite error for fail, got %v", err)
			}
			if err := store.Unknown(op.ID, "late_unknown", 99); !errors.Is(err, ErrOperationTerminal) {
				t.Fatalf("expected terminal overwrite error for unknown, got %v", err)
			}
			if err := store.Cancel(op.ID, "late_cancel", 99); !errors.Is(err, ErrOperationTerminal) {
				t.Fatalf("expected terminal overwrite error for cancel, got %v", err)
			}

			var current model.Operation
			if err := database.DB.First(&current, "id = ?", op.ID).Error; err != nil {
				t.Fatalf("reload operation: %v", err)
			}
			if current.State != tc.wantState || current.LogCursor == 99 {
				t.Fatalf("terminal state was overwritten: %+v", current)
			}
		})
	}
}

func TestOperationStoreProgressSkipsTerminalOperation(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	op, err := store.Create(OperationCreateOptions{ID: "terminal_progress", Kind: model.OperationKindTask})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := store.Start(op.ID, "running"); err != nil {
		t.Fatalf("start operation: %v", err)
	}
	if err := store.Finish(op.ID, 0, 10); err != nil {
		t.Fatalf("finish operation: %v", err)
	}
	if err := store.Progress(op.ID, "late-progress", 50, 99); err != nil {
		t.Fatalf("progress should ignore terminal operation, got %v", err)
	}

	var current model.Operation
	if err := database.DB.First(&current, "id = ?", op.ID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if current.State != model.OperationStateSuccess || current.Phase != "finished" || current.Progress != 100 || current.LogCursor != 10 {
		t.Fatalf("progress updated terminal operation: %+v", current)
	}
}

func TestOperationStoreConcurrentTerminalUpdatesKeepSingleWinner(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	op, err := store.Create(OperationCreateOptions{ID: "terminal_race", Kind: model.OperationKindTask})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := store.Start(op.ID, "running"); err != nil {
		t.Fatalf("start operation: %v", err)
	}

	var wg sync.WaitGroup
	winners := make(chan string, 2)
	runTerminal := func(state string, fn func() error) {
		defer wg.Done()
		err := fn()
		if err == nil {
			winners <- state
			return
		}
		if !errors.Is(err, ErrOperationTerminal) {
			t.Errorf("unexpected terminal update error for %s: %v", state, err)
		}
	}

	wg.Add(2)
	go runTerminal(model.OperationStateSuccess, func() error {
		return store.Finish(op.ID, 0, 21)
	})
	go runTerminal(model.OperationStateCanceled, func() error {
		return store.Cancel(op.ID, "aborted", 22)
	})
	wg.Wait()
	close(winners)

	if len(winners) != 1 {
		t.Fatalf("expected exactly one terminal winner, got %d", len(winners))
	}
	winner := <-winners

	var current model.Operation
	if err := database.DB.First(&current, "id = ?", op.ID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if current.State != winner {
		t.Fatalf("stored terminal state %q does not match winner %q", current.State, winner)
	}
	if current.LogCursor != 21 && current.LogCursor != 22 {
		t.Fatalf("unexpected log cursor after terminal race: %+v", current)
	}
}

func TestOperationStoreStopTerminalCannotBeOverwrittenByExecutorCompletion(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	op, err := store.Create(OperationCreateOptions{ID: "stop_terminal_unique", Kind: model.OperationKindTask})
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	if err := store.Start(op.ID, "running"); err != nil {
		t.Fatalf("start operation: %v", err)
	}
	if err := store.Cancel(op.ID, "aborted", 3); err != nil {
		t.Fatalf("cancel operation: %v", err)
	}

	if err := store.Finish(op.ID, 0, 4); !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("expected terminal overwrite error, got %v", err)
	}
	code := 1
	if err := store.Fail(op.ID, &code, "exit_code", 4); !errors.Is(err, ErrOperationTerminal) {
		t.Fatalf("expected terminal overwrite error for fail, got %v", err)
	}

	var current model.Operation
	if err := database.DB.First(&current, "id = ?", op.ID).Error; err != nil {
		t.Fatalf("reload operation: %v", err)
	}
	if current.State != model.OperationStateCanceled || current.ErrorCode != "aborted" || current.LogCursor != 3 {
		t.Fatalf("stop terminal state was overwritten: %+v", current)
	}
}

func TestOperationStoreTerminalUpdateRequiresExistingNonTerminalRow(t *testing.T) {
	testutil.SetupTestEnv(t)

	store := DefaultOperationStore()
	if err := store.Finish("missing-operation", 0, 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing operation error, got %v", err)
	}
}
