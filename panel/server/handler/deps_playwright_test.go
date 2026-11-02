package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/service"
	"daidai-panel/testutil"

	"gorm.io/gorm"
)

// stubPlaywrightHandler 把一键安装依赖的环境判定全部换成给定值，用例结束自动还原。
// installed 模拟「这个包是否真的装在系统里」（生产实现会去跑 dpkg-query / pip show）。
func stubPlaywrightHandler(t *testing.T, plan service.PlaywrightRuntimePlan, privilegeErr error, installed func(depType, name, version string) bool) {
	t.Helper()
	oldPlan, oldPrivilege := playwrightPlanFunc, playwrightPrivilegeCheckFunc
	oldInstalled, oldDefault := playwrightDependencyInstalledFunc, playwrightDefaultPythonVersionFunc
	t.Cleanup(func() {
		playwrightPlanFunc, playwrightPrivilegeCheckFunc = oldPlan, oldPrivilege
		playwrightDependencyInstalledFunc, playwrightDefaultPythonVersionFunc = oldInstalled, oldDefault
	})
	playwrightPlanFunc = func() service.PlaywrightRuntimePlan { return plan }
	playwrightPrivilegeCheckFunc = func() error { return privilegeErr }
	if installed == nil {
		installed = func(string, string, string) bool { return false }
	}
	playwrightDependencyInstalledFunc = installed
	playwrightDefaultPythonVersionFunc = func() string { return "3.12" }
}

func supportedPlaywrightPlan(browsersPath string) service.PlaywrightRuntimePlan {
	return service.PlaywrightRuntimePlan{
		Supported:    true,
		Distribution: "debian",
		VersionID:    "12",
		Arch:         "amd64",
		Packages:     service.PlaywrightDebian12Packages(),
		BrowsersPath: browsersPath,
	}
}

func stubDependencyRunner(t *testing.T, runner func(id uint, depType, name string)) {
	t.Helper()
	old := dependencyInstallRunner
	t.Cleanup(func() { dependencyInstallRunner = old })
	dependencyInstallRunner = runner
}

func mustCreateDependency(t *testing.T, dep model.Dependency) model.Dependency {
	t.Helper()
	if err := database.DB.Create(&dep).Error; err != nil {
		t.Fatalf("seed dependency %s: %v", dep.Name, err)
	}
	return dep
}

