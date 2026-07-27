package mobilecore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
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
			if err := store.sealGeneration(filepath.Base(active), generationBaseline{}); err != nil {
				t.Fatal(err)
			}
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

func TestConvergeAfterPointerCommitCrashRollsBackUnreadyGeneration(t *testing.T) {
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
	if active != oldGeneration {
		t.Fatalf("active=%q want previous=%q", active, oldGeneration)
	}
	assertTransactionPhase(t, root, recoveryPhaseRolledBack)
}

func TestConvergeAfterPointerRenameBeforePhaseWriteRollsBackUnreadyGeneration(t *testing.T) {
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
	if active != oldGeneration {
		t.Fatalf("active=%q want previous=%q", active, oldGeneration)
	}
	assertTransactionPhase(t, root, recoveryPhaseRolledBack)
}

func TestFirstGenerationCommittedCrashResumesWithoutMarkingVerified(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, ".jwt_secret"), "created-before-crash")

	restarted := newGenerationStore(root, defaultFilesystemOps())
	resumed, err := restarted.converge()
	if err != nil {
		t.Fatalf("resume first generation: %v", err)
	}
	if resumed != active {
		t.Fatalf("resumed=%q want=%q", resumed, active)
	}
	assertTransactionPhase(t, root, recoveryPhaseCommitted)
}

func TestFinalizeGenerationIsOnlyTransitionToVerified(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	assertTransactionPhase(t, root, recoveryPhaseCommitted)
	if err := store.finalize(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
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
	if err := store.sealGeneration(txn.NewGeneration, generationBaseline{}); err != nil {
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
	manifest := generationManifest{Version: 1, Files: map[string]manifestFile{"../outside": {SHA256: "digest"}}}
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

func TestVerifyGenerationRejectsManifestedSymlink(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "payload"), "safe")
	if err := store.sealGeneration(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(active, "payload")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "outside"), "safe")
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(active, "payload")); err != nil {
		t.Fatal(err)
	}
	if err := store.verifyGeneration(filepath.Base(active)); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestSealGenerationWritesManifestAtomically(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(active, generationManifestName))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "new-file"), "new")
	store.ops.boundary = failBoundary("manifest-rename")
	if err := store.sealGeneration(filepath.Base(active), generationBaseline{}); err == nil {
		t.Fatal("expected manifest rename failure")
	}
	after, err := os.ReadFile(filepath.Join(active, generationManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("manifest was partially replaced")
	}
}

func TestValidateGenerationChecksHashSizeMissingAndExtraFiles(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, active string)
	}{
		{
			name: "hash tamper",
			mutate: func(t *testing.T, active string) {
				writeTestFile(t, filepath.Join(active, "daidai.db"), "tampered")
			},
		},
		{
			name: "missing file",
			mutate: func(t *testing.T, active string) {
				if err := os.Remove(filepath.Join(active, "daidai.db")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "extra critical file",
			mutate: func(t *testing.T, active string) {
				writeTestFile(t, filepath.Join(active, "config.sh"), "unexpected")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			store := newGenerationStore(root, defaultFilesystemOps())
			active, err := store.converge()
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(active, "daidai.db"), "sealed")
			if err := store.sealGeneration(filepath.Base(active), generationBaseline{Schema: "1", Runtime: "1"}); err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, active)
			if err := store.validateGeneration(filepath.Base(active)); err == nil {
				t.Fatal("expected strict generation validation failure")
			}
		})
	}
}

func TestValidateGenerationAllowsSQLiteTransientSidecars(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "daidai.db"), "sealed")
	if err := store.sealGeneration(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "daidai.db-wal"), "transient")
	writeTestFile(t, filepath.Join(active, "daidai.db-shm"), "transient")

	if err := store.validateGeneration(filepath.Base(active)); err != nil {
		t.Fatalf("validate generation with SQLite sidecars: %v", err)
	}
}

