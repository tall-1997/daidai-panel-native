package service

import (
	"sort"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

// 任务 labels 里两类内部标签的前缀，和用户自己加的普通标签存在同一列：
//   - `分组:<名>` 是任务分组，App 的分组管理、网页的「任务分组」框写的都是它；
//   - `subscription:<ID>` 是订阅任务的归属标签，由 subscriptionTaskLabel 在订阅同步时写入。
const (
	taskGroupLabelPrefix        = "分组:"
	taskSubscriptionLabelPrefix = "subscription:"
)

// IsInternalTaskLabel 判断一个标签是不是带保留前缀的内部标签（分组: / subscription:）。
//
// 调用方负责先 TrimSpace 再传进来（下面两个调用方都是这么做的），这里只比前缀，
// 所以 `my分组:beta` 这种前缀不在开头的仍是普通标签。
// 「批量添加标签」清洗用户输入（handler.sanitizeIncomingLabels）与 GET /tasks/labels 的候选共用这一个函数：
// 两边口径一旦漂开，就会出现「候选里列着、点了加上去却被当成内部标签吞掉」或者反过来。
func IsInternalTaskLabel(label string) bool {
	return strings.HasPrefix(label, taskGroupLabelPrefix) || strings.HasPrefix(label, taskSubscriptionLabelPrefix)
}

// TaskLabelCount 是 GET /tasks/labels 的一项（#157 契约 L1）：自定义标签名 + 带这个标签的任务数。
// 形状与 GET /tasks/groups 逐字对齐，每项恰好 name、count 两个键。
type TaskLabelCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListTaskLabelCounts 列出全部任务用过的自定义标签及各自的任务数（#157），
// 给网页任务表单与「批量添加标签」弹窗的「已有标签」候选用。
//
// 任务列表是服务端分页的，前端只拿当前页的标签必然不全；拉全量（all=1）又重又会在 5000 条处截断，所以单开这个查询。
// 口径（契约 L1）：
//   - 逐条 TrimSpace，跳过空串；trim 之后以 `分组:` / `subscription:` 开头的是内部标签，跳过
//     （带前导空格的脏数据也排除；前缀不在开头的照常保留，例如 `my分组:beta`）；
//   - count 是带这个标签的任务数，同一任务里重复出现只算一次；
//   - 去重区分大小写（Prod 与 prod 各算一个），与 /tasks/groups 同口径；
//   - 按 name 字节序升序，与 /tasks/groups、/envs/groups 一致，「按常用排」由前端自己做；
//   - 一个都没有时返回空切片（序列化成 []，不是 null）。
func ListTaskLabelCounts() ([]TaskLabelCount, error) {
	var rawLabels []string
	// 只取 labels 一列；NULL 与空串都被这个条件挡掉（NULL <> '' 的结果是 NULL，不算命中）。
	if err := database.DB.Model(&model.Task{}).
		Where("labels <> ''").
		Pluck("labels", &rawLabels).Error; err != nil {
		return nil, err
	}

	counts := make(map[string]int)
	for _, raw := range rawLabels {
		// 拆分沿用模型自己的 GetLabels（按英文逗号），与存储的拼接方式保持同一口径。
		task := model.Task{Labels: raw}
		seen := make(map[string]struct{})
		for _, label := range task.GetLabels() {
			name := strings.TrimSpace(label)
			if name == "" || IsInternalTaskLabel(name) {
				continue
			}
			// 同一任务里重复的标签只算一次。
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			counts[name]++
		}
	}

	labels := make([]TaskLabelCount, 0, len(counts))
	for name, count := range counts {
		labels = append(labels, TaskLabelCount{Name: name, Count: count})
	}
	sort.Slice(labels, func(i, j int) bool {
		return labels[i].Name < labels[j].Name
	})
	return labels, nil
}
