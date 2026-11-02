package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// nodeABIRebuildFixture 描述注入的 node / npm 行为，全程不碰真实的 node / npm。
type nodeABIRebuildFixture struct {
	abi      string
	abiErr   error
	broken   []string // 探测时报加载失败的包目录（相对 deps/nodejs/node_modules，斜杠分隔）
	npmLines []string // 假 npm 的脚本行，nil 时正常退出
	npmErr   error    // 非空时模拟 rebuild 命令没构造出来
}

// nodeABIRebuildCalls 记录注入的函数被怎样调用。后台 goroutine 写、测试在 done 关闭后才读，不存在并发访问。
type nodeABIRebuildCalls struct {
	abiProbes    int
	addonProbes  []string   // 被探测的包目录（相对 node_modules）
	rebuildNames [][]string // 每次构造 rebuild 命令时传入的包名
}

func stubNodeABIRebuild(t *testing.T, f nodeABIRebuildFixture) *nodeABIRebuildCalls {
	t.Helper()
	calls := &nodeABIRebuildCalls{}
	fakeNpm := ""
	if f.npmErr == nil {
		lines := f.npmLines
		if lines == nil {
			lines = []string{"echo rebuilt"}
		}
		fakeNpm = writeFakeExecutable(t, t.TempDir(), "fake-npm-rebuild", lines)
	}

	originalABIProbe, originalAddonProbe, originalRebuild := nodeABIProbeFunc, nodeAddonLoadBrokenFunc, newNpmRebuildCommandFunc
	t.Cleanup(func() {
		nodeABIProbeFunc, nodeAddonLoadBrokenFunc, newNpmRebuildCommandFunc = originalABIProbe, originalAddonProbe, originalRebuild
	})
	nodeABIProbeFunc = func() (string, error) {
		calls.abiProbes++
		return f.abi, f.abiErr
	}
	nodeAddonLoadBrokenFunc = func(nodeDir, pkgDir string) bool {
		if nodeDir != testNodeDepsDir() {
			t.Errorf("addon probe got nodeDir %q, want %q", nodeDir, testNodeDepsDir())
		}
		rel := relTestNodeModules(pkgDir)
		calls.addonProbes = append(calls.addonProbes, rel)
		return slices.Contains(f.broken, rel)
	}
	newNpmRebuildCommandFunc = func(nodeDir string, names []string) (*exec.Cmd, error) {
		calls.rebuildNames = append(calls.rebuildNames, names)
		if f.npmErr != nil {
			return nil, f.npmErr
		}
		return exec.Command(fakeNpm), nil
	}
	return calls
}

func testNodeDepsDir() string {
	return filepath.Join(config.C.Data.Dir, "deps", "nodejs")
}

func testNodeModulesDir() string {
	return filepath.Join(testNodeDepsDir(), "node_modules")
}

// relTestNodeModules 把包目录转成相对 node_modules 的斜杠路径，断言与日志都好读。
// 后台 goroutine 里也会调用，所以出错不用 t.Fatal，原样返回让断言自己报出来。
func relTestNodeModules(dir string) string {
	rel, err := filepath.Rel(testNodeModulesDir(), dir)
	if err != nil {
		return filepath.ToSlash(dir)
	}
	return filepath.ToSlash(rel)
}

// seedNodePackage 在 node_modules 下放一个包（rel 可以是 @scope/name 或 a/node_modules/b），native 时带一个 .node 文件。
func seedNodePackage(t *testing.T, rel, name string, native bool) string {
	t.Helper()
	dir := filepath.Join(testNodeModulesDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Join(dir, "build", "Release"), 0o755); err != nil {
		t.Fatalf("mkdir node package %s: %v", rel, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(fmt.Sprintf(`{"name":%q,"version":"1.0.0"}`, name)), 0o644); err != nil {
		t.Fatalf("write package.json %s: %v", rel, err)
	}
	if native {
		if err := os.WriteFile(filepath.Join(dir, "build", "Release", "addon.node"), []byte("ELF"), 0o644); err != nil {
			t.Fatalf("write addon.node %s: %v", rel, err)
		}
	}
	return dir
}

func writeTestNodeABIMarker(t *testing.T, abi string) {
	t.Helper()
	if err := os.MkdirAll(testNodeDepsDir(), 0o755); err != nil {
		t.Fatalf("mkdir node deps dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testNodeDepsDir(), nodeABIMarkerFileName), []byte(abi+"\n"), 0o644); err != nil {
		t.Fatalf("write node abi marker: %v", err)
	}
}

func readTestNodeABIMarker(t *testing.T) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testNodeDepsDir(), nodeABIMarkerFileName))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read node abi marker: %v", err)
	}
	return strings.TrimSpace(string(data)), true
}

