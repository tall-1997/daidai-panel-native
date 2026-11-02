package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestMagiskServiceScriptGuardsOnlineUpgrade 锁住模块版在线升级依赖的两条 shell 侧行为。
// 同样只是静态字符串断言，真正的验证必须靠真机跑
// 「装 -> 重启 -> 面板内升级 -> 再重启不回滚 -> 杀进程看是否自动拉起」整条回路。
func TestMagiskServiceScriptGuardsOnlineUpgrade(t *testing.T) {
	text := readMagiskScript(t, "service.sh")

	// 1. 防回滚：模块目录写不进去时（KernelSU 只读 /data），容器内的新版本
	//    不能在下次开机被模块里的旧版本无条件覆盖掉。
	if !strings.Contains(text, "file_needs_sync()") {
		t.Fatal("service.sh 必须保留 file_needs_sync：无条件 cp 会把面板内在线升级的结果回滚掉")
	}
	for _, snippet := range []string{
		`file_needs_sync "$MODDIR/system/bin/daidai-server" "$rootfs/usr/local/bin/daidai-server"`,
		`file_needs_sync "$MODDIR/web/index.html" "$rootfs/app/web/index.html"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("service.sh 缺少条件同步判断: %q", snippet)
		}
	}

	// 2. 存活守护：模块版没有 supervisor，面板崩了或新版本起不来就只能重启手机。
	if !strings.Contains(text, "panel_is_running()") {
		t.Fatal("service.sh 必须保留 panel_is_running 存活探测")
	}
	if !strings.Contains(text, "UPDATING_FLAG") {
		t.Fatal("service.sh 的存活守护必须避让在线升级哨兵，否则会在替换窗口里拉起旧进程")
	}
	if !strings.Contains(text, magiskUpdatingSentinelName) {
		t.Fatalf("service.sh 里的升级哨兵文件名必须与 Go 侧的 %q 一致", magiskUpdatingSentinelName)
	}
}

// TestMagiskScriptsShareStopFlagPath 锁住「手动停止」这条链路里唯一的跨文件契约：
// Go 侧写开关、四个 shell 脚本读/写/删开关，用的必须是同一个路径。
//
// 这条断言只能防住路径被改歪（改一处漏三处 = 停止功能静默失效、或卸载后重装永远起不来），
// 防不住 shell 逻辑写错，真机验证不可省。
func TestMagiskScriptsShareStopFlagPath(t *testing.T) {
	if magiskStopFlagPath != magiskPersistDir+"/"+magiskStopFlagName {
		t.Fatalf("magiskStopFlagPath 必须由 magiskPersistDir + magiskStopFlagName 拼出，当前=%q", magiskStopFlagPath)
	}
	if magiskStopFlagPath != "/data/adb/daidai-panel/stopped" {
		t.Fatalf("停止开关路径与 Magisk 脚本约定的 /data/adb/daidai-panel/stopped 不一致：%q", magiskStopFlagPath)
	}
	// 停止开关绝不能落在容器数据目录里：service.sh 每次开机都会无条件删掉那里的
	// .updating，同目录的跨重启标记迟早被同类清理误伤；rootfs 重装还会整体删除它。
	if strings.Contains(magiskStopFlagPath, "Dumb-Panel") {
		t.Fatalf("停止开关不得放在容器数据目录下：%q", magiskStopFlagPath)
	}
	// 停止能力所需的外壳版本不能超过仓库里外壳的实际版本，
	// 否则刚刷完最新 zip 的用户在面板里也会看到「需重刷 ZIP」，永远点不动这个按钮。
	if magiskStopSupportedShellVersion > currentMagiskShellVersion {
		t.Fatalf("magiskStopSupportedShellVersion(%d) 不得大于 currentMagiskShellVersion(%d)",
			magiskStopSupportedShellVersion, currentMagiskShellVersion)
	}

	// service.sh：定义开关 + 定义守护代次文件 + 早退。
	serviceSh := readMagiskScript(t, "service.sh")
	for _, snippet := range []string{
		`PERSIST_DIR=/data/adb/daidai-panel`,
		`STOP_FLAG="$PERSIST_DIR/` + magiskStopFlagName + `"`,
		`WATCHDOG_GEN_FILE="$PERSIST_DIR/` + magiskWatchdogGenName + `"`,
		`if [ -f "$STOP_FLAG" ]; then`,
		// 守护自退：停止开关 + 代次比对，缺一条 toggle 就不成立
		`watchdog_should_exit() {`,
		`watchdog_should_exit && exit 0`,
		`printf '%s\n' "$WATCHDOG_GEN" > "$WATCHDOG_GEN_FILE"`,
	} {
		if !strings.Contains(serviceSh, snippet) {
			t.Fatalf("service.sh 缺少停止开关 / 守护代次相关片段: %q", snippet)
		}
	}
	// 早退点必须排在「模块→容器条件同步」之后、「进容器拉起面板」之前。
	// 放到同步之前会导致：停止状态下刷入新模块 zip 再重启，新二进制同步不进容器，
	// 点启动跑的还是旧版本，表现成「刷了新版但版本号没变」。
	syncIdx := strings.Index(serviceSh, `file_needs_sync "$MODDIR/system/bin/daidai-server"`)
	stopIdx := strings.Index(serviceSh, `if [ -f "$STOP_FLAG" ]; then`)
	startIdx := strings.Index(serviceSh, `"$RURIMA" ruri -p -N -S -A $rootfs "$CTR_SHELL" /tmp/daidai-startup.sh`)
	if syncIdx < 0 || stopIdx < 0 || startIdx < 0 {
		t.Fatalf("service.sh 找不到定位锚点 (sync=%d stop=%d start=%d)", syncIdx, stopIdx, startIdx)
	}
	if stopIdx < syncIdx {
		t.Fatal("service.sh 的停止早退点必须放在模块→容器条件同步之后，否则停止状态下刷新模块 zip 不会同步进容器")
	}
	if stopIdx > startIdx {
		t.Fatal("service.sh 的停止早退点必须放在拉起容器之前")
	}

	// action.sh：toggle 两条路径都要在。
	actionSh := readMagiskScript(t, "action.sh")
	for _, snippet := range []string{
		`STOP_FLAG="$PERSIST_DIR/` + magiskStopFlagName + `"`,
		// 停：写开关
		`> "$STOP_FLAG"`,
		// 启：删开关 + 重跑 service.sh（它会写新的守护代次）
		`rm -f "$STOP_FLAG"`,
		`sh "$MODDIR/service.sh"`,
	} {
		if !strings.Contains(actionSh, snippet) {
			t.Fatalf("action.sh 缺少 toggle 相关片段: %q", snippet)
		}
	}
	// 守护子 shell 靠停止开关自退，绝不能用 pkill -f service.sh：
	// 那会误杀正在执行的 service.sh 自己。
	if strings.Contains(actionSh, "pkill -f service.sh") {
		t.Fatal("action.sh 不得用 pkill -f service.sh 结束守护（会误杀正在执行的 service.sh 本身）")
	}

	// uninstall.sh：停止开关与守护代次必须【无条件】删除。
	// uninstall.sh 有 .keep_on_uninstall 分支，命中时 PERSIST_DIR 整个不删 ——
	// 删除语句要是落进那个分支里，「停止 → 保留数据卸载 → 重装」会得到一个
	// 永远起不来的新模块，而且零线索。
	uninstallSh := readMagiskScript(t, "uninstall.sh")
	keepIdx := strings.Index(uninstallSh, `if [ -f "$KEEP_FLAG" ]; then`)
	removeStopIdx := strings.Index(uninstallSh, `rm -f "$STOP_FLAG"`)
	removeGenIdx := strings.Index(uninstallSh, `rm -f "$WATCHDOG_GEN_FILE"`)
	if keepIdx < 0 || removeStopIdx < 0 || removeGenIdx < 0 {
		t.Fatalf("uninstall.sh 找不到定位锚点 (keep=%d stop=%d gen=%d)", keepIdx, removeStopIdx, removeGenIdx)
	}
	if removeStopIdx < keepIdx || removeGenIdx < keepIdx {
		t.Fatal("uninstall.sh 删除停止开关 / 守护代次的语句必须排在 KEEP_FLAG 分支之后（无条件执行）")
	}

	// customize.sh：刷 zip 前先让守护自退，装完无条件清掉开关。
	customize := readMagiskCustomizeScript(t)
	for _, snippet := range []string{
		`> "$PERSIST_DIR/` + magiskStopFlagName + `"`,
		`rm -f "$PERSIST_DIR/` + magiskWatchdogGenName + `"`,
		`rm -f "$PERSIST_DIR/` + magiskStopFlagName + `"`,
	} {
		if !strings.Contains(customize, snippet) {
			t.Fatalf("customize.sh 缺少停止开关收口片段: %q", snippet)
		}
	}
	stopWriteIdx := strings.Index(customize, `> "$PERSIST_DIR/`+magiskStopFlagName+`"`)
	rmRootfsIdx := strings.Index(customize, `rm -rf "$rootfs"`)
	if stopWriteIdx < 0 || rmRootfsIdx < 0 || stopWriteIdx > rmRootfsIdx {
		t.Fatalf("customize.sh 必须先写停止开关让守护自退，再 rm -rf rootfs (write=%d rm=%d)", stopWriteIdx, rmRootfsIdx)
	}
}

func TestMagiskServiceScriptExportsAndroidRuntimeEnv(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "Magisk", "service.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read Magisk service.sh: %v", err)
	}
	text := string(data)

	requiredSnippets := []string{
		"export DAIDAI_MAGISK_MODULE=1",
		"export DAIDAI_ANDROID_RUNTIME_BIN_DIR=/data/adb/daidai-panel/bin",
		"/data/adb/daidai-panel/bin/python/bin",
		"/data/adb/daidai-panel/bin/node/bin",
	}
	// service.sh 里 export 的外壳版本必须等于 currentMagiskShellVersion
	// —— 后者的定义就是「本仓库 service.sh 当前 export 的值」。
	//
	// 注意这里对齐的【不是】 requiredMagiskShellVersion：那个是「在线升级放行的最低外壳版本」，
	// 只有当新面板无法在旧外壳上运行时才提。两者相等只是巧合，不是契约。
	requiredSnippets = append(requiredSnippets,
		"export DAIDAI_MAGISK_SHELL_VERSION="+strconv.Itoa(currentMagiskShellVersion),
	)
	for _, snippet := range requiredSnippets {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected service.sh to contain %q", snippet)
		}
	}

	// 放行的最低外壳版本永远不能超过仓库里外壳的实际版本 —— 否则刚打出来的 zip
	// 装上去就会被自己的外壳版本自检拦住，在线升级直接变成死路。
	if requiredMagiskShellVersion > currentMagiskShellVersion {
		t.Fatalf("requiredMagiskShellVersion(%d) 不得大于 currentMagiskShellVersion(%d)",
			requiredMagiskShellVersion, currentMagiskShellVersion)
	}

	if strings.Contains(text, `deps/python/3.12`) {
		t.Fatal("expected service.sh to avoid hard-coded deps/python/3.12 venv path")
	}
	for _, snippet := range []string{
		`PY_MINOR=$(python3 -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"`,
		`export DAIDAI_PYTHON_VERSION="$PY_MINOR"`,
		`"$DAIDAI_DIR/deps/python/$PY_MINOR"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected service.sh to contain dynamic python runtime snippet %q", snippet)
		}
	}
}

// 下面这几个针对 Magisk 脚本的断言都是纯静态字符串检查。
// 它们只能防止相应改动被整段删掉 / 改回旧写法，**防不住逻辑写错**，
// 也不构成"Magisk 模块有测试覆盖"。真正的验证只能靠真机安装 ——
// Debian flavor 更是连一次真机安装都还没做过。

// readMagiskScript 统一去掉 CR：Windows 检出可能是 CRLF，
// 否则按行 / 按 heredoc 结束标记做的断言会在 Windows 上莫名其妙地失败。
func readMagiskScript(t *testing.T, name string) string {
	t.Helper()
	scriptPath := filepath.Join("..", "..", "Magisk", name)
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read Magisk %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func readMagiskCustomizeScript(t *testing.T) string {
	t.Helper()
	return readMagiskScript(t, "customize.sh")
}

// 安装期失败必须真的失败：rurima 前置检查 / 依赖装完再验证 / 成功提示有条件。
func TestMagiskCustomizeScriptFailsLoudlyOnInstallErrors(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	requiredSnippets := []string{
		// rurima 存在性 + 可执行性检查，必须在第一次调用它之前
		`RURIMA="$MODPATH/system/bin/rurima"`,
		`if [ ! -f "$RURIMA" ]; then`,
		`if [ ! -x "$RURIMA" ]; then`,
		// 装完再验证关键运行时（apk add 可能部分成功，只信退出码不可靠）
		`for c in python3 node npm git bash; do`,
		`missing_runtimes`,
		// 成功提示的开关
		`INSTALL_DEPS_OK=1`,
		`if [ "$INSTALL_DEPS_OK" != "1" ]; then`,
		// abort 时告知用户备份仍在
		`warn_backup_preserved`,
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain %q", snippet)
		}
	}

	// 依赖验证必须排在 apk 安装之后，否则验的是空容器
	apkIdx := strings.Index(text, "apk add --no-cache")
	verifyIdx := strings.Index(text, `missing_runtimes=""`)
	if apkIdx < 0 || verifyIdx < 0 {
		t.Fatalf("customize.sh 缺少 apk 安装段或运行时验证段 (apk=%d verify=%d)", apkIdx, verifyIdx)
	}
	if verifyIdx < apkIdx {
		t.Fatal("运行时验证必须放在 apk 安装之后")
	}

	// 「安装完成！」必须排在 INSTALL_DEPS_OK 判断之后
	gateIdx := strings.Index(text, `if [ "$INSTALL_DEPS_OK" != "1" ]; then`)
	doneIdx := strings.Index(text, "- 安装完成！")
	if gateIdx < 0 || doneIdx < 0 {
		t.Fatalf("customize.sh 缺少成功提示或其开关 (gate=%d done=%d)", gateIdx, doneIdx)
	}
	if doneIdx < gateIdx {
		t.Fatal("「安装完成！」必须排在 INSTALL_DEPS_OK 判断之后，不能无条件打印")
	}
}

// heredocBlock 取出 `<< 'MARKER'` 与行首 `MARKER` 之间的正文。
// 找不到起始 / 结束标记时直接 Fatal —— 标记被改名说明装依赖那段被重写过，
// 必须回来同步断言，而不是让测试静默变成空跑。
func heredocBlock(t *testing.T, text, marker string) string {
	t.Helper()
	start := strings.Index(text, "<< '"+marker+"'")
	if start < 0 {
		t.Fatalf("customize.sh 找不到 heredoc 起始标记 %q", marker)
	}
	rest := text[start:]
	end := strings.Index(rest, "\n"+marker+"\n")
	if end < 0 {
		t.Fatalf("customize.sh 找不到 heredoc %q 的结束标记", marker)
	}
	return rest[:end]
}

// 装依赖那几段 heredoc 内都不得使用 set -e：
// apk add / apt-get install 都可能局部失败，但仍要走完后面的 Node 下载与账号 / SSH 配置。
// 加了 set -e 会让整个安装在中途直接断掉，真正的判据是装完之后的运行时验证。
func TestMagiskCustomizeScriptDependencyHeredocHasNoSetE(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, marker := range []string{
		"DEPS_PKG_ALPINE_EOF",
		"DEPS_PKG_DEBIAN_EOF",
		"DEPS_COMMON_EOF",
	} {
		// 只看真正会被执行的行 —— 脚本里有注释专门解释"为什么不能加 set -e"，
		// 那行本身包含 set -e，不能被当成违规。
		for i, line := range strings.Split(heredocBlock(t, text, marker), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(trimmed, "set -e") {
				t.Fatalf("装依赖 heredoc %s 第 %d 行不得使用 set -e（包管理器允许局部失败，set -e 会直接中断安装）: %s",
					marker, i+1, trimmed)
			}
		}
	}
}

// ---- flavor（alpine / debian）相关断言 ----------------------------------
//
// 这一组是本次改动里最有价值的静态断言。Debian 容器里没有 /bin/ash，
// 只要「容器能力探测」或「依赖装完验证」里残留了写死的 /bin/ash，
// Debian 版就会 100% 探测失败，而报错完全指不到真正的原因。

// customize.sh 必须读 flavor 标记文件，且缺失 / 非法时回落 alpine。
func TestMagiskCustomizeScriptReadsFlavorWithAlpineDefault(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		// 缺省值必须在读文件之前先落成 alpine
		"FLAVOR=alpine",
		`if [ -f "$MODPATH/flavor" ]; then`,
		`read -r flavor_raw < "$MODPATH/flavor"`,
		// 只认 debian，其余一律回落 alpine —— 默认值就是安全值
		"debian*) FLAVOR=debian ;;",
		"*) FLAVOR=alpine ;;",
		`if [ "$FLAVOR" = "debian" ]; then`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain flavor snippet %q", snippet)
		}
	}
}

// assertNoHardcodedAsh 检查脚本里出现的 /bin/ash 只允许在注释或 CTR_SHELL 赋值里，
// 任何实际调用（尤其是 rurima ruri ... /bin/ash）都必须走 flavor 变量。
func assertNoHardcodedAsh(t *testing.T, name, text string) {
	t.Helper()
	for i, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "/bin/ash") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		// 注释（含 heredoc 里的 #!/bin/ash shebang）与 CTR_SHELL 赋值是仅有的两种合法出现
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "CTR_SHELL=") {
			continue
		}
		t.Fatalf("%s:%d 出现写死的 /bin/ash，必须改用 $CTR_SHELL（Debian 容器里没有 ash）: %s",
			name, i+1, trimmed)
	}
}

// 能力探测与依赖验证必须使用 flavor 变量而非写死 /bin/ash。
func TestMagiskCustomizeScriptProbeAndVerifyUseFlavorShell(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		// 两个 flavor 的容器 shell 都要定义
		"CTR_SHELL=/bin/ash",
		"CTR_SHELL=/bin/bash",
		// 容器能力探测
		`probe_out=$("$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" -c `,
		// 依赖装完验证：连着后面那行一起匹配，避免和上面的探测命令撞车
		"\"$RURIMA\" ruri -p -N -S -A \"$rootfs\" \"$CTR_SHELL\" -c '\n" +
			"  for c in python3 node npm git bash; do",
		// 装依赖脚本本身也要按 flavor 选 shell
		`"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" /tmp/daidai-install-deps.sh`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain flavor-aware snippet %q", snippet)
		}
	}

	assertNoHardcodedAsh(t, "customize.sh", text)
}

// Debian 分支里不得残留任何 Alpine 专有的命令 / 参数 / 镜像源。
func TestMagiskCustomizeScriptDebianBranchHasNoAlpineisms(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	block := heredocBlock(t, text, "DEPS_PKG_DEBIAN_EOF")

	for _, forbidden := range []string{
		"apk ",
		"--no-cache",
		"dl-cdn.alpinelinux.org",
		"/bin/ash",
		"/etc/apk/",
	} {
		// 注释里为了写清映射关系会提到 apk / Alpine，所以逐行判断并跳过注释行
		for i, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if strings.Contains(line, forbidden) {
				t.Fatalf("customize.sh Debian 分支第 %d 行出现 Alpine 专有内容 %q: %s", i+1, forbidden, line)
			}
		}
	}

	for _, required := range []string{
		"apt-get install -y --no-install-recommends",
		// bookworm 是 deb822 格式，只改老的 sources.list 会静默走原始源
		"/etc/apt/sources.list.d/debian.sources",
		"mirrors.nju.edu.cn",
		// 没有 python3-venv，service.sh 每次开机建的 deps/python venv 会直接失败
		"python3-venv",
		"ca-certificates",
		// openssh 在 Debian 拆成了 client + server 两个包
		"openssh-client openssh-server",
	} {
		if !strings.Contains(block, required) {
			t.Fatalf("expected customize.sh Debian 分支 to contain %q", required)
		}
	}
}

// bashrc 路径必须按 flavor 走：Alpine 是 /etc/bash/bashrc，Debian 是 /etc/bash.bashrc。
// 写错位置不会报错，只是环境变量静默不生效。
func TestMagiskCustomizeScriptUsesFlavorBashrcPath(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		`CTR_BASHRC="/etc/bash.bashrc"`,
		`CTR_BASHRC="/etc/bash/bashrc"`,
		`cat > "$rootfs$CTR_BASHRC"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain bashrc snippet %q", snippet)
		}
	}
	if strings.Contains(text, "cat > $rootfs/etc/bash/bashrc") {
		t.Fatal("customize.sh 不应再写死 Alpine 的 /etc/bash/bashrc 路径")
	}
}

// 运行期脚本同样要按 flavor 取容器 shell —— 装得上但起不来同样是坏的。
func TestMagiskRuntimeScriptsUseFlavorShell(t *testing.T) {
	for _, name := range []string{"service.sh", "action.sh"} {
		text := readMagiskScript(t, name)

		for _, snippet := range []string{
			`if [ -f "$MODDIR/flavor" ]; then`,
			"FLAVOR=alpine",
			`read -r flavor_raw < "$MODDIR/flavor"`,
			"debian*) FLAVOR=debian ;;",
			"*) FLAVOR=alpine ;;",
			"CTR_SHELL=/bin/ash",
			`[ "$FLAVOR" = "debian" ] && CTR_SHELL=/bin/bash`,
		} {
			if !strings.Contains(text, snippet) {
				t.Fatalf("expected %s to contain flavor snippet %q", name, snippet)
			}
		}

		assertNoHardcodedAsh(t, name, text)
	}
}

// build.sh 必须写出 flavor 标记文件，且默认（不传第三个参数）行为与产物名不变。
func TestMagiskBuildScriptWritesFlavorFile(t *testing.T) {
	text := readMagiskScript(t, "build.sh")

	for _, snippet := range []string{
		// 不传时默认 alpine
		`FLAVOR="${3:-alpine}"`,
		// alpine 用空后缀，保证默认产物名与历史逐字节一致
		`  alpine) FLAVOR_SUFFIX="" ;;`,
		`  debian) FLAVOR_SUFFIX="-debian" ;;`,
		`OUTZIP="$DIST/daidai-panel-magisk${FLAVOR_SUFFIX}-v${VERSION}.zip"`,
		// flavor 标记文件必须真的写进 staging
		`printf '%s\n' "$FLAVOR" > "$STAGING/flavor"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected build.sh to contain %q", snippet)
		}
	}
}

// 离线 apk（linux-pam / shadow）机制已整套删除，这条防的是有人照着旧版本把它加回来。
//
// 那两个包是 Alpine 3.18 专用的：在 3.23 rootfs 上用 --no-network 一旦装得进去，
// 联网那批 apk add 会认为 shadow 已满足而不再升级，留下新旧混装；
// 而联网安装本来就是必需步骤（失败会中止），离线包没有任何兜底价值。
// 原来这里锁的是「离线 apk 只进 alpine 包」，机制删掉之后改成锁「两边都不再出现」。
func TestMagiskScriptsDoNotShipOfflineApk(t *testing.T) {
	buildSh := readMagiskScript(t, "build.sh")
	for _, forbidden := range []string{`$MODDIR/apk`, `$STAGING/apk`, `.apk`} {
		assertNotInExecutableLines(t, "build.sh", buildSh, forbidden,
			"build.sh 不得再往模块 ZIP 里拷离线 apk")
	}

	customize := readMagiskCustomizeScript(t)
	for _, forbidden := range []string{`$MODPATH/apk`, `/tmp/apk`, `--no-network`, `--allow-untrusted`} {
		assertNotInExecutableLines(t, "customize.sh", customize, forbidden,
			"customize.sh 不得再搬运 / 安装离线 apk（容器依赖一律联网安装）")
	}

	// 仓库里也不应再躺着离线包：build.sh 不拷它们时，它们只是一份会误导后人的死文件。
	matches, err := filepath.Glob(filepath.Join("..", "..", "Magisk", "apk", "*.apk"))
	if err != nil {
		t.Fatalf("glob Magisk/apk: %v", err)
	}
	if len(matches) > 0 {
		t.Fatalf("Magisk/apk 下不应再有离线 apk：%v", matches)
	}
}

// 架构闸门只放行 arm64；容器运行时 rurima 只有 aarch64 构建。
func TestMagiskCustomizeScriptRejectsX64(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	if strings.Contains(text, `[ "$ARCH" != "arm64" ] && [ "$ARCH" != "x64" ]`) {
		t.Fatal("customize.sh 不得再放行 x64：rurima 只有 aarch64 构建，x86_64 会在 exec 时失败")
	}
	if !strings.Contains(text, `if [ "$ARCH" != "arm64" ]; then`) {
		t.Fatal("customize.sh 架构检查应只放行 arm64")
	}
	// x64 要有专门的提示，不能和「架构不支持」混为一谈
	if !strings.Contains(text, `if [ "$ARCH" = "x64" ] || [ "$ARCH" = "x86_64" ]; then`) {
		t.Fatal("customize.sh 应对 x86_64 给出专门的提示分支")
	}
	// x86_64 的 Alpine rootfs 分支已经是死代码，不应残留
	if strings.Contains(text, "alpine-minirootfs-3.18.9-x86_64.tar.gz") {
		t.Fatal("customize.sh 不应再保留 x86_64 的 Alpine rootfs 下载分支")
	}
	// 不绑定具体版本号再挡一次：Alpine 升版本时顺手把 x86_64 分支加回来同样是死代码
	for i, line := range magiskExecLines(text) {
		if strings.Contains(line, "alpine-minirootfs-") && strings.Contains(line, "x86_64") {
			t.Fatalf("customize.sh 第 %d 条可执行行不应再出现 x86_64 的 Alpine rootfs: %s", i+1, line)
		}
	}
}

// 硬性 API 闸门降到 23（Android 6.0）；真正的准入是容器能力探测。
func TestMagiskCustomizeScriptUsesCapabilityProbeInsteadOfVersionGate(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	if strings.Contains(text, `if [ "$API" -lt 24 ]; then`) {
		t.Fatal("customize.sh 的硬性 API 闸门应从 24 降到 23")
	}
	if !strings.Contains(text, `if [ "$API" -lt 23 ]; then`) {
		t.Fatal("customize.sh 应保留 API 23 的硬性下限")
	}

	// 能力探测：哨兵字符串 + 实际进容器执行
	if !strings.Contains(text, "DAIDAI_CONTAINER_PROBE_OK") {
		t.Fatal("customize.sh 缺少容器能力探测的哨兵字符串")
	}
	// 探测必须排在装依赖之前：装依赖耗时且强依赖网络，
	// 先探测才能把「容器起不来」和「网络不通」两类失败区分开
	probeIdx := strings.Index(text, "DAIDAI_CONTAINER_PROBE_OK")
	apkIdx := strings.Index(text, "apk add --no-cache")
	if apkIdx < 0 {
		t.Fatal("customize.sh 找不到 apk 安装段")
	}
	if probeIdx > apkIdx {
		t.Fatal("容器能力探测必须放在装依赖之前")
	}
}

func TestMagiskCheckRuntimesScriptIncludesInstalledRuntimePaths(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "Magisk", "scripts", "check-runtimes.sh")
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read Magisk check-runtimes.sh: %v", err)
	}
	text := string(data)

	requiredSnippets := []string{
		"\"$PANEL_DIR/bin/python/bin\"",
		"\"$PANEL_DIR/bin/node/bin\"",
		"\"$PANEL_DIR/bin\"",
		"PANEL_RUNTIME_PATHS",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected check-runtimes.sh to contain %q", snippet)
		}
	}
}

// ---- 容器内 DNS / apt 加固相关断言 ---------------------------------------
//
// 背景：Debian flavor 装依赖时 apt 全部报 Temporary failure resolving，
// Alpine 不受影响。三个互斥候选（apt 降权被拒网 / glibc 解析器行为 / DNS 本身不可达）
// 在脚本里原本没有任何东西能区分。下面这几条锁住的是「判别手段」和「低风险修复」本身，
// 同样只是静态字符串断言 —— 真机验证不可省。

// Debian 的 resolv.conf 不能再是单条硬编码：要吃宿主 net.dns*、要有公共 DNS 兜底、
// 还必须带 options single-request-reopen（这条正是 musl 不受影响而 glibc 受影响的关键）。
// 但这一整套【只能】落在 Debian 分支里 —— Alpine 必须逐字保留改动前那条单一 DNS，
// 原因见下面 alpineSingleDNS 处的注释。
func TestMagiskCustomizeScriptWritesResilientResolvConf(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		// 宿主真实 DNS 优先
		"for p in net.dns1 net.dns2 net.dns3 net.dns4; do",
		// 公共 DNS 兜底
		"for d in 223.5.5.5 119.29.29.29 8.8.8.8; do",
		// glibc 的 A+AAAA 同源端口并发查询是主要嫌疑，必须关掉
		"options single-request-reopen timeout:2 attempts:3",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain resolv.conf snippet %q", snippet)
		}
	}

	// Alpine 必须逐字保留改动前那条单一 DNS，这【不是】残留而是刻意的 flavor 差异：
	// musl 对所有 nameserver 并行发查询、谁先回就采信谁，并且把 NXDOMAIN 也当成
	// 确定性应答直接接受。把宿主 net.dns（校园网 / 企业网 / Captive Portal 的强制
	// 解析器）塞进 Alpine 的 resolv.conf，只要它抢先回一个 NXDOMAIN，musl 就直接判定
	// 域名不存在 —— 本来装得上的网络会开始装不上。Alpine 是目前唯一被真机证实可用的
	// flavor，不能拿它冒这个险。
	//
	// 但这条单行必须待在 else 分支里：一旦跑到公共段（或跑到 Debian 分支后面），
	// 它就会把上面那套多源写入整段覆盖掉，等于 Debian 的修复白做。
	const alpineSingleDNS = `echo "nameserver 223.5.5.5" > $rootfs/etc/resolv.conf`
	if got := strings.Count(text, alpineSingleDNS); got != 1 {
		t.Fatalf("单条硬编码 nameserver 应恰好出现 1 次（Alpine 分支），实际 %d 次", got)
	}

	// 用「标记行号严格递增」锁住 DNS 段落的结构：
	//   DNS 段落起头 -> cp hosts（公共段，两个 flavor 都要）-> if debian
	//     -> 多源写入 -> options -> else -> Alpine 单条写死 -> fi
	// 这样既保证 Alpine 的单条只在 else 里，也保证 cp hosts 没被误挪进某个分支。
	lines := strings.Split(text, "\n")
	findFrom := func(from int, match func(string) bool) int {
		for i := from; i < len(lines); i++ {
			if match(lines[i]) {
				return i
			}
		}
		return -1
	}
	contains := func(s string) func(string) bool {
		return func(line string) bool { return strings.Contains(line, s) }
	}
	// else / fi 必须整行精确匹配，否则会被注释里的字样勾到
	exact := func(s string) func(string) bool {
		return func(line string) bool { return strings.TrimSpace(line) == s }
	}

	at := findFrom(0, contains("# ---- DNS / hosts 准备"))
	if at < 0 {
		t.Fatal("customize.sh 里找不到「DNS / hosts 准备」段落")
	}
	for _, step := range []struct {
		desc  string
		match func(string) bool
	}{
		{"cp /system/etc/hosts（公共段）", contains("cp /system/etc/hosts $rootfs/etc/")},
		{`if [ "$FLAVOR" = "debian" ]; then`, exact(`if [ "$FLAVOR" = "debian" ]; then`)},
		{"宿主 net.dns* 循环（Debian 分支）", contains("for p in net.dns1 net.dns2 net.dns3 net.dns4; do")},
		{"options single-request-reopen（Debian 分支）", contains("options single-request-reopen timeout:2 attempts:3")},
		{"else（切到 Alpine 分支）", exact("else")},
		{"Alpine 单条写死", contains(alpineSingleDNS)},
		{"fi（DNS 分叉结束）", exact("fi")},
	} {
		next := findFrom(at+1, step.match)
		if next < 0 {
			t.Fatalf("DNS 段落结构不对：在第 %d 行之后按顺序找不到 %s", at+1, step.desc)
		}
		at = next
	}

	// nsswitch.conf 绝不能被截断写：Debian 的这个文件由 base-files 提供、本来就在，
	// `>` 覆盖会连 passwd: / group: / shadow: 一起删掉，
	// 直接搞坏紧随其后的 usermod / chpasswd 以及 service.sh 里的 adduser / sshd。
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(line, "nsswitch.conf") {
			continue
		}
		for _, bad := range []string{
			`> "$rootfs/etc/nsswitch.conf"`,
			`> $rootfs/etc/nsswitch.conf`,
		} {
			// 只禁截断写，追加写（>>）是允许的
			if strings.Contains(line, bad) && !strings.Contains(line, ">"+bad) {
				t.Fatalf("customize.sh:%d 不得截断写 nsswitch.conf（会删掉 passwd:/group:/shadow: 行）: %s",
					i+1, trimmed)
			}
		}
	}
}

// Alpine 分支里【只允许】那一行单条 DNS 写死，多源 DNS 逻辑必须整段被 debian 判断包住。
//
// 上面那个用例只锁住了「单条写死恰好出现 1 次」和 if/else/fi 的相对顺序，
// 锁不住 else 分支里【多出来】的语句：保留那一行、后面再追加 net.dns / 公共 DNS / options，
// 它照样全绿。而这正是脚本注释里反复警告的那种「顺手统一」回归 ——
// musl 对所有 nameserver 并行发查询、且把 NXDOMAIN 当成确定性应答直接采信，
// 宿主强制 DNS（校园网 / 企业网 / Captive Portal）抢先回一个 NXDOMAIN，
// 就会让本来装得上的 Alpine 变成装不上，而 Alpine 是目前唯一被真机证实可用的 flavor。
func TestMagiskCustomizeScriptKeepsAlpineDNSBranchSingleLine(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	lines := strings.Split(text, "\n")

	// 只在「DNS / hosts 准备」段落里定位：装依赖那段有一模一样写法的 flavor 判断，
	// 从文件开头找会勾错分支。
	sectionIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "# ---- DNS / hosts 准备") {
			sectionIdx = i
			break
		}
	}
	if sectionIdx < 0 {
		t.Fatal("customize.sh 里找不到「DNS / hosts 准备」段落")
	}

	// else / fi 必须整行精确匹配，否则会被注释里的字样勾到
	findExact := func(from int, want string) int {
		for i := from; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == want {
				return i
			}
		}
		return -1
	}

	ifIdx := findExact(sectionIdx, `if [ "$FLAVOR" = "debian" ]; then`)
	if ifIdx < 0 {
		t.Fatal("DNS 段落里找不到 flavor 判断的起始行")
	}
	elseIdx := findExact(ifIdx+1, "else")
	if elseIdx < 0 {
		t.Fatal("DNS 段落的 flavor 判断缺少 else 分支（Alpine 必须走单独分支）")
	}
	fiIdx := findExact(elseIdx+1, "fi")
	if fiIdx < 0 {
		t.Fatal("DNS 段落的 flavor 判断缺少收尾的 fi")
	}

	// 1. Alpine 分支（else..fi）里可执行的行必须【有且只有】那条单行写死。
	//    这里做全等比较而不是 Contains：多一条 nameserver、多一个 options、
	//    甚至把两条语句用 `;` 挤到同一行，都会被这条挡住。
	//    顺带说明：Alpine 分支里一旦嵌套 if/for，那个块的开头行本身就会算进
	//    可执行行里，所以不需要额外的嵌套判断。
	const alpineSingleDNS = `echo "nameserver 223.5.5.5" > $rootfs/etc/resolv.conf`
	alpineExec := make([]string, 0, 4)
	for i := elseIdx + 1; i < fiIdx; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		alpineExec = append(alpineExec, trimmed)
	}
	if len(alpineExec) != 1 || alpineExec[0] != alpineSingleDNS {
		t.Fatalf("Alpine 分支只允许保留那条单行 DNS 写死 %q，实际可执行行=%q\n"+
			"（musl 并行查询 + 采信 NXDOMAIN：多写 nameserver 会让本来装得上的设备开始装不上）",
			alpineSingleDNS, alpineExec)
	}

	// 2. 多源 DNS 的三段逻辑必须真的在 debian 分支体内。
	debianBody := strings.Join(lines[ifIdx+1:elseIdx], "\n")
	multiSourceSnippets := []string{
		"for p in net.dns1 net.dns2 net.dns3 net.dns4; do",
		"for d in 223.5.5.5 119.29.29.29 8.8.8.8; do",
		"options single-request-reopen timeout:2 attempts:3",
	}
	for _, snippet := range multiSourceSnippets {
		if !strings.Contains(debianBody, snippet) {
			t.Fatalf("多源 DNS 片段 %q 必须待在 debian 分支体内", snippet)
		}
	}

	// 3. 多源 DNS 的每一行都【只能】出现在 debian 分支内。
	//    把它们挪到公共段（例如 fi 之后）同样会覆盖掉 Alpine 那条单行，
	//    而第 1 条检查只看 else..fi 之间，单独挡不住这种绕法。
	for _, marker := range []string{
		"for p in net.dns1",
		"for d in 223.5.5.5",
		"options single-request-reopen",
	} {
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || !strings.Contains(line, marker) {
				continue
			}
			if i <= ifIdx || i >= elseIdx {
				t.Fatalf("customize.sh:%d 的多源 DNS 片段 %q 跑到了 debian 分支之外（分支体为第 %d..%d 行）: %s",
					i+1, marker, ifIdx+2, elseIdx, trimmed)
			}
		}
	}
}

// 装依赖之前必须先做 root / _apt 双身份的 DNS 判别探测，
// 否则三个互斥候选永远分不开，下次还是只能拿到同一句「解析失败」。
func TestMagiskCustomizeScriptProbesContainerDNSBeforeInstallingDeps(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		"PROBE_ROOT=",
		"PROBE_APT=",
		// _apt 在 bookworm 是 uid 42、bullseye 是 100 —— 只能动态取
		"id -u _apt",
		"id -g _apt",
		"setpriv --reuid=",
		"--clear-groups",
		`getent hosts "$probe_host"`,
		// 判别结论要留给后面的报错文案用
		"DNS_PROBE_VERDICT",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh to contain DNS probe snippet %q", snippet)
		}
	}

	// uid 写死过就等于探测错对象，且不会报错，只会给出错误结论
	for _, forbidden := range []string{"--reuid=42", "--reuid=100"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("customize.sh 不得写死 _apt 的 uid（%s），bookworm 与 bullseye 不同", forbidden)
		}
	}

	// 探测必须排在装依赖之前
	probeIdx := strings.Index(text, "daidai-dns-probe.sh")
	installIdx := strings.Index(text, `"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" /tmp/daidai-install-deps.sh`)
	if probeIdx < 0 || installIdx < 0 {
		t.Fatalf("customize.sh 找不到 DNS 探测或装依赖的定位锚点 (probe=%d install=%d)", probeIdx, installIdx)
	}
	if probeIdx > installIdx {
		t.Fatal("DNS 判别探测必须排在装依赖之前，否则拿不到「装依赖为什么失败」的判据")
	}
}

// Debian 分支的 apt 加固 + 镜像源回退。
func TestMagiskCustomizeScriptHardensDebianApt(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	block := heredocBlock(t, text, "DEPS_PKG_DEBIAN_EOF")

	for _, snippet := range []string{
		// apt.conf 落在 apt.conf.d 下，运行期那三条裸 apt 路径会自动继承
		"/etc/apt/apt.conf.d/99-daidai-android",
		`APT::Sandbox::User "root";`,
		`Acquire::Retries "3";`,
		`Acquire::ForceIPv4 "true";`,
		// 镜像源回退列表：第一个仍是 NJU（保持原行为），后面三个是新增兜底
		"mirrors.nju.edu.cn",
		"mirrors.tuna.tsinghua.edu.cn",
		"mirrors.aliyun.com",
		"deb.debian.org",
		// 每换一个源都要重跑 update，否则换了也白换
		"if apt-get update; then",
		// 供宿主侧报错文案分流用的状态记录
		"/tmp/daidai-deps-status",
	} {
		if !strings.Contains(block, snippet) {
			t.Fatalf("expected customize.sh Debian 分支 to contain %q", snippet)
		}
	}

	// 回退列表必须是「一个循环」而不是只改了域名，否则第一个源挂了照样全盘失败
	if !strings.Contains(block, "for _mirror in mirrors.nju.edu.cn") {
		t.Fatal("customize.sh Debian 分支必须用镜像源回退列表，而不是单一硬编码源")
	}

	// 给 _apt 加 aid_3003 是无效修复：apt 的 DropPrivs() 会 setgroups() 清空附加组，
	// 加了只会掩盖问题，让下次排查更难。
	if strings.Contains(text, "aid_3003 _apt") || strings.Contains(text, "_apt aid_3003") {
		t.Fatal("不得给 _apt 追加 aid_3003 组：apt 的 setgroups() 会清掉附加组，属于无效修复")
	}

	// 这份 apt.conf 必须留在容器里（运行期装 Linux 依赖同样受益），不能被 clean 段顺手删掉
	for i, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "99-daidai-android") && strings.Contains(line, "rm ") {
			t.Fatalf("customize.sh Debian 分支第 %d 行不得删除 99-daidai-android（运行期 apt 也靠它）: %s",
				i+1, trimmed)
		}
	}

	// ca-certificates 必须在那一大批之前先单独装一次：
	// 没有根证书时镜像源只要 301 到 https，整批安装会死在证书校验上。
	caIdx := strings.Index(block, "apt-get install -y --no-install-recommends ca-certificates")
	batchIdx := strings.Index(block, "apt-get install -y --no-install-recommends \\")
	if caIdx < 0 || batchIdx < 0 {
		t.Fatalf("customize.sh Debian 分支找不到 ca-certificates / 批量安装锚点 (ca=%d batch=%d)", caIdx, batchIdx)
	}
	if caIdx > batchIdx {
		t.Fatal("ca-certificates 必须排在批量安装之前单独装一次")
	}
}

// assertNotInExecutableLines 只在「真正会被执行的行」里查禁用字面量。
// 脚本里常常有注释专门解释「为什么不能再这么写」，那些行本身包含被禁的字面量，
// 用整文件 Contains 会把它们当成违规。
func assertNotInExecutableLines(t *testing.T, name, text, forbidden, reason string) {
	t.Helper()
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, forbidden) {
			t.Fatalf("%s:%d %s: %s", name, i+1, reason, trimmed)
		}
	}
}

// ---- SSH 相关断言（v3.0.7）-----------------------------------------------
//
// 背景：Debian 版刷完之后 SSH 连不上，Alpine 版正常。四条候选根因分别是
// 「pgrep -x sshd 跨宿主 /proc 误命中导致 sshd 从没被启动」「openssh-server 没装成
// 而安装期验证清单不含 sshd」「Debian 的 PAM 会话栈在 chroot 里失败」
// 「chpasswd 静默失败」。这一组锁住对应的修复与可观测性，同样只是静态字符串断言 ——
// **防不住 shell 逻辑写错**，真机验证不可省。

// sshd 的启动去重不能再用 pgrep：ruri 走 chroot 且不传 -u，容器与宿主共享进程表，
// pgrep -x sshd 会命中整机任何叫 sshd 的进程（包括上次安装遗留的孤儿），
// 一旦命中就跳过启动，表现就是「刷了另一个 flavor 之后 SSH 永远连不上」。
func TestMagiskServiceScriptStartsSshdWithPortBasedGuard(t *testing.T) {
	text := readMagiskScript(t, "service.sh")

	// 逐行判断并跳过注释：脚本里专门写了「为什么不能再用 pgrep」的说明，
	// 那几行本身包含这串字面量，不能被当成违规。
	assertNotInExecutableLines(t, "service.sh", text, "pgrep -x sshd",
		"不得再用 pgrep -x sshd 做 sshd 启动去重（容器没有 PID namespace，会命中宿主进程）")
	assertNotInExecutableLines(t, "service.sh", text, "/usr/sbin/sshd >/dev/null 2>&1",
		"不得再把 sshd 的启动输出丢进 /dev/null（容器里没有 syslogd，那等于没有任何日志）")

	// 判据也不能退回 nc -z：PATH 里的 nc 可能解析到 busybox applet，它不认 -z，
	// 会恒定返回「没监听」——每次开机都多起一个注定 Address already in use 的 sshd。
	assertNotInExecutableLines(t, "service.sh", text, "nc -z",
		"不得用 nc -z 判断端口（busybox 的 nc applet 不支持 -z，会恒定误判）")

	for _, snippet := range []string{
		// 端口判据取代进程名判据，且直接读 /proc/net/tcp 不依赖任何外部命令
		`ssh_port_listening() {`,
		`{ cat /proc/net/tcp /proc/net/tcp6 2>/dev/null; } | awk -v p="$_hexport" '`,
		`if ssh_port_listening; then`,
		// -D -e 缺一不可：-e 才有日志，没有 -D 时 sshd 会 daemon(0,0) 把 stdio 丢掉
		`nohup /usr/sbin/sshd -D -e >> $DAIDAI_DIR/sshd.log 2>&1 &`,
		// 启动前配置自检
		`_sshd_t=$(/usr/sbin/sshd -t 2>&1)`,
		// sshd_config 不存在时也要留线索，不能整段静默跳过
		"openssh-server 多半没装成，本次不启动 sshd",
		// 慢设备上 sshd 要一两秒才 bind，只探一次会打出误导性的假告警
		`for _ssh_try in 1 2 3 4 5; do`,
		// 每次开机的状态快照
		`echo "[ssh] loginuid=$(cat /proc/self/loginuid 2>/dev/null || echo ABSENT)"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("service.sh 缺少 sshd 启动/可观测性片段: %q", snippet)
		}
	}

	// 日志滚动必须在「确实要重新拉起 sshd」的分支里：放在外面的话，
	// 老 sshd 还开着这个 fd，mv 之后它继续往 .old 写，新文件永远长不到阈值，
	// 滚动再也不会触发，.old 变成无上限增长。
	startIdx := strings.Index(text, `if ssh_port_listening; then`)
	rotateIdx := strings.Index(text, `mv -f $DAIDAI_DIR/sshd.log $DAIDAI_DIR/sshd.log.old`)
	launchIdx := strings.Index(text, `nohup /usr/sbin/sshd -D -e`)
	if startIdx < 0 || rotateIdx < 0 || launchIdx < 0 {
		t.Fatalf("service.sh 找不到定位锚点 (guard=%d rotate=%d launch=%d)", startIdx, rotateIdx, launchIdx)
	}
	if rotateIdx < startIdx || rotateIdx > launchIdx {
		t.Fatal("sshd.log 的滚动必须夹在「端口未监听」判断与拉起 sshd 之间")
	}
}

// sshd_config 的写法：OpenSSH 是「第一次取值胜出」，所以必须先删净同名指令再统一追加；
// Debian 顶部还有一行未注释的 Include，drop-in 会压过主文件，两边要写同一份。
func TestMagiskServiceScriptWritesAuthoritativeSshdConfig(t *testing.T) {
	text := readMagiskScript(t, "service.sh")

	for _, snippet := range []string{
		// awk 一趟完成「删同名指令 + 插入我们的三行」
		`awk -v port="${SSH_PORT}" '`,
		`print "PermitRootLogin yes"`,
		`print "PasswordAuthentication yes"`,
		// Match 块的识别（大小写不敏感，逐字母字符组，不依赖某个 awk 的忽略大小写扩展）
		`/^[[:space:]]*[Mm][Aa][Tt][Cc][Hh][[:space:]]/ {`,
		// 进了 Match 就不再删任何东西
		`!inmatch && /^[#[:space:]]*([Pp]ort|[Pp]ermit[Rr]oot[Ll]ogin|[Pp]assword[Aa]uthentication)[[:space:]]+/ { next }`,
		`mv -f /etc/ssh/sshd_config.daidai-tmp /etc/ssh/sshd_config`,
		`Include[[:space:]]+/etc/ssh/sshd_config\.d/`,
		`/etc/ssh/sshd_config.d/00-daidai.conf`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("service.sh 缺少 sshd_config 权威写入片段: %q", snippet)
		}
	}

	// 绝不能退回「sed 删净 + 追加到文件末尾」：
	//   1. 删除会波及 Match 块里的同名指令 —— 那是用户的作用域限定安全策略；
	//   2. 追加到末尾时，只要文件尾部有一个生效的 Match 块，我们这三行就落进去，
	//      而 Port 在 Match 内是非法指令，sshd 直接起不来。
	assertNotInExecutableLines(t, "service.sh", text,
		`sed -i -E '/^[#[:space:]]*(Port|PermitRootLogin|PasswordAuthentication)[[:space:]]+/d'`,
		"不得用 sed 无差别删同名指令（会波及 Match 块）")
	assertNotInExecutableLines(t, "service.sh", text,
		`} >> /etc/ssh/sshd_config`,
		"不得把托管指令追加到 sshd_config 末尾（尾部有生效 Match 块时 Port 会非法）")
}

// Debian 的 sshd 走 PAM（Alpine 的 openssh 是 --without-pam 构建），
// pam_loginuid 在 chroot 里写 /proc/self/loginuid 失败会让整条会话打开失败，
// 表现就是「密码验证通过、随即断开」。降为 optional 是容器场景的通行做法。
//
// 两处都要有：customize.sh 管新装，service.sh 管每次开机 ——
// 后者才是「已装用户重刷一次 ZIP 就能修好」的关键。
func TestMagiskScriptsDowngradePamLoginuid(t *testing.T) {
	const sedSnippet = `s/^session[[:space:]]+required[[:space:]]+pam_loginuid\.so/session optional pam_loginuid.so/`

	for _, name := range []string{"service.sh", "customize.sh"} {
		text := readMagiskScript(t, name)
		if !strings.Contains(text, sedSnippet) {
			t.Fatalf("%s 必须把 pam_loginuid 从 required 降为 optional", name)
		}
		// 守卫必须是文件存在性：Alpine 没有 /etc/pam.d/sshd，这样天然是空操作，
		// 不需要再引入一处 flavor 判断（多一处就多一处忘了同步的地方）。
		if !strings.Contains(text, `if [ -f /etc/pam.d/sshd ]; then`) {
			t.Fatalf("%s 的 PAM 改动必须用 [ -f /etc/pam.d/sshd ] 做守卫", name)
		}
	}

	// 刻意不用 UsePAM no：Debian 有过「构建时没链上 crypt，UsePAM=no 下正确密码
	// 也一律被拒」的先例（Debian bug #1142354），风险比降级 pam_loginuid 大
	// —— 故障会从「连上就断」升级成「根本登不上」。
	for _, name := range []string{"service.sh", "customize.sh"} {
		text := readMagiskScript(t, name)
		for i, line := range strings.Split(text, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(trimmed, "UsePAM no") {
				t.Fatalf("%s:%d 不得写 UsePAM no: %s", name, i+1, trimmed)
			}
		}
	}

	// issue #100：只把 pam_loginuid 降级【不够】。用户重刷模块后仍然连不上，
	// 日志卡在了下一个模块：
	//   sh: 1: cannot create /run/motd.dynamic.new: Required key not available
	//   PAM: pam_open_session(): Error in service module
	// 那是 pam_motd 跑 /etc/update-motd.d/ 时写 /run 拿到 ENOKEY。
	//
	// 逐个模块 patch 是打地鼠 —— loginuid 要写 /proc、keyinit 要建内核 keyring、
	// motd 要写 /run、limits 要 setrlimit、common-session 里的 systemd 模块要连 D-Bus，
	// 在 Android chroot 里每一个都可能失败，修好一个下个用户换台机器又报一个新的。
	//
	// 所以两个脚本都必须把【session 栈整段】替换成恒成功的 pam_permit。
	// auth / account 栈保持不动是关键：密码校验仍走 PAM，就不依赖 sshd 自己的 crypt，
	// 从而绕开上面那条 UsePAM no 的风险。
	for _, name := range []string{"service.sh", "customize.sh"} {
		text := readMagiskScript(t, name)
		for _, snippet := range []string{
			// 替换动作本身
			`print "session required pam_permit.so"; emitted = 1`,
			// @include common-session 也必须被删掉，否则里面的 required 模块照样能掐断会话
			`/^[[:space:]]*@include[[:space:]]+common-session/`,
			// 改人家的 PAM 配置必须留备份
			`/etc/pam.d/sshd.daidai-bak`,
			// 幂等：service.sh 每次开机都跑，不能每次都再替换一遍
			`^session[[:space:]]+required[[:space:]]+pam_permit\.so[[:space:]]*$`,
		} {
			if !strings.Contains(text, snippet) {
				t.Fatalf("%s 必须把 sshd 的 PAM session 栈整段换成 pam_permit（issue #100），缺少片段: %q", name, snippet)
			}
		}

		// auth 栈绝不能被一起换掉 —— 换了就等于没人校验密码了。
		if strings.Contains(text, `print "auth required pam_permit.so"`) {
			t.Fatalf("%s 不得替换 PAM 的 auth 栈：那会让任何密码都能登录", name)
		}
	}
}

// chpasswd 在 Debian 上走 PAM，失败时 root 会保持锁定串、密码登录 100% 被拒，
// 而原来两处都是 2>/dev/null 且不看退出码。必须回读校验。
func TestMagiskScriptsVerifyPasswordHash(t *testing.T) {
	for _, name := range []string{"service.sh", "customize.sh"} {
		text := readMagiskScript(t, name)
		// 用 awk 直接读文件：musl 的 getent 不支持 shadow 数据库，
		// 写成 getent shadow 会在 Alpine 上恒为空、把两个 flavor 都误判成失败。
		if !strings.Contains(text, `awk -F: -v u="${SSH_USER}" '$1==u{print $2}' /etc/shadow`) {
			t.Fatalf("%s 必须回读 /etc/shadow 确认密码真的写进去了", name)
		}
		assertNotInExecutableLines(t, name, text, "getent shadow",
			"不得用 getent shadow 判断密码（musl 的 getent 不支持 shadow 数据库）")
		if !strings.Contains(text, `openssl passwd -6`) {
			t.Fatalf("%s 缺少 chpasswd 失败时的 openssl passwd -6 兜底", name)
		}
	}
}

// 重装时必须按 /proc/<pid>/root 归属把落在旧 rootfs 里的进程清干净。
// 两条 pkill 是按命令行匹配的，容器里 sshd 的 argv 是 /usr/sbin/sshd，一条都命中不了，
// 于是 rm -rf 之后它变成根目录已删除的孤儿进程，监听 socket 活到下次重启。
func TestMagiskCustomizeScriptKillsProcessesRootedInOldRootfs(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		`_rootfs_real=$(readlink -f "$rootfs" 2>/dev/null || echo "$rootfs")`,
		`_proc_root=$(readlink "$_proc_dir/root" 2>/dev/null) || continue`,
		`kill -9 "${_proc_dir#/proc/}" 2>/dev/null || true`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("customize.sh 缺少按 chroot 归属清理进程的片段: %q", snippet)
		}
	}

	// 必须排在删 rootfs 之前，否则清的是一个已经不存在的目录。
	killIdx := strings.Index(text, `_rootfs_real=$(readlink -f "$rootfs"`)
	rmIdx := strings.Index(text, `rm -rf "$rootfs"`)
	if killIdx < 0 || rmIdx < 0 {
		t.Fatalf("customize.sh 找不到定位锚点 (kill=%d rm=%d)", killIdx, rmIdx)
	}
	if killIdx > rmIdx {
		t.Fatal("按 chroot 归属清理进程必须排在删除旧 rootfs 之前")
	}
}

