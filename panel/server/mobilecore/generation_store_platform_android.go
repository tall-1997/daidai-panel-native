//go:build android

package mobilecore

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func platformAvailableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

// Android app-private storage relies on file fsync and same-directory rename.
func platformSyncDirectory(string) error { return nil }

func platformCreateRecoveryFile(path string, mode fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
}

func platformReadRegularFile(path string) ([]byte, error) {
	file, err := platformOpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func platformOpenRegularFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("recovery file is not regular")
	}
	return os.Open(path)
}

func platformPublishData(source, target string, mode fs.FileMode) error {
	data, err := platformReadRegularFile(source)
	if err != nil {
		return err
	}
	temporary, err := randomMetadataPath(filepath.Dir(target), filepath.Base(target)+".android-")
	if err != nil {
		return err
	}
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	defer os.Remove(temporary)
	file, err := os.OpenFile(temporary, os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if err = file.Sync(); err == nil {
		err = file.Close()
	} else {
		_ = file.Close()
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func platformExchangeMetadata(target, exchange string, _, _ metadataState) error {
	temporary := exchange + ".swap"
	if err := os.Rename(target, temporary); err != nil {
		return err
	}
	if err := os.Rename(exchange, target); err != nil {
		_ = os.Rename(temporary, target)
		return err
	}
	return os.Rename(temporary, exchange)
}

func platformRemoveMetadata(target, quarantine string, _ metadataState) error {
	if err := os.Rename(target, quarantine); err != nil {
		return err
	}
	return os.Remove(quarantine)
}

func platformProbeRecoveryMetadata(ops string) error {
	id, err := newGenerationID()
	if err != nil {
		return err
	}
	dir := filepath.Join(ops, id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	cleanup := func() error { return cleanupProbeOperation(ops, dir) }
	if err := platformSyncDirectory(ops); err != nil {
		return errors.Join(err, cleanup())
	}
	first := filepath.Join(dir, recoveryMetadataNewName)
	second := filepath.Join(dir, "publish-state")
	target := filepath.Join(dir, recoveryMetadataOldName)
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := recoveryProbeBoundary("probe-after-first-link"); err != nil {
		return err
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := platformSyncDirectory(dir); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := os.Rename(first, target); err != nil {
		return errors.Join(err, cleanup())
	}
	x, err := os.ReadFile(target)
	if err != nil {
		return errors.Join(err, cleanup())
	}
	y, err := os.ReadFile(second)
	if err != nil {
		return errors.Join(err, cleanup())
	}
	if string(x) != "first" || string(y) != "second" {
		return errors.Join(errors.New("recovery probe content mismatch"), cleanup())
	}
	return cleanup()
}

func platformFileLinkCount(string, fs.FileInfo) (uint64, error) { return 1, nil }

func openMetadataTarget(path string) (*os.File, error) {
	file, err := platformOpenRegularFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return file, err
}

func platformPrepareAtomicPublish(_ *os.File, _ *os.File, temporary, target string, _ fs.FileMode) (func() error, func() error, func() error, error) {
	publish := func() error { return os.Rename(temporary, target) }
	return publish, func() error { return nil }, func() error { return nil }, nil
}
