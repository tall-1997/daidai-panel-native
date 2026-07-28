package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

type OperationStore struct{}

var (
	ErrOperationNotFound = errors.New("operation not found")
	ErrOperationTerminal = errors.New("operation is already terminal")
)

type OperationCreateOptions struct {
	ID       string
	Kind     string
	Phase    string
	Progress float64
}

func NewOperationStore() *OperationStore {
	return &OperationStore{}
}

func DefaultOperationStore() *OperationStore {
	return NewOperationStore()
}

func (s *OperationStore) Create(opts OperationCreateOptions) (*model.Operation, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = fmt.Sprintf("op_%d", time.Now().UnixNano())
	}
	kind := strings.TrimSpace(opts.Kind)
	if kind == "" {
		kind = model.OperationKindTask
	}
	op := &model.Operation{
		ID:        id,
		Kind:      kind,
		State:     model.OperationStatePending,
		Phase:     strings.TrimSpace(opts.Phase),
		Sequence:  time.Now().UnixNano(),
		Progress:  clampOperationProgress(opts.Progress),
		CreatedAt: time.Now(),
	}
	if err := database.DB.Create(op).Error; err != nil {
		return nil, err
	}
	return op, nil
}

func (s *OperationStore) Start(id, phase string) error {
	now := time.Now()
	return s.updateNonTerminal(id, map[string]interface{}{
		"state":      model.OperationStateRunning,
		"phase":      strings.TrimSpace(phase),
		"started_at": now,
	}, false)
}

func (s *OperationStore) Progress(id, phase string, progress float64, logCursor int64) error {
	updates := map[string]interface{}{
		"phase":    strings.TrimSpace(phase),
		"progress": clampOperationProgress(progress),
	}
	if logCursor >= 0 {
		updates["log_cursor"] = logCursor
	}
	return s.updateNonTerminal(id, updates, true)
}

func (s *OperationStore) Finish(id string, exitCode int, logCursor int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"state":      model.OperationStateSuccess,
		"phase":      "finished",
		"progress":   100.0,
		"exit_code":  exitCode,
		"error_code": "",
		"ended_at":   now,
	}
	if logCursor >= 0 {
		updates["log_cursor"] = logCursor
	}
	return s.updateNonTerminal(id, updates, false)
}

func (s *OperationStore) Fail(id string, exitCode *int, errorCode string, logCursor int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"state":      model.OperationStateFailed,
		"phase":      "failed",
		"progress":   100.0,
		"error_code": strings.TrimSpace(errorCode),
		"ended_at":   now,
	}
	if exitCode != nil {
		updates["exit_code"] = *exitCode
	}
	if logCursor >= 0 {
		updates["log_cursor"] = logCursor
	}
	return s.updateNonTerminal(id, updates, false)
}

func (s *OperationStore) Unknown(id, errorCode string, logCursor int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"state":      model.OperationStateUnknown,
		"phase":      "unknown",
		"progress":   100.0,
		"error_code": strings.TrimSpace(errorCode),
		"ended_at":   now,
	}
	if logCursor >= 0 {
		updates["log_cursor"] = logCursor
	}
	return s.updateNonTerminal(id, updates, false)
}

func (s *OperationStore) Cancel(id, errorCode string, logCursor int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"state":      model.OperationStateCanceled,
		"phase":      "canceled",
		"progress":   100.0,
		"error_code": strings.TrimSpace(errorCode),
		"ended_at":   now,
	}
	if logCursor >= 0 {
		updates["log_cursor"] = logCursor
	}
	return s.updateNonTerminal(id, updates, false)
}

func (s *OperationStore) List(kind, state string, limit int) ([]model.Operation, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query := database.DB.Model(&model.Operation{})
	if kind = strings.TrimSpace(kind); kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if state = strings.TrimSpace(state); state != "" {
		query = query.Where("state = ?", state)
	}
	var operations []model.Operation
	err := query.Order("sequence DESC").Limit(limit).Find(&operations).Error
	return operations, err
}

func (s *OperationStore) Get(id string) (*model.Operation, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("database is not initialized")
	}
	var operation model.Operation
	if err := database.DB.First(&operation, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		return nil, err
	}
	return &operation, nil
}

func (s *OperationStore) updateNonTerminal(id string, updates map[string]interface{}, ignoreTerminal bool) error {
	if database.DB == nil {
		return fmt.Errorf("database is not initialized")
	}
	trimmedID := strings.TrimSpace(id)
	result := database.DB.Model(&model.Operation{}).
		Where("id = ? AND state NOT IN ?", trimmedID, model.OperationTerminalStates()).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var current model.Operation
	if err := database.DB.Select("state").First(&current, "id = ?", trimmedID).Error; err != nil {
		return ErrOperationNotFound
	}
	if !model.IsOperationTerminalState(current.State) {
		return nil
	}
	if ignoreTerminal && model.IsOperationTerminalState(current.State) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrOperationTerminal, current.State)
}

func clampOperationProgress(progress float64) float64 {
	if progress < 0 {
		return 0
	}
	if progress > 100 {
		return 100
	}
	return progress
}
