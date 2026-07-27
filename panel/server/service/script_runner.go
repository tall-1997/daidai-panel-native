package service

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"daidai-panel/pkg/pathutil"
)

type ScriptResult struct {
	ReturnCode int
	Output     string
	Truncated  bool
}

// 日志回调现在直接传原始输出片段。
// 这样可以把裸 \r 也保留下来，让前端有机会按终端语义做单行覆盖刷新。
type OnOutputFunc func(chunk string)

type OnProcessStartFunc func(process *os.Process)

type commandExecutionMode string

const (
	commandModeNormal commandExecutionMode = "normal"
	commandModeNow    commandExecutionMode = "now"
	commandModeConc   commandExecutionMode = "conc"
	commandModeDesi   commandExecutionMode = "desi"
)

type CommandExecutionPlan struct {
	Interpreter        string
	FullPath           string
	ManagedCommand     string
	PythonModule       string
	WorkDir            string
	ScriptArgs         []string
	TimeoutOverride    *int
	SkipRandomDelay    bool
	SuppressLiveOutput bool
	Mode               commandExecutionMode
	EnvName            string
	AccountSpec        string
}

type taskAccountSelection struct {
	Index int
	Value string
}

func RunCommand(command, scriptsDir string, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
	plan, err := ParseCommandExecutionPlan(command, scriptsDir)
	if err != nil {
		return nil, nil, err
	}
	return RunCommandWithPlan(plan, timeout, envVars, maxLogSize, onOutput, onProcessStart...)
}

func RunCommandWithPlan(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
	if plan == nil {
		return nil, nil, fmt.Errorf("命令执行计划不能为空")
	}

	effectiveTimeout := timeout
	if plan.TimeoutOverride != nil && *plan.TimeoutOverride > 0 {
		effectiveTimeout = *plan.TimeoutOverride
	}

	if plan.Mode == commandModeConc {
		return runConcurrentCommand(plan, effectiveTimeout, envVars, maxLogSize, onOutput, onProcessStart...)
	}

	resolvedEnv, err := applyCommandEnvOverrides(plan, envVars)
	if err != nil {
		return nil, nil, err
	}

	return runSingleCommand(plan, effectiveTimeout, resolvedEnv, maxLogSize, onOutput, onProcessStart...)
}

var extInterpreterMap = map[string]string{
	".py":  "python3",
	".js":  "node",
	".mjs": "node",
	".ts":  "ts-node",
	".sh":  "bash",
	".go":  "go",
}

var desiInterpreterMap = map[string]string{
	".js":  "node",
	".mjs": "node",
	".py":  "python3",
	".ts":  "ts-node",
	".sh":  "bash",
	".go":  "go",
}

func ParseCommandExecutionPlan(command, scriptsDir string) (*CommandExecutionPlan, error) {
	tokens, err := splitCommandTokens(command)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("命令格式无效")
	}

	switch tokens[0] {
	case "task":
		return parseTaskCommandPlan(tokens[1:], scriptsDir, "")
	case "desi":
		return parseTaskCommandPlan(tokens[1:], scriptsDir, commandModeDesi)
	case "python", "python3", "python3.10", "python3.11", "python3.12", "node", "ts-node", "bash", "go":
		return parseInterpreterCommandPlan(tokens[0], tokens[1:], scriptsDir)
	default:
		if isManagedExecutableName(tokens[0]) {
			return parseManagedCommandPlan(tokens, scriptsDir)
		}
		return nil, fmt.Errorf("不支持的解释器: %s", tokens[0])
	}
}

func parseTaskCommandPlan(tokens []string, scriptsDir string, forcedMode commandExecutionMode) (*CommandExecutionPlan, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("命令格式无效，缺少脚本路径或依赖命令名")
	}

	plan := &CommandExecutionPlan{
		Mode: forcedMode,
	}

	idx := 0
	for idx < len(tokens) {
		switch tokens[idx] {
		case "-m":
			if idx+1 >= len(tokens) {
				return nil, fmt.Errorf("缺少 -m 对应的超时时间")
			}
			timeoutSeconds, err := parseTaskTimeoutSeconds(tokens[idx+1])
			if err != nil {
				return nil, err
			}
			plan.TimeoutOverride = &timeoutSeconds
			idx += 2
		case "-l":
			idx++
		default:
			goto optionsDone
		}
	}

