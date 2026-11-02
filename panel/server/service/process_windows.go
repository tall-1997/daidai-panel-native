//go:build windows

package service

import (
	"os"
	"os/exec"
)

func setPgid(cmd *exec.Cmd) {
}

func SetPgid(cmd *exec.Cmd) {
}

func killGroup(p *os.Process) {
}

func killGroupByPid(pid int) {
}

// signalGroupTerm 在 Windows 上恒返回 false：没有 SIGTERM，也没有进程组。
// 调用方据此照旧立刻 KillProcessGroup（等于 p.Kill()），停止、超时、关停的行为和改动前一致（#159 修复 B）。
func signalGroupTerm(pid int) bool {
	return false
}

// processGroupAlive 在 Windows 上恒返回 false：没有进程组可查。
// 修复 A（任务结束后清理残留进程组）因此不会触发，宽限等待也会立刻结束。
func processGroupAlive(pid int) bool {
	return false
}

// describeTerminationSignal 在 Windows 上恒返回空串。
// Windows 没有 POSIX 信号那套语义，进程被 TerminateProcess 结束时拿到的是一个普通退出码
// （不会像 Unix 那样退化成 -1），所以不存在「退出码 -1 但没解释」的现象，
// runSingleCommand 里那行提示自然也就不会打出来。
// 保留同名同签名的空实现，只是为了让调用方不用写平台分支。
func describeTerminationSignal(waitErr error) string {
	return ""
}
