package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestResolveTaskRandomDelaySeconds(t *testing.T) {
	testutil.SetupTestEnv(t)

	model.SetConfig("random_delay", "15")
	model.SetConfig("random_delay_extensions", ".py,.sh")

	t.Run("inherits global delay when task value is nil and extension matches", func(t *testing.T) {
		task := &model.Task{Command: "task demo.py"}
		if got := resolveTaskRandomDelaySeconds(task, nil); got != 15 {
			t.Fatalf("expected inherited delay 15, got %d", got)
		}
	})

	t.Run("skips inherited delay when extension does not match", func(t *testing.T) {
		task := &model.Task{Command: "task demo.js"}
		if got := resolveTaskRandomDelaySeconds(task, nil); got != 0 {
			t.Fatalf("expected inherited delay 0, got %d", got)
		}
	})

	t.Run("explicit zero disables random delay", func(t *testing.T) {
		zero := 0
		task := &model.Task{Command: "task demo.py", RandomDelaySeconds: &zero}
		if got := resolveTaskRandomDelaySeconds(task, nil); got != 0 {
			t.Fatalf("expected explicit disable to return 0, got %d", got)
		}
	})

	t.Run("custom task value overrides global delay", func(t *testing.T) {
		custom := 42
		task := &model.Task{Command: "echo demo", RandomDelaySeconds: &custom}
		if got := resolveTaskRandomDelaySeconds(task, nil); got != 42 {
			t.Fatalf("expected custom delay 42, got %d", got)
		}
	})

	t.Run("now mode skips inherited delay", func(t *testing.T) {
		task := &model.Task{Command: "task demo.py now"}
		plan := &CommandExecutionPlan{SkipRandomDelay: true}
		if got := resolveTaskRandomDelaySeconds(task, plan); got != 0 {
			t.Fatalf("expected now mode to skip delay, got %d", got)
		}
	})
}

func TestShouldApplyRandomDelayForTrigger(t *testing.T) {
	// A5：定时与开机都是无人值守的自动触发，需要随机延迟错峰；只有手动执行立即运行。
	cases := []struct {
		trigger string
		want    bool
	}{
		{TriggerTypeCron, true},
		{TriggerTypeStartup, true},
		{TriggerTypeManual, false},
		{"", false},
	}
	for _, c := range cases {
		if got := shouldApplyRandomDelayForTrigger(c.trigger); got != c.want {
			t.Fatalf("trigger %q: expected %v, got %v", c.trigger, c.want, got)
		}
	}
}

func mustWriteDelayTestScript(t *testing.T, name string) {
	t.Helper()

	path := filepath.Join(config.C.Data.ScriptsDir, name)
	if err := os.WriteFile(path, []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
}

// A5 补充：开机任务经由执行器解析出的实际延迟必须大于 0，手动执行必须为 0。
func TestTaskExecutorResolveExecutionDelayByTrigger(t *testing.T) {
	testutil.SetupTestEnv(t)
	mustWriteDelayTestScript(t, "demo.py")

	model.SetConfig("random_delay", "15")
	model.SetConfig("random_delay_extensions", ".py,.sh")

	executor := NewTaskExecutor()
	task := &model.Task{ID: 1, Command: "task demo.py"}

	for _, c := range []struct {
		trigger  string
		wantWait bool
	}{
		{TriggerTypeCron, true},
		{TriggerTypeStartup, true},
		{TriggerTypeManual, false},
	} {
		req := &ExecutionRequest{TaskID: task.ID, Task: task, TriggerType: c.trigger}
		got := executor.ResolveExecutionDelay(req)
		if c.wantWait && got <= 0 {
			t.Fatalf("trigger %q: expected a positive delay, got %s", c.trigger, got)
		}
		if !c.wantWait && got != 0 {
			t.Fatalf("trigger %q: expected no delay, got %s", c.trigger, got)
		}
		if got > 15*time.Second {
			t.Fatalf("trigger %q: delay %s exceeds configured upper bound", c.trigger, got)
		}
	}
}

// 显式关闭随机延迟的任务不应被开机触发重新引入等待。
func TestTaskExecutorResolveExecutionDelayRespectsExplicitZero(t *testing.T) {
	testutil.SetupTestEnv(t)
	mustWriteDelayTestScript(t, "demo.py")

	model.SetConfig("random_delay", "15")
	model.SetConfig("random_delay_extensions", ".py,.sh")

	zero := 0
	task := &model.Task{ID: 2, Command: "task demo.py", RandomDelaySeconds: &zero}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TriggerType: TriggerTypeStartup}

	if got := NewTaskExecutor().ResolveExecutionDelay(req); got != 0 {
		t.Fatalf("expected explicit zero to disable delay, got %s", got)
	}
}
