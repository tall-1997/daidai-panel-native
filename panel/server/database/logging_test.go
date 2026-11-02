package database

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/config"
)

func TestInitLogDoesNotExposeDatabasePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "panel.db")
	var output bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		_ = Close()
	})

	Init(&config.DatabaseConfig{Path: path})
	if strings.Contains(output.String(), path) || strings.Contains(output.String(), filepath.Dir(path)) {
		t.Fatalf("database path leaked in log: %q", output.String())
	}
}
