package service

import "daidai-panel/model"

// shouldApplyRandomDelayForTrigger 判断某次执行是否应当应用随机延迟。
// 随机延迟只对定时(cron)任务生效；手动执行与开机自启一律立即运行、跳过延迟。
func shouldApplyRandomDelayForTrigger(triggerType string) bool {
	return triggerType == TriggerTypeCron
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
