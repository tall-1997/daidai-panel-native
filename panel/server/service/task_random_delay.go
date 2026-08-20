package service

import "daidai-panel/model"

// shouldApplyRandomDelayForTrigger 判断某次执行是否应当应用随机延迟。
// 定时(cron)与开机自启(startup)都属于无人值守的自动触发，需要随机延迟错峰；
// 只有手动执行才跳过延迟，避免用户手点后还要干等。
func shouldApplyRandomDelayForTrigger(triggerType string) bool {
	return triggerType == TriggerTypeCron || triggerType == TriggerTypeStartup
}

func resolveTaskRandomDelaySeconds(task *model.Task, plan *CommandExecutionPlan) int {
	if task == nil {
		return 0
	}
	if plan != nil && plan.SkipRandomDelay {
		return 0
	}

	if task.RandomDelaySeconds != nil {
		if *task.RandomDelaySeconds <= 0 {
			return 0
		}
		return *task.RandomDelaySeconds
	}

	randomDelay := model.GetRegisteredConfigInt("random_delay")
	if randomDelay <= 0 {
		return 0
	}

	delayExts := parseTaskExtensions(model.GetRegisteredConfig("random_delay_extensions"))
	if !shouldApplyRandomDelay(task.Command, delayExts) {
		return 0
	}

	return randomDelay
}
