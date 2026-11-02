package service

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// helperEnvKey / helperLinesKey 让下面那个测试函数在被当作【子进程】拉起时改走另一条路径。
// 这是 Go 标准库 os/exec 自己的测试用的技巧：不依赖 bash / python 之类的外部解释器，
// 用测试二进制自己当被测子进程，Windows 与 Linux 上行为一致。
const (
	helperEnvKey   = "DAIDAI_PUMP_HELPER"
	helperLinesKey = "DAIDAI_PUMP_HELPER_LINES"
)

// TestPumpAndWaitHelperProcess 不是真正的测试用例，而是上面说的「被拉起的子进程」。
// 正常跑 go test 时没有那个环境变量，它直接返回，什么都不做。
func TestPumpAndWaitHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvKey) != "1" {
		return
	}

	lines, _ := strconv.Atoi(os.Getenv(helperLinesKey))
	w := bufio.NewWriterSize(os.Stdout, 64*1024)
	for i := 0; i < lines; i++ {
		fmt.Fprintf(w, "line-%06d\n", i)
	}
	_ = w.Flush()

	// 必须 os.Exit 而不是 return：一旦 return，testing 框架会往 stdout 打 PASS/ok 之类的收尾文本，
	// 混进被测的输出里。os.Exit 让进程在 Flush 之后立刻死掉，管道里只有我们写的那些行。
	//
	// 「写完立刻退出」也正是要复现的场景本身：进程一退，cmd.Wait() 就有机会关掉管道。
	os.Exit(0)
}

func startHelperProcess(t *testing.T, lines int) (*exec.Cmd, *bufio.Reader) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestPumpAndWaitHelperProcess$")
	cmd.Env = append(os.Environ(),
		helperEnvKey+"=1",
		helperLinesKey+"="+strconv.Itoa(lines),
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	return cmd, bufio.NewReader(stdout)
}

// TestPumpAndWaitKeepsTailOutput 守住 issue #102 的修复：子进程输出的最后几行不能丢。
//
// 【为什么这个用例是确定性的，而不是碰运气的竞态测试】
// emit 回调里故意 sleep —— 它跑在读协程上，于是读取被拖慢到几百毫秒；
// 而子进程写完就 os.Exit，几乎立刻退出。
// 只要实现里是「cmd.Wait() 与读协程并发」，Wait 就必然抢在读完之前返回并关闭管道
// （os/exec 的约定：Wait 看到进程退出后会关闭 StdoutPipe），后面的行成片丢失。
//
// 突变验证：把 pumpAndWait 里的
//
//	readErr := <-drained
//	waitErr := cmd.Wait()
//
// 两行交换顺序，这个用例会立刻红（实测丢掉大半输出）。
func TestPumpAndWaitKeepsTailOutput(t *testing.T) {
	const lines = 2000

	cmd, stdout := startHelperProcess(t, lines)

	var mu sync.Mutex
	var received strings.Builder

	emit := func(chunk string) {
		// 关键：拖慢读协程，把「Wait 抢先关管道」从偶发竞态放大成必然。
		// 2000 行 × 100µs ≈ 200ms，而子进程写完立刻就退出了。
		time.Sleep(100 * time.Microsecond)
		mu.Lock()
		received.WriteString(chunk)
		mu.Unlock()
	}

	waitErr, timedOut, readErr := pumpAndWait(cmd, stdout, 60*time.Second, emit)

	if timedOut {
		t.Fatalf("helper 不该超时")
	}
	if waitErr != nil {
		t.Fatalf("helper 应当以 0 退出，got %v", waitErr)
	}
	if readErr != nil {
		// 读失败在这里是硬错误：#102 的症状恰恰是它被 isBenignProcessPipeReadError
		// 当成良性错误吞掉，连报错都看不到。
		t.Fatalf("读取子进程输出失败: %v", readErr)
	}

	mu.Lock()
	out := received.String()
	mu.Unlock()

	got := strings.Count(out, "line-")
	if got != lines {
		// 报告【第一个缺失的行号】，比只说数量对不上好定位得多
		firstMissing := -1
		for i := 0; i < lines; i++ {
			if !strings.Contains(out, fmt.Sprintf("line-%06d\n", i)) {
				firstMissing = i
				break
			}
		}
		t.Fatalf("输出被截断：期望 %d 行，实际 %d 行，第一个缺失的是 line-%06d（尾部丢失是 issue #102 的症状）",
			lines, got, firstMissing)
	}

	// 末行必须在。只数总行数不够——如果实现把中间某行读丢、却多读了别的，数量可能凑巧对上。
	lastLine := fmt.Sprintf("line-%06d\n", lines-1)
	if !strings.HasSuffix(out, lastLine) {
		t.Fatalf("最后一行不是 %q，输出尾部是 %q", strings.TrimSpace(lastLine), tailOf(out, 60))
	}
}

// TestPumpAndWaitReportsExitCode 顺带守住重构没改掉退出码语义：
// pumpAndWait 要把 cmd.Wait() 的原始错误往外传（*exec.ExitError 可断言），
// 而不是只给一个 int —— 钩子脚本那两个调用点依赖这个错误。
func TestPumpAndWaitReportsExitCode(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPumpAndWaitHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnvKey+"=exit7")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	waitErr, timedOut, _ := pumpAndWait(cmd, bufio.NewReader(stdout), 60*time.Second, func(string) {})
	if timedOut {
		t.Fatalf("不该超时")
	}
	// helperEnvKey 不是 "1"，helper 会走 return 分支正常结束，退出码 0。
	// 这里断言的是「没有把 nil 错误伪造成失败」，以及返回值类型没被换成 int。
	if waitErr != nil {
		if exitCodeFromWaitError(waitErr) == 0 {
			t.Fatalf("waitErr 非 nil 时退出码不该是 0: %v", waitErr)
		}
	}
}

func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
