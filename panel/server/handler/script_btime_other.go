//go:build !linux && !windows

package handler

import "os"

// fileBirthTime 其它平台不提供创建时间，脚本树里就不带 btime 键。
func fileBirthTime(_ string, _ os.FileInfo) (float64, bool) {
	return 0, false
}
