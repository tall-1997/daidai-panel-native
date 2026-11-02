package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// setupQingLongCompatConfig 只准备 config.C 里那三个目录，不碰数据库 ——
// 兼容层本身与库无关，用 testutil.SetupTestEnv 反而会拖一整套 DB 初始化。
func setupQingLongCompatConfig(t *testing.T) (scriptsDir, logDir, dataDir string) {
	t.Helper()

	oldConfig := config.C
	t.Cleanup(func() {
		config.C = oldConfig
	})

	dataDir = t.TempDir()
	scriptsDir = filepath.Join(dataDir, "scripts")
	logDir = filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}

	config.C = &config.Config{}
	config.C.Data.Dir = dataDir
	config.C.Data.ScriptsDir = scriptsDir
	config.C.Data.LogDir = logDir
	return scriptsDir, logDir, dataDir
}

// requireQingLongSymlinkSupport 在建不出软链的环境（Windows 未开发者模式 / 无 SeCreateSymbolicLink）
// 直接跳过。这类用例在 Linux 容器与 CI 上才是真正生效的那一份。
func requireQingLongSymlinkSupport(t *testing.T) {
	t.Helper()

	probeDir := t.TempDir()
	if err := os.Symlink(probeDir, filepath.Join(probeDir, "probe-link")); err != nil {
		t.Skipf("当前环境不支持创建软链，跳过: %v", err)
	}
}

func readQingLongSymlink(t *testing.T, path string) string {
	t.Helper()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s 应该是软链，实际不是", path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("readlink %s: %v", path, err)
	}
	return filepath.Clean(target)
}

// 完整映射表必须一次建齐：少一条就是某类青龙脚本静默找不到目录。
func TestEnsureQingLongCompatLayoutCreatesLayout(t *testing.T) {
	requireQingLongSymlinkSupport(t)
	scriptsDir, logDir, dataDir := setupQingLongCompatConfig(t)

	root := filepath.Join(t.TempDir(), "ql")
	ensureQingLongCompatLayoutAt(root)

	for _, dir := range []string{root, filepath.Join(root, "shell"), filepath.Join(root, "data")} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s 应该是真实目录，err=%v", dir, err)
		}
	}

	wants := map[string]string{
		filepath.Join(root, "data", "repo"):    scriptsDir,
		filepath.Join(root, "data", "scripts"): scriptsDir,
		filepath.Join(root, "scripts"):         scriptsDir,
		filepath.Join(root, "data", "log"):     logDir,
		filepath.Join(root, "log"):             logDir,
		filepath.Join(root, "data", "config"):  dataDir,
		filepath.Join(root, "config"):          dataDir,
		filepath.Join(root, "data", "deps"):    filepath.Join(dataDir, "deps"),
	}
	for link, want := range wants {
		if got := readQingLongSymlink(t, link); got != filepath.Clean(want) {
			t.Fatalf("%s 应该指向 %q，实际指向 %q", link, want, got)
		}
	}

	// deps 是懒创建目录，兼容层要顺手把它建出来，否则留下的是一条悬空软链，
	// 脚本再对它 mkdir -p 会直接报 File exists。
	if info, err := os.Stat(filepath.Join(dataDir, "deps")); err != nil || !info.IsDir() {
		t.Fatalf("deps 目标目录应被一并创建，err=%v", err)
	}

	// env.sh 是青龙脚本第一处报错点（touch "$dir_shell/env.sh"），必须是存在的空文件。
	content, err := os.ReadFile(filepath.Join(root, "shell", "env.sh"))
	if err != nil {
		t.Fatalf("read env.sh: %v", err)
	}
	if len(content) != 0 {
		t.Fatalf("env.sh 应该是空占位文件（它会被脚本 source），实际内容 %q", string(content))
	}

	// 重复执行必须幂等：Magisk/容器重建后每次启动都会再跑一遍。
	ensureQingLongCompatLayoutAt(root)
	if got := readQingLongSymlink(t, filepath.Join(root, "data", "repo")); got != filepath.Clean(scriptsDir) {
		t.Fatalf("重复执行后 repo 软链被改坏，指向 %q", got)
	}
}

