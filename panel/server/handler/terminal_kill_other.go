//go:build !linux && !android

package handler

import (
	"os"
	"os/exec"
	"time"
)

// terminateTerminalProcessGroup 的非 Unix 回退：Windows 没有 POSIX 进程组，
// 直接终止主进程，保证 handler 包在 windows 平台可编译。
func terminateTerminalProcessGroup(_ *terminalSession, process *os.Process) {
	_ = process.Kill()
}

// 防止 exec/time import 在极端裁剪下未使用（保留语义一致性）。
var _ = exec.Command
var _ = time.Millisecond
