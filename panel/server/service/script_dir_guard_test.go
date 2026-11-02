package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"daidai-panel/config"
)

func TestShouldIgnoreScriptEntryName(t *testing.T) {
	if !ShouldIgnoreScriptEntryName("node_modules") {
		t.Fatal("expected node_modules to be ignored")
	}
	if !ShouldIgnoreScriptEntryName("__pycache__") {
		t.Fatal("expected __pycache__ to be ignored")
	}
	if !ShouldIgnoreScriptEntryName("%SystemDrive%") {
		t.Fatal("expected %SystemDrive% to be ignored")
	}
	if ShouldIgnoreScriptEntryName("demo") {
		t.Fatal("expected normal directory not to be ignored")
	}
}

func TestShouldIgnoreScriptPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "%SystemDrive%", "ProgramData", "demo.db")
	if !ShouldIgnoreScriptPath(root, target) {
		t.Fatal("expected quarantined subtree path to be ignored")
	}

	normal := filepath.Join(root, "jobs", "demo.py")
	if ShouldIgnoreScriptPath(root, normal) {
		t.Fatal("expected normal script path not to be ignored")
	}
}

func TestShouldIgnoreScriptRelativePath(t *testing.T) {
	if !ShouldIgnoreScriptRelativePath("%SystemDrive%/ProgramData/test.db") {
		t.Fatal("expected quarantined relative path to be ignored")
	}
	if ShouldIgnoreScriptRelativePath("demo/regression.py") {
		t.Fatal("expected normal relative path not to be ignored")
	}
}

func TestShouldHideScriptTreeEntryName(t *testing.T) {
	for _, name := range []string{".git", ".GIT", " .git ", ".svn", ".hg", ".bzr", "node_modules", "__pycache__"} {
		if !ShouldHideScriptTreeEntryName(name) {
			t.Fatalf("expected %q to be hidden from the script tree", name)
		}
	}

	// v2.2.17 起 dotfile 必须保持可见，隐藏名单不能扩大成“一刀切隐藏隐藏文件”。
	for _, name := range []string{".env", ".hidden-dir", ".hidden-file", ".github", "demo.py"} {
		if ShouldHideScriptTreeEntryName(name) {
			t.Fatalf("expected %q to stay visible in the script tree", name)
		}
	}

	// 两套名单不能合并：ShouldIgnoreScriptEntryName 还决定备份打包 / 恢复跳过哪些路径，
	// 它对 .git 返回 true 的话，订阅仓库的 git 元数据就进不了备份。
	// 启动期隔离只认 quarantinedScriptDirNames（命中即 os.Rename 物理搬走），那份名单更不能有 .git。
	for _, name := range []string{".git", ".svn", ".hg", ".bzr"} {
		if ShouldIgnoreScriptEntryName(name) {
			t.Fatalf("ShouldIgnoreScriptEntryName(%q) must stay false, backup packing reuses it", name)
		}
		if quarantinedScriptDirNames[name] {
			t.Fatalf("quarantinedScriptDirNames must not contain %q, startup quarantine moves it away", name)
		}
	}
}

func TestShouldHideScriptTreeRelativePath(t *testing.T) {
	hidden := []string{
		".git",
		".git/config",
		"SmallWorld/.git/config",
		"SmallWorld/.git",
		"a/b/.GIT/objects/ab/cdef",
		"SmallWorld/node_modules/pkg/index.js",
	}
	for _, relPath := range hidden {
		if !ShouldHideScriptTreeRelativePath(relPath) {
			t.Fatalf("expected %q to be hidden", relPath)
		}
	}

	visible := []string{"", ".", "/", "SmallWorld/demo.py", ".hidden-dir/.env", "gitlab/config"}
	for _, relPath := range visible {
		if ShouldHideScriptTreeRelativePath(relPath) {
			t.Fatalf("expected %q to stay visible", relPath)
		}
	}
}

