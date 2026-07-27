package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

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

func InitSubscriptionScheduler() {
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

func ShutdownSubscriptionScheduler() {
	if globalSubscriptionScheduler == nil {
		return
	}

	ctx := globalSubscriptionScheduler.cron.Stop()
	<-ctx.Done()
	log.Println("subscription scheduler stopped")
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

	return PullSubscriptionWithContext(ctx, sub, onOutput)
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