// 安装期必须验一次 SSH：原来的清单只有 python3/node/npm/git/bash，
// openssh-server 装没装成完全没人管，用户会看到「安装完成！」然后 SSH 是死的。
func TestMagiskCustomizeScriptVerifiesSshAfterInstall(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		`for item in sshd-binary sshd-config sshd-privsep-user sshd-hostkey root-password sshd-config-test; do`,
		`missing_ssh`,
		// 特权分离用户缺失时 sshd 会直接 fatal，而 fatal 只走 stderr
		`useradd --system --no-create-home --home-dir /run/sshd --shell /usr/sbin/nologin sshd`,
		// Debian 的 openssh-server 可能停在「已解包未配置」，conffile 却照样落盘
		`--reinstall openssh-server`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("customize.sh 缺少 SSH 验收片段: %q", snippet)
		}
	}

	// SSH 自检失败【不能】中止安装：SSH 只是排障通道，面板 Web 不依赖它。
	sshIdx := strings.Index(text, `missing_ssh=""`)
	if sshIdx < 0 {
		t.Fatal("customize.sh 找不到 SSH 验收段")
	}
	tail := text[sshIdx:]
	gate := strings.Index(tail, `if [ "$INSTALL_DEPS_OK" != "1" ]; then`)
	if gate < 0 {
		t.Fatal("customize.sh 找不到成功提示的开关")
	}
	if strings.Contains(tail[:gate], `abort "! 安装已中止`) {
		t.Fatal("SSH 自检失败不应 abort 整个安装（面板 Web 不依赖 SSH）")
	}

	// 必须排在装依赖之后，否则验的是空容器。
	installIdx := strings.Index(text, `"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" /tmp/daidai-install-deps.sh`)
	if installIdx < 0 || sshIdx < installIdx {
		t.Fatalf("SSH 验收必须排在装依赖之后 (install=%d ssh=%d)", installIdx, sshIdx)
	}
}

