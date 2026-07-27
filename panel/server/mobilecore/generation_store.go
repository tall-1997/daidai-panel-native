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
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

type filesystemOps struct {
	openFile func(string, int, fs.FileMode) (*os.File, error)
	readFile func(string) ([]byte, error)
	mkdirAll func(string, fs.FileMode) error
	rename   func(string, string) error
	stat     func(string) (fs.FileInfo, error)
	walkDir  func(string, fs.WalkDirFunc) error
	boundary func(string) error
}

func defaultFilesystemOps() filesystemOps {
	return filesystemOps{
		openFile: os.OpenFile,
		readFile: os.ReadFile,
		mkdirAll: os.MkdirAll,
		rename:   os.Rename,
		stat:     os.Stat,
		walkDir:  filepath.WalkDir,
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
	active, err := store.activeGeneration()
	if err != nil {
		return recoveryTransaction{}, err
	}
	oldID := filepath.Base(active)
	newID, err := newGenerationID()
	if err != nil {
		return recoveryTransaction{}, err
	}
	txn := recoveryTransaction{Version: 1, Phase: recoveryPhaseBuilding, OldGeneration: oldID, NewGeneration: newID}
	if err := store.writeTransaction(txn); err != nil {
		return recoveryTransaction{}, err
	}
	if err := store.copyDataset(active, store.generationPath(newID), false); err != nil {
		return recoveryTransaction{}, err
	}
	txn.Phase = recoveryPhasePrepared
	if err := store.writeTransaction(txn); err != nil {
		return recoveryTransaction{}, err
	}
	txn.Phase = recoveryPhaseOldSealed
	if err := store.writeTransaction(txn); err != nil {
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
	return store.writeTransaction(txn)
}

func (store *generationStore) rollback(txn recoveryTransaction) error {
	if txn.OldGeneration == "" {
		return errors.New("recovery transaction has no old generation")
	}
	if err := store.writePointer(txn.OldGeneration); err != nil {
		return err
	}
	txn.Phase = recoveryPhaseRolledBack
	return store.writeTransaction(txn)
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
	data, err := store.ops.readFile(filepath.Join(store.generationPath(id), generationManifestName))
	if err != nil {
		return err
	}
	var manifest generationManifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Version != 1 {
		return errors.New("generation manifest is invalid")
	}
	return nil
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
	manifest := generationManifest{Version: 1, Files: map[string]string{}}
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
		manifest.Files[filepath.ToSlash(relative)] = digest
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
	for relative, want := range manifest.Files {
		if !validManifestPath(relative) {
			return errors.New("generation manifest path is invalid")
		}
		path := filepath.Join(directory, filepath.FromSlash(relative))
		data, err := store.ops.readFile(path)
		if err != nil {
			return err
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want {
			return fmt.Errorf("generation file checksum mismatch: %s", relative)
		}
	}
	return nil
}

func (store *generationStore) sealGeneration(id string) error {
	directory := store.generationPath(id)
	directories := []string{directory, filepath.Dir(directory)}
	manifest := generationManifest{Version: 1, Files: map[string]string{}}
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
		manifest.Files[filepath.ToSlash(relative)] = hex.EncodeToString(hash.Sum(nil))
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