optionsDone:
	remainingTokens := tokens[idx:]
	taskShellTokens, scriptArgs := splitTaskShellAndScriptArgs(remainingTokens)
	if len(taskShellTokens) == 0 {
		return nil, fmt.Errorf("命令格式无效，缺少脚本路径或依赖命令名")
	}

	fullPath, pathTokenCount, err := findTaskScriptTarget(taskShellTokens, scriptsDir, forcedMode)
	if err != nil {
		managedCommand, managedTokenCount, managedErr := findTaskManagedCommandTarget(taskShellTokens, forcedMode)
		if managedErr != nil {
			return nil, err
		}
		// Python / Node 依赖安装后会在托管 bin 目录暴露可执行命令，例如 dailycheckin。
		// 这类命令不是脚本文件，所以不能继续按 scriptsDir 里的 .py/.js 路径校验。
		plan.ManagedCommand = managedCommand
		plan.WorkDir = scriptsDir
		plan.ScriptArgs = scriptArgs
		remainder := taskShellTokens[managedTokenCount:]
		return applyTaskCommandRemainder(plan, remainder, forcedMode)
	}

	plan.FullPath = fullPath
	plan.WorkDir = filepath.Dir(fullPath)
	plan.ScriptArgs = scriptArgs
	remainder := taskShellTokens[pathTokenCount:]

	ext := strings.ToLower(filepath.Ext(fullPath))
	mapped, ok := extInterpreterMap[ext]
	if !ok {
		if forcedMode == commandModeDesi {
			return nil, fmt.Errorf("desi 命令不支持的文件扩展名: %s", ext)
		}
		return nil, fmt.Errorf("task 命令不支持的文件扩展名: %s", ext)
	}
	plan.Interpreter = mapped
	return applyTaskCommandRemainder(plan, remainder, forcedMode)
}

func applyTaskCommandRemainder(plan *CommandExecutionPlan, remainder []string, forcedMode commandExecutionMode) (*CommandExecutionPlan, error) {
	if forcedMode == commandModeDesi {
		if len(remainder) == 0 {
			return nil, fmt.Errorf("desi 命令缺少环境变量名称")
		}
		plan.Mode = commandModeDesi
		plan.SkipRandomDelay = true
		plan.EnvName = remainder[0]
		plan.AccountSpec = strings.Join(remainder[1:], " ")
		return plan, nil
	}

	if len(remainder) == 0 {
		plan.Mode = commandModeNormal
		return plan, nil
	}

	switch remainder[0] {
	case "now":
		if len(remainder) != 1 {
			return nil, fmt.Errorf("now 模式不支持额外参数")
		}
		plan.Mode = commandModeNow
		plan.SkipRandomDelay = true
	case "conc":
		if len(remainder) < 2 {
			return nil, fmt.Errorf("conc 模式缺少环境变量名称")
		}
		plan.Mode = commandModeConc
		plan.SkipRandomDelay = true
		plan.SuppressLiveOutput = true
		plan.EnvName = remainder[1]
		plan.AccountSpec = strings.Join(remainder[2:], " ")
	case "desi":
		if len(remainder) < 2 {
			return nil, fmt.Errorf("desi 命令缺少环境变量名称")
		}
		plan.Mode = commandModeDesi
		plan.SkipRandomDelay = true
		plan.EnvName = remainder[1]
		plan.AccountSpec = strings.Join(remainder[2:], " ")
	default:
		plan.ScriptArgs = append(remainder, plan.ScriptArgs...)
		plan.SkipRandomDelay = true
	}

	return plan, nil
}

