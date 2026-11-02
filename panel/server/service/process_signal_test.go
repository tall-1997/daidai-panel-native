package service

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// signalHelperEnvKey 让下面那个 Test 在被当作【子进程】拉起时改走「睡着等被杀」的分支。
// 沿用同包 script_runner_output_tail_test.go 的做法：用测试二进制自己当被测子进程，
// 不依赖 sh / python 之类的外部程序，Windows 与 Linux 上都能跑。
const signalHelperEnvKey = "DAIDAI_SIGNAL_HELPER"

// TestSignalHelperProcess 不是真正的用例，而是上面说的那个子进程。
// 正常 go test 时没有那个环境变量，它直接返回，什么都不做。
func TestSignalHelperProcess(t *testing.T) {
	if os.Getenv(signalHelperEnvKey) != "1" {
		return
	}
	// 睡到被父进程杀掉为止。真跑满这 60s 说明父进程那边出了问题，
	// 退出码用 0，好和「被信号终止」区分开。
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

// 被信号杀掉的进程在 Go 里 ExitCode() 恒为 -1，日志里只会留下「退出码 -1」，
// 用户没法区分是 OOM Killer、手动停止还是外部 kill。
// describeTerminationSignal 就是把这个静默现象变成可诊断的，这里守住它真的认得出来。
func TestDescribeTerminationSignalReportsKilledProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalHelperProcess$")
	cmd.Env = append(os.Environ(), signalHelperEnvKey+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	// 给子进程一点时间真正跑起来，避免在 exec 还没完成时就被杀。
	time.Sleep(200 * time.Millisecond)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	waitErr := cmd.Wait()

	got := describeTerminationSignal(waitErr)

	if runtime.GOOS == "windows" {
		// Windows 没有 POSIX 信号语义，被 TerminateProcess 结束的进程拿到的是普通退出码，
		// 不会退化成 -1，所以那行提示本来就不该出现（见 process_windows.go）。
		if got != "" {
			t.Fatalf("windows 上不该给出信号描述，got %q", got)
		}
		return
	}

	if got == "" {
		t.Fatalf("被 SIGKILL 杀掉的进程应当能识别出信号，waitErr=%v", waitErr)
	}
	// 描述里要带信号编号，否则用户还是不知道到底是什么信号。
	if !strings.Contains(got, "9") {
		t.Fatalf("期望描述里包含 SIGKILL 的编号 9，实际 %q", got)
	}
}

// 正常结束、以及不是 *exec.ExitError 的错误，都不能被当成「被信号终止」，
// 否则 runSingleCommand 会给每个失败任务都多打一行误导性的解释。
func TestDescribeTerminationSignalIgnoresNonSignalErrors(t *testing.T) {
	if got := describeTerminationSignal(nil); got != "" {
		t.Fatalf("nil 错误不该有信号描述，got %q", got)
	}
	if got := describeTerminationSignal(errors.New("failed to start process")); got != "" {
		t.Fatalf("普通错误不该有信号描述，got %q", got)
	}

	// 普通非 0 退出（脚本自己 return 非 0）同样不该命中。
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalHelperProcess$")
	// 不设 signalHelperEnvKey，helper 会立刻 return，测试框架正常收尾、退出码 0。
	cmd.Env = append(os.Environ(), signalHelperEnvKey+"=0")
	waitErr := cmd.Run()
	if got := describeTerminationSignal(waitErr); got != "" {
		t.Fatalf("正常退出的子进程不该有信号描述，got %q（waitErr=%v）", got, waitErr)
	}
}