func assertTestNodeABIMarker(t *testing.T, want string) {
	t.Helper()
	if got, ok := readTestNodeABIMarker(t); !ok || got != want {
		t.Fatalf("expected marker %q, got %q (exists=%v)", want, got, ok)
	}
}

func assertNoTestNodeABIMarker(t *testing.T) {
	t.Helper()
	if got, ok := readTestNodeABIMarker(t); ok {
		t.Fatalf("expected no marker, got %q", got)
	}
}

// waitNodeABIRebuild 等后台 goroutine 真正结束。上限只是防止实现写坏后整个测试挂死，不是在猜完成时间。
func waitNodeABIRebuild(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("node ABI rebuild did not finish within 60s")
	}
}

func assertNodeABIRebuildDoneImmediately(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	default:
		t.Fatal("expected no background work to be scheduled")
	}
}

func TestRebuildNodeDependenciesIfABIChangedOnlyRunsOnLinux(t *testing.T) {
	for goos, want := range map[string]bool{"linux": true, "windows": false, "darwin": false, "freebsd": false} {
		if got := nodeABIRebuildSupported(goos); got != want {
			t.Fatalf("nodeABIRebuildSupported(%q) = %v, want %v", goos, got, want)
		}
	}

	testutil.SetupTestEnv(t)
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137"})

	// node_modules 不存在：Linux 上同步写标记就结束；其它平台入口直接返回，连 node 都不探测。
	RebuildNodeDependenciesIfABIChanged()

	onLinux := runtime.GOOS == "linux"
	if probed := calls.abiProbes > 0; probed != onLinux {
		t.Fatalf("expected node probed=%v on %s, got %d probes", onLinux, runtime.GOOS, calls.abiProbes)
	}
	if _, exists := readTestNodeABIMarker(t); exists != onLinux {
		t.Fatalf("expected marker written=%v on %s, got %v", onLinux, runtime.GOOS, exists)
	}
}

func TestRebuildNodeDependenciesSkipsWhenABIUnchanged(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "sqlite3", "sqlite3", true)
	writeTestNodeABIMarker(t, "137")
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137\n", broken: []string{"sqlite3"}})

	assertNodeABIRebuildDoneImmediately(t, rebuildNodeDependenciesIfABIChanged())

	if len(calls.addonProbes) != 0 || len(calls.rebuildNames) != 0 {
		t.Fatalf("expected nothing probed or rebuilt when ABI unchanged, got probes=%v rebuilds=%v", calls.addonProbes, calls.rebuildNames)
	}
}

// node_modules 不存在，或者只剩 npm 自己的点条目（依赖全卸光后的常态）：没东西可查，直接记下当前 ABI。
func TestRebuildNodeDependenciesWritesMarkerWhenNoPackages(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{name: "node_modules missing", setup: func(t *testing.T) {}},
		{name: "only npm dot entries", setup: func(t *testing.T) {
			if err := os.MkdirAll(filepath.Join(testNodeModulesDir(), ".bin"), 0o755); err != nil {
				t.Fatalf("mkdir .bin: %v", err)
			}
			if err := os.WriteFile(filepath.Join(testNodeModulesDir(), ".package-lock.json"), []byte("{}"), 0o644); err != nil {
				t.Fatalf("write .package-lock.json: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			tc.setup(t)
			calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137"})

			assertNodeABIRebuildDoneImmediately(t, rebuildNodeDependenciesIfABIChanged())

			if len(calls.addonProbes) != 0 || len(calls.rebuildNames) != 0 {
				t.Fatalf("expected nothing probed or rebuilt without packages, got probes=%v rebuilds=%v", calls.addonProbes, calls.rebuildNames)
			}
			assertTestNodeABIMarker(t, "137")
		})
	}
}

// 纯 JS 包不含 .node：不起 node 探测，也不重建，只写标记。
func TestRebuildNodeDependenciesSkipsPackagesWithoutNativeAddons(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "lodash", "lodash", false)
	writeTestNodeABIMarker(t, "108")
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137"})

	waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())

	if len(calls.addonProbes) != 0 || len(calls.rebuildNames) != 0 {
		t.Fatalf("expected pure JS packages not probed or rebuilt, got probes=%v rebuilds=%v", calls.addonProbes, calls.rebuildNames)
	}
	assertTestNodeABIMarker(t, "137")
}