func parseInterpreterCommandPlan(interpreter string, tokens []string, scriptsDir string) (*CommandExecutionPlan, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("命令格式无效，格式: <解释器> <脚本路径>")
	}

	if IsPythonInterpreter(interpreter) && tokens[0] == "-m" {
		if len(tokens) < 2 {
			return nil, fmt.Errorf("python -m 命令缺少模块名")
		}
		if !isSafePythonModuleName(tokens[1]) {
			return nil, fmt.Errorf("python 模块名无效: %s", tokens[1])
		}
		return &CommandExecutionPlan{
			Interpreter:  interpreter,
			PythonModule: tokens[1],
			WorkDir:      scriptsDir,
			ScriptArgs:   append([]string{}, tokens[2:]...),
			Mode:         commandModeNormal,
		}, nil
	}

	fullPath, pathTokenCount, err := findScriptTarget(tokens, scriptsDir)
	if err != nil {
		return nil, err
	}

	return &CommandExecutionPlan{
		Interpreter: interpreter,
		FullPath:    fullPath,
		WorkDir:     filepath.Dir(fullPath),
		ScriptArgs:  append([]string{}, tokens[pathTokenCount:]...),
		Mode:        commandModeNormal,
	}, nil
}

func parseManagedCommandPlan(tokens []string, scriptsDir string) (*CommandExecutionPlan, error) {
	if len(tokens) == 0 || !isManagedExecutableName(tokens[0]) {
		return nil, fmt.Errorf("命令格式无效")
	}
	return &CommandExecutionPlan{
		ManagedCommand: tokens[0],
		WorkDir:        scriptsDir,
		ScriptArgs:     append([]string{}, tokens[1:]...),
		Mode:           commandModeNormal,
	}, nil
}

func splitCommandTokens(command string) ([]string, error) {
	tokens := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for _, r := range command {
		if escaped {
			if r == '\'' || r == '"' || r == '\\' || unicode.IsSpace(r) {
				current.WriteRune(r)
			} else {
				current.WriteRune('\\')
				current.WriteRune(r)
			}
			escaped = false
			continue
		}

		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}

		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			continue
		}

		if r == '\'' || r == '"' {
			quote = r
			continue
		}

		if unicode.IsSpace(r) {
			flush()
			continue
		}

		current.WriteRune(r)
	}

	if escaped {
		current.WriteRune('\\')
	}
	if quote != 0 {
		return nil, fmt.Errorf("命令引号未闭合")
	}
	flush()
	return tokens, nil
}

func splitTaskShellAndScriptArgs(tokens []string) ([]string, []string) {
	for idx, token := range tokens {
		if token == "--" {
			return tokens[:idx], tokens[idx+1:]
		}
	}
	return tokens, nil
}

func findTaskScriptTarget(tokens []string, scriptsDir string, forcedMode commandExecutionMode) (string, int, error) {
	var bestPath string
	bestCount := 0
	var lastResolveErr error

	for count := 1; count <= len(tokens); count++ {
		candidate := strings.Join(tokens[:count], " ")
		if !isSupportedScriptExtension(candidate) {
			continue
		}

		fullPath, err := resolveCommandScriptPath(candidate, scriptsDir)
		if err != nil {
			lastResolveErr = err
			continue
		}

		remainder := tokens[count:]
		if !isValidTaskRemainder(remainder, forcedMode) {
			continue
		}

		bestPath = fullPath
		bestCount = count
	}

	if bestCount == 0 {
		if lastResolveErr != nil {
			return "", 0, lastResolveErr
		}
		return "", 0, fmt.Errorf("脚本不存在或命令格式无效")
	}

	return bestPath, bestCount, nil
}

func findTaskManagedCommandTarget(tokens []string, forcedMode commandExecutionMode) (string, int, error) {
	if len(tokens) == 0 {
		return "", 0, fmt.Errorf("命令格式无效，缺少依赖命令名")
	}
	if filepath.Ext(tokens[0]) != "" || !isManagedExecutableName(tokens[0]) {
		return "", 0, fmt.Errorf("脚本不存在或命令格式无效")
	}
	if !isValidTaskRemainder(tokens[1:], forcedMode) {
		return "", 0, fmt.Errorf("脚本不存在或命令格式无效")
	}
	return tokens[0], 1, nil
}