// SSH 不通时用户的第一反应是点动作按钮，那里必须能拿到线索。
func TestMagiskActionScriptReportsSshStatus(t *testing.T) {
	text := readMagiskScript(t, "action.sh")

	for _, snippet := range []string{
		`SSH_PORT_INFO=$(netstat -ltn 2>/dev/null | grep ":${SSH_PORT}\b" | head -n2)`,
		`ui_print "--- 容器 SSH ---"`,
		`/usr/sbin/sshd -t 2>&1 | sed "s/^/sshd -t: /"`,
		`SSHD_LOG="$rootfs/app/Dumb-Panel/sshd.log"`,
		`CTR_SERVICE_LOG="$rootfs/app/Dumb-Panel/service.log"`,
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("action.sh 缺少 SSH 状态片段: %q", snippet)
		}
	}
}

// 装依赖失败时的报错必须能区分 DNS / 镜像源 / 下载中断，
// 且分流只作用于 Debian —— Alpine 的文案与行为要保持原样。
func TestMagiskCustomizeScriptSplitsDepsFailureHintByFlavor(t *testing.T) {
	text := readMagiskCustomizeScript(t)

	for _, snippet := range []string{
		"容器内 DNS 解析失败",
		"apt 的降权用户 _apt 不能",
		"的 apt-get update 全部失败",
		"失败发生在下载 / 解包阶段",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("expected customize.sh 装依赖失败提示 to contain %q", snippet)
		}
	}

	// 分流必须在「运行时验证未通过」这一段里，且 Alpine 走原来那句
	failIdx := strings.Index(text, `if [ -n "$missing_runtimes" ]; then`)
	hintIdx := strings.Index(text, "容器内 DNS 解析失败")
	alpineIdx := strings.Index(text, "! 请检查网络（公司 / 校园网被墙时可挂 VPN），然后重新安装本模块。")
	if failIdx < 0 || hintIdx < 0 || alpineIdx < 0 {
		t.Fatalf("customize.sh 找不到失败提示分流的定位锚点 (fail=%d hint=%d alpine=%d)", failIdx, hintIdx, alpineIdx)
	}
	if hintIdx < failIdx || alpineIdx < failIdx {
		t.Fatal("装依赖失败的分流提示必须在运行时验证未通过的分支里")
	}
}

