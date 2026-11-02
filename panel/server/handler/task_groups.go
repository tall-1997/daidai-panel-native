package handler

import (
	"sort"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

// taskGroupNameFromLabels 取任务的分组名：trim 后第一个以 `分组:` 开头、且名字非空的标签（#130）。
//
// 列表展示的分组 chip（buildPreparedTaskLabels 放在 display_labels 第 0 位的那个）、
// 视图筛选 / 排序的 group 字段、GET /tasks/groups 都走这一个函数，三处口径不能各写一份：
// 漂了就会出现「顶栏标签写着 X，点进去筛出来的却不是 X 名下那批任务」。
// 名字为空的 `分组:` 跳过、接着认下一个；多个分组标签只认第一个。返回空串表示没有分组。
func taskGroupNameFromLabels(labels []string) string {
	for _, label := range labels {
		trimmed := strings.TrimSpace(label)
		if !strings.HasPrefix(trimmed, taskGroupLabelPrefix) {
			continue
		}
		if name := strings.TrimSpace(strings.TrimPrefix(trimmed, taskGroupLabelPrefix)); name != "" {
			return name
		}
	}
	return ""
}

// taskGroupCount 是 GET /tasks/groups 的一项（契约 C2）：分组名 + 挂着这个分组的任务数。
type taskGroupCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// ListGroups 列出全部任务分组，给网页顶栏的分组标签用（#130）。
//
// 「分组」就是任务 labels 里的 `分组:<名>` 标签，App 的分组管理写的就是它，两端本来是同一份数据，
// 只是网页顶栏从来不渲染。任务列表是分页拉的，前端拿不到完整清单，所以单开这个接口。
//
// 口径与列表逐字一致：一条任务只认第一个非空的 `分组:`（taskGroupNameFromLabels），
// 视图筛选里 group 等于 X 筛出来的就是这里 X 名下的任务。
// 例外只有大小写：这里去重区分大小写（Prod 与 prod 各算一个），筛选的 equals 不区分，
// 所以大小写不同的两个分组会各占一个标签、却筛出同一批任务 —— 两边都沿用既有口径，不在这里统一。
//
// 返回裸数组 [{name, count}]，按 name 升序（字节序，与 GET /envs/groups 一致）；一个分组都没有时是 []，不是 null。
func (h *TaskHandler) ListGroups(c *gin.Context) {
	var rawLabels []string
	// SQL 只做粗筛（子串包含，比 Go 侧「trim 后前缀匹配」更宽），真正的判定在下面逐条做。
	if err := database.DB.Model(&model.Task{}).
		Where("labels LIKE ?", "%"+taskGroupLabelPrefix+"%").
		Pluck("labels", &rawLabels).Error; err != nil {
		response.InternalError(c, "加载任务分组失败")
		return
	}

	counts := make(map[string]int)
	for _, raw := range rawLabels {
		task := model.Task{Labels: raw}
		name := taskGroupNameFromLabels(task.GetLabels())
		if name == "" {
			continue
		}
		counts[name]++
	}

	groups := make([]taskGroupCount, 0, len(counts))
	for name, count := range counts {
		groups = append(groups, taskGroupCount{Name: name, Count: count})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Name < groups[j].Name
	})

	response.Success(c, groups)
}