func findScriptTarget(tokens []string, scriptsDir string) (string, int, error) {
	var bestPath string
	bestCount := 0
	var lastResolveErr error

	for count := 1; count <= len(tokens); count++ {
		candidate := strings.Join(tokens[:count], " ")
		if !isSupportedScriptExtension(candidate) {
			continue
		}

		fullPath, err := resolveCommandScriptPath(candidate, scriptsDir)
		if err != nil {
			lastResolveErr = err
			continue
		}

		bestPath = fullPath
		bestCount = count
	}

	if bestCount == 0 {
		if lastResolveErr != nil {
			return "", 0, lastResolveErr
		}
		return "", 0, fmt.Errorf("脚本不存在或命令格式无效")
	}

	return bestPath, bestCount, nil
}

func isValidTaskRemainder(tokens []string, forcedMode commandExecutionMode) bool {
	if forcedMode == commandModeDesi {
		return len(tokens) >= 1
	}
	if len(tokens) == 0 {
		return true
	}

	switch tokens[0] {
	case "now":
		return len(tokens) == 1
	case "conc", "desi":
		return len(tokens) >= 2
	default:
		return true
	}
}

func isSupportedScriptExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".js", ".mjs", ".ts", ".sh", ".go":
		return true
	default:
		return false
	}
}

func isManagedExecutableName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func isSafePythonModuleName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func resolveCommandScriptPath(scriptPath, scriptsDir string) (string, error) {
	dangerous := []string{"..", "~", "$", "`", ";", "|", "&", ">", "<"}
	for _, d := range dangerous {
		if strings.Contains(scriptPath, d) {
			return "", fmt.Errorf("脚本路径包含危险字符: %s", d)
		}
	}

	fullPath, err := pathutil.ResolveWithinBase(scriptsDir, scriptPath, true)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return "", fmt.Errorf("脚本不存在: %s", scriptPath)
	}
	return fullPath, nil
}

func parseTaskTimeoutSeconds(raw string) (int, error) {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return 0, fmt.Errorf("超时时间不能为空")
	}

	multiplier := 1
	switch suffix := value[len(value)-1]; suffix {
	case 's':
		value = value[:len(value)-1]
	case 'm':
		value = value[:len(value)-1]
		multiplier = 60
	case 'h':
		value = value[:len(value)-1]
		multiplier = 3600
	case 'd':
		value = value[:len(value)-1]
		multiplier = 86400
	}

	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("无效的超时时间: %s", raw)
	}

	return seconds * multiplier, nil
}

