//go:build linux || android

package mobilecore

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

func platformExchangeMetadata(target, exchange string, targetState, exchangeState metadataState) error {
	targetFile, err := platformOpenRegularFile(target)
	if err != nil {
		return err
	}
	defer targetFile.Close()
	exchangeFile, err := platformOpenRegularFile(exchange)
	if err != nil {
		return err
	}
	defer exchangeFile.Close()
	targetIdentity, _ := targetFile.Stat()
	exchangeIdentity, _ := exchangeFile.Stat()
	if data, err := io.ReadAll(targetFile); err != nil || stateForData(data) != targetState {
		return errors.New("target changed before exchange")
	}
	if data, err := io.ReadAll(exchangeFile); err != nil || stateForData(data) != exchangeState {
		return errors.New("exchange changed before rename")
	}
	currentTarget, err := os.Lstat(target)
	if err != nil || !os.SameFile(targetIdentity, currentTarget) {
		return errors.New("target path identity changed")
	}
	currentExchange, err := os.Lstat(exchange)
	if err != nil || !os.SameFile(exchangeIdentity, currentExchange) {
		return errors.New("exchange path identity changed")
	}
	if err := unix.Renameat2(unix.AT_FDCWD, target, unix.AT_FDCWD, exchange, unix.RENAME_EXCHANGE); err != nil {
		return err
	}
	newTarget, err := os.Lstat(target)
	if err != nil || !os.SameFile(exchangeIdentity, newTarget) {
		return errors.New("target identity mismatch after exchange")
	}
	newExchange, err := os.Lstat(exchange)
	if err != nil || !os.SameFile(targetIdentity, newExchange) {
		return errors.New("exchange identity mismatch after exchange")
	}
	if err := platformSyncDirectory(filepath.Dir(target)); err != nil {
		return err
	}
	return platformSyncDirectory(filepath.Dir(exchange))
}

func platformProbeRecoveryMetadata(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".owner") {
			continue
		}
		if !strings.HasPrefix(entry.Name(), recoveryProbePrefix) {
			continue
		}
		id := strings.TrimPrefix(entry.Name(), recoveryProbePrefix)
		if !validOperationID(id) {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); errors.Is(err, unix.EWOULDBLOCK) {
			unix.Close(fd)
			continue
		} else if err != nil {
			unix.Close(fd)
			return err
		}
		marker, err := platformReadRegularFile(filepath.Join(dir, recoveryProbeMarkerName))
		if errors.Is(err, os.ErrNotExist) {
			marker, err = platformReadRegularFile(dir + ".owner")
		}
		if err != nil || !validProbeMarker(marker, id) {
			unix.Close(fd)
			return errors.New("invalid recovery probe ownership")
		}
		if err := cleanupProbeDirectory(root, dir); err != nil {
			unix.Close(fd)
			return err
		}
		unix.Close(fd)
	}
	entries, err = os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), recoveryProbePrefix) || !strings.HasSuffix(entry.Name(), ".owner") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Name(), recoveryProbePrefix), ".owner")
		marker, err := platformReadRegularFile(filepath.Join(root, entry.Name()))
		if err != nil || !validProbeMarker(marker, id) {
			return errors.New("invalid retired recovery probe ownership")
		}
		if _, err := os.Stat(filepath.Join(root, strings.TrimSuffix(entry.Name(), ".owner"))); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
		if err := platformSyncDirectory(root); err != nil {
			return err
		}
	}
	id, err := newGenerationID()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, recoveryProbePrefix+id)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errors.Join(err, os.Remove(dir))
	}
	defer unix.Close(fd)
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return errors.Join(err, os.Remove(dir))
	}
	preCleanup := func() error { err := os.Remove(dir); return errors.Join(err, platformSyncDirectory(root)) }
	if err := platformSyncDirectory(root); err != nil {
		return errors.Join(err, preCleanup())
	}
	marker, _ := probeMarkerData(id)
	markerFile, err := os.OpenFile(filepath.Join(dir, recoveryProbeMarkerName), os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.Join(err, preCleanup())
	}
	if _, err = markerFile.Write(marker); err == nil {
		err = markerFile.Sync()
	}
	closeErr := markerFile.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return errors.Join(err, os.Remove(filepath.Join(dir, recoveryProbeMarkerName)), preCleanup())
	}
	cleanup := func() error { return cleanupProbeDirectory(root, dir) }
	if err := platformSyncDirectory(dir); err != nil {
		return errors.Join(err, cleanup())
	}
	newTmp := func(content string) (*os.File, error) {
		n, err := unix.Open(root, unix.O_TMPFILE|unix.O_RDWR, 0o600)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(n), root)
		if _, err = f.Write([]byte(content)); err == nil {
			err = f.Sync()
		}
		if err != nil {
			f.Close()
			return nil, err
		}
		return f, nil
	}
	a, err := newTmp("first")
	if err != nil {
		return errors.Join(err, cleanup())
	}
	defer a.Close()
	b, err := newTmp("second")
	if err != nil {
		return errors.Join(err, cleanup())
	}
	defer b.Close()
	first, second, target := filepath.Join(dir, "first"), filepath.Join(dir, "second"), filepath.Join(dir, "target")
	if err := linkOpenFileAt(a, first); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := linkOpenFileAt(b, second); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := platformSyncDirectory(dir); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := unix.Renameat2(unix.AT_FDCWD, first, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := unix.Renameat2(unix.AT_FDCWD, target, unix.AT_FDCWD, second, unix.RENAME_EXCHANGE); err != nil {
		return errors.Join(err, cleanup())
	}
	if err := platformSyncDirectory(dir); err != nil {
		return errors.Join(err, cleanup())
	}
	x, _ := os.ReadFile(target)
	y, _ := os.ReadFile(second)
	if string(x) != "second" || string(y) != "first" {
		return errors.Join(errors.New("recovery probe content mismatch"), cleanup())
	}
	return cleanup()
}

