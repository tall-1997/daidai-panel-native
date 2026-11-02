package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"

	"gorm.io/gorm"
)

// createLabelCountTask 建一条带标签的任务；labels 按存储口径用英文逗号拼进同一列。
func createLabelCountTask(t *testing.T, name string, labels ...string) *model.Task {
	t.Helper()

	task := &model.Task{Name: name, Command: "echo " + name, CronExpression: "0 0 * * *", Status: model.TaskStatusEnabled}
	task.SetLabelsFromSlice(labels)
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task %q: %v", name, err)
	}
	return task
}

// #157 契约 L1 的计算口径：trim、跳过空串与内部前缀（trim 之后判断）、同一任务内去重、区分大小写、字节序升序；
// 一个都没有时是非 nil 的空切片（序列化成 []，不是 null）。
func TestListTaskLabelCountsAppliesContractL1(t *testing.T) {
	testutil.SetupTestEnv(t)

	empty, err := ListTaskLabelCounts()
	if err != nil {
		t.Fatalf("list labels on empty db: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected a non-nil empty slice on empty db, got %#v", empty)
	}
	if encoded, _ := json.Marshal(empty); string(encoded) != "[]" {
		t.Fatalf("expected empty result to encode as [], got %s", encoded)
	}

	createLabelCountTask(t, "普通加分组", "京东", "分组:日常")
	// 全角空格（U+3000）也算空白，trim 后与「京东」合并；带前导空格的订阅标签 trim 后仍是内部标签。
	createLabelCountTask(t, "全角空格", string(rune(0x3000))+"京东", " subscription:1")
	// 只有空白的标签跳过。
	createLabelCountTask(t, "只有空白", "   ", "subscription:2")
	// 一个「a,b」存进去按英文逗号拆，读回来就是两个标签。
	createLabelCountTask(t, "逗号拆开", "a,b")
	// 同一任务里大小写不同的两个标签各算一个。
	createLabelCountTask(t, "大小写", "Prod", "prod")
	// 前缀不在开头的不是内部标签；「数据」是「数据库」的前缀，字节序里排在前面。
	createLabelCountTask(t, "前缀不在开头", "my分组:beta", "数据库", "数据")
	// 同一任务里重复的只算一次。
	createLabelCountTask(t, "重复", "京东", "京东")
	// labels 为 NULL 的行被 labels <> '' 挡掉，Pluck 不会因为它报错。
	nullTask := createLabelCountTask(t, "labels 为 NULL", "临时")
	if err := database.DB.Model(&model.Task{}).Where("id = ?", nullTask.ID).Update("labels", gorm.Expr("NULL")).Error; err != nil {
		t.Fatalf("set labels to NULL: %v", err)
	}
	createLabelCountTask(t, "没有标签")

	got, err := ListTaskLabelCounts()
	if err != nil {
		t.Fatalf("list labels: %v", err)
	}
	want := []TaskLabelCount{
		{Name: "Prod", Count: 1},
		{Name: "a", Count: 1},
		{Name: "b", Count: 1},
		{Name: "my分组:beta", Count: 1},
		{Name: "prod", Count: 1},
		{Name: "京东", Count: 3},
		{Name: "数据", Count: 1},
		{Name: "数据库", Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// 内部前缀只认开头、区分大小写；调用方负责先 trim。
func TestIsInternalTaskLabelMatchesOnlyReservedPrefixes(t *testing.T) {
	cases := map[string]bool{
		"分组:日常":          true,
		"分组:":            true,
		"subscription:3": true,
		"my分组:beta":      false,
		"分组":             false,
		"Subscription:3": false,
		"京东":             false,
	}
	for label, want := range cases {
		if got := IsInternalTaskLabel(label); got != want {
			t.Errorf("IsInternalTaskLabel(%q) = %v, want %v", label, got, want)
		}
	}
}
