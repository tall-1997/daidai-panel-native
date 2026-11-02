package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"daidai-panel/config"
)

// nodeABIMarkerFileName 记录上一次适配 deps/nodejs 时 node 的 process.versions.modules（原生扩展 ABI 号）。
// 放在 deps/nodejs 根目录、不进 node_modules：npm 与依赖清单修复只看 node_modules，不会把这个点文件当成包。
const nodeABIMarkerFileName = ".daidai-node-abi"

const (
	// nodeABIProbeTimeout：同步探测跑在启动主流程里，node 卡住也不能把面板拖着起不来。
	nodeABIProbeTimeout = 15 * time.Second
	// nodeAddonProbeTimeout：require 单个原生扩展包的上限，超时按「不需要重建」处理。
	nodeAddonProbeTimeout = 30 * time.Second
	// nodeABICommandWaitDelay：杀掉进程后孙进程若还攥着输出管道，最多再等这么久，免得包操作锁一直不释放。
	nodeABICommandWaitDelay = 10 * time.Second
	// nodeAddonBrokenMarker 是探测脚本判定「需要重建」时写到 stdout 的固定标记。
	nodeAddonBrokenMarker = "DAIDAI_ABI_BROKEN"
)

// nodeAddonProbeScript 在独立 node 进程里 require 包目录（process.argv[1]）。只有 ABI 不匹配（ERR_DLOPEN_FAILED
// 的消息里带 NODE_MODULE_VERSION）或找不到与当前 Node 对应的预编译产物时才向 stdout 输出标记，其它异常一律不算。
// 加载完主动 exit，免得包里开着的句柄把进程拖到超时。
const nodeAddonProbeScript = `try { require(process.argv[1]); } catch (e) {
  if (/NODE_MODULE_VERSION|No native build was found|Could not locate the bindings file/.test(String(e && e.message ? e.message : e))) {
    process.stdout.write("` + nodeAddonBrokenMarker + `\n");
  }
}
process.exit(0);
`

// 探测与命令构造抽成包级变量只为单测注入，测试里不真跑 node / npm。
var (
	nodeABIProbeFunc         = probeCurrentNodeABI
	nodeAddonLoadBrokenFunc  = probeNodeAddonLoadBroken
	newNpmRebuildCommandFunc = newNpmRebuildCommand
)

// RebuildNodeDependenciesIfABIChanged 在 node 的原生扩展 ABI 变化后，重建 deps/nodejs 里加载失败的原生扩展包。
//
// Magisk 模块刷新版、Docker 换镜像时 node_modules 原样保留，按旧 ABI 编译的 .node 在新 Node 上会报
// NODE_MODULE_VERSION 不匹配。只在 Linux 上执行：换 Node 大版本只发生在 Magisk 模块与 Docker 镜像，
// Windows / 二进制版用户自己管理 Node。
func RebuildNodeDependenciesIfABIChanged() {
	if !nodeABIRebuildSupported(runtime.GOOS) {
		return
	}
	rebuildNodeDependenciesIfABIChanged()
}

func nodeABIRebuildSupported(goos string) bool {
	return goos == "linux"
}

// rebuildNodeDependenciesIfABIChanged 同步判定（只起一次 node 进程），逐包探测与重建放到后台 goroutine、持 Node 包操作锁执行。
// node 不可用或输出异常 → 什么都不做、不写标记；标记与当前 ABI 一致 → 什么都不做；node_modules 里没有包 → 直接写标记。
// 返回的 channel 在本次检查彻底结束时关闭，供单测等待；不需要后台处理时直接返回已关闭的 channel。
func rebuildNodeDependenciesIfABIChanged() <-chan struct{} {
	done := make(chan struct{})
	if config.C == nil || strings.TrimSpace(config.C.Data.Dir) == "" {
		close(done)
		return done
	}
	nodeDir := filepath.Join(config.C.Data.Dir, "deps", "nodejs")
	markerPath := filepath.Join(nodeDir, nodeABIMarkerFileName)

	abi, err := nodeABIProbeFunc()
	if err != nil {
		// 没装 node 是正常形态，不值得每次开机刷一行日志。
		if !errors.Is(err, exec.ErrNotFound) {
			log.Printf("warn: 探测 node 原生扩展 ABI 失败，本次跳过 Node 依赖重建检查: %v", err)
		}
		close(done)
		return done
	}
	abi = strings.TrimSpace(abi)
	if abi == "" || strings.Trim(abi, "0123456789") != "" {
		// 输出混进了别的东西（例如 NODE_OPTIONS 预加载脚本往 stdout 打了字），宁可不判也不写一个脏标记。
		log.Printf("warn: node 输出的 process.versions.modules 不是纯数字（%q），本次跳过 Node 依赖重建检查", abi)
		close(done)
		return done
	}

	data, _ := os.ReadFile(markerPath)
	previous := strings.TrimSpace(string(data))
	if previous == abi {
		close(done)
		return done
	}
	// 只把非点开头的目录算作包：依赖全卸光后 node_modules 里往往只剩 .bin、.package-lock.json 这类 npm 自己的条目。
	if len(realSubdirNames(filepath.Join(nodeDir, "node_modules"))) == 0 {
		writeNodeABIMarker(nodeDir, markerPath, abi)
		close(done)
		return done
	}

	go func() {
		defer close(done)
		unlock := LockNodePackageOperation()
		defer unlock()
		if rebuildBrokenNodeAddons(nodeDir, previous, abi) {
			writeNodeABIMarker(nodeDir, markerPath, abi)
		}
	}()
	return done
}

