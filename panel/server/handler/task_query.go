package handler

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	panelcron "daidai-panel/pkg/cron"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type taskListFilter struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

type taskListSortRule struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type preparedTaskListItem struct {
	task                   model.Task
	item                   map[string]interface{}
	displayLabels          []string
	subscriptionLabels     []string
	notificationChannelMap map[uint]taskNotificationChannelInfo
}

func (h *TaskHandler) List(c *gin.Context) {
	keyword := c.Query("keyword")
	statusStr := c.Query("status")
	label := c.Query("label")
	filters := parseTaskListFilters(c.Query("filters"))
	sortRules := parseTaskListSortRules(c.Query("sort_rules"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	allRaw := strings.ToLower(strings.TrimSpace(c.Query("all")))
	wantAll := allRaw == "1" || allRaw == "true" || allRaw == "yes"

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := database.DB.Model(&model.Task{})

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR command LIKE ?", like, like)
	}
	if statusStr != "" {
		status, err := strconv.ParseFloat(statusStr, 64)
		if err == nil {
			query = query.Where("status = ?", status)
		}
	}
	if label != "" {
		query = query.Where("labels LIKE ?", "%"+label+"%")
	}

	if len(filters) == 0 && len(sortRules) == 0 {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			response.InternalError(c, "加载任务列表失败")
			return
		}

		ordered := applyDefaultTaskListOrdering(query.Session(&gorm.Session{}))
		var tasks []model.Task
		if wantAll {
			const taskAllSafeLimit = 5000
			if err := ordered.Limit(taskAllSafeLimit).Find(&tasks).Error; err != nil {
				response.InternalError(c, "加载任务列表失败")
				return
			}
			respondTaskList(c, tasks, total, 1, len(tasks))
			return
		}

		if err := ordered.
			Offset((page - 1) * pageSize).
			Limit(pageSize).
			Find(&tasks).Error; err != nil {
			response.InternalError(c, "加载任务列表失败")
			return
		}

		respondTaskList(c, tasks, total, page, pageSize)
		return
	}

	var tasks []model.Task
	if err := query.Find(&tasks).Error; err != nil {
		response.InternalError(c, "加载任务列表失败")
		return
	}

	subscriptionNames := loadSubscriptionNameMap(tasks)
	notificationChannels := loadTaskNotificationChannelMap(tasks)
	prepared := prepareTaskListItems(tasks, subscriptionNames, notificationChannels)
	prepared = filterPreparedTaskListItems(prepared, filters)
	sortPreparedTaskListItems(prepared, sortRules)

	total := int64(len(prepared))
	if wantAll {
		respondPreparedTaskList(c, prepared, total, 1, len(prepared))
		return
	}

	start := (page - 1) * pageSize
	if start > len(prepared) {
		start = len(prepared)
	}
	end := start + pageSize
	if end > len(prepared) {
		end = len(prepared)
	}
	prepared = prepared[start:end]

	respondPreparedTaskList(c, prepared, total, page, pageSize)
}

func applyDefaultTaskListOrdering(query *gorm.DB) *gorm.DB {
	return query.
		// 置顶是用户主动设置的展示优先级，禁用任务如果已经置顶，也必须继续留在置顶区。
		Order("is_pinned DESC").
		Order("CASE WHEN status IN (1, 0.5, 2) THEN 0 WHEN status = 0 THEN 1 ELSE 2 END ASC").
		Order("sort_order ASC").
		Order("created_at DESC").
		Order("id DESC")
}

func respondTaskList(c *gin.Context, tasks []model.Task, total int64, page, pageSize int) {
	subscriptionNames := loadSubscriptionNameMap(tasks)
	notificationChannels := loadTaskNotificationChannelMap(tasks)
	prepared := prepareTaskListItems(tasks, subscriptionNames, notificationChannels)
	respondPreparedTaskList(c, prepared, total, page, pageSize)
}

