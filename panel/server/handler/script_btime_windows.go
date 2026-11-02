//go:build windows

package handler

import (
	"os"
	"syscall"
)

// fileBirthTime 读取文件创建时间（Unix 秒），Windows 直接取文件属性里的 CreationTime。
func fileBirthTime(_ string, info os.FileInfo) (float64, bool) {
	if info == nil {
		return 0, false
	}
	attr, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return 0, false
	}
	return float64(attr.CreationTime.Nanoseconds() / 1e9), true
}
