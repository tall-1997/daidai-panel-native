package handler_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestScriptGetContentRejectsDirectoryTarget(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-open-dir", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	dirPath := filepath.Join(config.C.Data.ScriptsDir, "folder")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodGet,
		"/api/v1/scripts/content?path=folder",
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "当前路径是目录") {
		t.Fatalf("expected directory target error, body=%s", rec.Body.String())
	}
}

func TestScriptTreeShowsDotFilesAndSkipsPycache(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-tree-dotfiles", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	scriptsRoot := config.C.Data.ScriptsDir
	if err := os.MkdirAll(filepath.Join(scriptsRoot, ".hidden-dir"), 0o755); err != nil {
		t.Fatalf("mkdir hidden dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, ".hidden-dir", ".env"), []byte("TOKEN=demo\n"), 0o644); err != nil {
		t.Fatalf("write hidden env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, ".hidden-file"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "__pycache__"), 0o755); err != nil {
		t.Fatalf("mkdir pycache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "__pycache__", "notify.cpython-312.pyc"), []byte{0x00, 0x01}, 0o644); err != nil {
		t.Fatalf("write pycache file: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodGet,
		"/api/v1/scripts/tree",
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode tree payload: %v", err)
	}
	flatKeys := collectScriptTreeKeys(payload.Data)
	for _, key := range []string{".hidden-dir", ".hidden-dir/.env", ".hidden-file"} {
		if !flatKeys[key] {
			t.Fatalf("expected script tree to include %q, got keys=%v", key, flatKeys)
		}
	}
	for key := range flatKeys {
		if strings.Contains(key, "__pycache__") {
			t.Fatalf("expected script tree to skip __pycache__, got key=%q all=%v", key, flatKeys)
		}
	}
}

func collectScriptTreeKeys(nodes []map[string]interface{}) map[string]bool {
	keys := make(map[string]bool)
	var walk func([]map[string]interface{})
	walk = func(items []map[string]interface{}) {
		for _, item := range items {
			if key, ok := item["key"].(string); ok {
				keys[key] = true
			}
			children, ok := item["children"].([]interface{})
			if !ok {
				continue
			}
			childMaps := make([]map[string]interface{}, 0, len(children))
			for _, child := range children {
				if childMap, ok := child.(map[string]interface{}); ok {
					childMaps = append(childMaps, childMap)
				}
			}
			walk(childMaps)
		}
	}
	walk(nodes)
	return keys
}
