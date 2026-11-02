package service

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// reconcileStubs 记录启动校验里几个包级替身被怎么调用过。
type reconcileStubs struct {
	snapshotCalls  map[string]int // 每个 Python 版本列举了几次
	installedCalls []string       // dependencyInstalledFunc 被问过的「类型/名称/版本」
	scheduled      []string       // 第一轮排进重启重装的依赖名
	resumed        []string       // stale 轮续装的依赖名
}

// stubReconcileFuncs 把启动校验用到的四个包级替身都换成内存实现，用例结束时还原。
// snapshots 按 Python 版本给出快照（缺省 = 列举失败）；installed 决定逐条判定的结果。
func stubReconcileFuncs(t *testing.T, snapshots map[string]map[string]bool, installed func(depType, name, pythonVersion string) bool) *reconcileStubs {
	t.Helper()

	originalInstalled := dependencyInstalledFunc
	originalPythonPackages := pythonInstalledPackagesFunc
	originalReinstallBatch := dependencyReinstallBatchFunc
	originalRestartReinstallBatch := dependencyRestartReinstallBatchFunc
	t.Cleanup(func() {
		dependencyInstalledFunc = originalInstalled
		pythonInstalledPackagesFunc = originalPythonPackages
		dependencyReinstallBatchFunc = originalReinstallBatch
		dependencyRestartReinstallBatchFunc = originalRestartReinstallBatch
	})

	stubs := &reconcileStubs{snapshotCalls: map[string]int{}}
	pythonInstalledPackagesFunc = func(pythonVersion string) (map[string]bool, error) {
		stubs.snapshotCalls[pythonVersion]++
		if snapshot, ok := snapshots[pythonVersion]; ok {
			return snapshot, nil
		}
		return nil, errors.New("当前镜像不支持 Python " + pythonVersion)
	}
	dependencyInstalledFunc = func(depType, name, pythonVersion string) bool {
		stubs.installedCalls = append(stubs.installedCalls, depType+"/"+name+"/"+pythonVersion)
		return installed(depType, name, pythonVersion)
	}
	dependencyRestartReinstallBatchFunc = func(deps []model.Dependency) {
		for _, dep := range deps {
			stubs.scheduled = append(stubs.scheduled, dep.Name)
		}
	}
	dependencyReinstallBatchFunc = func(deps []model.Dependency) {
		for _, dep := range deps {
			stubs.resumed = append(stubs.resumed, dep.Name)
		}
	}
	return stubs
}

func mustCreateReconcileDependency(t *testing.T, depType, name, pythonVersion, status string) model.Dependency {
	t.Helper()
	dep := model.Dependency{Type: depType, Name: name, PythonVersion: pythonVersion, Status: status}
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("create dependency %s: %v", name, err)
	}
	return dep
}

func reloadReconcileDependency(t *testing.T, id uint) model.Dependency {
	t.Helper()
	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		t.Fatalf("reload dependency %d: %v", id, err)
	}
	return dep
}