// 原生扩展在新 Node 上照样能加载（N-API 模块、自带预编译产物）：一律不碰，否则重建失败会把能用的包弄坏。
func TestRebuildNodeDependenciesDoesNotRebuildLoadableNativeAddons(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "sqlite3", "sqlite3", true)
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137"})

	waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())

	if !slices.Equal(calls.addonProbes, []string{"sqlite3"}) {
		t.Fatalf("expected sqlite3 probed once, got %v", calls.addonProbes)
	}
	if len(calls.rebuildNames) != 0 {
		t.Fatalf("expected no rebuild when every native addon loads, got %v", calls.rebuildNames)
	}
	assertTestNodeABIMarker(t, "137")
}

// 只把加载失败的包名交给 npm rebuild（@scope 与嵌套 node_modules 里的也算，同名去重），
// 重建后再探测一次，仍失败的包在日志里提示去依赖管理重装。
func TestRebuildNodeDependenciesRebuildsOnlyBrokenPackages(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "@napi/canvas", "@napi/canvas", true)
	seedNodePackage(t, "app", "app", false)
	seedNodePackage(t, "app/node_modules/sqlite3", "sqlite3", true)
	seedNodePackage(t, "bcrypt", "bcrypt", true)
	seedNodePackage(t, "lodash", "lodash", false)
	seedNodePackage(t, "sqlite3", "sqlite3", true)
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{
		abi:    "137",
		broken: []string{"@napi/canvas", "app/node_modules/sqlite3", "sqlite3"},
	})
	logs := captureStandardLog(t)

	waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())

	if len(calls.rebuildNames) != 1 || !slices.Equal(calls.rebuildNames[0], []string{"@napi/canvas", "sqlite3"}) {
		t.Fatalf("expected exactly one rebuild for [@napi/canvas sqlite3], got %v", calls.rebuildNames)
	}
	wantProbes := []string{
		"@napi/canvas", "app/node_modules/sqlite3", "bcrypt", "sqlite3", // 重建前逐包探测
		"@napi/canvas", "app/node_modules/sqlite3", "sqlite3", // 重建后只复查失败的包
	}
	if !slices.Equal(calls.addonProbes, wantProbes) {
		t.Fatalf("expected probes %v, got %v", wantProbes, calls.addonProbes)
	}
	if !strings.Contains(logs.String(), "仍然加载失败") || !strings.Contains(logs.String(), "依赖管理里重装") {
		t.Fatalf("expected still-broken packages logged with a reinstall hint, got:\n%s", logs.String())
	}
	assertTestNodeABIMarker(t, "137")
}

// 找不到 npm 时根本没有尝试重建：不能写标记，否则 npm 装回来之后同一个 ABI 再也不会重建。
func TestRebuildNodeDependenciesDoesNotWriteMarkerWhenNpmMissing(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "sqlite3", "sqlite3", true)
	calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{
		abi:    "137",
		broken: []string{"sqlite3"},
		npmErr: fmt.Errorf("未找到 npm: %w", exec.ErrNotFound),
	})

	waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())

	if len(calls.rebuildNames) != 1 {
		t.Fatalf("expected one rebuild attempt, got %v", calls.rebuildNames)
	}
	assertNoTestNodeABIMarker(t)

	// 下一次启动必须再试一次。
	waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())
	if len(calls.rebuildNames) != 2 {
		t.Fatalf("expected rebuild retried on next startup when npm was missing, got %v", calls.rebuildNames)
	}
}

