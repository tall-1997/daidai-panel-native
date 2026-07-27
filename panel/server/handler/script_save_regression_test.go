package handler_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestScriptSaveRejectsDirectoryTarget(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-save-dir", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	dirPath := filepath.Join(config.C.Data.ScriptsDir, "folder")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir target dir: %v", err)
	}

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/scripts/content",
		`{"path":"folder","content":"print('hello')\n"}`,
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "当前路径是目录") {
		t.Fatalf("expected directory target error, body=%s", rec.Body.String())
	}
}

func TestScriptSaveWritesNormalizedShellContent(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-save-shell", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performJSONRequest(
		engine,
		http.MethodPut,
		"/api/v1/scripts/content",
		"{\"path\":\"demo.sh\",\"content\":\"echo hi\\r\\necho again\\r\\n\"}",
		map[string]string{"Authorization": "Bearer " + token},
		"",
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	content, err := os.ReadFile(filepath.Join(config.C.Data.ScriptsDir, "demo.sh"))
	if err != nil {
		t.Fatalf("read saved shell file: %v", err)
	}
	if got := string(content); got != "echo hi\necho again\n" {
		t.Fatalf("expected normalized shell content, got %q", got)
	}
}
