package service

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

// #159 修复 D：runTask 原来把整份输出累积在 outputCollector 里（上限约 97.7MiB），常驻大输出的任务会在堆里多存一份。
// 现在只留最后 1MiB（appendOutputTail），失败 / 成功摘要、依赖自动识别、运行时失败提示都只看这段尾部。
// 这组用例守住尾部窗口的形状：不超过上限、一定是完整输出的后缀、从行首开始、不切成空串。
func TestAppendOutputTailKeepsLastMiB(t *testing.T) {
	t.Run("小输入原样保留", func(t *testing.T) {
		buf := appendOutputTail(nil, "第一行\n")
		buf = appendOutputTail(buf, "second")
		if got := string(buf); got != "第一行\nsecond" {
			t.Fatalf("不超过上限时应原样拼接，got %q", got)
		}
	})

	t.Run("3MiB 长短不一的多字节行只留尾部且从行首开始", func(t *testing.T) {
		var full strings.Builder
		var buf []byte
		lastLine := ""
		for i := 0; full.Len() < 3<<20; i++ {
			// 行长在几十字节到约 600 字节之间变化，夹带中文（3 字节）与 emoji（4 字节），
			// 窗口的起点大概率先落在某个多字节字符的中间，必须挪到下一行行首
			line := fmt.Sprintf("第%d行 %s 🚀\n", i, strings.Repeat("内容x", i%97))
			full.WriteString(line)
			buf = appendOutputTail(buf, line)
			lastLine = line
		}
		whole := full.String()
		got := string(buf)
		if len(got) == 0 || len(got) > outputTailLimit {
			t.Fatalf("尾部长度应在 (0, %d] 之间，got %d", outputTailLimit, len(got))
		}
		if !strings.HasSuffix(whole, got) {
			t.Fatal("尾部必须是完整输出的后缀")
		}
		if start := len(whole) - len(got); start == 0 || whole[start-1] != '\n' {
			t.Fatalf("尾部必须从某一行的行首开始（前一个字节应是换行），start=%d", start)
		}
		if !strings.HasSuffix(got, lastLine) {
			t.Fatalf("最后一行必须完整保留，期望以 %q 结尾", lastLine)
		}
		if !utf8.ValidString(got) {
			t.Fatal("尾部不应以被切开的多字节字符开头")
		}
	})

	t.Run("单个超过 1MiB 的块也被压到上限以内", func(t *testing.T) {
		// 一次 onOutput 就灌进来约 1.9MiB（20 字节一行 × 10 万行）
		chunk := strings.Repeat("块里的一行 abc\n", 100000)
		got := string(appendOutputTail(nil, chunk))
		if len(got) == 0 || len(got) > outputTailLimit {
			t.Fatalf("尾部长度应在 (0, %d] 之间，got %d", outputTailLimit, len(got))
		}
		if !strings.HasSuffix(chunk, got) {
			t.Fatal("尾部必须是这个块的后缀")
		}
		if start := len(chunk) - len(got); chunk[start-1] != '\n' {
			t.Fatal("尾部必须从某一行的行首开始")
		}
	})

	t.Run("一整行就超过 1MiB 时保留这一行的后 1MiB，不切成空串", func(t *testing.T) {
		// 压成一行的报错、jq -c 的输出：窗口里唯一的换行就是最后一个字节，
		// 按「从下一行行首算起」硬切会切成空串，失败摘要和依赖识别就什么都拿不到了
		line := strings.Repeat("x", 2<<20) + "\n"
		got := string(appendOutputTail(nil, line))
		if got == "" {
			t.Fatal("一整行超过 1MiB 时不能被切成空串")
		}
		if len(got) > outputTailLimit {
			t.Fatalf("尾部长度不应超过 %d，got %d", outputTailLimit, len(got))
		}
		if !strings.HasSuffix(line, got) || !strings.HasSuffix(got, "x\n") {
			t.Fatal("应保留这一行的后缀，并以这一行的结尾收尾")
		}
	})
}

// 报错之前已经输出了超过 1MiB：尾部窗口装得下报错本身，运行时失败提示（这里是 ESM 兼容提示）照样能给出。
// 守的是「尾部」而不是「头部」——实现成只留前 1MiB 的话，报错落在窗口外，这一条就红。
func TestRunTaskFailureHintSurvivesOutputBeyondTail(t *testing.T) {
	testutil.SetupTestEnv(t)
	// 关掉依赖自动安装：这条只验失败提示看的是尾部，不让执行器真去装 uuid
	if err := model.SetConfig("auto_install_deps", "false"); err != nil {
		t.Fatalf("disable auto_install_deps: %v", err)
	}

	executor := NewTaskExecutor()
	task, req, taskLog := newRunningTaskReq(t, "大输出之后报 ESM 错误", nil)

	// 报错样例取自 task_notification_test.go 的 TestBuildModuleCompatibilityHintRecognizesRequireEsmFailure
	esmError := strings.Join([]string{
		`const { v4: uuidv4 } = require('uuid');`,
		"Error [ERR_REQUIRE_ESM]: require() of ES Module /app/Dumb-Panel/deps/nodejs/node_modules/uuid/dist-node/index.js from /app/Dumb-Panel/scripts/wc.js not supported.",
		"Instead change the require of index.js in /app/Dumb-Panel/scripts/wc.js to a dynamic import() which is available in all CommonJS modules.",
	}, "\n") + "\n"

	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		// 先吐约 1.5MiB 的填充行，把报错之前的内容顶出 1MiB 的尾部窗口
		const filler = "filler line before the real error\n"
		for written := 0; written < 3<<19; written += len(filler) {
			onOutput(filler)
		}
		onOutput(esmError)
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	tinyLog, err := NewTinyLog("output-tail-esm-hint")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	executor.runTask(req, taskLog, tinyLog)

	content := readSettledLogContent(t, taskLog.ID)
	if !strings.Contains(content, "ESM 模块") {
		t.Fatalf("报错之前输出超过 1MiB 时，运行时失败提示仍应出现在日志里；日志末尾=%q", content[max(0, len(content)-800):])
	}
	if stored := reloadServiceTask(t, task.ID); stored.LastRunStatus == nil || *stored.LastRunStatus != model.RunFailed {
		t.Fatalf("这次执行应结算为失败(%d)，got %v", model.RunFailed, stored.LastRunStatus)
	}
}