// 契约 S1：Python 依赖按版本懒加载一次已装包快照，第一轮与 stale 轮共用；
// 带版本号 / extras / @ url 的名字按基础包名判定（主包在就算已装），Node.js / Linux 照旧逐条判定。
func TestReconcileDependenciesAfterRestartJudgesPythonDepsBySnapshot(t *testing.T) {
	testutil.SetupTestEnv(t)

	pinned := mustCreateReconcileDependency(t, model.DepTypePython, "requests==2.31.0", "3.12", model.DepStatusInstalled)
	// python_version 为空的老记录按 3.12 算，与 3.12 共用同一份快照。
	extras := mustCreateReconcileDependency(t, model.DepTypePython, "httpx[http2]", "", model.DepStatusInstalled)
	ranged := mustCreateReconcileDependency(t, model.DepTypePython, "aiohttp>=3.9", "3.12", model.DepStatusInstalled)
	direct := mustCreateReconcileDependency(t, model.DepTypePython, "mypkg @ https://example.com/mypkg-1.0-py3-none-any.whl", "3.12", model.DepStatusInstalled)
	missing := mustCreateReconcileDependency(t, model.DepTypePython, "Notexist-Pkg", "3.12", model.DepStatusInstalled)
	otherVersion := mustCreateReconcileDependency(t, model.DepTypePython, "Requests", "3.11", model.DepStatusInstalled)
	linux := mustCreateReconcileDependency(t, model.DepTypeLinux, "curl", "", model.DepStatusInstalled)
	node := mustCreateReconcileDependency(t, model.DepTypeNodeJS, "left-pad", "", model.DepStatusInstalled)
	// stale 轮：排队中的记录名字带版本号，不查快照、照旧逐条精确判定（见 TestReconcileDependenciesAfterRestartJudgesStaleSpecifiersExactly）。
	queued := mustCreateReconcileDependency(t, model.DepTypePython, "PyYAML==6.0", "3.12", model.DepStatusQueued)

	stubs := stubReconcileFuncs(t, map[string]map[string]bool{
		"3.12": {"requests": true, "httpx": true, "aiohttp": true, "mypkg": true, "pyyaml": true},
		"3.11": {"requests": true},
	}, func(depType, name, pythonVersion string) bool {
		if depType == model.DepTypePython && name != "PyYAML==6.0" {
			t.Errorf("快照可用时第一轮不应再逐条判定 Python 依赖，实际问了 %s", name)
		}
		// 逐条 pip show 的真实口径：认不出带版本号的写法，判为未装。
		return depType == model.DepTypeLinux && name == "curl"
	})

	ReconcileDependenciesAfterRestart()

	if stubs.snapshotCalls["3.12"] != 1 || stubs.snapshotCalls["3.11"] != 1 || len(stubs.snapshotCalls) != 2 {
		t.Fatalf("每个 Python 版本整轮只应列举一次（两轮共用），实际 %v", stubs.snapshotCalls)
	}
	wantInstalledCalls := []string{"linux/curl/", "nodejs/left-pad/", "python/PyYAML==6.0/3.12"}
	sort.Strings(stubs.installedCalls)
	if strings.Join(stubs.installedCalls, ",") != strings.Join(wantInstalledCalls, ",") {
		t.Fatalf("只有 Node.js / Linux 依赖与 stale 轮带版本号的那条应逐条判定，实际 %v", stubs.installedCalls)
	}
	sort.Strings(stubs.scheduled)
	if strings.Join(stubs.scheduled, ",") != "Notexist-Pkg,left-pad" {
		t.Fatalf("只有真缺的依赖应排进重启重装，实际 %v", stubs.scheduled)
	}
	if len(stubs.resumed) != 0 {
		t.Fatalf("没有恢复备份的续装记录，不应触发续装，实际 %v", stubs.resumed)
	}

	for _, dep := range []model.Dependency{pinned, extras, ranged, direct, otherVersion, linux} {
		if got := reloadReconcileDependency(t, dep.ID); got.Status != model.DepStatusInstalled {
			t.Fatalf("%s 主包在就应保持已安装，实际 %q：%q", dep.Name, got.Status, got.Log)
		}
	}
	for _, dep := range []model.Dependency{missing, node} {
		if got := reloadReconcileDependency(t, dep.ID); got.Status != model.DepStatusInstalling {
			t.Fatalf("%s 缺失时应转成安装中等待重装，实际 %q", dep.Name, got.Status)
		}
	}
	// 主包 pyyaml 在快照里，但排队中的 PyYAML==6.0 一步都没执行过：照改快照之前的口径置失败，不能记成已安装。
	if got := reloadReconcileDependency(t, queued.ID); got.Status != model.DepStatusFailed || !strings.Contains(got.Log, "排队中的任务因服务重启而中断") {
		t.Fatalf("排队中的 PyYAML==6.0 应逐条判定后置为失败，实际 %q：%q", got.Status, got.Log)
	}
}

