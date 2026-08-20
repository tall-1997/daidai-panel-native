package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMagiskShellVersion(t *testing.T) {
	cases := []struct {
		raw  string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"1", 1},
		{" 2 ", 2},
		{"abc", 0},
		{"-3", 0},
	}

	for _, tc := range cases {
		t.Setenv(magiskShellVersionEnv, tc.raw)
		if got := resolveMagiskShellVersion(); got != tc.want {
			t.Fatalf("DAIDAI_MAGISK_SHELL_VERSION=%q 应解析为 %d，实际 %d", tc.raw, tc.want, got)
		}
	}
}

func TestMagiskModuleVersionCodeMatchesReleasePreflight(t *testing.T) {
	// 算法必须与 scripts/release-preflight.ps1 一致：major*10000 + minor*100 + patch。
	cases := []struct {
		version string
		want    int
		ok      bool
	}{
		{"v3.0.3", 30003, true},
		{"3.0.3", 30003, true},
		{"v2.3.10", 20310, true},
		{"v10.20.30", 102030, true},
		{"v3.0", 0, false},
		{"v3.0.x", 0, false},
		{"", 0, false},
	}

	for _, tc := range cases {
		got, ok := magiskModuleVersionCode(tc.version)
		if ok != tc.ok {
			t.Fatalf("%q 的可解析性应为 %v，实际 %v", tc.version, tc.ok, ok)
		}
		if ok && got != tc.want {
			t.Fatalf("%q 的 versionCode 应为 %d，实际 %d", tc.version, tc.want, got)
		}
	}
}

func TestRewriteMagiskModulePropOnlyTouchesVersionLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "module.prop")
	original := strings.Join([]string{
		"id=daidai-panel",
		"name=呆呆面板 (Daidai Panel)",
		"version=v3.0.2",
		"versionCode=30002",
		"author=linzixuanzz",
		"description=在 Android 设备上以 root 权限运行呆呆面板",
		"updateJson=https://example.com/update-debian.json",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("准备 module.prop 失败: %v", err)
	}

	if err := rewriteMagiskModuleProp(path, "3.0.3"); err != nil {
		t.Fatalf("改写 module.prop 失败: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 module.prop 失败: %v", err)
	}
	text := string(raw)

	if !strings.Contains(text, "version=v3.0.3") {
		t.Fatalf("version 行未更新:\n%s", text)
	}
	if !strings.Contains(text, "versionCode=30003") {
		t.Fatalf("versionCode 行未更新:\n%s", text)
	}
	// updateJson 在 debian flavor 里与 alpine 不同，整体重写会把它抹平，必须原样保留。
	if !strings.Contains(text, "updateJson=https://example.com/update-debian.json") {
		t.Fatalf("updateJson 行被破坏:\n%s", text)
	}
	if !strings.Contains(text, "id=daidai-panel") || !strings.Contains(text, "author=linzixuanzz") {
		t.Fatalf("其余字段被破坏:\n%s", text)
	}
	if strings.Contains(text, "v3.0.2") || strings.Contains(text, "30002") {
		t.Fatalf("仍残留旧版本号:\n%s", text)
	}
}

