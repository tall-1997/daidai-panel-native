package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeShellScriptFileRewritesCRLF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\r\necho hi\r\n"), 0755); err != nil {
		t.Fatalf("write temp shell script: %v", err)
	}

	if err := NormalizeShellScriptFile(path); err != nil {
		t.Fatalf("normalize shell script: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read normalized shell script: %v", err)
	}
	if string(content) != "#!/bin/bash\necho hi\n" {
		t.Fatalf("unexpected normalized content: %q", string(content))
	}
}
