package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ProcessResourceQuota struct {
	MaxOutputBytes int64
	MaxProcesses   int
	MaxMemoryBytes int64
}

var ErrUnsupportedProcessQuota = errors.New("unsupported process quota")

type ProcessSpec struct {
	Argv        []string
	Env         []string
	WorkingDir  string
	AllowedRoot string
	Timeout     time.Duration
	Quota       ProcessResourceQuota
}

type ProcessEvent struct {
	Stream string
	Data   []byte
}

type ProcessResult struct {
	ExitCode int
	TimedOut bool
	Canceled bool
	Output   string
	// Err 是 Wait 的原始错误：调用方据此判断是否被信号终止（describeTerminationSignal）。
	Err error
}

type SupervisedProcess interface {
	PID() int
	Wait() (ProcessResult, error)
	Cancel() error
}

type ProcessSupervisor interface {
	Start(context.Context, ProcessSpec, func(ProcessEvent)) (SupervisedProcess, error)
}

type DefaultProcessSupervisor struct{}

var defaultProcessSupervisor ProcessSupervisor = DefaultProcessSupervisor{}

func GetProcessSupervisor() ProcessSupervisor {
	return defaultProcessSupervisor
}

func SetProcessSupervisor(supervisor ProcessSupervisor) func() {
	previous := defaultProcessSupervisor
	if supervisor == nil {
		defaultProcessSupervisor = DefaultProcessSupervisor{}
	} else {
		defaultProcessSupervisor = supervisor
	}
	return func() { defaultProcessSupervisor = previous }
}

func (DefaultProcessSupervisor) Start(parent context.Context, spec ProcessSpec, onEvent func(ProcessEvent)) (SupervisedProcess, error) {
	argv := cleanManagedProcessArgs(spec.Argv)
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return nil, fmt.Errorf("process argv is empty")
	}
	if err := validateProcessQuota(spec.Quota); err != nil {
		return nil, err
	}
	workDir, err := validateProcessWorkingDir(spec.WorkingDir, spec.AllowedRoot)
	if err != nil {
		return nil, err
	}
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	if spec.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	cmd := androidManagedCommandContext(ctx, argv[0], argv[1:], workDir)
	cmd.Env = filterProcessEnv(spec.Env)
	androidFinalizeCommand(cmd)
	setPgid(cmd)

	proc := &defaultSupervisedProcess{
		cmd:     cmd,
		ctx:     ctx,
		cancel:  cancel,
		onEvent: onEvent,
		quota:   spec.Quota,
	}
	cmd.Stdout = processEventWriter{process: proc, stream: "stdout"}
	cmd.Stderr = processEventWriter{process: proc, stream: "stderr"}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	// 不在这里挂「ctx 结束就杀整组」的协程：Wait 正常收尾时也会 cancel，
	// 那会把任务有意留在后台的进程（nohup / &）一起杀掉，清理与否必须由调用方按配置决定。
	// 超时 / 取消的整组终止在 Wait 的 select 分支里做。
	return proc, nil
}

type defaultSupervisedProcess struct {
	cmd       *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	onEvent   func(ProcessEvent)
	quota     ProcessResourceQuota
	outputMu  sync.Mutex
	output    strings.Builder
	collected int64
	canceled  bool
	cancelMu  sync.Mutex
}

func (p *defaultSupervisedProcess) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *defaultSupervisedProcess) Wait() (ProcessResult, error) {
	// 等主进程退出；ctx 超时 / 取消时终止整组，再等回收。
	done := make(chan error, 1)
	go func() { done <- p.cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-p.ctx.Done():
		// 先 SIGTERM 再宽限后 SIGKILL（TerminateProcessGroup 不阻塞），让 trap 了 TERM 的脚本能收尾。
		TerminateProcessGroup(p.cmd.Process)
		err = <-done
	}
	p.cancel()
	exitCode := 0
	if err != nil {
		exitCode = 1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}
	p.cancelMu.Lock()
	canceled := p.canceled
	p.cancelMu.Unlock()
	timedOut := p.ctx.Err() == context.DeadlineExceeded
	if timedOut || canceled {
		KillProcessGroup(p.cmd.Process)
	}
	p.outputMu.Lock()
	output := p.output.String()
	p.outputMu.Unlock()
	return ProcessResult{ExitCode: exitCode, TimedOut: timedOut, Canceled: canceled, Output: output, Err: err}, err
}

