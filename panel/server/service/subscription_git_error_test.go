package service

import (
	"errors"
	"strings"
	"testing"
)

// 用户现场日志原文（拉 jdpro 库时镜像限流 + partial clone 补 blob 失败）：
//
//	[sparse-checkout] 设置订阅路径过滤（共 4 条）: **/*jd_*, **/*jx_*, **/*jddj_*, !**/*backUp*
//	fatal: unable to access 'https://js.googo.win/https://github.com/6dylan6/jdpro.git/': The requested URL returned error: 429
//	fatal: could not fetch 71c588b04a40d5845c8a3c8cf399e874044fbdb0 from promisor remote
//	[错误] sparse-checkout set 失败: exit status 128
//
// 用户能看到的全部信息只有最后那句 `exit status 128`，完全不可操作。
const (
	gitOutputRateLimited = `fatal: unable to access 'https://js.googo.win/https://github.com/6dylan6/jdpro.git/': The requested URL returned error: 429`
	gitOutputPromisor    = `fatal: could not fetch 71c588b04a40d5845c8a3c8cf399e874044fbdb0 from promisor remote`
	gitOutputUserReport  = gitOutputRateLimited + "\n" + gitOutputPromisor
)

// errExitStatus128 模拟 exec 返回给调用方的那句毫无信息量的错误。
var errExitStatus128 = errors.New("exit status 128")

func TestClassifyGitFailureTranslatesRealGitOutput(t *testing.T) {
	cases := []struct {
		name string
		// output 一律使用真实 git 输出片段，不要用编出来的措辞。
		output string
		err    error
		// wantKeywords 是提示里必须出现的可操作信息。
		wantKeywords []string
		// notWantKeywords 用来挡住「识别串味」——错误分类比不分类更害人。
		notWantKeywords []string
		wantEmpty       bool
	}{
		{
			name:   "单独 429：镜像限流",
			output: gitOutputRateLimited,
			err:    errExitStatus128,
			wantKeywords: []string{
				"429",
				"限流",
				"重试",
				"更换其他镜像地址",
			},
			notWantKeywords: []string{"partial clone"},
		},
		{
			name:   "单独 promisor：partial clone 补 blob 失败",
			output: gitOutputPromisor,
			err:    errExitStatus128,
			wantKeywords: []string{
				"partial clone",
				"--filter=blob:none",
				"删除该订阅的本地目录后重新拉取",
				"请求数远少于逐个补 blob",
			},
			notWantKeywords: []string{"429"},
		},
		{
			// 用户实际遇到的组合：两条特征必须合并成一条完整提示，
			// 既说明是限流，也说明为什么切换规则会触发大量请求、以及出路。
			name:   "429 + promisor 同时出现：合并成一条提示",
			output: gitOutputUserReport,
			err:    errExitStatus128,
			wantKeywords: []string{
				"429",
				"限流",
				"partial clone",
				"--filter=blob:none",
				"请求量被成倍放大",
				"删除该订阅的本地目录后重新拉取",
				"请求数远少于逐个补 blob",
			},
		},
		{
			name:   "鉴权失败：Authentication failed",
			output: `fatal: Authentication failed for 'https://github.com/someone/private-repo.git/'`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"身份验证",
				"SSH 密钥",
				"Access Token",
			},
		},
		{
			name:   "鉴权失败：拿不到用户名（终端提示被禁用）",
			output: `fatal: could not read Username for 'https://github.com': terminal prompts disabled`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"身份验证",
				"SSH 密钥",
			},
		},
		{
			name:   "鉴权失败：HTTP 403",
			output: `fatal: unable to access 'https://github.com/someone/private-repo.git/': The requested URL returned error: 403`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"身份验证",
				"Access Token",
			},
		},
		{
			name:   "鉴权失败：SSH publickey 被拒",
			output: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.",
			err:    errExitStatus128,
			wantKeywords: []string{
				"身份验证",
				"SSH 密钥",
			},
		},
		{
			name:   "仓库不存在：Repository not found",
			output: "remote: Repository not found.\nfatal: repository 'https://github.com/someone/ghost.git/' not found",
			err:    errExitStatus128,
			wantKeywords: []string{
				"404",
				"核对订阅里的仓库地址",
			},
		},
		{
			name:   "仓库不存在：HTTP 404",
			output: `fatal: unable to access 'https://mirror.example.com/https://github.com/someone/ghost.git/': The requested URL returned error: 404`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"404",
				"核对订阅里的仓库地址",
			},
		},
		{
			name:   "网络不可达：DNS 解析失败",
			output: `fatal: unable to access 'https://github.com/someone/repo.git/': Could not resolve host: github.com`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"无法连接到远端",
				"DNS",
				"检查容器的网络",
			},
		},
		{
			name:   "网络不可达：连接超时",
			output: `fatal: unable to access 'https://github.com/someone/repo.git/': Failed to connect to github.com port 443 after 21053 ms: Connection timed out`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"无法连接到远端",
				"检查容器的网络",
			},
		},
		{
			name:   "分支不存在：couldn't find remote ref",
			output: `fatal: couldn't find remote ref refs/heads/master`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"分支",
				"订阅设置里修正分支名",
			},
		},
		{
			name:   "分支不存在：Remote branch not found in upstream",
			output: `fatal: Remote branch develop not found in upstream origin`,
			err:    errExitStatus128,
			wantKeywords: []string{
				"分支",
				"订阅设置里修正分支名",
			},
		},
		{
			// 未识别的错误必须返回空串，让原有的通用错误照旧输出，
			// 绝不能硬套一条不相干的提示把用户带偏。
			name:      "未识别：本地改动会被覆盖",
			output:    "error: Your local changes to the following files would be overwritten by merge:\n\tscripts/jd_bean.js\nAborting",
			err:       errExitStatus128,
			wantEmpty: true,
		},
		{
			name:      "未识别：合并冲突",
			output:    "CONFLICT (content): Merge conflict in scripts/jd_bean.js\nAutomatic merge failed; fix conflicts and then commit the result.",
			err:       errExitStatus128,
			wantEmpty: true,
		},
		{
			// 本地目录不是仓库，和「远端仓库不存在」是两回事，不能误判成 404。
			name:      "未识别：本地目录不是 git 仓库",
			output:    "fatal: not a git repository (or any of the parent directories): .git",
			err:       errExitStatus128,
			wantEmpty: true,
		},
		{
			// 本地文件权限问题不是鉴权失败，处置方式完全不同。
			name:      "未识别：本地文件权限不足",
			output:    "error: unable to create file scripts/jd_bean.js: Permission denied",
			err:       errExitStatus128,
			wantEmpty: true,
		},
		{
			name:      "未识别：空输出",
			output:    "",
			err:       errExitStatus128,
			wantEmpty: true,
		},
		{
			// err 为 nil 表示命令成功，输出里出现什么字样都不该报错。
			name:      "命令成功时不产出提示",
			output:    gitOutputUserReport,
			err:       nil,
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyGitFailure(tc.output, tc.err)

			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("未识别的错误应返回空串，got %q", got)
				}
				return
			}

			if got == "" {
				t.Fatalf("应识别出错误原因，但返回空串\n原始输出:\n%s", tc.output)
			}
			if !strings.HasPrefix(got, "[错误] ") {
				t.Fatalf("提示应沿用 [错误] 前缀风格, got %q", got)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("提示必须是单行（emit 按行输出）, got %q", got)
			}
			for _, keyword := range tc.wantKeywords {
				if !strings.Contains(got, keyword) {
					t.Fatalf("提示缺少关键信息 %q\ngot: %s", keyword, got)
				}
			}
			for _, keyword := range tc.notWantKeywords {
				if strings.Contains(got, keyword) {
					t.Fatalf("提示不该出现 %q（分类串味）\ngot: %s", keyword, got)
				}
			}
		})
	}
}

