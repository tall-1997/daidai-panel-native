package service

// Android 内置 Linux 容器执行层。
//
// 当 Go Core 以 gomobile 形式嵌入 Android 进程时，宿主侧不存在可用的 python/node/bash
// 解释器（也没有 Termux）。本文件在检测到 DAIDAI_PROOT_PATH + DAIDAI_LINUX_ROOTFS_DIR
// 等环境变量后，把所有托管运行时的进程创建（venv、pip、npm、node、python、bash、
// 依赖安装）统一包装为 proot 进入内置 Alpine/Ubuntu rootfs 的 guest 命令，与
// Kotlin fallback 走同一条统一 Linux 环境。
//
// 路径映射约定（与 Kotlin AndroidLinuxRuntime 保持一致）：
//   - filesDir            -> /host-files
//   - cacheDir            -> /tmp/host-cache
//   - 任务工作目录 workDir -> /workspace（proot -w，同时 bind）
//
// 桌面 / 服务器进程未设置这些变量时，本层完全不生效，行为与改造前一致。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	androidGuestPATH      = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	androidGuestWorkDir   = "/workspace"
	androidGuestFilesRoot = "/host-files"
	androidGuestCacheRoot = "/tmp/host-cache"
)

// androidContainerRuntime 描述 Android 内置 Linux 容器（proot + rootfs）的运行时信息。
type androidContainerRuntime struct {
	active       bool
	proot        string
	prootLoader  string
	rootfs       string
	filesDir     string
	cacheDir     string
	nativeLibDir string
	guestPath    string
}

var (
	androidContainerMu sync.RWMutex
	cachedAndroid      *androidContainerRuntime
)

// androidContainer 返回当前进程的 Android 容器运行时配置（首次访问时按环境变量加载）。
// 未激活时返回 inactive 实例，所有调用退化为直执行。
func androidContainer() *androidContainerRuntime {
	androidContainerMu.RLock()
	cached := cachedAndroid
	androidContainerMu.RUnlock()
	if cached != nil {
		return cached
	}
	androidContainerMu.Lock()
	defer androidContainerMu.Unlock()
	if cachedAndroid != nil {
		return cachedAndroid
	}

	proot := strings.TrimSpace(os.Getenv("DAIDAI_PROOT_PATH"))
	prootLoader := strings.TrimSpace(os.Getenv("DAIDAI_PROOT_LOADER_PATH"))
	rootfs := strings.TrimSpace(os.Getenv("DAIDAI_LINUX_ROOTFS_DIR"))
	filesDir := strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_FILES_DIR"))
	cacheDir := strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_CACHE_DIR"))
	nativeLibDir := strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_NATIVE_LIB_DIR"))

	rt := &androidContainerRuntime{
		proot: proot, prootLoader: prootLoader, rootfs: rootfs,
		filesDir: filesDir, cacheDir: cacheDir, nativeLibDir: nativeLibDir,
		guestPath: androidGuestPATH,
	}
	rt.active = rt.proot != "" && isExecutableFile(rt.proot) &&
		rt.rootfs != "" && directoryExists(rt.rootfs) &&
		rt.filesDir != "" && directoryExists(rt.filesDir)
	if !rt.active {
		rt.active = rt.proot != "" && isExecutableFile(rt.proot) && rt.rootfs != ""
	}
	cachedAndroid = rt
	return rt
}

// rootfsBinary 在 rootfs 内查找 guest 二进制，返回 guest 侧绝对路径；找不到返回空串。
func (rt *androidContainerRuntime) rootfsBinary(name string) string {
	for _, rel := range []string{
		"/usr/local/bin/" + name,
		"/usr/local/sbin/" + name,
		"/usr/bin/" + name,
		"/usr/sbin/" + name,
		"/bin/" + name,
		"/sbin/" + name,
	} {
		if isExecutableFile(filepath.Join(rt.rootfs, strings.TrimPrefix(rel, "/"))) {
			return rel
		}
	}
	return ""
}

