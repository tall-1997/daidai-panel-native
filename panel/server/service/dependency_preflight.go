package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"daidai-panel/model"
)

const maxStaticDependencySourceBytes = 10 * 1024 * 1024

var (
	pythonImportStatementRe = regexp.MustCompile(`(?m)^\s*import\s+([^#\r\n]+)`)
	pythonFromStatementRe   = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z_][A-Za-z0-9_.]*)\s+import\b`)
	nodeRequireStatementRe  = regexp.MustCompile(`\brequire\s*\(\s*["']([^"']+)["']\s*\)`)
	nodeImportStatementRe   = regexp.MustCompile(`\bimport\s*(?:[^"'\r\n]+?\s+from\s*)?["']([^"']+)["']`)
	nodeDynamicImportRe     = regexp.MustCompile(`\bimport\s*\(\s*["']([^"']+)["']\s*\)`)
)

var nodeBuiltinModules = map[string]bool{
	"assert": true, "assert/strict": true, "async_hooks": true, "buffer": true,
	"child_process": true, "cluster": true, "console": true, "constants": true,
	"crypto": true, "dgram": true, "diagnostics_channel": true, "dns": true,
	"domain": true, "events": true, "fs": true, "fs/promises": true, "http": true,
	"http2": true, "https": true, "module": true, "net": true, "os": true,
	"path": true, "path/posix": true, "path/win32": true, "perf_hooks": true,
	"process": true, "punycode": true, "querystring": true, "readline": true,
	"repl": true, "stream": true, "stream/promises": true, "stream/web": true,
	"string_decoder": true, "sys": true, "timers": true, "timers/promises": true,
	"tls": true, "trace_events": true, "tty": true, "url": true, "util": true,
	"util/types": true, "v8": true, "vm": true, "wasi": true, "worker_threads": true,
	"zlib": true,
}

// DetectStaticScriptDependencies 从脚本源代码中提取可安全识别的 Python/Node 顶层依赖。
// 它只返回第三方包，不把标准库、本地模块或 Node 内置模块交给包管理器。
func DetectStaticScriptDependencies(ext, source, workDir string) []*AutoInstallCandidate {
	ext = strings.ToLower(strings.TrimSpace(ext))
	seen := make(map[string]bool)
	result := make([]*AutoInstallCandidate, 0)
	add := func(candidate *AutoInstallCandidate) {
		if candidate == nil {
			return
		}
		key := candidate.Manager + "\x00" + strings.ToLower(candidate.PackageName)
		if seen[key] {
			return
		}
		seen[key] = true
		result = append(result, candidate)
	}

	switch ext {
	case ".py":
		for _, matches := range pythonFromStatementRe.FindAllStringSubmatch(source, -1) {
			if len(matches) > 1 {
				add(staticPythonCandidate(strings.Split(matches[1], ".")[0], workDir))
			}
		}
		for _, matches := range pythonImportStatementRe.FindAllStringSubmatch(source, -1) {
			if len(matches) < 2 {
				continue
			}
			for _, item := range strings.Split(matches[1], ",") {
				fields := strings.Fields(strings.TrimSpace(item))
				if len(fields) == 0 {
					continue
				}
				name := strings.TrimSpace(fields[0])
				add(staticPythonCandidate(strings.Split(name, ".")[0], workDir))
			}
		}
	case ".js", ".mjs", ".ts":
		patterns := []*regexp.Regexp{nodeRequireStatementRe, nodeImportStatementRe, nodeDynamicImportRe}
		for _, pattern := range patterns {
			for _, matches := range pattern.FindAllStringSubmatch(source, -1) {
				if len(matches) > 1 {
					add(staticNodeCandidate(matches[1], workDir))
				}
			}
		}
	}
	return result
}

