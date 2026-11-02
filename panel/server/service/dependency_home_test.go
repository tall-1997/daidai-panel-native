package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/config"
)

// 这一组用例锁的是 v3.0.7 修的那个 bug：容器配了 PUID/PGID 降权之后，
// 面板进程拿到的 HOME 要么不存在（su-exec 按 passwd 覆写成从没被创建过的 /home/daidai），
// 要么属于 root（gosu 保持 Docker 注入的 /root）。两种情况下 npm 的 cache、.npmrc
// 与 pip 的 pip.conf 都写不进去，装依赖必报 EACCES。
//
// 纯逻辑部分（resolveWritableHome / redirectHomeEnv）刻意不读环境变量、不看操作系统，
// 所以在 Windows 开发机上也跑得到；只有走 os.Getenv + runtime.GOOS 的外层函数才需要跳过。

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 npm / pip 认的是 USERPROFILE / APPDATA，HOME 重定向刻意不生效")
	}
}

func useTempDataDir(t *testing.T) string {
	t.Helper()
	oldConfig := config.C
	t.Cleanup(func() {
		config.C = oldConfig
	})

	dataDir := t.TempDir()
	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	return dataDir
}

func TestResolveWritableHomeKeepsWritableHome(t *testing.T) {
	home := t.TempDir()
	if got := resolveWritableHome(home, t.TempDir()); got != home {
		t.Fatalf("HOME 可写时必须原样返回（历史部署零影响），want=%q got=%q", home, got)
	}
}

func TestResolveWritableHomeFallsBackWhenHomeMissing(t *testing.T) {
	dataDir := t.TempDir()
	// 这个目录刻意不创建：容器里 adduser -D -H / useradd -M 建出来的用户
	// 就是「passwd 里写着家目录、目录却从来不存在」这个形态。
	missing := filepath.Join(dataDir, "never-created-home")

	want := filepath.Join(dataDir, ".home")
	got := resolveWritableHome(missing, dataDir)
	if got != want {
		t.Fatalf("HOME 不存在时应回落到数据目录下的 .home，want=%q got=%q", want, got)
	}
	info, err := os.Stat(want)
	if err != nil || !info.IsDir() {
		t.Fatalf("回落目录必须被真正创建出来，stat err=%v info=%v", err, info)
	}
}

// HOME 为空时【不能】重定向：那时 npm / pip 会退回 getpwuid 解析家目录，
// 而那个结果通常是对的（裸机 systemd 以 root 跑、没写 Environment=HOME 就是这种）。
// 一旦在这里也回落到数据目录，用户手写在 /root/.npmrc 里的私有源与 token
// 会静默失效，表现成「升级之后私有包突然 401/404」。
func TestResolveWritableHomeKeepsEmptyHomeUntouched(t *testing.T) {
	dataDir := t.TempDir()
	if got := resolveWritableHome("", dataDir); got != "" {
		t.Fatalf("HOME 为空时必须原样返回，got=%q", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, ".home")); err == nil {
		t.Fatal("HOME 为空时不该在数据目录里建出 .home")
	}
}

func TestResolveWritableHomeKeepsOriginalWhenDataDirUnknown(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "never-created-home")
	// 数据目录还没初始化时无处可回落，原样返回比乱建目录安全。
	if got := resolveWritableHome(missing, ""); got != missing {
		t.Fatalf("数据目录未知时应原样返回 HOME，want=%q got=%q", missing, got)
	}
}

func TestRedirectHomeEnvReplacesHomeAndCache(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".home")
	env := redirectHomeEnv([]string{
		"PATH=/usr/bin",
		"HOME=/home/daidai",
		"npm_config_cache=/root/.npm",
		"NOT_A_KV_PAIR",
	}, home, "/home/daidai")

	wantHome := "HOME=" + home
	wantCache := "npm_config_cache=" + filepath.Join(home, ".npm")
	if !containsEnvEntry(env, wantHome) {
		t.Fatalf("expected env to carry %q, got %v", wantHome, env)
	}
	if !containsEnvEntry(env, wantCache) {
		t.Fatalf("expected env to carry %q, got %v", wantCache, env)
	}

	// 旧取值必须被剔掉，不能靠 os/exec 的去重语义兜底。
	for _, entry := range env {
		if entry == "npm_config_cache=/root/.npm" || entry == "HOME=/home/daidai" {
			t.Fatalf("旧的不可写取值必须被剔除，env=%v", env)
		}
	}
	for _, keep := range []string{"PATH=/usr/bin", "NOT_A_KV_PAIR"} {
		if !containsEnvEntry(env, keep) {
			t.Fatalf("无关变量必须保留 %q，env=%v", keep, env)
		}
	}
}

