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
