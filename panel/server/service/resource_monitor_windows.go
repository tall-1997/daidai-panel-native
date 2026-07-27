//go:build windows

package service

import (
	"math"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

var (
	kernel32DLL                = syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusExProc   = kernel32DLL.NewProc("GlobalMemoryStatusEx")
	getDiskFreeSpaceExWProc    = kernel32DLL.NewProc("GetDiskFreeSpaceExW")
)

// fillWindowsResourceInfo 为 Windows 运行态补齐资源信息，避免仪表板和系统设置长期显示 0。
// 这里优先补内存、磁盘和基础 CPU；网络速度暂时保持 0，不再误报总容量为 0。
func fillWindowsResourceInfo(info *ResourceInfo) {
	if info == nil {
		return
	}

	totalMemory, usedMemory, freeMemory, memoryUsage := getWindowsMemory()
	info.MemoryTotal = totalMemory
	info.MemoryUsed = usedMemory
	info.MemoryFree = freeMemory
	info.MemoryUsage = memoryUsage

	diskRoot := resolveWindowsDiskRoot(info.DataDir)
	totalDisk, usedDisk, freeDisk, diskUsage := getWindowsDisk(diskRoot)
	info.DiskTotal = totalDisk
	info.DiskUsed = usedDisk
	info.DiskFree = freeDisk
	info.DiskUsage = diskUsage

	info.CPUUsage = getWindowsCPUUsage()
}

func getWindowsMemory() (total, used, free uint64, usage float64) {
	status := memoryStatusEx{}
	status.dwLength = uint32(unsafe.Sizeof(status))

	ret, _, _ := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return 0, 0, 0, 0
	}

	total = status.ullTotalPhys
	free = status.ullAvailPhys
	if total >= free {
		used = total - free
	}
	if total > 0 {
		usage = math.Round(float64(used)/float64(total)*10000) / 100
	}

	return total, used, free, usage
}

func getWindowsDisk(root string) (total, used, free uint64, usage float64) {
	root = filepath.Clean(root)
	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, 0, 0
	}

	var freeBytesAvailable uint64
	var totalNumberOfBytes uint64
	var totalNumberOfFreeBytes uint64

	ret, _, _ := getDiskFreeSpaceExWProc.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if ret == 0 {
		return 0, 0, 0, 0
	}

	total = totalNumberOfBytes
	free = totalNumberOfFreeBytes
	if total >= free {
		used = total - free
	}
	if total > 0 {
		usage = math.Round(float64(used)/float64(total)*10000) / 100
	}

	return total, used, free, usage
}

func resolveWindowsDiskRoot(dataDir string) string {
	dataDir = stringsTrimSpaceWindows(dataDir)
	if dataDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			dataDir = cwd
		}
	}

	volume := filepath.VolumeName(dataDir)
	if volume == "" {
		return `C:\`
	}
	return volume + `\`
}

func getWindowsCPUUsage() float64 {
	start := time.Now()
	if start.IsZero() {
		return 0
	}

	// 当前阶段先返回 0，不再把缺失的内存/磁盘也一起误报。
	// 如果后续要补性能计数器，可在这里继续细化。
	return 0
}

func stringsTrimSpaceWindows(value string) string {
	start := 0
	end := len(value)
	for start < end {
		switch value[start] {
		case ' ', '\t', '\n', '\r':
			start++
		default:
			goto trimRight
		}
	}

trimRight:
	for end > start {
		switch value[end-1] {
		case ' ', '\t', '\n', '\r':
			end--
		default:
			return value[start:end]
		}
	}

	return value[start:end]
}