// stale 轮（安装中 / 排队中 / 卸载中）里名字带版本约束 / extras / url / marker 的 Python 记录不查快照，照旧逐条精确判定，
// 与改快照之前一致：带 [恢复备份] 的安装中续装、排队中置失败、其余的安装中与卸载中置失败。
// 快照里这些记录的主包全都在：只按主包判的话它们会一律被记成已安装，恢复备份钉住的版本永远装不上。
// 裸包名的 stale 记录照旧查快照；第一轮（已安装）带版本号的照旧按主包判，不逐条问。
func TestReconcileDependenciesAfterRestartJudgesStaleSpecifiersExactly(t *testing.T) {
	testutil.SetupTestEnv(t)

	restoreLog := "[恢复备份] 已提交依赖重装"
	mustCreateWithLog := func(name, status, logText string) model.Dependency {
		t.Helper()
		dep := model.Dependency{Type: model.DepTypePython, Name: name, PythonVersion: "3.12", Status: status, Log: logText}
		if err := database.DB.Create(&dep).Error; err != nil {
			t.Fatalf("create dependency %s: %v", name, err)
		}
		return dep
	}
	restoringPinned := mustCreateWithLog("requests==2.31.0", model.DepStatusInstalling, restoreLog)
	restoringExtras := mustCreateWithLog("httpx[http2]", model.DepStatusInstalling, restoreLog)
	queuedPinned := mustCreateWithLog("PyYAML==6.0", model.DepStatusQueued, "")
	installingPinned := mustCreateWithLog("aiohttp>=3.9", model.DepStatusInstalling, "[安装] 普通安装中途重启")
	removingPinned := mustCreateWithLog("urllib3<2", model.DepStatusRemoving, "")
	queuedBare := mustCreateWithLog("certifi", model.DepStatusQueued, "")
	installedPinned := mustCreateWithLog("idna==3.7", model.DepStatusInstalled, "")

	stubs := stubReconcileFuncs(t, map[string]map[string]bool{
		"3.12": {"requests": true, "httpx": true, "pyyaml": true, "aiohttp": true, "urllib3": true, "certifi": true, "idna": true},
	}, func(depType, name, pythonVersion string) bool {
		// 逐条 pip show 的真实口径：认不出带版本号 / extras 的写法，一律判未装。
		return false
	})

	ReconcileDependenciesAfterRestart()

	if stubs.snapshotCalls["3.12"] != 1 {
		t.Fatalf("3.12 整轮只应列举一次，实际 %v", stubs.snapshotCalls)
	}
	sort.Strings(stubs.installedCalls)
	wantInstalledCalls := []string{
		"python/PyYAML==6.0/3.12",
		"python/aiohttp>=3.9/3.12",
		"python/httpx[http2]/3.12",
		"python/requests==2.31.0/3.12",
		"python/urllib3<2/3.12",
	}
	if strings.Join(stubs.installedCalls, ",") != strings.Join(wantInstalledCalls, ",") {
		t.Fatalf("只有 stale 轮带版本约束 / extras 的记录应逐条判定（裸包名与第一轮都查快照），实际 %v", stubs.installedCalls)
	}
	sort.Strings(stubs.resumed)
	if strings.Join(stubs.resumed, ",") != "httpx[http2],requests==2.31.0" {
		t.Fatalf("带 [恢复备份] 的安装中记录应续装，实际续装 %v", stubs.resumed)
	}
	if len(stubs.scheduled) != 0 {
		t.Fatalf("第一轮没有缺失的依赖，不应排进重启重装，实际 %v", stubs.scheduled)
	}

	for _, dep := range []model.Dependency{restoringPinned, restoringExtras} {
		if got := reloadReconcileDependency(t, dep.ID); got.Status != model.DepStatusInstalling || !strings.Contains(got.Log, "检测到恢复任务未完成，已在重启后继续安装") {
			t.Fatalf("%s 恢复备份没装完，应保持安装中并续装，实际 %q：%q", dep.Name, got.Status, got.Log)
		}
	}
	if got := reloadReconcileDependency(t, queuedPinned.ID); got.Status != model.DepStatusFailed || !strings.Contains(got.Log, "排队中的任务因服务重启而中断") {
		t.Fatalf("排队中的 %s 应置为失败，实际 %q：%q", queuedPinned.Name, got.Status, got.Log)
	}
	for _, dep := range []model.Dependency{installingPinned, removingPinned} {
		if got := reloadReconcileDependency(t, dep.ID); got.Status != model.DepStatusFailed || !strings.Contains(got.Log, "操作因服务重启而中断") {
			t.Fatalf("%s（%s）被重启打断，应置为失败，实际 %q：%q", dep.Name, dep.Status, got.Status, got.Log)
		}
	}
	if got := reloadReconcileDependency(t, queuedBare.ID); got.Status != model.DepStatusInstalled || !strings.Contains(got.Log, "已同步状态为已安装") {
		t.Fatalf("裸包名 certifi 照旧查快照，主包在就同步成已安装，实际 %q：%q", got.Status, got.Log)
	}
	if got := reloadReconcileDependency(t, installedPinned.ID); got.Status != model.DepStatusInstalled {
		t.Fatalf("第一轮的 idna==3.7 照旧按主包判，应保持已安装，实际 %q：%q", got.Status, got.Log)
	}
}