func TestValidateGenerationRequiresDatabaseForSchemaBaseline(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(active, "daidai.db"), "sealed")
	if err := store.sealGeneration(filepath.Base(active), generationBaseline{Schema: "schema-1"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(active, "daidai.db")); err != nil {
		t.Fatal(err)
	}
	manifest := generationManifest{
		Version:  1,
		Baseline: generationBaseline{Schema: "schema-1"},
		Files:    map[string]manifestFile{},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, generationManifestName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.validateGeneration(filepath.Base(active)); err == nil {
		t.Fatal("expected missing required database failure")
	}
}

func TestPrepareMigrationPreflightAndCopyENOSPCCleanupCandidateAndTransaction(t *testing.T) {
	for _, failure := range []string{"preflight", "copy"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			ops := defaultFilesystemOps()
			store := newGenerationStore(root, ops)
			active, err := store.converge()
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, filepath.Join(active, "daidai.db"), "database payload")
			if err := store.finalize(filepath.Base(active), generationBaseline{}); err != nil {
				t.Fatal(err)
			}
			if failure == "preflight" {
				store.ops.availableBytes = func(string) (uint64, error) { return 0, nil }
			} else {
				store.ops.boundary = func(point string) error {
					if point == "copy-after-write" {
						return syscall.ENOSPC
					}
					return nil
				}
			}

			if _, err := store.prepareMigration(); !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("error=%v want ENOSPC", err)
			}
			entries, err := os.ReadDir(filepath.Join(root, generationsDirName))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != filepath.Base(active) {
				t.Fatalf("generations after failure=%v", entries)
			}
			wantPhase := recoveryPhaseVerified
			if failure == "copy" {
				wantPhase = recoveryPhaseRolledBack
			}
			assertTransactionPhase(t, root, wantPhase)
		})
	}
}

func TestPrepareMigrationRemovesOrphansBeforeSpacePreflight(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.finalize(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(root, generationsDirName, "orphan")
	writeTestFile(t, filepath.Join(orphan, "large"), "junk")
	store.ops.availableBytes = func(string) (uint64, error) {
		if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
			return 0, errors.New("orphan remained during preflight")
		}
		return 1 << 30, nil
	}
	if _, err := store.prepareMigration(); err != nil {
		t.Fatal(err)
	}
}

func TestConvergeRemovesInterruptedCandidateImmediately(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.finalize(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.generationPath(txn.NewGeneration)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.converge(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.generationPath(txn.NewGeneration)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted candidate remains: %v", err)
	}
}

func TestVerifiedGenerationPrunesOlderAndOrphanGenerations(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.sealGeneration(filepath.Base(active), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, generationsDirName, "orphan", "junk"), "junk")
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(store.generationPath(txn.NewGeneration), "daidai.db"), "migrated")
	if err := store.sealGeneration(txn.NewGeneration, generationBaseline{Schema: "2", Runtime: "3"}); err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	if err := store.verify(txn); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, generationsDirName))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("generation count=%d want active+previous", len(entries))
	}
	for _, entry := range entries {
		if entry.Name() == "orphan" {
			t.Fatal("orphan generation remains")
		}
	}
}

func TestGenerationBaselineControlsMigration(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	baseline := generationBaseline{Schema: "schema-1", Runtime: "runtime-1"}
	if err := store.sealGeneration(filepath.Base(active), baseline); err != nil {
		t.Fatal(err)
	}
	needsMigration, err := store.needsMigration(filepath.Base(active), baseline)
	if err != nil || needsMigration {
		t.Fatalf("same baseline migration=%v err=%v", needsMigration, err)
	}
	for _, changed := range []generationBaseline{{Schema: "schema-2", Runtime: "runtime-1"}, {Schema: "schema-1", Runtime: "runtime-2"}} {
		needsMigration, err = store.needsMigration(filepath.Base(active), changed)
		if err != nil || !needsMigration {
			t.Fatalf("changed baseline migration=%v err=%v", needsMigration, err)
		}
	}
}

func TestConvergeRollsBackMutableActiveGenerationAfterCrash(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "healthy")
	if err := store.sealGeneration(filepath.Base(oldGeneration), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	if err := store.verify(txn); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(store.generationPath(txn.NewGeneration), "daidai.db"), "crash-mutated")

	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if active != oldGeneration {
		t.Fatalf("active=%q want previous healthy=%q", active, oldGeneration)
	}
	assertTransactionPhase(t, root, recoveryPhaseRolledBack)
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
