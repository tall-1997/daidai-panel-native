package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// stubPlaywrightDeployment 把部署形态（平台 / 是否容器 / 是否面具版）换成给定值，用例结束自动还原。
// 同时把进程环境里的 PLAYWRIGHT_BROWSERS_PATH 清空并登记还原：
// ApplyPlaywrightBrowsersPathProcessEnv 会 os.Setenv，不还原就会串到后面的用例。
func stubPlaywrightDeployment(t *testing.T, goos string, inContainer, magisk bool) {
	t.Helper()
	oldGOOS, oldContainer, oldMagisk := playwrightGOOS, playwrightInContainerFunc, playwrightMagiskFunc
	t.Cleanup(func() {
		playwrightGOOS, playwrightInContainerFunc, playwrightMagiskFunc = oldGOOS, oldContainer, oldMagisk
	})
	playwrightGOOS = goos
	playwrightInContainerFunc = func() bool { return inContainer }
	playwrightMagiskFunc = func() bool { return magisk }
	t.Setenv(PlaywrightBrowsersPathEnv, "")
}

func expectedDefaultBrowsersPath() string {
	return filepath.Join(config.C.Data.Dir, "deps", "ms-playwright")
}

func TestRunningInContainerDetectsMarkersAndCgroup(t *testing.T) {
	oldMarkers, oldCgroup := containerMarkerFiles, containerCgroupFile
	t.Cleanup(func() {
		containerMarkerFiles, containerCgroupFile = oldMarkers, oldCgroup
	})

	dir := t.TempDir()
	marker := filepath.Join(dir, ".dockerenv")
	cgroup := filepath.Join(dir, "cgroup")
	containerMarkerFiles = []string{marker}
	containerCgroupFile = cgroup

	if runningInContainer() {
		t.Fatal("没有标志文件、也没有 cgroup 文件时不应判成容器")
	}

	if err := os.WriteFile(cgroup, []byte("0::/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runningInContainer() {
		t.Fatal("cgroup v2 的 0::/ 认不出运行时，应退回「不是容器」")
	}

	if err := os.WriteFile(cgroup, []byte("12:pids:/docker/abc123\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !runningInContainer() {
		t.Fatal("cgroup 里带 docker 应判成容器")
	}

	if err := os.WriteFile(cgroup, []byte("0::/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !runningInContainer() {
		t.Fatal("有 /.dockerenv 标志文件应判成容器")
	}
}

// 只有容器部署才有默认目录。Windows、裸机、面具版改掉默认目录都会让已装好的浏览器「消失」
// （面具版还会被 deps 快照反复拷贝数百 MB），一律返回空。
func TestDefaultPlaywrightBrowsersPathOnlyForContainers(t *testing.T) {
	testutil.SetupTestEnv(t)

	cases := []struct {
		name        string
		goos        string
		inContainer bool
		magisk      bool
		wantDefault bool
	}{
		{name: "Linux 容器", goos: "linux", inContainer: true, wantDefault: true},
		{name: "Linux 裸机", goos: "linux", inContainer: false},
		{name: "面具版（即便 cgroup 看着像容器）", goos: "linux", inContainer: true, magisk: true},
		{name: "Windows 桌面版", goos: "windows", inContainer: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubPlaywrightDeployment(t, tc.goos, tc.inContainer, tc.magisk)
			got := DefaultPlaywrightBrowsersPath()
			want := ""
			if tc.wantDefault {
				want = expectedDefaultBrowsersPath()
			}
			if got != want {
				t.Fatalf("期望 %q，实际 %q", want, got)
			}
		})
	}
}

// 任务环境：用户没配时补上默认目录；进程环境优先于默认值；"0" 这类用户值原样透传；非容器不注入。
func TestManagedRuntimeEnvInjectsPlaywrightBrowsersPath(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	build := func(t *testing.T) map[string]string {
		t.Helper()
		envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
		if err != nil {
			t.Fatalf("build managed runtime env map: %v", err)
		}
		return envMap
	}

	t.Run("容器且没人设过时补默认目录", func(t *testing.T) {
		stubPlaywrightDeployment(t, "linux", true, false)
		if got := build(t)[PlaywrightBrowsersPathEnv]; got != expectedDefaultBrowsersPath() {
			t.Fatalf("期望默认目录 %q，实际 %q", expectedDefaultBrowsersPath(), got)
		}
	})

	t.Run("进程环境优先于默认目录", func(t *testing.T) {
		stubPlaywrightDeployment(t, "linux", true, false)
		t.Setenv(PlaywrightBrowsersPathEnv, "/from/compose")
		if got := build(t)[PlaywrightBrowsersPathEnv]; got != "/from/compose" {
			t.Fatalf("进程环境里的值应优先，实际 %q", got)
		}
	})

	t.Run("0 原样透传不当空值", func(t *testing.T) {
		stubPlaywrightDeployment(t, "linux", true, false)
		t.Setenv(PlaywrightBrowsersPathEnv, "0")
		if got := build(t)[PlaywrightBrowsersPathEnv]; got != "0" {
			t.Fatalf("PLAYWRIGHT_BROWSERS_PATH=0 必须原样透传，实际 %q", got)
		}
	})

	t.Run("非容器不注入", func(t *testing.T) {
		stubPlaywrightDeployment(t, "linux", false, false)
		if value, exists := build(t)[PlaywrightBrowsersPathEnv]; exists {
			t.Fatalf("非容器部署不应注入默认目录，实际注入了 %q", value)
		}
	})
}

// 环境变量页里已有同名变量时必须原样生效，不能被默认目录覆盖（语义同 QL_DIR，而不是 TZ）。
func TestManagedRuntimeEnvKeepsUserPlaywrightBrowsersPath(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	stubPlaywrightDeployment(t, "linux", true, false)

	if err := database.DB.Create(&model.EnvVar{
		Name:    PlaywrightBrowsersPathEnv,
		Value:   "/my/browsers",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if got := envMap[PlaywrightBrowsersPathEnv]; got != "/my/browsers" {
		t.Fatalf("用户在环境变量页设的目录必须优先，实际 %q", got)
	}
}

// 下载目录必须与任务看到的是同一个：环境变量页 > config.sh > 进程环境 > 默认目录。
func TestResolvePlaywrightBrowsersPathMatchesTaskPriority(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubPlaywrightDeployment(t, "linux", true, false)

	if got := ResolvePlaywrightBrowsersPath(); got != expectedDefaultBrowsersPath() {
		t.Fatalf("什么都没设时应是默认目录，实际 %q", got)
	}

	t.Setenv(PlaywrightBrowsersPathEnv, "/from/process")
	if got := ResolvePlaywrightBrowsersPath(); got != "/from/process" {
		t.Fatalf("进程环境应优先于默认目录，实际 %q", got)
	}

	configSh := filepath.Join(config.C.Data.Dir, "config.sh")
	if err := os.WriteFile(configSh, []byte("export PLAYWRIGHT_BROWSERS_PATH=\"/from/config-sh\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolvePlaywrightBrowsersPath(); got != "/from/config-sh" {
		t.Fatalf("config.sh 里的值会进任务环境，应优先于进程环境，实际 %q", got)
	}

	if err := database.DB.Create(&model.EnvVar{Name: PlaywrightBrowsersPathEnv, Value: "/from/env-page", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	if got := ResolvePlaywrightBrowsersPath(); got != "/from/env-page" {
		t.Fatalf("环境变量页优先级最高，实际 %q", got)
	}

	// 禁用的变量任务里拿不到，这里也不能认。
	database.DB.Model(&model.EnvVar{}).Where("name = ?", PlaywrightBrowsersPathEnv).Update("enabled", false)
	if got := ResolvePlaywrightBrowsersPath(); got != "/from/config-sh" {
		t.Fatalf("禁用的环境变量不应生效，实际 %q", got)
	}
}

// 订阅钩子不读环境变量页，所以它要拿 Resolve 的结果，才能与任务看到的目录一致。
func TestSubscriptionHookEnvCarriesPlaywrightBrowsersPath(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubPlaywrightDeployment(t, "linux", true, false)

	sub := &model.Subscription{ID: 1, Name: "demo", Type: model.SubTypeGitRepo, URL: "https://example.com/a/b.git"}
	env := buildSubscriptionHookEnv(sub, config.C.Data.ScriptsDir)
	if got := env[PlaywrightBrowsersPathEnv]; got != expectedDefaultBrowsersPath() {
		t.Fatalf("钩子环境应带上默认目录，实际 %q", got)
	}

	if err := database.DB.Create(&model.EnvVar{Name: PlaywrightBrowsersPathEnv, Value: "/my/browsers", Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	env = buildSubscriptionHookEnv(sub, config.C.Data.ScriptsDir)
	if got := env[PlaywrightBrowsersPathEnv]; got != "/my/browsers" {
		t.Fatalf("钩子要与任务一致地采用环境变量页里的目录，实际 %q", got)
	}
}

func TestApplyPlaywrightBrowsersPathProcessEnv(t *testing.T) {
	t.Run("容器下写入进程环境并建目录", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		stubPlaywrightDeployment(t, "linux", true, false)

		ApplyPlaywrightBrowsersPathProcessEnv()

		want := expectedDefaultBrowsersPath()
		if got := os.Getenv(PlaywrightBrowsersPathEnv); got != want {
			t.Fatalf("进程环境应为 %q，实际 %q", want, got)
		}
		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Fatalf("默认目录应被建出来，err=%v", err)
		}
	})

	t.Run("进程环境已有值时原样尊重", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		stubPlaywrightDeployment(t, "linux", true, false)
		t.Setenv(PlaywrightBrowsersPathEnv, "/from/compose")

		ApplyPlaywrightBrowsersPathProcessEnv()

		if got := os.Getenv(PlaywrightBrowsersPathEnv); got != "/from/compose" {
			t.Fatalf("用户显式设的值不能被改掉，实际 %q", got)
		}
		if _, err := os.Stat(expectedDefaultBrowsersPath()); err == nil {
			t.Fatal("用户自己设了目录时不应再去建默认目录")
		}
	})

	t.Run("非容器什么都不做", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		stubPlaywrightDeployment(t, "linux", false, false)

		ApplyPlaywrightBrowsersPathProcessEnv()

		if got := os.Getenv(PlaywrightBrowsersPathEnv); got != "" {
			t.Fatalf("非容器部署不应写进程环境，实际 %q", got)
		}
	})

	// PUID 降权部署的存量浏览器在 <data>/.home/.cache/ms-playwright，默认目录一改不搬就会报找不到浏览器。
	t.Run("PUID 存量浏览器搬到新目录", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		stubPlaywrightDeployment(t, "linux", true, false)
		legacy := filepath.Join(config.C.Data.Dir, ".home", ".cache", "ms-playwright")
		writeFakeBrowser(t, legacy, "chromium_headless_shell-1181")

		ApplyPlaywrightBrowsersPathProcessEnv()

		target := expectedDefaultBrowsersPath()
		if _, err := os.Stat(filepath.Join(target, "chromium_headless_shell-1181", "INSTALLATION_COMPLETE")); err != nil {
			t.Fatalf("旧目录里的浏览器应被搬到 %s，err=%v", target, err)
		}
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Fatalf("搬迁后旧目录应已不存在，err=%v", err)
		}
	})

	// 用户在环境变量页自己指定了目录（甚至可能就是旧目录）时，搬走等于把他正在用的浏览器挪没了。
	t.Run("用户在环境变量页指定目录时不搬", func(t *testing.T) {
		testutil.SetupTestEnv(t)
		stubPlaywrightDeployment(t, "linux", true, false)
		legacy := filepath.Join(config.C.Data.Dir, ".home", ".cache", "ms-playwright")
		writeFakeBrowser(t, legacy, "chromium-1181")
		if err := database.DB.Create(&model.EnvVar{Name: PlaywrightBrowsersPathEnv, Value: legacy, Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}

		ApplyPlaywrightBrowsersPathProcessEnv()

		if _, err := os.Stat(filepath.Join(legacy, "chromium-1181")); err != nil {
			t.Fatalf("用户正在用的目录不能被搬走，err=%v", err)
		}
	})
}

func TestMigrateLegacyPlaywrightBrowsers(t *testing.T) {
	t.Run("新目录不存在时整体搬过去", func(t *testing.T) {
		dir := t.TempDir()
		legacy, target := filepath.Join(dir, "legacy"), filepath.Join(dir, "deps", "ms-playwright")
		writeFakeBrowser(t, legacy, "chromium-1181")

		moved, err := migrateLegacyPlaywrightBrowsers(legacy, target)
		if err != nil || !moved {
			t.Fatalf("应搬迁成功，moved=%v err=%v", moved, err)
		}
		if _, err := os.Stat(filepath.Join(target, "chromium-1181")); err != nil {
			t.Fatalf("新目录里应有浏览器，err=%v", err)
		}
	})

	t.Run("新目录是空目录时照样搬", func(t *testing.T) {
		dir := t.TempDir()
		legacy, target := filepath.Join(dir, "legacy"), filepath.Join(dir, "target")
		writeFakeBrowser(t, legacy, "chromium-1181")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}

		moved, err := migrateLegacyPlaywrightBrowsers(legacy, target)
		if err != nil || !moved {
			t.Fatalf("空的新目录不应妨碍搬迁，moved=%v err=%v", moved, err)
		}
	})

	t.Run("新目录已有内容时不合并不覆盖", func(t *testing.T) {
		dir := t.TempDir()
		legacy, target := filepath.Join(dir, "legacy"), filepath.Join(dir, "target")
		writeFakeBrowser(t, legacy, "chromium-1181")
		writeFakeBrowser(t, target, "chromium-1200")

		moved, err := migrateLegacyPlaywrightBrowsers(legacy, target)
		if err != nil || moved {
			t.Fatalf("两边都有内容时不应搬，moved=%v err=%v", moved, err)
		}
		if _, err := os.Stat(filepath.Join(legacy, "chromium-1181")); err != nil {
			t.Fatalf("旧目录应原样保留，err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(target, "chromium-1181")); !os.IsNotExist(err) {
			t.Fatalf("新目录不应被合并进旧内容，err=%v", err)
		}
	})

	t.Run("旧目录为空或不存在时什么都不做", func(t *testing.T) {
		dir := t.TempDir()
		legacy, target := filepath.Join(dir, "legacy"), filepath.Join(dir, "target")
		if moved, err := migrateLegacyPlaywrightBrowsers(legacy, target); err != nil || moved {
			t.Fatalf("旧目录不存在时不应搬，moved=%v err=%v", moved, err)
		}
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if moved, err := migrateLegacyPlaywrightBrowsers(legacy, target); err != nil || moved {
			t.Fatalf("旧目录为空时不应搬，moved=%v err=%v", moved, err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("没搬时不应凭空建出新目录，err=%v", err)
		}
	})
}

func writeFakeBrowser(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "INSTALLATION_COMPLETE"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWithEnvEntryKeepsSingleValue(t *testing.T) {
	env := []string{"A=1", PlaywrightBrowsersPathEnv + "=/old", "B=2", PlaywrightBrowsersPathEnv + "=/older"}
	got := withEnvEntry(env, PlaywrightBrowsersPathEnv, "/new")

	count := 0
	for _, entry := range got {
		if len(entry) > len(PlaywrightBrowsersPathEnv) && entry[:len(PlaywrightBrowsersPathEnv)+1] == PlaywrightBrowsersPathEnv+"=" {
			count++
			if entry != PlaywrightBrowsersPathEnv+"=/new" {
				t.Fatalf("应只保留新值，实际 %q", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("同名变量应只剩一份，实际 %d 份：%v", count, got)
	}
	// 前缀相同的别的变量不能被误删。
	if got := withEnvEntry([]string{"PLAYWRIGHT_BROWSERS_PATH_X=1"}, PlaywrightBrowsersPathEnv, "/new"); len(got) != 2 {
		t.Fatalf("不应误删前缀相同的其它变量：%v", got)
	}
}
