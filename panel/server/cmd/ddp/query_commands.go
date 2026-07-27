package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

func runTaskList(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	statusFilter := ""
	keyword := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 >= len(args) {
				return fmt.Errorf("--status 需要参数")
			}
			statusFilter = strings.TrimSpace(args[i+1])
			i++
		case "--keyword":
			if i+1 >= len(args) {
				return fmt.Errorf("--keyword 需要参数")
			}
			keyword = strings.TrimSpace(args[i+1])
			i++
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}

	query := database.DB.Model(&model.Task{})
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR command LIKE ?", like, like)
	}

	var tasks []model.Task
	if err := query.Find(&tasks).Error; err != nil {
		return err
	}

	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].IsPinned != tasks[j].IsPinned {
			return tasks[i].IsPinned
		}
		if tasks[i].SortOrder != tasks[j].SortOrder {
			return tasks[i].SortOrder < tasks[j].SortOrder
		}
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	printed := 0
	for _, task := range tasks {
		if statusFilter != "" && !taskStatusMatches(task.Status, statusFilter) {
			continue
		}
		fmt.Printf("[%d] %s %s\n", task.ID, taskStatusText(task.Status), task.Name)
		fmt.Printf("    command: %s\n", truncateText(strings.TrimSpace(task.Command), 200))
		if cron := strings.TrimSpace(task.CronExpression); cron != "" {
			fmt.Printf("    cron: %s\n", cron)
		}
		printed++
	}

	if printed == 0 {
		fmt.Println("当前没有匹配的任务")
	}
	return nil
}

func runTaskLogs(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp task logs <任务ID或名称> [--lines N]")
	}

	lines := 120
	identifier := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lines":
			if i+1 >= len(args) {
				return fmt.Errorf("--lines 需要参数")
			}
			parsed, err := strconv.Atoi(args[i+1])
			if err != nil || parsed <= 0 {
				return fmt.Errorf("无效的日志行数: %s", args[i+1])
			}
			lines = parsed
			i++
		default:
			if identifier != "" {
				return fmt.Errorf("只能指定一个任务标识")
			}
			identifier = args[i]
		}
	}
	if identifier == "" {
		return fmt.Errorf("缺少任务标识")
	}

	task, err := findTask(identifier)
	if err != nil {
		return err
	}

	var taskLog model.TaskLog
	if err := database.DB.Where("task_id = ?", task.ID).Order("started_at DESC").First(&taskLog).Error; err != nil {
		return fmt.Errorf("任务暂无执行日志")
	}

	output := latestTaskLogOutput(taskLog)
	if output == "" {
		fmt.Println("该任务最近一次执行没有可读取的日志内容")
		return nil
	}

	for _, line := range tailLines(output, lines) {
		fmt.Println(line)
	}
	return nil
}

func runSubscriptionList(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	subType := ""
	keyword := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--type":
			if i+1 >= len(args) {
				return fmt.Errorf("--type 需要参数")
			}
			subType = strings.TrimSpace(args[i+1])
			i++
		case "--keyword":
			if i+1 >= len(args) {
				return fmt.Errorf("--keyword 需要参数")
			}
			keyword = strings.TrimSpace(args[i+1])
			i++
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}

	query := database.DB.Model(&model.Subscription{}).Order("created_at DESC")
	if subType != "" {
		query = query.Where("type = ?", subType)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR url LIKE ?", like, like)
	}

	var subs []model.Subscription
	if err := query.Find(&subs).Error; err != nil {
		return err
	}
	if len(subs) == 0 {
		fmt.Println("当前没有匹配的订阅")
		return nil
	}

	for _, sub := range subs {
		lastPullAt := "-"
		if sub.LastPullAt != nil {
			lastPullAt = sub.LastPullAt.Format("2006-01-02 15:04:05")
		}
		fmt.Printf("[%d] %s %s (%s)\n", sub.ID, boolLabel(sub.Enabled, "启用", "禁用"), sub.Name, sub.Type)
		fmt.Printf("    url: %s\n", sub.URL)
		if strings.TrimSpace(sub.Schedule) != "" {
			fmt.Printf("    schedule: %s\n", sub.Schedule)
		}
		fmt.Printf("    last_pull: %s\n", lastPullAt)
	}
	return nil
}

func runSubscriptionLogs(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp sub logs <订阅ID或名称> [--lines N]")
	}

	lines := 120
	identifier := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lines":
			if i+1 >= len(args) {
				return fmt.Errorf("--lines 需要参数")
			}
			parsed, err := strconv.Atoi(args[i+1])
			if err != nil || parsed <= 0 {
				return fmt.Errorf("无效的日志行数: %s", args[i+1])
			}
			lines = parsed
			i++
		default:
			if identifier != "" {
				return fmt.Errorf("只能指定一个订阅标识")
			}
			identifier = args[i]
		}
	}
	if identifier == "" {
		return fmt.Errorf("缺少订阅标识")
	}

	sub, err := findSubscription(identifier)
	if err != nil {
		return err
	}

	var logItem model.SubLog
	if err := database.DB.Where("subscription_id = ?", sub.ID).Order("created_at DESC").First(&logItem).Error; err != nil {
		return fmt.Errorf("订阅暂无拉取日志")
	}

	content := strings.TrimSpace(logItem.Content)
	if content == "" {
		fmt.Println("该订阅最近一次拉取没有可读取的日志内容")
		return nil
	}

	for _, line := range tailLines(content, lines) {
		fmt.Println(line)
	}
	return nil
}

func tailLines(text string, lines int) []string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimRight(text, "\n"), "\r\n", "\n"), "\n")
	if lines > 0 && len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return parts
}

func taskStatusMatches(status float64, raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "running", "run":
		return status == model.TaskStatusRunning
	case "queued", "queue":
		return status == model.TaskStatusQueued
	case "disabled", "disable", "off":
		return status == model.TaskStatusDisabled
	case "enabled", "enable", "idle", "ready":
		return status == model.TaskStatusEnabled
	default:
		return false
	}
}