func TestPlaywrightStatusReportsReadiness(t *testing.T) {
	testutil.SetupTestEnv(t)

	browsersPath := filepath.Join(t.TempDir(), "ms-playwright")
	if err := os.MkdirAll(filepath.Join(browsersPath, "chromium_headless_shell-1181"), 0o755); err != nil {
		t.Fatal(err)
	}
	stubPlaywrightHandler(t, supportedPlaywrightPlan(browsersPath), nil, func(depType, name, version string) bool {
		// libnss3 登记了且真装着；libasound2 登记了但容器重建后已经不在；libgbm1 装着但没登记。
		return name == "libnss3" || name == "libgbm1" || (depType == model.DepTypePython && name == "playwright")
	})
	mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libnss3", Status: model.DepStatusInstalled})
	mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libasound2", Status: model.DepStatusInstalled})
	mustCreateDependency(t, model.Dependency{Type: model.DepTypePython, Name: "playwright", PythonVersion: "3.12", Status: model.DepStatusInstalled})

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodGet, "/api/v1/deps/playwright", nil, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Supported         bool     `json:"supported"`
		Reason            string   `json:"reason"`
		Distribution      string   `json:"distribution"`
		VersionID         string   `json:"version_id"`
		Arch              string   `json:"arch"`
		Packages          []string `json:"packages"`
		BrowsersPath      string   `json:"browsers_path"`
		PythonInstalled   bool     `json:"python_installed"`
		BrowsersInstalled bool     `json:"browsers_installed"`
		LinuxInstalled    int      `json:"linux_installed"`
		LinuxTotal        int      `json:"linux_total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Supported || body.Reason != "" || body.Distribution != "debian" || body.VersionID != "12" || body.Arch != "amd64" {
		t.Fatalf("判定字段不对：%+v", body)
	}
	if body.LinuxTotal != 26 || len(body.Packages) != 26 {
		t.Fatalf("清单应为 26 个包，实际 total=%d packages=%d", body.LinuxTotal, len(body.Packages))
	}
	// 只数「登记为已安装、且确实装着」的：libasound2 已经不在，libgbm1 没登记，都不算。
	if body.LinuxInstalled != 1 {
		t.Fatalf("linux_installed 期望 1，实际 %d", body.LinuxInstalled)
	}
	if !body.PythonInstalled || !body.BrowsersInstalled || body.BrowsersPath != browsersPath {
		t.Fatalf("python / 浏览器状态不对：%+v", body)
	}
}

// 降权部署下 GET 就把按钮置灰并给出原因，不让用户点了才收到 400；清单照常下发。
func TestPlaywrightStatusFoldsPrivilegeIntoSupported(t *testing.T) {
	testutil.SetupTestEnv(t)
	privilegeErr := errors.New("当前面板以非 root 用户（uid=1000）运行，无法安装或卸载 Linux 系统依赖")
	stubPlaywrightHandler(t, supportedPlaywrightPlan("/data/deps/ms-playwright"), privilegeErr, nil)

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodGet, "/api/v1/deps/playwright", nil, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["supported"] != false || body["reason"] != privilegeErr.Error() {
		t.Fatalf("非 root 时应 supported=false 且 reason 为拦截说明，实际 %v", body)
	}
	if packages, _ := body["packages"].([]any); len(packages) != 26 {
		t.Fatalf("非 root 时清单仍应下发，实际 %v", body["packages"])
	}
	if body["browsers_installed"] != false || body["python_installed"] != false {
		t.Fatalf("什么都没装时两项都应为 false，实际 %v", body)
	}
}

// 前置检查必须在建任何记录之前：原来 Alpine / 非 root 下会先建出一批记录再全部 failed。
func TestInstallPlaywrightRejectsBeforeCreatingRecords(t *testing.T) {
	cases := []struct {
		name         string
		plan         service.PlaywrightRuntimePlan
		privilegeErr error
		wantError    string
	}{
		{
			name:      "不支持的环境",
			plan:      service.PlaywrightRuntimePlan{Supported: false, Reason: "Alpine 镜像（musl）跑不了 Playwright 官方的 Chromium", Packages: []string{}},
			wantError: "Alpine 镜像（musl）跑不了 Playwright 官方的 Chromium",
		},
		{
			name:         "非 root",
			plan:         supportedPlaywrightPlan("/data/deps/ms-playwright"),
			privilegeErr: errors.New("当前面板以非 root 用户（uid=1000）运行"),
			wantError:    "当前面板以非 root 用户（uid=1000）运行",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			stubPlaywrightHandler(t, tc.plan, tc.privilegeErr, nil)
			stubDependencyRunner(t, func(uint, string, string) {
				t.Error("被拒绝时不应开始任何安装")
			})

			engine := newDepsTestRouter()
			token := testutil.MustCreateAccessToken(t, "admin", "admin")
			rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/playwright/install", map[string]any{}, map[string]string{"Authorization": "Bearer " + token})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			var body map[string]string
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body["error"] != tc.wantError {
				t.Fatalf("error 应为 %q，实际 %q", tc.wantError, body["error"])
			}

			var count int64
			database.DB.Model(&model.Dependency{}).Count(&count)
			if count != 0 {
				t.Fatalf("被拒绝时不应建任何依赖记录，实际 %d 条", count)
			}
		})
	}
}

func TestInstallPlaywrightQueuesLinuxThenPython(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubPlaywrightHandler(t, supportedPlaywrightPlan("/data/deps/ms-playwright"), nil, func(depType, name, version string) bool {
		return depType == model.DepTypeLinux && name == "libnss3"
	})

	// 已登记且真装着 → 跳过；正在安装（例如重建后启动校验正在重装）→ 跳过；
	// failed → 复用原记录；登记为已装但实际不在 → 复用原记录；Python 已装 → 仍要重新入队补下浏览器。
	installedOK := mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libnss3", Status: model.DepStatusInstalled})
	busy := mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libcups2", Status: model.DepStatusInstalling})
	failed := mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libasound2", Status: model.DepStatusFailed, Log: "E: 旧的失败日志"})
	missing := mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "libdrm2", Status: model.DepStatusInstalled})
	python := mustCreateDependency(t, model.Dependency{Type: model.DepTypePython, Name: "playwright", PythonVersion: "3.12", Status: model.DepStatusInstalled})

	// 26 个包去掉 libnss3、libcups2 两个跳过的，剩 24 个系统库，再加 1 个 Python。
	const wantQueued = 25
	type call struct {
		id      uint
		depType string
		name    string
	}
	var (
		mu    sync.Mutex
		calls []call
		done  = make(chan struct{})
	)
	stubDependencyRunner(t, func(id uint, depType, name string) {
		var current model.Dependency
		database.DB.First(&current, id)
		if current.Status != model.DepStatusInstalling {
			t.Errorf("交给安装器之前应已置为 installing，%s 实际为 %s", name, current.Status)
		}
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)

		mu.Lock()
		calls = append(calls, call{id: id, depType: depType, name: name})
		n := len(calls)
		mu.Unlock()
		if n == wantQueued {
			close(done)
		}
	})

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/playwright/install", map[string]any{}, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Message      string           `json:"message"`
		Data         []map[string]any `json:"data"`
		Packages     []string         `json:"packages"`
		BrowsersPath string           `json:"browsers_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != wantQueued || len(body.Packages) != 26 || body.BrowsersPath != "/data/deps/ms-playwright" {
		t.Fatalf("响应不对：data=%d packages=%d browsers_path=%q", len(body.Data), len(body.Packages), body.BrowsersPath)
	}
	if !strings.Contains(body.Message, "共 25 项") || !strings.Contains(body.Message, "2 项已跳过") {
		t.Fatalf("message 应说明入队与跳过数量，实际 %q", body.Message)
	}
	for _, item := range body.Data {
		if item["status"] != model.DepStatusQueued {
			t.Fatalf("响应里的记录应是排队状态，实际 %v", item)
		}
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("等待顺序安装超时")
	}

	mu.Lock()
	got := append([]call(nil), calls...)
	mu.Unlock()

	// 顺序：清单顺序的系统库在前（跳过的两个不在其中），Python 的 playwright 最后。
	wantLinux := make([]string, 0, 24)
	for _, name := range service.PlaywrightDebian12Packages() {
		if name != "libnss3" && name != "libcups2" {
			wantLinux = append(wantLinux, name)
		}
	}
	gotLinux := make([]string, 0, 24)
	for _, c := range got[:len(got)-1] {
		if c.depType != model.DepTypeLinux {
			t.Fatalf("Python 必须排在所有系统库之后，实际顺序 %v", got)
		}
		gotLinux = append(gotLinux, c.name)
	}
	if !slices.Equal(gotLinux, wantLinux) {
		t.Fatalf("系统库应按清单顺序安装：\n期望 %v\n实际 %v", wantLinux, gotLinux)
	}
	last := got[len(got)-1]
	if last.depType != model.DepTypePython || last.name != "playwright" || last.id != python.ID {
		t.Fatalf("最后一项应是复用原记录的 Python playwright，实际 %+v", last)
	}

	// 复用原记录，而不是再建一条（否则失败记录和侧栏角标越积越多）。
	for _, reused := range []model.Dependency{failed, missing} {
		var count int64
		database.DB.Model(&model.Dependency{}).Where("type = ? AND name = ?", model.DepTypeLinux, reused.Name).Count(&count)
		if count != 1 {
			t.Fatalf("%s 应复用原记录，实际有 %d 条", reused.Name, count)
		}
		if !slices.ContainsFunc(got, func(c call) bool { return c.id == reused.ID }) {
			t.Fatalf("%s 的原记录（id=%d）应被重新入队", reused.Name, reused.ID)
		}
	}
	var linuxCount int64
	database.DB.Model(&model.Dependency{}).Where("type = ?", model.DepTypeLinux).Count(&linuxCount)
	if linuxCount != 26 {
		t.Fatalf("每个系统库应恰好一条记录，实际共 %d 条", linuxCount)
	}

	// 跳过的两条原样不动。
	if reloaded := reloadDependency(t, busy.ID); reloaded.Status != model.DepStatusInstalling {
		t.Fatalf("正在安装的记录不应被动，实际 %s", reloaded.Status)
	}
	if reloaded := reloadDependency(t, installedOK.ID); reloaded.Status != model.DepStatusInstalled || strings.Contains(reloaded.Log, "Playwright 一键安装") {
		t.Fatalf("已就绪的记录不应被动，实际 %+v", reloaded)
	}

	// 队列日志：先写「已加入顺序队列（i/n）」，开始时再写「开始执行（i/n）」。
	reloaded := reloadDependency(t, failed.ID)
	for _, want := range []string{"E: 旧的失败日志", "[Playwright 一键安装] 已加入顺序队列（", "/25）", "[Playwright 一键安装] 开始执行（"} {
		if !strings.Contains(reloaded.Log, want) {
			t.Fatalf("复用记录的日志应包含 %q，实际 %q", want, reloaded.Log)
		}
	}
}

