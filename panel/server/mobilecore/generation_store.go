package mobilecore

import (
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
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	generationsDirName      = "generations"
	activeGenerationName    = "active-generation"
	recoveryTransactionName = "recovery-transaction.json"
	generationManifestName  = "generation-manifest.json"
	recoveryPhaseBuilding   = "building"
	recoveryPhasePrepared   = "prepared"
	recoveryPhaseOldSealed  = "old-generation-sealed"
	recoveryPhaseCommitted  = "pointer-committed"
	recoveryPhaseVerified   = "verified"
	recoveryPhaseRolledBack = "rolled-back"
)

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
	walkDir        func(string, fs.WalkDirFunc) error
	remove         func(string) error
	removeAll      func(string) error
	availableBytes func(string) (uint64, error)
	boundary       func(string) error
}

func defaultFilesystemOps() filesystemOps {
	return filesystemOps{
		openFile:  os.OpenFile,
		readFile:  os.ReadFile,
		mkdirAll:  os.MkdirAll,
		rename:    os.Rename,
		stat:      os.Stat,
		walkDir:   filepath.WalkDir,
		remove:    os.Remove,
		removeAll: os.RemoveAll,
		availableBytes: func(path string) (uint64, error) {
			var stat syscall.Statfs_t
			if err := syscall.Statfs(path, &stat); err != nil {
				return 0, err
			}
			return stat.Bavail * uint64(stat.Bsize), nil
		},
		boundary: func(string) error { return nil },
	}
}

type generationStore struct {
	root string
	ops  filesystemOps
}

func newGenerationStore(root string, ops filesystemOps) *generationStore {
	return &generationStore{root: root, ops: ops}
}

func (store *generationStore) generationPath(id string) string {
	return filepath.Join(store.root, generationsDirName, id)
}

func (store *generationStore) converge() (string, error) {
	if err := store.ops.mkdirAll(filepath.Join(store.root, generationsDirName), 0o700); err != nil {
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
			if pointerErr == nil && pointerID == txn.NewGeneration && store.verifyGeneration(txn.NewGeneration) == nil {
				txn.Phase = recoveryPhaseVerified
				if err := store.writeTransaction(txn); err != nil {
					return "", err
				}
				break
			}
			if txn.OldGeneration != "" {
				if err := store.writePointer(txn.OldGeneration); err != nil {
					return "", err
				}
			}
			txn.Phase = recoveryPhaseRolledBack
			if err := store.writeTransaction(txn); err != nil {
				return "", err
			}
		case recoveryPhaseCommitted:
			if err := store.verifyGeneration(txn.NewGeneration); err != nil {
				if txn.OldGeneration == "" {
					return "", fmt.Errorf("committed generation invalid: %w", err)
				}
				if pointerErr := store.writePointer(txn.OldGeneration); pointerErr != nil {
					return "", pointerErr
				}
				txn.Phase = recoveryPhaseRolledBack
			} else {
				if err := store.writePointer(txn.NewGeneration); err != nil {
					return "", err
				}
				txn.Phase = recoveryPhaseVerified
			}
			if err := store.writeTransaction(txn); err != nil {
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
				if err := store.ops.removeAll(store.generationPath(txn.NewGeneration)); err != nil {
					return "", err
				}
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
	if err := store.verifyGeneration(id); err != nil {
		return "", err
	}
	txn.Phase = recoveryPhaseVerified
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
	active := store.generationPath(oldID)
	baseline, err := store.generationBaseline(oldID)
	if err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.sealGeneration(oldID, baseline); err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.preflightMigration(active); err != nil {
		_ = store.ops.remove(filepath.Join(store.root, recoveryTransactionName))
		return recoveryTransaction{}, err
	}
	newID, err := newGenerationID()
	if err != nil {
		return recoveryTransaction{}, err
	}
	txn := recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, OldGeneration: oldID, NewGeneration: newID}
	if err := store.writeTransaction(txn); err != nil {
		store.cleanupFailedMigration(newID)
		return recoveryTransaction{}, err
	}
	if err := store.copyDataset(active, store.generationPath(newID), false); err != nil {
		store.cleanupFailedMigration(newID)
		return recoveryTransaction{}, err
	}
	txn.Phase = recoveryPhasePrepared
	if err := store.writeTransaction(txn); err != nil {
		store.cleanupFailedMigration(newID)
		return recoveryTransaction{}, err
	}
	txn.Phase = recoveryPhaseOldSealed
	if err := store.writeTransaction(txn); err != nil {
		store.cleanupFailedMigration(newID)
		return recoveryTransaction{}, err
	}
	return txn, nil
}

func (store *generationStore) commitPointer(txn recoveryTransaction) error {
	if err := store.verifyGeneration(txn.NewGeneration); err != nil {
		return err
	}
	if err := store.writePointer(txn.NewGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseCommitted
	return store.writeTransaction(txn)
}

func (store *generationStore) verify(txn recoveryTransaction) error {
	if err := store.verifyGeneration(txn.NewGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseVerified
	if err := store.writeTransaction(txn); err != nil {
		return err
	}
	return store.pruneGenerations(txn.NewGeneration, txn.OldGeneration)
}

func (store *generationStore) rollback(txn recoveryTransaction) error {
	if txn.OldGeneration == "" {
		return errors.New("recovery transaction has no old generation")
	}
	if err := store.writePointer(txn.OldGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseRolledBack
	if err := store.writeTransaction(txn); err != nil {
		return err
	}
	return store.ops.removeAll(store.generationPath(txn.NewGeneration))
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

func (store *generationStore) validateGeneration(id string) error {
	info, err := store.ops.stat(store.generationPath(id))
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("active generation is not a directory")
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
	if err := store.ops.mkdirAll(destination, 0o700); err != nil {
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
		if flat && (first == generationsDirName || relative == activeGenerationName || relative == recoveryTransactionName) {
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
		info, err := store.ops.stat(path)
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
	if err := store.writeSyncedFile(filepath.Join(directory, generationManifestName), data, 0o600); err != nil {
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

func (store *generationStore) cleanupFailedMigration(id string) {
	_ = store.ops.removeAll(store.generationPath(id))
	_ = store.ops.remove(filepath.Join(store.root, recoveryTransactionName))
}

func (store *generationStore) pruneGenerations(activeID, previousID string) error {
	entries, err := os.ReadDir(filepath.Join(store.root, generationsDirName))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == activeID || entry.Name() == previousID {
			continue
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
	data, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	return store.writeAtomic(filepath.Join(store.root, recoveryTransactionName), data, 0o600, "transaction-rename")
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

func (store *generationStore) writeAtomic(path string, data []byte, mode fs.FileMode, renameBoundary string) error {
	temporary := path + ".tmp"
	if err := store.writeSyncedFile(temporary, data, mode); err != nil {
		return err
	}
	if err := store.ops.boundary(renameBoundary); err != nil {
		return err
	}
	if err := store.ops.rename(temporary, path); err != nil {
		return err
	}
	return store.syncDirectory(filepath.Dir(path))
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