// rebuildBrokenNodeAddons 找出加载失败的原生扩展包并 npm rebuild 它们，返回是否写入标记。调用方持有 Node 包操作锁。
//
// 只重建「现在确实加载失败」的包：npm rebuild 走 node-gyp 时第一步就删 build 目录，对本来能用的包
// （N-API 模块、自带新 ABI 预编译产物的包）重建失败会把它弄坏；只碰已经坏掉的包，失败也不会更糟。
//
// 重建失败也写标记是刻意的取舍：Docker 精简镜像没有编译链，重建必然失败，不写标记就每次开机重跑一遍。
// 代价是同一个 ABI 不再自动重试，仍然加载失败的包要用户在依赖管理里重装。
// 只有找不到 npm（根本没尝试重建）时不写标记，npm 装回来之后下次启动再判。依赖记录的状态一律不改。
func rebuildBrokenNodeAddons(nodeDir, previous, abi string) bool {
	if previous == "" {
		previous = "无记录"
	}
	packages := collectNodeNativeAddonPackages(filepath.Join(nodeDir, "node_modules"))
	broken := filterBrokenNodeAddons(nodeDir, packages)
	if len(broken) == 0 {
		log.Printf("[Node.js 依赖] Node 原生扩展 ABI 变化（%s -> %s），检查了 %d 个含原生扩展的包，没有加载失败的，无需重建", previous, abi, len(packages))
		return true
	}

	names := nodePackageNames(broken)
	joined := strings.Join(names, "、")
	cmd, err := newNpmRebuildCommandFunc(nodeDir, names)
	if errors.Is(err, exec.ErrNotFound) {
		log.Printf("warn: [Node.js 依赖] %s 在 Node ABI %s 下加载失败，但找不到 npm，本次无法重建，下次启动再检查: %v", joined, abi, err)
		return false
	}
	log.Printf("[Node.js 依赖] Node 原生扩展 ABI 变化（%s -> %s），%s 加载失败，后台执行 npm rebuild", previous, abi, joined)

	var output bytes.Buffer
	if err == nil {
		cmd.Stdout = &output
		cmd.Stderr = &output
		err = runNodeABICommand(cmd, DependencyOperationTimeout())
	}
	if err != nil {
		log.Printf("[Node.js 依赖] npm rebuild %s 失败（ABI %s）: %v；输出末尾：\n%s", joined, abi, err, tailNodeABIRebuildOutput(output.Bytes()))
	} else {
		log.Printf("[Node.js 依赖] npm rebuild %s 完成（ABI %s）", joined, abi)
	}

	// 无论 npm 退出码如何都按包再探测一次：非 0 不代表每个包都失败，0 也不保证都能加载。
	if stillBroken := nodePackageNames(filterBrokenNodeAddons(nodeDir, broken)); len(stillBroken) > 0 {
		log.Printf("warn: [Node.js 依赖] %s 重建后仍然加载失败，请到依赖管理里重装这些包", strings.Join(stillBroken, "、"))
	} else {
		log.Printf("[Node.js 依赖] 重建后 %s 均已能正常加载", joined)
	}
	return true
}

// collectNodeNativeAddonPackages 列出 node_modules 里含 .node 原生扩展的包目录（<name> 或 @scope/<name>，
// 递归包内嵌套的 node_modules）。不跟随软链接：file: 本地目录包、workspace 指向的真实目录不归面板管。
func collectNodeNativeAddonPackages(nodeModulesDir string) []string {
	var packages []string
	visit := func(pkgDir string) {
		if nodePackageHasNativeAddon(pkgDir) {
			packages = append(packages, pkgDir)
		}
		nested := filepath.Join(pkgDir, "node_modules")
		if info, err := os.Lstat(nested); err == nil && info.IsDir() {
			packages = append(packages, collectNodeNativeAddonPackages(nested)...)
		}
	}
	for _, name := range realSubdirNames(nodeModulesDir) {
		dir := filepath.Join(nodeModulesDir, name)
		if !strings.HasPrefix(name, "@") {
			visit(dir)
			continue
		}
		for _, scoped := range realSubdirNames(dir) {
			visit(filepath.Join(dir, scoped))
		}
	}
	return packages
}

// realSubdirNames 列出 dir 下非点开头的子目录。os.ReadDir 的条目类型不跟随软链接，指向目录的软链接不算目录。
func realSubdirNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			names = append(names, entry.Name())
		}
	}
	return names
}

