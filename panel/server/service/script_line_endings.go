package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

func NormalizeShellLineEndings(content []byte) []byte {
	if !bytes.ContainsRune(content, '\r') {
		return content
	}

	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	normalized = bytes.ReplaceAll(normalized, []byte("\r"), []byte("\n"))
	return normalized
}

func NormalizeShellScriptFile(fullPath string) error {
	if strings.ToLower(filepath.Ext(fullPath)) != ".sh" {
		return nil
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}

	normalized := NormalizeShellLineEndings(content)
	if bytes.Equal(content, normalized) {
		return nil
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}

	return os.WriteFile(fullPath, normalized, info.Mode())
}