func cleanupProbeDirectory(root, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	allowed := map[string]bool{recoveryProbeMarkerName: true, "first": true, "second": true, "target": true}
	for _, e := range entries {
		if !allowed[e.Name()] {
			return errors.New("unknown recovery probe object")
		}
		info, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("unsafe recovery probe object")
		}
	}
	var result error
	for _, name := range []string{"first", "second", "target"} {
		if err := recoveryProbeBoundary("probe-remove-" + name); err != nil {
			return errors.Join(err, result)
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
		result = errors.Join(result, platformSyncDirectory(dir))
	}
	if result != nil {
		return result
	}
	marker := filepath.Join(dir, recoveryProbeMarkerName)
	owner := dir + ".owner"
	if _, err := os.Stat(owner); errors.Is(err, os.ErrNotExist) {
		if err := recoveryProbeBoundary("probe-remove-" + recoveryProbeMarkerName); err != nil {
			return err
		}
		if err := os.Rename(marker, owner); err != nil {
			return err
		}
		if err := platformSyncDirectory(root); err != nil {
			return err
		}
	}
	if err := recoveryProbeBoundary("probe-remove-directory"); err != nil {
		return err
	}
	if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := platformSyncDirectory(root); err != nil {
		return err
	}
	if err := os.Remove(owner); err != nil {
		return err
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
			return err
		}
		if err := platformSyncDirectory(operationDirectory); err != nil {
			return err
		}
		if err := recoveryPublishBoundary("publish-after-link-sync"); err != nil {
			return err
		}
		for attempt := 0; attempt < 4; attempt++ {
			err = unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
			if err == nil {
				if err := platformSyncDirectory(directory); err != nil {
					return err
				}
				if err := recoveryPublishBoundary("publish-after-target-sync"); err != nil {
					return err
				}
				if err := platformSyncDirectory(operationDirectory); err != nil {
					return err
				}
				if err := recoveryPublishBoundary("publish-after-operation-sync"); err != nil {
					return err
				}
				return verifyPublishedFile(publishFile, target)
			}
			if !errors.Is(err, unix.EEXIST) {
				return err
			}
			err = unix.Renameat2(unix.AT_FDCWD, staging, unix.AT_FDCWD, target, unix.RENAME_EXCHANGE)
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			if err != nil {
				return err
			}
			if err := platformSyncDirectory(directory); err != nil {
				return err
			}
			if err := recoveryPublishBoundary("publish-after-target-sync"); err != nil {
				return err
			}
			if err := platformSyncDirectory(operationDirectory); err != nil {
				return err
			}
			if err := recoveryPublishBoundary("publish-after-operation-sync"); err != nil {
				return err
			}
			if err := verifyPublishedFile(publishFile, target); err != nil {
				return err
			}
			return nil
		}
		return errors.New("atomic metadata publish did not converge")
	}
	rollback := func() error {
		if err := recoveryRollbackBoundary("rollback-before-restore"); err != nil {
			return err
		}
		if recoveryFile == nil {
			if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
				return nil
			} else if err != nil {
				return err
			}
			if _, err := publishFile.Seek(0, io.SeekStart); err != nil {
				return err
			}
			data, err := io.ReadAll(publishFile)
			if err != nil {
				return err
			}
			return platformRemoveMetadata(target, filepath.Join(operationDirectory, recoveryMetadataExchangeName), stateForData(data))
		}
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
