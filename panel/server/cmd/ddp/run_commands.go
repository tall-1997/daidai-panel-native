package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"daidai-panel/service"
)

// ddp python / ddp shell 解决的痛点：
// 面板把依赖装进托管 venv（{DATA_DIR}/deps/python/<版本>/），任务执行时由 server 进程
// 注入 venv 的 PATH/PYTHONPATH 才能找到依赖；但用户 docker exec 进来的终端不继承这套环境，
// 直接 python3 会落到引导解释器/系统 python，找不到面板装的包，交互式脚本（如 telethon
// 首次登录要输手机号/验证码）也没法在面板非交互任务里跑。
// 这两个子命令在当前 TTY 前台、用与任务一致的环境（venv + 已装依赖 + 面板环境变量）执行，
// stdin 直通，可交互输入。

// 长 TTL：交互式会话可能持续很久，沿用任务“无超时”场景的 env TTL，保证通知 helper 令牌不过期。
const ddpRuntimeEnvTTL = 365 * 24 * time.Hour

func runRuntimePython(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp python <脚本路径> [参数...]（路径相对脚本目录，或给绝对路径）")
	}

	scriptPath, err := resolveRunnableScript(rt, args[0])
	if err != nil {
		return err
	}

	pythonBin := service.ResolveManagedPythonBinary()
	if pythonBin == "" {
		return fmt.Errorf("面板托管 Python 环境不可用：请先在面板安装一次 Python 依赖以创建 venv，或确认服务器存在受支持的 Python")
	}

	workDir := filepath.Dir(scriptPath)
	envMap, envErr := service.BuildManagedRuntimeEnvMap(workDir, rt.cfg.Data.ScriptsDir, nil, ddpRuntimeEnvTTL)
	if envErr != nil {
		rt.warnings = append(rt.warnings, "构建运行环境部分失败（通知/辅助可能不可用）: "+envErr.Error())
	}

	env := mergeEnvMap(os.Environ(), envMap)
	env = ensureEnvDefault(env, "PYTHONUTF8", "1")
	env = ensureEnvDefault(env, "PYTHONIOENCODING", "utf-8")

	cmdArgs := append([]string{"-u", scriptPath}, args[1:]...)
	cmd := exec.Command(pythonBin, cmdArgs...)
	cmd.Dir = workDir
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Fprintf(os.Stderr, "[ddp] 使用面板 Python: %s\n", pythonBin)
	fmt.Fprintf(os.Stderr, "[ddp] 工作目录: %s\n", workDir)
	return runForeground(rt, cmd)
}

func runRuntimeShell(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) > 0 {
		return fmt.Errorf("用法: ddp shell（无参数；进入面板运行环境的交互 shell）")
	}

	envMap, envErr := service.BuildManagedRuntimeEnvMap(rt.cfg.Data.ScriptsDir, rt.cfg.Data.ScriptsDir, nil, ddpRuntimeEnvTTL)
	if envErr != nil {
		rt.warnings = append(rt.warnings, "构建运行环境部分失败（通知/辅助可能不可用）: "+envErr.Error())
	}

	env := mergeEnvMap(os.Environ(), envMap)
	env = ensureEnvDefault(env, "PYTHONUTF8", "1")
	env = ensureEnvDefault(env, "PYTHONIOENCODING", "utf-8")

	shell := resolveInteractiveShell()
	cmd := exec.Command(shell)
	cmd.Dir = rt.cfg.Data.ScriptsDir
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Fprintf(os.Stderr, "[ddp] 进入面板运行环境 shell（PATH 已前置 venv，python3 即面板解释器；exit 退出）\n")
	if pythonBin := service.ResolveManagedPythonBinary(); pythonBin != "" {
		fmt.Fprintf(os.Stderr, "[ddp] Python: %s\n", pythonBin)
	}
	return runForeground(rt, cmd)
}

// resolveRunnableScript 优先按面板脚本目录解析（与任务执行一致），退化到用户给的绝对/当前目录路径。
func resolveRunnableScript(rt *cliRuntime, arg string) (string, error) {
	if full, _, err := resolveCLIScriptPath(rt.cfg.Data.ScriptsDir, arg, true); err == nil {
		return full, nil
	}
	if info, err := os.Stat(arg); err == nil && !info.IsDir() {
		abs, absErr := filepath.Abs(arg)
		if absErr != nil {
			return "", absErr
		}
		return abs, nil
	}
	return "", fmt.Errorf("脚本不存在: %s（路径相对脚本目录 %s，也可给绝对路径）", arg, rt.cfg.Data.ScriptsDir)
}

func resolveInteractiveShell() string {
	if runtime.GOOS == "windows" {
		if c := strings.TrimSpace(os.Getenv("COMSPEC")); c != "" {
			return c
		}
		return "cmd.exe"
	}
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		return s
	}
	for _, candidate := range []string{"/bin/bash", "/bin/sh"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "sh"
}

// runForeground 在前台运行交互命令，并尽量透传子进程的退出码。
func runForeground(rt *cliRuntime, cmd *exec.Cmd) error {
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		rt.printWarnings()
		os.Exit(exitErr.ExitCode())
	}
	return err
}

// mergeEnvMap 把 overrides 覆盖到 base（os.Environ() 形式）上，保留 base 中未被覆盖的项。
func mergeEnvMap(base []string, overrides map[string]string) []string {
	result := append([]string(nil), base...)
	index := make(map[string]int, len(result))
	for i, kv := range result {
		if eq := strings.IndexByte(kv, '='); eq > 0 {
			index[kv[:eq]] = i
		}
	}

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		entry := k + "=" + overrides[k]
		if i, ok := index[k]; ok {
			result[i] = entry
		} else {
			index[k] = len(result)
			result = append(result, entry)
		}
	}
	return result
}

// ensureEnvDefault 仅在 env 中不存在该键时追加默认值，不覆盖已有设置。
func ensureEnvDefault(env []string, key, value string) []string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return env
		}
	}
	return append(env, prefix+value)
}
