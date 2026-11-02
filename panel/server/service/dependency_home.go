package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"daidai-panel/config"
)

// EffectiveHomeDir 返回一个确定可写的 HOME 目录。
//
// 背景（v3.0.7）：容器配了 PUID/PGID 降权之后，面板进程拿到的 HOME 可能根本不存在
// —— su-exec 会按 /etc/passwd 把 HOME 覆写成 /home/daidai，而那个目录从来没被创建过
// （建用户用的是 adduser -D -H / useradd -M，都是「不创建家目录」）；
// 也可能存在但属于 root —— gosu 只在 HOME 为空时才设置，于是保持 Docker 注入的 /root。
// 两种情况下 npm 的 cache（$HOME/.npm）、$HOME/.npmrc 与 pip 的 pip.conf 都写不进去，
// 表现就是「面板能开、一装依赖就 EACCES」。
//
// 容器侧 entrypoint.sh 已经把 HOME 建好并 chown 了，这里是与之对称的代码侧兜底
// （跟 entrypoint 的 unset PIP_* 与 SanitizePipEnv 是同一种双保险写法）。
// 代码侧这一层额外覆盖两种 entrypoint 够不着的部署：Magisk 模块版，
// 以及裸机 systemd 部署 —— systemd 的 User= 同样按 passwd 决定 HOME，
// 家目录不存在时坏法一模一样。
func EffectiveHomeDir() string {
	home := strings.TrimSpace(os.Getenv("HOME"))

	// Windows 上 npm / pip 认的是 USERPROFILE / APPDATA，HOME 只有 git 之类少数工具会读。
	// 在这里改写 HOME 只会引入新的意外，直接原样返回。
	if runtime.GOOS == "windows" {
		return home
	}
	return resolveWritableHome(home, currentDataDir())
}

// resolveWritableHome 是上面那个函数的纯逻辑部分：不读环境变量、不看操作系统，
// 这样它在任何平台上都能被直接测到（否则整块逻辑在 Windows 开发机上没有任何覆盖）。
//
// 判据是「能不能真的写进去」而不是「目录存不存在」：只读挂载、属主不符、
// NFS/CIFS 的 root squash 都只有真写一次才看得出来。
// HOME 本来就可写的部署走原路径，零影响。
func resolveWritableHome(home, dataDir string) string {
	// HOME 为空时【不】重定向，原样返回。
	//
	// 这条守卫是刻意的：HOME 没设置时 npm / pip 会退回 getpwuid 去解析家目录，
	// 而那个结果通常是对的 —— 典型场景是裸机 systemd 部署（没写 User=、也没写
	// Environment=HOME），npm 解析到 /root，用户手写的 /root/.npmrc（可能带私有源
	// 与 _authToken）与 /root/.npm 缓存一直在正常工作。
	// 这里要是也回落到数据目录，就会把这些配置和缓存整个丢掉，
	// 表现成「升级之后私有包突然 401 / 404」，而且没有任何日志。
	//
	// 本次要修的那个 bug 里 HOME 一定是有值的（Docker 会注入 HOME=/root，
	// su-exec 会按 passwd 覆写成 /home/daidai），所以收窄到「有值但不可写」
	// 既能覆盖问题，又不会波及上面那类部署。
	if home == "" {
		return home
	}
	if isWritableDir(home) {
		return home
	}
	if dataDir == "" {
		// 数据目录还没初始化，无处可回落。原样返回比乱建目录安全。
		return home
	}

	// 与 entrypoint.sh 里的 DAIDAI_HOME 指向同一个位置，两层修复落在同一处，
	// 不会出现「entrypoint 建在 A、代码写到 B」的割裂。
	// docker_entrypoint_assets_test.go 里有一条断言专门锁这个契约。
	fallback := filepath.Join(dataDir, ".home")
	if err := os.MkdirAll(fallback, 0o700); err != nil {
		return home
	}
	if !isWritableDir(fallback) {
		return home
	}
	return fallback
}

// homeRedirectEnv 在 HOME 不可写时，把可写 HOME 钉进要交给 npm / pip 的环境变量里。
// HOME 本来可写就原样返回，历史部署零影响。
func homeRedirectEnv(env []string) []string {
	if runtime.GOOS == "windows" {
		return env
	}
	return redirectHomeEnv(env, EffectiveHomeDir(), strings.TrimSpace(os.Getenv("HOME")))
}

// WritableHomeEnv 给「不装依赖、但同样要跑 npm / pip 的」命令用（依赖列表）。
// npm 只要启动就会初始化 $HOME/.npm 下的 cache，HOME 不可写时连 npm list 都会失败 ——
// 表现成「装得上、却看不到」，而这两件事看起来毫无关联。
// （导出依赖从 v3.3.3 起按记录原样输出，不再跑 npm / pip，#150。）
// 与安装路径共用同一份 HOME 判定，避免两条路解析出不同的 HOME。
func WritableHomeEnv(base []string) []string {
	return homeRedirectEnv(base)
}

// redirectHomeEnv 同样是纯逻辑部分，便于在任何平台上直接测。
//
// 一并钉住 npm_config_cache：npm 的 cache 默认就是 $HOME/.npm，但用户可能在面板的
// 环境变量里预设过 npm_config_cache 指向别处，那种情况下只改 HOME 不够。
// 这两个变量交给 pip 是惰性的（pip 不认 npm_config_cache），所以两条路共用一个函数。
func redirectHomeEnv(env []string, home, currentHome string) []string {
	if home == "" || home == currentHome {
		return env
	}

	cleaned := make([]string, 0, len(env)+2)
	for _, entry := range env {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx > 0 {
			key = entry[:idx]
		}
		switch strings.ToUpper(key) {
		case "HOME", "NPM_CONFIG_CACHE":
			continue
		}
		cleaned = append(cleaned, entry)
	}
	cleaned = append(cleaned, "HOME="+home)
	cleaned = append(cleaned, "npm_config_cache="+filepath.Join(home, ".npm"))
	return cleaned
}

func currentDataDir() string {
	if config.C == nil {
		return ""
	}
	return strings.TrimSpace(config.C.Data.Dir)
}

// isWritableDir 真写一个探测文件再删掉。只 Stat 判断存在性是不够的：
// 属主不符、只读挂载、SELinux 拒绝都要写一次才暴露。
func isWritableDir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	probe, err := os.CreateTemp(dir, ".daidai-write-probe-")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}
