package handler

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"daidai-panel/config"
	"daidai-panel/pkg/pathutil"
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
	Logs     []string
	Done     bool
	ExitCode *int
	Status   string
	mu       sync.Mutex
}

type ScriptHandler struct {
	debugRuns map[string]*debugRun
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
