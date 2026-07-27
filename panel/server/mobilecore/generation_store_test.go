package mobilecore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerationStoreImportsFlatDataOnce(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "daidai.db"), "legacy-db")
	writeTestFile(t, filepath.Join(root, "scripts", "task.sh"), "echo ok")
	writeTestFile(t, filepath.Join(root, "config.sh"), "TOKEN=secret")

	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(active) != filepath.Join(root, generationsDirName) {
		t.Fatalf("active generation = %q", active)
	}
	for _, relative := range []string{"daidai.db", filepath.Join("scripts", "task.sh"), "config.sh"} {
		data, err := os.ReadFile(filepath.Join(active, relative))
		if err != nil || len(data) == 0 {
			t.Fatalf("imported %s: data=%q err=%v", relative, data, err)
		}
	}

	second, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if second != active {
		t.Fatalf("flat data imported twice: first=%q second=%q", active, second)
	}
}

func TestPrepareFailureNeverChangesActiveGeneration(t *testing.T) {
	for _, failure := range []string{"copy-before-write", "copy-after-write", "file-fsync", "directory-fsync"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			store := newGenerationStore(root, defaultFilesystemOps())
			active, err := store.converge()
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(active, "config.sh"), "stable")
			store.ops.boundary = failBoundary(failure)

			if _, err := store.prepareMigration(); err == nil {
				t.Fatalf("expected %s failure", failure)
			}
			got, err := store.activeGeneration()
			if err != nil {
				t.Fatal(err)
			}
			if got != active {
				t.Fatalf("active changed after %s: got=%q want=%q", failure, got, active)
			}
		})
	}
}

func TestMigrationFailureRollsBackPointer(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "old")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(store.generationPath(txn.NewGeneration), "daidai.db"), "broken")
	if err := store.rollback(txn); err != nil {
		t.Fatal(err)
	}
	active, err := store.activeGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if active != oldGeneration {
		t.Fatalf("active=%q want old=%q", active, oldGeneration)
	}
	assertTransactionPhase(t, root, recoveryPhaseRolledBack)
}

func TestConvergeAfterPointerCommitCrashKeepsCompleteGeneration(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "complete")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}

	restarted := newGenerationStore(root, defaultFilesystemOps())
	active, err := restarted.converge()
	if err != nil {
		t.Fatal(err)
	}
	if active != restarted.generationPath(txn.NewGeneration) {
		t.Fatalf("active=%q want committed=%q", active, restarted.generationPath(txn.NewGeneration))
	}
	assertTransactionPhase(t, root, recoveryPhaseVerified)
}

func TestConvergeAfterPointerRenameBeforePhaseWriteKeepsNewGeneration(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "complete")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.writePointer(txn.NewGeneration); err != nil {
		t.Fatal(err)
	}

	restarted := newGenerationStore(root, defaultFilesystemOps())
	active, err := restarted.converge()
	if err != nil {
		t.Fatal(err)
	}
	if active != restarted.generationPath(txn.NewGeneration) {
		t.Fatalf("active=%q want renamed generation=%q", active, restarted.generationPath(txn.NewGeneration))
	}
	assertTransactionPhase(t, root, recoveryPhaseVerified)
}

func TestPointerRenameFailureLeavesOldGenerationActive(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "complete")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	store.ops.boundary = failBoundary("pointer-rename")
	if err := store.commitPointer(txn); err == nil {
		t.Fatal("expected pointer rename failure")
	}
	active, err := store.activeGeneration()
	if err != nil {
		t.Fatal(err)
	}
	if active != oldGeneration {
		t.Fatalf("active=%q want=%q", active, oldGeneration)
	}
}

func TestSealGenerationAcceptsMigrationChangesAndDetectsLaterCorruption(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "daidai.db"), "before")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	candidate := store.generationPath(txn.NewGeneration)
	writeTestFile(t, filepath.Join(candidate, "daidai.db"), "after migration")
	if err := store.sealGeneration(txn.NewGeneration); err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(candidate, "daidai.db"), "corrupt")
	if err := store.verifyGeneration(txn.NewGeneration); err == nil {
		t.Fatal("expected post-seal corruption detection")
	}
}

func TestConvergeRejectsInvalidRecoveryTransactionMetadata(t *testing.T) {
	for _, transaction := range []string{
		`{"version":1,"phase":"unknown","newGeneration":"valid"}`,
		`{"version":1,"phase":"building","newGeneration":"../escape"}`,
		`{"version":2,"phase":"building","newGeneration":"valid"}`,
	} {
		t.Run(transaction, func(t *testing.T) {
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, recoveryTransactionName), transaction)
			store := newGenerationStore(root, defaultFilesystemOps())
			if _, err := store.converge(); err == nil {
				t.Fatal("expected invalid transaction rejection")
			}
		})
	}
}

func TestVerifyGenerationRejectsManifestPathEscape(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	manifest := generationManifest{Version: 1, Files: map[string]string{"../outside": "digest"}}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, generationManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyGeneration(filepath.Base(active)); err == nil {
		t.Fatal("expected manifest path escape rejection")
	}
}

func failBoundary(want string) func(string) error {
	return func(got string) error {
		if got == want {
			return errors.New("injected filesystem failure")
		}
		return nil
	}
}

func writeTestFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTransactionPhase(t *testing.T, root, phase string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, recoveryTransactionName))
	if err != nil {
		t.Fatal(err)
	}
	var txn recoveryTransaction
	if err := json.Unmarshal(data, &txn); err != nil {
		t.Fatal(err)
	}
	if txn.Phase != phase {
		t.Fatalf("phase=%q want=%q", txn.Phase, phase)
	}
}