// 列举失败（版本不支持、pip 坏了、超时、输出解析失败）时只有这个版本退回逐条判定，并且每个版本只打一行原因。
func TestReconcileDependenciesAfterRestartFallsBackPerVersionWhenSnapshotFails(t *testing.T) {
	testutil.SetupTestEnv(t)
	logs := captureStandardLog(t)

	mustCreateReconcileDependency(t, model.DepTypePython, "requests", "3.11", model.DepStatusInstalled)
	pinned311 := mustCreateReconcileDependency(t, model.DepTypePython, "flask==3.0.0", "3.11", model.DepStatusInstalled)
	ok312 := mustCreateReconcileDependency(t, model.DepTypePython, "requests", "3.12", model.DepStatusInstalled)

	stubs := stubReconcileFuncs(t, map[string]map[string]bool{
		"3.12": {"requests": true},
	}, func(depType, name, pythonVersion string) bool {
		// 老路径（逐条 pip show）认不出带版本号的写法，这里照实模拟：只有 requests 判已装。
		return name == "requests"
	})

	ReconcileDependenciesAfterRestart()

	if stubs.snapshotCalls["3.11"] != 1 || stubs.snapshotCalls["3.12"] != 1 {
		t.Fatalf("列举失败也只试一次，不应每条依赖重试，实际 %v", stubs.snapshotCalls)
	}
	sort.Strings(stubs.installedCalls)
	if strings.Join(stubs.installedCalls, ",") != "python/flask==3.0.0/3.11,python/requests/3.11" {
		t.Fatalf("只有列举失败的 3.11 应逐条判定，实际 %v", stubs.installedCalls)
	}
	if strings.Join(stubs.scheduled, ",") != "flask==3.0.0" {
		t.Fatalf("退回逐条判定后语义与原来一致，实际排进重装的是 %v", stubs.scheduled)
	}
	if got := reloadReconcileDependency(t, pinned311.ID); got.Status != model.DepStatusInstalling {
		t.Fatalf("flask==3.0.0 走老路径判缺，应转成安装中，实际 %q", got.Status)
	}
	if got := reloadReconcileDependency(t, ok312.ID); got.Status != model.DepStatusInstalled {
		t.Fatalf("3.12 的快照可用，requests 应保持已安装，实际 %q", got.Status)
	}

	output := logs.String()
	if count := strings.Count(output, "[启动校验] 列举 Python 3.11 已安装的包失败"); count != 1 {
		t.Fatalf("列举失败的版本应恰好打一行原因，实际 %d 行：\n%s", count, output)
	}
	if !strings.Contains(output, "当前镜像不支持 Python 3.11") {
		t.Fatalf("回退日志里应写明失败原因，实际：\n%s", output)
	}
	if strings.Contains(output, "列举 Python 3.12") {
		t.Fatalf("列举成功的版本不应打回退日志，实际：\n%s", output)
	}
}

// 没有 Python 依赖时一次都不列举：懒加载，免得只有 Node / Linux 依赖的面板白跑一遍 pip。
func TestReconcileDependenciesAfterRestartSkipsSnapshotWithoutPythonDeps(t *testing.T) {
	testutil.SetupTestEnv(t)

	mustCreateReconcileDependency(t, model.DepTypeLinux, "curl", "", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypeNodeJS, "left-pad", "", model.DepStatusInstalling)

	stubs := stubReconcileFuncs(t, nil, func(depType, name, pythonVersion string) bool { return true })

	ReconcileDependenciesAfterRestart()

	if len(stubs.snapshotCalls) != 0 {
		t.Fatalf("没有 Python 依赖时不应列举 Python 包，实际 %v", stubs.snapshotCalls)
	}
	if len(stubs.installedCalls) != 2 {
		t.Fatalf("Node.js / Linux 依赖应照旧逐条判定，实际 %v", stubs.installedCalls)
	}
}