func runSingleCommand(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
	cmd, cleanup, err := buildCmd(plan, filepath.Dir(plan.FullPath), envVars)
	if err != nil {
		return nil, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to start process: %w", err)
	}

	process := cmd.Process
	if len(onProcessStart) > 0 && onProcessStart[0] != nil {
		onProcessStart[0](process)
	}

	var outputBuilder strings.Builder
	totalSize := 0
	truncated := false

	// 不再用 Scanner 按行切，因为终端进度条常用裸 \r 覆盖当前行。
	// 这里按字节流切片，把 \n / \r / \r\n 都当成边界保留下来，
	// 让实时日志、历史日志和前端渲染都能还原真实终端语义。
	reader := bufio.NewReaderSize(stdout, 256*1024)

	emitChunk := func(chunk string) {
		if truncated {
			return
		}
		if totalSize >= maxLogSize {
			truncated = true
			msg := "\n[日志已截断，超过最大大小限制]"
			outputBuilder.WriteString(msg)
			if onOutput != nil {
				onOutput(msg)
			}
			return
		}
		outputBuilder.WriteString(chunk)
		totalSize += len(chunk)
		if onOutput != nil {
			onOutput(chunk)
		}
	}

	done := make(chan error, 1)
	go func() {
		defer close(done)
		var chunkBuf strings.Builder
		for {
			text, err := reader.ReadString('\n')
			if len(text) > 0 {
				lastBoundaryIndex := -1
				for i := 0; i < len(text); i++ {
					chunkBuf.WriteByte(text[i])
					if text[i] == '\r' {
						// 兼容 Windows 风格 \r\n，整对一起作为一个边界发出。
						if i+1 < len(text) && text[i+1] == '\n' {
							chunkBuf.WriteByte(text[i+1])
							i++
						}
						emitChunk(chunkBuf.String())
						chunkBuf.Reset()
						lastBoundaryIndex = i
						continue
					}
					if text[i] == '\n' {
						emitChunk(chunkBuf.String())
						chunkBuf.Reset()
						lastBoundaryIndex = i
					}
				}
				if lastBoundaryIndex == -1 {
					// 当前片段里没有换行边界，继续累积，等待后续数据拼完整。
				}
			}
			if err != nil {
				if chunkBuf.Len() > 0 {
					emitChunk(chunkBuf.String())
					chunkBuf.Reset()
				}
				done <- err
				return
			}
		}
	}()

	var timerC <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(time.Duration(timeout) * time.Second)
		timerC = timer.C
		defer timer.Stop()
	}

	waitCh := make(chan error, 1)
	go func() {
		waitErr := cmd.Wait()
		cleanup()
		waitCh <- waitErr
	}()

	var returnCode int
	select {
	case err := <-waitCh:
		readErr := <-done
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				returnCode = exitErr.ExitCode()
			} else {
				returnCode = 1
			}
		}
		if readErr != nil && readErr != io.EOF && totalSize < maxLogSize && !truncated {
			if !isBenignProcessPipeReadError(readErr) {
				emitChunk(fmt.Sprintf("[读取脚本输出失败] %s\n", readErr.Error()))
			}
		}
	case <-timerC:
		KillProcessGroup(cmd.Process)
		readErr := <-done
		<-waitCh
		returnCode = -1
		msg := fmt.Sprintf("\n[任务超时，已在 %d 秒后终止]", timeout)
		outputBuilder.WriteString(msg)
		if onOutput != nil {
			onOutput(msg)
		}
		if readErr != nil && readErr != io.EOF && totalSize < maxLogSize && !truncated && !isBenignProcessPipeReadError(readErr) {
			emitChunk(fmt.Sprintf("[读取脚本输出失败] %s\n", readErr.Error()))
		}
	}

	return &ScriptResult{
		ReturnCode: returnCode,
		Output:     outputBuilder.String(),
		Truncated:  truncated,
	}, process, nil
}

func isBenignProcessPipeReadError(err error) bool {
	if err == nil || err == io.EOF {
		return true
	}

	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" {
		return false
	}

	benignMarkers := []string{
		"file already closed",
		"read |0:",
		"the pipe has been ended",
		"handle is invalid",
		"io: read/write on closed pipe",
	}
	for _, marker := range benignMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}

	return false
}

