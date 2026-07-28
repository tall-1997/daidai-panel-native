package mobilecore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"daidai-panel/service"
)

const runtimeLifecycleTimeout = 5 * time.Second

type HealthSnapshot struct {
	Running    bool              `json:"running"`
	Components []ComponentHealth `json:"components"`
}

type ComponentHealth struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	LastError string `json:"lastError,omitempty"`
}

type RuntimeContainer interface {
	Start(context.Context) error
	Stop(context.Context) error
	Health() HealthSnapshot
}

type runtimeComponent struct {
	name   string
	start  func(context.Context) error
	stop   func(context.Context) error
	health func() bool
}

type orderedRuntimeContainer struct {
	mu         sync.Mutex
	components []runtimeComponent
	started    bool
	status     map[string]ComponentHealth
}

func newServiceRuntimeContainer() RuntimeContainer {
	return newOrderedRuntimeContainer([]runtimeComponent{
		{
			name:  "scheduler",
			start: service.StartSchedulerV2,
			stop:  service.StopSchedulerV2,
			health: func() bool {
				return service.GetSchedulerV2() != nil && service.GetTaskExecutor() != nil
			},
		},
		{
			name:  "subscription_scheduler",
			start: service.StartSubscriptionScheduler,
			stop:  service.StopSubscriptionScheduler,
			health: func() bool {
				return service.GetSubscriptionScheduler() != nil
			},
		},
		{
			name:  "backup_scheduler",
			start: service.StartBackupScheduler,
			stop:  service.StopBackupScheduler,
			health: func() bool {
				return service.GetBackupScheduler() != nil
			},
		},
		{
			name:   "log_cleanup",
			start:  service.StartLogCleanup,
			stop:   service.StopLogCleanup,
			health: service.LogCleanupWorkerRunning,
		},
	})
}

func newOrderedRuntimeContainer(components []runtimeComponent) RuntimeContainer {
	status := make(map[string]ComponentHealth, len(components))
	for _, component := range components {
		status[component.name] = ComponentHealth{Name: component.name, State: "stopped"}
	}
	return &orderedRuntimeContainer{components: components, status: status}
}

func (container *orderedRuntimeContainer) Start(ctx context.Context) error {
	container.mu.Lock()
	defer container.mu.Unlock()

	if container.started {
		return nil
	}

	startOrder := make([]runtimeComponent, 0, len(container.components))
	for _, component := range container.components {
		if err := component.start(ctx); err != nil {
			container.status[component.name] = ComponentHealth{Name: component.name, State: "failed", LastError: err.Error()}
			rollbackErr := container.rollbackStarted(ctx, startOrder)
			if rollbackErr != nil {
				return errors.Join(fmt.Errorf("start %s: %w", component.name, err), rollbackErr)
			}
			return fmt.Errorf("start %s: %w", component.name, err)
		}
		container.status[component.name] = ComponentHealth{Name: component.name, State: "running"}
		startOrder = append(startOrder, component)
	}

	container.started = true
	return nil
}

func (container *orderedRuntimeContainer) Stop(ctx context.Context) error {
	container.mu.Lock()
	defer container.mu.Unlock()

	if !container.started {
		return nil
	}

	var errs []error
	for index := len(container.components) - 1; index >= 0; index-- {
		component := container.components[index]
		if err := component.stop(ctx); err != nil {
			container.status[component.name] = ComponentHealth{Name: component.name, State: "failed", LastError: err.Error()}
			errs = append(errs, fmt.Errorf("stop %s: %w", component.name, err))
			continue
		}
		container.status[component.name] = ComponentHealth{Name: component.name, State: "stopped"}
	}

	if len(errs) == 0 {
		container.started = false
		return nil
	}

	return errors.Join(errs...)
}

func (container *orderedRuntimeContainer) Health() HealthSnapshot {
	container.mu.Lock()
	defer container.mu.Unlock()

	components := make([]ComponentHealth, 0, len(container.components))
	for _, component := range container.components {
		health := container.status[component.name]
		if component.health != nil {
			if component.health() {
				health.State = "running"
			} else if health.State == "running" {
				health.State = "stopped"
			}
		}
		components = append(components, health)
	}

	return HealthSnapshot{Running: container.started, Components: components}
}

func (container *orderedRuntimeContainer) rollbackStarted(ctx context.Context, started []runtimeComponent) error {
	var errs []error
	for index := len(started) - 1; index >= 0; index-- {
		component := started[index]
		if err := component.stop(ctx); err != nil {
			container.status[component.name] = ComponentHealth{Name: component.name, State: "failed", LastError: err.Error()}
			errs = append(errs, fmt.Errorf("rollback %s: %w", component.name, err))
			continue
		}
		container.status[component.name] = ComponentHealth{Name: component.name, State: "stopped"}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
