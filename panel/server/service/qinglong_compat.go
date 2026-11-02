package service

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"daidai-panel/config"
)

// qingLongCompatRoot 是青龙脚本硬编码的默认根目录。
// 青龙生态的脚本基本都写成 `QL_DIR=${QL_DIR:-"/ql"}`，再按 `$QL_DIR/shell`、
// `$QL_DIR/data/repo` 拼后续路径，所以这个值只能是 /ql，不能改成别的。
const qingLongCompatRoot = "/ql"

// qingLongCompatBranch 是注入给脚本的 QL_BRANCH 默认值。
// 青龙自己的默认分支曾经是 develop、现在是 master；脚本里普遍只用它拼
// `<owner>_<repo>_<branch>` 这种目录名去 find，取 master 与当前青龙一致。
const qingLongCompatBranch = "master"

// EnsureQingLongCompatLayout 在启动期把青龙的目录布局映射到面板真实目录上，
// 让那些硬编码 /ql 的青龙脚本能原样跑起来（#110）。
//
// 【为什么必须每次启动都跑，不能带「只跑一次」的幂等标记】
// Magisk 版重刷 zip 会重建整个 rootfs，/ql 连同里面的软链一起消失；
// Docker 版重建容器同理。落一个「已初始化」的标记文件反而会让重建后永远不再修复。
// 这里的每一步本身都是幂等的（目录已存在就跳过、软链指对了就跳过），重复跑没有代价。
//
// 【全程 best-effort】只读根、EACCES、异常挂载、被别的东西占了位置……
// 任何失败都只 log 一行，绝不返回 error、绝不阻塞启动。
// 把「青龙脚本跑不了」放大成「面板起不来」是不可接受的。
func EnsureQingLongCompatLayout() {
	// 只有 Linux 才有 /ql 这个对位概念。Windows 单机版没有等价物
	// （"/ql" 在 Windows 上会落到当前盘符的根目录，建出来纯属污染），直接跳过。
	if runtime.GOOS != "linux" {
		return
	}
	// 🔴 还必须确认自己跑在容器/chroot 里，不能见到 Linux 就往 / 写东西。
	// 裸机 systemd 部署（packaging/linux）默认以 root 跑、根文件系统可写，
	// 不加这道判据就会在用户宿主机根目录凭空多出 /ql 与 8 条软链，卸载面板也不会清掉；
	// 那台机器上若另装了原生青龙，还会把它没有的 /ql/scripts、/ql/log、/ql/config
	// 补成指向面板数据目录的软链，污染第三方安装。
	// 这也是 docs/script-api.md §9 对用户的承诺口径：只有 Docker / Magisk 才有。
	if !qingLongCompatApplicable() {
		return
	}
	ensureQingLongCompatLayoutAt(qingLongCompatRoot)
}

// qingLongCompatApplicable 判断当前部署形态该不该动根目录下的 /ql。
//
// 判据顺序：显式开关 > Magisk chroot > 容器运行时。都不命中就返回 false（裸机部署）。
func qingLongCompatApplicable() bool {
	// 显式开关最优先：给「我们没认出来的容器运行时」和「就是不想要这一层」两种人留出口。
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DAIDAI_QINGLONG_COMPAT"))) {
	case "1", "true", "on", "yes":
		return true
	case "0", "false", "off", "no":
		return false
	}

	// Magisk 版：面板由 Magisk/service.sh 用 ruri chroot 进一个完整 rootfs 后拉起，
	// 那份脚本会 export DAIDAI_MAGISK_SHELL_VERSION。容器内的 / 是 /data/local/daidai
	// 这类真实可写目录，建 /ql 不会碰到 Android 系统分区。
	// 这里刻意用字面量而不是 import handler 的常量：service 不能反向依赖 handler。
	if strings.TrimSpace(os.Getenv("DAIDAI_MAGISK_SHELL_VERSION")) != "" {
		return true
	}

	// Docker / Podman：标志文件 + cgroup 兜底，与 Playwright 默认目录共用同一套判据（见 playwright_env.go）。
	// 认不出来就退回「不动 /」这个安全方向。
	return runningInContainer()
}