func respondPreparedTaskList(c *gin.Context, prepared []preparedTaskListItem, total int64, page, pageSize int) {
	data := make([]map[string]interface{}, len(prepared))
	for i := range prepared {
		data[i] = prepared[i].item
	}

	response.Paginated(c, data, total, page, pageSize)
}

func prepareTaskListItems(tasks []model.Task, subscriptionNames map[uint]string, notificationChannels map[uint]taskNotificationChannelInfo) []preparedTaskListItem {
	prepared := make([]preparedTaskListItem, 0, len(tasks))
	for _, task := range tasks {
		displayLabels, subscriptionLabels := buildPreparedTaskLabels(task.GetLabels(), subscriptionNames)

		item := task.ToDict()
		item["display_labels"] = displayLabels
		if task.NotificationChannelID != nil {
			if channel, exists := notificationChannels[*task.NotificationChannelID]; exists {
				item["notification_channel_name"] = channel.Name
				item["notification_channel_enabled"] = channel.Enabled
			}
		}
		if task.Status != model.TaskStatusDisabled && task.UsesCronSchedule() && task.CronExpression != "" {
			nextTimes := panelcron.NextRunTimesForExpressions(task.CronExpression, 1)
			if len(nextTimes) > 0 {
				item["next_run_at"] = nextTimes[0]
			}
		}

		prepared = append(prepared, preparedTaskListItem{
			task:               task,
			item:               item,
			displayLabels:      displayLabels,
			subscriptionLabels: subscriptionLabels,
		})
	}
	return prepared
}

func taskSortGroup(status float64) int {
	switch status {
	case model.TaskStatusEnabled, model.TaskStatusQueued, model.TaskStatusRunning:
		return 0
	case model.TaskStatusDisabled:
		return 1
	default:
		return 2
	}
}

func defaultTaskListLess(left, right model.Task) bool {
	// 置顶优先级要高于任务状态，避免置顶任务一禁用就被普通启用任务挤下去。
	if left.IsPinned != right.IsPinned {
		return left.IsPinned
	}
	leftGroup := taskSortGroup(left.Status)
	rightGroup := taskSortGroup(right.Status)
	if leftGroup != rightGroup {
		return leftGroup < rightGroup
	}
	if left.SortOrder != right.SortOrder {
		return left.SortOrder < right.SortOrder
	}
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	return left.ID > right.ID
}

func loadSubscriptionNameMap(tasks []model.Task) map[uint]string {
	subscriptionIDs := make(map[uint]struct{})
	for _, task := range tasks {
		for _, label := range task.GetLabels() {
			if !strings.HasPrefix(label, "subscription:") {
				continue
			}
			rawID := strings.TrimSpace(strings.TrimPrefix(label, "subscription:"))
			subID, err := strconv.ParseUint(rawID, 10, 32)
			if err != nil {
				continue
			}
			subscriptionIDs[uint(subID)] = struct{}{}
		}
	}

	if len(subscriptionIDs) == 0 {
		return map[uint]string{}
	}

	ids := make([]uint, 0, len(subscriptionIDs))
	for id := range subscriptionIDs {
		ids = append(ids, id)
	}

	var subscriptions []model.Subscription
	database.DB.Model(&model.Subscription{}).
		Where("id IN ?", ids).
		Find(&subscriptions)

	result := make(map[uint]string, len(subscriptions))
	for _, sub := range subscriptions {
		result[sub.ID] = strings.TrimSpace(sub.Name)
	}
	return result
}

func buildTaskDisplayLabels(labels []string, subscriptionNames map[uint]string) []string {
	displayLabels, _ := buildPreparedTaskLabels(labels, subscriptionNames)
	return displayLabels
}

const taskGroupLabelPrefix = "分组:"