// ---- Node 24 升级相关断言（Alpine 3.23 / Debian 官方二进制 / 大版本校验）------------
//
// 背景：两个 flavor 的容器 Node 原来都是 18.x（2025-04-30 已停止维护）。Alpine 升到 3.23
// 直接拿到仓库里的 24.x；Debian bookworm 的 apt 只有 18，改装 nodejs.org 官方 v24 二进制。
// 这一组同样是静态断言，但尽量按分支 / 按命令 / 按可执行行截取，不让注释里的字样满足断言。

// magiskExecLines 返回去掉首尾空白后「真正会被执行」的行：跳过空行与 # 注释行。
// 行号丢了，但相对顺序保留，足够做「A 必须排在 B 之前」这类断言。
func magiskExecLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// magiskIndexOfLine 在 lines[from:] 里找第一条满足 match 的行，找不到返回 -1。
func magiskIndexOfLine(lines []string, from int, match func(string) bool) int {
	if from < 0 {
		from = 0
	}
	for i := from; i < len(lines); i++ {
		if match(lines[i]) {
			return i
		}
	}
	return -1
}

// 整行精确匹配：else / fi / if 这类行必须用它，否则会被注释或别处的同名片段勾到。
func magiskLineEquals(want string) func(string) bool {
	return func(line string) bool { return strings.TrimSpace(line) == want }
}

