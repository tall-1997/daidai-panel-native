package main

import (
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var cliAllowedScriptExtensions = map[string]bool{
	".py": true, ".js": true, ".mjs": true, ".sh": true, ".ts": true, ".json": true,
	".yaml": true, ".yml": true, ".txt": true, ".md": true, ".conf": true,
	".ini": true, ".env": true, ".toml": true, ".xml": true, ".csv": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true,
	".ico": true, ".bmp": true, ".webp": true, ".log": true, ".htm": true,
	".html": true, ".css": true, ".sql": true, ".bat": true, ".cmd": true, ".ps1": true, ".go": true,
	".so": true,
}

var cliBinaryScriptExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".ico": true, ".bmp": true, ".webp": true, ".so": true,
}

var invalidCLIPathCharsPattern = regexp.MustCompile(`[<>:"\\|?*\x00-\x1F]`)

func runScript(rt *cliRuntime, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp script <list|cat|fetch> ...")
	}

	switch args[0] {
	case "list":
		return runScriptList(rt)
	case "cat":
		return runScriptCat(rt, args[1:])
	case "fetch":
		return runScriptFetch(rt, args[1:])
	default:
		return fmt.Errorf("未知 script 子命令: %s", args[0])
	}
}

func runScriptList(rt *cliRuntime) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}

	var files []string
	err := filepath.Walk(rt.cfg.Data.ScriptsDir, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(rt.cfg.Data.ScriptsDir, current)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext != "" && !cliAllowedScriptExtensions[ext] {
			return nil
		}

		files = append(files, rel)
		return nil
	})
	if err != nil {
		return err
	}

	sort.Strings(files)
	if len(files) == 0 {
		fmt.Println("脚本目录当前没有文件")
		return nil
	}

	for _, file := range files {
		fmt.Println(file)
	}
	return nil
}

func runScriptCat(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) != 1 {
		return fmt.Errorf("用法: ddp script cat <相对路径>")
	}

	full, rel, err := resolveCLIScriptPath(rt.cfg.Data.ScriptsDir, args[0], true)
	if err != nil {
		return err
	}

	ext := strings.ToLower(filepath.Ext(full))
	if cliBinaryScriptExtensions[ext] {
		return fmt.Errorf("脚本 %s 是二进制文件，不能直接输出", rel)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return err
	}

	fmt.Print(string(data))
	return nil
}

func runScriptFetch(rt *cliRuntime, args []string) error {
	if err := rt.bootstrap(); err != nil {
		return err
	}
	if len(args) == 0 {
		return fmt.Errorf("用法: ddp script fetch <url> [--path 相对路径] [--force]")
	}

	rawURL := strings.TrimSpace(args[0])
	targetPath := ""
	force := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path 需要参数")
			}
			targetPath = args[i+1]
			i++
		case "--force":
			force = true
		default:
			return fmt.Errorf("未知参数: %s", args[i])
		}
	}

	parsed, err := neturl.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("无效的下载地址")
	}

	if targetPath == "" {
		targetPath = path.Base(parsed.Path)
		if strings.TrimSpace(targetPath) == "" || targetPath == "." || targetPath == "/" {
			return fmt.Errorf("无法从 URL 推断文件名，请使用 --path 指定保存路径")
		}
	}

	full, rel, err := resolveCLIScriptPath(rt.cfg.Data.ScriptsDir, targetPath, false)
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(full); err == nil {
			return fmt.Errorf("目标文件已存在，请加 --force 覆盖: %s", rel)
		}
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("下载失败，HTTP 状态码 %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return err
	}

	fmt.Printf("已保存脚本: %s\n", rel)
	return nil
}

func resolveCLIScriptPath(baseDir, relativePath string, mustExist bool) (string, string, error) {
	normalized, err := normalizeCLIScriptRelativePath(relativePath)
	if err != nil {
		return "", "", err
	}

	full := filepath.Join(baseDir, filepath.FromSlash(normalized))
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", "", err
	}

	basePrefix := baseAbs
	if !strings.HasSuffix(basePrefix, string(os.PathSeparator)) {
		basePrefix += string(os.PathSeparator)
	}
	if fullAbs != baseAbs && !strings.HasPrefix(fullAbs, basePrefix) {
		return "", "", fmt.Errorf("不允许路径穿越")
	}

	ext := strings.ToLower(filepath.Ext(fullAbs))
	if ext != "" && !cliAllowedScriptExtensions[ext] {
		return "", "", fmt.Errorf("不支持的文件类型: %s", ext)
	}

	if mustExist {
		if _, err := os.Stat(fullAbs); err != nil {
			return "", "", fmt.Errorf("文件不存在: %s", normalized)
		}
	}

	return fullAbs, normalized, nil
}

func normalizeCLIScriptRelativePath(relativePath string) (string, error) {
	trimmed := strings.TrimSpace(relativePath)
	if trimmed == "" {
		return "", fmt.Errorf("路径不能为空")
	}

	normalized := strings.ReplaceAll(trimmed, "\\", "/")
	normalized = strings.TrimPrefix(path.Clean("/"+normalized), "/")
	if normalized == "" || normalized == "." {
		return "", fmt.Errorf("路径不能为空")
	}

	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("不允许路径穿越")
		}
		if invalidCLIPathCharsPattern.MatchString(segment) {
			return "", fmt.Errorf("路径包含非法字符")
		}
	}

	return normalized, nil
}