func buildPreparedTaskLabels(labels []string, subscriptionNames map[uint]string) ([]string, []string) {
	displayLabels := make([]string, 0, len(labels))
	subscriptionLabels := make([]string, 0, len(labels))
	seen := make(map[string]struct{})
	seenSubscriptions := make(map[string]struct{})
	groupName := ""

	addLabel := func(label string) {
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		if _, exists := seen[label]; exists {
			return
		}
		seen[label] = struct{}{}
		displayLabels = append(displayLabels, label)
	}
	addSubscriptionLabel := func(label string) {
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		if _, exists := seenSubscriptions[label]; exists {
			return
		}
		seenSubscriptions[label] = struct{}{}
		subscriptionLabels = append(subscriptionLabels, label)
	}

	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if strings.HasPrefix(trimmed, taskGroupLabelPrefix) {
			group := strings.TrimSpace(strings.TrimPrefix(trimmed, taskGroupLabelPrefix))
			if group != "" && groupName == "" {
				groupName = group
			}
			continue
		}

		if !strings.HasPrefix(label, "subscription:") {
			addLabel(label)
			continue
		}

		rawID := strings.TrimSpace(strings.TrimPrefix(label, "subscription:"))
		subID, err := strconv.ParseUint(rawID, 10, 32)
		if err != nil {
			continue
		}

		if subName := subscriptionNames[uint(subID)]; subName != "" {
			addLabel(subName)
			addSubscriptionLabel(subName)
			continue
		}

		addLabel("订阅任务")
		addSubscriptionLabel("订阅任务")
	}

	if groupName != "" {
		displayLabels = append([]string{groupName}, displayLabels...)
	}

	return displayLabels, subscriptionLabels
}

func parseTaskListFilters(raw string) []taskListFilter {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var filters []taskListFilter
	if err := json.Unmarshal([]byte(raw), &filters); err != nil {
		return nil
	}

	valid := make([]taskListFilter, 0, len(filters))
	for _, filter := range filters {
		filter.Field = strings.TrimSpace(filter.Field)
		filter.Operator = strings.TrimSpace(filter.Operator)
		filter.Value = strings.TrimSpace(filter.Value)
		if filter.Field == "" || filter.Operator == "" || filter.Value == "" {
			continue
		}
		valid = append(valid, filter)
	}
	return valid
}

func parseTaskListSortRules(raw string) []taskListSortRule {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var rules []taskListSortRule
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil
	}

	valid := make([]taskListSortRule, 0, len(rules))
	for _, rule := range rules {
		rule.Field = strings.TrimSpace(rule.Field)
		rule.Direction = strings.ToLower(strings.TrimSpace(rule.Direction))
		if rule.Field == "" {
			continue
		}
		if rule.Direction != "desc" {
			rule.Direction = "asc"
		}
		valid = append(valid, rule)
	}
	return valid
}

func filterPreparedTaskListItems(items []preparedTaskListItem, filters []taskListFilter) []preparedTaskListItem {
	if len(filters) == 0 {
		return items
	}

	filtered := make([]preparedTaskListItem, 0, len(items))
	for _, item := range items {
		if preparedTaskMatchesFilters(item, filters) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func preparedTaskMatchesFilters(item preparedTaskListItem, filters []taskListFilter) bool {
	for _, filter := range filters {
		values := preparedTaskFilterValues(item, filter.Field)
		if !matchTaskFilterValues(values, filter.Operator, filter.Value) {
			return false
		}
	}
	return true
}

func preparedTaskFilterValues(item preparedTaskListItem, field string) []string {
	switch field {
	case "command":
		return []string{strings.TrimSpace(item.task.Command)}
	case "name":
		return []string{strings.TrimSpace(item.task.Name)}
	case "cron_expression":
		raw := strings.TrimSpace(item.task.CronExpression)
		if raw == "" {
			return nil
		}
		values := make([]string, 0, 2)
		values = append(values, raw)
		for _, expression := range splitTaskCronExpressionLines(raw) {
			values = append(values, expression)
		}
		return values
	case "status":
		return []string{
			strconv.FormatFloat(item.task.Status, 'f', -1, 64),
			taskStatusFilterText(item.task.Status),
			taskStatusFilterAlias(item.task.Status),
		}
	case "labels":
		return item.displayLabels
	case "subscription":
		return item.subscriptionLabels
	default:
		return nil
	}
}

func matchTaskFilterValues(values []string, operator, target string) bool {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		return true
	}

	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			normalized = append(normalized, value)
		}
	}

	switch operator {
	case "contains":
		for _, value := range normalized {
			if strings.Contains(value, target) {
				return true
			}
		}
		return false
	case "not_contains":
		for _, value := range normalized {
			if strings.Contains(value, target) {
				return false
			}
		}
		return true
	case "equals":
		for _, value := range normalized {
			if value == target {
				return true
			}
		}
		return false
	case "not_equals":
		for _, value := range normalized {
			if value == target {
				return false
			}
		}
		return true
	default:
		return true
	}
}

