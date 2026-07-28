package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeRuntimeSecurityPersistsKeyAndTrustRecords(t *testing.T) {
	dataDir := t.TempDir()
	if err := InitializeRuntimeSecurity(dataDir); err != nil {
		t.Fatalf("initialize runtime security: %v", err)
	}

	secretStatus := RuntimeSecretStoreInstance().Status()
	if !secretStatus.Ready {
		t.Fatalf("secret store status=%+v", secretStatus)
	}
	sealed, err := RuntimeSecretStoreInstance().Seal(context.Background(), "runtime", []byte("secret-value"))
	if err != nil {
		t.Fatalf("seal runtime secret: %v", err)
	}
	if string(sealed.Cipher) == "secret-value" {
		t.Fatal("expected encrypted payload")
	}

	masterPath := filepath.Join(dataDir, ".runtime_master_key")
	info, err := os.Stat(masterPath)
	if err != nil {
		t.Fatalf("stat runtime master key: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("master key mode=%#o want=%#o", info.Mode().Perm(), os.FileMode(0o600))
	}

	SeedRuntimeTrustRecord("subscription", "v1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", []string{"python"})
	status := RuntimeTrustAuthorizer().Status()
	if !status.Ready {
		t.Fatalf("trust status=%+v", status)
	}
	if status.RecordCount == 0 {
		t.Fatalf("trust records should persist, status=%+v", status)
	}

	trustPath := filepath.Join(dataDir, ".runtime_trust_authorizations.json")
	trustInfo, err := os.Stat(trustPath)
	if err != nil {
		t.Fatalf("stat trust authorization file: %v", err)
	}
	if trustInfo.Mode().Perm() != 0o600 {
		t.Fatalf("trust file mode=%#o want=%#o", trustInfo.Mode().Perm(), os.FileMode(0o600))
	}

	if err := InitializeRuntimeSecurity(dataDir); err != nil {
		t.Fatalf("reinitialize runtime security: %v", err)
	}
	records := RuntimeTrustAuthorizer().List()
	if len(records) == 0 {
		t.Fatal("expected trust records to reload from disk")
	}
}
