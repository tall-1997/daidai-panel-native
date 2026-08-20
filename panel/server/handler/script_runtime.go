package handler

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/service"
)

var trackedScriptProcesses sync.Map

var scriptInterpreterMap = map[string][]string{
	".py":  {"python", "-u"},
	".js":  {"node"},
	".mjs": {"node"},
	".ts":  {"npx", "ts-node"},
	".sh":  {"bash"},
	".go":  {service.RuntimeIDYaegiGo},
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

func (run *debugRun) setSupervisedProcess(process service.SupervisedProcess) {
	run.mu.Lock()
	run.Managed = process
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

	if run.Managed != nil {
		_ = run.Managed.Cancel()
	} else {
		service.KillProcessGroup(run.Process)
	}
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
		if run.Managed != nil {
			_ = run.Managed.Cancel()
		} else {
			service.KillProcessGroup(run.Process)
		}
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
		return service.RuntimeIDYaegiGo, nil
	default:
		return "", fmt.Errorf("不支持执行此文件类型")
	}
}

// scriptDebugEnvTTL 是脚本编辑器「调试运行 / 运行代码」注入凭据的兜底有效期。
// 主控手段是运行结束后的吊销（见 DebugRun / RunCode），这里只在面板被 kill -9 时兜底。
const scriptDebugEnvTTL = 2 * time.Hour

// buildScriptExecEnv 返回调试运行的环境，以及注入其中的那枚 operator 凭据。
// 调用方必须在运行结束（含启动失败、被手动停止）后调 service.RevokeScriptToken 吊销它，
// 否则这枚凭据会一直有效到 scriptDebugEnvTTL 到期。
func buildScriptExecEnv(workDir string) (map[string]string, *service.ScriptTokenInfo) {
	// 与原实现一致：构建部分失败时仍返回已拼好的 env（通知 helper 不可用而已，脚本照跑）。
	envMap, scriptToken, _ := service.BuildManagedRuntimeEnvMapWithScriptToken(workDir, config.C.Data.ScriptsDir, nil, scriptDebugEnvTTL, "")
	return envMap, scriptToken
}

func newScriptCommand(interpreter string, target string, scriptArgs []string, workDir string, envMap map[string]string) (*exec.Cmd, func(), error) {
	if interpreter == service.RuntimeIDYaegiGo || interpreter == service.RuntimeIDGoBuilderAndroidARM {
		return service.CreateScriptRuntimeCommand(interpreter, target, scriptArgs, workDir, envMap)
	}
	return service.CreateManagedCommand(interpreter, target, scriptArgs, workDir, envMap)
}

func startTrackedCommand(cmd *exec.Cmd, run *debugRun) (*io.PipeWriter, chan struct{}, error) {
	pipeReader, pipeWriter := io.Pipe()
	proc, err := service.GetProcessSupervisor().Start(context.Background(), service.ProcessSpec{
		Argv:        cmd.Args,
		Env:         cmd.Env,
		WorkingDir:  cmd.Dir,
		AllowedRoot: cmd.Dir,
		Timeout:     2 * time.Hour,
		Quota:       service.ProcessResourceQuota{MaxOutputBytes: 10 * 1024 * 1024},
	}, func(event service.ProcessEvent) {
		_, _ = pipeWriter.Write(event.Data)
	})
	if err != nil {
		pipeWriter.Close()
		return nil, nil, err
	}
	run.setSupervisedProcess(proc)
	trackedScriptProcesses.Store(cmd, proc)
	process, _ := os.FindProcess(proc.PID())
	run.setProcess(process)
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
	var err error
	if raw, ok := trackedScriptProcesses.LoadAndDelete(cmd); ok {
		_, err = raw.(service.SupervisedProcess).Wait()
	} else {
		err = cmd.Wait()
	}
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
