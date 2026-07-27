package handler

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/service"
)

var scriptInterpreterMap = map[string][]string{
	".py":  {"python", "-u"},
	".js":  {"node"},
	".mjs": {"node"},
	".ts":  {"npx", "ts-node"},
	".sh":  {"bash"},
	".go":  {"go", "run"},
}

var scriptLanguageExtMap = map[string]string{
	"python":     ".py",
	"javascript": ".js",
	"node":       ".mjs",
	"mjs":        ".mjs",
	"typescript": ".ts",
	"shell":      ".sh",
	"go":         ".go",
}

func newDebugRun() *debugRun {
	return &debugRun{
		Logs:   []string{},
		Status: "running",
	}
}

func (h *ScriptHandler) storeRun(runID string, run *debugRun) {
	h.mu.Lock()
	h.debugRuns[runID] = run
	h.mu.Unlock()
}

func (h *ScriptHandler) loadRun(runID string) (*debugRun, bool) {
	h.mu.Lock()
	run, exists := h.debugRuns[runID]
	h.mu.Unlock()
	return run, exists
}

func (h *ScriptHandler) deleteRun(runID string) (*debugRun, bool) {
	h.mu.Lock()
	run, exists := h.debugRuns[runID]
	if exists {
		delete(h.debugRuns, runID)
	}
	h.mu.Unlock()
	return run, exists
}

func (run *debugRun) setProcess(process *os.Process) {
	run.mu.Lock()
	run.Process = process
	run.mu.Unlock()
}

func (run *debugRun) appendLog(line string) {
	run.mu.Lock()
	run.Logs = append(run.Logs, line)
	run.mu.Unlock()
}

func (run *debugRun) logOutput() string {
	run.mu.Lock()
	defer run.mu.Unlock()
	return strings.Join(run.Logs, "\n")
}

func (run *debugRun) logOutputSince(offset int) string {
	run.mu.Lock()
	defer run.mu.Unlock()
	if offset >= len(run.Logs) {
		return ""
	}
	return strings.Join(run.Logs[offset:], "\n")
}

func (run *debugRun) logLen() int {
	run.mu.Lock()
	defer run.mu.Unlock()
	return len(run.Logs)
}

func (run *debugRun) snapshot() ([]string, bool, *int, string) {
	run.mu.Lock()
	defer run.mu.Unlock()

	logs := make([]string, len(run.Logs))
	copy(logs, run.Logs)

	var exitCode *int
	if run.ExitCode != nil {
		value := *run.ExitCode
		exitCode = &value
	}

	return logs, run.Done, exitCode, run.Status
}

func (run *debugRun) stop() {
	run.mu.Lock()
	defer run.mu.Unlock()

	if run.Process == nil || run.Done {
		return
	}

	service.KillProcessGroup(run.Process)
	run.Status = "stopped"
	exitCode := -1
	run.ExitCode = &exitCode
	run.Done = true
	run.Logs = append(run.Logs, "[调试运行已停止]")
}

func (run *debugRun) killIfRunning() {
	run.mu.Lock()
	defer run.mu.Unlock()

	if run.Process != nil && !run.Done {
		service.KillProcessGroup(run.Process)
	}
}

func (run *debugRun) isStopped() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.Status == "stopped"
}

func (run *debugRun) finish(exitCode int, waitErr error, elapsed float64) {
	run.mu.Lock()
	defer run.mu.Unlock()

	if run.Status == "stopped" {
		return
	}

	run.ExitCode = &exitCode
	run.Done = true
	if exitCode == 0 {
		run.Status = "success"
		run.Logs = append(run.Logs, fmt.Sprintf("[进程结束, 退出码: %d, 耗时: %.2f秒]", exitCode, elapsed))
		return
	}

	run.Status = "failed"
	errMsg := ""
	if waitErr != nil {
		errMsg = waitErr.Error()
	}
	if errMsg != "" {
		run.Logs = append(run.Logs, fmt.Sprintf("[进程异常退出, 退出码: %d, 错误: %s, 耗时: %.2f秒]", exitCode, errMsg, elapsed))
		return
	}
	run.Logs = append(run.Logs, fmt.Sprintf("[进程异常退出, 退出码: %d, 耗时: %.2f秒]", exitCode, elapsed))
}

func scriptCommandParts(ext, target string) ([]string, error) {
	baseCmd, ok := scriptInterpreterMap[ext]
	if !ok {
		return nil, fmt.Errorf("不支持执行此文件类型")
	}

	if ext == ".sh" {
		if err := service.NormalizeShellScriptFile(target); err != nil {
			return nil, fmt.Errorf("脚本换行规范化失败: %w", err)
		}
	}

	cmdParts := append([]string{}, baseCmd...)
	cmdParts = append(cmdParts, target)
	return cmdParts, nil
}

func scriptRuntimeInterpreter(ext string) (string, error) {
	switch ext {
	case ".py":
		return "python3", nil
	case ".js", ".mjs":
		return "node", nil
	case ".ts":
		return "ts-node", nil
	case ".sh":
		return "bash", nil
	case ".go":
		return "go", nil
	default:
		return "", fmt.Errorf("不支持执行此文件类型")
	}
}

func buildScriptExecEnv(workDir string) map[string]string {
	envMap, err := service.BuildManagedRuntimeEnvMapForPythonVersion(workDir, config.C.Data.ScriptsDir, nil, 2*time.Hour, "")
	if err != nil {
		return envMap
	}
	return envMap
}

func newScriptCommand(interpreter string, target string, scriptArgs []string, workDir string, envMap map[string]string) (*exec.Cmd, func(), error) {
	return service.CreateManagedCommand(interpreter, target, scriptArgs, workDir, envMap)
}

func startTrackedCommand(cmd *exec.Cmd, run *debugRun) (*io.PipeWriter, chan struct{}, error) {
	pipeReader, pipeWriter := io.Pipe()
	cmd.Stdout = pipeWriter
	cmd.Stderr = pipeWriter

	if err := cmd.Start(); err != nil {
		pipeWriter.Close()
		return nil, nil, err
	}

	run.setProcess(cmd.Process)
	scanDone := collectRunLogs(pipeReader, run)
	return pipeWriter, scanDone, nil
}

func collectRunLogs(reader io.Reader, run *debugRun) chan struct{} {
	done := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			run.appendLog(scanner.Text())
		}
		close(done)
	}()

	return done
}

func waitTrackedCommand(cmd *exec.Cmd, pipeWriter *io.PipeWriter, scanDone chan struct{}) error {
	err := cmd.Wait()
	pipeWriter.Close()
	<-scanDone
	return err
}

func resolveExitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}
