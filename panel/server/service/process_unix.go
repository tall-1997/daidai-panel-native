//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func setPgid(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func SetPgid(cmd *exec.Cmd) {
	setPgid(cmd)
}

func killGroup(p *os.Process) {
	syscall.Kill(-p.Pid, syscall.SIGKILL)
}

func killGroupByPid(pid int) {
	// pid <= 1 不发信号：Kill(0, …) / Kill(-1, …) 会打到面板自己的进程组 / 所有能打的进程，理由同 KillProcessByPid 与下方 signalGroupTerm。
	if pid <= 1 {
		return
	}
	syscall.Kill(-pid, syscall.SIGKILL)
}

// signalGroupTerm 给以 pid 为组号的整个进程组发 SIGTERM（#159 修复 B 的「先 TERM」）。
// 返回 false 表示组已经不在（ESRCH）或信号发不出去，调用方改走立即 KillProcessGroup。
// pid <= 1 一律返回 false：Kill(0, …) 会打到面板自己的进程组，Kill(-1, …) 会打到所有能打的进程，
// 任务进程不可能是这两个号，这里只是防误用。
func signalGroupTerm(pid int) bool {
	if pid <= 1 {
		return false
	}
	return syscall.Kill(-pid, syscall.SIGTERM) == nil
}

// processGroupAlive 判断以 pid 为组号的进程组里是否还有进程（#159 修复 A / B）。
// Kill(-pid, 0) 只做检查、不发信号。还没被回收的僵尸也算「还在」：
// 极少数情况下会多写一行清理提示，或者让宽限等待多等一小会儿，无害。
// pid <= 1 一律按「组已经不在」处理，理由同 signalGroupTerm。
func processGroupAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	return syscall.Kill(-pid, 0) == nil
}

// describeTerminationSignal 从 cmd.Wait() 的错误里判断「这个进程是不是被信号杀掉的」，
// 是的话返回一个人能看懂的描述（例如 killed(9) / terminated(15)），否则返回空串。
//
// 为什么需要它：进程被信号终止时 exec.ExitError.ExitCode() 恒为 -1，
// 脚本日志里只剩一句「退出码 -1」，用户没法区分是 OOM Killer 杀的、面板停止的，还是外部 kill 的。
// runSingleCommand 拿它来补一行可见提示（调用点有更详细的说明）。
//
// 正常退出（哪怕退出码非 0）、以及 waitErr 不是 *exec.ExitError 的情况，都返回空串，
// 这样调用方一个 != "" 判断就够，不用再关心平台差异。
func describeTerminationSignal(waitErr error) string {
	exitErr, ok := waitErr.(*exec.ExitError)
	if !ok {
		return ""
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	sig := status.Signal()
	return fmt.Sprintf("%s(%d)", sig.String(), int(sig))
}