// 连点两次：第二次必须看到第一次已排上的记录，一条都不再重复入队、也不新建记录。
func TestInstallPlaywrightTwiceDoesNotDuplicate(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubPlaywrightHandler(t, supportedPlaywrightPlan("/data/deps/ms-playwright"), nil, nil)

	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	var (
		mu    sync.Mutex
		count int
	)
	stubDependencyRunner(t, func(id uint, depType, name string) {
		<-release
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("status", model.DepStatusInstalled)
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		if n == 27 {
			once.Do(func() { close(finished) })
		}
	})

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	headers := map[string]string{"Authorization": "Bearer " + token}

	first := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/playwright/install", map[string]any{}, headers)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: expected 201, got %d: %s", first.Code, first.Body.String())
	}
	second := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/playwright/install", map[string]any{}, headers)
	if second.Code != http.StatusCreated {
		t.Fatalf("second: expected 201, got %d: %s", second.Code, second.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(second.Body.Bytes(), &body)
	if len(body.Data) != 0 {
		t.Fatalf("第二次提交时全部都在排队 / 安装中，不应再入队，实际 %d 项", len(body.Data))
	}

	var total int64
	database.DB.Model(&model.Dependency{}).Count(&total)
	if total != 27 {
		t.Fatalf("两次提交后应只有 26 个系统库 + 1 个 Python 共 27 条记录，实际 %d 条", total)
	}

	close(release)
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("等待安装队列结束超时")
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 27 {
		t.Fatalf("每条记录只应安装一次，实际调用 %d 次", count)
	}
}

// 登记到一半建记录失败（例如 SQLite busy）：回 500、不起安装协程，这一次新建的 queued 记录必须删干净，
// 否则它们没人接手，删除、重装、取消、再点一键安装又都拒绝 queued，只能卡着。复用的旧记录原样保留。
func TestInstallPlaywrightRollsBackCreatedRecordsOnError(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubPlaywrightHandler(t, supportedPlaywrightPlan("/data/deps/ms-playwright"), nil, nil)
	stubDependencyRunner(t, func(uint, string, string) {
		t.Error("登记失败时不应开始任何安装")
	})

	packages := service.PlaywrightDebian12Packages()
	reused := mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: packages[0], Status: model.DepStatusFailed, Log: "E: 旧的失败日志"})

	// 让第 3 次新建依赖记录失败：前两次（packages[1]、packages[2]）已经真的落库成 queued。
	creates := 0
	if err := database.DB.Callback().Create().Before("gorm:create").Register("test:fail_third_dependency_create", func(tx *gorm.DB) {
		if tx.Statement.Table != "dependencies" {
			return
		}
		creates++
		if creates == 3 {
			tx.AddError(errors.New("database is locked"))
		}
	}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps/playwright/install", map[string]any{}, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
	if creates != 3 {
		t.Fatalf("应在第 3 次新建时出错，实际新建了 %d 次", creates)
	}

	var remaining []model.Dependency
	database.DB.Find(&remaining)
	if len(remaining) != 1 || remaining[0].ID != reused.ID {
		t.Fatalf("出错后只应剩下原有的那条记录，新建的都要删掉，实际 %+v", remaining)
	}
	if remaining[0].Status != model.DepStatusFailed || remaining[0].Log != reused.Log {
		t.Fatalf("复用的旧记录不应被改动，实际 %+v", remaining[0])
	}
}