func TestReplaceDirAtomicallyDropsStaleFiles(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	target := filepath.Join(root, "web")

	if err := os.MkdirAll(filepath.Join(source, "assets"), 0o755); err != nil {
		t.Fatalf("准备源目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("new"), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "assets", "app-new.js"), []byte("new"), 0o644); err != nil {
		t.Fatalf("写入源文件失败: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(target, "assets"), 0o755); err != nil {
		t.Fatalf("准备目标目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "assets", "app-old.js"), []byte("old"), 0o644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}

	if err := replaceDirAtomically(source, target); err != nil {
		t.Fatalf("替换目录失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "assets", "app-new.js")); err != nil {
		t.Fatalf("新产物缺失: %v", err)
	}
	// 带 hash 的旧产物必须被清掉，否则每升级一次前端目录就膨胀一轮。
	if _, err := os.Stat(filepath.Join(target, "assets", "app-old.js")); err == nil {
		t.Fatal("旧产物没有被清理，说明目录是叠加而不是整体替换")
	}
	for _, leftover := range []string{target + ".new", target + ".old"} {
		if _, err := os.Stat(leftover); err == nil {
			t.Fatalf("临时目录未清理: %s", leftover)
		}
	}
}

// helper 脚本里有三条不能丢的约束，任意一条被改掉都会让模块版升级后起不来。
func TestMagiskUpdateHelperScriptKeepsCriticalConstraints(t *testing.T) {
	workDir := t.TempDir()
	plan := &panelUpdatePlan{
		DeploymentType: panelUpdateDeploymentMagisk,
		BinaryName:     "daidai-linux-arm64",
		WebDir:         "/app/web",
		DataDir:        "/app/Dumb-Panel",
		CurrentPID:     4321,
		// 面板自己发起升级的场景：调用进程就是面板进程。
		ServerPID: 4321,
	}

	scriptPath, err := writeMagiskUpdateHelperScript(plan, "/tmp/src", workDir)
	if err != nil {
		t.Fatalf("生成 helper 脚本失败: %v", err)
	}

	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取 helper 脚本失败: %v", err)
	}
	script := string(raw)

	// 1. 启动路径必须仍是 /usr/local/bin/daidai-server，
	//    否则 Magisk/service.sh 的 `pgrep -f /usr/local/bin/daidai-server` 去重失效，
	//    下次开机会再拉起一个实例抢同一个端口。
	if !strings.Contains(script, "TARGET_BIN='/usr/local/bin/daidai-server'") {
		t.Fatalf("helper 脚本没有把目标程序固定在 /usr/local/bin/daidai-server:\n%s", script)
	}

	// 2. 必须先写 .new 再 mv，不能 cp 直接覆盖正在执行的文件（ETXTBSY）。
	if !strings.Contains(script, `mv -f "$TARGET_BIN.new" "$TARGET_BIN"`) {
		t.Fatalf("helper 脚本没有用 rename 覆盖二进制:\n%s", script)
	}

	// 3. 启动前必须 cd 到数据目录，否则 appboot.ResolveConfigPath 找不到 config.yaml，
	//    main.go 会直接 log.Fatalf，而模块版没有守护进程会把它拉起来。
	cdIndex := strings.Index(script, `cd "$DATA_DIR"`)
	startIndex := strings.Index(script, `nohup "$TARGET_BIN"`)
	if cdIndex < 0 || startIndex < 0 || cdIndex > startIndex {
		t.Fatalf("helper 脚本必须先 cd 到数据目录再启动面板:\n%s", script)
	}

	if !strings.Contains(script, "DATA_DIR='/app/Dumb-Panel'") {
		t.Fatalf("helper 脚本的数据目录不正确:\n%s", script)
	}
	if !strings.Contains(script, "PID=4321") {
		t.Fatalf("helper 脚本没有携带待退出的旧进程 PID:\n%s", script)
	}
	// 升级窗口哨兵必须在启动新进程前删掉，否则 service.sh 的存活守护会一直不敢接管。
	if !strings.Contains(script, `rm -f "$SENTINEL"`) {
		t.Fatalf("helper 脚本没有清理升级哨兵:\n%s", script)
	}
}

// `ddp update` 发起升级时，调用进程是 CLI 而不是面板本身，面板不会自己退出。
// helper 必须显式请面板退出，否则会在面板还活着的时候替换二进制并再起一个实例。
func TestMagiskUpdateHelperScriptTerminatesPanelWhenInvokedFromCLI(t *testing.T) {
	workDir := t.TempDir()
	plan := &panelUpdatePlan{
		DeploymentType: panelUpdateDeploymentMagisk,
		BinaryName:     "daidai-linux-arm64",
		WebDir:         "/app/web",
		DataDir:        "/app/Dumb-Panel",
		CurrentPID:     1111, // ddp CLI
		ServerPID:      2222, // 真正的面板进程
	}

	scriptPath, err := writeMagiskUpdateHelperScript(plan, "/tmp/src", workDir)
	if err != nil {
		t.Fatalf("生成 helper 脚本失败: %v", err)
	}
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读取 helper 脚本失败: %v", err)
	}
	script := string(raw)

	if !strings.Contains(script, "SERVER_PID=2222") {
		t.Fatalf("helper 脚本没有携带面板进程 PID:\n%s", script)
	}
	if !strings.Contains(script, `kill -TERM "$SERVER_PID"`) {
		t.Fatalf("helper 脚本必须先请面板进程退出:\n%s", script)
	}
	// TERM 之后要等它真的走掉，超时再 KILL；不能 TERM 完立刻替换文件。
	termIndex := strings.Index(script, `kill -TERM "$SERVER_PID"`)
	replaceIndex := strings.Index(script, `mv -f "$TARGET_BIN.new" "$TARGET_BIN"`)
	killIndex := strings.Index(script, `kill -KILL "$SERVER_PID"`)
	if termIndex < 0 || killIndex < 0 || replaceIndex < 0 {
		t.Fatalf("helper 脚本缺少必要步骤:\n%s", script)
	}
	if !(termIndex < killIndex && killIndex < replaceIndex) {
		t.Fatalf("helper 脚本必须在替换文件之前确认面板已退出:\n%s", script)
	}
}

func TestBuildPanelUpdateTargetForMagisk(t *testing.T) {
	target := buildPanelUpdateTarget(&panelUpdatePlan{
		DeploymentType: panelUpdateDeploymentMagisk,
		UpdateManager:  panelUpdateManagerPanel,
		ReleaseVersion: "3.0.3",
		AssetName:      "daidai-linux-arm64.tar.gz",
		BinaryName:     "daidai-linux-arm64",
		WebDir:         "/app/web",
		ModuleDir:      "/data/adb/modules/daidai-panel",
	})

	if target["deployment_type"] != panelUpdateDeploymentMagisk {
		t.Fatalf("deployment_type 应为 magisk，实际 %v", target["deployment_type"])
	}
	if target["asset_name"] != "daidai-linux-arm64.tar.gz" {
		t.Fatalf("asset_name 不正确: %v", target["asset_name"])
	}
	if target["web_dir"] != "/app/web" {
		t.Fatalf("web_dir 不正确: %v", target["web_dir"])
	}
	// 模块版没有容器镜像概念，不能把 docker 那套字段带出去误导前端。
	if _, exists := target["image_name"]; exists {
		t.Fatal("magisk 目标不应包含 image_name")
	}
	if _, exists := target["container_name"]; exists {
		t.Fatal("magisk 目标不应包含 container_name")
	}
}

// 非模块版环境下必须返回内部哨兵错误，让更新链路继续往 docker / binary 走，
// 而不是把「不是模块版」当成升级失败抛给用户。
func TestBuildMagiskPanelUpdatePlanSkipsNonMagiskRuntime(t *testing.T) {
	t.Setenv("DAIDAI_MAGISK_MODULE", "")

	_, err := buildMagiskPanelUpdatePlan(nil)
	if err == nil {
		t.Fatal("非模块版环境应返回错误")
	}
	if err != errMagiskRuntimeNotDetected {
		t.Fatalf("非模块版环境应返回 errMagiskRuntimeNotDetected，实际: %v", err)
	}
}
