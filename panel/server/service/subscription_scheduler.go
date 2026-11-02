package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	cronu "daidai-panel/pkg/cron"

	"github.com/robfig/cron/v3"
)

type SubscriptionScheduler struct {
	cron     *cron.Cron
	entryMap map[uint]cron.EntryID
	mu       sync.Mutex
}

var (
	globalSubscriptionScheduler *SubscriptionScheduler
	subscriptionPullStateMu     sync.Mutex
	subscriptionPullRunning     = make(map[uint]bool)
	subscriptionPullCancels     = make(map[uint]context.CancelFunc)
)

// ReconcileInterruptedSubscriptionPulls 启动时把上次异常退出留下的 pending/running 拉取标成 unknown，
// 避免操作记录永远停在「进行中」（本机版 Android 提升）。
func ReconcileInterruptedSubscriptionPulls() {
	if database.DB == nil {
		return
	}
	var operations []model.Operation
	if err := database.DB.Where("kind = ? AND state IN ?", model.OperationKindSubscription, []string{model.OperationStatePending, model.OperationStateRunning}).Find(&operations).Error; err != nil {
		log.Printf("subscription pull reconciliation query failed: %v", err)
		return
	}
	store := DefaultOperationStore()
	for _, operation := range operations {
		if err := store.Unknown(operation.ID, "interrupted_pull", operation.LogCursor); err != nil {
			log.Printf("subscription pull reconciliation failed for %s: %v", operation.ID, err)
		}
	}
}

func InitSubscriptionScheduler() {
	ReconcileInterruptedSubscriptionPulls()
	s := &SubscriptionScheduler{
		cron:     cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		entryMap: make(map[uint]cron.EntryID),
	}

	var subs []model.Subscription
	database.DB.Where("enabled = ? AND schedule != ''", true).Find(&subs)
	for i := range subs {
		if err := s.AddOrUpdateJob(&subs[i]); err != nil {
			log.Printf("failed to add subscription job %d: %v", subs[i].ID, err)
		}
	}

	s.cron.Start()
	globalSubscriptionScheduler = s
	log.Printf("subscription scheduler initialized with %d jobs", len(subs))
}

// ShutdownSubscriptionScheduler 停止订阅定时拉取：立刻不再触发；正在跑的那次最多等到 deadline，不主动取消
// （git stash push 与 pop 之间被打断会把用户改动留在 stash 里，能跑完就让它跑完）。
// deadline 由面板关停流程统一给出，与备份调度共用，不再各写死一个时长。
func ShutdownSubscriptionScheduler(deadline time.Time) {
	if globalSubscriptionScheduler == nil {
		return
	}

	ctx := globalSubscriptionScheduler.cron.Stop()
	// 至少留 1 毫秒，理由同 ShutdownBackupScheduler。
	wait := time.Until(deadline)
	if wait < time.Millisecond {
		wait = time.Millisecond
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		log.Println("subscription scheduler stopped")
	case <-timer.C:
		log.Println("subscription scheduler stopped (a scheduled pull is still running, not waiting for it)")
	}
}

func GetSubscriptionScheduler() *SubscriptionScheduler {
	return globalSubscriptionScheduler
}

func IsSubscriptionPullRunning(subID uint) bool {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	return subscriptionPullRunning[subID]
}

func beginSubscriptionPull(subID uint) (context.Context, bool) {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	if subscriptionPullRunning[subID] {
		return nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscriptionPullRunning[subID] = true
	subscriptionPullCancels[subID] = cancel
	return ctx, true
}

func finishSubscriptionPull(subID uint) {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	if cancel, exists := subscriptionPullCancels[subID]; exists {
		cancel()
		delete(subscriptionPullCancels, subID)
	}
	delete(subscriptionPullRunning, subID)
}

func StopSubscriptionPull(subID uint) bool {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()

	cancel, exists := subscriptionPullCancels[subID]
	if !exists {
		return false
	}
	cancel()
	return true
}

func ExecuteSubscriptionPull(sub *model.Subscription, onOutput PullCallback) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("订阅不存在")
	}
	ctx, ok := beginSubscriptionPull(sub.ID)
	if !ok {
		return "", fmt.Errorf("该订阅正在拉取中")
	}
	defer finishSubscriptionPull(sub.ID)

	// 本机版（Android）：为每次拉取建一条操作记录，SubLog 一并带上 operation_id。
	operationID := fmt.Sprintf("subscription_%d_%d", sub.ID, time.Now().UnixNano())
	operation, opErr := DefaultOperationStore().Create(OperationCreateOptions{
		ID:       operationID,
		Kind:     model.OperationKindSubscription,
		Phase:    "queued",
		Progress: 0,
	})
	if opErr != nil {
		return "", opErr
	}
	return pullSubscriptionWithOperation(ctx, sub, onOutput, operation.ID)
}

func (s *SubscriptionScheduler) AddOrUpdateJob(sub *model.Subscription) error {
	if s == nil || sub == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if oldID, exists := s.entryMap[sub.ID]; exists {
		s.cron.Remove(oldID)
		delete(s.entryMap, sub.ID)
	}

	if !sub.Enabled || strings.TrimSpace(sub.Schedule) == "" {
		return nil
	}

	schedule, err := cronu.ParseSchedule(sub.Schedule)
	if err != nil {
		return fmt.Errorf("invalid subscription schedule")
	}

	subID := sub.ID
	entryID := s.cron.Schedule(schedule, cron.FuncJob(func() {
		var latest model.Subscription
		if err := database.DB.First(&latest, subID).Error; err != nil {
			log.Printf("subscription %d not found: %v", subID, err)
			return
		}
		if !latest.Enabled {
			return
		}
		if _, err := ExecuteSubscriptionPull(&latest, nil); err != nil {
			log.Printf("subscription %d scheduled pull skipped: %v", subID, err)
		}
	}))

	s.entryMap[sub.ID] = entryID
	return nil
}

func (s *SubscriptionScheduler) RemoveJob(subID uint) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entryMap[subID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryMap, subID)
	}
}

func (s *SubscriptionScheduler) ReloadAllJobs() {
	if s == nil {
		return
	}

	s.mu.Lock()
	for subID, entryID := range s.entryMap {
		s.cron.Remove(entryID)
		delete(s.entryMap, subID)
	}
	s.mu.Unlock()

	var subs []model.Subscription
	database.DB.Where("enabled = ? AND schedule != ''", true).Find(&subs)
	for i := range subs {
		if err := s.AddOrUpdateJob(&subs[i]); err != nil {
			log.Printf("reload subscription job failed for %d: %v", subs[i].ID, err)
		}
	}
}

func ValidateSubscriptionSchedule(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	_, err := cronu.ParseSchedule(expr)
	return err == nil
}
