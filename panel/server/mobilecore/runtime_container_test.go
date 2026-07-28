package mobilecore

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRuntimeContainerStartStopOrder(t *testing.T) {
	trace := make([]string, 0, 6)
	mu := sync.Mutex{}
	push := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		trace = append(trace, event)
	}

	container := newOrderedRuntimeContainer([]runtimeComponent{
		{name: "scheduler", start: func(context.Context) error { push("start:scheduler"); return nil }, stop: func(context.Context) error { push("stop:scheduler"); return nil }},
		{name: "subscription", start: func(context.Context) error { push("start:subscription"); return nil }, stop: func(context.Context) error { push("stop:subscription"); return nil }},
		{name: "backup", start: func(context.Context) error { push("start:backup"); return nil }, stop: func(context.Context) error { push("stop:backup"); return nil }},
	})

	if err := container.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := container.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	want := []string{"start:scheduler", "start:subscription", "start:backup", "stop:backup", "stop:subscription", "stop:scheduler"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("trace=%v want=%v", trace, want)
	}
}

func TestRuntimeContainerPartialStartRollback(t *testing.T) {
	trace := make([]string, 0, 4)
	mu := sync.Mutex{}
	push := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		trace = append(trace, event)
	}

	container := newOrderedRuntimeContainer([]runtimeComponent{
		{name: "scheduler", start: func(context.Context) error { push("start:scheduler"); return nil }, stop: func(context.Context) error { push("stop:scheduler"); return nil }},
		{name: "subscription", start: func(context.Context) error { push("start:subscription"); return errors.New("boom") }, stop: func(context.Context) error { push("stop:subscription"); return nil }},
	})

	err := container.Start(context.Background())
	if err == nil {
		t.Fatal("expected start failure")
	}
	want := []string{"start:scheduler", "start:subscription", "stop:scheduler"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("trace=%v want=%v", trace, want)
	}
	health := container.Health()
	if health.Running {
		t.Fatal("container marked running after rollback")
	}
}

func TestRuntimeContainerRestartAndIdempotent(t *testing.T) {
	starts := 0
	stops := 0
	container := newOrderedRuntimeContainer([]runtimeComponent{
		{
			name: "scheduler",
			start: func(context.Context) error {
				starts++
				return nil
			},
			stop: func(context.Context) error {
				stops++
				return nil
			},
		},
	})

	if err := container.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := container.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := container.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := container.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := container.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := container.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	if starts != 2 || stops != 2 {
		t.Fatalf("starts=%d stops=%d", starts, stops)
	}
}

func TestRuntimeContainerActiveInterruptionPath(t *testing.T) {
	active := make(chan struct{})
	interrupted := make(chan struct{})
	container := newOrderedRuntimeContainer([]runtimeComponent{
		{
			name: "scheduler",
			start: func(context.Context) error {
				close(active)
				return nil
			},
			stop: func(context.Context) error {
				close(interrupted)
				return nil
			},
		},
	})

	if err := container.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-active

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := container.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-interrupted:
	case <-time.After(time.Second):
		t.Fatal("active work interruption not observed")
	}
}
