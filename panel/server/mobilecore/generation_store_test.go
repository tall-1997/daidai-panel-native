package mobilecore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestFlatImportPreservesLegacyPrefixFilesAndSkipsExactRecoveryNamespace(t *testing.T) {
	root := t.TempDir()
	legacyFiles := map[string]string{
		".active-generation.tmp-user":             "user-data",
		".recovery-transaction.json.publish-user": "user-data",
		".generation-manifest.json.recover-user":  "user-data",
	}
	for name, value := range legacyFiles {
		writeTestFile(t, filepath.Join(root, name), value)
	}
	writeTestFile(t, filepath.Join(root, recoveryMetadataDirName, recoveryMetadataMarkerName), recoveryMetadataMarkerValue)
	if err := os.MkdirAll(filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName), 0o700); err != nil {
		t.Fatal(err)
	}

	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range legacyFiles {
		data, err := os.ReadFile(filepath.Join(active, name))
		if err != nil || string(data) != value {
			t.Fatalf("legacy business file %q: data=%q err=%v", name, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(active, recoveryMetadataDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserved namespace copied into generation: %v", err)
	}
}

func TestRecoveryMetadataNamespaceRejectsConflicts(t *testing.T) {
	for _, kind := range []string{"file", "symlink", "bad-marker"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			reserved := filepath.Join(root, recoveryMetadataDirName)
			switch kind {
			case "file":
				writeTestFile(t, reserved, "conflict")
			case "symlink":
				if err := os.Symlink(t.TempDir(), reserved); err != nil {
					t.Fatal(err)
				}
			case "bad-marker":
				writeTestFile(t, filepath.Join(reserved, recoveryMetadataMarkerName), "wrong")
			}
			store := newGenerationStore(root, defaultFilesystemOps())
			if _, err := store.converge(); err == nil {
				t.Fatal("expected reserved namespace conflict rejection")
			}
		})
	}
}

func TestWriteAtomicRejectsTargetsOutsideMetadataAllowlist(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	if err := store.writeAtomic(filepath.Join(root, "user-data"), []byte("overwrite"), 0o600, "test"); err == nil {
		t.Fatal("expected metadata target allowlist rejection")
	}
}

func TestConvergeRejectsCorruptAndLinkedOwnershipJournals(t *testing.T) {
	for _, kind := range []string{"corrupt", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			store := newGenerationStore(root, defaultFilesystemOps())
			if err := store.ensureRecoveryMetadataNamespace(); err != nil {
				t.Fatal(err)
			}
			opDir := filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName, "operation-test")
			if err := os.Mkdir(opDir, 0o700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(opDir, recoveryMetadataJournalName)
			external := filepath.Join(t.TempDir(), "external")
			writeTestFile(t, external, "outside")
			switch kind {
			case "corrupt":
				writeTestFile(t, journal, "not-json")
			case "symlink":
				if err := os.Symlink(external, journal); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(external, journal); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.converge(); err == nil {
				t.Fatal("expected unsafe ownership journal rejection")
			}
			outside, err := os.ReadFile(external)
			if err != nil || string(outside) != "outside" {
				t.Fatalf("external file changed: data=%q err=%v", outside, err)
			}
		})
	}
}

func TestMetadataJournalCrashConvergence(t *testing.T) {
	for _, phase := range []string{"journal-prepared", "journal-committed"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			store := newGenerationStore(root, defaultFilesystemOps())
			if err := store.ensureTrustedContainer(); err != nil {
				t.Fatal(err)
			}
			if err := store.ensureRecoveryMetadataNamespace(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, activeGenerationName)
			writeTestFile(t, target, "old\n")
			store.ops.boundary = failBoundary(phase)
			if err := store.writeAtomic(target, []byte("new\n"), 0o600, "test-rename"); err == nil {
				t.Fatal("expected crash boundary")
			}
			restarted := newGenerationStore(root, defaultFilesystemOps())
			if err := restarted.ensureRecoveryMetadataNamespace(); err != nil {
				t.Fatal(err)
			}
			if err := restarted.convergeMetadataJournals(); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			want := "old\n"
			if phase == "journal-committed" {
				want = "new\n"
			}
			if string(data) != want {
				t.Fatalf("target=%q want=%q", data, want)
			}
		})
	}
}

