//go:build !windows

package service

// fillWindowsResourceInfo 是非 Windows 平台的空实现。
// 这里保留同名函数，目的是让 resource_monitor.go 里的共享调用点
// 在 Linux / macOS / 其它平台交叉编译时也能正常通过编译。
// 真正的 Windows 资源采集逻辑只在 resource_monitor_windows.go 中生效。
func fillWindowsResourceInfo(info *ResourceInfo) {
	// 非 Windows 平台不需要补 Windows 资源信息，这里直接留空即可。
}
