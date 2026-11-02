package service

import (
	"bytes"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// stubMagiskPythonInterpreters 把模块版迁移用的解释器探测换成只认列出的版本。
// 真实探测会 exec python3.X，结果随宿主机漂移，这里必须由用例写死。
func stubMagiskPythonInterpreters(t *testing.T, versions ...string) {
	t.Helper()
	original := magiskPythonInterpreterProbeFunc
	magiskPythonInterpreterProbeFunc = installedProbe(versions...)
	t.Cleanup(func() {
		magiskPythonInterpreterProbeFunc = original
	})
}

// setMagiskPythonRuntimeEnv 模拟 service.sh 导出的模块版环境。
// DAIDAI_PYTHON_RUNTIME_MODE 显式清空：宿主机上若设了 single，会让用例混进 Docker 单版本语义。
func setMagiskPythonRuntimeEnv(t *testing.T, current string) {
	t.Helper()
	t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	t.Setenv("DAIDAI_PYTHON_VERSION", current)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "")
}

type magiskMigrationSeed struct {
	deps  []model.Dependency
	tasks []model.Task
}

// seedMagiskMigrationData 造一份「Alpine 3.18 升 3.23 之后」的典型老库：
// 3.11 名下有依赖、任务和默认值，3.12 名下已经有一条同名依赖，另外混一条 Node 依赖和一条 3.10 任务作对照。
func seedMagiskMigrationData(t *testing.T) magiskMigrationSeed {
	t.Helper()

	deps := []model.Dependency{
		{Type: model.DepTypePython, Name: "requests", PythonVersion: "3.11", Status: model.DepStatusInstalled, Log: "old 3.11 requests"},
		// 3.12 名下的同名依赖（大小写不同），迁移后必须与上一条合并成一条。
		{Type: model.DepTypePython, Name: "Requests", PythonVersion: "3.12", Status: model.DepStatusFailed, Log: "old 3.12 requests"},
		{Type: model.DepTypePython, Name: "flask", PythonVersion: "3.11", Status: model.DepStatusInstalled, Log: "old 3.11 flask"},
		{Type: model.DepTypePython, Name: "pyyaml", PythonVersion: "3.10", Status: model.DepStatusFailed, Log: "old 3.10 pyyaml"},
		// 非 Python 依赖哪怕 python_version 列碰巧写着 3.11 也不能动。
		{Type: model.DepTypeNodeJS, Name: "axios", PythonVersion: "3.11", Status: model.DepStatusInstalled, Log: "node axios"},
	}
	for i := range deps {
		if err := database.DB.Create(&deps[i]).Error; err != nil {
			t.Fatalf("seed dependency %d: %v", i, err)
		}
	}

	tasks := []model.Task{
		{Name: "task-311", Command: "task a.py", PythonVersion: "3.11", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
		{Name: "task-312", Command: "task b.py", PythonVersion: "3.12", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
		{Name: "task-310", Command: "task c.py", PythonVersion: "3.10", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
	}
	for i := range tasks {
		if err := database.DB.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}

	if err := model.SetConfig("python_default_version", "3.11"); err != nil {
		t.Fatalf("seed python_default_version: %v", err)
	}

	return magiskMigrationSeed{deps: deps, tasks: tasks}
}

// runMagiskPythonStartupSequence 按 appboot 的真实顺序执行：先迁移，再合并同名重复依赖。
// appboot.go 里的真实顺序由 startup_wiring_test.go 按源码核对，两边不会悄悄脱节。
func runMagiskPythonStartupSequence() {
	ApplyMagiskPythonRuntimeMigrationOnStartup()
	MergeDuplicatePythonDependencies()
}

func loadDependencyByID(t *testing.T, id uint) (model.Dependency, bool) {
	t.Helper()
	var dep model.Dependency
	result := database.DB.Where("id = ?", id).Limit(1).Find(&dep)
	if result.Error != nil {
		t.Fatalf("load dependency %d: %v", id, result.Error)
	}
	return dep, result.RowsAffected == 1
}

func loadTaskPythonVersion(t *testing.T, id uint) string {
	t.Helper()
	var task model.Task
	if err := database.DB.First(&task, id).Error; err != nil {
		t.Fatalf("load task %d: %v", id, err)
	}
	return task.PythonVersion
}

// captureStandardLog 临时接管标准 log 输出，用例结束还原。
func captureStandardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
	return &buf
}

// 核心场景：模块版升级后 python3 变成 3.12，3.11 / 3.10 解释器都没了。
// 依赖、任务、默认值都要迁到 3.12，同名依赖合并成一条，status 与 log 原样保留（重装交给启动校验）。
func TestApplyMagiskPythonRuntimeMigrationMovesRecordsWhenOldInterpreterMissing(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.12")
	seed := seedMagiskMigrationData(t)
	logs := captureStandardLog(t)

	runMagiskPythonStartupSequence()

	var leftover int64
	if err := database.DB.Model(&model.Dependency{}).
		Where("type = ? AND python_version IN ?", model.DepTypePython, []string{"3.10", "3.11"}).
		Count(&leftover).Error; err != nil {
		t.Fatalf("count leftover python deps: %v", err)
	}
	if leftover != 0 {
		t.Fatalf("expected no python dependency left on 3.10/3.11, got %d", leftover)
	}

	// requests 两条合并为一条，保留状态优先级更高的 installed（原 3.11 那条）。
	var requestsRows []model.Dependency
	if err := database.DB.Where("type = ? AND python_version = ?", model.DepTypePython, "3.12").
		Where("name IN ?", []string{"requests", "Requests"}).Find(&requestsRows).Error; err != nil {
		t.Fatalf("query requests rows: %v", err)
	}
	if len(requestsRows) != 1 {
		t.Fatalf("expected requests to be merged into 1 row on 3.12, got %d: %+v", len(requestsRows), requestsRows)
	}
	if requestsRows[0].ID != seed.deps[0].ID || requestsRows[0].Status != model.DepStatusInstalled {
		t.Fatalf("expected the installed 3.11 requests row to win the merge, got %+v", requestsRows[0])
	}

	// 迁移只改版本列，status / log 一个字都不动。
	for _, idx := range []int{2, 3} {
		want := seed.deps[idx]
		got, ok := loadDependencyByID(t, want.ID)
		if !ok {
			t.Fatalf("expected dependency %q to survive migration", want.Name)
		}
		if got.PythonVersion != "3.12" {
			t.Fatalf("expected dependency %q moved to 3.12, got %q", want.Name, got.PythonVersion)
		}
		if got.Status != want.Status || got.Log != want.Log {
			t.Fatalf("expected dependency %q status/log untouched, got status=%q log=%q", want.Name, got.Status, got.Log)
		}
	}

	nodeDep, ok := loadDependencyByID(t, seed.deps[4].ID)
	if !ok || nodeDep.PythonVersion != "3.11" || nodeDep.Status != model.DepStatusInstalled {
		t.Fatalf("expected nodejs dependency to be left alone, got ok=%v dep=%+v", ok, nodeDep)
	}

	for _, task := range seed.tasks {
		if got := loadTaskPythonVersion(t, task.ID); got != "3.12" {
			t.Fatalf("expected task %q moved to 3.12, got %q", task.Name, got)
		}
	}

	if got := model.GetRegisteredConfig("python_default_version"); got != "3.12" {
		t.Fatalf("expected python_default_version moved to 3.12, got %q", got)
	}

	text := logs.String()
	for _, want := range []string{"Python 3.11", "Python 3.10", "当前 Python 3.12"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected migration log to mention %q, got:\n%s", want, text)
		}
	}
}

// 3.11 解释器仍在（例如用户在容器里自己装回了 python3.11）时，3.11 名下的记录一条都不能动；
// 同一次启动里真正缺失的 3.10 仍照常迁移——判定是逐版本的，不是「有一个缺就全搬」。
func TestApplyMagiskPythonRuntimeMigrationKeepsVersionsWhoseInterpreterStillExists(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.11", "3.12")
	seed := seedMagiskMigrationData(t)

	runMagiskPythonStartupSequence()

	for _, idx := range []int{0, 2} {
		got, ok := loadDependencyByID(t, seed.deps[idx].ID)
		if !ok || got.PythonVersion != "3.11" {
			t.Fatalf("expected 3.11 dependency %q untouched while python3.11 exists, got ok=%v dep=%+v", seed.deps[idx].Name, ok, got)
		}
	}
	// 3.12 名下原有的 requests 也不能被合并删掉（它和 3.11 那条不属于同一组）。
	if _, ok := loadDependencyByID(t, seed.deps[1].ID); !ok {
		t.Fatal("expected existing 3.12 requests row to survive when 3.11 is kept")
	}
	if got := loadTaskPythonVersion(t, seed.tasks[0].ID); got != "3.11" {
		t.Fatalf("expected 3.11 task untouched while python3.11 exists, got %q", got)
	}
	if got := model.GetRegisteredConfig("python_default_version"); got != "3.11" {
		t.Fatalf("expected python_default_version kept at 3.11 while python3.11 exists, got %q", got)
	}

	pyyaml, ok := loadDependencyByID(t, seed.deps[3].ID)
	if !ok || pyyaml.PythonVersion != "3.12" {
		t.Fatalf("expected missing 3.10 dependency moved to 3.12, got ok=%v dep=%+v", ok, pyyaml)
	}
	if got := loadTaskPythonVersion(t, seed.tasks[2].ID); got != "3.12" {
		t.Fatalf("expected missing 3.10 task moved to 3.12, got %q", got)
	}
}

// 非模块运行态（Docker / Windows / 普通 Linux）绝不能因为探测不到 3.11 就改掉用户显式选的版本。
func TestApplyMagiskPythonRuntimeMigrationSkipsNonModuleRuntime(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_MAGISK_MODULE", "")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "")
	if IsMagiskModuleRuntime() {
		t.Skip("当前机器上存在 Magisk 模块标记文件，无法模拟非模块运行态")
	}
	stubMagiskPythonInterpreters(t, "3.12")
	seed := seedMagiskMigrationData(t)

	ApplyMagiskPythonRuntimeMigrationOnStartup()

	assertMagiskMigrationSeedUntouched(t, seed)
}

// DAIDAI_PYTHON_VERSION 为空 / 不在支持范围 / 当前版本自己都探测不到解释器时，容器状态不可信，一律不搬。
func TestApplyMagiskPythonRuntimeMigrationSkipsUnhealthyCurrentVersion(t *testing.T) {
	cases := []struct {
		name      string
		current   string
		installed []string
	}{
		{name: "empty env", current: "", installed: []string{"3.12"}},
		{name: "unsupported version", current: "3.14", installed: []string{"3.12", "3.14"}},
		{name: "current interpreter missing", current: "3.12", installed: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			setMagiskPythonRuntimeEnv(t, tc.current)
			stubMagiskPythonInterpreters(t, tc.installed...)
			seed := seedMagiskMigrationData(t)

			ApplyMagiskPythonRuntimeMigrationOnStartup()

			assertMagiskMigrationSeedUntouched(t, seed)
		})
	}
}

