package handler_test

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestScriptDownloadSupportsBinarySOFiles(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "script-download", "operator")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	fileName := "libs/demo.so"
	filePath := filepath.Join(config.C.Data.ScriptsDir, filepath.FromSlash(fileName))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("create script dir: %v", err)
	}

	expected := []byte{0x7f, 0x45, 0x4c, 0x46, 0x01, 0x02, 0x03, 0x04}
	if err := os.WriteFile(filePath, expected, 0o644); err != nil {
		t.Fatalf("write .so file: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodGet,
		"/api/v1/scripts/download?path="+url.QueryEscape(fileName),
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	if got := rec.Header().Get("Content-Disposition"); got == "" {
		t.Fatal("expected Content-Disposition header for attachment")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Fatalf("expected download response to disable cache, got %q", got)
	}

	if !bytes.Equal(rec.Body.Bytes(), expected) {
		t.Fatalf("unexpected download body: %#v", rec.Body.Bytes())
	}
}