// rebuild 失败（退出码非 0 / 命令没构造出来）：只记日志，依赖表一个字段都不能变；
// 标记照样写入，同一个 ABI 下次启动不再重试。
func TestRebuildNodeDependenciesFailureKeepsDependencyRowsAndWritesMarker(t *testing.T) {
	cases := []struct {
		name     string
		npmLines []string
		npmErr   error
		wantLog  string
	}{
		{name: "rebuild exits non-zero", npmLines: []string{"echo gyp ERR! build error", "exit 3"}, wantLog: "gyp ERR! build error"},
		{name: "rebuild command not built", npmErr: errors.New("写入 Node.js package.json 失败"), wantLog: "写入 Node.js package.json 失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			seedNodePackage(t, "sqlite3", "sqlite3", true)
			dep := model.Dependency{Type: model.DepTypeNodeJS, Name: "sqlite3", Status: model.DepStatusInstalled, Log: "installed before upgrade"}
			if err := database.DB.Create(&dep).Error; err != nil {
				t.Fatalf("seed node dependency: %v", err)
			}
			var before model.Dependency
			if err := database.DB.First(&before, dep.ID).Error; err != nil {
				t.Fatalf("reload seeded dependency: %v", err)
			}
			calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: "137", broken: []string{"sqlite3"}, npmLines: tc.npmLines, npmErr: tc.npmErr})
			logs := captureStandardLog(t)

			waitNodeABIRebuild(t, rebuildNodeDependenciesIfABIChanged())

			if len(calls.rebuildNames) != 1 {
				t.Fatalf("expected one rebuild attempt, got %v", calls.rebuildNames)
			}
			for _, want := range []string{"npm rebuild sqlite3 失败", tc.wantLog, "依赖管理里重装"} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("expected log to contain %q, got:\n%s", want, logs.String())
				}
			}

			var after model.Dependency
			if err := database.DB.First(&after, dep.ID).Error; err != nil {
				t.Fatalf("reload dependency after failed rebuild: %v", err)
			}
			if after.Status != before.Status || after.Log != before.Log || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("expected dependency row untouched by failed rebuild, before=%+v after=%+v", before, after)
			}
			var total int64
			database.DB.Model(&model.Dependency{}).Count(&total)
			if total != 1 {
				t.Fatalf("expected dependency table unchanged (1 row), got %d", total)
			}

			assertTestNodeABIMarker(t, "137")
			assertNodeABIRebuildDoneImmediately(t, rebuildNodeDependenciesIfABIChanged())
			if len(calls.rebuildNames) != 1 {
				t.Fatalf("expected failed ABI not retried on next startup, got %v", calls.rebuildNames)
			}
		})
	}
}

// node 不可用、或输出不是 ABI 号：既不探测包也不写标记，等 node 恢复后的下一次启动再判。
func TestRebuildNodeDependenciesSkipsWhenNodeProbeFails(t *testing.T) {
	cases := []struct {
		name string
		abi  string
		err  error
	}{
		{name: "node not found", err: exec.ErrNotFound},
		{name: "node probe failed", err: errors.New("signal: killed")},
		{name: "empty output", abi: "\n"},
		{name: "garbage output", abi: "preload banner\n137"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			seedNodePackage(t, "sqlite3", "sqlite3", true)
			calls := stubNodeABIRebuild(t, nodeABIRebuildFixture{abi: tc.abi, abiErr: tc.err, broken: []string{"sqlite3"}})

			assertNodeABIRebuildDoneImmediately(t, rebuildNodeDependenciesIfABIChanged())

			if len(calls.addonProbes) != 0 || len(calls.rebuildNames) != 0 {
				t.Fatalf("expected nothing probed or rebuilt, got probes=%v rebuilds=%v", calls.addonProbes, calls.rebuildNames)
			}
			assertNoTestNodeABIMarker(t)
		})
	}
}

// 收集口径：@scope/<name>、嵌套 node_modules 里的包都要收；包自身是否含原生扩展不看它嵌套 node_modules 里的文件；
// 点开头目录不算包，目录名以 .node 结尾也不算原生扩展。
func TestNodeABICollectNativeAddonPackagesWalksScopesAndNestedModules(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "@scope/native", "@scope/native", true)
	seedNodePackage(t, "@scope/plain", "@scope/plain", false)
	seedNodePackage(t, "host", "host", false)
	seedNodePackage(t, "host/node_modules/inner", "inner", true)
	seedNodePackage(t, "host/node_modules/@deep/addon", "@deep/addon", true)
	seedNodePackage(t, ".cache/stale", "stale", true)
	fake := seedNodePackage(t, "fake-dir", "fake-dir", false)
	if err := os.MkdirAll(filepath.Join(fake, "lib", "not-an-addon.node"), 0o755); err != nil {
		t.Fatalf("mkdir directory named *.node: %v", err)
	}

	var got []string
	for _, dir := range collectNodeNativeAddonPackages(testNodeModulesDir()) {
		got = append(got, relTestNodeModules(dir))
	}
	want := []string{"@scope/native", "host/node_modules/@deep/addon", "host/node_modules/inner"}
	if !slices.Equal(got, want) {
		t.Fatalf("expected native addon packages %v, got %v", want, got)
	}
}

