package handler

import (
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAndroidPTYStartsInteractiveProcessWithChildControllingTerminal(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", "printf 'PTY_OK\\n'")
	master, err := startAndroidPTY(command, 24, 80)
	if err != nil {
		t.Fatalf("startAndroidPTY: %v", err)
	}
	defer master.Close()

	done := make(chan struct{})
	var output []byte
	var readErr error
	go func() {
		output, readErr = io.ReadAll(master)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("PTY read timed out")
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("command.Wait: %v", err)
	}
	if readErr != nil && !strings.Contains(readErr.Error(), "input/output error") {
		t.Fatalf("PTY read: %v", readErr)
	}
	if !strings.Contains(string(output), "PTY_OK") {
		t.Fatalf("output=%q", output)
	}
}

func TestClampTerminalSize(t *testing.T) {
	if got := clampTerminalSize(0, 24, 2, 200); got != 24 {
		t.Fatalf("fallback=%d", got)
	}
	if got := clampTerminalSize(1, 24, 2, 200); got != 2 {
		t.Fatalf("minimum=%d", got)
	}
	if got := clampTerminalSize(300, 24, 2, 200); got != 200 {
		t.Fatalf("maximum=%d", got)
	}
}