// ensureQingLongCompatLayoutAt 是 EnsureQingLongCompatLayout 的真实实现。
// 单独拆出来只为一件事：让测试能传一个 t.TempDir() 当根，而不是真的去动机器上的 /ql。
func ensureQingLongCompatLayoutAt(root string) {
	if config.C == nil {
		return
	}
	scriptsDir := strings.TrimSpace(config.C.Data.ScriptsDir)
	logDir := strings.TrimSpace(config.C.Data.LogDir)
	dataDir := strings.TrimSpace(config.C.Data.Dir)
	if root == "" || scriptsDir == "" || logDir == "" || dataDir == "" {
		return
	}

	// 根目录建不出来（只读根 / 降权后没权限）就没什么可做的了，收工。
	// Docker 下这一步通常已经由 entrypoint.sh 以 root 身份建好，这里只是兜底。
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Printf("qinglong compat skipped, mkdir %s failed: %v", root, err)
		return
	}
	// shell 与 data 是真实目录（青龙自己也是），其余全部是软链。
	// 这两条失败不 return：/ql/scripts、/ql/log、/ql/config 那几条软链不依赖它们。
	for _, dir := range []string{filepath.Join(root, "shell"), filepath.Join(root, "data")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("qinglong compat mkdir %s failed: %v", dir, err)
		}
	}

	// 软链映射表。左边是青龙脚本会去访问的路径，右边是面板的真实目录。
	// repo 与 scripts 都指向脚本根目录，是因为订阅的检出目录名（<owner>_<repo>）
	// 与青龙 /ql/data/repo/<owner>_<repo> 逐字相同 —— 这正是兼容层能靠一条软链落地的原因。
	// /ql/scripts、/ql/log、/ql/config 是旧版青龙的布局，一并补上。
	links := []struct {
		link   string
		target string
	}{
		{filepath.Join(root, "data", "repo"), scriptsDir},
		{filepath.Join(root, "data", "scripts"), scriptsDir},
		{filepath.Join(root, "scripts"), scriptsDir},
		{filepath.Join(root, "data", "log"), logDir},
		{filepath.Join(root, "log"), logDir},
		// config.sh 就在数据目录根下，所以 config 指的是 dataDir 本身而不是它的子目录。
		{filepath.Join(root, "data", "config"), dataDir},
		{filepath.Join(root, "config"), dataDir},
		{filepath.Join(root, "data", "deps"), filepath.Join(dataDir, "deps")},
	}
	for _, item := range links {
		ensureQingLongCompatSymlink(item.link, item.target)
	}

	// 青龙脚本的第一处报错点就是 `touch "$dir_shell/env.sh"`：
	// 目录不存在时 touch 直接失败，整个脚本从这里断掉。
	// 刻意建成空文件而不是塞内容：它会被脚本 source，塞任何东西都可能改变脚本行为。
	// 已存在就一个字节都不碰 —— 用户可能自己往里写过东西。
	envPath := filepath.Join(root, "shell", "env.sh")
	if _, err := os.Lstat(envPath); os.IsNotExist(err) {
		if writeErr := os.WriteFile(envPath, nil, 0o644); writeErr != nil {
			log.Printf("qinglong compat create %s failed: %v", envPath, writeErr)
		}
	}
}

// ensureQingLongCompatSymlink 保证 linkPath 是一条指向 target 的软链。
// 三种情况：指对了就跳过；指错了删掉重建；是真实目录/文件就原样保留并记一行日志。
func ensureQingLongCompatSymlink(linkPath, target string) {
	info, err := os.Lstat(linkPath)
	switch {
	case err == nil && info.Mode()&os.ModeSymlink != 0:
		current, readErr := os.Readlink(linkPath)
		if readErr == nil && filepath.Clean(current) == filepath.Clean(target) {
			return
		}
		// 指错了：用户改过数据目录、或者上一版留下的链接。
		// 留着比没有更糟 —— 青龙脚本会顺着旧路径找到一个空目录，报错信息还完全看不出根因。
		if rmErr := os.Remove(linkPath); rmErr != nil {
			log.Printf("qinglong compat cannot replace stale symlink %s: %v", linkPath, rmErr)
			return
		}
	case err == nil:
		// 已经是真实目录或文件：可能是用户自己建的，也可能是这台机器上真装了青龙。
		// 一律不动。删掉别人的目录换成软链是不可接受的破坏，宁可兼容层这一条不生效。
		log.Printf("qinglong compat keeps existing real path, symlink skipped: %s", linkPath)
		return
	case !os.IsNotExist(err):
		log.Printf("qinglong compat stat %s failed: %v", linkPath, err)
		return
	}

	// 先把目标目录建出来，避免留下一条悬空软链 —— 悬空链上再执行 `mkdir -p` 会直接报
	// File exists，比没有软链还难查。deps 目录是懒创建的，最容易撞上这一条。
	if mkErr := os.MkdirAll(target, 0o755); mkErr != nil {
		log.Printf("qinglong compat mkdir link target %s failed: %v", target, mkErr)
	}
	if linkErr := os.Symlink(target, linkPath); linkErr != nil {
		log.Printf("qinglong compat symlink %s -> %s failed: %v", linkPath, target, linkErr)
	}
}