func runConcurrentCommand(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
	selections, err := resolveTaskAccountSelections(envVars, plan.EnvName, plan.AccountSpec)
	if err != nil {
		return nil, nil, err
	}
	if len(selections) == 0 {
		return nil, nil, fmt.Errorf("未匹配到可执行的账号")
	}

	var outputBuilder strings.Builder
	totalSize := 0
	truncated := false
	var outputMu sync.Mutex
	var firstProcess *os.Process
	var firstProcessMu sync.Mutex

	appendLine := func(line string) {
		outputMu.Lock()
		defer outputMu.Unlock()

		if totalSize < maxLogSize {
			outputBuilder.WriteString(line)
			outputBuilder.WriteString("\n")
			totalSize += len(line) + 1
			if onOutput != nil {
				onOutput(line)
			}
			return
		}

		if !truncated {
			truncated = true
			msg := "[日志已截断，超过最大大小限制]"
			outputBuilder.WriteString(msg)
			outputBuilder.WriteString("\n")
			if onOutput != nil {
				onOutput(msg)
			}
		}
	}

	type concurrentResult struct {
		index      int
		returnCode int
		err        error
	}

	results := make(chan concurrentResult, len(selections))
	var waitGroup sync.WaitGroup

	for _, selection := range selections {
		selection := selection
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			selectionEnv := applyConcurrentAccountEnv(plan, envVars, selection)
			prefixedOutput := func(line string) {
				appendLine(fmt.Sprintf("[%s#%d] %s", plan.EnvName, selection.Index, line))
			}
			processCapture := func(process *os.Process) {
				firstProcessMu.Lock()
				if firstProcess == nil {
					firstProcess = process
				}
				firstProcessMu.Unlock()
				if len(onProcessStart) > 0 && onProcessStart[0] != nil {
					onProcessStart[0](process)
				}
			}

			appendLine(fmt.Sprintf("[%s#%d] 开始执行", plan.EnvName, selection.Index))
			result, _, runErr := runSingleCommand(plan, timeout, selectionEnv, maxLogSize, prefixedOutput, processCapture)
			if runErr != nil {
				appendLine(fmt.Sprintf("[%s#%d] 执行错误: %s", plan.EnvName, selection.Index, runErr.Error()))
				results <- concurrentResult{index: selection.Index, returnCode: 1, err: runErr}
				return
			}

			appendLine(fmt.Sprintf("[%s#%d] 执行完成，退出码 %d", plan.EnvName, selection.Index, result.ReturnCode))
			results <- concurrentResult{index: selection.Index, returnCode: result.ReturnCode}
		}()
	}

	waitGroup.Wait()
	close(results)

	overallCode := 0
	var overallErr error
	for result := range results {
		if result.err != nil && overallErr == nil {
			overallErr = result.err
		}
		if result.returnCode != 0 && overallCode == 0 {
			overallCode = result.returnCode
		}
	}

	return &ScriptResult{
		ReturnCode: overallCode,
		Output:     outputBuilder.String(),
		Truncated:  truncated,
	}, firstProcess, overallErr
}

func resolveTaskAccountSelections(envVars map[string]string, envName, accountSpec string) ([]taskAccountSelection, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return nil, fmt.Errorf("缺少环境变量名称")
	}

	rawValue, exists := envVars[envName]
	if !exists || strings.TrimSpace(rawValue) == "" {
		return nil, fmt.Errorf("环境变量 %s 不存在或为空", envName)
	}

	values := splitTaskEnvValues(rawValue)
	indices, err := parseTaskAccountSpec(accountSpec, len(values))
	if err != nil {
		return nil, err
	}

	selections := make([]taskAccountSelection, 0, len(indices))
	for _, index := range indices {
		selections = append(selections, taskAccountSelection{
			Index: index,
			Value: values[index-1],
		})
	}
	return selections, nil
}

func parseTaskAccountSpec(spec string, total int) ([]int, error) {
	if total <= 0 {
		return nil, fmt.Errorf("环境变量账号数量为空")
	}

	spec = strings.TrimSpace(spec)
	if spec == "" {
		spec = "1-max"
	}

	tokens := strings.FieldsFunc(spec, func(r rune) bool {
		return unicode.IsSpace(r) || r == ','
	})

	seen := make(map[int]bool)
	indices := make([]int, 0)

	for _, token := range tokens {
		if token == "" {
			continue
		}
		expanded, err := expandTaskAccountToken(token, total)
		if err != nil {
			return nil, err
		}
		for _, index := range expanded {
			if !seen[index] {
				seen[index] = true
				indices = append(indices, index)
			}
		}
	}

	if len(indices) == 0 {
		return nil, fmt.Errorf("未匹配到有效的账号序号")
	}

	return indices, nil
}

