package service

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"daidai-panel/config"
)

// quarantinedScriptDirNames 记录启动期需要自动隔离的异常脚本目录名。
// 这些目录不属于正常脚本文件树，一旦混入会污染脚本管理、备份恢复和统计结果。
var quarantinedScriptDirNames = map[string]bool{
	"%systemdrive%": true,
}

// ShouldIgnoreScriptEntryName 判断脚本目录中的某个顶级/子级名称是否应该在展示、统计、备份等场景忽略。
// 这里保留统一出口，避免 handler / service / backup 各写一套判断逻辑。
func ShouldIgnoreScriptEntryName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	switch strings.ToLower(name) {
	case "node_modules", "__pycache__":
		return true
	}

	return quarantinedScriptDirNames[strings.ToLower(name)]
}

// ShouldIgnoreScriptPath 判断某个脚本目录内的绝对路径是否命中异常隔离规则。
// 只要相对路径第一段命中黑名单，就认为这条路径不该继续参与脚本管理链路。
func ShouldIgnoreScriptPath(scriptsDir, targetPath string) bool {
	scriptsDir = strings.TrimSpace(scriptsDir)
	targetPath = strings.TrimSpace(targetPath)
	if scriptsDir == "" || targetPath == "" {
		return false
	}

	absScriptsDir, err := filepath.Abs(scriptsDir)
	if err != nil {
		return false
	}
	absTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return false
	}

	relPath, err := filepath.Rel(absScriptsDir, absTargetPath)
	if err != nil {
		return false
	}

	relPath = filepath.ToSlash(relPath)
	if relPath == "." || strings.HasPrefix(relPath, "../") || relPath == ".." {
		return false
	}

	firstSegment := relPath
	if slashIndex := strings.Index(firstSegment, "/"); slashIndex >= 0 {
		firstSegment = firstSegment[:slashIndex]
	}

	return ShouldIgnoreScriptEntryName(firstSegment)
}

// ShouldIgnoreScriptRelativePath 用于还原备份、导入脚本等“目标路径尚未落盘”的场景。
// 只检查相对路径第一段，避免把异常目录重新写回脚本根目录。
func ShouldIgnoreScriptRelativePath(relPath string) bool {
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	if relPath == "" || relPath == "." || relPath == "/" {
		return false
	}

	if strings.HasPrefix(relPath, "/") {
		relPath = strings.TrimPrefix(relPath, "/")
	}

	firstSegment := relPath
	if slashIndex := strings.Index(firstSegment, "/"); slashIndex >= 0 {
		firstSegment = firstSegment[:slashIndex]
	}

	return ShouldIgnoreScriptEntryName(firstSegment)
}

// QuarantineUnexpectedScriptEntriesOnStartup 会在启动时把脚本目录下的已知异常目录隔离到 quarantine 子目录。
// 这样能先把当前污染从用户视野和备份链路里移走，同时保留原始证据方便后续排查根因。
func QuarantineUnexpectedScriptEntriesOnStartup() {
	if config.C == nil {
		return
	}

	scriptsDir := strings.TrimSpace(config.C.Data.ScriptsDir)
	if scriptsDir == "" {
		return
	}

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		log.Printf("scan scripts dir failed: %v", err)
		return
	}

	for _, entry := range entries {
		if !ShouldIgnoreScriptEntryName(entry.Name()) {
			continue
		}

		sourcePath := filepath.Join(scriptsDir, entry.Name())
		quarantineRoot := filepath.Join(config.C.Data.Dir, "quarantine", "scripts")
		if err := os.MkdirAll(quarantineRoot, 0o755); err != nil {
			log.Printf("create script quarantine dir failed: %v", err)
			continue
		}

		targetPath := filepath.Join(quarantineRoot, entry.Name())
		targetPath = uniqueQuarantinePath(targetPath)

		if err := os.Rename(sourcePath, targetPath); err != nil {
			log.Printf("quarantine unexpected script entry failed: %s -> %s: %v", sourcePath, targetPath, err)
			continue
		}

		log.Printf("unexpected script entry quarantined: %s -> %s", sourcePath, targetPath)
	}
}

// uniqueQuarantinePath 避免隔离目录重名导致历史证据被覆盖。
func uniqueQuarantinePath(targetPath string) string {
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return targetPath
	}

	dir := filepath.Dir(targetPath)
	ext := filepath.Ext(targetPath)
	base := strings.TrimSuffix(filepath.Base(targetPath), ext)

	for index := 1; index < 1000; index++ {
		candidate := filepath.Join(dir, base+".duplicate-"+strconv.Itoa(index)+ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}

	return filepath.Join(dir, base+".duplicate-overflow"+ext)
}