// POST /deps 抽出公共查重函数后，failed / cancelled 的同名记录仍然不挡新提交（与重构前一致）。
func TestDependencyCreateStillAllowsResubmittingFailedName(t *testing.T) {
	testutil.SetupTestEnv(t)
	stubDependencyRunner(t, func(uint, string, string) {})
	mustCreateDependency(t, model.Dependency{Type: model.DepTypeLinux, Name: "curl", Status: model.DepStatusFailed})

	engine := newDepsTestRouter()
	token := testutil.MustCreateAccessToken(t, "admin", "admin")
	rec := performDepsJSONRequest(engine, http.MethodPost, "/api/v1/deps", map[string]any{
		"type":  model.DepTypeLinux,
		"names": []string{"curl"},
	}, map[string]string{"Authorization": "Bearer " + token})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var count int64
	database.DB.Model(&model.Dependency{}).Where("type = ? AND name = ?", model.DepTypeLinux, "curl").Count(&count)
	if count != 2 {
		t.Fatalf("failed 的同名记录不挡新提交，期望 2 条，实际 %d 条", count)
	}
}

// ---------- runCmdWithSSEThen：主命令成功后在同一条记录里接着跑后续步骤 ----------

func testShellCommand(script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", script)
	}
	return exec.Command("sh", "-c", script)
}

// testLongRunningCommand 是一个会跑 30 秒左右、且自己就是直接子进程的命令：
// Windows 上 KillProcessGroup 只杀直接子进程，套一层 cmd /c 的话孙进程会一直占着输出管道。
func testLongRunningCommand() *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("ping", "-n", "30", "127.0.0.1")
	}
	return exec.Command("sleep", "30")
}

func setupRunCmdTest(t *testing.T) model.Dependency {
	t.Helper()
	testutil.SetupTestEnv(t)
	old := dependencySnapshotFunc
	t.Cleanup(func() { dependencySnapshotFunc = old })
	dependencySnapshotFunc = func() {}
	return mustCreateDependency(t, model.Dependency{Type: model.DepTypePython, Name: "playwright", PythonVersion: "3.12", Status: model.DepStatusInstalling})
}

func reloadDependency(t *testing.T, id uint) model.Dependency {
	t.Helper()
	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	return dep
}

func downloadStep(script string, built chan<- struct{}) depFollowUpStep {
	return depFollowUpStep{
		build: func() (*exec.Cmd, string, error) {
			if built != nil {
				close(built)
			}
			return testShellCommand(script), service.PlaywrightDownloadStartLine("/data/deps/ms-playwright"), nil
		},
		doneLine: service.PlaywrightDownloadReadyLine,
	}
}