func TestShouldHideScriptTreePath(t *testing.T) {
	root := t.TempDir()

	if !ShouldHideScriptTreePath(root, filepath.Join(root, "SmallWorld", ".git", "config")) {
		t.Fatal("expected nested .git/config to be hidden")
	}
	if !ShouldHideScriptTreePath(root, filepath.Join(root, ".git", "HEAD")) {
		t.Fatal("expected top-level .git/HEAD to be hidden")
	}
	if ShouldHideScriptTreePath(root, filepath.Join(root, "SmallWorld", "demo.py")) {
		t.Fatal("expected normal script path to stay visible")
	}
	// 路径不在脚本目录内时不做判定，交给上游的穿越校验处理
	if ShouldHideScriptTreePath(root, filepath.Join(filepath.Dir(root), "outside", ".git")) {
		t.Fatal("expected path outside scripts dir not to be treated as hidden")
	}
}

func TestQuarantineDoesNotMoveGitRepository(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	dataDir := t.TempDir()
	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir

	QuarantineUnexpectedScriptEntriesOnStartup()

	if _, err := os.Stat(filepath.Join(scriptsDir, ".git", "config")); err != nil {
		t.Fatalf("expected .git to stay in place, stat err=%v", err)
	}
}

func TestQuarantineUnexpectedScriptEntriesOnStartup(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	dataDir := t.TempDir()
	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(filepath.Join(scriptsDir, "%SystemDrive%", "ProgramData"), 0o755); err != nil {
		t.Fatalf("mkdir polluted scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "%SystemDrive%", "ProgramData", "demo.db"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write polluted file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "regression.py"), []byte("print('ok')"), 0o644); err != nil {
		t.Fatalf("write normal script: %v", err)
	}

	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir

	QuarantineUnexpectedScriptEntriesOnStartup()

	if _, err := os.Stat(filepath.Join(scriptsDir, "%SystemDrive%")); !os.IsNotExist(err) {
		t.Fatalf("expected polluted directory to be moved away, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "regression.py")); err != nil {
		t.Fatalf("expected normal script to stay in place: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "quarantine", "scripts", "%SystemDrive%", "ProgramData", "demo.db")); err != nil {
		t.Fatalf("expected polluted directory to be quarantined: %v", err)
	}
}

// useStartupScriptDirs 把 config.C 指到一个临时数据目录，用例结束还原。返回数据目录与脚本目录。
func useStartupScriptDirs(t *testing.T) (string, string) {
	t.Helper()
	oldConfig := config.C
	t.Cleanup(func() { config.C = oldConfig })

	dataDir := t.TempDir()
	scriptsDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir
	return dataDir, scriptsDir
}

func writeStartupScriptFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustCreateNodeModulesLink 用面板自己的写法（createManagedDirectoryLink：Windows 上是 junction，其它平台是软链）建目录链接。
// 当前环境建不了时整条用例跳过（这类用例必须交叉编译到 Linux 再跑一遍）。
func mustCreateNodeModulesLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(link), err)
	}
	if err := createManagedDirectoryLink(target, link); err != nil {
		t.Skipf("当前环境建不了目录软链 / junction，跳过：%v", err)
	}
}

