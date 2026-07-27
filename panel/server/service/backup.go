package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
)

type ScriptFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type BackupData struct {
	Version   string                `json:"version"`
	CreatedAt time.Time             `json:"created_at"`
	Tasks     []model.Task          `json:"tasks"`
	EnvVars   []model.EnvVar        `json:"env_vars"`
	Subs      []model.Subscription  `json:"subscriptions"`
	Channels  []model.NotifyChannel `json:"notify_channels"`
	SSHKeys   []model.SSHKey        `json:"ssh_keys"`
	Configs   []model.SystemConfig  `json:"system_configs"`
	Scripts   []ScriptFile          `json:"scripts,omitempty"`
	Deps      []model.Dependency    `json:"dependencies,omitempty"`
}

func collectScripts(scriptsDir string) []ScriptFile {
	var files []ScriptFile
	allowedExts := map[string]bool{".js": true, ".mjs": true, ".py": true, ".ts": true, ".sh": true}

	filepath.Walk(scriptsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			if ShouldIgnoreScriptPath(scriptsDir, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldIgnoreScriptPath(scriptsDir, path) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if !allowedExts[ext] {
			return nil
		}
		if info.Size() > 10*1024*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(scriptsDir, path)
		rel = filepath.ToSlash(rel)
		files = append(files, ScriptFile{
			Path:    rel,
			Content: base64.StdEncoding.EncodeToString(data),
		})
		return nil
	})
	return files
}

func restoreScripts(scriptsDir string, scripts []ScriptFile) {
	for _, sf := range scripts {
		if strings.Contains(sf.Path, "..") {
			continue
		}
		if ShouldIgnoreScriptRelativePath(sf.Path) {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(sf.Content)
		if err != nil {
			continue
		}
		fullPath := filepath.Join(scriptsDir, filepath.FromSlash(sf.Path))
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, data, 0755)
	}
}

func CreateBackup(options BackupCreateOptions) (string, error) {
	return createBackupArchive(options)
}

func encryptData(data []byte, password string) ([]byte, error) {
	key := sha256.Sum256([]byte(password))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, data, nil), nil
}

func decryptData(data []byte, password string) ([]byte, error) {
	key := sha256.Sum256([]byte(password))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("密文数据过短")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func RestoreBackup(filename, password string) error {
	return restoreBackupFile(filename, password)
}

func ListBackups() ([]map[string]interface{}, error) {
	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return []map[string]interface{}{}, nil
	}

	var backups []map[string]interface{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, map[string]interface{}{
			"name":       entry.Name(),
			"size":       info.Size(),
			"created_at": info.ModTime(),
		})
	}

	return backups, nil
}

func DeleteBackup(filename string) error {
	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	filePath := filepath.Join(backupDir, filepath.Base(filename))
	return os.Remove(filePath)
}
