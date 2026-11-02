package handler

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/pkg/pathutil"
	"daidai-panel/service"
)

var allowedExtensions = map[string]bool{
	".py": true, ".js": true, ".mjs": true, ".sh": true, ".ts": true, ".json": true,
	".yaml": true, ".yml": true, ".txt": true, ".md": true, ".conf": true,
	".ini": true, ".env": true, ".toml": true, ".xml": true, ".csv": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".bmp": true, ".webp": true, ".log": true, ".htm": true,
	".html": true, ".css": true, ".sql": true, ".bat": true, ".cmd": true, ".ps1": true, ".go": true,
	".so": true,
}

var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".bmp": true, ".webp": true, ".so": true,
}

var invalidScriptPathCharsPattern = regexp.MustCompile(`[<>:"|?*\x00-\x1F]`)

const maxUploadSize = 100 * 1024 * 1024

type debugRun struct {
	Process  *os.Process
	Managed  service.SupervisedProcess
	Logs     []string
	Done     bool
	ExitCode *int
	Status   string
	// discardedLogs 是日志超过上限后被成块丢弃的行数。
	// Logs 被截断后切片下标就不再等于「第几行输出」，logOutputSince / logLen 靠它
	// 把 offset 解释成只增不减的全局序号（见 script_runtime.go 的 trimLogsLocked）。
	discardedLogs int
	finishedAt    time.Time // 结束时刻（#159 修复 C），与 Done 在同一处、同一把锁里写，还在跑时为零值；注册表据它删掉结束超过 30 分钟的记录
	mu            sync.Mutex
}

type ScriptHandler struct {
	debugRuns map[string]*debugRun
	order     []string // debugRuns 的插入顺序（#159 修复 C），淘汰时从最老的一头开始找，照搬 ConsoleHandler.order
	mu        sync.Mutex
}

func NewScriptHandler() *ScriptHandler {
	return &ScriptHandler{
		debugRuns: make(map[string]*debugRun),
	}
}

func scriptsDir() string {
	return config.C.Data.ScriptsDir
}

func normalizeScriptRelativePath(relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	normalized := strings.ReplaceAll(relPath, "\\", "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("不允许路径穿越")
	}

	rawSegments := strings.Split(normalized, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "." {
			continue
		}
		if segment == ".." {
			return "", fmt.Errorf("不允许路径穿越")
		}
		if invalidScriptPathCharsPattern.MatchString(segment) {
			return "", fmt.Errorf("路径包含非法字符")
		}
		segments = append(segments, segment)
	}

	if len(segments) == 0 {
		return "", fmt.Errorf("路径不能为空")
	}

	return path.Join(segments...), nil
}

func safePath(relPath string, mustExist bool) (string, error) {
	normalizedPath, err := normalizeScriptRelativePath(relPath)
	if err != nil {
		return "", err
	}

	// safePath 是脚本读写删改 13 个入口的唯一收口，
	// 在这里拒绝一次就等于同时堵住 content/download/save/delete/rename/move/copy/
	// batch-delete/mkdir/rollback/debug-run。树里隐藏而 API 可读等于藏起来但没锁上。
	if service.ShouldHideScriptTreeRelativePath(normalizedPath) {
		return "", fmt.Errorf("该路径不可访问")
	}

	full, err := pathutil.ResolveWithinBase(scriptsDir(), normalizedPath, false)
	if err != nil {
		return "", err
	}

	if mustExist {
		if _, err := os.Stat(full); os.IsNotExist(err) {
			return "", fmt.Errorf("文件不存在: %s", normalizedPath)
		}
	}
	return full, nil
}

func relPath(absPath string) string {
	absDir, _ := filepath.Abs(scriptsDir())
	rel, _ := filepath.Rel(absDir, absPath)
	return filepath.ToSlash(rel)
}