func expandTaskAccountToken(token string, total int) ([]int, error) {
	token = strings.TrimSpace(strings.ToLower(token))
	for _, sep := range []string{"-", "~", "_"} {
		if strings.Contains(token, sep) {
			parts := strings.SplitN(token, sep, 2)
			start, err := parseTaskAccountEndpoint(parts[0], total)
			if err != nil {
				return nil, err
			}
			end, err := parseTaskAccountEndpoint(parts[1], total)
			if err != nil {
				return nil, err
			}
			step := 1
			if start > end {
				step = -1
			}
			result := make([]int, 0, absInt(start-end)+1)
			for current := start; ; current += step {
				result = append(result, current)
				if current == end {
					break
				}
			}
			return result, nil
		}
	}

	value, err := parseTaskAccountEndpoint(token, total)
	if err != nil {
		return nil, err
	}
	return []int{value}, nil
}

func parseTaskAccountEndpoint(raw string, total int) (int, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "", "max":
		return total, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > total {
		return 0, fmt.Errorf("无效的账号序号: %s", raw)
	}
	return value, nil
}

func applyCommandEnvOverrides(plan *CommandExecutionPlan, envVars map[string]string) (map[string]string, error) {
	cloned := cloneEnvMap(envVars)
	if plan == nil || plan.Mode != commandModeDesi {
		return cloned, nil
	}

	selections, err := resolveTaskAccountSelections(cloned, plan.EnvName, plan.AccountSpec)
	if err != nil {
		return nil, err
	}

	selectedValues := make([]string, 0, len(selections))
	selectedIndexes := make([]string, 0, len(selections))
	for _, selection := range selections {
		selectedValues = append(selectedValues, selection.Value)
		selectedIndexes = append(selectedIndexes, strconv.Itoa(selection.Index))
	}

	cloned[plan.EnvName] = joinTaskEnvValues(selectedValues)
	cloned["envParam"] = plan.EnvName
	cloned["numParam"] = strings.Join(selectedIndexes, " ")
	cloned["TASK_EXEC_MODE"] = string(plan.Mode)
	cloned["TASK_ENV_NAME"] = plan.EnvName
	cloned["TASK_ACCOUNT_SPEC"] = strings.Join(selectedIndexes, " ")
	return cloned, nil
}

func applyConcurrentAccountEnv(plan *CommandExecutionPlan, envVars map[string]string, selection taskAccountSelection) map[string]string {
	cloned := cloneEnvMap(envVars)
	cloned[plan.EnvName] = selection.Value
	cloned["envParam"] = plan.EnvName
	cloned["numParam"] = strconv.Itoa(selection.Index)
	cloned["TASK_EXEC_MODE"] = string(plan.Mode)
	cloned["TASK_ENV_NAME"] = plan.EnvName
	cloned["TASK_ACCOUNT_SPEC"] = strconv.Itoa(selection.Index)
	cloned["TASK_ACCOUNT_NUMBER"] = strconv.Itoa(selection.Index)
	return cloned
}