// 契约 S2：启动期隔离只搬 quarantinedScriptDirNames 里的污染目录。顶层的真 node_modules（用户自己 npm install 的）、
// __pycache__（被 import 的模块每次运行都会生成）都留在原地，quarantine 里也不再多出它们的副本（#156）。
func TestQuarantineKeepsNodeModulesAndPycacheInPlace(t *testing.T) {
	dataDir, scriptsDir := useStartupScriptDirs(t)

	nodeModulesFile := filepath.Join(scriptsDir, "node_modules", "left-pad", "index.js")
	pycacheFile := filepath.Join(scriptsDir, "__pycache__", "notify.cpython-312.pyc")
	writeStartupScriptFile(t, nodeModulesFile, "module.exports = 1\n")
	writeStartupScriptFile(t, pycacheFile, "pyc")
	writeStartupScriptFile(t, filepath.Join(scriptsDir, "%SystemDrive%", "ProgramData", "demo.db"), "x")

	QuarantineUnexpectedScriptEntriesOnStartup()

	for _, kept := range []string{nodeModulesFile, pycacheFile} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("%s 应留在原地，stat err=%v", kept, err)
		}
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "%SystemDrive%")); !os.IsNotExist(err) {
		t.Fatalf("真正的污染目录仍要隔离，stat err=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "quarantine", "scripts"))
	if err != nil {
		t.Fatalf("read quarantine dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "%SystemDrive%" {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("quarantine 里只应有被隔离的污染目录，实际 %v", names)
	}
}

// 不悬空的软链启动时一律不动：指向托管依赖目录的面板软链（根目录、子目录都算）与面板现在会建的等价，留着无害，
// 而且 ddp task run、被强杀的面板留下的孤儿任务可能正在用它；指向旧数据目录但目标还在的、指向别处的、
// 相对路径的也都不动，真 node_modules 目录原样保留。不往 quarantine 里搬任何东西（#156）。
func TestQuarantineKeepsLiveNodeModulesLinks(t *testing.T) {
	dataDir, scriptsDir := useStartupScriptDirs(t)

	managed := filepath.Join(dataDir, "deps", "nodejs", "node_modules")
	legacyLive := filepath.Join(t.TempDir(), "data-old", "deps", "nodejs", "node_modules")
	elsewhere := filepath.Join(t.TempDir(), "my-project", "node_modules")
	writeStartupScriptFile(t, filepath.Join(managed, "axios", "index.js"), "module.exports = {}\n")
	writeStartupScriptFile(t, filepath.Join(legacyLive, "axios", "index.js"), "module.exports = {}\n")
	writeStartupScriptFile(t, filepath.Join(elsewhere, "lodash", "index.js"), "module.exports = {}\n")
	vendoredPkg := filepath.Join(scriptsDir, "vendored", "node_modules", "pkg")
	writeStartupScriptFile(t, filepath.Join(vendoredPkg, "index.js"), "module.exports = {}\n")

	links := map[string]string{
		filepath.Join(scriptsDir, "node_modules"):                managed, // 根目录脚本的工作目录就是脚本根目录
		filepath.Join(scriptsDir, "jobs", "node_modules"):        managed,
		filepath.Join(scriptsDir, "repo", "sub", "node_modules"): managed,
		filepath.Join(scriptsDir, "legacy", "node_modules"):      legacyLive,
		filepath.Join(scriptsDir, "other", "node_modules"):       elsewhere,
	}
	for link, target := range links {
		mustCreateNodeModulesLink(t, target, link)
	}
	// 面板建软链只用绝对路径；相对路径的软链是用户自己建的（junction 没有相对路径，只在类 Unix 上验）。
	relativeLink := filepath.Join(scriptsDir, "rel", "node_modules")
	if runtime.GOOS != "windows" {
		writeStartupScriptFile(t, filepath.Join(scriptsDir, "rel", "run.js"), "require('axios')\n")
		if err := os.Symlink(filepath.Join("..", "..", "deps", "nodejs", "node_modules"), relativeLink); err != nil {
			t.Fatalf("create relative symlink: %v", err)
		}
	}

	QuarantineUnexpectedScriptEntriesOnStartup()

	for link, target := range links {
		if got, err := os.Readlink(link); err != nil || filepath.Clean(got) != filepath.Clean(target) {
			t.Fatalf("不悬空的软链 %s 应原样保留，Readlink=%q err=%v", link, got, err)
		}
	}
	for _, kept := range []string{
		filepath.Join(managed, "axios", "index.js"),
		filepath.Join(legacyLive, "axios", "index.js"),
		filepath.Join(elsewhere, "lodash", "index.js"),
		filepath.Join(vendoredPkg, "index.js"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("软链指向的目录、真 node_modules 目录都不能受影响，stat err=%v", err)
		}
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Lstat(relativeLink); err != nil {
			t.Fatalf("相对路径的软链是用户自己建的，应保留，Lstat err=%v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, "quarantine")); !os.IsNotExist(err) {
		t.Fatalf("清软链不应往 quarantine 里搬任何东西，stat err=%v", err)
	}
}

// 悬空的面板式软链启动时清掉：指向当前托管依赖目录（依赖全卸光、数据目录被清过）的，与指向旧数据目录
// （…/deps/nodejs/node_modules，数据目录搬家前建的）的都算。ensureManagedNodeModulesAccess 看到同名项就不再建，
// ESM 的 import 又没有 NODE_PATH 兜底，悬空软链留着会让脚本一直报找不到模块。
// 指向别处的悬空软链、相对路径的悬空软链不归面板管，照旧不动；真 node_modules 目录整棵跳过，里面的不碰。
func TestQuarantineRemovesDanglingPanelNodeModulesLink(t *testing.T) {
	dataDir, scriptsDir := useStartupScriptDirs(t)

	managed := filepath.Join(dataDir, "deps", "nodejs", "node_modules")
	oldDataDir := filepath.Join(t.TempDir(), "data-old")
	legacy := filepath.Join(oldDataDir, "deps", "nodejs", "node_modules")
	elsewhere := filepath.Join(t.TempDir(), "gone", "node_modules")
	for _, dir := range []string{managed, legacy, elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	panelLinks := []string{
		filepath.Join(scriptsDir, "node_modules"),
		filepath.Join(scriptsDir, "jobs", "node_modules"),
	}
	for _, link := range panelLinks {
		mustCreateNodeModulesLink(t, managed, link)
	}
	legacyLink := filepath.Join(scriptsDir, "legacy", "node_modules")
	mustCreateNodeModulesLink(t, legacy, legacyLink)
	userLink := filepath.Join(scriptsDir, "other", "node_modules")
	mustCreateNodeModulesLink(t, elsewhere, userLink)
	// 真 node_modules 里面的东西不碰：任务不会在真 node_modules 里跑，整棵跳过（同样悬空、同样面板式也不进去）。
	vendoredPkg := filepath.Join(scriptsDir, "vendored", "node_modules", "pkg")
	writeStartupScriptFile(t, filepath.Join(vendoredPkg, "index.js"), "module.exports = {}\n")
	insideRealNodeModules := filepath.Join(vendoredPkg, "node_modules")
	mustCreateNodeModulesLink(t, managed, insideRealNodeModules)
	// 相对路径、目标同样悬空的软链（junction 没有相对路径，只在类 Unix 上验）。
	relativeLink := filepath.Join(scriptsDir, "rel", "node_modules")
	if runtime.GOOS != "windows" {
		if err := os.MkdirAll(filepath.Dir(relativeLink), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(relativeLink), err)
		}
		if err := os.Symlink(filepath.Join("..", "..", "deps", "nodejs", "node_modules"), relativeLink); err != nil {
			t.Fatalf("create relative symlink: %v", err)
		}
	}
	for _, dir := range []string{managed, oldDataDir, elsewhere} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatalf("remove %s: %v", dir, err)
		}
	}

	QuarantineUnexpectedScriptEntriesOnStartup()

	for _, link := range append(panelLinks, legacyLink) {
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatalf("悬空的面板软链 %s 应被清掉，Lstat err=%v", link, err)
		}
	}
	if _, err := os.Lstat(userLink); err != nil {
		t.Fatalf("指向别处的悬空软链不归面板管，应保留，Lstat err=%v", err)
	}
	if _, err := os.Lstat(insideRealNodeModules); err != nil {
		t.Fatalf("真 node_modules 里面的东西不应被碰，Lstat err=%v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Lstat(relativeLink); err != nil {
			t.Fatalf("相对路径的悬空软链是用户自己建的，应保留，Lstat err=%v", err)
		}
	}
}

// 脚本目录本身是软链 / junction（脚本放到别的盘、NAS 上）时照样清理：原来 WalkDir 只看根这一项、不进链接里面，整棵树一个都清不到。
// 根目录链接本身、里面不悬空的面板软链都不动。Windows 上根目录是 junction（EvalSymlinks 不解析它），其它平台是软链。
func TestQuarantineCleansDanglingLinksWhenScriptsDirIsLink(t *testing.T) {
	oldConfig := config.C
	t.Cleanup(func() { config.C = oldConfig })

	dataDir := t.TempDir()
	realScripts := filepath.Join(t.TempDir(), "real-scripts")
	scriptsDir := filepath.Join(dataDir, "scripts")
	managed := filepath.Join(dataDir, "deps", "nodejs", "node_modules")
	oldDataDir := filepath.Join(t.TempDir(), "data-old")
	legacy := filepath.Join(oldDataDir, "deps", "nodejs", "node_modules")
	writeStartupScriptFile(t, filepath.Join(realScripts, "jobs", "run.js"), "require('axios')\n")
	writeStartupScriptFile(t, filepath.Join(managed, "axios", "index.js"), "module.exports = {}\n")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", legacy, err)
	}
	// 借用建 node_modules 软链的同一个辅助函数把脚本目录建成链接（Windows 上是 junction）。
	mustCreateNodeModulesLink(t, realScripts, scriptsDir)
	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir

	danglingLinks := []string{
		filepath.Join(realScripts, "node_modules"),
		filepath.Join(realScripts, "sub", "node_modules"),
	}
	for _, link := range danglingLinks {
		mustCreateNodeModulesLink(t, legacy, link)
	}
	liveLink := filepath.Join(realScripts, "jobs", "node_modules")
	mustCreateNodeModulesLink(t, managed, liveLink)
	if err := os.RemoveAll(oldDataDir); err != nil {
		t.Fatalf("remove %s: %v", oldDataDir, err)
	}

	QuarantineUnexpectedScriptEntriesOnStartup()

	for _, link := range danglingLinks {
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatalf("脚本目录是链接时，里面悬空的面板软链 %s 也应被清掉，Lstat err=%v", link, err)
		}
	}
	if target, err := os.Readlink(liveLink); err != nil || filepath.Clean(target) != filepath.Clean(managed) {
		t.Fatalf("不悬空的面板软链应保留，Readlink=%q err=%v", target, err)
	}
	if _, err := os.Stat(filepath.Join(scriptsDir, "jobs", "run.js")); err != nil {
		t.Fatalf("脚本目录链接本身与里面的脚本都不能受影响，stat err=%v", err)
	}
}