// 每次启动都会执行：第二次执行必须一行都不改（含 updated_at），也不再打迁移日志。
func TestApplyMagiskPythonRuntimeMigrationIsIdempotentAcrossRestarts(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.12")
	seedMagiskMigrationData(t)

	runMagiskPythonStartupSequence()
	firstDeps, firstTasks, firstDefault := snapshotMagiskMigrationState(t)

	logs := captureStandardLog(t)
	runMagiskPythonStartupSequence()
	secondDeps, secondTasks, secondDefault := snapshotMagiskMigrationState(t)

	if firstDefault != secondDefault {
		t.Fatalf("expected default python version stable across restarts, got %q then %q", firstDefault, secondDefault)
	}
	if len(firstDeps) != len(secondDeps) {
		t.Fatalf("expected dependency rows stable across restarts, got %d then %d", len(firstDeps), len(secondDeps))
	}
	for i := range firstDeps {
		a, b := firstDeps[i], secondDeps[i]
		if a.ID != b.ID || a.Name != b.Name || a.PythonVersion != b.PythonVersion || a.Status != b.Status || a.Log != b.Log || !a.UpdatedAt.Equal(b.UpdatedAt) {
			t.Fatalf("expected dependency row unchanged on second run, got %+v then %+v", a, b)
		}
	}
	if len(firstTasks) != len(secondTasks) {
		t.Fatalf("expected task rows stable across restarts, got %d then %d", len(firstTasks), len(secondTasks))
	}
	for i := range firstTasks {
		if firstTasks[i].ID != secondTasks[i].ID || firstTasks[i].PythonVersion != secondTasks[i].PythonVersion || !firstTasks[i].UpdatedAt.Equal(secondTasks[i].UpdatedAt) {
			t.Fatalf("expected task row unchanged on second run, got %+v then %+v", firstTasks[i], secondTasks[i])
		}
	}
	if strings.Contains(logs.String(), "模块版 Python 迁移") {
		t.Fatalf("expected no migration log on second run, got:\n%s", logs.String())
	}
}

