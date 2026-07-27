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

func atomicOpenFlags() int {
	return os.O_CREATE | os.O_EXCL | os.O_RDWR | unix.O_NOFOLLOW
}

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

func platformProbeRecoveryMetadata(root string) error {
	fd, err := unix.Open(root, unix.O_TMPFILE|unix.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("recovery metadata O_TMPFILE unavailable: %w", err)
	}
	file := os.NewFile(uintptr(fd), root)
	defer file.Close()
	first, err := linkOpenFile(file, root, "recovery-probe-")
	if err != nil {
		return err
	}
	defer os.Remove(first)
	second, err := randomMetadataPath(root, "recovery-probe-target-")
	if err != nil {
		return err
	}
	defer os.Remove(second)
	if err := unix.Renameat2(unix.AT_FDCWD, first, unix.AT_FDCWD, second, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return platformSyncDirectory(root)
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

func platformCreateAtomicTemporary(path string, mode fs.FileMode) (string, *os.File, fs.FileInfo, error) {
	directory := filepath.Dir(path)
	fd, err := unix.Open(directory, unix.O_TMPFILE|unix.O_RDWR, uint32(mode.Perm()))
	if err != nil {
		return "", nil, nil, fmt.Errorf("create unnamed metadata temporary: %w", err)
	}
	file := os.NewFile(uintptr(fd), directory)
	temporary, journal, err := createMetadataJournal(directory, filepath.Base(path)+".tmp-", mode)
	if err != nil {
		file.Close()
		return "", nil, nil, err
	}
	identity, err := journal.Stat()
	closeErr := journal.Close()
	if err != nil {
		file.Close()
		_ = os.Remove(temporary)
		return "", nil, nil, err
	}
	if closeErr != nil {
		file.Close()
		_ = os.Remove(temporary)
		return "", nil, nil, closeErr
	}
	return temporary, file, identity, nil
}

func createMetadataJournal(directory, prefix string, mode fs.FileMode) (string, *os.File, error) {
	for attempt := 0; attempt < 16; attempt++ {
		path, err := randomMetadataPath(directory, prefix)
		if err != nil {
			return "", nil, err
		}
		file, err := os.OpenFile(path, atomicOpenFlags(), mode)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		return path, file, nil
	}
	return "", nil, errors.New("create metadata journal: exhausted attempts")
}

func platformPrepareAtomicPublish(file, oldFile *os.File, temporary, target string, mode fs.FileMode) (func() error, func() error, func(), error) {
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
	cleanup := func() {
		_ = publishFile.Close()
		if recoveryFile != nil {
			_ = recoveryFile.Close()
		}
	}
	publish := func() error {
		staging, err := linkOpenFile(publishFile, operationDirectory, "publish-")
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
