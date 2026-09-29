//go:build !linux && !android

package handler

import (
	"fmt"
	"os"
	"os/exec"
)

// startAndroidPTY 在非 Linux/Android 平台没有可用的 /dev/ptmx 生态，
// 返回明确错误，保证 handler 包在 windows/darwin 等平台可正常编译。
func startAndroidPTY(_ *exec.Cmd, _, _ uint16) (*os.File, error) {
	return nil, fmt.Errorf("PTY 终端仅支持 Linux/Android 平台")
}

func resizeAndroidPTY(_ *os.File, _, _ uint16) error {
	return fmt.Errorf("PTY 终端仅支持 Linux/Android 平台")
}