// pip list 写死不列出 python / wsgiref / argparse（pip 的 stdlib_pkgs），pip show 却查得到：
// 快照里没有这几个名字时不能直接判缺，要按老办法逐条问一次；快照里有的名字照旧不起子进程。
func TestReconcileDependenciesAfterRestartAsksPipShowForNamesPipListHides(t *testing.T) {
	testutil.SetupTestEnv(t)

	hidden := mustCreateReconcileDependency(t, model.DepTypePython, "argparse", "3.12", model.DepStatusInstalled)
	listed := mustCreateReconcileDependency(t, model.DepTypePython, "Requests", "3.12", model.DepStatusInstalled)

	stubs := stubReconcileFuncs(t, map[string]map[string]bool{
		"3.12": {"requests": true},
	}, func(depType, name, pythonVersion string) bool {
		return name == "argparse"
	})

	ReconcileDependenciesAfterRestart()

	if strings.Join(stubs.installedCalls, ",") != "python/argparse/3.12" {
		t.Fatalf("只有 pip list 不列出的名字才逐条问，实际 %v", stubs.installedCalls)
	}
	if len(stubs.scheduled) != 0 {
		t.Fatalf("argparse 用 pip show 查得到、Requests 在快照里，都不应重装，实际 %v", stubs.scheduled)
	}
	for _, dep := range []model.Dependency{hidden, listed} {
		if got := reloadReconcileDependency(t, dep.ID); got.Status != model.DepStatusInstalled {
			t.Fatalf("%s 应保持已安装，实际 %q", dep.Name, got.Status)
		}
	}
}

// 整轮超过阈值时打一行分类型耗时，给慢启动定位用；没超过时不打。
func TestReconcileDependenciesAfterRestartLogsSlowRoundByType(t *testing.T) {
	testutil.SetupTestEnv(t)
	logs := captureStandardLog(t)

	mustCreateReconcileDependency(t, model.DepTypePython, "requests", "3.12", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypeNodeJS, "left-pad", "", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypeLinux, "curl", "", model.DepStatusInstalled)
	stubReconcileFuncs(t, map[string]map[string]bool{"3.12": {"requests": true}}, func(depType, name, pythonVersion string) bool { return true })

	originalThreshold := dependencyReconcileSlowLogThreshold
	t.Cleanup(func() { dependencyReconcileSlowLogThreshold = originalThreshold })

	// 判定都很快，默认 3 秒阈值下不打。
	ReconcileDependenciesAfterRestart()
	if strings.Contains(logs.String(), "[启动校验] 校验") {
		t.Fatalf("整轮没超过阈值时不应打耗时日志，实际：\n%s", logs.String())
	}

	dependencyReconcileSlowLogThreshold = 0
	ReconcileDependenciesAfterRestart()
	var line string
	for _, candidate := range strings.Split(logs.String(), "\n") {
		if strings.Contains(candidate, "[启动校验] 校验") {
			line = candidate
		}
	}
	if !strings.Contains(line, "[启动校验] 校验 3 条已安装依赖耗时 ") ||
		!strings.Contains(line, "（Python ") || !strings.Contains(line, " / Node.js ") || !strings.Contains(line, " / Linux ") {
		t.Fatalf("超过阈值时应打一行「校验 N 条已安装依赖耗时 X（Python a / Node.js b / Linux c）」，实际 %q", line)
	}
}

// 用托管 venv 里冒充的 pip 端到端走一遍：托管 pip 在时直接 pip list，不做健康检查（#158），4 条 Python 依赖只起这 1 个子进程；
// 规范名匹配大小写与 - _ 的差异、带版本号的名字按主包判定，真缺的那条排进重装。
func TestReconcileDependenciesAfterRestartListsPythonPackagesOnceWithManagedPip(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests", "PyYAML", "python-dotenv"}, "")

	mustCreateReconcileDependency(t, model.DepTypePython, "requests==2.31.0", "3.12", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypePython, "pyyaml", "3.12", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypePython, "python_dotenv", "", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypePython, "notexist-pkg", "3.12", model.DepStatusInstalled)

	originalRestartReinstallBatch := dependencyRestartReinstallBatchFunc
	t.Cleanup(func() { dependencyRestartReinstallBatchFunc = originalRestartReinstallBatch })
	var scheduled []string
	dependencyRestartReinstallBatchFunc = func(deps []model.Dependency) {
		for _, dep := range deps {
			scheduled = append(scheduled, dep.Name)
		}
	}

	ReconcileDependenciesAfterRestart()

	if strings.Join(scheduled, ",") != "notexist-pkg" {
		t.Fatalf("只有 notexist-pkg 是真缺的，实际排进重装 %v", scheduled)
	}
	calls := readFakeVenvExecLog(t, execLog)
	if len(calls) != 1 || calls[0] != "pip3 list --format=json --disable-pip-version-check" {
		t.Fatalf("托管 pip 在时 4 条 Python 依赖应只起 1 个子进程（直接 pip list，不做健康检查、不逐条 pip show），实际 %d 个：%v", len(calls), calls)
	}
}