// 提示是「追加」而不是「替换」：原始 fatal 行仍然由 runCmdWithCallback 逐行 emit，
// 分类器只负责多给一条中文说明，不得吞掉或改写任何原始输出。
func TestClassifyGitFailureDoesNotSwallowRawGitOutput(t *testing.T) {
	hint := classifyGitFailure(gitOutputUserReport, errExitStatus128)
	if hint == "" {
		t.Fatal("用户现场日志应能被识别")
	}
	for _, raw := range []string{"fatal: unable to access", "promisor remote", "71c588b0"} {
		if strings.Contains(hint, raw) {
			t.Fatalf("提示不应把原始输出复述一遍（原文由 runCmdWithCallback 逐行 emit）: %q", raw)
		}
	}
}

// 分类只在真正失败时才做；GitHub 的二级限流以 403 返回，此时限流才是根因，
// 不能按鉴权失败提示用户去配 Token。
func TestClassifyGitFailurePrefersRateLimitOverAuthForSecondaryRateLimit(t *testing.T) {
	output := "remote: You have exceeded a secondary rate limit.\n" +
		`fatal: unable to access 'https://github.com/someone/repo.git/': The requested URL returned error: 403`

	got := classifyGitFailure(output, errExitStatus128)
	if !strings.Contains(got, "限流") {
		t.Fatalf("二级限流应按限流提示, got %q", got)
	}
	if strings.Contains(got, "Access Token") {
		t.Fatalf("二级限流不应提示配置 Access Token, got %q", got)
	}
}

func TestDetectGitFailureSignalsMatchesCaseInsensitively(t *testing.T) {
	// git 自身大小写不统一：ssh 侧的仓库缺失是 `ERROR: Repository not found.`
	signals := detectGitFailureSignals("ERROR: Repository not found.")
	if !signals.repoNotFound {
		t.Fatal("大写的 Repository not found 也应被识别")
	}

	signals = detectGitFailureSignals("error: RPC failed; HTTP 429 curl 22 The requested URL returned error: 429")
	if !signals.rateLimited {
		t.Fatal("RPC failed 形态的 429 也应被识别")
	}
	if signals.promisor {
		t.Fatal("单独的 429 不应命中 promisor")
	}
}

func TestWrapGitCommandErrorKeepsStderrAndAppendsHint(t *testing.T) {
	err := wrapGitCommandError("读取远端配置", gitOutputUserReport, errExitStatus128)
	if err == nil {
		t.Fatal("应返回错误")
	}

	message := err.Error()
	if !strings.Contains(message, "读取远端配置失败") {
		t.Fatalf("应保留动作说明, got %q", message)
	}
	if !strings.Contains(message, "exit status 128") {
		t.Fatalf("应保留原始 error, got %q", message)
	}
	// stderr 原文对排查有价值，必须原样带上。
	if !strings.Contains(message, "The requested URL returned error: 429") {
		t.Fatalf("应保留 stderr 原文, got %q", message)
	}
	if !strings.Contains(message, "partial clone") {
		t.Fatalf("应追加可操作提示, got %q", message)
	}

	// 识别不出来时只带 stderr，不硬编提示。
	err = wrapGitCommandError("读取远端配置", "fatal: not a git repository", errExitStatus128)
	if strings.Contains(err.Error(), "partial clone") {
		t.Fatalf("未识别的错误不应追加提示, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "fatal: not a git repository") {
		t.Fatalf("未识别时也要保留 stderr 原文, got %q", err.Error())
	}

	if wrapGitCommandError("读取远端配置", "", nil) != nil {
		t.Fatal("err 为 nil 时不应造出错误")
	}
}