// CleanupManagedHelperCopiesUnderRoot 遍历时整棵跳过 node_modules、__pycache__、.git：任务不会在这些目录里跑，
// 面板也不会往里放通知脚本副本；订阅仓库的 .git、真实 node_modules 动辄上千个目录，面板每次启动都要在后台白扫一遍
// （#156；v3.3.6 起 ddp 不再整树扫、启动时改在后台跑，#158）。
func TestCleanupManagedHelperCopiesUnderRootSkipsHiddenDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scripts")
	managedCopy := "// " + managedNotifyHelperToken + "\n"

	workDirCopy := filepath.Join(root, "repo", sendNotifyJSFilename)
	writeStartupScriptFile(t, workDirCopy, managedCopy)
	skipped := []string{
		filepath.Join(root, "repo", "node_modules", "pkg", sendNotifyJSFilename),
		filepath.Join(root, "repo", ".git", "hooks", notifyPyFilename),
		filepath.Join(root, "repo", "__pycache__", notifyPyFilename),
	}
	for _, path := range skipped {
		writeStartupScriptFile(t, path, managedCopy)
	}

	if err := CleanupManagedHelperCopiesUnderRoot(root); err != nil {
		t.Fatalf("cleanup under root: %v", err)
	}

	if _, err := os.Stat(workDirCopy); !os.IsNotExist(err) {
		t.Fatalf("普通子目录里的托管副本仍要清掉，stat err=%v", err)
	}
	for _, path := range skipped {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s 所在目录应整棵跳过、不被遍历，stat err=%v", path, err)
		}
	}
}