func magiskLineContains(sub string) func(string) bool {
	return func(line string) bool { return strings.Contains(line, sub) }
}

func magiskHasLine(lines []string, want string) bool {
	return magiskIndexOfLine(lines, 0, magiskLineEquals(want)) >= 0
}

// magiskInstallCommands 把 script 里以 prefix（如 "apk add"）开头的命令
// 连同 `\` 续行拼成一条，返回每条命令的包名列表：跳过 - 开头的参数，
// 遇到 || / && / ; / 重定向就截止（后面不是包名）。
// 注释行不参与 —— 脚本注释里为了写清两个 flavor 的包名对应关系，本来就会提到 nodejs / npm。
// 它只认行首前缀；要断言「某个包【没有】被 apt 装」时用 magiskAptInstallCommands，那边认得更多写法。
func magiskInstallCommands(script, prefix string) [][]string {
	lines := strings.Split(script, "\n")
	var cmds [][]string
	for i := 0; i < len(lines); i++ {
		joined := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(joined, prefix+" ") {
			continue
		}
		for strings.HasSuffix(joined, `\`) && i+1 < len(lines) {
			i++
			joined = strings.TrimSuffix(joined, `\`) + " " + strings.TrimSpace(lines[i])
		}
		pkgs := []string{}
		for _, field := range strings.Fields(strings.TrimPrefix(joined, prefix)) {
			if field == "||" || field == "&&" || field == ";" ||
				strings.HasPrefix(field, ">") || strings.HasPrefix(field, "<") || strings.HasPrefix(field, "2>") {
				break
			}
			if strings.HasPrefix(field, "-") {
				continue
			}
			pkgs = append(pkgs, field)
		}
		cmds = append(cmds, pkgs)
	}
	return cmds
}

// magiskAptInstallCommands 找出 script 里所有 apt-get / apt 的 install / reinstall 调用，返回每条调用的包名列表
// （包名去掉 =版本、/发行版、:架构 后缀）。Debian 侧「不得再装 nodejs / npm」靠它判断，所以它不能只认
// 行首的 `apt-get install`（那是 magiskInstallCommands 的规则）—— 下面这些写法同样会把 apt 的 Node 18 装回来：
//   - 选项写在子命令前：apt-get -y --no-install-recommends install nodejs npm
//   - 用 apt 而不是 apt-get：apt install -y nodejs
//   - 前面带环境变量赋值 / env / command：DEBIAN_FRONTEND=noninteractive apt-get install -y npm
//   - 跟在 if / ! / && / || / ; / then / do 后面：if ! apt-get install -y npm; then
//
// 只在「命令位置」认 apt-get / apt：echo "... apt-get install nodejs ..." 这种字符串里的字样不算。
// 包名截止规则与 magiskInstallCommands 一致：遇到 || / && / ; / | / 重定向就停 ——
// `apt-get install ca-certificates || echo "... pip / npm / git ..."` 里 echo 的 npm 不是包名。
// 注释行不参与。
func magiskAptInstallCommands(script string) [][]string {
	separators := map[string]bool{
		";": true, "&&": true, "||": true, "|": true, "&": true, "!": true,
		"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true,
		"{": true, "(": true, "time": true,
	}
	wrappers := map[string]bool{"env": true, "command": true, "exec": true, "nohup": true, "sudo": true}
	takesValue := map[string]bool{
		"-o": true, "--option": true, "-c": true, "--config-file": true,
		"-t": true, "--target-release": true, "--default-release": true,
	}
	envAssign := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	isStop := func(field string) bool {
		if field == "||" || field == "&&" || field == ";" || field == "|" || field == "&" {
			return true
		}
		for _, p := range []string{">", "<", "1>", "2>", "&>"} {
			if strings.HasPrefix(field, p) {
				return true
			}
		}
		return false
	}

	lines := strings.Split(script, "\n")
	var cmds [][]string
	for i := 0; i < len(lines); i++ {
		joined := strings.TrimSpace(lines[i])
		if joined == "" || strings.HasPrefix(joined, "#") {
			continue
		}
		for strings.HasSuffix(joined, `\`) && i+1 < len(lines) {
			i++
			joined = strings.TrimSuffix(joined, `\`) + " " + strings.TrimSpace(lines[i])
		}
		fields := strings.Fields(joined)
		atStart := true
		for j := 0; j < len(fields); j++ {
			field := fields[j]
			if separators[field] {
				atStart = true
				continue
			}
			if !atStart {
				atStart = strings.HasSuffix(field, ";")
				continue
			}
			word := strings.TrimSuffix(field, ";")
			// 环境变量赋值、env / command 这类前缀（及其选项）之后仍是命令位置
			if envAssign.MatchString(word) || wrappers[word] || (j > 0 && wrappers[fields[j-1]] && strings.HasPrefix(word, "-")) {
				continue
			}
			base := word[strings.LastIndex(word, "/")+1:]
			if (base != "apt-get" && base != "apt") || strings.HasSuffix(field, ";") {
				atStart = strings.HasSuffix(field, ";")
				continue
			}

			// fields[j] 是 apt-get / apt：先找子命令（跳过选项及其取值），install / reinstall 再收包名
			sub := ""
			pkgs := []string{}
			k := j + 1
			for ; k < len(fields); k++ {
				arg := fields[k]
				if isStop(arg) {
					break
				}
				ends := strings.HasSuffix(arg, ";")
				arg = strings.Trim(strings.TrimSuffix(arg, ";"), `"'`)
				switch {
				case arg == "":
				case strings.HasPrefix(arg, "-"):
					if takesValue[arg] && !ends {
						k++
					}
				case sub == "":
					sub = arg
				case sub == "install" || sub == "reinstall":
					if cut := strings.IndexAny(arg, "=/:"); cut > 0 {
						arg = arg[:cut]
					}
					pkgs = append(pkgs, arg)
				}
				if ends {
					break
				}
			}
			if sub == "install" || sub == "reinstall" {
				cmds = append(cmds, pkgs)
			}
			// k 停在截止字段（|| / ; 结尾 / 重定向）上，它之后是下一条命令的开头
			j = k
			atStart = true
		}
	}
	return cmds
}

