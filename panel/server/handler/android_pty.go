package handler

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func startAndroidPTY(command *exec.Cmd, rows, columns uint16) (*os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = master.Close()
		}
	}()

	ptyNumber, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, err
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		return nil, err
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", ptyNumber), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	defer slave.Close()
	if err := resizeAndroidPTY(master, rows, columns); err != nil {
		return nil, err
	}
	command.Stdin = slave
	command.Stdout = slave
	command.Stderr = slave
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := command.Start(); err != nil {
		return nil, err
	}
	closeOnError = false
	return master, nil
}

func resizeAndroidPTY(master *os.File, rows, columns uint16) error {
	return unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: columns})
}
