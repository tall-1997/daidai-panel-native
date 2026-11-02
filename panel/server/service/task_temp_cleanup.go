package service

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// leakedTaskTempPatterns 是执行任务时在系统临时目录里建的几类临时文件。正常路径下用完即删；
// 面板被 SIGKILL、os.Exit、关停总兜底强退，或者任务卡在读输出里（逃出进程组的孙进程攥着管道）
// 走不到清理时会留下来。它们装着任务的全部环境变量（Cookie、令牌）和 7 天有效的脚本凭据，
// docker restart 不清容器里的 /tmp，二进制 / Magisk 要到重启机器才清，所以启动时要清掉。
//
// 前缀与形状必须与创建处一字不差（创建处见每条后面的注释），
// TestLeakedTaskTempPatternsMatchCreators 用真实的创建函数守着这一点。
var leakedTaskTempPatterns = []struct {
	prefix string
	suffix string
	dir    bool // true：只认目录；false：只认普通文件
}{
	{prefix: "daidai-runtime-", dir: true},              // runtime_exec.go：writeManagedRuntimeEnvFile / writeManagedRuntimeShellEnvFile
	{prefix: "daidai-hook-env-", dir: true},             // task_hook_env.go：newHookEnvCapture（前置钩子的环境快照）
	{prefix: "daidai-log-", suffix: ".log", dir: false}, // tiny_log.go：NewTinyLog（实时日志的落盘文件）
}

// CleanupLeakedTaskTempEntriesOnStartup 在面板启动、任务调度器还没起来时，清掉上次没来得及删的任务临时文件。
// 位置与创建处相同：os.TempDir()（Unix 上遵从 TMPDIR）。只删上面几类前缀、并且形状对得上的条目
// （该是目录的必须是目录、该是日志文件的必须是普通文件），软链与别的程序的文件一律不碰。
//
// 只能由 main 在启动早期调用，测试里不要调：同一台机器上并行跑的其它测试包也在系统临时目录里建这些文件。
func CleanupLeakedTaskTempEntriesOnStartup() {
	dir := os.TempDir()
	if removed := cleanupLeakedTaskTempEntries(dir); removed > 0 {
		log.Printf("removed %d leftover task temp entries from %s", removed, dir)
	}
}

func cleanupLeakedTaskTempEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	removed := 0
	for _, entry := range entries {
		if !isLeakedTaskTempEntry(entry) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			log.Printf("remove leftover task temp entry %s failed: %v", entry.Name(), err)
			continue
		}
		removed++
	}
	return removed
}

func isLeakedTaskTempEntry(entry os.DirEntry) bool {
	name := entry.Name()
	// ReadDir 给的是条目自身的类型（不跟随软链）：软链既不是目录也不是普通文件，下面两种判定都不会命中。
	mode := entry.Type()
	for _, pattern := range leakedTaskTempPatterns {
		if !strings.HasPrefix(name, pattern.prefix) || !strings.HasSuffix(name, pattern.suffix) {
			continue
		}
		if pattern.dir {
			return mode.IsDir()
		}
		return mode.IsRegular()
	}
	return false
}
