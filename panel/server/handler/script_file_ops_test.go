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
	// git 订阅 clone 出来的 .git（含注入 PAT 的 remote URL）必须整棵消失，
	// 同时覆盖 submodule 场景下 .git 是一个文件的情况。
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "SmallWorld", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir nested git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "SmallWorld", ".git", "config"), []byte(gitConfigWithToken), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "SubModule"), 0o755); err != nil {
		t.Fatalf("mkdir submodule dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "SubModule", ".git"), []byte("gitdir: ../.git/modules/SubModule\n"), 0o644); err != nil {
		t.Fatalf("write submodule git file: %v", err)
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
		if strings.Contains(key, ".git") {
			t.Fatalf("expected script tree to skip .git, got key=%q all=%v", key, flatKeys)
		}
	}
}

// gitConfigWithToken 复刻订阅 Token 鉴权落盘后的真实内容：remote URL 里内嵌了 PAT。
const gitConfigWithToken = "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = https://x-access-token:ghp_regressionToken1234@github.com/demo/scripts.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

func TestScriptFileOpsRejectGitDirectoryAccess(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-git-guard", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)
	authHeader := map[string]string{"Authorization": "Bearer " + token}

	scriptsRoot := config.C.Data.ScriptsDir
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "SmallWorld", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir nested git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "SmallWorld", ".git", "config"), []byte(gitConfigWithToken), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "content", method: http.MethodGet, path: "/api/v1/scripts/content?path=SmallWorld/.git/config"},
		{name: "download", method: http.MethodGet, path: "/api/v1/scripts/download?path=SmallWorld/.git/config"},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/scripts?path=SmallWorld/.git&type=directory"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := performRequest(engine, tc.method, tc.path, authHeader)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "ghp_regressionToken1234") {
				t.Fatalf("response must not leak the PAT, body=%s", rec.Body.String())
			}
		})
	}

	// 拒绝之后文件必须仍在磁盘上——订阅还要靠它继续 fetch
	if _, err := os.Stat(filepath.Join(scriptsRoot, "SmallWorld", ".git", "config")); err != nil {
		t.Fatalf("expected .git/config to survive the rejected requests: %v", err)
	}
}

func TestScriptCopySkipsGitDirectory(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-copy-git", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	scriptsRoot := config.C.Data.ScriptsDir
	if err := os.MkdirAll(filepath.Join(scriptsRoot, "SmallWorld", ".git"), 0o755); err != nil {
		t.Fatalf("mkdir nested git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "SmallWorld", ".git", "config"), []byte(gitConfigWithToken), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsRoot, "SmallWorld", "demo.py"), []byte("print('ok')\n"), 0o644); err != nil {
		t.Fatalf("write demo script: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPost,
		"/api/v1/scripts/copy",
		`{"source_path":"SmallWorld","new_name":"SmallWorldCopy"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	if _, err := os.Stat(filepath.Join(scriptsRoot, "SmallWorldCopy", "demo.py")); err != nil {
		t.Fatalf("expected normal script to be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(scriptsRoot, "SmallWorldCopy", ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected copy to skip .git (hidden credential replica), stat err=%v", err)
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