// Debian 版模块：rootfs 仍是 bookworm，python3 = 3.11，没有装一键 Python 3.12。
// 比当前版本新的 3.12（InitDefaultConfigs 写进库的默认值、用户选的 3.12 任务与依赖）一条都不能动：
// 迁到 3.11 之后，用户再装上一键 3.12，3.11 解释器仍在，这些记录就永远迁不回来了。
// 比当前版本旧、解释器又确实不在的 3.10 仍照常迁到 3.11。
func TestApplyMagiskPythonRuntimeMigrationLeavesNewerVersionsOnDebianFlavor(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.11")
	stubMagiskPythonInterpreters(t, "3.11")

	var storedDefault model.SystemConfig
	if err := database.DB.Where("`key` = ?", "python_default_version").First(&storedDefault).Error; err != nil || storedDefault.Value != "3.12" {
		t.Fatalf("expected InitDefaultConfigs to persist python_default_version=3.12 (the Debian trap), got value=%q err=%v", storedDefault.Value, err)
	}

	deps := []model.Dependency{
		{Type: model.DepTypePython, Name: "httpx", PythonVersion: "3.12", Status: model.DepStatusInstalled, Log: "3.12 httpx"},
		{Type: model.DepTypePython, Name: "pyyaml", PythonVersion: "3.10", Status: model.DepStatusFailed, Log: "3.10 pyyaml"},
	}
	for i := range deps {
		if err := database.DB.Create(&deps[i]).Error; err != nil {
			t.Fatalf("seed dependency %d: %v", i, err)
		}
	}
	tasks := []model.Task{
		{Name: "task-312", Command: "task b.py", PythonVersion: "3.12", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
		{Name: "task-310", Command: "task c.py", PythonVersion: "3.10", CronExpression: "0 0 * * *", TaskType: model.TaskTypeCron, Status: model.TaskStatusEnabled},
	}
	for i := range tasks {
		if err := database.DB.Create(&tasks[i]).Error; err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}
	logs := captureStandardLog(t)

	assertNewerKept := func(stage string) {
		t.Helper()
		if got, ok := loadDependencyByID(t, deps[0].ID); !ok || got.PythonVersion != "3.12" {
			t.Fatalf("%s: expected 3.12 dependency untouched, got ok=%v dep=%+v", stage, ok, got)
		}
		if got := loadTaskPythonVersion(t, tasks[0].ID); got != "3.12" {
			t.Fatalf("%s: expected 3.12 task untouched, got %q", stage, got)
		}
		if got := model.GetRegisteredConfig("python_default_version"); got != "3.12" {
			t.Fatalf("%s: expected python_default_version kept at 3.12, got %q", stage, got)
		}
	}

	// 第一次启动：Debian 版 C=3.11。
	runMagiskPythonStartupSequence()
	assertNewerKept("debian boot")
	if got, ok := loadDependencyByID(t, deps[1].ID); !ok || got.PythonVersion != "3.11" {
		t.Fatalf("expected missing older 3.10 dependency moved to 3.11, got ok=%v dep=%+v", ok, got)
	}
	if got := loadTaskPythonVersion(t, tasks[1].ID); got != "3.11" {
		t.Fatalf("expected missing older 3.10 task moved to 3.11, got %q", got)
	}
	if strings.Contains(logs.String(), "Python 3.12 解释器") {
		t.Fatalf("expected no migration away from 3.12 on the Debian flavor, got:\n%s", logs.String())
	}

	// 用户随后装上一键 Python 3.12：PATH 最前的 python3 变成 3.12，系统 3.11 仍在。
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.11", "3.12")
	runMagiskPythonStartupSequence()
	assertNewerKept("after one-click python 3.12")
}

// 迁移用裸 SQL 就是为了不刷新 updated_at：合并同名依赖时状态相同就按 updated_at 挑保留行。
// 这里只跑迁移、不跑合并（合并会删行），逐行核对 updated_at 与迁移前完全一致。
func TestApplyMagiskPythonRuntimeMigrationKeepsUpdatedAt(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.12")
	seedMagiskMigrationData(t)

	// 统一回拨到一个固定的旧时间，任何「顺手刷新成当前时间」的写法都不可能碰巧相等。
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := database.DB.Exec("UPDATE dependencies SET updated_at = ?", old).Error; err != nil {
		t.Fatalf("backdate dependencies: %v", err)
	}
	if err := database.DB.Exec("UPDATE tasks SET updated_at = ?", old).Error; err != nil {
		t.Fatalf("backdate tasks: %v", err)
	}
	beforeDeps, beforeTasks, _ := snapshotMagiskMigrationState(t)

	ApplyMagiskPythonRuntimeMigrationOnStartup()

	afterDeps, afterTasks, _ := snapshotMagiskMigrationState(t)
	if len(beforeDeps) != len(afterDeps) || len(beforeTasks) != len(afterTasks) {
		t.Fatalf("expected migration alone not to add or delete rows, deps %d->%d tasks %d->%d", len(beforeDeps), len(afterDeps), len(beforeTasks), len(afterTasks))
	}
	movedDeps, movedTasks := 0, 0
	for i := range beforeDeps {
		a, b := beforeDeps[i], afterDeps[i]
		if a.PythonVersion != b.PythonVersion {
			movedDeps++
		}
		if a.ID != b.ID || !a.UpdatedAt.Equal(b.UpdatedAt) {
			t.Fatalf("expected dependency %q updated_at untouched by migration, got %s -> %s", a.Name, a.UpdatedAt, b.UpdatedAt)
		}
	}
	for i := range beforeTasks {
		a, b := beforeTasks[i], afterTasks[i]
		if a.PythonVersion != b.PythonVersion {
			movedTasks++
		}
		if a.ID != b.ID || !a.UpdatedAt.Equal(b.UpdatedAt) {
			t.Fatalf("expected task %q updated_at untouched by migration, got %s -> %s", a.Name, a.UpdatedAt, b.UpdatedAt)
		}
	}
	// 确认迁移确实改了行，否则上面的「没变」没有意义。
	if movedDeps != 3 || movedTasks != 2 {
		t.Fatalf("expected 3 dependencies and 2 tasks moved, got %d and %d", movedDeps, movedTasks)
	}
}

// 状态相同的同名依赖，合并按 updated_at 保留较新的那条。迁移若刷新了旧版本记录的 updated_at，
// 就会让刚被迁过来的旧记录反超，把用户在当前版本上较新的那条删掉。
func TestApplyMagiskPythonRuntimeMigrationKeepsNewerRowWinningSameStatusMerge(t *testing.T) {
	testutil.SetupTestEnv(t)
	setMagiskPythonRuntimeEnv(t, "3.12")
	stubMagiskPythonInterpreters(t, "3.12")

	older := model.Dependency{Type: model.DepTypePython, Name: "requests", PythonVersion: "3.11", Status: model.DepStatusInstalled, Log: "older 3.11"}
	newer := model.Dependency{Type: model.DepTypePython, Name: "requests", PythonVersion: "3.12", Status: model.DepStatusInstalled, Log: "newer 3.12"}
	for _, dep := range []*model.Dependency{&older, &newer} {
		if err := database.DB.Create(dep).Error; err != nil {
			t.Fatalf("seed dependency: %v", err)
		}
	}
	if err := database.DB.Exec("UPDATE dependencies SET updated_at = ? WHERE id = ?", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), older.ID).Error; err != nil {
		t.Fatalf("backdate older dependency: %v", err)
	}
	if err := database.DB.Exec("UPDATE dependencies SET updated_at = ? WHERE id = ?", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), newer.ID).Error; err != nil {
		t.Fatalf("backdate newer dependency: %v", err)
	}

	runMagiskPythonStartupSequence()

	var rows []model.Dependency
	if err := database.DB.Where("type = ? AND name = ?", model.DepTypePython, "requests").Find(&rows).Error; err != nil {
		t.Fatalf("query requests rows: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != newer.ID || rows[0].Log != "newer 3.12" {
		t.Fatalf("expected the newer 3.12 row to survive the same-status merge, got %+v", rows)
	}
}

