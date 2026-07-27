//go:build !linux && !android && !windows

package mobilecore

import (
	"errors"
	"io/fs"
	"os"
)

func atomicOpenFlags() int                                      { return os.O_CREATE | os.O_EXCL | os.O_RDWR }
func platformAvailableBytes(string) (uint64, error)             { return ^uint64(0), nil }
func platformFileLinkCount(string, fs.FileInfo) (uint64, error) { return 1, nil }
func openMetadataTarget(path string) (*os.File, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return file, err
}
func platformCreateAtomicTemporary(path string, mode fs.FileMode) (string, *os.File, fs.FileInfo, error) {
	return "", nil, nil, errors.New("atomic metadata temporary files are unsupported on this platform")
}
func platformPrepareAtomicPublish(file, oldFile *os.File, temporary, target string, mode fs.FileMode) (func() error, func() error, func(), error) {
	return nil, nil, nil, errors.New("atomic metadata publish is unsupported on this platform")
}
