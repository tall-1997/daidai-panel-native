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

func TestSystemBackupDownloadSupportsQueryFilename(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "backup-download", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	filename := "daily_20260605_030000.tgz"
	expected := []byte{0x1f, 0x8b, 0x08, 0x00, 0x01, 0x02, 0x03}
	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatalf("create backup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, filename), expected, 0o644); err != nil {
		t.Fatalf("write backup file: %v", err)
	}

	rec := performRequest(
		engine,
		http.MethodGet,
		"/api/v1/system/backup/download?filename="+url.QueryEscape(filename),
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

func TestSystemBackupDownloadRejectsInvalidFilename(t *testing.T) {
	testutil.SetupTestEnv(t)

	engine := newProtectedRouter()
	user := testutil.MustCreateUser(t, "backup-download-invalid", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	rec := performRequest(
		engine,
		http.MethodGet,
		"/api/v1/system/backup/download?filename="+url.QueryEscape("../config.yaml"),
		map[string]string{"Authorization": "Bearer " + token},
	)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", rec.Code, rec.Body.String())
	}
}
