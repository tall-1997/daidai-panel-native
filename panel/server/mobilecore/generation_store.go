package mobilecore

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	generationsDirName           = "generations"
	activeGenerationName         = "active-generation"
	recoveryTransactionName      = "recovery-transaction.json"
	generationManifestName       = "generation-manifest.json"
	recoveryPhaseBuilding        = "building"
	recoveryPhasePrepared        = "prepared"
	recoveryPhaseOldSealed       = "old-generation-sealed"
	recoveryPhaseCommitted       = "pointer-committed"
	recoveryPhaseVerified        = "verified"
	recoveryPhaseRolledBack      = "rolled-back"
	recoveryMetadataDirName      = ".recovery-meta"
	recoveryMetadataOpsDirName   = "ops"
	recoveryMetadataMarkerName   = "format"
	recoveryMetadataMarkerValue  = "daidai-recovery-metadata-v1\n"
	recoveryMetadataJournalName  = "journal.json"
	recoveryMetadataOldName      = "old-state"
	recoveryMetadataNewName      = "new-state"
	recoveryMetadataExchangeName = "exchange-state"
	recoveryProbePrefix          = ".recovery-probe-"
	recoveryProbeMarkerName      = "owner.json"
	metadataJournalPrepared      = "PREPARED"
	metadataJournalCommitted     = "COMMITTED"
	metadataJournalRolledBack    = "ROLLED_BACK"
)