// magiskFlavorSelectionBranches 取 customize.sh 顶部「按 flavor 定容器参数」那段
// if debian / else / fi，分别返回两个分支体的可执行行。
// 用 CTR_SHELL 赋值确认取到的就是这一段 —— 后面 DNS、装依赖几处都有一模一样的判断写法。
func magiskFlavorSelectionBranches(t *testing.T, text string) (debian, alpine []string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	ifIdx := magiskIndexOfLine(lines, 0, magiskLineEquals(`if [ "$FLAVOR" = "debian" ]; then`))
	if ifIdx < 0 {
		t.Fatal("customize.sh 找不到 flavor 参数选择段的 if 行")
	}
	elseIdx := magiskIndexOfLine(lines, ifIdx+1, magiskLineEquals("else"))
	if elseIdx < 0 {
		t.Fatal("customize.sh 的 flavor 参数选择段缺少 else 分支")
	}
	fiIdx := magiskIndexOfLine(lines, elseIdx+1, magiskLineEquals("fi"))
	if fiIdx < 0 {
		t.Fatal("customize.sh 的 flavor 参数选择段缺少收尾的 fi")
	}
	debian = magiskExecLines(strings.Join(lines[ifIdx+1:elseIdx], "\n"))
	alpine = magiskExecLines(strings.Join(lines[elseIdx+1:fiIdx], "\n"))
	if !magiskHasLine(debian, "CTR_SHELL=/bin/bash") || !magiskHasLine(alpine, "CTR_SHELL=/bin/ash") {
		t.Fatalf("customize.sh 第一处 flavor 判断不是容器参数选择段（debian=%q alpine=%q）", debian, alpine)
	}
	return debian, alpine
}

// R1：Alpine flavor 的 rootfs 升到 3.23.5。
// 3.23 是目前唯一一个 nodejs 为 24.x、python3 又还在面板支持范围（3.10–3.12）内的 Alpine 版本：
// 3.22 的 nodejs 还是 22，3.24 的 python3 已经是 3.14。
func TestMagiskCustomizeScriptAlpineFlavorUsesAlpine323(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	_, alpine := magiskFlavorSelectionBranches(t, text)

	for _, want := range []string{
		`ROOTFS_URL="https://mirrors.nju.edu.cn/alpine/v3.23/releases/aarch64/alpine-minirootfs-3.23.5-aarch64.tar.gz"`,
		`CTR_NAME="Alpine 3.23"`,
		// 装依赖下载量跟着 rootfs 版本走：这是按 3.23 aarch64 的 APKINDEX 对 apk 清单统计的包体积（新装约 145 MiB，没在真机上实装量过），
		// 会原样打进装依赖失败提示。和 rootfs 锁在一起，换 rootfs 时就得重新量 ——
		// 原来的「约 50MB」就是 rootfs 换过几轮都没人重量，偏小了三倍。
		`CTR_DEPS_SIZE="约 150MB"`,
	} {
		if !magiskHasLine(alpine, want) {
			t.Fatalf("customize.sh 的 Alpine 参数分支缺少可执行行 %q，实际分支体=%q", want, alpine)
		}
	}
	// README 里给用户看的两处下载量必须和安装失败提示里的是同一个数
	var alpineDepsMB string
	for _, line := range alpine {
		if m := regexp.MustCompile(`^CTR_DEPS_SIZE="约 ([0-9]+)MB"$`).FindStringSubmatch(line); m != nil {
			alpineDepsMB = m[1]
		}
	}
	readme := readMagiskScript(t, "README.md")
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`Alpine 侧累计约 ` + alpineDepsMB + ` MB`),
		regexp.MustCompile(`(?m)^\| Alpine \| [^|\n]*\| 约 ` + alpineDepsMB + ` MB，`),
	} {
		if alpineDepsMB == "" || !want.MatchString(readme) {
			t.Fatalf("Magisk/README.md 的 Alpine 装依赖下载量与 customize.sh 的 CTR_DEPS_SIZE（约 %sMB）不一致，找不到 %q", alpineDepsMB, want)
		}
	}
	// 这个数是按 APKINDEX 的包体积统计出来的，没在真机上实装测过。README 紧跟在数字后面的括注
	// 必须写明「按 APKINDEX 统计」，不能写成「实测」—— 同一份 README 把 Debian 的 800 MB
	// 特意标了「未重新实测」，Alpine 这边写「实测」会让用户误以为是真机测出来的。
	labelRe := regexp.MustCompile(`Alpine 侧累计约 ` + alpineDepsMB + ` MB（([^（）\n]*)）`)
	labelMatch := labelRe.FindStringSubmatch(readme)
	if labelMatch == nil {
		t.Fatalf("Magisk/README.md 的「Alpine 侧累计约 %s MB」后面缺少说明数字来源的括注，找不到 %q", alpineDepsMB, labelRe)
	}
	if label := labelMatch[1]; !strings.Contains(label, "APKINDEX") || strings.Contains(label, "实测") {
		t.Fatalf("Magisk/README.md 的 Alpine 装依赖下载量括注必须写明按 APKINDEX 统计、不得写成「实测」，实际=%q", label)
	}
	// 3.18 已 EOL，仓库里只有 Node 18：残留在任何可执行行里都是回归
	for _, forbidden := range []string{"v3.18", "3.18.9", `"Alpine 3.18"`} {
		assertNotInExecutableLines(t, "customize.sh", text, forbidden, "customize.sh 不得再引用 Alpine 3.18")
	}

	// Alpine 上的 Node 24 就来自 3.23 仓库，apk 清单里 nodejs / npm 必须还在。
	alpineDeps := heredocBlock(t, text, "DEPS_PKG_ALPINE_EOF")
	var pkgs []string
	for _, cmd := range magiskInstallCommands(alpineDeps, "apk add") {
		pkgs = append(pkgs, cmd...)
	}
	for _, want := range []string{"nodejs", "npm", "python3", "shadow", "procps"} {
		if !magiskHasLine(pkgs, want) {
			t.Fatalf("customize.sh Alpine 装依赖脚本的 apk add 清单缺少 %q，解析结果=%q", want, pkgs)
		}
	}
	// 反过来，nodejs.org 的官方包是 glibc 构建，在 musl 上根本跑不起来，Alpine 分支不得去下载它。
	for _, forbidden := range []string{"nodejs.org/dist", "npmmirror.com/-/binary/node", "NODE_SHA256"} {
		assertNotInExecutableLines(t, "customize.sh(DEPS_PKG_ALPINE_EOF)", alpineDeps, forbidden,
			"Alpine(musl) 不得下载 nodejs.org 的 glibc 官方构建")
	}
}

// R2：Debian 的 apt 清单不再装 nodejs / npm（bookworm 仓库只有 Node 18）。
// 按命令解析出包名再判断，不做整段 Contains：注释里为了写清对应关系本来就会提到它俩。
func TestMagiskCustomizeScriptDebianAptNoLongerInstallsNode(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	block := heredocBlock(t, text, "DEPS_PKG_DEBIAN_EOF")

	// 用按命令位置识别的解析器，不用行首前缀匹配：`apt-get -y install nodejs`、`apt install npm`、
	// `DEBIAN_FRONTEND=noninteractive apt-get install nodejs` 都得认得出来。
	// 装回来的 Node 18 落在 /usr/bin，运行期 PATH 里 /usr/local/bin 排在它前面，大版本校验看到的多半仍是 24，指望不上。
	cmds := magiskAptInstallCommands(block)
	var batch []string
	for _, pkgs := range cmds {
		for _, pkg := range pkgs {
			if pkg == "nodejs" || pkg == "npm" {
				t.Fatalf("customize.sh Debian 分支的 apt 安装不得再装 %s（bookworm 只有 Node 18，已改装官方 v24 二进制）: %q", pkg, pkgs)
			}
		}
		if magiskHasLine(pkgs, "python3-venv") {
			batch = pkgs
		}
	}
	if batch == nil {
		t.Fatalf("customize.sh Debian 分支找不到 apt 批量安装命令（含 python3-venv），解析结果=%q", cmds)
	}
	// 官方 Node 要靠 curl 走 https 下载：这两个包必须还在批量清单里，否则 Node 段必然失败
	for _, want := range []string{"curl", "ca-certificates", "python3", "git"} {
		if !magiskHasLine(batch, want) {
			t.Fatalf("customize.sh Debian 批量安装清单缺少 %q，解析结果=%q", want, batch)
		}
	}
}

