package handler

import (
	"cmp"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

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
	// nextRunAt 是 prepareTaskListItems 里算 next_run_at 时顺手留下的同一份快照。
	// 下次运行不是数据库列，只能在响应期按 cron 现算，所以按它排序只能走内存路径；
	// 存这一份是为了排序时绝不第二次解析 cron —— 再算一次不但更慢，
	// 拿到的时间点还可能已经跨过一个周期，导致排序结果和下发给前端的 next_run_at 对不上。
	// 值为 nil 表示「算不出下次运行」（已禁用 / 非 cron 任务 / cron 表达式为空）。
	nextRunAt *time.Time
	// firstRunSecond 是按「定时规则」（sort_rules 的 cron_expression）排序用的键（issue #151）：
	// 这条任务一天之内最早一次触发的时刻，距 0 点的秒数，取法见 panelcron.FirstRunSecondOfDay。
	// 和 nextRunAt 一样在预处理时算一次，排序比较器里绝不再解析 cron。
	// 值为 nil 表示没有时刻可排（手动 / 开机任务、表达式为空、2 月 30 日这种永不触发）。
	// 与 nextRunAt 不同，禁用任务照样有值：它们在禁用分区内部也按时刻排，重新启用前方便查时间冲突。
	firstRunSecond *int
	// groupName 是这条任务的分组名（第一个非空的 `分组:` 标签，取法见 taskGroupNameFromLabels），
	// 视图筛选 / 排序的 group 字段只认它（#130）。刻意不从 displayLabels 里取：
	// 分组名在那里排第 0 位，和自定义标签、订阅名混在一起，按 labels 等于 X 会误中同名的标签。
	// 空串表示没有分组。
	groupName string
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
		// 运行中的任务提到【本区】最前（issue #118）：正在跑的那几条是用户此刻唯一想看的，
		// 而它们平时散落在几百条任务里，得靠工具栏的「运行中」页签才能捞出来。
		//
		// 只提 status = 2（运行中），不提 0.5（排队中）：排队是几秒钟就过去的瞬时态，
		// 一并提上来只会让同一次运行多跳一次（排队跳一次、转运行再跳一次）。
		//
		// 这一层【不能】并进上面的 taskSortGroup：那个函数同时被 task_sort.go 的拖拽落点校验复用，
		// 一旦把运行中拆成独立的桶，(1) 列表顶部本来就是最常见的落点，跨桶 400 会明显变多；
		// (2) 桶还会随任务自己起跑/跑完而漂移 —— 用户在原桶里拖拽时看不到正在跑的那条，
		// 等它跑完回到本桶，list_order 已经是按缺了它的序列重编过的陈旧值。
		// 排序要分区、拖拽不要分区，语义不同，所以只在排序这一侧多加一层，别顺手统一。
		Order("CASE WHEN status = 2 THEN 0 ELSE 1 END ASC").
		// 拖拽顺序插在 sort_order 之前：拖拽是用户对这一列表最直接的表达，
		// 而 sort_order 是开机执行顺序，只在没拖过的桶里继续兜底。
		// 存量数据 list_order 全是 0，这一层比较等价于不存在，升级后顺序不变。
		Order("list_order ASC").
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

// taskDictWithEnabledSwitch 在 ToDict 之上补一个 enabled：启用开关位，与运行态无关（issue #133，契约 C1）。
// 禁用中的任务被手动运行时 status 同样会走到 0.5 / 2，前端光看 status 分不出开关是开是关。
// ToDict 在 model 包里、不能反向调 service，所以这一步只能放在 handler；
// 任务列表与启用 / 禁用接口的响应都走这里，免得各写一份口径。
func taskDictWithEnabledSwitch(task *model.Task) map[string]interface{} {
	item := task.ToDict()
	item["enabled"] = service.ResolveTaskEnabledSwitch(task)
	return item
}

func prepareTaskListItems(tasks []model.Task, subscriptionNames map[uint]string, notificationChannels map[uint]taskNotificationChannelInfo) []preparedTaskListItem {
	prepared := make([]preparedTaskListItem, 0, len(tasks))
	// FirstRunSecondOfDay 只拿它定位「今天 0 点」，算出的时刻与哪一天无关，整批共用一个即可。
	now := time.Now()
	for _, task := range tasks {
		displayLabels, subscriptionLabels := buildPreparedTaskLabels(task.GetLabels(), subscriptionNames)

		item := taskDictWithEnabledSwitch(&task)
		item["display_labels"] = displayLabels
		// 订阅名再单独下发一份，供前端「按类别隐藏标签」区分订阅标签与自定义标签：
		// display_labels 是扁平数组，订阅名和自定义标签在里面长得一模一样，前端分不出来。
		// 值与下面视图筛选/排序用的 subscriptionLabels 同源，不会出现两套口径。
		// buildPreparedTaskLabels 用 make 初始化两份切片，空值序列化成 []（不是 null），
		// 与 display_labels 一致，前端可直接当数组用。
		item["subscription_labels"] = subscriptionLabels
		if task.NotificationChannelID != nil {
			if channel, exists := notificationChannels[*task.NotificationChannelID]; exists {
				item["notification_channel_name"] = channel.Name
				item["notification_channel_enabled"] = channel.Enabled
			}
		}
		var nextRunAt *time.Time
		if task.Status != model.TaskStatusDisabled && task.UsesCronSchedule() && task.CronExpression != "" {
			nextTimes := panelcron.NextRunTimesForExpressions(task.CronExpression, 1)
			if len(nextTimes) > 0 {
				item["next_run_at"] = nextTimes[0]
				// 顺手把同一份快照留给排序用，绝不在排序里第二次解析 cron。
				nextRunAt = &nextTimes[0]
			}
		}
		// 「定时规则」排序的键（issue #151）。刻意不看启用状态，理由见 preparedTaskListItem.firstRunSecond。
		var firstRunSecond *int
		if task.UsesCronSchedule() && task.CronExpression != "" {
			if second, ok := panelcron.FirstRunSecondOfDay(task.CronExpression, now); ok {
				firstRunSecond = &second
			}
		}

		// schedule_hint 回答的是「这条任务看着会自动跑、实际到底跑不跑」。
		// 上面那段 next_run_at 只认数据库的三个字段（状态 / 任务类型 / cron 表达式），
		// 三个字段都对就现算一个时间下发，从不回头问调度器这条任务有没有真正注册上 ——
		// 可任务能不能被触发的唯一判据，是它在 SchedulerV2 的 entryMap 里有没有 cron 条目。
		// 「编辑保存正好撞上排队中/运行中」「AddJob 解析 cron 报错」「面板启动时装载失败」
		// 这些路径都会让 entry 丢掉或压根没建起来，数据库那三个字段却纹丝不动，
		// 于是列表里它和正常任务长得一模一样：已启用、有下次运行时间、永远不触发、连一条日志都没有。
		// 所以这里必须再拿调度器的真实状态兜一层，把静默失联翻译成用户看得见的一句话。
		// 只是 entryMap 的一次内存查找（读锁），列表逐行调用也不会引入任何 DB 查询。
		//
		// 已禁用的任务整段跳过：它本来就不该在调度器里，状态列也已经写着「已禁用」，
		// 再挂一个警示图标只是噪音。
		if task.Status != model.TaskStatusDisabled {
			switch {
			case task.GetTaskType() == model.TaskTypeManual:
				// 手动 / 开机运行本来就不进 cron，AddJob 只给它们留一个空条目列表，
				// 数量恒为 0，不能和下面「注册丢了」共用一句文案。
				// 这两条纯看任务类型，不依赖调度器，所以调度器没起来时照样下发。
				item["schedule_hint"] = "该任务类型不会自动触发，只能手动运行"
			case task.GetTaskType() == model.TaskTypeStartup:
				// 开机运行**会**自动跑，只是按面板启动、不按时间，不能跟手动任务共用一句话。
				item["schedule_hint"] = "该任务只在面板启动时自动执行一次，不按定时规则触发"
			case service.HasPendingDisable(&task):
				// 「运行中被禁用、等这次跑完再生效」：条目确实已经被摘掉了，但那正是用户要的，
				// 这时候提示「未注册到调度器，重新保存一次即可恢复」是在教用户把自己刚关掉的任务打开。
			case service.GetSchedulerV2() != nil && service.GetSchedulerV2().ScheduledEntryCount(task.ID) <= 0:
				// 这一条必须有调度器才判得准。调度器为 nil（单元测试、只读演示站等根本没起
				// 调度器的场景）时一律不下发：那种情况下每一条任务都查不到条目，全量误报比不报更糟。
				item["schedule_hint"] = "未注册到调度器，重新保存一次任务即可恢复"
			}
		}

		prepared = append(prepared, preparedTaskListItem{
			task:               task,
			item:               item,
			displayLabels:      displayLabels,
			subscriptionLabels: subscriptionLabels,
			nextRunAt:          nextRunAt,
			firstRunSecond:     firstRunSecond,
			groupName:          taskGroupNameFromLabels(task.GetLabels()),
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
	// 运行中优先，口径与 applyDefaultTaskListOrdering 里那条 CASE WHEN status = 2 逐字对应
	// （issue #118）：同样排在状态分区之下、list_order 之上，同样只认 2（运行中）不认 0.5（排队中）。
	// 不并进 taskSortGroup 的理由见 applyDefaultTaskListOrdering 上的注释（拖拽桶会跟着分裂并漂移）。
	//
	// 这一层只作用于【默认序】。走内存路径时 defaultTaskListLess 是 sortPreparedTaskListItems
	// 的兜底比较器，只有用户给的 sort_rules 全部打平才轮得到它 ——
	// 用户显式点了「名称 A→Z」，运行中就不该越过他的规则插到前面。
	leftRunning := left.Status == model.TaskStatusRunning
	rightRunning := right.Status == model.TaskStatusRunning
	if leftRunning != rightRunning {
		return leftRunning
	}
	// 内存路径的比较口径必须和 applyDefaultTaskListOrdering 的 SQL 完全一致：
	// list_order 在前、sort_order 在后，两条路径漂了就会出现「带筛选和不带筛选顺序不一样」。
	if left.ListOrder != right.ListOrder {
		return left.ListOrder < right.ListOrder
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
	// 展示标签的去重按【类别】分开，绝不共用一个集合：
	// 自定义标签可能和订阅名重名（自建标签「娱乐」+ 名为「娱乐」的订阅），共用去重集合会把两条合并成一条，
	// 前端「自定义标签 / 订阅标签」两个开关就同时作用在同一条上 ——
	// 表现为「关掉自定义藏不掉它、关掉订阅反而把它藏了」（issue #109-3 的姊妹 bug）。
	// 分组名不参与这里任何一个集合（它在函数末尾单独 unshift），与订阅名重名时同样各留一条。
	seenCustom := make(map[string]struct{})
	seenSubscriptions := make(map[string]struct{})
	// 分组名的取法统一在 taskGroupNameFromLabels（视图筛选的 group 字段、GET /tasks/groups 都用它），
	// 这里只负责把它放到第 0 位；下面逐条遍历时跳过分组标签。
	groupName := taskGroupNameFromLabels(labels)

	addLabel := func(label string) {
		label = strings.TrimSpace(label)
		if label == "" {
			return
		}
		if _, exists := seenCustom[label]; exists {
			return
		}
		seenCustom[label] = struct{}{}
		displayLabels = append(displayLabels, label)
	}

	for _, label := range labels {
		if strings.HasPrefix(strings.TrimSpace(label), taskGroupLabelPrefix) {
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

		// 订阅源被删掉后 subscriptionNames 里查不到名字，退回字面量，用户仍能看出这是订阅任务
		subName := subscriptionNames[uint(subID)]
		if subName == "" {
			subName = "订阅任务"
		}
		// 两个数组用同一个订阅去重集合、同一次判定，保证 display_labels 里的订阅项
		// 与 subscription_labels 逐条对得上（前端按出现次数认领类别，多一条少一条都会错位）。
		if _, exists := seenSubscriptions[subName]; exists {
			continue
		}
		seenSubscriptions[subName] = struct{}{}
		displayLabels = append(displayLabels, subName)
		subscriptionLabels = append(subscriptionLabels, subName)
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
	case "group":
		// 只认分组名本身（#130，契约 C3），不看 display_labels：那里分组名和同名的自定义标签、订阅名混在一起。
		// 没有分组时返回 nil —— equals / contains 为假，not_equals / not_contains 为真，与其它字段的空值口径一致。
		// 🔴 老面板不认这个字段（落到 default 返回 nil），带 group 规则的视图还原到老面板上会筛成空列表。
		if item.groupName == "" {
			return nil
		}
		return []string{item.groupName}
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
	// 「首要排序字段就是 status」时豁免状态分区 —— 状态分区本身就是按 status 分的，
	// 再按 status 排一次是自相矛盾：分区把 {空闲,排队,运行} 归一组、{禁用} 归另一组，
	// 组内再按 status 升序，读出来是 0.5 → 1 → 2 → 0（禁用的 0 反而在最后），
	// 一个标着「升序」的排序数值上并不递增；翻成降序又是 2 → 1 → 0.5 → 0，
	// 禁用照样垫底 —— 对禁用任务而言 asc/desc 这个开关完全失效。
	// 用户选「按状态排序」，要的就是纯粹按状态值排，这时分区不该再插一脚。
	//
	// 判据只看**第一条**规则，不是「任意一条规则里有 status」：
	// status 当次级 tie-break（比如先按名称、名称相同再按状态）时，
	// 先分区、再在区内 tie-break 才是对的，那种情况不能豁免。
	//
	// 置顶分区**不**豁免：置顶是用户主动设置的展示优先级，与状态语义无关，
	// 按任何字段排序时它都留在最前（口径同 applyDefaultTaskListOrdering / defaultTaskListLess）。
	//
	// 提到比较器外面算一次：这个判定和被比较的两条任务无关，
	// 放进比较器里每次比较都要重算一遍，纯属白费。
	skipStatusGrouping := len(sortRules) > 0 && sortRules[0].Field == "status"

	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		// 置顶区与状态分区必须压在所有排序规则【之上】，而不是等规则全部打平之后才轮到。
		// 分区是列表的骨架，排序规则只决定「同一个区内部怎么排」：
		// 点一下「最后运行」倒序，用户要的是「最近跑过的启用任务排前面」，
		// 不是「刚被禁用、恰好留着最新 last_run_at 的任务冲到第一行」。
		// 口径与默认排序（applyDefaultTaskListOrdering 的 SQL 与 defaultTaskListLess）逐条一致：
		// 先置顶、再 taskSortGroup（空闲/排队/运行 → 禁用 → 其他），复用同一个 taskSortGroup，
		// 免得内存排序和 SQL 排序各写一份分组口径、日后漂掉。
		// 置顶之所以要排在状态分组前面：置顶是用户主动设置的展示优先级，
		// 置顶的禁用任务在默认排序里也留在置顶区，这里翻转的话会出现
		// 「默认排序在最上、点一下列排序掉到最下」的自相矛盾。
		// 唯一的例外是「首要排序字段就是 status」，见 skipStatusGrouping：
		// 那条规则的语义本来就是按状态值排，状态分区会把它压成非单调序列。
		if left.task.IsPinned != right.task.IsPinned {
			return left.task.IsPinned
		}
		// skipStatusGrouping 见函数开头：首要排序字段就是 status 时跳过这一层，
		// 否则「按状态升序」读出来的状态值序列并不递增。
		if !skipStatusGrouping {
			if leftGroup, rightGroup := taskSortGroup(left.task.Status), taskSortGroup(right.task.Status); leftGroup != rightGroup {
				return leftGroup < rightGroup
			}
		}

		for _, rule := range sortRules {
			// last_run_at / next_run_at 允许没有值：从未运行过、已禁用、非 cron 任务都算不出时间。
			// 这类任务一律沉到【本区】最后（分区在上面已经分完了），且【不跟随 asc/desc 翻转】——
			// 所以空值判定必须写在下面翻转 direction 之前。写在后面的话，
			// 用户点「按下次运行倒序」想看最晚要跑的，结果一整屏全是压根不会跑的禁用任务。
			if rule.Field == "last_run_at" || rule.Field == "next_run_at" {
				leftTime := preparedTaskRuleTime(left, rule.Field)
				rightTime := preparedTaskRuleTime(right, rule.Field)
				if (leftTime == nil) != (rightTime == nil) {
					return rightTime == nil
				}
			}
			// cron_expression（界面叫「定时规则」）按每天时刻排（issue #151），空值口径与上面一致：
			// 手动 / 开机 / 表达式为空 / 永不触发的任务没有时刻，一律沉到本区最后、不随 asc/desc 翻转，
			// 同样必须写在翻转 direction 之前。否则倒序时这些压根不按时间跑的任务会整片冲到最前面。
			if rule.Field == "cron_expression" && (left.firstRunSecond == nil) != (right.firstRunSecond == nil) {
				return right.firstRunSecond == nil
			}

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
		// 按「一天之内最早一次执行的时刻」比较（issue #151），不再比表达式字符串：
		// 字符串序会把 15 点排在 8 点前面（'1' < '8'），同是 09:00 的 5 段与 6 段写法也被拆到两头，
		// 用户想按定时规则看「哪个时间段有哪些脚本」时完全没法用。
		// 空值已在 sortPreparedTaskListItems 里判掉，走到这里两边要么都有值、要么都为空（按相等处理）。
		// 时刻相同返回 0，交给下一条规则；全部打平再回落默认序（置顶、list_order 那一套）。
		if left.firstRunSecond == nil || right.firstRunSecond == nil {
			return 0
		}
		return cmp.Compare(*left.firstRunSecond, *right.firstRunSecond)
	case "status":
		return compareFloat64(left.task.Status, right.task.Status)
	case "labels":
		return strings.Compare(strings.ToLower(strings.Join(left.displayLabels, ",")), strings.ToLower(strings.Join(right.displayLabels, ",")))
	case "subscription":
		return strings.Compare(strings.ToLower(strings.Join(left.subscriptionLabels, ",")), strings.ToLower(strings.Join(right.subscriptionLabels, ",")))
	case "group":
		// 与 labels / subscription 同一套比较口径：不区分大小写，没有分组的按空串参与比较（#130）。
		return strings.Compare(strings.ToLower(left.groupName), strings.ToLower(right.groupName))
	case "created_at":
		// 按创建时间比较：早于为 -1、晚于为 1，相等返回 0，direction 由上层 sortPreparedTaskListItems 翻转
		if left.task.CreatedAt.Before(right.task.CreatedAt) {
			return -1
		}
		if left.task.CreatedAt.After(right.task.CreatedAt) {
			return 1
		}
		return 0
	case "last_run_at", "next_run_at":
		// 走到这里时空值情况已经在 sortPreparedTaskListItems 里判掉了（恒排本区最后、不随方向翻转），
		// 剩下的要么两边都有值、要么两边都为空 —— 后者按相等处理，退回默认序。
		leftTime := preparedTaskRuleTime(left, rule.Field)
		rightTime := preparedTaskRuleTime(right, rule.Field)
		if leftTime == nil || rightTime == nil {
			return 0
		}
		if leftTime.Before(*rightTime) {
			return -1
		}
		if leftTime.After(*rightTime) {
			return 1
		}
		return 0
	default:
		// 未知字段静默回落默认序，不报 400：存量任务视图里可能存着面板已经不认的字段，
		// 报错会让那些视图直接打不开。
		return 0
	}
}

// preparedTaskRuleTime 取排序字段对应的时间值，nil 表示没有值。
// last_run_at 是数据库列，next_run_at 是响应期按 cron 现算的快照（prepareTaskListItems 里存的那份），
// 两个取值口径只写这一处，免得空值判定和大小比较各拿一套。
func preparedTaskRuleTime(item preparedTaskListItem, field string) *time.Time {
	if field == "next_run_at" {
		return item.nextRunAt
	}
	return item.task.LastRunAt
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
