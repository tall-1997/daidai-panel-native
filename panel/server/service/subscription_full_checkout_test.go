package service

import (
	"reflect"
	"strings"
	"testing"

	"daidai-panel/model"
)

// 完整检出开关的对照用例：同一条订阅、只翻 FullCheckout 这一个 bool。
//
// 关着时白名单/黑名单照常收窄 sparse 规则；打开后规则必须整个消失 ——
// applySparseCheckout 见到空规则会把已有的 sparse-checkout 关掉，
// pullGitRepoWithCallback 首次 clone 那条 `--filter=blob:none --no-checkout`
// 分支的条件也是 len(patterns) > 0，两处都靠「空规则」这一个信号联动。
//
// 同时锁住那条提示：用户同时填了白名单又打开完整检出时，看到的现象是整仓文件全落盘、
// 白名单像是失效了，一声不吭的话没人能把这个现象和这个开关对上。
func TestBuildSparseCheckoutPatternsFullCheckoutDropsAllPatterns(t *testing.T) {
	// #110 里那条真实订阅：BiliBiliToolPro 的脚本要读仓库 src/ 自行编译，
	// 所以用户既填了白名单（只想给 bili_task_ 建任务），又需要整仓落盘。
	newSub := func(fullCheckout bool) *model.Subscription {
		return &model.Subscription{
			Name:         "BiliBiliToolPro",
			Type:         model.SubTypeGitRepo,
			URL:          "https://github.com/RayWangQvQ/BiliBiliToolPro.git",
			SaveDir:      "RayWangQvQ_BiliBiliToolPro",
			Whitelist:    "bili_task_",
			Blacklist:    "backUp",
			FullCheckout: fullCheckout,
		}
	}

	// 基线：开关关着时，行为与加这个开关之前完全一致（存量订阅升级后就落在这一档）。
	patterns, warnings := buildSubscriptionSparseCheckoutPatterns(newSub(false))
	want := []string{"**/*bili_task_*", "**/*bili_task_*/**", "!**/*backUp*", "!**/*backUp*/**"}
	if !reflect.DeepEqual(patterns, want) {
		t.Fatalf("关闭完整检出时 sparse patterns = %#v, want %#v", patterns, want)
	}
	if len(warnings) != 0 {
		t.Fatalf("关闭完整检出时不该有任何告警, got %#v", warnings)
	}

	// 打开后：一条 sparse 规则都不能剩。
	// 断言 len 而不是 == nil，是因为下游只看 len（空切片与 nil 等价），
	// 不该为一个语义上没差别的写法变化把用例判红。
	patterns, warnings = buildSubscriptionSparseCheckoutPatterns(newSub(true))
	if len(patterns) != 0 {
		t.Fatalf("打开完整检出后不应下发任何 sparse 规则, got %#v", patterns)
	}
	if len(warnings) != 1 {
		t.Fatalf("打开完整检出应给出且只给出一条提示, got %#v", warnings)
	}
	notice := warnings[0]
	// 三个要素缺一不可：整仓落盘这个事实、白/黑名单不再限制落盘范围、以及它们仍然管着建不建任务。
	// 少了最后一句，用户会以为打开开关等于把过滤器整个关掉，转头去删白名单。
	for _, keyword := range []string{"整个仓库", "不限制落盘范围", "定时任务"} {
		if !strings.Contains(notice, keyword) {
			t.Errorf("完整检出提示应包含 %q, got %q", keyword, notice)
		}
	}

	// 三项过滤都留空时不该有话说：那种订阅本来就是整仓检出，开关开不开结果一样。
	// 无条件打提示会让用户去设置里找一个自己从来没配过的白名单。
	bare := newSub(true)
	bare.Whitelist = ""
	bare.Blacklist = ""
	if _, bareWarnings := buildSubscriptionSparseCheckoutPatterns(bare); len(bareWarnings) != 0 {
		t.Fatalf("过滤项全空时不该打完整检出提示, got %#v", bareWarnings)
	}
}

// 完整检出打开后，「指定子目录」必须继续限制**建任务的范围**。
//
// sub_path 在 service 里只有 sparse-checkout 一处引用，它对建任务范围的约束一直是靠
// 「不在子目录里的文件根本不落盘」间接实现的。开了完整检出整仓都落盘，这条间接约束就没了 ——
// 不补回来的话，一个 sub_path=qinglong/DefaultTasks + 自动建任务的订阅，
// 会把仓库里每个 .sh/.js/.py 都建成定时任务并真的跑起来。
func TestFullCheckoutKeepsSubPathTaskScope(t *testing.T) {
	exts := map[string]bool{".sh": true}
	sub := &model.Subscription{
		Type:         model.SubTypeGitRepo,
		SubPath:      "qinglong/DefaultTasks",
		FullCheckout: true,
	}

	if !shouldManageSubscriptionFile(sub, "qinglong/DefaultTasks/bili_task_manga.sh", exts) {
		t.Fatal("子目录内的脚本应该照常建任务")
	}
	for _, outside := range []string{"tools/build.sh", "ci/release.sh", "setup.sh"} {
		if shouldManageSubscriptionFile(sub, outside, exts) {
			t.Errorf("子目录外的 %q 不该被建成定时任务", outside)
		}
	}

	// 前缀相同但不是同一个目录，不能误命中（DefaultTasksExtra ≠ DefaultTasks）
	if shouldManageSubscriptionFile(sub, "qinglong/DefaultTasksExtra/x.sh", exts) {
		t.Error("同前缀的兄弟目录不该命中")
	}

	// 关掉开关时这条护栏不生效：行为必须与加开关之前逐字一致（落盘范围本来就由 sparse 保证）。
	off := *sub
	off.FullCheckout = false
	if !shouldManageSubscriptionFile(&off, "tools/build.sh", exts) {
		t.Error("关闭完整检出时不该由这条护栏改变既有行为")
	}

	// 子目录留空 = 用户没有限制过范围，护栏不该凭空生出限制。
	noScope := *sub
	noScope.SubPath = ""
	if !shouldManageSubscriptionFile(&noScope, "tools/build.sh", exts) {
		t.Error("没填指定子目录时不该限制建任务范围")
	}
}