// 托管 venv 坏了（pip 跑不起来）时：直接 pip list 失败 → 走老路，健康检查发现坏了 → 修不好 → 挪成 <venv>.broken-<时间> 再重建
// → 在新 venv 上再列一次（#158）。「venv 损坏时能发现并重建」靠的就是这条回退。新 venv 是空的，两条依赖都排进重装。
func TestReconcileDependenciesAfterRestartRebuildsBrokenManagedVenv(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests", "PyYAML"}, "")
	venvDir := ManagedPythonVenvDir("3.12")

	// 把托管 venv 弄坏：pip / pip3 不管什么子命令都报「No module named pip」并以 1 退出（换镜像后 Python 小版本变了就是这样）。
	broken := "#!/bin/sh\necho \"pip3 $*\" >> '" + execLog + "'\necho \"ModuleNotFoundError: No module named 'pip'\" >&2\nexit 1\n"
	for _, name := range []string{"pip", "pip3"} {
		if err := os.WriteFile(filepath.Join(venvDir, "bin", name), []byte(broken), 0o755); err != nil {
			t.Fatalf("write broken %s: %v", name, err)
		}
	}

	// 新 venv 的模板：python 报 3.12，pip3 --version 正常，pip3 list 输出空列表；日志行带 new- 前缀，和旧 venv 区分开。
	template := filepath.Join(t.TempDir(), "venv-template")
	if err := os.MkdirAll(filepath.Join(template, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir venv template: %v", err)
	}
	newScripts := map[string]string{
		"python": "#!/bin/sh\necho \"new-python $*\" >> '" + execLog + "'\necho 3.12\n",
		"pip3": "#!/bin/sh\necho \"new-pip3 $*\" >> '" + execLog + "'\n" +
			"case \"$1\" in\n--version) echo 'pip 24.0 from /fake (python 3.12)' ;;\nlist) echo '[]' ;;\n*) exit 2 ;;\nesac\n",
	}
	for name, content := range newScripts {
		if err := os.WriteFile(filepath.Join(template, "bin", name), []byte(content), 0o755); err != nil {
			t.Fatalf("write template %s: %v", name, err)
		}
	}
	// PATH 最前面放一个假的 python3.12：--version 报 3.12.13；-m venv DIR 把模板拷进 DIR。
	// 要用 mkdir / cp，所以只能前置，不能用 isolatePythonProbePath 把 PATH 整个换掉。
	fakeBin := t.TempDir()
	bootstrap := "#!/bin/sh\necho \"python3.12 $*\" >> '" + execLog + "'\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'Python 3.12.13'; exit 0; fi\n" +
		"if [ \"$1\" = \"-m\" ] && [ \"$2\" = \"venv\" ]; then mkdir -p \"$3\" && cp -R '" + template + "/.' \"$3/\"; exit $?; fi\n" +
		"exit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "python3.12"), []byte(bootstrap), 0o755); err != nil {
		t.Fatalf("write fake python3.12: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	mustCreateReconcileDependency(t, model.DepTypePython, "requests", "3.12", model.DepStatusInstalled)
	mustCreateReconcileDependency(t, model.DepTypePython, "pyyaml", "3.12", model.DepStatusInstalled)
	originalRestartReinstallBatch := dependencyRestartReinstallBatchFunc
	t.Cleanup(func() { dependencyRestartReinstallBatchFunc = originalRestartReinstallBatch })
	var scheduled []string
	dependencyRestartReinstallBatchFunc = func(deps []model.Dependency) {
		for _, dep := range deps {
			scheduled = append(scheduled, dep.Name)
		}
	}
	logs := captureStandardLog(t)

	ReconcileDependenciesAfterRestart()

	if strings.Join(scheduled, ",") != "requests,pyyaml" {
		t.Fatalf("重建出来的 venv 是空的，两条依赖都应排进重装，实际 %v", scheduled)
	}
	moved, _ := filepath.Glob(venvDir + ".broken-*")
	if len(moved) != 1 {
		t.Fatalf("坏掉的 venv 应被挪成 %s.broken-<时间>，实际 %v", venvDir, moved)
	}
	calls := readFakeVenvExecLog(t, execLog)
	joined := strings.Join(calls, "\n")
	if len(calls) < 2 || calls[0] != "pip3 list --format=json --disable-pip-version-check" ||
		!strings.Contains(joined, "python3.12 -m venv "+venvDir) ||
		calls[len(calls)-1] != "new-pip3 list --format=json --disable-pip-version-check" {
		t.Fatalf("应先直接 pip list（失败），再重建 venv，最后在新 venv 上 pip list，实际：\n%s", joined)
	}
	if strings.Contains(logs.String(), "列举 Python 3.12 已安装的包失败") {
		t.Fatalf("重建后列举成功，不应退回逐条校验，实际日志：\n%s", logs.String())
	}
}

// listPythonInstalledPackages 只解析 stdout：中断安装留下的 ~xxx 残目录让 pip 往 stderr 打告警，不能影响 JSON。
// 冒充的 pip 在没带 --disable-pip-version-check 时直接报错，所以能列出来就说明这个参数带上了。
func TestListPythonInstalledPackagesReadsOnlyStdout(t *testing.T) {
	testutil.SetupTestEnv(t)
	execLog := writeFakeManagedVenv(t, []string{"requests", "PyYAML", "python-dotenv"}, "")

	installed, err := listPythonInstalledPackages("3.12")
	if err != nil {
		t.Fatalf("stderr 里的告警不应让列举失败：%v", err)
	}
	for _, key := range []string{"requests", "pyyaml", "python-dotenv"} {
		if !installed[key] {
			t.Fatalf("快照应按 PEP 503 规范名收录 %s，实际 %v", key, installed)
		}
	}
	if len(installed) != 3 {
		t.Fatalf("快照只应有 pip list 列出的 3 个包，实际 %v", installed)
	}
	calls := readFakeVenvExecLog(t, execLog)
	if last := calls[len(calls)-1]; last != "pip3 list --format=json --disable-pip-version-check" {
		t.Fatalf("应只用 pip list --format=json --disable-pip-version-check 列举，实际最后一条是 %q", last)
	}
}

func TestListPythonInstalledPackagesRejectsNonJSONOutput(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeFakeManagedVenv(t, []string{"requests"}, "garbage")

	installed, err := listPythonInstalledPackages("3.12")
	if installed != nil || err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("输出不是 JSON 时应返回 nil 并说明原因，实际 %v / %v", installed, err)
	}
}

// pip 卡住时按超时收尾，并且整组杀掉：冒充的 pip 挂着一个攥住 stdout 的孙进程，
// 只杀 pip 本身的话每次都要再等 WaitDelay（10 秒）才返回。
// #158 起直接列、走老路各超时一次（各 1 秒：直接列超时后，健康检查照样通过，再列一次又卡住），6 秒上界照旧够用。
func TestListPythonInstalledPackagesTimesOutAndKillsProcessGroup(t *testing.T) {
	testutil.SetupTestEnv(t)
	writeFakeManagedVenv(t, []string{"requests"}, "hang")

	originalTimeout := pythonInstalledPackagesTimeout
	t.Cleanup(func() { pythonInstalledPackagesTimeout = originalTimeout })
	pythonInstalledPackagesTimeout = time.Second

	startedAt := time.Now()
	installed, err := listPythonInstalledPackages("3.12")
	elapsed := time.Since(startedAt)
	if installed != nil || err == nil || !strings.Contains(err.Error(), "仍未结束") {
		t.Fatalf("超时应返回 nil 并说明已终止，实际 %v / %v", installed, err)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("超时后应整组杀掉立即返回，实际等了 %s", elapsed)
	}
}

// readFakeVenvExecLog 读出冒充的 python / pip 被调用的记录，一行一次。
func readFakeVenvExecLog(t *testing.T, execLog string) []string {
	t.Helper()
	data, err := os.ReadFile(execLog)
	if err != nil {
		t.Fatalf("read fake venv exec log: %v", err)
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}
