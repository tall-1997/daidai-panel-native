//go:build linux || android

package handler

import (
	"os/exec"
	"syscall"
	"time"
)

// terminateTerminalProcessGroup 先向整个进程组发送 SIGTERM，
// 500ms 后仍存活则 SIGKILL。PTY 会话的子进程在同一进程组中。
func terminateTerminalProcessGroup(session *terminalSession, process *os.Process) {
	_ = syscall.Kill(-process.Pid, syscall.SIGTERM)
	_ = process.Signal(syscall.SIGTERM)
	time.AfterFunc(500*time.Millisecond, func() {
		session.mu.Lock()
		stillRunning := session.ExitCode == nil
		session.mu.Unlock()
		if stillRunning {
			_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
			_ = process.Kill()
		}
	})
}
