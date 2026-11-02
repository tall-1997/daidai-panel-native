package handler

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

// ---------- runCmdWithSSEThen 的输出管道：不丢尾巴、不被后台进程拖住、不被超长单行卡死 ----------

// TestDependencyOutputHelperProcess 不是真正的用例：下面的用例把测试二进制自己当成「依赖命令」起子进程，
// 按环境变量打印一大段输出后立刻退出。不依赖 sh / cmd 的行为，Windows 与 Linux 上一致。
func TestDependencyOutputHelperProcess(t *testing.T) {
	mode := os.Getenv("DAIDAI_TEST_DEP_OUTPUT")
	if mode == "" {
		return
	}
	var b strings.Builder
	if mode == "longline" {
		// 先来一行 300KB，超过读协程 256KB 的单行上限。
		b.WriteString(strings.Repeat("a", 300*1024))
		b.WriteString("\n")
	}
	// 30000 行约 470KB，远大于 Linux 管道的 64KB 缓冲：子进程最后一截输出写进管道后立刻退出。
	for i := 0; i < 30000; i++ {
		fmt.Fprintf(&b, "dep-line-%06d\n", i)
	}
	os.Stdout.WriteString(b.String())
	// 必须 os.Exit：return 的话 testing 框架会往 stdout 补 PASS 之类的收尾文本。
	// 「写完立刻退出」也正是要复现的场景：进程一退，旧实现的 Wait 就会抢在读完之前关掉管道。
	os.Exit(0)
}

func dependencyOutputHelperCommand(mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDependencyOutputHelperProcess$")
	cmd.Env = append(os.Environ(), "DAIDAI_TEST_DEP_OUTPUT="+mode)
	return cmd
}

// 写完一大段输出立刻退出的命令，依赖日志必须一行不少，也不能冒出「读取安装输出失败」。
// 旧实现（StdoutPipe + 与读协程并发的 Wait）在 Linux 上这种场景约九成会丢掉末尾一截，连跑 5 轮几乎必红。
func TestRunCmdWithSSEKeepsTailOfFastExitingCommand(t *testing.T) {
	dep := setupRunCmdTest(t)
	for round := 1; round <= 5; round++ {
		database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).
			Updates(map[string]interface{}{"log": "", "status": model.DepStatusInstalling})

		runCmdWithSSE(dependencyOutputHelperCommand("lines"), dep.ID, model.DepStatusInstalled, false)

		got := reloadDependency(t, dep.ID)
		if got.Status != model.DepStatusInstalled {
			t.Fatalf("第 %d 轮：命令以 0 退出应记为 installed，实际 %s", round, got.Status)
		}
		if strings.Contains(got.Log, "[读取安装输出失败]") {
			t.Fatalf("第 %d 轮：不应出现读取失败行（输出管道被提前关掉的症状）", round)
		}
		// 只数总行数不够，最后一行必须在：丢的恰恰是末尾那一截。
		if count := strings.Count(got.Log, "dep-line-"); count != 30000 || !strings.Contains(got.Log, "dep-line-029999\n") {
			t.Fatalf("第 %d 轮：日志应有完整的 30000 行输出，实际 %d 行", round, count)
		}
	}
}

// 命令本体已经 0 退出、但它拉起的后台进程还攥着输出管道：不能一直等到后台进程结束
// （依赖会一直停在「安装中」、还攥着包操作锁），WaitDelay 到点后按成功收尾并写一行说明。
func TestRunCmdWithSSEDoesNotWaitForBackgroundProcessHoldingOutput(t *testing.T) {
	dep := setupRunCmdTest(t)
	old := dependencyOutputWaitDelay
	t.Cleanup(func() { dependencyOutputWaitDelay = old })
	dependencyOutputWaitDelay = 300 * time.Millisecond

	script := "echo main-ok; sleep 20 & echo bg-started"
	if runtime.GOOS == "windows" {
		script = "echo main-ok & start /b ping -n 20 127.0.0.1"
	}
	start := time.Now()
	runCmdWithSSE(testShellCommand(script), dep.ID, model.DepStatusInstalled, false)
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("后台进程攥着输出管道时应在 WaitDelay 到点后收尾，实际等了 %s", elapsed)
	}

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusInstalled {
		t.Fatalf("命令本体以 0 退出，应记为 installed，实际 %s：\n%s", got.Status, got.Log)
	}
	if !strings.Contains(got.Log, "main-ok") || !strings.Contains(got.Log, "仍有它拉起的后台进程持有输出管道") {
		t.Fatalf("日志应保留输出并说明有后台进程攥着管道：\n%s", got.Log)
	}
	if strings.Contains(got.Log, "[读取安装输出失败]") {
		t.Fatalf("不应出现读取失败行：\n%s", got.Log)
	}
}

// 单行超过读协程的 256KB 上限之后还有大量输出：读协程必须把剩下的读空再走。
// 不读空的话，旧实现里子进程写满管道就卡住、直到依赖超时（默认 20 分钟）；
// 换成 io.Pipe 之后更是 Wait 永久卡死、连取消都救不回来。
func TestRunCmdWithSSEDrainsOutputAfterOverlongLine(t *testing.T) {
	dep := setupRunCmdTest(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runCmdWithSSE(dependencyOutputHelperCommand("longline"), dep.ID, model.DepStatusInstalled, false)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		cancelDepOperation(dep.ID)
		t.Fatal("单行超长之后命令卡住了：读协程没有把剩下的输出读空")
	}

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusInstalled {
		t.Fatalf("命令以 0 退出应记为 installed，实际 %s", got.Status)
	}
	if !strings.Contains(got.Log, "[读取安装输出失败] bufio.Scanner: token too long") {
		t.Fatalf("应写明超长单行导致后续输出未收集，实际日志末尾：\n%s", tailOfDependencyLog(got.Log, 400))
	}
}

func tailOfDependencyLog(text string, n int) string {
	if len(text) <= n {
		return text
	}
	return "..." + text[len(text)-n:]
}