func cloneEnvMap(envVars map[string]string) map[string]string {
	cloned := make(map[string]string, len(envVars))
	for key, value := range envVars {
		cloned[key] = value
	}
	return cloned
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func buildCmd(plan *CommandExecutionPlan, workDir string, envVars map[string]string) (*exec.Cmd, func(), error) {
	if strings.TrimSpace(plan.WorkDir) != "" {
		workDir = plan.WorkDir
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	if plan.ManagedCommand != "" {
		return createManagedExecutableCommand(plan.ManagedCommand, plan.ScriptArgs, workDir, envVars)
	}
	if plan.PythonModule != "" {
		return createManagedPythonModuleCommand(plan.Interpreter, plan.PythonModule, plan.ScriptArgs, workDir, envVars)
	}

	helperBaseDir := strings.TrimSpace(envVars["DAIDAI_SCRIPTS_DIR"])
	if helperBaseDir != "" {
		_ = EnsureBuiltinNotifyHelpers(helperBaseDir)
		_ = cleanupManagedHelperCopies(helperBaseDir, filepath.Dir(plan.FullPath))
	}
	if plan.Interpreter == "bash" {
		_ = NormalizeShellScriptFile(plan.FullPath)
	}

	return CreateManagedCommand(plan.Interpreter, plan.FullPath, plan.ScriptArgs, workDir, envVars)
}

func buildEnv(envVars map[string]string) []string {
	safeKeys := []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TZ"}
	if runtime.GOOS == "windows" {
		safeKeys = append(safeKeys, "SYSTEMROOT", "PATHEXT", "TEMP", "TMP", "APPDATA", "LOCALAPPDATA", "USERPROFILE")
	}

	env := make([]string, 0)
	for _, key := range safeKeys {
		if val := os.Getenv(key); val != "" {
			env = append(env, key+"="+val)
		}
	}

	dangerousVars := map[string]bool{
		"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "DYLD_INSERT_LIBRARIES": true,
	}

	for k, v := range envVars {
		if dangerousVars[k] {
			continue
		}
		if strings.ContainsRune(v, 0) {
			continue
		}
		env = append(env, k+"="+v)
	}

	// 实时读取 system_configs.proxy_url，把 HTTP_PROXY / HTTPS_PROXY 等
	// 注入 bash / go 等标准命令的执行环境。Python / Node 走 buildBootstrapProcessEnv 已包含此逻辑。
	return AppendProxyEnv(env)
}

func RunInlineScript(content, scriptsDir string, envVars map[string]string, timeout int, onOutput OnOutputFunc, scriptArgs ...string) error {
	tmpFile := filepath.Join(scriptsDir, fmt.Sprintf(".hook_%d.sh", time.Now().UnixNano()))
	if err := os.WriteFile(tmpFile, NormalizeShellLineEndings([]byte(content)), 0755); err != nil {
		return err
	}
	defer os.Remove(tmpFile)

	cmd, cleanup, err := CreateManagedCommand("bash", tmpFile, scriptArgs, scriptsDir, envVars)
	if err != nil {
		return err
	}
	defer cleanup()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReaderSize(stdout, 256*1024)
	go func() {
		for {
			chunk, err := reader.ReadString('\n')
			if len(chunk) > 0 && onOutput != nil {
				onOutput(chunk)
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(time.Duration(timeout) * time.Second)
	defer timer.Stop()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case err := <-waitCh:
		return err
	case <-timer.C:
		KillProcessGroup(cmd.Process)
		<-waitCh
		return fmt.Errorf("钩子脚本超时，已超过 %d 秒", timeout)
	}
}

func RunHookScript(scriptName, scriptsDir string, envVars map[string]string, onOutput OnOutputFunc, scriptArgs ...string) {
	hookPath, err := pathutil.ResolveWithinBase(scriptsDir, scriptName, true)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		return
	}
	if err := NormalizeShellScriptFile(hookPath); err != nil {
		if onOutput != nil {
			onOutput(fmt.Sprintf("[hook %s line ending normalize failed: %s]", scriptName, err))
		}
		return
	}

	cmd, cleanup, err := CreateManagedCommand("bash", hookPath, scriptArgs, scriptsDir, envVars)
	if err != nil {
		if onOutput != nil {
			onOutput(fmt.Sprintf("[hook %s failed to prepare: %s]", scriptName, err))
		}
		return
	}
	defer cleanup()

	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		if onOutput != nil {
			onOutput(fmt.Sprintf("[hook %s failed to start: %s]", scriptName, err))
		}
		return
	}

	reader := bufio.NewReaderSize(stdout, 256*1024)
	go func() {
		for {
			chunk, err := reader.ReadString('\n')
			if len(chunk) > 0 && onOutput != nil {
				onOutput(chunk)
			}
			if err != nil {
				return
			}
		}
	}()

	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case <-waitCh:
	case <-timer.C:
		KillProcessGroup(cmd.Process)
		<-waitCh
	}
}

func cleanProcessArgs(args []string) []string {
	cleaned := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned
}
