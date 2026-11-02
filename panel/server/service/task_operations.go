package service

import (
	"fmt"
	"sync"
	"time"

	"daidai-panel/model"
)

// Task operations：本机版（Android）把每次任务手动运行自动停止也纳入操作记录，
// 与依赖安装共用同一套 OperationStore，移动端回归测试靠它串起任务生命周期。

type taskOperationMap struct {
	mu      sync.Mutex
	pending map[uint][]string // taskID → 排队中但还没被执行器认领的操作
	current map[uint]string   // taskID → 执行器当前认领的操作
}

var taskOperations = &taskOperationMap{
	pending: make(map[uint][]string),
	current: make(map[uint]string),
}

func NewTaskRunOperation(taskID uint) (string, error) {
	operation, err := DefaultOperationStore().Create(OperationCreateOptions{
		ID:       fmt.Sprintf("task_%d_%d", taskID, time.Now().UnixNano()),
		Kind:     model.OperationKindTask,
		Phase:    "queued",
		Progress: 0,
	})
	if err != nil {
		return "", err
	}
	taskOperations.mu.Lock()
	taskOperations.pending[taskID] = append(taskOperations.pending[taskID], operation.ID)
	taskOperations.mu.Unlock()
	return operation.ID, nil
}

func ClaimTaskRunOperation(taskID uint) string {
	taskOperations.mu.Lock()
	defer taskOperations.mu.Unlock()

	pending := taskOperations.pending[taskID]
	if len(pending) > 0 {
		operationID := pending[0]
		if len(pending) == 1 {
			delete(taskOperations.pending, taskID)
		} else {
			taskOperations.pending[taskID] = pending[1:]
		}
		taskOperations.current[taskID] = operationID
		return operationID
	}
	return ""
}

// setCurrentTaskOperation 把执行器认领的操作记为任务当前执行。
func setCurrentTaskOperation(taskID uint, operationID string) {
	if operationID == "" {
		return
	}
	taskOperations.mu.Lock()
	taskOperations.current[taskID] = operationID
	taskOperations.mu.Unlock()
}

// clearTaskOperation 清掉任务当前执行记录（保留 pending 队列）。
func clearTaskOperation(taskID uint, operationID string) {
	taskOperations.mu.Lock()
	if taskOperations.current[taskID] == operationID {
		delete(taskOperations.current, taskID)
	}
	taskOperations.mu.Unlock()
}

func CurrentTaskOperationID(taskID uint) string {
	taskOperations.mu.Lock()
	defer taskOperations.mu.Unlock()

	if operationID := taskOperations.current[taskID]; operationID != "" {
		return operationID
	}
	if pending := taskOperations.pending[taskID]; len(pending) > 0 {
		return pending[0]
	}
	return ""
}

func ClearTaskOperationForTask(taskID uint, operationID string) {
	taskOperations.mu.Lock()
	defer taskOperations.mu.Unlock()

	pending := taskOperations.pending[taskID]
	for i, id := range pending {
		if id != operationID {
			continue
		}
		pending = append(pending[:i], pending[i+1:]...)
		if len(pending) == 0 {
			delete(taskOperations.pending, taskID)
		} else {
			taskOperations.pending[taskID] = pending
		}
		break
	}
	if taskOperations.current[taskID] == operationID {
		delete(taskOperations.current, taskID)
	}
}

func CancelTaskOperation(taskID uint, errorCode string) string {
	operationID := CurrentTaskOperationID(taskID)
	if operationID == "" {
		return ""
	}
	_ = DefaultOperationStore().Cancel(operationID, errorCode, 0)
	ClearTaskOperationForTask(taskID, operationID)
	return operationID
}

// RecordTaskControlOperation 为没有 pending/current 运行记录的控制动作兜底建一条终态操作。
func RecordTaskControlOperation(kind string, taskID uint, state string, exitCode int, errorCode string) string {
	operation, err := DefaultOperationStore().Create(OperationCreateOptions{
		ID:       fmt.Sprintf("%s_%d_%d", kind, taskID, time.Now().UnixNano()),
		Kind:     model.OperationKindTask,
		Phase:    kind,
		Progress: 0,
	})
	if err != nil {
		return ""
	}
	operationID := operation.ID
	_ = DefaultOperationStore().Start(operationID, kind)
	switch state {
	case model.OperationStateSuccess:
		_ = DefaultOperationStore().Finish(operationID, exitCode, 0)
	case model.OperationStateFailed:
		code := exitCode
		_ = DefaultOperationStore().Fail(operationID, &code, errorCode, 0)
	case model.OperationStateCanceled:
		_ = DefaultOperationStore().Cancel(operationID, errorCode, 0)
	default:
		_ = DefaultOperationStore().Unknown(operationID, errorCode, 0)
	}
	return operationID
}

// EndTaskOperation 任务结束时按结果结束操作记录；已在终态的操作不再改动。
func EndTaskOperation(taskID uint, success bool, exitCode int, errorCode string) {
	operationID := CurrentTaskOperationID(taskID)
	if operationID == "" {
		return
	}
	if success {
		_ = DefaultOperationStore().Finish(operationID, exitCode, 0)
	} else {
		code := exitCode
		_ = DefaultOperationStore().Fail(operationID, &code, errorCode, 0)
	}
	ClearTaskOperationForTask(taskID, operationID)
}