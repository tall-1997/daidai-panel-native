package handler

import (
	"encoding/json"
	"os/exec"
	"strings"

	"daidai-panel/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *ScriptHandler) Format(c *gin.Context) {
	var req struct {
		Content   string `json:"content" binding:"required"`
		Language  string `json:"language" binding:"required"`
		Formatter string `json:"formatter"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var formatted string
	var usedFormatter string

	switch req.Language {
	case "python":
		formatted, usedFormatter = formatPython(req.Content)
	case "shell":
		formatted, usedFormatter = formatShell(req.Content)
	case "go":
		formatted, usedFormatter = formatGo(req.Content)
	case "json":
		formatted, usedFormatter = formatJSON(req.Content)
	default:
		response.BadRequest(c, "不支持的语言")
		return
	}

	if formatted == "" {
		formatted = req.Content
	}

	response.Success(c, gin.H{
		"data": gin.H{
			"content":   formatted,
			"language":  req.Language,
			"formatter": usedFormatter,
		},
	})
}

func formatPython(content string) (string, string) {
	if _, err := exec.LookPath("black"); err == nil {
		cmd := exec.Command("black", "--line-length", "88", "--quiet", "-")
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.Output()
		if err == nil {
			return string(out), "black"
		}
	}
	if _, err := exec.LookPath("autopep8"); err == nil {
		cmd := exec.Command("autopep8", "--max-line-length", "88", "-a", "-")
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.Output()
		if err == nil {
			return string(out), "autopep8"
		}
	}
	return content, "none"
}

func formatShell(content string) (string, string) {
	if _, err := exec.LookPath("shfmt"); err == nil {
		cmd := exec.Command("shfmt", "-i", "2", "-bn", "-ci", "-sr")
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.Output()
		if err == nil {
			return string(out), "shfmt"
		}
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n"), "basic"
}

func formatGo(content string) (string, string) {
	if _, err := exec.LookPath("gofmt"); err == nil {
		cmd := exec.Command("gofmt")
		cmd.Stdin = strings.NewReader(content)
		out, err := cmd.Output()
		if err == nil {
			return string(out), "gofmt"
		}
	}
	return content, "none"
}

func formatJSON(content string) (string, string) {
	var obj interface{}
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return content, "none"
	}
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return content, "none"
	}
	return string(out), "json"
}
