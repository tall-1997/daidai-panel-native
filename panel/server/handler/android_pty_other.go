//go:build !linux
// +build !linux

package handler

import (
	"errors"
	"os"
	"os/exec"
)

// startAndroidPTY is a stub for non-Linux platforms where PTY syscalls are
// unavailable. It always returns an error so callers fail gracefully during
// local development on Windows/macOS without affecting production Linux builds.
func startAndroidPTY(command *exec.Cmd, rows, columns uint16) (*os.File, error) {
	return nil, errors.New("android PTY is only supported on Linux")
}

// resizeAndroidPTY is a no-op stub for non-Linux platforms.
func resizeAndroidPTY(master *os.File, rows, columns uint16) error {
	return nil
}