func taskStatusFilterText(status float64) string {
	switch status {
	case model.TaskStatusDisabled:
		return "禁用中"
	case model.TaskStatusQueued:
		return "排队中"
	case model.TaskStatusRunning:
		return "运行中"
	default:
		return "空闲中"
	}
}

func taskStatusFilterAlias(status float64) string {
	switch status {
	case model.TaskStatusDisabled:
		return "已禁用"
	case model.TaskStatusQueued:
		return "排队中"
	case model.TaskStatusRunning:
		return "运行中"
	default:
		return "已启用"
	}
}

func sortPreparedTaskListItems(items []preparedTaskListItem, sortRules []taskListSortRule) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		for _, rule := range sortRules {
			comparison := comparePreparedTaskByRule(left, right, rule)
			if comparison == 0 {
				continue
			}
			if rule.Direction == "desc" {
				return comparison > 0
			}
			return comparison < 0
		}

		return defaultTaskListLess(left.task, right.task)
	})
}

func comparePreparedTaskByRule(left, right preparedTaskListItem, rule taskListSortRule) int {
	switch rule.Field {
	case "name":
		return strings.Compare(strings.ToLower(strings.TrimSpace(left.task.Name)), strings.ToLower(strings.TrimSpace(right.task.Name)))
	case "command":
		return strings.Compare(strings.ToLower(strings.TrimSpace(left.task.Command)), strings.ToLower(strings.TrimSpace(right.task.Command)))
	case "cron_expression":
		return strings.Compare(strings.ToLower(strings.TrimSpace(left.task.CronExpression)), strings.ToLower(strings.TrimSpace(right.task.CronExpression)))
	case "status":
		return compareFloat64(left.task.Status, right.task.Status)
	case "labels":
		return strings.Compare(strings.ToLower(strings.Join(left.displayLabels, ",")), strings.ToLower(strings.Join(right.displayLabels, ",")))
	case "subscription":
		return strings.Compare(strings.ToLower(strings.Join(left.subscriptionLabels, ",")), strings.ToLower(strings.Join(right.subscriptionLabels, ",")))
	case "created_at":
		// 按创建时间比较：早于为 -1、晚于为 1，相等返回 0，direction 由上层 sortPreparedTaskListItems 翻转
		if left.task.CreatedAt.Before(right.task.CreatedAt) {
			return -1
		}
		if left.task.CreatedAt.After(right.task.CreatedAt) {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func compareFloat64(left, right float64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func splitTaskCronExpressionLines(raw string) []string {
	lines := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func (h *TaskHandler) Stats(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	daysStr := c.DefaultQuery("days", "7")
	days, _ := strconv.Atoi(daysStr)
	if days < 1 {
		days = 7
	}

	stats := service.GetTaskStats(uint(taskID), days)
	if stats == nil {
		response.NotFound(c, "任务不存在")
		return
	}
	response.Success(c, stats)
}
