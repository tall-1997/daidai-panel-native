package handler

import (
	"encoding/base64"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

func (h *ScriptHandler) List(c *gin.Context) {
	dir := scriptsDir()
	var files []map[string]interface{}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldSkipScriptTreeDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if service.ShouldIgnoreScriptPath(dir, path) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if !allowedExtensions[ext] && ext != "" {
			return nil
		}
		rel := relPath(path)
		files = append(files, map[string]interface{}{
			"path":  rel,
			"name":  info.Name(),
			"size":  info.Size(),
			"mtime": float64(info.ModTime().Unix()),
		})
		return nil
	})

	if files == nil {
		files = []map[string]interface{}{}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i]["path"].(string) < files[j]["path"].(string)
	})

	response.Success(c, gin.H{"data": files, "total": len(files)})
}

func (h *ScriptHandler) Tree(c *gin.Context) {
	tree := buildTree(scriptsDir(), "")
	response.Success(c, gin.H{"data": tree})
}

func shouldSkipScriptTreeDir(name string) bool {
	return service.ShouldIgnoreScriptEntryName(name)
}

func buildTree(baseDir, prefix string) []map[string]interface{} {
	dir := filepath.Join(baseDir, prefix)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]interface{}{}
	}

	var dirs, files []map[string]interface{}

	sorted := make([]os.DirEntry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name()) < strings.ToLower(sorted[j].Name())
	})

	for _, entry := range sorted {
		name := entry.Name()

		rel := name
		if prefix != "" {
			rel = prefix + "/" + name
		}

		if entry.IsDir() {
			if shouldSkipScriptTreeDir(name) {
				continue
			}
			children := buildTree(baseDir, rel)
			dirs = append(dirs, map[string]interface{}{
				"key":      rel,
				"title":    name,
				"isLeaf":   false,
				"type":     "directory",
				"children": children,
			})
		} else {
			info, _ := entry.Info()
			size := int64(0)
			mtime := float64(0)
			if info != nil {
				size = info.Size()
				mtime = float64(info.ModTime().Unix())
			}
			files = append(files, map[string]interface{}{
				"key":       rel,
				"title":     name,
				"isLeaf":    true,
				"type":      "file",
				"extension": strings.ToLower(filepath.Ext(name)),
				"size":      size,
				"mtime":     mtime,
			})
		}
	}

	result := make([]map[string]interface{}, 0, len(dirs)+len(files))
	result = append(result, dirs...)
	result = append(result, files...)
	return result
}

func (h *ScriptHandler) GetContent(c *gin.Context) {
	path := c.Query("path")
	full, err := safePath(path, true)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if info, statErr := os.Stat(full); statErr == nil && info.IsDir() {
		response.BadRequest(c, "当前路径是目录，不能作为脚本文件打开")
		return
	}

	ext := strings.ToLower(filepath.Ext(full))
	if binaryExtensions[ext] {
		data, err := os.ReadFile(full)
		if err != nil {
			response.InternalError(c, "读取文件失败")
			return
		}
		mimeType := mime.TypeByExtension(ext)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		response.Success(c, gin.H{
			"data": gin.H{
				"path":      path,
				"content":   base64.StdEncoding.EncodeToString(data),
				"binary":    true,
				"is_binary": true,
				"mime":      mimeType,
			},
		})
		return
	}

	data, err := os.ReadFile(full)
	if err != nil {
		response.InternalError(c, "读取文件失败")
		return
	}

	response.Success(c, gin.H{
		"data": gin.H{
			"path":      path,
			"content":   string(data),
			"binary":    false,
			"is_binary": false,
		},
	})
}

func (h *ScriptHandler) Download(c *gin.Context) {
	path := c.Query("path")
	full, err := safePath(path, true)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.FileAttachment(full, filepath.Base(full))
}
