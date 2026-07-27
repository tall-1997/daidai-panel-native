//go:build !linux && !android && !windows

package mobilecore

import (
	"errors"
	"io/fs"
	"os"
)

func platformAvailableBytes(string) (uint64, error) { return ^uint64(0), nil }
func platformSyncDirectory(string) error            { return errors.New("durable directory sync is unavailable") }
func platformCreateRecoveryFile(string, fs.FileMode) (*os.File, error) {
	return nil, errors.New("durable recovery files are unavailable")
}
func platformReadRegularFile(string) ([]byte, error) {
	return nil, errors.New("durable recovery reads are unavailable")
}
func platformOpenRegularFile(string) (*os.File, error) {
	return nil, errors.New("durable recovery handles are unavailable")
}
func platformPublishData(string, string, fs.FileMode) error {
	return errors.New("durable recovery publish is unavailable")
}
func platformExchangeMetadata(string, string) error {
	return errors.New("durable metadata exchange is unavailable")
}
func platformRemoveMetadata(string, string, metadataState) error {
	return errors.New("durable metadata removal is unavailable")
}
func platformProbeRecoveryMetadata(string) error {
	return errors.New("durable recovery metadata is unavailable")
}
func platformFileLinkCount(string, fs.FileInfo) (uint64, error) { return 1, nil }
func openMetadataTarget(path string) (*os.File, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return file, err
}
func platformPrepareAtomicPublish(file, oldFile *os.File, temporary, target string, mode fs.FileMode) (func() error, func() error, func() error, error) {
	return nil, nil, nil, errors.New("atomic metadata publish is unsupported on this platform")
}
