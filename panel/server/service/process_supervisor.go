package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type ProcessResourceQuota struct {
	MaxOutputBytes int64
	MaxProcesses   int
	MaxMemoryBytes int64
}

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
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	cmd.Env = filterProcessEnv(spec.Env)
	setPgid(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	proc := &defaultSupervisedProcess{
		cmd:     cmd,
		ctx:     ctx,
		cancel:  cancel,
		onEvent: onEvent,
		quota:   spec.Quota,
	}
	proc.readWG.Add(2)
	go proc.collect("stdout", stdout)
	go proc.collect("stderr", stderr)
	go func() {
		<-ctx.Done()
		if ctx.Err() != nil {
			KillProcessGroup(cmd.Process)
		}
	}()
	return proc, nil
}

type defaultSupervisedProcess struct {
	cmd       *exec.Cmd
	ctx       context.Context
	cancel    context.CancelFunc
	onEvent   func(ProcessEvent)
	quota     ProcessResourceQuota
	readWG    sync.WaitGroup
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
	err := p.cmd.Wait()
	p.readWG.Wait()
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
	return ProcessResult{ExitCode: exitCode, TimedOut: timedOut, Canceled: canceled, Output: output}, err
}

func (p *defaultSupervisedProcess) Cancel() error {
	p.cancelMu.Lock()
	p.canceled = true
	p.cancelMu.Unlock()
	p.cancel()
	KillProcessGroup(p.cmd.Process)
	return nil
}

func (p *defaultSupervisedProcess) collect(stream string, reader io.Reader) {
	defer p.readWG.Done()
	buf := bufio.NewReaderSize(reader, 256*1024)
	for {
		chunk, err := buf.ReadBytes('\n')
		if len(chunk) > 0 {
			p.emit(stream, chunk)
		}
		if err != nil {
			return
		}
	}
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

func validateProcessWorkingDir(workDir, allowedRoot string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(allowedRoot) == "" {
		return absWork, nil
	}
	absRoot, err := filepath.Abs(allowedRoot)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absWork)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("working directory is outside allowed root")
	}
	return absWork, nil
}

func filterProcessEnv(env []string) []string {
	blocked := map[string]bool{
		"LD_PRELOAD":            true,
		"DYLD_INSERT_LIBRARIES": true,
	}
	if runtime.GOOS == "android" {
		blocked["LD_LIBRARY_PATH"] = true
	}
	result := make([]string, 0, len(env))
	for _, entry := range env {
		idx := strings.IndexByte(entry, '=')
		if idx <= 0 || strings.ContainsRune(entry, 0) {
			continue
		}
		key := entry[:idx]
		if blocked[key] {
			continue
		}
		result = append(result, entry)
	}
	return result
}