// 迁移循环遇到当前版本就停，依赖 allPythonRuntimeVersions 严格升序；以后插入新版本打乱顺序，这里会先红。
func TestAllPythonRuntimeVersionsAreAscending(t *testing.T) {
	parse := func(version string) (int, int) {
		major, minor, ok := strings.Cut(version, ".")
		if !ok {
			t.Fatalf("unexpected python runtime version %q", version)
		}
		ma, errMajor := strconv.Atoi(major)
		mi, errMinor := strconv.Atoi(minor)
		if errMajor != nil || errMinor != nil {
			t.Fatalf("unexpected python runtime version %q", version)
		}
		return ma, mi
	}
	for i := 1; i < len(allPythonRuntimeVersions); i++ {
		prevMajor, prevMinor := parse(allPythonRuntimeVersions[i-1])
		major, minor := parse(allPythonRuntimeVersions[i])
		if major < prevMajor || (major == prevMajor && minor <= prevMinor) {
			t.Fatalf("expected allPythonRuntimeVersions strictly ascending, got %v", allPythonRuntimeVersions)
		}
	}
}

func assertMagiskMigrationSeedUntouched(t *testing.T, seed magiskMigrationSeed) {
	t.Helper()
	for _, want := range seed.deps {
		got, ok := loadDependencyByID(t, want.ID)
		if !ok {
			t.Fatalf("expected dependency %q (%s) to still exist", want.Name, want.PythonVersion)
		}
		if got.PythonVersion != want.PythonVersion || got.Status != want.Status || got.Log != want.Log {
			t.Fatalf("expected dependency %q untouched, want %+v got %+v", want.Name, want, got)
		}
	}
	for _, want := range seed.tasks {
		if got := loadTaskPythonVersion(t, want.ID); got != want.PythonVersion {
			t.Fatalf("expected task %q python version untouched (%q), got %q", want.Name, want.PythonVersion, got)
		}
	}
	if got := model.GetRegisteredConfig("python_default_version"); got != "3.11" {
		t.Fatalf("expected python_default_version untouched (3.11), got %q", got)
	}
}

func snapshotMagiskMigrationState(t *testing.T) ([]model.Dependency, []model.Task, string) {
	t.Helper()
	var deps []model.Dependency
	if err := database.DB.Order("id").Find(&deps).Error; err != nil {
		t.Fatalf("snapshot dependencies: %v", err)
	}
	var tasks []model.Task
	if err := database.DB.Order("id").Find(&tasks).Error; err != nil {
		t.Fatalf("snapshot tasks: %v", err)
	}
	return deps, tasks, model.GetRegisteredConfig("python_default_version")
}