// 软链接不跟随：file: 本地目录包、链接到别处的嵌套 node_modules、链接进来的 .node 文件都不算。
func TestNodeABICollectNativeAddonPackagesSkipsSymlinks(t *testing.T) {
	testutil.SetupTestEnv(t)
	seedNodePackage(t, "real", "real", true)
	outside := t.TempDir()
	linkedPkg := filepath.Join(outside, "linked-pkg")
	if err := os.MkdirAll(linkedPkg, 0o755); err != nil {
		t.Fatalf("mkdir linked package: %v", err)
	}
	if err := os.WriteFile(filepath.Join(linkedPkg, "addon.node"), []byte("ELF"), 0o644); err != nil {
		t.Fatalf("write linked addon: %v", err)
	}
	if err := os.Symlink(linkedPkg, filepath.Join(testNodeModulesDir(), "linked")); err != nil {
		t.Skipf("symlink not supported here: %v", err)
	}
	host := seedNodePackage(t, "host", "host", false)
	if err := os.MkdirAll(filepath.Join(outside, "modules", "inner"), 0o755); err != nil {
		t.Fatalf("mkdir outside modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "modules", "inner", "addon.node"), []byte("ELF"), 0o644); err != nil {
		t.Fatalf("write outside nested addon: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "modules"), filepath.Join(host, "node_modules")); err != nil {
		t.Fatalf("symlink nested node_modules: %v", err)
	}
	if err := os.Symlink(filepath.Join(linkedPkg, "addon.node"), filepath.Join(host, "linked.node")); err != nil {
		t.Fatalf("symlink addon file: %v", err)
	}

	var got []string
	for _, dir := range collectNodeNativeAddonPackages(testNodeModulesDir()) {
		got = append(got, relTestNodeModules(dir))
	}
	if want := []string{"real"}; !slices.Equal(got, want) {
		t.Fatalf("expected only the real package %v, got %v", want, got)
	}
}

func TestNewNpmRebuildCommandTargetsBrokenPackages(t *testing.T) {
	testutil.SetupTestEnv(t)
	nodeDir := testNodeDepsDir()
	// 先确认 npm 在 PATH 上的检查要放一个假的，免得用例结果跟着宿主机有没有装 npm 走。
	fakeBin := t.TempDir()
	writeFakeExecutable(t, fakeBin, "npm", []string{"exit 0"})
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	const proxyURL = "http://127.0.0.1:7890"
	if err := model.SetConfig("proxy_url", proxyURL); err != nil {
		t.Fatalf("set proxy_url: %v", err)
	}

	cmd, err := newNpmRebuildCommand(nodeDir, []string{"sqlite3", "@napi/canvas"})
	if err != nil {
		t.Fatalf("build npm rebuild command: %v", err)
	}
	if want := []string{"npm", "rebuild", "--prefix", nodeDir, "sqlite3", "@napi/canvas"}; !slices.Equal(cmd.Args, want) {
		t.Fatalf("expected args %q, got %q", want, cmd.Args)
	}
	// 与 npm install 同一套前置：缺 package.json 时先补出来。
	if _, err := os.Stat(filepath.Join(nodeDir, "package.json")); err != nil {
		t.Fatalf("expected package.json ensured before rebuild: %v", err)
	}
	// 必须带上 npm 安装的那套环境：镜像源与代理（本机环境里可能本来就有 HTTPS_PROXY，所以按值核对）。
	if got, want := lastEnvValue(cmd.Env, "npm_config_registry"), EffectiveNpmMirror(CurrentNpmMirror()); got != want {
		t.Fatalf("expected rebuild env npm_config_registry=%q, got %q", want, got)
	}
	if got := lastEnvValue(cmd.Env, "HTTPS_PROXY"); got != proxyURL {
		t.Fatalf("expected rebuild env HTTPS_PROXY=%q, got %q", proxyURL, got)
	}
}

// PATH 上没有 npm 时要报出 exec.ErrNotFound（调用方据此不写标记），而且不能先去改写 package.json。
func TestNewNpmRebuildCommandReportsMissingNpm(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("PATH", t.TempDir())
	nodeDir := testNodeDepsDir()

	cmd, err := newNpmRebuildCommand(nodeDir, []string{"sqlite3"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("expected exec.ErrNotFound when npm is not on PATH, got cmd=%v err=%v", cmd, err)
	}
	if _, statErr := os.Stat(filepath.Join(nodeDir, "package.json")); !os.IsNotExist(statErr) {
		t.Fatalf("expected package.json not touched when npm is missing, stat err=%v", statErr)
	}
}

// lastEnvValue 取 key 最后一次出现的值：exec.Cmd 对重复键以最后一个为准。
func lastEnvValue(env []string, key string) string {
	value := ""
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == key {
			value = v
		}
	}
	return value
}

func TestTailNodeABIRebuildOutputKeepsRuneBoundary(t *testing.T) {
	got := tailNodeABIRebuildOutput([]byte(strings.Repeat("编", 2000)))
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("expected long output to be truncated with prefix, got %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "编") || !utf8.ValidString(got) {
		t.Fatalf("expected truncation on a rune boundary, got prefix %q", got[:12])
	}
}