// nodePackageHasNativeAddon 判断包目录内（不进入嵌套 node_modules、不跟随软链接）有没有 .node 普通文件。
func nodePackageHasNativeAddon(pkgDir string) bool {
	found := false
	_ = filepath.WalkDir(pkgDir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case d.IsDir():
			if path != pkgDir && d.Name() == "node_modules" {
				return filepath.SkipDir
			}
		case d.Type().IsRegular() && filepath.Ext(d.Name()) == ".node":
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func filterBrokenNodeAddons(nodeDir string, pkgDirs []string) []string {
	var broken []string
	for _, pkgDir := range pkgDirs {
		if nodeAddonLoadBrokenFunc(nodeDir, pkgDir) {
			broken = append(broken, pkgDir)
		}
	}
	return broken
}

// nodePackageNames 取各包 package.json 里的 name（读不到时按目录推断），去重并保持顺序。
func nodePackageNames(pkgDirs []string) []string {
	seen := map[string]bool{}
	var names []string
	for _, pkgDir := range pkgDirs {
		var pkg struct {
			Name string `json:"name"`
		}
		if data, err := os.ReadFile(filepath.Join(pkgDir, "package.json")); err == nil {
			_ = json.Unmarshal(data, &pkg)
		}
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			name = filepath.Base(pkgDir)
			if scope := filepath.Base(filepath.Dir(pkgDir)); strings.HasPrefix(scope, "@") {
				name = scope + "/" + name
			}
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// probeNodeAddonLoadBroken 起独立 node 进程 require 一次包目录，输出了 nodeAddonBrokenMarker 才算需要重建；
// 加载成功、其它异常、超时、崩溃一律按不需要重建处理。只看 stdout：脚本出错时 node 会把源码行（含标记字样）打到 stderr。
func probeNodeAddonLoadBroken(nodeDir, pkgDir string) bool {
	absDir, err := filepath.Abs(pkgDir)
	if err != nil {
		return false
	}
	var stdout bytes.Buffer
	cmd := exec.Command("node", "-e", nodeAddonProbeScript, absDir)
	cmd.Dir = nodeDir
	cmd.Stdout = &stdout
	_ = runNodeABICommand(cmd, nodeAddonProbeTimeout)
	return strings.Contains(stdout.String(), nodeAddonBrokenMarker)
}

func probeCurrentNodeABI() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nodeABIProbeTimeout)
	defer cancel()
	// 与 NewNpmInstallCommand 一样直接用 PATH 上的 node，只取 stdout，stderr 里的告警不影响判定。
	out, err := exec.CommandContext(ctx, "node", "-p", "process.versions.modules").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func newNpmRebuildCommand(nodeDir string, names []string) (*exec.Cmd, error) {
	// 先确认 npm 在 PATH 上：找不到时调用方不写标记，也免得在 npm 都没有时去改写 package.json。
	if _, err := exec.LookPath("npm"); err != nil {
		return nil, fmt.Errorf("未找到 npm: %w", err)
	}
	// 与 NewNpmInstallCommand 同一套前置：package.json 坏了 npm 会先 EJSONPARSE 退出。
	if err := ensureNodePackageManifest(nodeDir); err != nil {
		return nil, err
	}
	cmd := exec.Command("npm", append([]string{"rebuild", "--prefix", nodeDir}, names...)...)
	// 部分原生扩展重建时会去下载预编译产物，代理与 npm 镜像要和安装时一致。
	cmd.Env = NpmInstallEnv(AppendProxyEnv(os.Environ()), CurrentNpmMirror())
	return cmd, nil
}

// runNodeABICommand 带超时执行 cmd（调用方先接好 Stdout / Stderr）。启动期这条路径没有界面入口可以取消，
// 而它全程攥着 Node 包操作锁；超时按进程组杀，node-gyp / make 这类孙进程一并结束。
func runNodeABICommand(cmd *exec.Cmd, timeout time.Duration) error {
	cmd.WaitDelay = nodeABICommandWaitDelay
	SetPgid(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-waitCh:
		return err
	case <-timer.C:
		KillProcessGroup(cmd.Process)
		<-waitCh
		return fmt.Errorf("执行超过 %s 仍未结束，已终止", timeout)
	}
}

func writeNodeABIMarker(nodeDir, markerPath, abi string) {
	err := os.MkdirAll(nodeDir, 0o755)
	if err == nil {
		err = os.WriteFile(markerPath, []byte(abi+"\n"), 0o644)
	}
	if err != nil {
		log.Printf("warn: 写入 Node ABI 标记 %s 失败: %v", markerPath, err)
	}
}

// tailNodeABIRebuildOutput 只保留输出末尾：node-gyp 失败时动辄几百行，真正的报错在最后。
func tailNodeABIRebuildOutput(output []byte) string {
	const maxBytes = 4000
	text := strings.TrimSpace(string(output))
	if len(text) <= maxBytes {
		return text
	}
	// 按字节截断可能切在多字节字符中间，往后挪到下一个字符起点，日志里不留半个汉字。
	start := len(text) - maxBytes
	for start < len(text) && !utf8.RuneStart(text[start]) {
		start++
	}
	return "..." + text[start:]
}
