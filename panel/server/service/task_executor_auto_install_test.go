package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// issue #154 的任务级回归：Alpine 上任务运行时自动安装 playwright 必然失败（PyPI 上没有 musl 能用的包）。
//
// 走真实的 runTask → detectAndInstallDeps → InstallAutoDependency → 托管 venv 的 pip3，只换掉两样东西：
//   - 脚本进程：runCommandWithPlanFunc 换成桩，吐出 ModuleNotFoundError 并以 1 退出；
//   - 托管 venv 里的 python / pip3：假可执行文件，pip3 install 吐出 pip 在 musl 上的原文并以 1 退出。
//
// Alpine 环境从 os-release 注入，提示走真实的 playwrightHintEnvFunc。
// 本机是 Windows、CI 是 Linux，假可执行文件分别写 cmd 版和 sh 版（writeFakeExecutable 按平台加后缀）。
func TestRunTaskAutoInstallFailureOnAlpineKeepsLinesApartAndHintsDebian(t *testing.T) {
	testutil.SetupTestEnv(t)
	// PATH 上一个 Python 都不留：版本探测停在默认的 3.12，宿主机的真 pip 也不会被拉起来联网。
	isolatePythonProbePath(t)

	// 健康的托管 venv —— Alpine 镜像上自动安装走的就是它的 pip3。
	// 健康检查会让 python 打印 major.minor（-c 那条），再问 pip3 --version。
	venvBin := resolveManagedVenvBin(ManagedPythonVenvDir("3.12"))
	pipLines := []string{
		`if [ "$1" = "--version" ]; then echo "pip 24.0 from test"; exit 0; fi`,
		`echo "Looking in indexes: https://mirrors.cloud.tencent.com/pypi/simple"`,
		`echo "ERROR: Could not find a version that satisfies the requirement playwright (from versions: none)"`,
		`echo "ERROR: No matching distribution found for playwright"`,
		`exit 1`,
	}
	if runtime.GOOS == "windows" {
		// cmd 的 if 块里不能出现未转义的括号，所以用 goto 分流；块外的 echo 可以原样带括号。
		pipLines = []string{
			`if "%~1"=="--version" goto version`,
			`echo Looking in indexes: https://mirrors.cloud.tencent.com/pypi/simple`,
			`echo ERROR: Could not find a version that satisfies the requirement playwright (from versions: none)`,
			`echo ERROR: No matching distribution found for playwright`,
			`exit /b 1`,
			`:version`,
			`echo pip 24.0 from test`,
			`exit /b 0`,
		}
	}
	writeFakeExecutable(t, venvBin, "python", []string{"echo 3.12"})
	writeFakeExecutable(t, venvBin, "pip3", pipLines)

	// Alpine 容器（Linux、在容器里、不是面具版），架构钉成 amd64，免得随宿主机变。
	oldOSRelease, oldArch := linuxOSReleaseFiles, playwrightGOARCH
	t.Cleanup(func() { linuxOSReleaseFiles, playwrightGOARCH = oldOSRelease, oldArch })
	osRelease := filepath.Join(t.TempDir(), "os-release")
	if err := os.WriteFile(osRelease, []byte("ID=alpine\nVERSION_ID=3.23.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linuxOSReleaseFiles = []string{osRelease}
	playwrightGOARCH = "amd64"
	stubPlaywrightDeployment(t, "linux", true, false)

	traceback := "Traceback (most recent call last):\n" +
		"  File \"/app/Dumb-Panel/scripts/pw_demo.py\", line 1, in <module>\n" +
		"    from playwright.sync_api import sync_playwright\n" +
		"ModuleNotFoundError: No module named 'playwright'\n"
	previous := runCommandWithPlanFunc
	runCommandWithPlanFunc = func(plan *CommandExecutionPlan, timeout int, envVars map[string]string, maxLogSize int, onOutput OnOutputFunc, onProcessStart ...OnProcessStartFunc) (*ScriptResult, *os.Process, error) {
		onOutput(traceback)
		return &ScriptResult{ReturnCode: 1}, nil, nil
	}
	t.Cleanup(func() { runCommandWithPlanFunc = previous })

	// max_retries=1：第二轮会再撞上同一个缺失模块，原来这一轮会误报「已安装但仍然报错」。
	task := &model.Task{
		Name:       "alpine-playwright-auto-install",
		Command:    "python3 pw_demo.py",
		TaskType:   model.TaskTypeManual,
		Status:     model.TaskStatusRunning,
		MaxRetries: 1,
	}
	if err := database.DB.Create(task).Error; err != nil {
		t.Fatalf("create task: %v", err)
	}
	runningStatus := model.LogStatusRunning
	taskLog := &model.TaskLog{TaskID: task.ID, Status: &runningStatus, StartedAt: time.Now()}
	if err := database.DB.Create(taskLog).Error; err != nil {
		t.Fatalf("create task log: %v", err)
	}
	tinyLog, err := NewTinyLog("alpine-playwright-auto-install")
	if err != nil {
		t.Fatalf("create tiny log: %v", err)
	}
	plan := &CommandExecutionPlan{
		Interpreter: "python3",
		FullPath:    filepath.Join(config.C.Data.ScriptsDir, "pw_demo.py"),
		Mode:        commandModeNormal,
	}
	req := &ExecutionRequest{TaskID: task.ID, Task: task, TaskLogID: taskLog.ID, CommandPlan: plan}
	NewTaskExecutor().runTask(req, taskLog, tinyLog)

	var stored model.TaskLog
	if err := database.DB.First(&stored, taskLog.ID).Error; err != nil {
		t.Fatalf("reload task log: %v", err)
	}
	content, err := DecompressFromBase64(stored.Content)
	if err != nil {
		t.Fatalf("decompress task log: %v", err)
	}
	// Windows 上 cmd 的 echo 输出 CRLF，按行比对前统一成 \n。
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	hasLine := func(want string) bool {
		for _, line := range lines {
			if line == want {
				return true
			}
		}
		return false
	}

	// 1. 面板打的行各占一行：没有「…][安装失败:」「…]=== 执行结束」这种粘行。
	if strings.Contains(content, "][") || strings.Contains(content, "]===") {
		t.Fatalf("面板行粘在了一起：\n%s", content)
	}
	for _, want := range []string{
		"[检测到缺失依赖: playwright，正在自动安装...]",
		"[安装失败: Looking in indexes: https://mirrors.cloud.tencent.com/pypi/simple",
		"ERROR: No matching distribution found for playwright]",
		"[第 1 次重试，等待 0 秒]",
	} {
		if !hasLine(want) {
			t.Fatalf("日志里应有独立的一行 %q，实际：\n%s", want, content)
		}
	}
	if !strings.HasPrefix(lines[len(lines)-1], "=== 执行结束") {
		t.Fatalf("最后一行应是结束横幅，实际 %q，完整日志：\n%s", lines[len(lines)-1], content)
	}

	// 2. 重试轮如实说「装过一次并失败」，不再误报「已安装但仍然报错」，也不再白跑一遍 pip。
	if strings.Contains(content, "已安装但仍然报错") {
		t.Fatalf("playwright 从没装上过，不应报「已安装但仍然报错」：\n%s", content)
	}
	if !hasLine("[安装失败: playwright 本次执行已自动安装过一次并失败，不再重复安装，原因见上方日志]") {
		t.Fatalf("重试轮应说明已装过一次并失败，实际：\n%s", content)
	}
	if n := strings.Count(content, "Looking in indexes:"); n != 1 {
		t.Fatalf("同一次执行里 pip 只应跑一次，实际跑了 %d 次：\n%s", n, content)
	}

	// 3. 装失败不建依赖记录。
	var depCount int64
	database.DB.Model(&model.Dependency{}).Count(&depCount)
	if depCount != 0 {
		t.Fatalf("装失败不应建依赖记录，实际 %d 条", depCount)
	}

	// 4. 每轮失败后都有一行 Alpine 提示，给出按标签换 Debian 版镜像的出路（latest → debian、latest-full → debian-full）。
	hints := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "[提示] 当前系统是 Alpine") {
			hints++
			if !strings.Contains(line, "latest → debian") {
				t.Fatalf("容器部署的 Alpine 提示应给出换 Debian 版镜像标签的出路，实际 %q", line)
			}
		}
	}
	if hints != 2 {
		t.Fatalf("两轮失败各应有一行 Alpine 提示，实际 %d 行：\n%s", hints, content)
	}
}