func TestRunCmdWithSSEThenRunsFollowUpInSameRecord(t *testing.T) {
	dep := setupRunCmdTest(t)

	runCmdWithSSEThen(testShellCommand("echo pip-ok"), dep.ID, model.DepStatusInstalled, false,
		[]depFollowUpStep{downloadStep("echo download-ok", nil)})

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusInstalled {
		t.Fatalf("两段都成功应记为 installed，实际 %s，日志：\n%s", got.Status, got.Log)
	}
	previous := -1
	for _, want := range []string{
		dependencyRunStartMarker,
		"pip-ok",
		service.PlaywrightDownloadStartPrefix + "/data/deps/ms-playwright",
		"download-ok",
		service.PlaywrightDownloadReadyLine,
	} {
		index := strings.Index(got.Log, want)
		if index < 0 || index <= previous {
			t.Fatalf("日志里应按顺序出现 %q，实际：\n%s", want, got.Log)
		}
		previous = index
	}
}

func TestRunCmdWithSSEThenFailsRecordWhenFollowUpFails(t *testing.T) {
	dep := setupRunCmdTest(t)

	runCmdWithSSEThen(testShellCommand("echo pip-ok"), dep.ID, model.DepStatusInstalled, false,
		[]depFollowUpStep{downloadStep("exit 5", nil)})

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusFailed {
		t.Fatalf("下载失败时整条记录应记为 failed，实际 %s", got.Status)
	}
	if strings.Contains(got.Log, service.PlaywrightDownloadReadyLine) {
		t.Fatalf("下载失败时不应写就绪行：\n%s", got.Log)
	}
	if !strings.Contains(got.Log, "Playwright 浏览器下载失败") {
		t.Fatalf("应追加浏览器下载失败的提示：\n%s", got.Log)
	}
}

func TestRunCmdWithSSEThenSkipsFollowUpWhenMainFails(t *testing.T) {
	dep := setupRunCmdTest(t)

	built := false
	runCmdWithSSEThen(testShellCommand("exit 3"), dep.ID, model.DepStatusInstalled, false,
		[]depFollowUpStep{{
			build: func() (*exec.Cmd, string, error) {
				built = true
				return testShellCommand("echo never"), "never", nil
			},
		}})

	got := reloadDependency(t, dep.ID)
	if built {
		t.Fatal("pip 失败时不应再去构造下载命令")
	}
	if got.Status != model.DepStatusFailed {
		t.Fatalf("主命令失败应记为 failed，实际 %s", got.Status)
	}
	if strings.Contains(got.Log, "Playwright 浏览器下载失败") {
		t.Fatalf("pip 就失败了，不能说成浏览器下载失败：\n%s", got.Log)
	}
}

func TestRunCmdWithSSEThenRecordsBuildError(t *testing.T) {
	dep := setupRunCmdTest(t)

	runCmdWithSSEThen(testShellCommand("echo pip-ok"), dep.ID, model.DepStatusInstalled, false,
		[]depFollowUpStep{{
			build: func() (*exec.Cmd, string, error) {
				return nil, "", errors.New("[Playwright] 找不到 Python 3.12 的面板托管解释器")
			},
		}})

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusFailed || !strings.Contains(got.Log, "找不到 Python 3.12 的面板托管解释器") {
		t.Fatalf("构造下载命令失败应记为 failed 并写明原因，实际 %s：\n%s", got.Status, got.Log)
	}
}

// 取消必须覆盖第二段下载：用户在弹窗里点取消时，正在下载的浏览器进程要被杀掉，记录记为 cancelled。
func TestRunCmdWithSSEThenCancelCoversFollowUp(t *testing.T) {
	dep := setupRunCmdTest(t)

	built := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runCmdWithSSEThen(testShellCommand("echo pip-ok"), dep.ID, model.DepStatusInstalled, false,
			[]depFollowUpStep{{
				build: func() (*exec.Cmd, string, error) {
					close(built)
					return testLongRunningCommand(), service.PlaywrightDownloadStartLine("/data/deps/ms-playwright"), nil
				},
				doneLine: service.PlaywrightDownloadReadyLine,
			}})
	}()

	select {
	case <-built:
	case <-time.After(10 * time.Second):
		t.Fatal("等待进入下载步骤超时")
	}
	if !cancelDepOperation(dep.ID) {
		t.Fatal("下载步骤期间应仍能找到这条记录的取消函数")
	}

	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("取消后下载进程没有被终止")
	}
	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusCancelled {
		t.Fatalf("取消后应记为 cancelled，实际 %s：\n%s", got.Status, got.Log)
	}
	if !strings.Contains(got.Log, "[依赖任务已取消]") || strings.Contains(got.Log, service.PlaywrightDownloadReadyLine) {
		t.Fatalf("日志应写明已取消且没有就绪行：\n%s", got.Log)
	}
}

