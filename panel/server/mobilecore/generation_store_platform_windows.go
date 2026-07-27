//go:build windows

package mobilecore

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func platformAvailableBytes(path string) (uint64, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(pathPointer, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}

func platformSyncDirectory(string) error {
	return errors.New("durable directory sync is unavailable on Windows")
}

func platformCreateRecoveryFile(path string, mode fs.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
}

func platformReadRegularFile(path string) ([]byte, error) { return os.ReadFile(path) }
func platformPublishData(string, string, fs.FileMode) error {
	return errors.New("durable recovery publish is unavailable on Windows")
}

func platformProbeRecoveryMetadata(string) error {
	return errors.New("durable recovery metadata is unavailable on Windows")
}

func platformFileLinkCount(path string, _ fs.FileInfo) (uint64, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(pathPointer, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 0, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, err
	}
	if err := windows.CloseHandle(handle); err != nil {
		return 0, err
	}
	return uint64(info.NumberOfLinks), nil
}

func openMetadataTarget(path string) (*os.File, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(pointer, windows.GENERIC_READ|windows.DELETE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("atomic metadata target is not a regular file")
	}
	return file, nil
}

func platformPrepareAtomicPublish(file, oldFile *os.File, temporary, target string, mode fs.FileMode) (func() error, func() error, func() error, error) {
	directoryPointer, err := windows.UTF16PtrFromString(filepath.Dir(target))
	if err != nil {
		return nil, nil, nil, err
	}
	directoryHandle, err := windows.CreateFile(directoryPointer, windows.FILE_LIST_DIRECTORY, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	renameHandle := func(handle windows.Handle) error {
		name, err := windows.UTF16FromString(filepath.Base(target))
		if err != nil {
			return err
		}
		nameLength := (len(name) - 1) * 2
		var template windowsFileRenameInformation
		buffer := make([]byte, int(unsafe.Offsetof(template.FileName))+nameLength)
		information := (*windowsFileRenameInformation)(unsafe.Pointer(&buffer[0]))
		information.ReplaceIfExists = windows.FILE_RENAME_REPLACE_IF_EXISTS | windows.FILE_RENAME_POSIX_SEMANTICS
		information.RootDirectory = directoryHandle
		information.FileNameLength = uint32(nameLength)
		copy((*[windows.MAX_LONG_PATH]uint16)(unsafe.Pointer(&information.FileName[0]))[:nameLength/2:nameLength/2], name)
		var status windows.IO_STATUS_BLOCK
		return windows.NtSetInformationFile(handle, &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
	}
	publish := func() error { return renameHandle(windows.Handle(file.Fd())) }
	rollback := func() error {
		if oldFile == nil {
			if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
		return renameHandle(windows.Handle(oldFile.Fd()))
	}
	return publish, rollback, func() error { return windows.CloseHandle(directoryHandle) }, nil
}

type windowsFileRenameInformation struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}
