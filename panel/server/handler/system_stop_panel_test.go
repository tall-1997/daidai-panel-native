package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"daidai-panel/service"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

// 这一组用例守住「停止面板服务」与「重启面板」之间那条最贵的边界：
//
//	重启 = 只退出进程，靠外部（Magisk 存活守护 / systemd / Docker）拉回来；
//	停止 = 先写跨重启的停止开关，再退出进程，谁都不许把它拉回来。
//
// 一旦 /system/restart 顺手写了停止开关，用户点一次「重启面板」就会变成永久停机：
// 面板不会自己回来，Web 页面也没了，只能去模块管理器点动作按钮。
// 这种故障在真机上极难自查，所以必须有测试钉死。

// newSystemStopPanelTestEnv 把 os.Exit、停止开关路径、模块运行态三样都换成可控值。
// 返回值是「进程退出码」的接收通道 —— 生产代码里那句 os.Exit 会把整个 test 二进制干掉，
// 所以只能把它抽成变量再替换。
func newSystemStopPanelTestEnv(t *testing.T, magiskRuntime bool, shellVersion string) (*gin.Engine, string, string, chan int) {
	t.Helper()

	testutil.SetupTestEnv(t)

	flagPath := filepath.Join(t.TempDir(), "daidai-panel", "stopped")
	previousFlagPath := magiskStopFlagPathForTest
	magiskStopFlagPathForTest = flagPath

	exitCodes := make(chan int, 4)
	previousExit := panelProcessExit
	previousDelay := panelProcessExitDelay
	panelProcessExit = func(code int) { exitCodes <- code }
	panelProcessExitDelay = time.Millisecond

	t.Cleanup(func() {
		magiskStopFlagPathForTest = previousFlagPath
		panelProcessExit = previousExit
		panelProcessExitDelay = previousDelay
	})

	// IsMagiskModuleRuntime 优先看这个环境变量（空串等于「不是模块版」），
	// 其次探 /data/adb 下的模块标记文件 —— Windows / CI 上都不存在。
	// 这里显式写空值而不是「不设置」，避免受同包其它用例残留的环境变量影响。
	if magiskRuntime {
		t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	} else {
		t.Setenv("DAIDAI_MAGISK_MODULE", "")
	}
	t.Setenv(magiskShellVersionEnv, shellVersion)

	user := testutil.MustCreateUser(t, "system-stop-admin", "admin")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	engine := gin.New()
	api := engine.Group("/api/v1")
	NewSystemHandler().RegisterRoutes(api)

	return engine, token, flagPath, exitCodes
}

func postSystemAction(t *testing.T, engine *gin.Engine, token, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func awaitProcessExitCode(t *testing.T, exitCodes chan int) int {
	t.Helper()

	select {
	case code := <-exitCodes:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("面板进程退出回调没有被触发")
		return 0
	}
}

// 重启绝不能写停止开关 —— 这是本次改动里最危险的一条误接线。
func TestRestartPanelNeverWritesMagiskStopFlag(t *testing.T) {
	engine, token, flagPath, exitCodes := newSystemStopPanelTestEnv(t, true, "2")

	rec := postSystemAction(t, engine, token, "/api/v1/system/restart")
	if rec.Code != http.StatusOK {
		t.Fatalf("restart 应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if code := awaitProcessExitCode(t, exitCodes); code != 1 {
		t.Fatalf("restart 的退出码应为 1（保持既有行为），实际 %d", code)
	}
	if _, err := os.Stat(flagPath); !os.IsNotExist(err) {
		t.Fatalf("restart 绝不能写停止开关，但 %s 出现了（err=%v）——模块版一次正常重启会变成永久停机", flagPath, err)
	}
}

// 停止：模块版 + 新外壳，写开关后退出。
func TestStopPanelWritesStopFlagOnMagiskRuntime(t *testing.T) {
	engine, token, flagPath, exitCodes := newSystemStopPanelTestEnv(t, true, "2")

	rec := postSystemAction(t, engine, token, "/api/v1/system/stop")
	if rec.Code != http.StatusOK {
		t.Fatalf("stop 应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(flagPath)
	if err != nil {
		t.Fatalf("停止开关未写入 %s: %v", flagPath, err)
	}
	if len(data) == 0 {
		t.Fatal("停止开关内容为空，至少要写一行时间戳便于事后排查")
	}
	if code := awaitProcessExitCode(t, exitCodes); code != 0 {
		t.Fatalf("stop 的退出码应为 0，实际 %d", code)
	}
}

// 非模块版：直接 400，且不留下任何开关文件。
// 其它部署形态的进程管理器会立刻把面板拉回来，放行只会让用户误以为停成功了。
func TestStopPanelRejectedOutsideMagiskRuntime(t *testing.T) {
	engine, token, flagPath, _ := newSystemStopPanelTestEnv(t, false, "")

	rec := postSystemAction(t, engine, token, "/api/v1/system/stop")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非模块版的 stop 应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(flagPath); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的 stop 不得写停止开关，但 %s 出现了（err=%v）", flagPath, err)
	}
}

// 旧外壳（在线升级上来的 v3.0.4 用户）：写了开关也没人读，必须在接口层拦下并提示重刷 zip。
func TestStopPanelRejectedOnLegacyShellVersion(t *testing.T) {
	engine, token, flagPath, _ := newSystemStopPanelTestEnv(t, true, "1")

	rec := postSystemAction(t, engine, token, "/api/v1/system/stop")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("旧外壳的 stop 应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(flagPath); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的 stop 不得写停止开关，但 %s 出现了（err=%v）", flagPath, err)
	}
}

// /system/info 必须平铺地带出部署形态与外壳版本：
// 前端要靠它决定「停止面板服务」按钮显示 / 禁用，而且老字段的位置一个都不能挪
// —— 这个接口独立发版的 Flutter APP 也在读。
func TestSystemInfoExposesDeploymentTypeAndShellVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_MAGISK_MODULE", "1")
	t.Setenv(magiskShellVersionEnv, "2")

	// 采集真实资源快照会去读磁盘 / 起外部命令，跟本用例要验的东西无关，换成固定值。
	previousResourceInfo := systemHealthGetResourceInfo
	systemHealthGetResourceInfo = func() service.ResourceInfo {
		return service.ResourceInfo{Hostname: "stop-panel-test-host", DataDir: "/tmp/daidai"}
	}
	t.Cleanup(func() { systemHealthGetResourceInfo = previousResourceInfo })

	user := testutil.MustCreateUser(t, "system-info-viewer", "viewer")
	token := testutil.MustCreateAccessToken(t, user.Username, user.Role)

	engine := gin.New()
	api := engine.Group("/api/v1")
	NewSystemHandler().RegisterRoutes(api)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("system/info 应返回 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, key := range []string{
		`"deployment_type"`,
		`"magisk_shell_version"`,
		// 老字段必须仍在同一层，不能被包进 "resource" 之类的新对象里
		`"hostname"`,
		`"memory_total"`,
		`"data_dir"`,
	} {
		if !strings.Contains(body, key) {
			t.Fatalf("system/info 响应缺少 %s，实际=%s", key, body)
		}
	}
}
