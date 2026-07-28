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
	subscriptionSchedulerMu     sync.Mutex
	subscriptionSchedulerOn     bool
	subscriptionPullStateMu     sync.Mutex
	subscriptionPullRunning     = make(map[uint]string)
	subscriptionPullCancels     = make(map[uint]context.CancelFunc)
)

func StartSubscriptionScheduler(ctx context.Context) error {
	_ = ctx
	subscriptionSchedulerMu.Lock()
	defer subscriptionSchedulerMu.Unlock()

	if subscriptionSchedulerOn {
		return nil
	}
	ReconcileInterruptedSubscriptionPulls()

	s := &SubscriptionScheduler{
		cron:     cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		entryMap: make(map[uint]cron.EntryID),
	}

	var subs []model.Subscription
	database.DB.Where("enabled = ? AND schedule != ''", true).Find(&subs)
	for i := range subs {
		if err := s.AddOrUpdateJob(&subs[i]); err != nil {
			return fmt.Errorf("failed to add subscription job %d: %w", subs[i].ID, err)
		}
	}

	s.cron.Start()
	globalSubscriptionScheduler = s
	subscriptionSchedulerOn = true
	log.Printf("subscription scheduler initialized with %d jobs", len(subs))
	return nil
}

func StopSubscriptionScheduler(ctx context.Context) error {
	subscriptionSchedulerMu.Lock()
	defer subscriptionSchedulerMu.Unlock()

	if !subscriptionSchedulerOn {
		return nil
	}

	if globalSubscriptionScheduler != nil {
		stopCtx := globalSubscriptionScheduler.cron.Stop()
		if ctx == nil {
			<-stopCtx.Done()
		} else {
			select {
			case <-stopCtx.Done():
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	globalSubscriptionScheduler = nil
	subscriptionSchedulerOn = false
	log.Println("subscription scheduler stopped")
	return nil
}

func InitSubscriptionScheduler() {
	if err := StartSubscriptionScheduler(context.Background()); err != nil {
		log.Printf("subscription scheduler start failed: %v", err)
	}
}

func ShutdownSubscriptionScheduler() {
	if err := StopSubscriptionScheduler(context.Background()); err != nil {
		log.Printf("subscription scheduler stop failed: %v", err)
	}
}

func GetSubscriptionScheduler() *SubscriptionScheduler {
	return globalSubscriptionScheduler
}

func IsSubscriptionPullRunning(subID uint) bool {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	_, ok := subscriptionPullRunning[subID]
	return ok
}

func CurrentSubscriptionPullOperationID(subID uint) string {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	return subscriptionPullRunning[subID]
}

func BeginSubscriptionPull(subID uint) (context.Context, *model.Operation, error) {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	if _, ok := subscriptionPullRunning[subID]; ok {
		return nil, nil, fmt.Errorf("该订阅正在拉取中")
	}

	operationID := fmt.Sprintf("subscription_%d_%d", subID, time.Now().UnixNano())
	operation, err := DefaultOperationStore().Create(OperationCreateOptions{
		ID:       operationID,
		Kind:     model.OperationKindSubscription,
		Phase:    "queued",
		Progress: 0,
	})
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	subscriptionPullRunning[subID] = operationID
	subscriptionPullCancels[subID] = cancel
	return ctx, operation, nil
}

func FinishSubscriptionPull(subID uint) {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()
	if cancel, exists := subscriptionPullCancels[subID]; exists {
		cancel()
		delete(subscriptionPullCancels, subID)
	}
	delete(subscriptionPullRunning, subID)
}

func StopSubscriptionPull(subID uint) bool {
	_, ok := StopSubscriptionPullWithOperation(subID)
	return ok
}

func StopSubscriptionPullWithOperation(subID uint) (string, bool) {
	subscriptionPullStateMu.Lock()
	defer subscriptionPullStateMu.Unlock()

	cancel, exists := subscriptionPullCancels[subID]
	if !exists {
		return "", false
	}
	operationID := subscriptionPullRunning[subID]
	cancel()
	return operationID, true
}

func ExecuteSubscriptionPull(sub *model.Subscription, onOutput PullCallback) (string, error) {
	if sub == nil {
		return "", fmt.Errorf("订阅不存在")
	}
	ctx, operation, err := BeginSubscriptionPull(sub.ID)
	if err != nil {
		return "", err
	}
	defer FinishSubscriptionPull(sub.ID)

	return PullSubscriptionWithOperation(ctx, sub, onOutput, operation.ID)
}

func PullSubscriptionWithOperation(ctx context.Context, sub *model.Subscription, onOutput PullCallback, operationID string) (string, error) {
	return pullSubscriptionWithContext(ctx, sub, onOutput, operationID)
}

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
