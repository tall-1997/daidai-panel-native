package pathutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveWithinBase resolves target against baseDir and ensures the final path
// stays inside baseDir after accounting for absolute paths and symlinks.
func ResolveWithinBase(baseDir, target string, mustExist bool) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	target = strings.TrimSpace(target)
	if baseDir == "" {
		return "", fmt.Errorf("基础目录不能为空")
	}
	if target == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("基础目录无效: %w", err)
	}
	baseResolved := resolvePathFromExistingAncestor(baseAbs)

	candidate := target
	if !filepath.IsAbs(target) {
		candidate = filepath.Join(baseAbs, target)
	}

	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("无效路径: %w", err)
	}

	resolvedTarget, err := resolveTargetPath(candidateAbs, mustExist)
	if err != nil {
		return "", err
	}

	if !isWithinResolvedBase(baseResolved, resolvedTarget) {
		return "", fmt.Errorf("检测到路径穿越")
	}

	return resolvedTarget, nil
}

func IsWithinBase(baseDir, target string) bool {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}

	baseResolved := resolvePathFromExistingAncestor(baseAbs)
	targetResolved := resolvePathFromExistingAncestor(targetAbs)

	return isWithinResolvedBase(baseResolved, targetResolved)
}

func isWithinResolvedBase(baseResolved, targetResolved string) bool {
	baseResolved = filepath.Clean(baseResolved)
	targetResolved = filepath.Clean(targetResolved)
	if runtime.GOOS == "windows" {
		baseResolved = strings.ToLower(baseResolved)
		targetResolved = strings.ToLower(targetResolved)
	}

	rel, err := filepath.Rel(baseResolved, targetResolved)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveTargetPath(path string, mustExist bool) (string, error) {
	if mustExist {
		if _, err := os.Stat(path); err != nil {
			return "", err
		}
		return resolveExistingPath(path), nil
	}

	return resolvePathFromExistingAncestor(path), nil
}

func resolveExistingPath(path string) string {
	cleaned := filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err == nil {
		cleaned = resolved
	}
	abs, err := filepath.Abs(cleaned)
	if err == nil {
		return abs
	}
	return cleaned
}

func resolvePathFromExistingAncestor(path string) string {
	current := filepath.Clean(path)
	segments := make([]string, 0)

	for {
		if _, err := os.Stat(current); err == nil {
			resolved := resolveExistingPath(current)
			for i := len(segments) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, segments[i])
			}
			return resolved
		}

		parent := filepath.Dir(current)
		if parent == current {
			return resolveExistingPath(path)
		}

		segments = append(segments, filepath.Base(current))
		current = parent
	}
}