func TestMetadataJournalNextCrashConvergenceAndConflicts(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, activeGenerationName)
	writeTestFile(t, target, "old\n")
	opID := "operation-next-test"
	opDir := filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName, opID)
	if err := os.Mkdir(opDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(opDir, recoveryMetadataOldName), "old\n")
	writeTestFile(t, filepath.Join(opDir, recoveryMetadataNewName), "new\n")
	prepared := metadataJournal{Version: 1, OperationID: opID, Target: activeGenerationName, Old: stateForData([]byte("old\n")), New: stateForData([]byte("new\n")), State: metadataJournalPrepared, Staging: recoveryMetadataExchangeName}
	prepared.Checksum, _ = metadataJournalChecksum(prepared)
	data, _ := json.Marshal(prepared)
	writeTestFile(t, filepath.Join(opDir, recoveryMetadataJournalName+".next"), string(data))
	if err := store.convergeMetadataJournals(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old\n" {
		t.Fatalf("prepared next did not restore old: %q %v", got, err)
	}

	if err := os.Mkdir(opDir, 0o700); err != nil {
		t.Fatal(err)
	}
	prepared.Checksum, _ = metadataJournalChecksum(prepared)
	data, _ = json.Marshal(prepared)
	writeTestFile(t, filepath.Join(opDir, recoveryMetadataJournalName), string(data))
	committed := prepared
	committed.State = metadataJournalCommitted
	committed.Checksum, _ = metadataJournalChecksum(committed)
	data, _ = json.Marshal(committed)
	writeTestFile(t, filepath.Join(opDir, recoveryMetadataJournalName+".next"), string(data))
	if err := store.convergeMetadataJournals(); err == nil {
		t.Fatal("expected journal/next state conflict safety close")
	}
}