// 不带后续步骤时与改动前的 runCmdWithSSE 一致：成功记 installed；deleteOnSuccess 时删记录。
func TestRunCmdWithSSEWithoutFollowUpsKeepsBehavior(t *testing.T) {
	dep := setupRunCmdTest(t)
	runCmdWithSSE(testShellCommand("echo ok"), dep.ID, model.DepStatusInstalled, false)
	if got := reloadDependency(t, dep.ID); got.Status != model.DepStatusInstalled || !strings.Contains(got.Log, "ok") {
		t.Fatalf("成功应记为 installed，实际 %s：\n%s", got.Status, got.Log)
	}

	runCmdWithSSE(testShellCommand("echo removed"), dep.ID, model.DepStatusInstalled, true)
	var count int64
	database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Count(&count)
	if count != 0 {
		t.Fatal("deleteOnSuccess 时成功后应删除记录")
	}
}

func TestNewPlaywrightBrowserDownloadStepUsesInjectedCommand(t *testing.T) {
	old := playwrightBrowserInstallCommandFunc
	t.Cleanup(func() { playwrightBrowserInstallCommandFunc = old })
	var askedVersion string
	playwrightBrowserInstallCommandFunc = func(version string) (*exec.Cmd, string, error) {
		askedVersion = version
		return testShellCommand("echo x"), "/data/deps/ms-playwright", nil
	}

	step := newPlaywrightBrowserDownloadStep("3.11")
	cmd, startLine, err := step.build()
	if err != nil || cmd == nil {
		t.Fatalf("build 失败：%v", err)
	}
	if askedVersion != "3.11" {
		t.Fatalf("应按依赖记录的 Python 版本构造下载命令，实际 %q", askedVersion)
	}
	// 期望值改用 PlaywrightDownloadStartLine 组装：v3.3.2 起这一行后面还跟着体量与耗时说明（issue #146），
	// 断言的意思不变 —— startLine 必须就是那条下载开始行。
	if startLine != service.PlaywrightDownloadStartLine("/data/deps/ms-playwright") || step.doneLine != service.PlaywrightDownloadReadyLine {
		t.Fatalf("前后日志行不对：start=%q done=%q", startLine, step.doneLine)
	}
}

// ---------- Chromium 下载全进程串行（playwrightBrowserDownloadSem） ----------

// TestPlaywrightDownloadHelperProcess 不是真正的用例：下面的用例把测试二进制自己当成「下载命令」起子进程，
// 它一直等到环境变量指定的文件出现才退出，用例就能精确控制「第一条下载什么时候结束」，不靠 sleep 猜时序。
func TestPlaywrightDownloadHelperProcess(t *testing.T) {
	path := os.Getenv("DAIDAI_TEST_WAIT_FOR_FILE")
	if path == "" {
		return
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			os.Exit(0)
		}
		time.Sleep(20 * time.Millisecond)
	}
	os.Exit(1)
}

func helperWaitForFileCommand(path string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestPlaywrightDownloadHelperProcess$")
	cmd.Env = append(os.Environ(), "DAIDAI_TEST_WAIT_FOR_FILE="+path)
	return cmd
}

// waitForDependencyLog 轮询库里的日志直到出现 want：排队提示行是立刻落库的，不用等任务结束。
func waitForDependencyLog(t *testing.T, id uint, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var dep model.Dependency
		if err := database.DB.Select("log").First(&dep, id).Error; err == nil && strings.Contains(dep.Log, want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待日志出现 %q 超时，当前：\n%s", want, reloadDependency(t, id).Log)
}

// all 镜像上一次 POST /deps 会给每个 Python 版本各起一条 playwright 安装，pip 装完几乎同时进入下载，
// 下到同一个浏览器目录，抢 Playwright 的 __dirlock 抢输的只重试约 8 分钟就 failed。
// 下载步骤必须全进程串行：第二条要等第一条的下载命令结束之后才开始构造命令，等待时写一行排队提示。
func TestPlaywrightBrowserDownloadRunsOneAtATime(t *testing.T) {
	first := setupRunCmdTest(t)
	second := mustCreateDependency(t, model.Dependency{Type: model.DepTypePython, Name: "playwright", PythonVersion: "3.11", Status: model.DepStatusInstalling})

	releaseFirst := filepath.Join(t.TempDir(), "release-first")
	var (
		mu     sync.Mutex
		events []string
	)
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	firstBuilt := make(chan struct{})
	old := playwrightBrowserInstallCommandFunc
	t.Cleanup(func() { playwrightBrowserInstallCommandFunc = old })
	playwrightBrowserInstallCommandFunc = func(version string) (*exec.Cmd, string, error) {
		record("build " + version)
		if version == "3.12" {
			close(firstBuilt)
			return helperWaitForFileCommand(releaseFirst), "/data/deps/ms-playwright", nil
		}
		return testShellCommand("echo download-ok"), "/data/deps/ms-playwright", nil
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		runCmdWithSSEThen(testShellCommand("echo pip-ok"), first.ID, model.DepStatusInstalled, false,
			[]depFollowUpStep{newPlaywrightBrowserDownloadStep("3.12")})
	}()
	select {
	case <-firstBuilt:
	case <-time.After(10 * time.Second):
		t.Fatal("等待第一条进入下载步骤超时")
	}

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		runCmdWithSSEThen(testShellCommand("echo pip-ok"), second.ID, model.DepStatusInstalled, false,
			[]depFollowUpStep{newPlaywrightBrowserDownloadStep("3.11")})
	}()
	waitForDependencyLog(t, second.ID, playwrightBrowserDownloadWaitLine)

	mu.Lock()
	builtSecondEarly := slices.Contains(events, "build 3.11")
	mu.Unlock()
	if builtSecondEarly {
		t.Fatal("第一条还在下载时，第二条不应开始构造下载命令")
	}

	if err := os.WriteFile(releaseFirst, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, done := range map[string]chan struct{}{"first": firstDone, "second": secondDone} {
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: 等待下载结束超时", name)
		}
	}

	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if !slices.Equal(got, []string{"build 3.12", "build 3.11"}) {
		t.Fatalf("应先构造第一条、第一条下载结束后才构造第二条，实际事件 %v", got)
	}
	for _, dep := range []model.Dependency{first, second} {
		if reloaded := reloadDependency(t, dep.ID); reloaded.Status != model.DepStatusInstalled ||
			!strings.Contains(reloaded.Log, service.PlaywrightDownloadReadyLine) {
			t.Fatalf("两条最终都应下载成功，id=%d 实际 %s：\n%s", dep.ID, reloaded.Status, reloaded.Log)
		}
	}
	if strings.Contains(reloadDependency(t, first.ID).Log, playwrightBrowserDownloadWaitLine) {
		t.Fatal("第一条没有排队，不应写排队提示")
	}
	assertPlaywrightDownloadSlotFree(t)
}

