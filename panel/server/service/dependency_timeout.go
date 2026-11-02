package service

import (
	"time"

	"daidai-panel/model"
)

// DefaultDependencyOperationTimeout 是 dependency_install_timeout_minutes 读不出来或越界时的兜底值，
// 与该配置项的注册默认值保持一致（dependency_timeout_test.go 核对）。
const DefaultDependencyOperationTimeout = 20 * time.Minute

// DependencyOperationTimeout 读取用户配置的依赖操作超时：依赖安装 / 卸载与启动期的 npm rebuild 共用
// （需要现场编译大包的用户通常已经把它调大了）。配置项注册在 model 层并带 5-720 分钟的区间校验，
// 这里只对「数据库里存着历史越界值」这一种情况再兜一次底，避免非法值把超时变成 0（等于立刻杀进程）。
func DependencyOperationTimeout() time.Duration {
	minutes := model.GetRegisteredConfigInt("dependency_install_timeout_minutes")
	if minutes < 5 || minutes > 720 {
		return DefaultDependencyOperationTimeout
	}
	return time.Duration(minutes) * time.Minute
}