func staticPythonCandidate(module, workDir string) *AutoInstallCandidate {
	module = strings.TrimSpace(module)
	if module == "" || isPythonStdlib(module) || thirdPartyExcludedModules[module] || isLocalPythonModule(module, workDir) {
		return nil
	}
	packageName := ResolvePythonAutoInstallPackage(module)
	return &AutoInstallCandidate{
		Manager:       "python",
		RequestedName: module,
		PackageName:   packageName,
		DisplayName:   formatAutoInstallDisplayName(module, packageName),
		WorkDir:       workDir,
		RecordType:    model.DepTypePython,
		RecordName:    packageName,
	}
}

func staticNodeCandidate(spec, workDir string) *AutoInstallCandidate {
	spec = strings.TrimSpace(spec)
	packageName := normalizeNodeRequireSpecifier(spec)
	if packageName == "" || nodeBuiltinModules[strings.TrimPrefix(packageName, "node:")] || isLocalNodeModule(packageName, workDir) {
		return nil
	}
	return &AutoInstallCandidate{
		Manager:       "nodejs",
		RequestedName: packageName,
		PackageName:   packageName,
		DisplayName:   packageName,
		WorkDir:       workDir,
		RecordType:    model.DepTypeNodeJS,
		RecordName:    packageName,
	}
}

func isLocalNodeModule(packageName, workDir string) bool {
	if workDir == "" || packageName == "" {
		return false
	}
	path := filepath.Join(workDir, "node_modules", filepath.FromSlash(packageName))
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// PreflightScriptDependencies 在启动用户脚本前检查并安装全部静态依赖。
// 安装成功会写入现有 dependencies 表并落在对应托管运行时目录，后续脚本直接复用。
func PreflightScriptDependencies(ext, scriptPath string, envVars map[string]string, onOutput func(string)) error {
	if !model.GetRegisteredConfigBool("auto_install_deps") {
		return nil
	}
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext != ".py" && ext != ".js" && ext != ".mjs" && ext != ".ts" {
		return nil
	}
	if info, err := os.Stat(scriptPath); err != nil || !info.Mode().IsRegular() {
		// 某些托管命令/测试计划没有对应源码文件；静态预检无输入时跳过，
		// 由后续实际执行路径报告真正的启动错误。
		return nil
	}
	source, err := readStaticDependencySource(scriptPath)
	if err != nil {
		return fmt.Errorf("读取脚本依赖失败: %w", err)
	}
	candidates := DetectStaticScriptDependencies(filepath.Ext(scriptPath), source, filepath.Dir(scriptPath))
	if len(candidates) == 0 {
		return nil
	}
	for _, candidate := range candidates {
		if onOutput != nil {
			onOutput(fmt.Sprintf("[依赖预检] %s", candidate.DisplayName))
		}
		if DependencyInstalledForPythonVersion(candidate.RecordType, candidate.PackageName, ResolvePythonVersionFromEnv(envVars)) {
			if onOutput != nil {
				onOutput(fmt.Sprintf("[依赖已存在，跳过安装] %s", candidate.DisplayName))
			}
			continue
		}
		if onOutput != nil {
			onOutput(fmt.Sprintf("[检测到缺失依赖: %s，正在安装...]", candidate.DisplayName))
		}
		result := InstallAutoDependency(candidate, envVars)
		if !result.Success {
			reason := strings.TrimSpace(result.Error)
			if reason == "" {
				reason = "未知安装错误"
			}
			if onOutput != nil {
				onOutput(fmt.Sprintf("[依赖安装失败: %s]", reason))
			}
			return fmt.Errorf("依赖 %s 安装失败: %s", candidate.DisplayName, reason)
		}
		if onOutput != nil {
			onOutput(fmt.Sprintf("[依赖安装成功并已持久化: %s]", candidate.DisplayName))
		}
	}
	return nil
}

func readStaticDependencySource(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	limited := io.LimitReader(file, maxStaticDependencySourceBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}
	if len(data) > maxStaticDependencySourceBytes {
		return "", fmt.Errorf("脚本超过依赖预检大小限制 %d 字节", maxStaticDependencySourceBytes)
	}
	return string(data), nil
}