// 排队中的记录必须能被取消：点取消后立刻结束、记为 cancelled，不去构造下载命令，也不占着槽位。
func TestPlaywrightBrowserDownloadWaitIsCancellable(t *testing.T) {
	dep := setupRunCmdTest(t)

	// 模拟另一条记录正占着下载槽位。
	playwrightBrowserDownloadSem <- struct{}{}
	t.Cleanup(func() { <-playwrightBrowserDownloadSem })

	old := playwrightBrowserInstallCommandFunc
	t.Cleanup(func() { playwrightBrowserInstallCommandFunc = old })
	playwrightBrowserInstallCommandFunc = func(string) (*exec.Cmd, string, error) {
		t.Error("取消了的排队记录不应再构造下载命令")
		return testShellCommand("echo never"), "/data/deps/ms-playwright", nil
	}

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		runCmdWithSSEThen(testShellCommand("echo pip-ok"), dep.ID, model.DepStatusInstalled, false,
			[]depFollowUpStep{newPlaywrightBrowserDownloadStep("3.12")})
	}()
	waitForDependencyLog(t, dep.ID, playwrightBrowserDownloadWaitLine)

	if !cancelDepOperation(dep.ID) {
		t.Fatal("排队等待期间应仍能找到这条记录的取消函数")
	}
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("取消后排队中的记录没有结束")
	}

	got := reloadDependency(t, dep.ID)
	if got.Status != model.DepStatusCancelled {
		t.Fatalf("排队中被取消应记为 cancelled，实际 %s：\n%s", got.Status, got.Log)
	}
	if !strings.Contains(got.Log, "[依赖任务已取消，后续步骤未执行]") || strings.Contains(got.Log, service.PlaywrightDownloadStartPrefix) {
		t.Fatalf("日志应写明已取消、且没有下载开始行：\n%s", got.Log)
	}
	// 槽位仍归「另一条记录」：被取消的这条不能顺手把别人的槽位放掉，也不能自己占着不还。
	select {
	case playwrightBrowserDownloadSem <- struct{}{}:
		<-playwrightBrowserDownloadSem
		t.Fatal("被取消的排队记录不应放掉别人占着的槽位")
	default:
	}
}

// assertPlaywrightDownloadSlotFree 确认下载槽位已经还回来了，否则之后所有 playwright 下载都会永远排队。
func assertPlaywrightDownloadSlotFree(t *testing.T) {
	t.Helper()
	select {
	case playwrightBrowserDownloadSem <- struct{}{}:
		<-playwrightBrowserDownloadSem
	default:
		t.Fatal("下载结束后必须释放下载槽位")
	}
}

// ---------- buildDependencyFailureHint 的 Playwright 分支 ----------

const testRunStartLine = dependencyRunStartMarker + "20m0s，可在「系统设置 - 任务运行 - 依赖安装超时(分钟)」调整]"