func TestMetadataJournalStrictValidation(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"version":1,"operationId":"op","target":"active-generation","old":{},"new":{},"state":"PREPARED","checksum":"bad","unknown":true}`,
		`{"version":1,"operationId":"op","target":"active-generation","old":{"present":false,"size":1,"sha256":""},"new":{"present":true,"size":-1,"sha256":"00"},"state":"PREPARED","checksum":"bad"}`,
	} {
		opDir := filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName, "op")
		_ = os.RemoveAll(opDir)
		if err := os.Mkdir(opDir, 0o700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(opDir, recoveryMetadataJournalName), raw)
		if _, err := store.readMetadataJournal(opDir); err == nil {
			t.Fatalf("accepted invalid journal: %s", raw)
		}
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

func TestRollbackKeepsCandidateUntilRolledBackTransactionIsDurable(t *testing.T) {
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
	candidate := store.generationPath(txn.NewGeneration)
	store.ops.boundary = failBoundary("rollback-transaction")
	if err := store.rollback(txn); err == nil {
		t.Fatal("expected rollback transaction persistence failure")
	}
	if _, err := os.Stat(candidate); err != nil {
		t.Fatalf("candidate removed before rollback transaction became durable: %v", err)
	}
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

func TestValidateGenerationRejectsGenerationRootSymlink(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(active)
	realGeneration := filepath.Join(root, "real-generation")
	if err := os.Rename(active, realGeneration); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realGeneration, active); err != nil {
		t.Fatal(err)
	}
	if err := store.validateGeneration(id); err == nil {
		t.Fatal("expected generation root symlink rejection")
	}
}

func TestGenerationStoreRejectsSymlinkedGenerationsContainerWithoutExternalWrites(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, generationsDirName)); err != nil {
		t.Fatal(err)
	}
	store := newGenerationStore(root, defaultFilesystemOps())
	if _, err := store.converge(); err == nil {
		t.Fatal("expected symlinked generations container rejection")
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("external directory was modified: %v", entries)
	}
}

func TestGenerationStoreRejectsSymlinkedDataDirComponentWithoutExternalWrites(t *testing.T) {
	rootParent := t.TempDir()
	external := t.TempDir()
	writeTestFile(t, filepath.Join(external, "sentinel"), "safe")
	linkedParent := filepath.Join(rootParent, "linked")
	if err := os.Symlink(external, linkedParent); err != nil {
		t.Fatal(err)
	}
	store := newGenerationStore(filepath.Join(linkedParent, "data"), defaultFilesystemOps())
	if _, err := store.converge(); err == nil {
		t.Fatal("expected symlinked dataDir component rejection")
	}
	if _, err := os.Stat(filepath.Join(external, "data")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("external data directory was created: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(external, "sentinel"))
	if err != nil || string(data) != "safe" {
		t.Fatalf("external sentinel changed: data=%q err=%v", data, err)
	}
}

func TestGenerationStoreCreatesMissingTrustedRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new-data-root")
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(active) != filepath.Join(root, generationsDirName) {
		t.Fatalf("active=%q", active)
	}
}

func TestPruneRejectsSymlinkedGenerationsContainerWithoutExternalCleanup(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	writeTestFile(t, filepath.Join(external, "keep", "data"), "safe")
	if err := os.Symlink(external, filepath.Join(root, generationsDirName)); err != nil {
		t.Fatal(err)
	}
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.pruneGenerations("active", "previous"); err == nil {
		t.Fatal("expected symlinked generations container rejection")
	}
	if _, err := os.Stat(filepath.Join(external, "keep", "data")); err != nil {
		t.Fatalf("external data was cleaned: %v", err)
	}
}

func TestCopyDatasetRejectsSymlinkedGenerationTargetWithoutExternalWrites(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	writeTestFile(t, filepath.Join(source, "payload"), "unsafe")
	external := t.TempDir()
	target := store.generationPath("candidate")
	if err := os.Symlink(external, target); err != nil {
		t.Fatal(err)
	}
	if err := store.copyDataset(source, target, false); err == nil {
		t.Fatal("expected symlinked generation target rejection")
	}
	entries, err := os.ReadDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("external target was modified: %v", entries)
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
	entries, err := os.ReadDir(active)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+generationManifestName+".tmp-") {
			t.Fatalf("owned temporary file remains after failure: %s", entry.Name())
		}
	}
}

func TestAtomicMetadataWritesIgnorePredictableTemporarySymlinks(t *testing.T) {
	tests := []struct {
		name      string
		target    func(root, active string) string
		write     func(*generationStore, string) error
		readValue func(string) ([]byte, error)
	}{
		{
			name:   "active pointer",
			target: func(root, _ string) string { return filepath.Join(root, activeGenerationName) },
			write:  func(store *generationStore, _ string) error { return store.writePointer("generation-safe") },
			readValue: func(path string) ([]byte, error) {
				return os.ReadFile(path)
			},
		},
		{
			name:   "recovery transaction",
			target: func(root, _ string) string { return filepath.Join(root, recoveryTransactionName) },
			write: func(store *generationStore, _ string) error {
				return store.writeTransaction(recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, NewGeneration: "generation-safe"})
			},
			readValue: os.ReadFile,
		},
		{
			name:      "generation manifest",
			target:    func(_, active string) string { return filepath.Join(active, generationManifestName) },
			write:     func(store *generationStore, active string) error { return nil },
			readValue: os.ReadFile,
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
			external := filepath.Join(t.TempDir(), "external")
			writeTestFile(t, external, "outside")
			target := tt.target(root, active)
			if err := os.Symlink(external, target+".tmp"); err != nil {
				t.Fatal(err)
			}
			if err := tt.write(store, active); err != nil {
				t.Fatal(err)
			}
			outside, err := os.ReadFile(external)
			if err != nil || string(outside) != "outside" {
				t.Fatalf("external content changed: content=%q err=%v", outside, err)
			}
			info, err := os.Lstat(target)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("metadata target mode=%v", info.Mode())
			}
			if tt.name != "generation manifest" {
				if _, err := tt.readValue(target); err != nil {
					t.Fatal(err)
				}
			}
			temporaryInfo, err := os.Lstat(target + ".tmp")
			if err != nil {
				t.Fatal(err)
			}
			if temporaryInfo.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("attacker temporary path was replaced: mode=%v", temporaryInfo.Mode())
			}
		})
	}
}

func TestAtomicMetadataWritesRejectSymlinkedTargets(t *testing.T) {
	tests := []struct {
		name   string
		target func(root, active string) string
		write  func(*generationStore, string) error
	}{
		{
			name:   "active pointer",
			target: func(root, _ string) string { return filepath.Join(root, activeGenerationName) },
			write:  func(store *generationStore, _ string) error { return store.writePointer("generation-safe") },
		},
		{
			name:   "recovery transaction",
			target: func(root, _ string) string { return filepath.Join(root, recoveryTransactionName) },
			write: func(store *generationStore, _ string) error {
				return store.writeTransaction(recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, NewGeneration: "generation-safe"})
			},
		},
		{
			name:   "generation manifest",
			target: func(_, active string) string { return filepath.Join(active, generationManifestName) },
			write: func(store *generationStore, active string) error {
				return store.sealGeneration(filepath.Base(active), generationBaseline{Schema: "safe"})
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
			external := filepath.Join(t.TempDir(), "external")
			writeTestFile(t, external, "outside")
			target := tt.target(root, active)
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if err := os.Symlink(external, target); err != nil {
				t.Fatal(err)
			}
			if err := tt.write(store, active); err == nil {
				t.Fatal("expected symlinked metadata target rejection")
			}
			outside, err := os.ReadFile(external)
			if err != nil || string(outside) != "outside" {
				t.Fatalf("external content changed: content=%q err=%v", outside, err)
			}
		})
	}
}

func TestAtomicMetadataWriteRejectsReplacedOwnedTemporaryWithoutDeletingAttackerPath(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external")
	writeTestFile(t, external, "outside")
	var attackerPath string
	store.ops.boundary = func(point string) error {
		if point != "pointer-rename" {
			return nil
		}
		entries, err := os.ReadDir(filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName))
		if err != nil {
			return err
		}
		for _, entry := range entries {
			candidate := filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName, entry.Name(), recoveryMetadataNewName)
			if _, err := os.Stat(candidate); err == nil {
				attackerPath = candidate
				if err := os.Remove(attackerPath); err != nil {
					return err
				}
				return os.Symlink(external, attackerPath)
			}
		}
		return errors.New("owned temporary file was not found")
	}

	if err := store.writePointer("generation-safe"); err == nil {
		t.Fatal("expected unsafe operation cleanup failure")
	}
	data, err := os.ReadFile(filepath.Join(root, activeGenerationName))
	if err != nil || string(data) != "generation-safe\n" {
		t.Fatalf("open temp inode was not installed: data=%q err=%v", data, err)
	}
	info, err := os.Lstat(attackerPath)
	if err != nil {
		t.Fatalf("attacker path was deleted: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("attacker path mode=%v", info.Mode())
	}
	outside, err := os.ReadFile(external)
	if err != nil || string(outside) != "outside" {
		t.Fatalf("external content changed: content=%q err=%v", outside, err)
	}
}

func TestAtomicMetadataPublishBindsInstalledInodeToOpenFile(t *testing.T) {
	for _, targetExists := range []bool{false, true} {
		name := "target missing"
		if targetExists {
			name = "target exists"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store := newGenerationStore(root, defaultFilesystemOps())
			if err := store.ensureTrustedContainer(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, activeGenerationName)
			if targetExists {
				writeTestFile(t, target, "old-valid\n")
			}
			external := filepath.Join(t.TempDir(), "external")
			writeTestFile(t, external, "outside")
			var attackerTemp string
			store.ops.boundary = func(point string) error {
				if point != "publish-before-commit" {
					return nil
				}
				entries, err := os.ReadDir(filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName))
				if err != nil {
					return err
				}
				for _, entry := range entries {
					candidate := filepath.Join(root, recoveryMetadataDirName, recoveryMetadataOpsDirName, entry.Name(), recoveryMetadataNewName)
					if _, err := os.Stat(candidate); err == nil {
						attackerTemp = candidate
						if err := os.Remove(attackerTemp); err != nil {
							return err
						}
						if err := os.Link(external, attackerTemp); err != nil {
							return err
						}
						if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
							return err
						}
						return os.Symlink(external, target)
					}
				}
				return errors.New("metadata temporary file was not found")
			}

			err := store.writePointer("generation-safe")
			if err == nil {
				data, readErr := os.ReadFile(target)
				if readErr != nil || string(data) != "generation-safe\n" {
					t.Fatalf("installed metadata is not the open temp inode: data=%q err=%v", data, readErr)
				}
			} else {
				info, statErr := os.Lstat(target)
				if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
					t.Fatal("attacker symlink remained installed after safe failure")
				}
				if targetExists && statErr == nil {
					data, readErr := os.ReadFile(target)
					if readErr != nil || (string(data) != "old-valid\n" && string(data) != "generation-safe\n") {
						t.Fatalf("metadata did not converge safely: data=%q err=%v", data, readErr)
					}
				}
			}
			outside, err := os.ReadFile(external)
			if err != nil || string(outside) != "outside" {
				t.Fatalf("external content changed: content=%q err=%v", outside, err)
			}
			targetInfo, targetErr := os.Lstat(target)
			externalInfo, externalErr := os.Lstat(external)
			if targetErr == nil && externalErr == nil && os.SameFile(targetInfo, externalInfo) {
				t.Fatal("attacker inode was installed as metadata")
			}
			if attackerTemp == "" {
				t.Fatal("replacement injection did not run")
			}
		})
	}
}

func TestAtomicMetadataPublishFailureRestoresExistingMetadata(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	if err := store.ensureTrustedContainer(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, activeGenerationName)
	writeTestFile(t, target, "old-valid\n")
	store.ops.boundary = func(point string) error {
		if point != "publish-after-commit" {
			return nil
		}
		return errors.New("injected post-publish failure")
	}
	if err := store.writePointer("generation-safe"); err == nil {
		t.Fatal("expected injected publish failure")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "old-valid\n" {
		t.Fatalf("old metadata was not restored: data=%q err=%v", data, err)
	}
}

func TestConvergeCleansMetadataTemporariesBeforeVerifiedValidation(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(oldGeneration, "daidai.db"), "old")
	if err := store.sealGeneration(filepath.Base(oldGeneration), generationBaseline{Schema: "old"}); err != nil {
		t.Fatal(err)
	}
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	newGeneration := store.generationPath(txn.NewGeneration)
	writeTestFile(t, filepath.Join(newGeneration, "daidai.db"), "new")
	if err := store.sealGeneration(txn.NewGeneration, generationBaseline{Schema: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	if err := store.markReady(txn.NewGeneration); err != nil {
		t.Fatal(err)
	}
	rootTemporary := filepath.Join(root, "."+activeGenerationName+".tmp-crash")
	generationTemporary := filepath.Join(newGeneration, "."+generationManifestName+".tmp-crash")
	writeTestFile(t, rootTemporary, "stale")
	writeTestFile(t, generationTemporary, "stale")
	if err := store.sealGeneration(txn.NewGeneration, generationBaseline{Schema: "new"}); err != nil {
		t.Fatal(err)
	}

	restarted := newGenerationStore(root, defaultFilesystemOps())
	active, err := restarted.converge()
	if err != nil {
		t.Fatal(err)
	}
	if active != newGeneration {
		t.Fatalf("active=%q want=%q", active, newGeneration)
	}
	data, err := os.ReadFile(filepath.Join(newGeneration, "daidai.db"))
	if err != nil || string(data) != "new" {
		t.Fatalf("verified generation data changed: data=%q err=%v", data, err)
	}
	for _, path := range []string{rootTemporary, generationTemporary} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("legacy business file was removed at %s: %v", path, err)
		}
	}
}

func TestFirstImportDoesNotCopyMetadataTemporaries(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "daidai.db"), "legacy")
	for _, name := range []string{
		"." + activeGenerationName + ".tmp-crash",
		"." + recoveryTransactionName + ".publish-crash",
	} {
		writeTestFile(t, filepath.Join(root, name), "stale")
	}
	store := newGenerationStore(root, defaultFilesystemOps())
	active, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(active)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 3 {
		t.Fatalf("legacy prefix files were not imported: %v", entries)
	}
}

func TestConvergeRejectsSuspiciousMetadataTemporaryWithoutTouchingExternalFile(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			external := filepath.Join(t.TempDir(), "external")
			writeTestFile(t, external, "outside")
			temporary := filepath.Join(root, "."+activeGenerationName+".tmp-suspicious")
			var err error
			if kind == "symlink" {
				err = os.Symlink(external, temporary)
			} else {
				err = os.Link(external, temporary)
			}
			if err != nil {
				t.Fatal(err)
			}
			store := newGenerationStore(root, defaultFilesystemOps())
			_, _ = store.converge()
			outside, err := os.ReadFile(external)
			if err != nil || string(outside) != "outside" {
				t.Fatalf("external content changed: content=%q err=%v", outside, err)
			}
			if _, err := os.Lstat(temporary); err != nil {
				t.Fatalf("suspicious path was removed: %v", err)
			}
		})
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

func TestConvergeCompletesPruneAfterVerifiedTransactionCrash(t *testing.T) {
	root := t.TempDir()
	store := newGenerationStore(root, defaultFilesystemOps())
	oldGeneration, err := store.converge()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.finalize(filepath.Base(oldGeneration), generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	txn, err := store.prepareMigration()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.sealGeneration(txn.NewGeneration, generationBaseline{}); err != nil {
		t.Fatal(err)
	}
	if err := store.commitPointer(txn); err != nil {
		t.Fatal(err)
	}
	orphan := store.generationPath("orphan-after-verified")
	writeTestFile(t, filepath.Join(orphan, "junk"), "junk")
	store.ops.removeAll = func(path string) error {
		if path == orphan {
			return errors.New("prune interrupted")
		}
		return os.RemoveAll(path)
	}
	if err := store.markReady(txn.NewGeneration); err == nil {
		t.Fatal("expected interrupted prune failure")
	}
	assertTransactionPhase(t, root, recoveryPhaseVerified)

	restarted := newGenerationStore(root, defaultFilesystemOps())
	active, err := restarted.converge()
	if err != nil {
		t.Fatal(err)
	}
	if active != restarted.generationPath(txn.NewGeneration) {
		t.Fatalf("active=%q want=%q", active, restarted.generationPath(txn.NewGeneration))
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("orphan remains after restart: %v", err)
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