// hostToGuestPath 把 filesDir/cacheDir 下的宿主绝对路径映射为 guest 路径；映射不到返回空串。
func (rt *androidContainerRuntime) hostToGuestPath(hostPath string) string {
	hostPath = filepath.Clean(hostPath)
	if rel, err := filepath.Rel(rt.filesDir, hostPath); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Join(androidGuestFilesRoot, rel))
	}
	if rel, err := filepath.Rel(rt.cacheDir, hostPath); err == nil &&
		rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(filepath.Join(androidGuestCacheRoot, rel))
	}
	return ""
}

// mapArg 把命令参数里的宿主绝对路径映射为 guest 路径；非映射范围或相对路径原样保留。
func (rt *androidContainerRuntime) mapArg(arg string) string {
	if arg == "" || !filepath.IsAbs(arg) {
		return arg
	}
	if guest := rt.hostToGuestPath(arg); guest != "" {
		return guest
	}
	return arg
}

// resolveGuestBinary 解析最终要交给 proot 执行的 guest 二进制：
//   - 宿主 venv/node_modules bin 下的绝对路径 -> 映射为 guest 路径
//   - guest 绝对路径（如 /usr/bin/python3）-> 原样
//   - 裸命令名 -> 优先 rootfs 内查找，找不到保留原样交由 guest PATH 解析
func (rt *androidContainerRuntime) resolveGuestBinary(binary string) string {
	if binary == "" {
		return binary
	}
	if strings.Contains(binary, "/") {
		if guest := rt.mapArg(binary); guest != "" {
			return guest
		}
		return binary
	}
	if guest := rt.rootfsBinary(binary); guest != "" {
		return guest
	}
	return binary
}

// prootArgs 组装 proot 基础参数：link2symlink + 伪内核版本 + rootfs + 工作目录 + bind 映射。
func (rt *androidContainerRuntime) prootArgs(workDir string) []string {
	args := []string{
		"--link2symlink",
		"--kill-on-exit",
		"-k", "4.14.0",
		"-r", rt.rootfs,
		"-w", androidGuestWorkDir,
	}
	if workDir != "" && directoryExists(workDir) {
		args = append(args, "-b", workDir+":"+androidGuestWorkDir)
	}
	args = append(args,
		"-b", rt.filesDir+":"+androidGuestFilesRoot,
		"-b", rt.cacheDir+":"+androidGuestCacheRoot,
		"-0",
	)
	for _, path := range []string{"/proc", "/dev", "/sys", "/sdcard", "/storage"} {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "-b", path+":"+path)
		}
	}
	return args
}

// buildAndroidCmd 把「binary + args + workDir」构造为一条待执行命令。
// 容器激活时返回 proot 包装的 guest 命令（binary 与路径参数都会被映射）；
// 未激活时返回直执行命令，与改造前行为一致。
func (rt *androidContainerRuntime) buildAndroidCmd(binary string, args []string, workDir string) *exec.Cmd {
	guestArgs := make([]string, 0, len(args))
	for _, arg := range args {
		guestArgs = append(guestArgs, rt.mapArg(arg))
	}
	fullArgs := append(rt.prootArgs(workDir), rt.resolveGuestBinary(binary))
	fullArgs = append(fullArgs, guestArgs...)
	cmd := exec.Command(rt.proot, fullArgs...)
	cmd.Dir = workDir
	return cmd
}

// applyAndroidEnv 对已设置好 Env 的命令做 guest 侧环境收敛：
//   - PATH 替换为 guest PATH（proot 内查找 rootfs 工具）
//   - 注入 PROOT_LOADER / PROOT_NO_SECCOMP / PROOT_TMP_DIR（Android 上 proot 正常运行所需）
//   - 去掉宿主 LD_LIBRARY_PATH，避免污染 guest 动态链接
//
// 桌面/服务器路径不做任何修改。
func (rt *androidContainerRuntime) applyAndroidEnv(cmd *exec.Cmd) {
	if !rt.active || cmd == nil {
		return
	}
	env := make([]string, 0, len(cmd.Env)+4)
	for _, entry := range cmd.Env {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" {
			env = append(env, "PATH="+rt.guestPath)
			continue
		}
		if key == "LD_LIBRARY_PATH" {
			continue
		}
		if key == "PROOT_LOADER" || key == "PROOT_NO_SECCOMP" || key == "PROOT_TMP_DIR" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"PATH="+rt.guestPath,
		"PROOT_NO_SECCOMP=1",
		"PROOT_TMP_DIR="+rt.cacheDir,
		"PROOT_VERBOSE=0",
	)
	if rt.prootLoader != "" && isExecutableFile(rt.prootLoader) {
		env = append(env, "PROOT_LOADER="+rt.prootLoader)
	}
	cmd.Env = env
}

