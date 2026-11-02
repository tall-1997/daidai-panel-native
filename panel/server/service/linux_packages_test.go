package service

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/testutil"
)

// 容器配了 PUID/PGID 之后面板以普通用户跑，apt-get / apk 必须 root。
// 原来这条路会一路跑到包管理器自己报英文的 Permission denied / dpkg 锁失败，
// 用户很容易当成面板的 bug。这里锁住「提前拦下并说清楚」这个行为。
func TestEnsureLinuxPackageManagerPrivilege(t *testing.T) {
	err := EnsureLinuxPackageManagerPrivilege()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("root / Windows 下不应拦截，err=%v", err)
		}
		return
	}

	if err == nil {
		t.Fatal("非 root 运行时必须拦下 Linux 系统依赖操作")
	}
	// 报错必须自带出路，否则用户只知道「不行」不知道「怎么办」。
	for _, want := range []string{
		"非 root",
		"可选做法：",
		"Node.js / Python 依赖不受此限制",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("提示里缺少 %q，实际=%q", want, err.Error())
		}
	}

	// 出路必须按部署形态给：Docker 里才提 docker exec，裸机部署提的是 systemd 的 User=。
	// 给二进制部署的用户一段 docker exec 指引等于没给（他机器上既没容器也没 compose）。
	inDocker := false
	if _, statErr := os.Stat("/.dockerenv"); statErr == nil {
		inDocker = true
	}
	if inDocker && !strings.Contains(err.Error(), "docker exec -u 0") {
		t.Fatalf("容器内应给出 docker exec 出路，实际=%q", err.Error())
	}
	if !inDocker && strings.Contains(err.Error(), "docker exec -u 0") {
		t.Fatalf("非容器部署不应给出 docker exec 出路，实际=%q", err.Error())
	}
}

func TestBuildLinuxPackageCommandChecksPrivilegeBeforeTouchingMirror(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("这条用例只在非 root 环境下有意义")
	}

	mirrorCalled := false
	// 签名跟着 v3.3.2 的 ensureMirror 一起多了「源是否真被改写」这个返回值（issue #146）。
	ensureMirror := func(LinuxPackageManager, string) (bool, error) {
		mirrorCalled = true
		return false, nil
	}

	for _, action := range []string{"install", "remove"} {
		cmd, err := BuildLinuxPackageCommand(
			LinuxPackageManager{Name: "apk", Binary: "apk"},
			action, "curl", false, "alpine", ensureMirror,
		)
		if err == nil {
			t.Fatalf("action=%s 非 root 时必须直接返回错误", action)
		}
		if cmd != nil {
			t.Fatalf("action=%s 被拦下时不应返回命令", action)
		}
	}

	// 权限闸必须排在换镜像源之前：注定要失败的操作没有理由先去改用户的 sources.list。
	if mirrorCalled {
		t.Fatal("权限检查必须排在 ensureMirror 之前")
	}
}

// 容器重建后启动校验的后台重装不持有网页端的包操作锁，两边的 apt-get 会抢 dpkg 锁；
// apt 默认不等锁直接报错，所以装、卸两条命令都要带上等锁超时（#142）。
func TestAptCommandsWaitForDpkgLock(t *testing.T) {
	apt := LinuxPackageManager{Name: "apt", Binary: "apt-get"}

	_, args, err := LinuxInstallCommandSpec(apt, "libnss3", true)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Join(args, " ")
	if !strings.Contains(script, "apt-get -o DPkg::Lock::Timeout=300 install -y --no-install-recommends 'libnss3'") {
		t.Fatalf("apt 安装命令应带等锁超时，实际 %q", script)
	}
	if !strings.Contains(script, "apt-get update") {
		t.Fatalf("索引过期时仍要先 update，实际 %q", script)
	}

	for _, force := range []bool{false, true} {
		bin, args, err := LinuxRemoveCommandSpec(apt, "libnss3", force)
		if err != nil {
			t.Fatal(err)
		}
		// -o 是 apt-get 的全局选项，必须写在子命令之前。
		if bin != "apt-get" || len(args) < 3 || args[0] != "-o" || args[1] != aptLockTimeoutOption || args[2] != "remove" {
			t.Fatalf("apt 卸载命令应以 -o %s remove 开头，实际 %s %v", aptLockTimeoutOption, bin, args)
		}
		if args[len(args)-1] != "libnss3" {
			t.Fatalf("包名应在最后，实际 %v", args)
		}
	}

	// 其它包管理器不受影响。
	if _, args, _ := LinuxInstallCommandSpec(LinuxPackageManager{Name: "apk", Binary: "apk"}, "curl", false); strings.Join(args, " ") != "add --no-cache curl" {
		t.Fatalf("apk 安装命令不应变化，实际 %v", args)
	}
}

// 容器重建后的自动重装以前传的是 nil ensureMirror，一律走 deb.debian.org；现在必须与网页安装一样先换源（#142）。
func TestRestartLinuxReinstallEnsuresMirror(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("非 root 时权限闸会先拦下，走不到换源这一步")
	}
	// 构造命令时会经 AppendProxyEnv 读代理配置，需要数据库。
	testutil.SetupTestEnv(t)

	oldLookPath, oldEnsure := DetectLinuxPackageManagerLookPathFunc, linuxDependencyEnsureMirrorFunc
	if oldEnsure == nil {
		t.Fatal("重启重装的 ensureMirror 不能是 nil")
	}
	t.Cleanup(func() {
		DetectLinuxPackageManagerLookPathFunc, linuxDependencyEnsureMirrorFunc = oldLookPath, oldEnsure
	})
	DetectLinuxPackageManagerLookPathFunc = func(file string) (string, error) {
		if file == "apt-get" {
			return "/usr/bin/apt-get", nil
		}
		return "", os.ErrNotExist
	}
	var ensuredManager string
	linuxDependencyEnsureMirrorFunc = func(manager LinuxPackageManager, distribution string) (bool, error) {
		ensuredManager = manager.Name
		return false, nil
	}

	cmd, err := buildLinuxDependencyInstallCommand("libnss3")
	if err != nil {
		t.Fatalf("构造重装命令失败：%v", err)
	}
	if ensuredManager != "apt" {
		t.Fatalf("重启重装应先调用 ensureMirror，实际调用到的包管理器 %q", ensuredManager)
	}
	if !strings.Contains(strings.Join(cmd.Args, " "), "DPkg::Lock::Timeout=300") {
		t.Fatalf("重启重装的 apt 命令同样要带等锁超时，实际 %v", cmd.Args)
	}
}