type metadataState struct {
	Present bool   `json:"present"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type metadataJournal struct {
	Version     int           `json:"version"`
	OperationID string        `json:"operationId"`
	Target      string        `json:"target"`
	Old         metadataState `json:"old"`
	New         metadataState `json:"new"`
	State       string        `json:"state"`
	Staging     string        `json:"staging"`
	Checksum    string        `json:"checksum"`
}

type recoveryProbeMarker struct {
	Version     int    `json:"version"`
	OperationID string `json:"operationId"`
	Checksum    string `json:"checksum"`
}

func probeMarkerData(id string) ([]byte, error) {
	marker := recoveryProbeMarker{Version: 1, OperationID: id}
	base, err := json.Marshal(marker)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(base)
	marker.Checksum = hex.EncodeToString(digest[:])
	return json.Marshal(marker)
}

func validProbeMarker(data []byte, id string) bool {
	var marker recoveryProbeMarker
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&marker) != nil || decoder.Decode(&struct{}{}) != io.EOF || marker.Version != 1 || marker.OperationID != id || len(marker.Checksum) != 64 {
		return false
	}
	want := marker.Checksum
	marker.Checksum = ""
	base, err := json.Marshal(marker)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(base)
	canonical, _ := probeMarkerData(id)
	return want == hex.EncodeToString(digest[:]) && bytes.Equal(data, canonical)
}

type recoveryTransaction struct {
	Version       int    `json:"version"`
	Phase         string `json:"phase"`
	OldGeneration string `json:"oldGeneration"`
	NewGeneration string `json:"newGeneration"`
}

type generationManifest struct {
	Version  int                     `json:"version"`
	Baseline generationBaseline      `json:"baseline"`
	Files    map[string]manifestFile `json:"files"`
}

type generationBaseline struct {
	Schema  string `json:"schema"`
	Runtime string `json:"runtime"`
}

type manifestFile struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type filesystemOps struct {
	openFile       func(string, int, fs.FileMode) (*os.File, error)
	readFile       func(string) ([]byte, error)
	mkdirAll       func(string, fs.FileMode) error
	rename         func(string, string) error
	stat           func(string) (fs.FileInfo, error)
	lstat          func(string) (fs.FileInfo, error)
	walkDir        func(string, fs.WalkDirFunc) error
	remove         func(string) error
	removeAll      func(string) error
	availableBytes func(string) (uint64, error)
	boundary       func(string) error
}

var probeRecoveryMetadataPlatform = platformProbeRecoveryMetadata
var recoveryPublishBoundary = func(string) error { return nil }
var recoveryProbeBoundary = func(string) error { return nil }
var recoveryNamespaceSync = platformSyncDirectory
var recoveryRollbackBoundary = func(string) error { return nil }
var useAndroidPrivateStorage = runtime.GOOS == "android"

func defaultFilesystemOps() filesystemOps {
	return filesystemOps{
		openFile:       os.OpenFile,
		readFile:       os.ReadFile,
		mkdirAll:       os.MkdirAll,
		rename:         os.Rename,
		stat:           os.Stat,
		lstat:          os.Lstat,
		walkDir:        filepath.WalkDir,
		remove:         os.Remove,
		removeAll:      os.RemoveAll,
		availableBytes: platformAvailableBytes,
		boundary:       func(string) error { return nil },
	}
}

type generationStore struct {
	root string
	ops  filesystemOps
}

func newGenerationStore(root string, ops filesystemOps) *generationStore {
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &generationStore{root: root, ops: ops}
}

func (store *generationStore) generationPath(id string) string {
	return filepath.Join(store.root, generationsDirName, id)
}

func (store *generationStore) validateRootComponents() error {
	return store.ensureTrustedDirectoryComponents(store.root, true)
}

func (store *generationStore) ensureTrustedContainer() error {
	root := store.root
	if useAndroidPrivateStorage {
		if err := store.ensureOwnedDirectory(root); err != nil {
			return fmt.Errorf("generation root is not a trusted directory: %w", err)
		}
		if err := store.ensureOwnedDirectory(filepath.Join(root, generationsDirName)); err != nil {
			return fmt.Errorf("generations container is not a trusted directory: %w", err)
		}
		return nil
	}
	if err := store.ensureTrustedDirectoryComponents(root, true); err != nil {
		return err
	}
	if err := store.ops.mkdirAll(root, 0o700); err != nil {
		return err
	}
	if err := store.ensureTrustedDirectoryComponents(root, false); err != nil {
		return errors.New("generation root is not a trusted directory")
	}
	container := filepath.Join(root, generationsDirName)
	if err := store.ensureTrustedDirectoryComponents(container, true); err != nil {
		return err
	}
	if err := store.ops.mkdirAll(container, 0o700); err != nil {
		return err
	}
	if err := store.ensureTrustedDirectoryComponents(container, false); err != nil {
		return errors.New("generations container is not a trusted directory")
	}
	return nil
}

func (store *generationStore) ensureOwnedDirectory(path string) error {
	if err := store.ops.mkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := store.ops.lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("owned path is not a directory")
	}
	return nil
}

func (store *generationStore) ensureTrustedDirectoryComponents(path string, allowMissing bool) error {
	path = filepath.Clean(path)
	components := []string{}
	for current := path; ; current = filepath.Dir(current) {
		components = append(components, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for i := len(components) - 1; i >= 0; i-- {
		info, err := store.ops.lstat(components[i])
		if allowMissing && errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if runtime.GOOS == "android" && info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			return fmt.Errorf("untrusted directory component: %s", components[i])
		}
	}
	return nil
}

func (store *generationStore) ensureTrustedGeneration(id string, allowMissing bool) error {
	if err := store.ensureTrustedContainer(); err != nil {
		return err
	}
	if !validGenerationID(id) {
		return errors.New("generation ID is invalid")
	}
	info, err := store.ops.lstat(store.generationPath(id))
	if allowMissing && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("generation is not a trusted directory")
	}
	return nil
}

func (store *generationStore) converge() (string, error) {
	if err := store.ensureTrustedContainer(); err != nil {
		return "", err
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		return "", err
	}
	if useAndroidPrivateStorage {
		if err := store.convergeAndroidOperations(); err != nil {
			return "", err
		}
	} else if err := store.convergeMetadataJournals(); err != nil {
		return "", err
	}
	txn, err := store.readTransaction()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err == nil {
		switch txn.Phase {
		case recoveryPhaseBuilding, recoveryPhasePrepared, recoveryPhaseOldSealed:
			pointerID, pointerErr := store.readPointerID()
			if pointerErr == nil && pointerID == txn.NewGeneration && txn.OldGeneration == "" {
				txn.Phase = recoveryPhaseCommitted
				if err := store.writeTransaction(txn); err != nil {
					return "", err
				}
				return store.generationPath(txn.NewGeneration), nil
			}
			if txn.OldGeneration != "" {
				if err := store.writePointer(txn.OldGeneration); err != nil {
					return "", err
				}
			}
			if err := store.ensureTrustedGeneration(txn.NewGeneration, true); err != nil {
				return "", err
			}
			if err := store.removeGeneration(txn.NewGeneration); err != nil {
				return "", err
			}
			txn.Phase = recoveryPhaseRolledBack
			if err := store.writeTransaction(txn); err != nil {
				return "", err
			}
		case recoveryPhaseCommitted:
			if txn.OldGeneration == "" {
				if pointerID, pointerErr := store.readPointerID(); pointerErr != nil || pointerID != txn.NewGeneration {
					return "", errors.New("first generation pointer is unavailable")
				}
				return store.generationPath(txn.NewGeneration), nil
			}
			if err := store.rollback(txn); err != nil {
				return "", err
			}
		case recoveryPhaseVerified:
			if err := store.verifyGeneration(txn.NewGeneration); err != nil {
				if txn.OldGeneration == "" {
					return "", fmt.Errorf("verified generation invalid: %w", err)
				}
				if oldErr := store.verifyGeneration(txn.OldGeneration); oldErr != nil {
					return "", fmt.Errorf("active and previous generations invalid: %w", err)
				}
				if err := store.writePointer(txn.OldGeneration); err != nil {
					return "", err
				}
				txn.Phase = recoveryPhaseRolledBack
				if err := store.writeTransaction(txn); err != nil {
					return "", err
				}
				if err := store.ensureTrustedGeneration(txn.NewGeneration, true); err != nil {
					return "", err
				}
				if err := store.removeGeneration(txn.NewGeneration); err != nil {
					return "", err
				}
			} else if err := store.pruneGenerations(txn.NewGeneration, txn.OldGeneration); err != nil {
				return "", err
			}
		}
	}

	active, err := store.activeGeneration()
	if err == nil {
		return active, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return store.importFlatData()
}

func (store *generationStore) importFlatData() (string, error) {
	if err := store.ensureTrustedContainer(); err != nil {
		return "", err
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		return "", err
	}
	if !useAndroidPrivateStorage {
		if err := store.convergeMetadataJournals(); err != nil {
			return "", err
		}
	} else if err := store.convergeAndroidOperations(); err != nil {
		return "", err
	}
	id, err := newGenerationID()
	if err != nil {
		return "", err
	}
	txn := recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, NewGeneration: id}
	if err := store.writeTransaction(txn); err != nil {
		return "", err
	}
	if err := store.copyDataset(store.root, store.generationPath(id), true); err != nil {
		return "", err
	}
	txn.Phase = recoveryPhasePrepared
	if err := store.writeTransaction(txn); err != nil {
		return "", err
	}
	txn.Phase = recoveryPhaseOldSealed
	if err := store.writeTransaction(txn); err != nil {
		return "", err
	}
	if err := store.writePointer(id); err != nil {
		return "", err
	}
	txn.Phase = recoveryPhaseCommitted
	if err := store.writeTransaction(txn); err != nil {
		return "", err
	}
	return store.generationPath(id), nil
}

func (store *generationStore) prepareMigration() (recoveryTransaction, error) {
	oldID, err := store.readPointerID()
	if err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.ensureTrustedGeneration(oldID, false); err != nil {
		return recoveryTransaction{}, err
	}
	active := store.generationPath(oldID)
	baseline, err := store.generationBaseline(oldID)
	if err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.sealGeneration(oldID, baseline); err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.cleanupOrphans(oldID); err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.preflightMigration(active); err != nil {
		return recoveryTransaction{}, err
	}
	newID, err := newGenerationID()
	if err != nil {
		return recoveryTransaction{}, err
	}
	txn := recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, OldGeneration: oldID, NewGeneration: newID}
	if err := store.writeTransaction(txn); err != nil {
		if trustErr := store.ensureTrustedGeneration(newID, true); trustErr != nil {
			return recoveryTransaction{}, errors.Join(err, trustErr)
		}
		cleanupErr := store.removeGeneration(newID)
		return recoveryTransaction{}, errors.Join(err, cleanupErr)
	}
	if err := store.copyDataset(active, store.generationPath(newID), false); err != nil {
		return recoveryTransaction{}, errors.Join(err, store.rollback(txn))
	}
	txn.Phase = recoveryPhasePrepared
	if err := store.writeTransaction(txn); err != nil {
		return recoveryTransaction{}, errors.Join(err, store.rollback(txn))
	}
	txn.Phase = recoveryPhaseOldSealed
	if err := store.writeTransaction(txn); err != nil {
		return recoveryTransaction{}, errors.Join(err, store.rollback(txn))
	}
	return txn, nil
}

func (store *generationStore) commitPointer(txn recoveryTransaction) error {
	if err := store.ensureTrustedGeneration(txn.NewGeneration, true); err != nil {
		return err
	}
	if err := store.verifyGeneration(txn.NewGeneration); err != nil {
		return err
	}
	if err := store.writePointer(txn.NewGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseCommitted
	return store.writeTransaction(txn)
}

func (store *generationStore) finalize(id string, baseline generationBaseline) error {
	if err := store.sealGeneration(id, baseline); err != nil {
		return err
	}
	return store.markReady(id)
}

func (store *generationStore) markReady(id string) error {
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return err
	}
	txn, err := store.readTransaction()
	if err != nil {
		return err
	}
	if txn.NewGeneration != id || txn.Phase != recoveryPhaseCommitted {
		return errors.New("generation is not ready to finalize")
	}
	if err := store.verifyGeneration(id); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseVerified
	if err := store.writeTransaction(txn); err != nil {
		return err
	}
	return store.pruneGenerations(id, txn.OldGeneration)
}

func (store *generationStore) verify(txn recoveryTransaction) error {
	return store.markReady(txn.NewGeneration)
}

func (store *generationStore) rollback(txn recoveryTransaction) error {
	if txn.OldGeneration == "" {
		return errors.New("recovery transaction has no old generation")
	}
	if err := store.ensureTrustedGeneration(txn.OldGeneration, false); err != nil {
		return err
	}
	if err := store.ensureTrustedGeneration(txn.NewGeneration, true); err != nil {
		return err
	}
	if err := store.writePointer(txn.OldGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseRolledBack
	if err := store.writeTransactionAt(txn, "rollback-transaction"); err != nil {
		return err
	}
	return store.removeGeneration(txn.NewGeneration)
}

func (store *generationStore) activeGeneration() (string, error) {
	id, err := store.readPointerID()
	if err != nil {
		return "", err
	}
	if err := store.validateGeneration(id); err != nil {
		return "", err
	}
	return store.generationPath(id), nil
}

func (store *generationStore) removeGeneration(id string) error {
	if err := store.ensureTrustedGeneration(id, true); err != nil {
		return err
	}
	if err := store.ops.removeAll(store.generationPath(id)); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Join(store.root, generationsDirName))
}

func (store *generationStore) validateGeneration(id string) error {
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return err
	}
	return store.verifyGeneration(id)
}

func (store *generationStore) readPointerID() (string, error) {
	data, err := store.ops.readFile(filepath.Join(store.root, activeGenerationName))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !validGenerationID(id) {
		return "", errors.New("active generation pointer is invalid")
	}
	return id, nil
}

func (store *generationStore) copyDataset(source, destination string, flat bool) error {
	if err := store.ensureTrustedContainer(); err != nil {
		return err
	}
	id := filepath.Base(destination)
	if filepath.Clean(filepath.Dir(destination)) != filepath.Clean(filepath.Join(store.root, generationsDirName)) {
		return errors.New("generation destination escapes trusted container")
	}
	if err := store.ensureTrustedGeneration(id, true); err != nil {
		return err
	}
	if err := store.ops.mkdirAll(destination, 0o700); err != nil {
		return err
	}
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return err
	}
	directories := []string{destination, filepath.Dir(destination)}
	manifest := generationManifest{Version: 1, Files: map[string]manifestFile{}}
	err := store.ops.walkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == "." {
			return err
		}
		first := strings.Split(relative, string(filepath.Separator))[0]
		if flat && (first == generationsDirName || first == recoveryMetadataDirName || relative == activeGenerationName || relative == recoveryTransactionName) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !flat && relative == generationManifestName {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.Mode().IsRegular() && !info.IsDir()) {
			return fmt.Errorf("unsupported data entry %q", relative)
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			if err := store.ops.mkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			directories = append(directories, target)
			return nil
		}
		digest, err := store.copyFile(path, target, info.Mode().Perm())
		if err != nil {
			return err
		}
		manifest.Files[filepath.ToSlash(relative)] = manifestFile{Size: info.Size(), SHA256: digest}
		return nil
	})
	if err != nil {
		return err
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := store.writeSyncedFile(filepath.Join(destination, generationManifestName), manifestData, 0o600); err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i], string(filepath.Separator)) > strings.Count(directories[j], string(filepath.Separator))
	})
	for _, directory := range directories {
		if err := store.syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

func (store *generationStore) copyFile(source, destination string, mode fs.FileMode) (string, error) {
	if err := store.ops.boundary("copy-before-write"); err != nil {
		return "", err
	}
	input, err := store.ops.openFile(source, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := store.ops.openFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	if copyErr == nil {
		copyErr = store.ops.boundary("copy-after-write")
	}
	if copyErr == nil {
		copyErr = store.ops.boundary("file-fsync")
	}
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (store *generationStore) verifyGeneration(id string) error {
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return err
	}
	directory := store.generationPath(id)
	data, err := store.ops.readFile(filepath.Join(directory, generationManifestName))
	if err != nil {
		return err
	}
	var manifest generationManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return errors.New("generation manifest is invalid")
	}
	if manifest.Baseline.Schema != "" {
		if _, ok := manifest.Files["daidai.db"]; !ok {
			return errors.New("generation manifest is missing required database")
		}
	}
	seen := make(map[string]bool, len(manifest.Files))
	err = store.ops.walkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil || relative == "." || entry.IsDir() {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == generationManifestName {
			return nil
		}
		if isSQLiteSidecar(relative) {
			return nil
		}
		if _, ok := manifest.Files[relative]; !ok {
			return fmt.Errorf("generation contains unmanifested file: %s", relative)
		}
		seen[relative] = true
		return nil
	})
	if err != nil {
		return err
	}
	for relative, want := range manifest.Files {
		if !validManifestPath(relative) {
			return errors.New("generation manifest path is invalid")
		}
		path := filepath.Join(directory, filepath.FromSlash(relative))
		info, err := store.ops.lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != want.Size {
			return fmt.Errorf("generation file size mismatch: %s", relative)
		}
		data, err := store.ops.readFile(path)
		if err != nil {
			return err
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want.SHA256 {
			return fmt.Errorf("generation file checksum mismatch: %s", relative)
		}
		if !seen[relative] {
			return fmt.Errorf("generation file missing: %s", relative)
		}
	}
	return nil
}

func isSQLiteSidecar(relative string) bool {
	return relative == "daidai.db-wal" || relative == "daidai.db-shm"
}

func (store *generationStore) sealGeneration(id string, baseline generationBaseline) error {
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return err
	}
	directory := store.generationPath(id)
	directories := []string{directory, filepath.Dir(directory)}
	manifest := generationManifest{Version: 1, Baseline: baseline, Files: map[string]manifestFile{}}
	err := store.ops.walkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil || relative == "." {
			return err
		}
		if relative == generationManifestName {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported data entry %q", relative)
		}
		file, err := store.ops.openFile(path, os.O_RDONLY, 0)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, file)
		if copyErr == nil {
			copyErr = store.ops.boundary("file-fsync")
		}
		if copyErr == nil {
			copyErr = file.Sync()
		}
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		manifest.Files[filepath.ToSlash(relative)] = manifestFile{Size: info.Size(), SHA256: hex.EncodeToString(hash.Sum(nil))}
		return nil
	})
	if err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := store.writeAtomic(filepath.Join(directory, generationManifestName), data, 0o600, "manifest-rename"); err != nil {
		return err
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i], string(filepath.Separator)) > strings.Count(directories[j], string(filepath.Separator))
	})
	for _, path := range directories {
		if err := store.syncDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func (store *generationStore) generationBaseline(id string) (generationBaseline, error) {
	if err := store.ensureTrustedGeneration(id, false); err != nil {
		return generationBaseline{}, err
	}
	data, err := store.ops.readFile(filepath.Join(store.generationPath(id), generationManifestName))
	if err != nil {
		return generationBaseline{}, err
	}
	var manifest generationManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return generationBaseline{}, errors.New("generation manifest is invalid")
	}
	return manifest.Baseline, nil
}

func (store *generationStore) isInitialBootstrap(id string) bool {
	txn, err := store.readTransaction()
	return err == nil && txn.Phase == recoveryPhaseCommitted && txn.OldGeneration == "" && txn.NewGeneration == id
}

func (store *generationStore) needsMigration(id string, baseline generationBaseline) (bool, error) {
	stored, err := store.generationBaseline(id)
	if err != nil {
		return false, err
	}
	return stored != baseline, nil
}

func (store *generationStore) preflightMigration(source string) error {
	var estimated uint64
	if err := store.ops.walkDir(source, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() == generationManifestName {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		estimated += uint64(info.Size())
		return nil
	}); err != nil {
		return err
	}
	available, err := store.ops.availableBytes(filepath.Join(store.root, generationsDirName))
	if err != nil {
		return err
	}
	margin := estimated / 10
	const minimumMargin = uint64(16 << 20)
	if margin < minimumMargin {
		margin = minimumMargin
	}
	if available < estimated+margin {
		return syscall.ENOSPC
	}
	return nil
}

func (store *generationStore) cleanupOrphans(activeID string) error {
	if err := store.ensureTrustedGeneration(activeID, false); err != nil {
		return err
	}
	keep := map[string]bool{activeID: true}
	if txn, err := store.readTransaction(); err == nil && txn.Phase == recoveryPhaseVerified && txn.NewGeneration == activeID && txn.OldGeneration != "" {
		keep[txn.OldGeneration] = true
	}
	entries, err := os.ReadDir(filepath.Join(store.root, generationsDirName))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if keep[entry.Name()] {
			continue
		}
		if err := store.ensureTrustedGeneration(entry.Name(), false); err != nil {
			return err
		}
		if err := store.ops.removeAll(store.generationPath(entry.Name())); err != nil {
			return err
		}
	}
	return store.syncDirectory(filepath.Join(store.root, generationsDirName))
}

func (store *generationStore) pruneGenerations(activeID, previousID string) error {
	if err := store.ensureTrustedGeneration(activeID, false); err != nil {
		return err
	}
	if previousID != "" {
		if err := store.ensureTrustedGeneration(previousID, false); err != nil {
			return err
		}
	}
	entries, err := os.ReadDir(filepath.Join(store.root, generationsDirName))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == activeID || entry.Name() == previousID {
			continue
		}
		if err := store.ensureTrustedGeneration(entry.Name(), false); err != nil {
			return err
		}
		if err := store.ops.removeAll(store.generationPath(entry.Name())); err != nil {
			return err
		}
	}
	return store.syncDirectory(filepath.Join(store.root, generationsDirName))
}

func (store *generationStore) writePointer(id string) error {
	return store.writeAtomic(filepath.Join(store.root, activeGenerationName), []byte(id+"\n"), 0o600, "pointer-rename")
}

func (store *generationStore) writeTransaction(txn recoveryTransaction) error {
	return store.writeTransactionAt(txn, "transaction-rename")
}

func (store *generationStore) writeTransactionAt(txn recoveryTransaction, boundary string) error {
	data, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	return store.writeAtomic(filepath.Join(store.root, recoveryTransactionName), data, 0o600, boundary)
}

func (store *generationStore) readTransaction() (recoveryTransaction, error) {
	data, err := store.ops.readFile(filepath.Join(store.root, recoveryTransactionName))
	if err != nil {
		return recoveryTransaction{}, err
	}
	var txn recoveryTransaction
	if err := json.Unmarshal(data, &txn); err != nil {
		return recoveryTransaction{}, err
	}
	if txn.Version != 1 || !validGenerationID(txn.NewGeneration) {
		return recoveryTransaction{}, errors.New("recovery transaction is invalid")
	}
	if txn.OldGeneration != "" && !validGenerationID(txn.OldGeneration) {
		return recoveryTransaction{}, errors.New("recovery transaction is invalid")
	}
	switch txn.Phase {
	case recoveryPhaseBuilding, recoveryPhasePrepared, recoveryPhaseOldSealed,
		recoveryPhaseCommitted, recoveryPhaseVerified, recoveryPhaseRolledBack:
	default:
		return recoveryTransaction{}, errors.New("recovery transaction phase is invalid")
	}
	return txn, nil
}

func validGenerationID(id string) bool {
	return id != "" && filepath.Base(id) == id && id != "." && id != ".."
}

func validManifestPath(path string) bool {
	if path == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func (store *generationStore) writeAtomic(path string, data []byte, mode fs.FileMode, renameBoundary string) (resultErr error) {
	if useAndroidPrivateStorage {
		return store.writeAtomicPortable(path, data, mode, renameBoundary)
	}
	canonical, err := store.canonicalMetadataTarget(path)
	if err != nil {
		return err
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		return err
	}
	if err := store.validateMetadataTarget(path); err != nil {
		return err
	}
	operationID, err := newGenerationID()
	if err != nil {
		return err
	}
	opDir := filepath.Join(store.root, recoveryMetadataDirName, recoveryMetadataOpsDirName, operationID)
	if err := os.Mkdir(opDir, 0o700); err != nil {
		return err
	}
	if err := platformSyncDirectory(filepath.Dir(opDir)); err != nil {
		return err
	}
	newPath := filepath.Join(opDir, recoveryMetadataNewName)
	if err := store.writeSyncedFileExclusive(newPath, data, mode); err != nil {
		return err
	}
	if err := platformSyncDirectory(opDir); err != nil {
		return err
	}
	newState := stateForData(data)
	publishPath := filepath.Join(opDir, "publish-state")
	if err := store.writeSyncedFileExclusive(publishPath, data, mode); err != nil {
		return err
	}
	if err := platformSyncDirectory(opDir); err != nil {
		return err
	}
	oldState, err := store.captureMetadataState(path, filepath.Join(opDir, recoveryMetadataOldName), mode)
	if err != nil {
		return err
	}
	if oldState.Present {
		if err := platformSyncDirectory(opDir); err != nil {
			return err
		}
	}
	journal := metadataJournal{Version: 1, OperationID: operationID, Target: canonical, Old: oldState, New: newState, State: metadataJournalPrepared, Staging: recoveryMetadataExchangeName}
	if err := store.writeMetadataJournal(opDir, journal); err != nil {
		return err
	}
	if err := store.ops.boundary("journal-prepared"); err != nil {
		return err
	}
	file, err := platformOpenRegularFile(publishPath)
	if err != nil {
		return err
	}
	defer file.Close()
	oldFile, err := openMetadataTarget(path)
	if err != nil {
		return err
	}
	if oldFile != nil {
		defer oldFile.Close()
	}
	publish, rollbackPublish, cleanupPublish, err := platformPrepareAtomicPublish(file, oldFile, publishPath, path, mode)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, cleanupPublish()) }()
	if err := store.ops.boundary(renameBoundary); err != nil {
		return err
	}
	if err := store.ops.boundary("publish-before-commit"); err != nil {
		return err
	}
	if err := publish(); err != nil {
		return store.rollbackMetadataWrite(opDir, path, journal, rollbackPublish, err)
	}
	if err := store.ops.boundary("publish-after-commit"); err != nil {
		return store.rollbackMetadataWrite(opDir, path, journal, rollbackPublish, err)
	}
	if err := store.verifyMetadataState(path, newState); err != nil {
		return store.rollbackMetadataWrite(opDir, path, journal, rollbackPublish, err)
	}
	if err := platformSyncDirectory(filepath.Dir(path)); err != nil {
		return store.rollbackMetadataWrite(opDir, path, journal, rollbackPublish, err)
	}
	journal.State = metadataJournalCommitted
	if err := store.writeMetadataJournal(opDir, journal); err != nil {
		return store.rollbackMetadataWrite(opDir, path, journal, rollbackPublish, err)
	}
	if err := store.ops.boundary("journal-committed"); err != nil {
		return err
	}
	return store.removeOperation(opDir)
}

func (store *generationStore) writeAtomicPortable(path string, data []byte, mode fs.FileMode, renameBoundary string) (resultErr error) {
	if _, err := store.canonicalMetadataTarget(path); err != nil {
		return err
	}
	if err := store.validateMetadataTarget(path); err != nil {
		return err
	}
	temporary, err := randomMetadataPath(filepath.Dir(path), filepath.Base(path)+".android-")
	if err != nil {
		return err
	}
	file, err := store.ops.openFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	closed = true
	if err := store.ops.boundary(renameBoundary); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(path))
}

func (store *generationStore) validateMetadataTarget(path string) error {
	info, err := store.ops.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("atomic metadata target is not a regular file")
	}
	return nil
}

func (store *generationStore) canonicalMetadataTarget(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(store.root, abs)
	if err != nil {
		return "", err
	}
	relative = filepath.ToSlash(relative)
	if relative == activeGenerationName || relative == recoveryTransactionName {
		return relative, nil
	}
	parts := strings.Split(relative, "/")
	if len(parts) == 3 && parts[0] == generationsDirName && validGenerationID(parts[1]) && parts[2] == generationManifestName {
		return relative, nil
	}
	return "", errors.New("metadata target is outside the recovery allowlist")
}

func (store *generationStore) isOwnedProbeNamespace(path string) bool {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, recoveryProbePrefix) {
		return false
	}
	id := strings.TrimPrefix(name, recoveryProbePrefix)
	if !validOperationID(id) {
		return false
	}
	data, err := platformReadRegularFile(filepath.Join(path, recoveryProbeMarkerName))
	if errors.Is(err, os.ErrNotExist) {
		data, err = platformReadRegularFile(path + ".owner")
	}
	return err == nil && validProbeMarker(data, id)
}

func (store *generationStore) ensureRecoveryMetadataNamespace() error {
	if useAndroidPrivateStorage {
		return store.ensureAndroidRecoveryMetadataNamespace()
	}
	if err := store.ensureDurableDirectory(store.root); err != nil {
		return err
	}
	base := filepath.Join(store.root, recoveryMetadataDirName)
	if info, err := store.ops.lstat(base); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("recovery metadata namespace is not a trusted directory")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := store.ensureDurableDirectory(base); err != nil {
			return err
		}
		if err := store.writeSyncedFileExclusive(filepath.Join(base, recoveryMetadataMarkerName), []byte(recoveryMetadataMarkerValue), 0o600); err != nil {
			return err
		}
	} else {
		return err
	}
	marker, err := os.ReadFile(filepath.Join(base, recoveryMetadataMarkerName))
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(base)
		if readErr != nil {
			return readErr
		}
		if len(entries) != 0 {
			return errors.New("recovery metadata marker is missing")
		}
		if err := store.writeSyncedFileExclusive(filepath.Join(base, recoveryMetadataMarkerName), []byte(recoveryMetadataMarkerValue), 0o600); err != nil {
			return err
		}
		if err := recoveryNamespaceSync(base); err != nil {
			return err
		}
		marker = []byte(recoveryMetadataMarkerValue)
		err = nil
	}
	if err != nil || string(marker) != recoveryMetadataMarkerValue {
		return errors.New("recovery metadata marker is invalid")
	}
	markerPath := filepath.Join(base, recoveryMetadataMarkerName)
	markerInfo, err := store.ops.lstat(markerPath)
	if err != nil || !markerInfo.Mode().IsRegular() {
		return errors.New("recovery metadata marker is unsafe")
	}
	markerLinks, err := platformFileLinkCount(markerPath, markerInfo)
	if err != nil || markerLinks != 1 {
		return errors.New("recovery metadata marker has unsafe links")
	}
	ops := filepath.Join(base, recoveryMetadataOpsDirName)
	if err := store.ensureDurableDirectory(ops); err != nil {
		return err
	}
	info, err := store.ops.lstat(ops)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("recovery metadata ops is not a trusted directory")
	}
	return recoveryNamespaceSync(base)
}

func (store *generationStore) ensureAndroidRecoveryMetadataNamespace() error {
	base := filepath.Join(store.root, recoveryMetadataDirName)
	ops := filepath.Join(base, recoveryMetadataOpsDirName)
	for _, path := range []string{store.root, base, ops} {
		if err := store.ensureOwnedDirectory(path); err != nil {
			return err
		}
	}
	markerPath := filepath.Join(base, recoveryMetadataMarkerName)
	if info, err := store.ops.lstat(markerPath); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("recovery metadata marker is unsafe")
		}
		marker, err := store.ops.readFile(markerPath)
		if err != nil || string(marker) != recoveryMetadataMarkerValue {
			return errors.New("recovery metadata marker is invalid")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return store.writeSyncedFileExclusive(markerPath, []byte(recoveryMetadataMarkerValue), 0o600)
}

func (store *generationStore) convergeAndroidOperations() error {
	ops := filepath.Join(store.root, recoveryMetadataDirName, recoveryMetadataOpsDirName)
	entries, err := os.ReadDir(ops)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(ops, entry.Name())
		info, err := store.ops.lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe Android recovery operation")
		}
		if err := store.ops.removeAll(path); err != nil {
			return err
		}
	}
	return nil
}

func (store *generationStore) ensureDurableDirectory(path string) error {
	if info, err := store.ops.lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			if runtime.GOOS == "android" && info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			return errors.New("durable namespace path is unsafe")
		}
		parent := filepath.Dir(path)
		if parent != path {
			if err := recoveryNamespaceSync(parent); err != nil {
				return err
			}
		}
		return recoveryNamespaceSync(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return errors.New("durable namespace parent unavailable")
	}
	if err := store.ensureDurableDirectory(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if err := recoveryNamespaceSync(parent); err != nil {
		return err
	}
	return recoveryNamespaceSync(path)
}

func stateForData(data []byte) metadataState {
	digest := sha256.Sum256(data)
	return metadataState{Present: true, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:])}
}

func (store *generationStore) captureMetadataState(target, backup string, mode fs.FileMode) (metadataState, error) {
	data, err := platformReadRegularFile(target)
	if errors.Is(err, os.ErrNotExist) {
		return metadataState{}, nil
	}
	if err != nil {
		return metadataState{}, err
	}
	if err := store.writeSyncedFileExclusive(backup, data, mode); err != nil {
		return metadataState{}, err
	}
	return stateForData(data), nil
}

func metadataJournalChecksum(journal metadataJournal) (string, error) {
	journal.Checksum = ""
	data, err := json.Marshal(journal)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func (store *generationStore) writeMetadataJournal(opDir string, journal metadataJournal) error {
	checksum, err := metadataJournalChecksum(journal)
	if err != nil {
		return err
	}
	journal.Checksum = checksum
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	path := filepath.Join(opDir, recoveryMetadataJournalName)
	next := filepath.Join(opDir, recoveryMetadataJournalName+".next")
	file, err := platformCreateRecoveryFile(next, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err := os.Rename(next, path); err != nil {
		return err
	}
	return platformSyncDirectory(opDir)
}

func (store *generationStore) readMetadataJournal(opDir string) (metadataJournal, error) {
	journal, journalErr := store.readMetadataJournalFile(opDir, recoveryMetadataJournalName)
	next, nextErr := store.readMetadataJournalFile(opDir, recoveryMetadataJournalName+".next")
	if journalErr != nil && nextErr != nil {
		return metadataJournal{}, errors.Join(journalErr, nextErr)
	}
	if journalErr != nil {
		return next, nil
	}
	if nextErr != nil {
		return journal, nil
	}
	if next.OperationID != journal.OperationID || next.Target != journal.Target || next.Old != journal.Old || next.New != journal.New {
		return metadataJournal{}, errors.New("metadata journal candidates conflict")
	}
	rank := map[string]int{metadataJournalPrepared: 1, metadataJournalCommitted: 2, metadataJournalRolledBack: 2}
	if rank[next.State] < rank[journal.State] || rank[next.State] == rank[journal.State] && next.State != journal.State {
		return metadataJournal{}, errors.New("metadata journal state transition conflicts")
	}
	return next, nil
}

func (store *generationStore) readMetadataJournalFile(opDir, name string) (metadataJournal, error) {
	path := filepath.Join(opDir, name)
	info, err := store.ops.lstat(path)
	if err != nil {
		return metadataJournal{}, err
	}
	if !info.Mode().IsRegular() {
		return metadataJournal{}, errors.New("metadata journal is not regular")
	}
	links, err := platformFileLinkCount(path, info)
	if err != nil || links != 1 {
		return metadataJournal{}, errors.New("metadata journal has unsafe links")
	}
	data, err := platformReadRegularFile(path)
	if err != nil {
		return metadataJournal{}, err
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return metadataJournal{}, err
	}
	var journal metadataJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&journal) != nil || decoder.Decode(&struct{}{}) != io.EOF || journal.Version != 1 {
		return metadataJournal{}, errors.New("metadata journal is invalid")
	}
	want, err := metadataJournalChecksum(journal)
	if err != nil || want != journal.Checksum {
		return metadataJournal{}, errors.New("metadata journal checksum is invalid")
	}
	if journal.OperationID != filepath.Base(opDir) {
		return metadataJournal{}, errors.New("metadata journal operation mismatch")
	}
	if _, err := store.canonicalMetadataTarget(filepath.Join(store.root, filepath.FromSlash(journal.Target))); err != nil {
		return metadataJournal{}, err
	}
	if !validMetadataState(journal.Old) || !validMetadataState(journal.New) || !journal.New.Present || journal.Staging != recoveryMetadataExchangeName || journal.State != metadataJournalPrepared && journal.State != metadataJournalCommitted && journal.State != metadataJournalRolledBack {
		return metadataJournal{}, errors.New("metadata journal semantic state is invalid")
	}
	canonical, err := json.Marshal(journal)
	if err != nil || !bytes.Equal(data, canonical) {
		return metadataJournal{}, errors.New("metadata journal is not canonical JSON")
	}
	return journal, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("metadata journal has duplicate JSON key")
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("metadata journal JSON delimiter is invalid")
		}
	}
	return walk()
}

func validMetadataState(state metadataState) bool {
	if !state.Present {
		return state.Size == 0 && state.SHA256 == ""
	}
	if state.Size < 0 || len(state.SHA256) != 64 {
		return false
	}
	_, err := hex.DecodeString(state.SHA256)
	return err == nil
}

func (store *generationStore) verifyMetadataState(path string, state metadataState) error {
	data, err := platformReadRegularFile(path)
	if !state.Present && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !state.Present {
		return errors.New("metadata target should be absent")
	}
	got := stateForData(data)
	if got.Size != state.Size || got.SHA256 != state.SHA256 {
		return errors.New("metadata target state mismatch")
	}
	return nil
}

func (store *generationStore) rollbackMetadataWrite(opDir, target string, journal metadataJournal, rollback func() error, cause error) error {
	if err := rollback(); err != nil {
		return errors.Join(cause, fmt.Errorf("metadata rollback incomplete: %w", err))
	}
	if err := store.verifyMetadataState(target, journal.Old); err != nil {
		return errors.Join(cause, fmt.Errorf("metadata rollback verification: %w", err))
	}
	if err := platformSyncDirectory(filepath.Dir(target)); err != nil {
		return errors.Join(cause, fmt.Errorf("metadata rollback directory sync: %w", err))
	}
	journal.State = metadataJournalRolledBack
	if err := store.writeMetadataJournal(opDir, journal); err != nil {
		return errors.Join(cause, err)
	}
	if err := store.removeOperation(opDir); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func (store *generationStore) removeOperation(opDir string) error {
	entries, err := os.ReadDir(opDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != recoveryMetadataJournalName && entry.Name() != recoveryMetadataJournalName+".next" && entry.Name() != recoveryMetadataOldName && entry.Name() != recoveryMetadataNewName && entry.Name() != "publish-state" && entry.Name() != recoveryMetadataExchangeName {
			return fmt.Errorf("unknown recovery operation object: %s", entry.Name())
		}
		path := filepath.Join(opDir, entry.Name())
		info, err := store.ops.lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe recovery operation object: %s", entry.Name())
		}
		links, err := platformFileLinkCount(path, info)
		if err != nil || links != 1 {
			return fmt.Errorf("unsafe recovery operation links: %s", entry.Name())
		}
	}
	if err := store.ops.boundary("cleanup-before-payload-remove"); err != nil {
		return err
	}
	for _, name := range []string{recoveryMetadataOldName, recoveryMetadataNewName, "publish-state", recoveryMetadataExchangeName} {
		if err := os.Remove(filepath.Join(opDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := platformSyncDirectory(opDir); err != nil {
		return err
	}
	if err := store.ops.boundary("cleanup-after-payload-sync"); err != nil {
		return err
	}
	if err := store.ops.boundary("cleanup-before-journal-remove"); err != nil {
		return err
	}
	journalRemoved := false
	for _, name := range []string{recoveryMetadataJournalName, recoveryMetadataJournalName + ".next"} {
		if err := os.Remove(filepath.Join(opDir, name)); err == nil {
			journalRemoved = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !journalRemoved {
		return errors.New("recovery operation lost journal authority")
	}
	if err := platformSyncDirectory(opDir); err != nil {
		return err
	}
	if err := store.ops.boundary("cleanup-after-journal-sync"); err != nil {
		return err
	}
	if err := store.ops.boundary("cleanup-before-op-remove"); err != nil {
		return err
	}
	if err := os.Remove(opDir); err != nil {
		return err
	}
	if err := platformSyncDirectory(filepath.Dir(opDir)); err != nil {
		return err
	}
	return store.ops.boundary("cleanup-after-ops-sync")
}

func (store *generationStore) convergeMetadataJournals() error {
	ops := filepath.Join(store.root, recoveryMetadataDirName, recoveryMetadataOpsDirName)
	entries, err := os.ReadDir(ops)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return errors.New("unknown recovery metadata object")
		}
		opDir := filepath.Join(ops, entry.Name())
		info, err := store.ops.lstat(opDir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe recovery metadata operation")
		}
		if !validOperationID(entry.Name()) {
			return errors.New("invalid recovery operation ID")
		}
		journal, err := store.readMetadataJournal(opDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || bothJournalsMissing(opDir) {
				if err := store.retireUnauthorisedOperation(opDir); err != nil {
					return err
				}
				continue
			}
			return err
		}
		nextPath := filepath.Join(opDir, recoveryMetadataJournalName+".next")
		if _, err := os.Lstat(nextPath); err == nil {
			if err := os.Rename(nextPath, filepath.Join(opDir, recoveryMetadataJournalName)); err != nil {
				return err
			}
			if err := platformSyncDirectory(opDir); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		target := filepath.Join(store.root, filepath.FromSlash(journal.Target))
		switch journal.State {
		case metadataJournalPrepared:
			if err := store.convergePreparedMetadata(opDir, target, journal); err != nil {
				return err
			}
			journal.State = metadataJournalRolledBack
			if err := store.writeMetadataJournal(opDir, journal); err != nil {
				return err
			}
		case metadataJournalCommitted:
			if err := store.verifyMetadataState(target, journal.New); err != nil {
				return err
			}
		case metadataJournalRolledBack:
			if err := store.verifyMetadataState(target, journal.Old); err != nil {
				return err
			}
		default:
			return errors.New("unknown metadata journal state")
		}
		if err := store.removeOperation(opDir); err != nil {
			return err
		}
	}
	return nil
}

func validOperationID(id string) bool {
	parts := strings.Split(id, "-")
	if len(parts) != 2 || len(parts[1]) != 16 {
		return false
	}
	if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
		return false
	}
	_, err := hex.DecodeString(parts[1])
	return err == nil
}

func bothJournalsMissing(opDir string) bool {
	for _, name := range []string{recoveryMetadataJournalName, recoveryMetadataJournalName + ".next"} {
		if _, err := os.Lstat(filepath.Join(opDir, name)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return true
}

func (store *generationStore) retireUnauthorisedOperation(opDir string) error {
	entries, err := os.ReadDir(opDir)
	if err != nil {
		return err
	}
	allowed := map[string]bool{recoveryMetadataOldName: true, recoveryMetadataNewName: true, "publish-state": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return fmt.Errorf("unknown pre-authority object: %s", entry.Name())
		}
		if _, err := platformReadRegularFile(filepath.Join(opDir, entry.Name())); err != nil {
			return err
		}
	}
	for _, entry := range entries {
		if err := os.Remove(filepath.Join(opDir, entry.Name())); err != nil {
			return err
		}
	}
	if err := platformSyncDirectory(opDir); err != nil {
		return err
	}
	if err := os.Remove(opDir); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(opDir))
}

func (store *generationStore) convergePreparedMetadata(opDir, target string, journal metadataJournal) error {
	targetState, err := store.metadataStateAt(target)
	if err != nil {
		return err
	}
	exchange := filepath.Join(opDir, recoveryMetadataExchangeName)
	exchangeState, err := store.metadataStateAt(exchange)
	if err != nil {
		return err
	}
	old, newState := journal.Old, journal.New
	valid := func(state metadataState, choices ...metadataState) bool {
		for _, choice := range choices {
			if state == choice {
				return true
			}
		}
		return false
	}
	absent := metadataState{}
	if !valid(targetState, old, newState, absent) || !valid(exchangeState, old, newState, absent) {
		return errors.New("prepared metadata matrix is unsafe")
	}
	if targetState == old && (exchangeState == newState || exchangeState == old || exchangeState == absent) {
		return nil
	}
	if !old.Present && targetState == absent && exchangeState == absent {
		return nil
	}
	if old.Present && targetState == newState && exchangeState == old {
		if err := store.ops.boundary("rollback-exchange-before-rename"); err != nil {
			return err
		}
		if err := platformExchangeMetadata(target, exchange, newState, old); err != nil {
			return err
		}
		return store.verifyMetadataState(target, old)
	}
	if old.Present && targetState == newState && exchangeState == absent {
		return store.restoreJournalOld(opDir, target, journal)
	}
	if !old.Present && targetState == newState && exchangeState == absent {
		return platformRemoveMetadata(target, exchange, newState)
	}
	return errors.New("prepared metadata matrix conflicts")
}

func (store *generationStore) metadataStateAt(path string) (metadataState, error) {
	data, err := platformReadRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return metadataState{}, nil
	}
	if err != nil {
		return metadataState{}, err
	}
	return stateForData(data), nil
}

func (store *generationStore) restoreJournalOld(opDir, target string, journal metadataJournal) error {
	if journal.Old.Present {
		data, err := platformReadRegularFile(filepath.Join(opDir, recoveryMetadataOldName))
		if err != nil {
			return err
		}
		if stateForData(data) != journal.Old {
			return errors.New("metadata old state mismatch")
		}
		if err := platformPublishData(filepath.Join(opDir, recoveryMetadataOldName), target, 0o600); err != nil {
			return err
		}
	} else if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := store.verifyMetadataState(target, journal.Old); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(target))
}

func (store *generationStore) writeSyncedFileExclusive(path string, data []byte, mode fs.FileMode) error {
	file, err := store.ops.openFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func randomMetadataPath(directory, prefix string) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return filepath.Join(directory, "."+prefix+hex.EncodeToString(random)), nil
}

func (store *generationStore) writeSyncedFile(path string, data []byte, mode fs.FileMode) error {
	file, err := store.ops.openFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := store.ops.boundary("file-fsync"); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func (store *generationStore) syncDirectory(path string) error {
	if useAndroidPrivateStorage {
		return nil
	}
	if err := store.ops.boundary("directory-fsync"); err != nil {
		return err
	}
	directory, err := store.ops.openFile(path, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func newGenerationID() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(random)), nil
}