func TestBuildDependencyFailureHintPlaywrightDownload(t *testing.T) {
	// 本次运行 pip 中途重试过 DNS / 连接超时但最终装上了，失败发生在下载阶段：
	// 必须归到浏览器下载，不能被 DNS / 镜像源分支抢走、把人引去查 pip 镜像。
	downloadFailed := testRunStartLine + "\n" +
		"WARNING: Retrying after connection broken by 'NewConnectionError: Temporary failure in name resolution'\n" +
		"Connection timed out\n" +
		"Successfully installed playwright-1.47.0\n" +
		service.PlaywrightDownloadStartLine("/app/Dumb-Panel/deps/ms-playwright") + "\n" +
		"Error: getaddrinfo EAI_AGAIN playwright.download.prss.microsoft.com"

	got := buildDependencyFailureHint(downloadFailed)
	// 要讲清楚失败的是浏览器下载、不是 pip，并给出代理与下载镜像两条出路。
	for _, want := range []string{"Playwright 浏览器下载失败", "pip 包 playwright 已经装好", "代理", "PLAYWRIGHT_DOWNLOAD_HOST"} {
		if !strings.Contains(got, want) {
			t.Fatalf("提示应包含 %q，实际 %q", want, got)
		}
	}
	if strings.Contains(got, "DNS") || strings.Contains(got, "镜像源不可达") {
		t.Fatalf("下载阶段失败不能归到 DNS / 镜像源分支，实际 %q", got)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") || strings.Contains(got, "\n") {
		t.Fatalf("提示应与其它归因一样是单行方括号文案，实际 %q", got)
	}

	// 上一轮下载失败的日志还留在前面，这一轮 pip 就失败了：只看本次运行那一段，不能误报成下载失败。
	pipFailedAfterOldDownload := downloadFailed + "\n" + buildPlaywrightDownloadFailureHint() + "\n" +
		"[Playwright 一键安装] 已加入顺序队列（1/1）\n" +
		testRunStartLine + "\n" +
		"ERROR: Could not find a version that satisfies the requirement playwright"
	if got := buildDependencyFailureHint(pipFailedAfterOldDownload); strings.Contains(got, "Playwright 浏览器下载失败") {
		t.Fatalf("本轮失败在 pip 阶段，不能套用上一轮的下载失败结论，实际 %q", got)
	}

	// 下载成功（有就绪行）后别的原因失败，也不算下载失败。
	downloadOK := testRunStartLine + "\n" +
		service.PlaywrightDownloadStartLine("/data") + "\n" + service.PlaywrightDownloadReadyLine + "\n" + "Connection timed out"
	if got := buildDependencyFailureHint(downloadOK); strings.Contains(got, "Playwright 浏览器下载失败") {
		t.Fatalf("有就绪行时不是下载失败，实际 %q", got)
	}
}

// 下载撞上浏览器目录的安装锁（Playwright 的 __dirlock）不是网络问题：要说清楚是另一个 playwright install
// 进程占着目录，不能把人引去配代理 / PLAYWRIGHT_DOWNLOAD_HOST。面板内的下载已串行，
// 占用者只可能在面板外，所以提示里不能再出现「另一条记录正在并行下载」这种指向依赖列表的说法。
func TestBuildDependencyFailureHintPlaywrightDirLock(t *testing.T) {
	lockFailed := testRunStartLine + "\n" +
		"Successfully installed playwright-1.47.0\n" +
		service.PlaywrightDownloadStartLine("/data/deps/ms-playwright") + "\n" +
		"Error: \n╔════════════════════════════╗\n" +
		"║ An active lockfile is found at:\n" +
		"║   /data/deps/ms-playwright/__dirlock\n" +
		"║ - wait a few minutes if other Playwright is installing browsers in parallel\n"

	got := buildDependencyFailureHint(lockFailed)
	if !strings.Contains(got, "Playwright 浏览器下载失败") || !strings.Contains(got, "playwright install 进程占用") {
		t.Fatalf("撞上安装锁应提示目录被另一个 playwright install 进程占用，实际 %q", got)
	}
	// 面板内的下载已串行，旧说法会让人去依赖列表里找一条并不存在的「并行记录」。
	if strings.Contains(got, "另一条记录正在并行下载") {
		t.Fatalf("撞上安装锁时不应再说另一条记录在并行下载，实际 %q", got)
	}
	if strings.Contains(got, "系统设置 → 代理设置") || strings.Contains(got, "添加 PLAYWRIGHT_DOWNLOAD_HOST") {
		t.Fatalf("撞上安装锁时不应引导去配代理或下载镜像，实际 %q", got)
	}
	if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") || strings.Contains(got, "\n") {
		t.Fatalf("提示应是单行方括号文案，实际 %q", got)
	}

	// 锁字样只出现在 pip 阶段（下载开始行之前）时不算：真正的下载失败原因仍走原来的代理提示。
	pipMentionsLock := testRunStartLine + "\n" +
		"Collecting lockfile\n" +
		service.PlaywrightDownloadStartLine("/data/deps/ms-playwright") + "\n" +
		"Error: getaddrinfo EAI_AGAIN playwright.download.prss.microsoft.com"
	if got := buildDependencyFailureHint(pipMentionsLock); strings.Contains(got, "playwright install 进程占用") ||
		!strings.Contains(got, "PLAYWRIGHT_DOWNLOAD_HOST") {
		t.Fatalf("锁字样不在下载阶段时应仍给代理 / 下载镜像提示，实际 %q", got)
	}
}