// 指错的软链必须重建。用户改过数据目录、或上一版留下的链接如果原样保留，
// 青龙脚本会顺着旧路径找到一个空目录，报错信息完全看不出根因。
func TestEnsureQingLongCompatLayoutRebuildsWrongSymlink(t *testing.T) {
	requireQingLongSymlinkSupport(t)
	scriptsDir, _, _ := setupQingLongCompatConfig(t)

	root := filepath.Join(t.TempDir(), "ql")
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatalf("mkdir root/data: %v", err)
	}
	stalePath := filepath.Join(root, "data", "repo")
	staleTarget := t.TempDir()
	if err := os.Symlink(staleTarget, stalePath); err != nil {
		t.Fatalf("create stale symlink: %v", err)
	}

	ensureQingLongCompatLayoutAt(root)

	if got := readQingLongSymlink(t, stalePath); got != filepath.Clean(scriptsDir) {
		t.Fatalf("指错的软链应被重建到 %q，实际指向 %q", scriptsDir, got)
	}
}

// 已经是真实目录（用户自己建的、或这台机器上真装了青龙）时必须原样保留：
// 删掉别人的目录换成软链是不可接受的破坏。
func TestEnsureQingLongCompatLayoutKeepsRealDirectory(t *testing.T) {
	setupQingLongCompatConfig(t)

	root := filepath.Join(t.TempDir(), "ql")
	realRepo := filepath.Join(root, "data", "repo")
	if err := os.MkdirAll(realRepo, 0o755); err != nil {
		t.Fatalf("mkdir real repo dir: %v", err)
	}
	marker := filepath.Join(realRepo, "user-file.txt")
	if err := os.WriteFile(marker, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	ensureQingLongCompatLayoutAt(root)

	info, err := os.Lstat(realRepo)
	if err != nil {
		t.Fatalf("lstat real repo dir: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("已存在的真实目录不应被替换成软链")
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "keep me" {
		t.Fatalf("真实目录里的文件必须原样保留，err=%v content=%q", err, string(content))
	}
}

// 根目录建不出来（只读根、EACCES、位置被文件占了）时只能记一行日志后收工，
// 绝不能 panic、更不能把错误往上抛阻塞启动 —— 把「青龙脚本跑不了」放大成
// 「面板起不来」是不可接受的。
func TestEnsureQingLongCompatLayoutSurvivesUnusableRoot(t *testing.T) {
	setupQingLongCompatConfig(t)

	// 用一个普通文件占住根路径，MkdirAll 必然失败（Linux ENOTDIR / Windows 同理）。
	root := filepath.Join(t.TempDir(), "ql")
	if err := os.WriteFile(root, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	ensureQingLongCompatLayoutAt(root)

	content, err := os.ReadFile(root)
	if err != nil || string(content) != "not a dir" {
		t.Fatalf("占位文件必须原样保留，err=%v content=%q", err, string(content))
	}
}

// config.C 还没初始化时（极端启动顺序）也不能炸。
func TestEnsureQingLongCompatLayoutSurvivesNilConfig(t *testing.T) {
	oldConfig := config.C
	defer func() {
		config.C = oldConfig
	}()

	config.C = nil
	ensureQingLongCompatLayoutAt(filepath.Join(t.TempDir(), "ql"))
}

// 非 Linux 平台必须直接跳过：Windows 上 "/ql" 会落到当前盘符的根目录，
// 真建出来纯属污染用户机器。
func TestEnsureQingLongCompatLayoutSkipsNonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Linux 上 EnsureQingLongCompatLayout 会真的去建 /ql，这条只验证非 Linux 平台的短路")
	}
	if _, err := os.Stat(qingLongCompatRoot); err == nil {
		t.Skipf("本机已存在 %s，无法据此判断是否为本次创建", qingLongCompatRoot)
	}

	setupQingLongCompatConfig(t)
	EnsureQingLongCompatLayout()

	if _, err := os.Stat(qingLongCompatRoot); err == nil {
		t.Fatalf("非 Linux 平台不应创建 %s", qingLongCompatRoot)
	}

	// 同一条守卫也管着 resolveQingLongDir：非 Linux 一律回落数据目录。
	if got := resolveQingLongDir(); got != config.C.Data.Dir {
		t.Fatalf("非 Linux 平台 QL_DIR 应回落数据目录 %q，实际 %q", config.C.Data.Dir, got)
	}
}

// QL_* 路径刻意直接指向面板真实目录，而不是拼成 <QL_DIR>/data/repo：
// 有兼容层时两者是同一个目录，没兼容层时前者仍然真实存在、后者是死路径。
func TestBuildQingLongCompatEnvPointsAtRealDirs(t *testing.T) {
	scriptsDir, logDir, dataDir := setupQingLongCompatConfig(t)

	env := buildQingLongCompatEnv()
	wants := map[string]string{
		"QL_BRANCH":      qingLongCompatBranch,
		"QL_REPO_PATH":   scriptsDir,
		"QL_SCRIPT_PATH": scriptsDir,
		"QL_LOG_PATH":    logDir,
		"QL_CONFIG_PATH": dataDir,
	}
	for key, want := range wants {
		if got := env[key]; got != want {
			t.Fatalf("%s 应为 %q，实际 %q", key, want, got)
		}
	}
	if env["QL_DIR"] == "" {
		t.Fatal("QL_DIR 不应为空")
	}
}

// dir_repo / dir_scripts / dir_raw 这三个小写变量必须指向脚本目录这个**真实路径**，
// 不能顺手改成拼 `<QL_DIR>/data/repo`（那是 /ql 下的一条软链）。
//
// 理由写在 buildQingLongCompatEnv 的注释里：GNU find 默认是 -P，命令行参数本身是软链时
// 不会下钻，于是青龙脚本定位自己仓库目录的标准写法
// `find "$dir_repo" -type d -name "<owner>_<repo>"` 会一条都搜不到 —— 这正是 #110 的第二处报错。
// 软链能兜住 cd / ls / test -d / source，唯独兜不住 find，所以这三个不能跟 QL_DIR 统一。
func TestBuildQingLongCompatEnvInjectsLowercaseDirVars(t *testing.T) {
	scriptsDir, _, dataDir := setupQingLongCompatConfig(t)

	env := buildQingLongCompatEnv()
	for _, key := range []string{"dir_repo", "dir_scripts", "dir_raw"} {
		got, ok := env[key]
		if !ok {
			t.Fatalf("%s 必须被注入：缺了它脚本会回落到 $QL_DIR/data/repo 这条软链，find 搜不出东西", key)
		}
		if got != scriptsDir {
			t.Fatalf("%s 应为脚本目录的真实路径 %q，实际 %q", key, scriptsDir, got)
		}
	}

	// QL_DIR 与它们是刻意分开的两档逻辑，不能被「顺手统一」掉：
	// 脚本里的 $QL_DIR/shell、$QL_DIR/data/repo 这些拼法只在 /ql 下成立，
	// 所以兼容层建成时 QL_DIR 必须是 /ql，只有建不成（Windows 单机版、只读根）才回落数据目录。
	wantQLDir := dataDir
	if runtime.GOOS == "linux" {
		if info, err := os.Stat(qingLongCompatRoot); err == nil && info.IsDir() {
			wantQLDir = qingLongCompatRoot
		}
	}
	if got := env["QL_DIR"]; got != wantQLDir {
		t.Fatalf("QL_DIR 应为 %q，实际 %q", wantQLDir, got)
	}
}

// 「只在 key 不存在时才写」是这批变量与 DAIDAI_* / TZ 保留名最大的差别：
// 用户在环境变量页手建的 QL_DIR 必须原样生效，被静默覆盖的话他在页面上看到自己设的值、
// 实际却不生效，排查毫无线索。
func TestManagedRuntimeEnvKeepsUserDefinedQingLongVars(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	if err := database.DB.Create(&model.EnvVar{
		Name:    "QL_DIR",
		Value:   "/my/own/ql",
		Enabled: true,
	}).Error; err != nil {
		t.Fatalf("create env var: %v", err)
	}

	envMap, err := BuildManagedRuntimeEnvMapForPythonVersion(root, root, nil, time.Hour, "3.10")
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if got := envMap["QL_DIR"]; got != "/my/own/ql" {
		t.Fatalf("用户自建的 QL_DIR 必须优先，实际 %q", got)
	}
	// 用户没设的那几个仍然要补上默认值。
	if got := envMap["QL_REPO_PATH"]; got != config.C.Data.ScriptsDir {
		t.Fatalf("QL_REPO_PATH 应补成脚本目录 %q，实际 %q", config.C.Data.ScriptsDir, got)
	}
	if got := envMap["QL_BRANCH"]; got != qingLongCompatBranch {
		t.Fatalf("QL_BRANCH 应补成 %q，实际 %q", qingLongCompatBranch, got)
	}
}
