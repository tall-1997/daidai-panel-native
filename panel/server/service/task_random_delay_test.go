package service

import (
	"testing"

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
	// 随机延迟只对定时(cron)任务生效；手动、开机等其它来源都应立即执行、跳过延迟。
	cases := []struct {
		trigger string
		want    bool
	}{
		{TriggerTypeCron, true},
		{TriggerTypeManual, false},
		{TriggerTypeStartup, false},
		{"", false},
	}
	for _, c := range cases {
		if got := shouldApplyRandomDelayForTrigger(c.trigger); got != c.want {
			t.Fatalf("trigger %q: expected %v, got %v", c.trigger, c.want, got)
		}
	}
}