func TestRedirectHomeEnvIsNoopWhenHomeUnchanged(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "npm_config_cache=/custom/cache"}

	// 逐项比对而不是只比长度：只比长度的话，把早退守卫删掉也照样绿 ——
	// 那时 env 会被「剔掉 HOME 与 npm_config_cache 再各追加一条」，条数不变，
	// 但用户自设的 npm_config_cache=/custom/cache 已经被顶掉了。
	assertSameEnv := func(desc string, got []string) {
		t.Helper()
		if len(got) != len(base) {
			t.Fatalf("%s：条数应保持 %d，got=%v", desc, len(base), got)
		}
		for i := range base {
			if got[i] != base[i] {
				t.Fatalf("%s：第 %d 项应保持 %q，got=%q（完整=%v）", desc, i, base[i], got[i], got)
			}
		}
	}

	// HOME 本来就可写（解析结果与当前值相同）时一个字节都不该动 ——
	// 用户可能自己设过 npm_config_cache，没理由替他改掉。
	assertSameEnv("HOME 未变化", redirectHomeEnv(base, "/root", "/root"))
	// 解析不出可写 HOME 时同样不动。
	assertSameEnv("解析不出 HOME", redirectHomeEnv(base, "", "/root"))
}

func TestNpmInstallEnvRedirectsHomeWhenUnwritable(t *testing.T) {
	skipOnWindows(t)
	dataDir := useTempDataDir(t)
	t.Setenv("HOME", filepath.Join(dataDir, "never-created-home"))

	env := NpmInstallEnv([]string{"PATH=/usr/bin", "npm_config_cache=/root/.npm"}, "")

	wantHome := "HOME=" + filepath.Join(dataDir, ".home")
	if !containsEnvEntry(env, wantHome) {
		t.Fatalf("expected env to carry %q, got %v", wantHome, env)
	}
	if !containsEnvEntry(env, "npm_config_registry=https://registry.npmmirror.com/") {
		t.Fatalf("镜像源注入不能被 HOME 重定向挤掉，env=%v", env)
	}
}

func TestPipInstallEnvRedirectsHomeWhenUnwritable(t *testing.T) {
	skipOnWindows(t)
	dataDir := useTempDataDir(t)
	t.Setenv("HOME", filepath.Join(dataDir, "never-created-home"))

	env := PipInstallEnv([]string{"PATH=/usr/bin"}, "https://example.com/simple")

	wantHome := "HOME=" + filepath.Join(dataDir, ".home")
	if !containsEnvEntry(env, wantHome) {
		t.Fatalf("pip 的 pip.conf 与 --user 落点同样只认 HOME，expected %q, got %v", wantHome, env)
	}
	if !containsEnvEntry(env, "PIP_INDEX_URL=https://example.com/simple") {
		t.Fatalf("镜像源注入不能被 HOME 重定向挤掉，env=%v", env)
	}
}

func TestInstallEnvLeavesWritableHomeUntouched(t *testing.T) {
	skipOnWindows(t)
	useTempDataDir(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	npmEnv := NpmInstallEnv([]string{"PATH=/usr/bin", "HOME=" + home}, "")
	for _, entry := range npmEnv {
		if strings.HasPrefix(entry, "npm_config_cache=") {
			t.Fatalf("HOME 可写时不应改写 npm cache（用户可能自己设过），env=%v", npmEnv)
		}
	}
	if !containsEnvEntry(npmEnv, "HOME="+home) {
		t.Fatalf("HOME 可写时必须原样保留，env=%v", npmEnv)
	}
}

// 镜像源配置的读写路径必须跟安装时用的 HOME 是同一个，
// 否则会出现「面板里改了镜像源、装依赖时却读不到」的读写不对称。
func TestMirrorConfigPathsFollowEffectiveHome(t *testing.T) {
	skipOnWindows(t)
	dataDir := useTempDataDir(t)
	t.Setenv("HOME", filepath.Join(dataDir, "never-created-home"))
	t.Setenv("XDG_CONFIG_HOME", "")

	fallback := filepath.Join(dataDir, ".home")
	if got := npmConfigPath(); got != filepath.Join(fallback, ".npmrc") {
		t.Fatalf("npmConfigPath 应跟随 EffectiveHomeDir，got=%q", got)
	}
	if got := pipMirrorConfigPath(); got != filepath.Join(fallback, ".config", "pip", "pip.conf") {
		t.Fatalf("pipMirrorConfigPath 应跟随 EffectiveHomeDir，got=%q", got)
	}
}

func containsEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