// resolveQingLongDir 返回注入给脚本的 QL_DIR，订阅钩子与任务执行两条链路共用同一份取值。
//
// v3.2.0 之前 subscription_hook.go 里是一段「dataDir 末段叫 data 就取父目录」的启发式，
// 在 Docker 下（dataDir=/app/Dumb-Panel）算出 /app，于是 $QL_DIR/data/repo 永远指向
// 一个不存在的目录 —— 这就是 #110 里钩子照着青龙写法拼路径必定失败的原因。
func resolveQingLongDir() string {
	dataDir := ""
	if config.C != nil {
		dataDir = strings.TrimSpace(config.C.Data.Dir)
	}

	// 兼容层建成了就用 /ql：青龙脚本里的 $QL_DIR/shell、$QL_DIR/data/repo 全按这个根拼，
	// 指到别处那些拼法就全是死路径。runtime.GOOS 这道判断不能省 ——
	// Windows 上 os.Stat("/ql") 查的是当前盘符根目录，恰好有同名文件夹就会误判。
	if runtime.GOOS == "linux" {
		if info, err := os.Stat(qingLongCompatRoot); err == nil && info.IsDir() {
			return qingLongCompatRoot
		}
	}
	// 兼容层没建成（Windows 单机版、只读根）时回落数据目录：
	// 拼出来的路径未必对得上青龙布局，但至少是一个真实存在的目录。
	return dataDir
}

// buildQingLongCompatEnv 返回注入给脚本运行时的 QL_* 变量。
//
// QL_REPO_PATH / QL_SCRIPT_PATH 等刻意直接指向面板的真实目录，而不是拼成
// `<QL_DIR>/data/repo`：有兼容层时两者本来就是同一个目录（软链），
// 没兼容层时前者仍然是个真实存在的路径，后者则是死路径。一种形态、两处都能用。
func buildQingLongCompatEnv() map[string]string {
	if config.C == nil {
		return nil
	}

	scriptsDir := strings.TrimSpace(config.C.Data.ScriptsDir)
	logDir := strings.TrimSpace(config.C.Data.LogDir)
	dataDir := strings.TrimSpace(config.C.Data.Dir)

	env := map[string]string{}
	if qlDir := resolveQingLongDir(); qlDir != "" {
		env["QL_DIR"] = qlDir
	}
	env["QL_BRANCH"] = qingLongCompatBranch
	if scriptsDir != "" {
		env["QL_REPO_PATH"] = scriptsDir
		env["QL_SCRIPT_PATH"] = scriptsDir
	}
	if logDir != "" {
		env["QL_LOG_PATH"] = logDir
	}
	if dataDir != "" {
		env["QL_CONFIG_PATH"] = dataDir
	}

	// 🔴 这三个小写变量不是可有可无的补充，是 #110 的**第二处报错**必须的修法。
	//
	// 青龙的 shell/share.sh 定义了一批 dir_* 变量，脚本在青龙之外运行时普遍写成
	// `dir_repo=${dir_repo:-"$QL_DIR/data/repo"}` 来兜底（BiliBiliToolPro 的
	// bili_task_base.sh 就是这么写的），所以我们只要把它们预先 export 成**真实路径**，
	// 脚本就会直接采用，不再去拼那条软链路径。
	//
	// 为什么非这么做不可：软链能兜住 cd / ls / test -d / source / 重定向，
	// 唯独兜不住 `find`。GNU find 默认是 -P，**命令行参数本身是软链时不会下钻**，
	// 于是 `find "$dir_repo" -type d -name "<owner>_<repo>"` 一条都找不到，
	// 而这正是青龙脚本定位自己仓库目录的标准写法。（实测：
	//   find <软链>   -> 空
	//   find <软链>/  -> 命中     ← 但我们控制不了脚本怎么写
	//   find <真实目录> -> 命中）
	//
	// 只 export 这三个「会被 find 搜的」，不 export dir_log / dir_config / dir_dep：
	// 后者在脚本里基本只用于 cd / 重定向 / source，软链已经够用，
	// 而 dir_* 是很通用的小写名，往每个任务环境里多塞一个就多一分撞名的可能。
	if scriptsDir != "" {
		env["dir_repo"] = scriptsDir
		env["dir_scripts"] = scriptsDir
		// 青龙的 raw 是「单文件订阅」的落点，面板把它和仓库订阅一起放在脚本根目录下。
		env["dir_raw"] = scriptsDir
	}
	return env
}