// magiskAptInstallCommands 自己的门禁：上面「apt 不得再装 nodejs / npm」的断言只和这个解析器一样强。
// 每种能把 nodejs / npm 装回来的写法都必须被解析出来；echo 文案、|| 之后的命令、注释里的字样不能被当成包名。
func TestMagiskAptInstallCommandsParser(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   []string
	}{
		{"当前的多行批量写法", "apt-get install -y --no-install-recommends \\\n  curl nodejs \\\n  git", []string{"curl", "nodejs", "git"}},
		{"选项写在子命令前", "apt-get -y --no-install-recommends install nodejs npm", []string{"nodejs", "npm"}},
		{"apt 而不是 apt-get", "apt install -y nodejs", []string{"nodejs"}},
		{"环境变量前缀", "DEBIAN_FRONTEND=noninteractive apt-get install -y npm", []string{"npm"}},
		{"缩进 + || true", "  apt-get -y install npm || true", []string{"npm"}},
		{"if ! ...; then", "if ! apt-get install -y nodejs; then\n  echo x\nfi", []string{"nodejs"}},
		{"&& 之后", "apt-get update && apt-get install -y npm", []string{"npm"}},
		{"-o 选项取值 + 版本钉", "apt-get -o Dpkg::Options::=--force-confold install -y nodejs=18.19.0+dfsg-6", []string{"nodejs"}},
		{"绝对路径 + reinstall", "/usr/bin/apt-get reinstall -y npm", []string{"npm"}},
		{"|| 之后的 echo 文案不是包名", "apt-get install -y --no-install-recommends ca-certificates || \\\n  echo \"[daidai] pip / npm / git 的 https 可能不可用\"", []string{"ca-certificates"}},
		{"echo 里提到 apt-get install 不算", "echo \"[daidai] 请手动 apt-get install nodejs npm\"", nil},
		{"重定向之后不是包名", "apt-get install -y curl >/dev/null 2>&1 && echo npm", []string{"curl"}},
		{"非 install 子命令", "apt-get update\napt-get -y remove nodejs", nil},
		{"注释行", "# apt-get install nodejs npm", nil},
	}
	for _, tc := range cases {
		var got []string
		for _, pkgs := range magiskAptInstallCommands(tc.script) {
			got = append(got, pkgs...)
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s: magiskAptInstallCommands(%q) 解析出的包名=%q，期望=%q", tc.name, tc.script, got, tc.want)
		}
	}
}