// androidManagedCommand 是容器模式下的统一命令构造入口（非容器模式等价于 exec.Command）。
// 调用方在返回值上设置 cmd.Env 后，应调用 androidFinalizeCommand(cmd) 完成环境收敛。
func androidManagedCommand(binary string, args []string, workDir string) *exec.Cmd {
	if rt := androidContainer(); rt.active {
		return rt.buildAndroidCmd(binary, args, workDir)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = workDir
	return cmd
}

// androidManagedCommandContext 是带 context 的容器感知命令构造入口，
// 语义与 androidManagedCommand 一致，适用于需要超时/取消的场景。
func androidManagedCommandContext(ctx context.Context, binary string, args []string, workDir string) *exec.Cmd {
	if rt := androidContainer(); rt.active {
		guestArgs := make([]string, 0, len(args))
		for _, arg := range args {
			guestArgs = append(guestArgs, rt.mapArg(arg))
		}
		fullArgs := append(rt.prootArgs(workDir), rt.resolveGuestBinary(binary))
		fullArgs = append(fullArgs, guestArgs...)
		cmd := exec.CommandContext(ctx, rt.proot, fullArgs...)
		cmd.Dir = workDir
		return cmd
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = workDir
	return cmd
}

// androidFinalizeCommand 完成命令的容器环境收敛（仅容器激活时生效）。
// 所有构造完 cmd.Env 的托管运行时调用点都应收尾调用一次。
func androidFinalizeCommand(cmd *exec.Cmd) {
	if rt := androidContainer(); rt.active {
		rt.applyAndroidEnv(cmd)
	}
}

// androidExec 以容器感知方式执行 binary+args 并收集输出（非容器模式直执行）。
func androidExec(binary string, args []string, workDir string, env []string) ([]byte, error) {
	cmd := androidManagedCommand(binary, args, workDir)
	cmd.Env = env
	androidFinalizeCommand(cmd)
	return cmd.CombinedOutput()
}

// mapEnvPathList 把 PATH/PYTHONPATH/NODE_PATH 这类路径列表环境变量的 host 项映射为 guest 路径。
// 不在 filesDir/cacheDir 内的项原样保留。
func (rt *androidContainerRuntime) mapEnvPathList(value string) string {
	parts := strings.Split(value, string(os.PathListSeparator))
	for index := range parts {
		if guest := rt.mapArg(parts[index]); guest != "" {
			parts[index] = guest
		}
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// androidMapRuntimeEnvVars 在容器激活时，把托管运行时环境变量里的路径列表
// （PYTHONPATH / NODE_PATH / PATH）映射为 guest 路径，供 bootstrap 注入使用。
func androidMapRuntimeEnvVars(envVars map[string]string) {
	rt := androidContainer()
	if !rt.active || envVars == nil {
		return
	}
	for _, key := range []string{"PYTHONPATH", "NODE_PATH", "PATH"} {
		if value, ok := envVars[key]; ok {
			envVars[key] = rt.mapEnvPathList(value)
		}
	}
}

// androidContainerRuntimeTempDir 容器激活时把运行时临时文件落到 cacheDir 下
// （bind 到 /tmp/host-cache），保证 guest 进程能读到 env.json 等文件。
func androidContainerRuntimeTempDir() string {
	if rt := androidContainer(); rt.active {
		return rt.cacheDir
	}
	return ""
}

// managedRuntimeBinary 在宿主 PATH 或 Android 容器 rootfs 中解析一个裸命令名，
// 返回最终可执行的路径与是否命中。用于 Linux 依赖存在性探测等场景。
func managedRuntimeBinary(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	if _, err := exec.LookPath(name); err == nil {
		return name, true
	}
	if rt := androidContainer(); rt.active {
		if guest := rt.rootfsBinary(name); guest != "" {
			return guest, true
		}
	}
	return "", false
}