func (p *defaultSupervisedProcess) Cancel() error {
	p.cancelMu.Lock()
	p.canceled = true
	p.cancelMu.Unlock()
	p.cancel()
	KillProcessGroup(p.cmd.Process)
	return nil
}

type processEventWriter struct {
	process *defaultSupervisedProcess
	stream  string
}

func (writer processEventWriter) Write(payload []byte) (int, error) {
	if len(payload) == 0 {
		return 0, nil
	}
	writer.process.emit(writer.stream, payload)
	return len(payload), nil
}

func (p *defaultSupervisedProcess) emit(stream string, chunk []byte) {
	p.outputMu.Lock()
	defer p.outputMu.Unlock()
	if p.quota.MaxOutputBytes > 0 && p.collected >= p.quota.MaxOutputBytes {
		return
	}
	data := append([]byte{}, chunk...)
	if p.quota.MaxOutputBytes > 0 && p.collected+int64(len(data)) > p.quota.MaxOutputBytes {
		data = data[:p.quota.MaxOutputBytes-p.collected]
	}
	p.collected += int64(len(data))
	p.output.Write(data)
	if p.onEvent != nil {
		p.onEvent(ProcessEvent{Stream: stream, Data: data})
	}
}

func validateProcessQuota(quota ProcessResourceQuota) error {
	if quota.MaxProcesses > 0 {
		return fmt.Errorf("%w: MaxProcesses", ErrUnsupportedProcessQuota)
	}
	if quota.MaxMemoryBytes > 0 {
		return fmt.Errorf("%w: MaxMemoryBytes", ErrUnsupportedProcessQuota)
	}
	return nil
}

func validateProcessWorkingDir(workDir, allowedRoot string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	realWork, err := filepath.EvalSymlinks(absWork)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(allowedRoot) == "" {
		return realWork, nil
	}
	absRoot, err := filepath.Abs(allowedRoot)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, realWork)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("working directory is outside allowed root")
	}
	return realWork, nil
}

func filterProcessEnv(env []string) []string {
	if len(env) == 0 {
		env = os.Environ()
	}
	blocked := map[string]bool{
		"BASH_ENV":                   true,
		"DYLD_FALLBACK_LIBRARY_PATH": true,
		"DYLD_FRAMEWORK_PATH":        true,
		"DYLD_INSERT_LIBRARIES":      true,
		"DYLD_LIBRARY_PATH":          true,
		"ENV":                        true,
		"GCONV_PATH":                 true,
		"IFS":                        true,
		"JAVA_TOOL_OPTIONS":          true,
		"LD_AUDIT":                   true,
		"LD_DEBUG":                   true,
		"LD_LIBRARY_PATH":            true,
		"LD_ORIGIN_PATH":             true,
		"LD_PRELOAD":                 true,
		"NODE_OPTIONS":               true,
		"PERL5LIB":                   true,
		"PYTHONHOME":                 true,
		"PYTHONPATH":                 true,
		"RUBYOPT":                    true,
		"SHELLOPTS":                  true,
		"_JAVA_OPTIONS":              true,
	}
	result := make([]string, 0, len(env))
	for _, entry := range env {
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 || strings.ContainsRune(entry, 0) {
			continue
		}
		key := entry[:idx]
		if blocked[strings.ToUpper(key)] {
			continue
		}
		result = append(result, entry)
	}
	return result
}
