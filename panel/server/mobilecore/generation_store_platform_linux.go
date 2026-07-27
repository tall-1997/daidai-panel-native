//go:build linux || android

package mobilecore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformAvailableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func platformSyncDirectory(path string) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	return unix.Fsync(fd)
}

func platformCreateRecoveryFile(path string, mode fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, mode)
}

func platformReadRegularFile(path string) ([]byte, error) {
	file, err := platformOpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("recovery file is not regular")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return nil, errors.New("recovery file has unsafe links")
	}
	return io.ReadAll(file)
}

func platformOpenRegularFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("recovery file is not regular")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		file.Close()
		return nil, errors.New("recovery file has unsafe links")
	}
	return file, nil
}

func platformPublishData(source, target string, mode fs.FileMode) error {
	file, err := os.OpenFile(source, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	publish, _, cleanup, err := platformPrepareAtomicPublish(file, nil, source, target, mode)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := publish(); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(target))
}

func platformExchangeMetadata(target, exchange string) error {
	if err := unix.Renameat2(unix.AT_FDCWD, target, unix.AT_FDCWD, exchange, unix.RENAME_EXCHANGE); err != nil {
		return err
	}
	if err := platformSyncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(exchange))
}

func platformProbeRecoveryMetadata(root string) error {
	fd, err := unix.Open(root, unix.O_TMPFILE|unix.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("recovery metadata O_TMPFILE unavailable: %w", err)
	}
	file := os.NewFile(uintptr(fd), root)
	defer file.Close()
	missing := filepath.Join(root, ".recovery-probe-missing", "slot")
	if err := unix.Linkat(int(file.Fd()), "", unix.AT_FDCWD, missing, unix.AT_EMPTY_PATH); !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("linkat capability probe: %w", err)
	}
	if err := unix.Renameat2(unix.AT_FDCWD, missing, unix.AT_FDCWD, missing+"-target", unix.RENAME_NOREPLACE); !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("renameat2 capability probe: %w", err)
	}
	return platformSyncDirectory(root)
}

func platformRemoveMetadata(target, quarantine string, expected metadataState) error {
	file, err := platformOpenRegularFile(target)
	if err != nil {
		return err
	}
	defer file.Close()
	identity, err := file.Stat()
	if err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, target, unix.AT_FDCWD, quarantine, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	actual, err := os.Lstat(quarantine)
	if err != nil || !os.SameFile(identity, actual) {
		_ = os.Rename(quarantine, target)
		return errors.New("metadata removal identity changed")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = os.Rename(quarantine, target)
		return err
	}
	data, err := io.ReadAll(file)
	if err != nil || stateForData(data) != expected {
		_ = os.Rename(quarantine, target)
		return errors.New("metadata removal state changed")
	}
	if err := os.Remove(quarantine); err != nil {
		return err
	}
	if err := platformSyncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(quarantine))
}

func platformFileLinkCount(_ string, info fs.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("metadata temporary stat type is unavailable")
	}
	return uint64(stat.Nlink), nil
}

func openMetadataTarget(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("atomic metadata target is not a regular file")
	}
	return file, nil
}

func platformPrepareAtomicPublish(file, oldFile *os.File, temporary, target string, mode fs.FileMode) (func() error, func() error, func() error, error) {
	directory := filepath.Dir(target)
	operationDirectory := filepath.Dir(temporary)
	publishFile, err := copyToUnnamedFile(file, operationDirectory, mode)
	if err != nil {
		return nil, nil, nil, err
	}
	var recoveryFile *os.File
	if oldFile != nil {
		recoveryFile, err = copyToUnnamedFile(oldFile, directory, mode)
		if err != nil {
			publishFile.Close()
			return nil, nil, nil, err
		}
	}
	cleanup := func() error {
		err := publishFile.Close()
		if recoveryFile != nil {
			err = errors.Join(err, recoveryFile.Close())
		}
		return err
	}
	publish := func() error {
		staging := filepath.Join(operationDirectory, recoveryMetadataExchangeName)
		err := linkOpenFileAt(publishFile, staging)
		if err != nil {
			return errors.Join(err, restorePinnedMetadata(recoveryFile, target, directory))
		}
		defer os.Remove(staging)
		for attempt := 0; attempt < 4; attempt++ {
			err = unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
			if err == nil {
				return verifyPublishedFile(publishFile, target)
			}
			if !errors.Is(err, unix.EEXIST) {
				return errors.Join(err, restorePinnedMetadata(recoveryFile, target, directory))
			}
			err = unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, target, unix.RENAME_EXCHANGE)
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			if err != nil {
				return errors.Join(err, restorePinnedMetadata(recoveryFile, target, directory))
			}
			if err := verifyPublishedFile(publishFile, target); err != nil {
				return errors.Join(err, restorePinnedMetadata(recoveryFile, target, directory))
			}
			return nil
		}
		return errors.Join(errors.New("atomic metadata publish did not converge"), restorePinnedMetadata(recoveryFile, target, directory))
	}
	rollback := func() error {
		return restorePinnedMetadata(recoveryFile, target, operationDirectory)
	}
	return publish, rollback, cleanup, nil
}

func linkOpenFileAt(file *os.File, name string) error {
	err := unix.Linkat(int(file.Fd()), "", unix.AT_FDCWD, name, unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
		err = unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", file.Fd()), unix.AT_FDCWD, name, unix.AT_SYMLINK_FOLLOW)
	}
	return err
}

func copyToUnnamedFile(source *os.File, directory string, mode fs.FileMode) (*os.File, error) {
	fd, err := unix.Open(directory, unix.O_TMPFILE|unix.O_RDWR, uint32(mode.Perm()))
	if err != nil {
		return nil, fmt.Errorf("create unnamed metadata recovery file: %w", err)
	}
	output := os.NewFile(uintptr(fd), directory)
	if _, err = source.Seek(0, io.SeekStart); err == nil {
		_, err = io.Copy(output, source)
	}
	if err == nil {
		err = output.Sync()
	}
	if err != nil {
		output.Close()
		return nil, err
	}
	return output, nil
}

func linkOpenFile(file *os.File, directory, prefix string) (string, error) {
	for attempt := 0; attempt < 16; attempt++ {
		name, err := randomMetadataPath(directory, prefix)
		if err != nil {
			return "", err
		}
		err = unix.Linkat(int(file.Fd()), "", unix.AT_FDCWD, name, unix.AT_EMPTY_PATH)
		if errors.Is(err, unix.EPERM) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOENT) {
			err = unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", file.Fd()), unix.AT_FDCWD, name, unix.AT_SYMLINK_FOLLOW)
		}
		if errors.Is(err, unix.EEXIST) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("link open metadata file: %w", err)
		}
		return name, nil
	}
	return "", errors.New("link open metadata file: exhausted attempts")
}

func verifyPublishedFile(file *os.File, target string) error {
	expected, err := file.Stat()
	if err != nil {
		return err
	}
	actual, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return errors.New("published metadata identity mismatch")
	}
	return nil
}

func restorePinnedMetadata(recovery *os.File, target, directory string) error {
	if recovery == nil {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	path, err := linkOpenFile(recovery, directory, filepath.Base(target)+".recover-")
	if err != nil {
		return err
	}
	defer os.Remove(path)
	return os.Rename(path, target)
}