// R2：Debian 的 Node 装的是 nodejs.org 官方 v24 二进制，npmmirror 优先、nodejs.org 兜底，
// 每个源都 SHA256 校验后才解压到 /usr/local，结论追加写进状态文件供宿主侧分流报错。
func TestMagiskCustomizeScriptDebianInstallsOfficialNode24(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	lines := magiskExecLines(heredocBlock(t, text, "DEPS_PKG_DEBIAN_EOF"))

	// 版本号与哈希必须是整行赋值，而不是注释里提一句。
	// 升级 Node 时这三行要和 customize.sh 一起改：哈希取自 nodejs.org/dist/v<版本>/SHASUMS256.txt，
	// 只改版本号不改哈希的话，两个源都会校验失败，Debian 版必然装不上。
	for _, want := range []string{
		"NODE_VERSION=24.21.0",
		`NODE_TARBALL="node-v${NODE_VERSION}-linux-arm64.tar.gz"`,
		"NODE_SHA256=724282c3b43aec998aa9527380465b45d229e021b58035f5f4f63095eabfe5d5",
	} {
		if !magiskHasLine(lines, want) {
			t.Fatalf("customize.sh Debian 分支缺少可执行行 %q", want)
		}
	}

	// 批量安装的锚点必须是那条命令的首行整行。它上面还有一条单独预装根证书的
	// `apt-get install -y --no-install-recommends ca-certificates || \`，按「apt-get install 开头、\ 结尾」
	// 宽松匹配会先勾到它：整段 Node 下载挪到两条 apt 之间（debian-slim 此时还没有 curl），断言照样通过。
	batchIdx := magiskIndexOfLine(lines, 0, magiskLineEquals(`apt-get install -y --no-install-recommends \`))
	npmmirrorIdx := magiskIndexOfLine(lines, 0, magiskLineContains(`"https://registry.npmmirror.com/-/binary/node/v${NODE_VERSION}/${NODE_TARBALL}"`))
	nodejsOrgIdx := magiskIndexOfLine(lines, 0, magiskLineContains(`"https://nodejs.org/dist/v${NODE_VERSION}/${NODE_TARBALL}"`))
	curlIdx := magiskIndexOfLine(lines, 0, magiskLineContains(`curl -fL `))
	// 校验行整行匹配：只认「if ! ...; then」这种校验不过就进分支的写法，下面再锁住分支里必须 continue。
	shaIdx := magiskIndexOfLine(lines, 0, magiskLineEquals(`if ! echo "${NODE_SHA256}  /tmp/${NODE_TARBALL}" | sha256sum -c - >/dev/null 2>&1; then`))
	tarIdx := magiskIndexOfLine(lines, 0, magiskLineContains(`tar -xzf "/tmp/${NODE_TARBALL}" -C /usr/local`))
	if batchIdx < 0 || npmmirrorIdx < 0 || nodejsOrgIdx < 0 || curlIdx < 0 || shaIdx < 0 || tarIdx < 0 {
		t.Fatalf("customize.sh Debian 分支的 Node 下载段不完整 (batch=%d npmmirror=%d nodejs.org=%d curl=%d sha=%d tar=%d)",
			batchIdx, npmmirrorIdx, nodejsOrgIdx, curlIdx, shaIdx, tarIdx)
	}
	// 确认锚到的就是整批安装：沿续行拼出整条命令，python3-venv / curl / ca-certificates 都得在里面。
	// 以后改写这条命令的首行时，这里会明确报错，而不是悄悄锚到别的 apt 调用上。
	var batchFields []string
	for i := batchIdx; i < len(lines); i++ {
		batchFields = append(batchFields, strings.Fields(strings.TrimSuffix(lines[i], `\`))...)
		if !strings.HasSuffix(lines[i], `\`) {
			break
		}
	}
	for _, want := range []string{"python3-venv", "curl", "ca-certificates"} {
		if !magiskHasLine(batchFields, want) {
			t.Fatalf("Node 下载段的顺序锚点没有落在 apt 整批安装上（缺 %q），实际命令=%q", want, batchFields)
		}
	}
	// 顺序：apt 批量安装（带来 curl 与根证书）-> curl 下载 / npmmirror -> nodejs.org
	if !(batchIdx < curlIdx && batchIdx < npmmirrorIdx && npmmirrorIdx < nodejsOrgIdx) {
		t.Fatalf("Node 下载源必须 npmmirror 优先、nodejs.org 兜底，且下载排在 apt 批量安装之后 (batch=%d curl=%d npmmirror=%d nodejs.org=%d)",
			batchIdx, curlIdx, npmmirrorIdx, nodejsOrgIdx)
	}
	// 下载必须带低速中止。--connect-timeout 只管建连：连上之后对端停发却不断开（代理 / CDN 半挂）时，
	// curl 永远不会自己结束，安装界面无限挂在「正在下载」，换源与 NODE=FAIL 分流都走不到。
	// 卡死判定窗口设上限：几个小时才判超时，和没设没有区别。
	for _, flag := range []string{"--speed-limit", "--speed-time"} {
		m := regexp.MustCompile(regexp.QuoteMeta(flag) + ` ([0-9]+)(\s|$)`).FindStringSubmatch(lines[curlIdx])
		if m == nil {
			t.Fatalf("Node 下载的 curl 缺少 %s <数值>（防连接卡死让安装界面无限挂起）: %s", flag, lines[curlIdx])
		}
		n, _ := strconv.Atoi(m[1])
		if n <= 0 || (flag == "--speed-time" && n > 300) {
			t.Fatalf("Node 下载的 curl %s %d 不起作用（--speed-limit 须 > 0，--speed-time 须在 1..300 秒）: %s", flag, n, lines[curlIdx])
		}
	}
	// 顺序：下载 -> 校验 -> 解压。先解压后校验等于把一个坏包装进了 /usr/local
	if !(curlIdx < shaIdx && shaIdx < tarIdx) {
		t.Fatalf("Node 安装必须按「下载 -> SHA256 校验 -> 解压」的顺序 (curl=%d sha=%d tar=%d)", curlIdx, shaIdx, tarIdx)
	}
	if !strings.Contains(lines[shaIdx], "${NODE_SHA256}") {
		t.Fatalf("SHA256 校验必须用写死的 NODE_SHA256: %s", lines[shaIdx])
	}
	// 光排在中间不够，校验必须真的拦得住：不通过就 continue 换下一个源，这个 if 要在解压之前闭合。
	// 改成 `... | sha256sum -c - || echo 校验不通过`、或者分支里没有 continue，
	// 哈希对不上的包（镜像同步不全 / 内容被替换）照样会落到下面的 tar 解压进 /usr/local。
	shaFiIdx := magiskShellBlockEnd(lines, shaIdx)
	if shaFiIdx < 0 || shaFiIdx > tarIdx {
		t.Fatalf("SHA256 校验的 if 必须在解压之前闭合 (sha=%d fi=%d tar=%d)", shaIdx, shaFiIdx, tarIdx)
	}
	if !magiskHasLine(lines[shaIdx+1:shaFiIdx], "continue") {
		t.Fatalf("SHA256 校验不通过时必须 continue 换下一个源，不能继续往下解压，实际分支体=%q", lines[shaIdx+1:shaFiIdx])
	}
	for _, flag := range []string{"--strip-components=1", "--no-same-owner"} {
		if !strings.Contains(lines[tarIdx], flag) {
			t.Fatalf("Node 解压命令缺少 %s: %s", flag, lines[tarIdx])
		}
	}

	// 状态文件：MIRROR= 用 > 首写，NODE= 只能 >> 追加，且必须写在 MIRROR= 之后，
	// 否则镜像源结论会被覆盖，宿主侧分不清是 apt 挂了还是 Node 没下下来。
	//
	// 追加判定按「正向」做：这一行里必须有 >> 追加到状态文件，而且去掉这些追加重定向之后，
	// 状态文件路径不能再在别处出现。只数「> /tmp/...」与「>> /tmp/...」的个数是否相等挡不住
	// 不带空格的 >/tmp/...（两边都数到 0），也挡不住 1>/tmp、>|/tmp、不带 -a 的 tee 这类覆盖写法。
	appendToStatus := regexp.MustCompile(`>>\s*/tmp/daidai-deps-status`)
	var mirrorWrites, nodeWrites []int
	for i, line := range lines {
		if !strings.Contains(line, "/tmp/daidai-deps-status") {
			continue
		}
		switch {
		case strings.Contains(line, "MIRROR="):
			mirrorWrites = append(mirrorWrites, i)
		case strings.Contains(line, "NODE="):
			if !appendToStatus.MatchString(line) ||
				strings.Contains(appendToStatus.ReplaceAllString(line, ""), "/tmp/daidai-deps-status") {
				t.Fatalf("NODE= 状态必须用 >> 追加写入，不能覆盖 MIRROR= 那一行: %s", line)
			}
			nodeWrites = append(nodeWrites, i)
		}
	}
	if len(mirrorWrites) == 0 || len(nodeWrites) == 0 {
		t.Fatalf("customize.sh Debian 分支缺少状态文件写入 (MIRROR=%v NODE=%v)", mirrorWrites, nodeWrites)
	}
	if nodeWrites[0] < mirrorWrites[len(mirrorWrites)-1] {
		t.Fatalf("NODE= 状态必须写在 MIRROR= 首写之后 (MIRROR=%v NODE=%v)", mirrorWrites, nodeWrites)
	}
	for _, want := range []string{`"NODE=OK"`, `"NODE=FAIL"`} {
		found := false
		for _, i := range nodeWrites {
			if strings.Contains(lines[i], want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("customize.sh Debian 分支缺少状态写入 %s", want)
		}
	}
}

// R3：运行时验证在「能执行」之外还要校验 node 大版本，两个 flavor 共用同一份清单。
func TestMagiskCustomizeScriptVerifiesNodeMajorVersion(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	lines := strings.Split(text, "\n")

	reqIdx := magiskIndexOfLine(lines, 0, magiskLineEquals("REQUIRED_NODE_MAJOR=24"))
	// 运行时验证那次 ruri 调用：起始行后面紧跟着清单循环，两行一起定位，避免勾到 SSH 自检那次
	startIdx := -1
	for i := 0; i+1 < len(lines); i++ {
		if lines[i] == `"$RURIMA" ruri -p -N -S -A "$rootfs" "$CTR_SHELL" -c '` &&
			lines[i+1] == "  for c in python3 node npm git bash; do" {
			startIdx = i
			break
		}
	}
	if reqIdx < 0 || startIdx < 0 {
		t.Fatalf("customize.sh 找不到 REQUIRED_NODE_MAJOR=24 或运行时验证调用 (req=%d start=%d)", reqIdx, startIdx)
	}
	endIdx := magiskIndexOfLine(lines, startIdx+1, func(line string) bool {
		return strings.HasPrefix(line, `' > "$DEPS_REPORT"`)
	})
	if endIdx < 0 {
		t.Fatal("customize.sh 运行时验证的内联脚本找不到收尾的 ' > \"$DEPS_REPORT\"")
	}
	if reqIdx > startIdx {
		t.Fatal("REQUIRED_NODE_MAJOR 必须在运行时验证之前定义")
	}

	// 内联脚本是单引号包起来传给容器 shell 的：里面任何一个单引号（注释里的也算）
	// 都会提前闭合字符串，后半段变成宿主侧的命令，而错误又被 2>/dev/null 吞掉。
	inline := lines[startIdx+1 : endIdx]
	for i, line := range inline {
		if strings.Contains(line, "'") {
			t.Fatalf("运行时验证的内联脚本第 %d 行出现单引号，会提前闭合 -c 的字符串: %s", i+1, line)
		}
	}
	// 版本号必须在同一次 ruri 调用里输出，不能再多进一次容器
	if magiskIndexOfLine(magiskExecLines(strings.Join(inline, "\n")), 0,
		magiskLineContains(`echo "NODE_VERSION_LINE=$(node --version`)) < 0 {
		t.Fatal("运行时验证的内联脚本必须输出 NODE_VERSION_LINE=<node --version>")
	}

	host := magiskExecLines(strings.Join(lines[endIdx+1:], "\n"))
	at := -1
	next := func(desc string, match func(string) bool) int {
		i := magiskIndexOfLine(host, at+1, match)
		if i < 0 {
			t.Fatalf("运行时验证的宿主侧结构不对：在第 %d 条可执行行之后按顺序找不到 %s", at+1, desc)
		}
		at = i
		return i
	}
	// 两条解析行整行锁死。它们是两个 flavor 把 node --version 变成主版本号的唯一一处：
	// 正则写坏（例如 [0-9][0-9]* 少了 *、丢了 p 标志）或 cut 取错列，node_major 就恒为空，
	// 装着正确 Node 24 的安装也会被判「版本不符」而中止 —— 而真机安装在这里测不了，只能靠这道静态断言。
	// 改这两行时必须先在 busybox sh 里实测（sed / grep / cut / head 都用 busybox applet，报告里带 OK node）：
	// v24.21.0 与 v24.18.1 -> 24 且不判不符；v18.20.4 -> 18 判不符；版本行为空 -> 空、判不符。实测过再改这里的期望值。
	parseIdx := next("NODE_VERSION_LINE 的解析（整行）",
		magiskLineEquals(`node_version_line=$(grep '^NODE_VERSION_LINE=' "$DEPS_REPORT" 2>/dev/null | head -n 1 | cut -d= -f2-)`))
	majorIdx := next("主版本解析 node_major=（整行）",
		magiskLineEquals(`node_major=$(printf '%s\n' "$node_version_line" | sed -n 's/^v\([0-9][0-9]*\)\..*$/\1/p')`))
	next("大版本比对", magiskLineEquals(`if grep -q '^OK node$' "$DEPS_REPORT" 2>/dev/null && [ "$node_major" != "$REQUIRED_NODE_MAJOR" ]; then`))
	next("版本不符时把 node 计入缺失", magiskLineEquals(`missing_runtimes="$missing_runtimes node"`))
	failIdx := next("运行时验证未通过的判断", magiskLineEquals(`if [ -n "$missing_runtimes" ]; then`))
	next("版本不符时打印实际版本", magiskLineContains(`实际 ${node_version_line`))

	// 这段跑在宿主侧，解释它的是管理器自带的 busybox：grep -P 与 \d 都不认
	for _, line := range host[parseIdx:failIdx] {
		for _, bad := range []string{"grep -P", `\d`} {
			if strings.Contains(line, bad) {
				t.Fatalf("Node 版本解析跑在宿主侧 busybox 里，不得使用 %s: %s", bad, line)
			}
		}
	}
	if !strings.Contains(host[majorIdx], "sed -n") {
		t.Fatalf("主版本应从版本行里用 sed 取出（取不出时为空、按不符处理）: %s", host[majorIdx])
	}
}

// R2：Debian 装依赖失败时，「Node 官方包没装上」要能和「apt 镜像源全挂」「下载 / 解包中断」分开报。
// 判断顺序本身是契约：镜像源全挂时 curl 与根证书都装不上，Node 必然连带失败，
// 所以必须先判 MIRROR=FAIL，再判 NODE=FAIL，否则根因会被报成「Node 下载失败」。
func TestMagiskCustomizeScriptSplitsNodeDownloadFailureHint(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	lines := magiskExecLines(text)

	at := -1
	next := func(desc string, match func(string) bool) int {
		i := magiskIndexOfLine(lines, at+1, match)
		if i < 0 {
			t.Fatalf("装依赖失败提示的结构不对：在第 %d 条可执行行之后按顺序找不到 %s", at+1, desc)
		}
		at = i
		return i
	}
	next("运行时验证未通过的判断", magiskLineEquals(`if [ -n "$missing_runtimes" ]; then`))
	next("Debian 分流的 flavor 判断", magiskLineEquals(`if [ "$FLAVOR" = "debian" ]; then`))
	next("NODE= 状态的读取", magiskLineContains(`node_status=$(grep '^NODE=' "$rootfs/tmp/daidai-deps-status"`))
	next("MIRROR=FAIL 分支（最先判断）", magiskLineEquals(`if [ "$deps_status" = "FAIL" ]; then`))
	nodeFailIdx := next("NODE=FAIL 分支（其次）", magiskLineEquals(`elif [ "$node_status" = "FAIL" ]; then`))
	downloadIdx := next("镜像源通但下载 / 解包失败的分支（再其次）", magiskLineEquals(`elif [ -n "$deps_status" ]; then`))

	nodeHint := strings.Join(lines[nodeFailIdx+1:downloadIdx], "\n")
	for _, want := range []string{"Node.js 官方二进制没装上", "npmmirror", "nodejs.org"} {
		if !strings.Contains(nodeHint, want) {
			t.Fatalf("NODE=FAIL 分支的提示缺少 %q，实际=%q", want, nodeHint)
		}
	}
	if strings.Contains(nodeHint, "apt-get update 全部失败") {
		t.Fatal("NODE=FAIL 分支不得复用「镜像源全挂」的文案")
	}
}

// magiskShellBlockEnd 返回从 lines[ifIdx]（一条以 then 结尾的 if 行）开始、与之配对的 fi 的下标，
// 找不到返回 -1。lines 须是 magiskExecLines 的结果（已去掉首尾空白与注释行）。
// 只数「if ... then」与「fi」两种整行：单行写完的 `if ...; then ...; fi` 自成一对，不影响嵌套深度。
func magiskShellBlockEnd(lines []string, ifIdx int) int {
	depth := 0
	for i := ifIdx; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "if ") && strings.HasSuffix(line, "then"):
			depth++
		case line == "fi":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// R5 的 Magisk 侧配套：service.sh 开机把宿主 deps-snapshot 回填进容器之后，
// 快照里没有 Node ABI 标记，就必须删掉容器里的标记。
//
// 快照每 10 分钟才刷新一次，回填用的 cp -rf 只覆盖、不删除多余文件：升级 Node 后首次开机，
// 面板已经 npm rebuild 并写下新标记；10 分钟内重启，快照里旧 ABI 编出来的 .node 把重建结果覆盖回去，
// 容器里的新标记却留下来，面板读到「ABI 未变」就再也不会重建。标记必须永远描述与之同源的二进制。
//
// 按可执行行断言（注释里解释原因时同样会提到文件名），并锁住位置：
// 在快照回填的 if 块里、cp 之后；在拉起容器之前。挪到 cp 之前等于没删（cp 不会删它），
// 挪出 if 块会让从没有快照的用户每次开机都丢一次标记、白白重跑 rebuild。
func TestMagiskServiceScriptDropsNodeABIMarkerMissingFromSnapshot(t *testing.T) {
	// 标记文件名以面板侧常量为准。那是 service 包的未导出常量，跨包只能读源码取值；
	// 一边改名一边没跟，service.sh 删的就是一个永远不存在的文件，本测试必须变红。
	src, err := os.ReadFile(filepath.Join("..", "service", "node_abi_rebuild.go"))
	if err != nil {
		t.Fatalf("read service/node_abi_rebuild.go: %v", err)
	}
	m := regexp.MustCompile(`(?m)^const nodeABIMarkerFileName = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal("service/node_abi_rebuild.go 找不到 const nodeABIMarkerFileName = \"...\"，标记文件名的定义被改写过，请同步本测试")
	}
	marker := "nodejs/" + string(m[1])

	lines := magiskExecLines(readMagiskScript(t, "service.sh"))

	blockIdx := magiskIndexOfLine(lines, 0, magiskLineEquals(`if [ -d "$DEPS_PERSIST" ]; then`))
	if blockIdx < 0 {
		t.Fatal(`service.sh 找不到开机回填 deps 快照的 if [ -d "$DEPS_PERSIST" ]; then`)
	}
	if again := magiskIndexOfLine(lines, blockIdx+1, magiskLineEquals(`if [ -d "$DEPS_PERSIST" ]; then`)); again >= 0 {
		t.Fatalf("service.sh 出现了多处 if [ -d \"$DEPS_PERSIST\" ]（第 %d、%d 条可执行行），本测试无法确定哪一处是开机回填", blockIdx+1, again+1)
	}
	blockEnd := magiskShellBlockEnd(lines, blockIdx)
	if blockEnd < 0 {
		t.Fatal("service.sh 开机回填 deps 快照的 if 块找不到配对的 fi")
	}

	cpIdx := magiskIndexOfLine(lines, blockIdx+1, func(line string) bool {
		return strings.HasPrefix(line, `cp -rf "$DEPS_PERSIST/." `) && strings.Contains(line, "/app/Dumb-Panel/deps/")
	})
	if cpIdx < 0 || cpIdx > blockEnd {
		t.Fatalf("service.sh 开机回填 deps 快照的 if 块里找不到 cp -rf \"$DEPS_PERSIST/.\" (block=%d..%d cp=%d)", blockIdx, blockEnd, cpIdx)
	}

	checkLine := `if [ ! -f "$DEPS_PERSIST/` + marker + `" ]; then`
	rmLine := `rm -f "$rootfs/app/Dumb-Panel/deps/` + marker + `" 2>/dev/null`
	checkIdx := magiskIndexOfLine(lines, 0, magiskLineEquals(checkLine))
	if checkIdx < 0 {
		t.Fatalf("service.sh 缺少可执行行 %q：快照里没有 Node ABI 标记时必须删掉容器里的标记", checkLine)
	}
	if !(cpIdx < checkIdx && checkIdx < blockEnd) {
		t.Fatalf("Node ABI 标记的判断必须在快照回填 if 块里、cp 之后 (block=%d..%d cp=%d check=%d)", blockIdx, blockEnd, cpIdx, checkIdx)
	}
	if checkIdx+2 >= len(lines) || lines[checkIdx+1] != rmLine || lines[checkIdx+2] != "fi" {
		t.Fatalf("快照里没有 Node ABI 标记时，分支体必须恰好是 %q 再接 fi，实际=%q", rmLine, lines[checkIdx+1:min(checkIdx+3, len(lines))])
	}

	launchIdx := magiskIndexOfLine(lines, 0, func(line string) bool {
		return strings.HasPrefix(line, `"$RURIMA" ruri -p -N -S -A $rootfs "$CTR_SHELL" /tmp/daidai-startup.sh`)
	})
	if launchIdx < 0 || checkIdx > launchIdx {
		t.Fatalf("Node ABI 标记的清理必须在拉起容器（面板开机检查 ABI）之前 (check=%d launch=%d)", checkIdx, launchIdx)
	}
}
