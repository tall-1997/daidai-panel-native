package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestBuildNotifyHelperEnvCreatesManagedHelpers(t *testing.T) {
	root := testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	workDir := filepath.Join(scriptsDir, "nested")

	env, tokenInfo, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour, false)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	if env["DAIDAI_NOTIFY_URL"] == "" || env["DAIDAI_NOTIFY_TOKEN"] == "" {
		t.Fatalf("expected notify url/token in env, got %#v", env)
	}
	claims, err := middleware.ParseToken(env["DAIDAI_NOTIFY_TOKEN"])
	if err != nil {
		t.Fatalf("parse helper token: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info with jti, got %#v", tokenInfo)
	}
	if claims.ID != tokenInfo.JTI {
		t.Fatalf("expected returned jti %q to match token claim %q", tokenInfo.JTI, claims.ID)
	}

	paths := []string{
		filepath.Join(scriptsDir, notifyPyFilename),
		filepath.Join(scriptsDir, sendNotifyJSFilename),
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read helper %s: %v", path, err)
		}
		if !strings.Contains(string(content), "DAIDAI_PANEL_MANAGED_NOTIFY_HELPER") {
			t.Fatalf("expected helper marker in %s", path)
		}
	}
	if _, err := os.Stat(filepath.Join(workDir, notifyPyFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested notify.py to stay absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, sendNotifyJSFilename)); !os.IsNotExist(err) {
		t.Fatalf("expected nested sendNotify.js to stay absent, got err=%v", err)
	}
	if got := env["DAIDAI_SCRIPTS_DIR"]; got != scriptsDir {
		t.Fatalf("expected DAIDAI_SCRIPTS_DIR=%q, got %q", scriptsDir, got)
	}

	if _, err := os.Stat(filepath.Join(root, "data")); err != nil {
		t.Fatalf("expected test data dir to exist: %v", err)
	}
}

func TestBuildNotifyHelperEnvUsesAbsoluteHelperPaths(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := filepath.Join(config.C.Data.ScriptsDir, "nested")
	workDir := filepath.Join(config.C.Data.ScriptsDir, "jobs")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	env, _, err := BuildNotifyHelperEnv(scriptsDir, workDir, config.C.Server.Port, nil, time.Hour, false)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}

	for _, key := range []string{"DAIDAI_SCRIPTS_DIR", "DAIDAI_NOTIFY_PY", "DAIDAI_SEND_NOTIFY_JS"} {
		if !filepath.IsAbs(env[key]) {
			t.Fatalf("expected %s to be absolute, got %q", key, env[key])
		}
	}
}

// 验收 A5：通用入口变量必须存在，而历史的 notify 专用变量一个都不能少 ——
// 内置 notify.py / sendNotify.js 和用户既有脚本都在读它们。
func TestBuildNotifyHelperEnvExposesGenericAPIEntrypoint(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	env, tokenInfo, err := BuildNotifyHelperEnv(scriptsDir, scriptsDir, config.C.Server.Port, nil, time.Hour, false)
	if err != nil {
		t.Fatalf("build notify helper env: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info, got %#v", tokenInfo)
	}

	wantBase := fmt.Sprintf("http://127.0.0.1:%d/api/v1", config.C.Server.Port)
	if got := env["DAIDAI_API_BASE"]; got != wantBase {
		t.Fatalf("expected DAIDAI_API_BASE=%q, got %q", wantBase, got)
	}
	if got := env["DAIDAI_NOTIFY_URL"]; got != wantBase+"/notifications/send" {
		t.Fatalf("expected DAIDAI_NOTIFY_URL to stay unchanged, got %q", got)
	}
	if env["DAIDAI_TOKEN"] == "" {
		t.Fatalf("expected DAIDAI_TOKEN to be populated")
	}
	if env["DAIDAI_TOKEN"] != env["DAIDAI_NOTIFY_TOKEN"] {
		t.Fatalf("expected DAIDAI_TOKEN and DAIDAI_NOTIFY_TOKEN to be the same credential, got %q vs %q",
			env["DAIDAI_TOKEN"], env["DAIDAI_NOTIFY_TOKEN"])
	}

	claims, err := middleware.ParseToken(env["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse DAIDAI_TOKEN: %v", err)
	}
	if claims.Role != "operator" {
		t.Fatalf("expected operator role on script token, got %q", claims.Role)
	}
}

// 同样的四个变量必须一路走到任务运行时的 env map，而不只是 helper 内部有。
func TestManagedRuntimeEnvMapCarriesScriptAPIEntrypoint(t *testing.T) {
	testutil.SetupTestEnv(t)

	scriptsDir := config.C.Data.ScriptsDir
	envMap, tokenInfo, err := BuildManagedRuntimeEnvMapWithScriptToken(scriptsDir, scriptsDir, nil, time.Hour, "", nil)
	if err != nil {
		t.Fatalf("build managed runtime env map: %v", err)
	}
	if tokenInfo == nil || tokenInfo.JTI == "" {
		t.Fatalf("expected script token info from runtime env builder, got %#v", tokenInfo)
	}

	for _, key := range []string{"DAIDAI_API_BASE", "DAIDAI_TOKEN", "DAIDAI_NOTIFY_URL", "DAIDAI_NOTIFY_TOKEN"} {
		if envMap[key] == "" {
			t.Fatalf("expected %s in task runtime env, got %q", key, envMap[key])
		}
	}

	claims, err := middleware.ParseToken(envMap["DAIDAI_TOKEN"])
	if err != nil {
		t.Fatalf("parse runtime DAIDAI_TOKEN: %v", err)
	}
	if claims.ID != tokenInfo.JTI {
		t.Fatalf("expected runtime token jti %q to match returned jti %q", claims.ID, tokenInfo.JTI)
	}
}

func TestEnsureManagedHelperFileRewritesManagedJSFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), sendNotifyJSFilename)
	if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\nmodule.exports = {}\n"), 0o644); err != nil {
		t.Fatalf("seed helper file: %v", err)
	}

	if err := ensureManagedHelperFile(path, managedSendNotifyJSContent+"\n"); err != nil {
		t.Fatalf("rewrite managed helper file: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten helper file: %v", err)
	}
	if string(content) != managedSendNotifyJSContent+"\n" {
		t.Fatalf("expected managed JS helper to be refreshed")
	}
}

func TestManagedHelperContentIncludesUsageDocs(t *testing.T) {
	if !strings.Contains(managedNotifyPyContent, "Usage:") {
		t.Fatalf("expected python helper usage docs")
	}
	if !strings.Contains(managedNotifyPyContent, "def send(title, content, ignore_default_config=False, **kwargs):") {
		t.Fatalf("expected python helper send signature docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "QingLong-style notify entry point.") {
		t.Fatalf("expected js helper entry point docs")
	}
	if !strings.Contains(managedSendNotifyJSContent, "@param {object} params") {
		t.Fatalf("expected js helper JSDoc params")
	}
	// issue #135：内容格式与按渠道发送要写进 helper 自带的用法说明，脚本作者打开文件就能看到。
	for _, want := range []string{`content_type="html"`, "def send_to(channel_type, title, content, content_type=None, **kwargs):", "wxpusher_bot", "channel_name"} {
		if !strings.Contains(managedNotifyPyContent, want) {
			t.Fatalf("expected python helper to document %q", want)
		}
	}
	for _, want := range []string{"async function sendTo(channelType, text, desp, params = {})", "content_type: 'html'", "channel_name"} {
		if !strings.Contains(managedSendNotifyJSContent, want) {
			t.Fatalf("expected js helper to document %q", want)
		}
	}
}

// findUsableInterpreter 按顺序找第一个真能跑的解释器。
// Windows 上 python3.exe 常常是应用商店的占位程序，LookPath 找得到但跑不了，所以要先 --version 验一遍。
func findUsableInterpreter(candidates ...string) string {
	for _, candidate := range candidates {
		found, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		if err := exec.Command(found, "--version").Run(); err != nil {
			continue
		}
		return found
	}
	return ""
}

// captureHelperRequests 起一个假面板记录 helper 发来的请求体，按到达顺序返回。
func captureHelperRequests(t *testing.T) (*httptest.Server, func() []map[string]interface{}) {
	t.Helper()
	var (
		mu     sync.Mutex
		bodies []map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode helper request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"ok"}`))
	}))
	t.Cleanup(server.Close)
	return server, func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]interface{}(nil), bodies...)
	}
}

func helperRunEnv(serverURL string) []string {
	return append(os.Environ(),
		"DAIDAI_NOTIFY_URL="+serverURL,
		"DAIDAI_NOTIFY_TOKEN=test-token",
		"DAIDAI_NOTIFY_TIMEOUT=5000",
		"DAIDAI_NOTIFY_CHANNEL_ID=7",
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONIOENCODING=utf-8",
		"NODE_OPTIONS=",
	)
}

func assertHelperBodies(t *testing.T, got, want []map[string]interface{}) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("helper 发出了 %d 个请求，期望 %d 个：%#v", len(got), len(want), got)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("第 %d 个请求体不对：\n got  %#v\n want %#v", i+1, got[i], want[i])
		}
	}
}

// TestManagedNotifyPyForwardsContentTypeAndSelectors 真跑托管 notify.py（issue #135）：
// content_type / channel_type / channel_name 必须进请求体而不是模板变量 context；
// 带了类型或名称选择器时不再回落任务默认渠道 DAIDAI_NOTIFY_CHANNEL_ID；青龙同名分渠道函数都在。
// TestManagedNotifyPyChannelSendersUseRegisteredTypes：青龙同名分渠道函数写死的渠道类型必须是注册表里真实存在的类型。
// 写错一个（比如 serverJ 写成 "serverj"）面板就回 400「未知的通知渠道类型」，而真跑 python 的用例只查函数在不在、查不出来。
func TestManagedNotifyPyChannelSendersUseRegisteredTypes(t *testing.T) {
	matches := regexp.MustCompile(`(?m)^(\w+) = _channel_sender\("(\w+)", "(\w+)"\)$`).FindAllStringSubmatch(managedNotifyPyContent, -1)
	if len(matches) != 17 {
		t.Fatalf("expected 17 QingLong-style channel senders, found %d", len(matches))
	}
	for _, m := range matches {
		if m[1] != m[2] {
			t.Errorf("sender %s is registered under a different __name__ %q", m[1], m[2])
		}
		if _, ok := model.GetNotifyChannelDefinition(m[3]); !ok {
			t.Errorf("sender %s targets channel type %q, which is not in the notify channel registry", m[1], m[3])
		}
	}
}

func TestManagedNotifyPyForwardsContentTypeAndSelectors(t *testing.T) {
	pythonBin := findUsableInterpreter("python", "python3")
	if pythonBin == "" {
		t.Skip("python not found or not usable")
	}

	server, bodies := captureHelperRequests(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, notifyPyFilename), []byte(managedNotifyPyContent+"\n"), 0o644); err != nil {
		t.Fatalf("write managed notify.py: %v", err)
	}
	driver := `import ast
import sys

import notify

# 托管 helper 承诺兼容 Python 3.6：按 3.6 语法再解析一遍，挡住海象运算符、仅位置参数这类新语法。
with open(notify.__file__, encoding="utf-8") as source:
    ast.parse(source.read(), feature_version=(3, 6))

names = ["wxpusher_bot", "smtp", "pushplus_bot", "dingding_bot", "feishu_bot", "telegram_bot", "wecom_bot",
         "wecom_app", "bark", "gotify", "iGot", "serverJ", "pushdeer", "qmsg_bot", "pushme", "ntfy", "custom_notify"]
missing = [name for name in names if not callable(getattr(notify, name, None))]
if missing:
    print("MISSING", missing)
    sys.exit(2)

notify.send("t1", "<b>c</b>", content_type="html", foo="1")
notify.wxpusher_bot("t2", "<b>c</b>", content_type="html")
notify.send_to("email", "t3", "c")
notify.smtp("t4", "c", channel_name="邮件")
notify.send("t5", "c", channel_names=["A", "B"], channel_types="webhook")
notify.send("t6", "c")
# 显式传空名称要原样发给面板（面板回 400），不能被当成没传、悄悄变成广播。
notify.send("t7", "c", channel_name="")
# 老脚本把 content_type 当模板变量传（不是字符串）：照旧进 context，不进请求体被面板 400。
notify.send("t8", "c", content_type=2)
# send_to 的类型为空时本地直接报错，不发请求。
for blank in (None, "", "  "):
    try:
        notify.send_to(blank, "t9", "c")
    except ValueError:
        pass
    else:
        print("send_to accepted blank channel_type", repr(blank))
        sys.exit(3)
`
	if err := os.WriteFile(filepath.Join(dir, "check.py"), []byte(driver), 0o644); err != nil {
		t.Fatalf("write driver script: %v", err)
	}

	cmd := exec.Command(pythonBin, "check.py")
	cmd.Dir = dir
	cmd.Env = helperRunEnv(server.URL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run managed notify.py driver: %v\n%s", err, out)
	}

	assertHelperBodies(t, bodies(), []map[string]interface{}{
		// 保留键进请求体，普通 kwargs 仍进 context；没有选择器时照旧回落任务默认渠道。
		{"title": "t1", "content": "<b>c</b>", "channel_id": float64(7), "content_type": "html", "context": map[string]interface{}{"foo": "1"}},
		// 分渠道函数带上渠道类型，不再带默认渠道 ID（否则和类型求交集多半是空集）。
		{"title": "t2", "content": "<b>c</b>", "channel_type": "wxpusher", "content_type": "html"},
		{"title": "t3", "content": "c", "channel_type": "email"},
		{"title": "t4", "content": "c", "channel_type": "email", "channel_name": "邮件"},
		{"title": "t5", "content": "c", "channel_names": []interface{}{"A", "B"}, "channel_types": []interface{}{"webhook"}},
		// 老调用的请求体形状不变。
		{"title": "t6", "content": "c", "channel_id": float64(7)},
		{"title": "t7", "content": "c", "channel_id": float64(7), "channel_name": ""},
		{"title": "t8", "content": "c", "channel_id": float64(7), "context": map[string]interface{}{"content_type": float64(2)}},
	})
}

// TestManagedNotifyPyQingLongSendersSkipWhenNoChannelMatches：青龙同名分渠道函数在面板没有匹配渠道时（issue #135 复查），
// 和青龙一样打印一行跳过、返回 None，后面的调用照常发；其它 400 照旧抛异常，send / send_to 保持严格。
// 假面板回的 400 文案取服务端真实报错，面板改了措辞 helper 就认不出来，这里会红。
func TestManagedNotifyPyQingLongSendersSkipWhenNoChannelMatches(t *testing.T) {
	pythonBin := findUsableInterpreter("python", "python3")
	if pythonBin == "" {
		t.Skip("python not found or not usable")
	}
	testutil.SetupTestEnv(t)

	_, broadcastErr := SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{ChannelTypes: []string{"wxpusher"}})
	_, targetedErr := SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{ChannelIDs: []uint{9999}, ChannelTypes: []string{"bark"}})
	if broadcastErr == nil || targetedErr == nil {
		t.Fatalf("空库里按类型发送应当报错，实际 broadcast=%v targeted=%v", broadcastErr, targetedErr)
	}

	var (
		mu     sync.Mutex
		bodies []map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]interface{}{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode helper request body: %v", err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()

		// 与 handler 的 response.BadRequest(c, "发送失败: "+err.Error()) 同形。
		reject := func(message string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
		}
		switch body["channel_type"] {
		case "wxpusher":
			reject("发送失败: " + broadcastErr.Error())
		case "bark":
			reject("发送失败: " + targetedErr.Error())
		case "gotify":
			reject("未找到名称为「写错的名字」的通知渠道")
		case "ntfy":
			// 渠道自己发送失败（handler 按「渠道名: 错误」拼接），下游回的正文里恰好也带着同一句。
			reject("发送失败: 坏渠道: HTTP 400: " + `{"error":"发送失败: ` + broadcastErr.Error() + `"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message":"ok"}`))
		}
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, notifyPyFilename), []byte(managedNotifyPyContent+"\n"), 0o644); err != nil {
		t.Fatalf("write managed notify.py: %v", err)
	}
	driver := `import sys

import notify

# 没有匹配的渠道：跳过并返回 None，后面的调用照常发出去。
if notify.wxpusher_bot("t1", "<b>c</b>", content_type="html") is not None:
    print("wxpusher_bot should return None when skipped")
    sys.exit(2)
notify.smtp("t2", "c")
if notify.bark("t3", "c", channel_id=9999) is not None:
    print("bark should return None when skipped")
    sys.exit(3)

# 其它 400（比如名称写错）照旧抛异常。
try:
    notify.gotify("t4", "c", channel_name="写错的名字")
except RuntimeError:
    pass
else:
    print("gotify should raise on other 400")
    sys.exit(4)

# 渠道发送失败时报错里恰好带着「暂无参与广播…」（下游的正文）：不是没有匹配的渠道，照旧抛异常。
try:
    notify.ntfy("t5", "c")
except RuntimeError:
    pass
else:
    print("ntfy should raise on delivery failure")
    sys.exit(6)

# send / send_to 保持严格。
for title, call in (("t6", lambda: notify.send_to("wxpusher", "t6", "c")), ("t7", lambda: notify.send("t7", "c", channel_type="wxpusher"))):
    try:
        call()
    except RuntimeError:
        pass
    else:
        print("strict call did not raise", title)
        sys.exit(5)
print("DRIVER-DONE")
`
	if err := os.WriteFile(filepath.Join(dir, "check.py"), []byte(driver), 0o644); err != nil {
		t.Fatalf("write driver script: %v", err)
	}

	cmd := exec.Command(pythonBin, "check.py")
	cmd.Dir = dir
	cmd.Env = helperRunEnv(server.URL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run managed notify.py driver: %v\n%s", err, out)
	}
	output := string(out)
	if !strings.Contains(output, "DRIVER-DONE") {
		t.Fatalf("driver did not finish:\n%s", output)
	}
	for _, want := range []string{"wxpusher_bot", "暂无参与广播的默认推送渠道", "bark", "未找到已启用的通知渠道"} {
		if !strings.Contains(output, want) {
			t.Fatalf("跳过时应打印一行说明（含 %q），实际输出：\n%s", want, output)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	var titles []string
	for _, body := range bodies {
		titles = append(titles, fmt.Sprint(body["title"]))
	}
	if !reflect.DeepEqual(titles, []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7"}) {
		t.Fatalf("请求顺序不对（第一个没渠道时后面的调用必须照常发出）：%v", titles)
	}
}

// TestManagedSendNotifyJSForwardsContentTypeAndSelectors 是上一条的 Node 版本（issue #135）。
func TestManagedSendNotifyJSForwardsContentTypeAndSelectors(t *testing.T) {
	nodeBin := findUsableInterpreter("node")
	if nodeBin == "" {
		t.Skip("node not found or not usable")
	}

	server, bodies := captureHelperRequests(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sendNotifyJSFilename), []byte(managedSendNotifyJSContent+"\n"), 0o644); err != nil {
		t.Fatalf("write managed sendNotify.js: %v", err)
	}
	driver := `const { sendNotify, sendTo } = require('./sendNotify.js');

(async () => {
  await sendNotify('t1', '<b>c</b>', { content_type: 'html', foo: '1' });
  await sendTo('wxpusher', 't2', '<b>c</b>', { content_type: 'html' });
  await sendNotify('t3', 'c', { channel_types: 'email', channel_name: '邮件' });
  await sendNotify('t4', 'c');
  // 显式传空类型要原样发给面板（面板回 400），不能被当成没传、悄悄变成广播。
  await sendNotify('t5', 'c', { channel_type: '' });
  // 老脚本把 content_type 当模板变量传（不是字符串）：照旧进 context，不进请求体被面板 400。
  await sendNotify('t6', 'c', { content_type: 2 });
  // sendTo 的类型为空时本地直接报错，不发请求。
  for (const blank of [undefined, null, '', '  ']) {
    let rejected = false;
    try {
      await sendTo(blank, 't7', 'c');
    } catch (err) {
      rejected = true;
    }
    if (!rejected) {
      console.error('sendTo accepted blank channelType', JSON.stringify(blank));
      process.exit(3);
    }
  }
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
`
	if err := os.WriteFile(filepath.Join(dir, "check.js"), []byte(driver), 0o644); err != nil {
		t.Fatalf("write driver script: %v", err)
	}

	cmd := exec.Command(nodeBin, "check.js")
	cmd.Dir = dir
	cmd.Env = append(helperRunEnv(server.URL), "DAIDAI_SCRIPTS_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("run managed sendNotify.js driver: %v\n%s", err, out)
	}

	assertHelperBodies(t, bodies(), []map[string]interface{}{
		{"title": "t1", "content": "<b>c</b>", "channel_id": float64(7), "content_type": "html", "context": map[string]interface{}{"foo": "1"}},
		{"title": "t2", "content": "<b>c</b>", "channel_type": "wxpusher", "content_type": "html"},
		// 字符串形式的 channel_types 也要当成列表发出去，不能被静默丢掉退化成广播。
		{"title": "t3", "content": "c", "channel_types": []interface{}{"email"}, "channel_name": "邮件"},
		{"title": "t4", "content": "c", "channel_id": float64(7)},
		{"title": "t5", "content": "c", "channel_id": float64(7), "channel_type": ""},
		{"title": "t6", "content": "c", "channel_id": float64(7), "context": map[string]interface{}{"content_type": float64(2)}},
	})
}

// issue #111 守卫：托管标记里的 " v1" 绝对不能升成 v2 或别的值。
// ensureManagedHelperFile 是靠「文件正文里有没有这枚 token」来区分
// 「面板自己写的托管文件」和「用户手写的同名文件」的。
// 磁盘上已经存在的 v1 文件里当然不含 "v2" 字串，一旦把 token 改掉，
// 这些老文件全都会被误判成用户自定义而**永久停止更新** —— 比 502 更隐蔽的回归。
func TestManagedNotifyHelperTokenStaysV1(t *testing.T) {
	if !strings.HasSuffix(managedNotifyHelperToken, " v1") {
		t.Fatalf("managedNotifyHelperToken 必须仍以 \" v1\" 结尾，当前为 %q", managedNotifyHelperToken)
	}
	// 两份托管正文都必须带上这枚 token，否则面板自己写出去的文件下次也会被当成用户自定义。
	if !strings.Contains(managedNotifyPyContent, managedNotifyHelperToken) {
		t.Fatalf("expected managed token inside notify.py content")
	}
	if !strings.Contains(managedSendNotifyJSContent, managedNotifyHelperToken) {
		t.Fatalf("expected managed token inside sendNotify.js content")
	}
}

// issue #111：面板开代理后，urllib 会把发往面板自身 127.0.0.1:<端口> 的通知请求
// 也一并交给代理，代理直接回 502。修法是显式 opener + **只对回环豁免**：
// request_notify 支持 url= 覆盖成外网地址，无条件关代理会把那种用法在
// 「只能走代理出网」的环境里直接打断。
func TestManagedNotifyPyDisablesProxyOnlyForLoopback(t *testing.T) {
	if !strings.Contains(managedNotifyPyContent, "import urllib.parse") {
		t.Fatalf("expected `import urllib.parse` in notify.py content")
	}
	if !strings.Contains(managedNotifyPyContent, "urllib.request.build_opener(urllib.request.ProxyHandler({}))") {
		t.Fatalf("expected loopback branch to build an opener with ProxyHandler({})")
	}
	// 必须留着 else 分支的裸 build_opener()：只有 ProxyHandler({}) 一条路就等于无条件关代理。
	if !strings.Contains(managedNotifyPyContent, "        opener = urllib.request.build_opener()") {
		t.Fatalf("expected non-loopback branch to keep the default proxy-aware opener")
	}
	// 三种回环写法都要覆盖到：127.x.x.x / localhost / ::1。
	for _, want := range []string{"\"localhost\", \"::1\"", "host.startswith(\"127.\")"} {
		if !strings.Contains(managedNotifyPyContent, want) {
			t.Fatalf("expected loopback host check to contain %q", want)
		}
	}
	// urlopen 用的是模块级默认 opener，回环豁免根本不会生效，必须换成 opener.open。
	if strings.Contains(managedNotifyPyContent, "urllib.request.urlopen(") {
		t.Fatalf("expected urlopen to be replaced by the explicit opener")
	}
	if !strings.Contains(managedNotifyPyContent, "with opener.open(request, timeout=timeout_seconds) as response:") {
		t.Fatalf("expected the request to go through the explicit opener")
	}
	// 绝不能退回 CIDR 写法：127.0.0.1/32 这类 Python 的代理白名单一律匹配不上（已实测）。
	// 只查真正的代码行 —— 注释里恰恰要写着「别用 CIDR」来警告后来人，
	// 一刀切地匹配整份内容会把那条警告注释本身判成违规。
	for _, line := range strings.Split(managedNotifyPyContent, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "127.0.0.1/") {
			t.Fatalf("CIDR 写法在 Python 侧无效，不要用它做回环白名单，问题行: %s", line)
		}
	}
	// 报错分支是脚本作者排障的唯一线索，改 opener 时不能顺手丢掉。
	if !strings.Contains(managedNotifyPyContent, "except urllib.error.HTTPError as err:") ||
		!strings.Contains(managedNotifyPyContent, "except urllib.error.URLError as err:") {
		t.Fatalf("expected HTTPError / URLError branches to be preserved")
	}
}

// 真跑一次：把托管 notify.py 落到临时目录，在「代理环境变量指向一个死端口」的前提下
// 让它给本机 httptest 假面板发通知。修复前请求会被丢给死代理而失败。
// 顺带把 Go 字面量拼出来的 Python 缩进也验了 —— 缩进写坏 python 直接语法错。
func TestManagedNotifyPyReachesLoopbackPanelWithProxyEnv(t *testing.T) {
	// Windows 上 python3.exe 常常是应用商店的占位程序，LookPath 找得到但跑不了，
	// 所以每个候选都要先用 --version 验一遍能不能真跑；Linux 镜像里则可能只有 python3。
	pythonBin := ""
	for _, candidate := range []string{"python", "python3"} {
		found, lookErr := exec.LookPath(candidate)
		if lookErr != nil {
			continue
		}
		if runErr := exec.Command(found, "--version").Run(); runErr != nil {
			continue
		}
		pythonBin = found
		break
	}
	if pythonBin == "" {
		t.Skip("python not found or not usable")
	}

	// httptest 默认就监听 127.0.0.1，正好是要豁免的回环地址。
	const wantMessage = "panel-ok-9f3a"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"message":%q}`, wantMessage)
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, notifyPyFilename), []byte(managedNotifyPyContent+"\n"), 0o644); err != nil {
		t.Fatalf("write managed notify.py: %v", err)
	}
	// control 分支走原始的 urlopen 写法，用来确认「代理环境变量在本机真的生效」；
	// 不做这个对照，本机若自带 bypass 规则，这条用例会假绿。
	driver := `import os
import sys
import urllib.request

if sys.argv[1] == "control":
    request = urllib.request.Request(
        os.environ["DAIDAI_NOTIFY_URL"],
        data=b"{}",
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        response.read()
    print("CONTROL-REACHED-SERVER")
    sys.exit(0)

import notify

print(notify.request_notify("t", "c").get("message", ""))
`
	if err := os.WriteFile(filepath.Join(dir, "check.py"), []byte(driver), 0o644); err != nil {
		t.Fatalf("write driver script: %v", err)
	}

	// 127.0.0.1:1 上不可能有人监听，所以「请求被交给代理」等价于「立刻失败」。
	// no_proxy 显式置空，避免本机既有的 no_proxy 让对照组失去意义。
	const deadProxy = "http://127.0.0.1:1"
	runEnv := append(os.Environ(),
		"DAIDAI_NOTIFY_URL="+server.URL,
		"DAIDAI_NOTIFY_TOKEN=test-token",
		"DAIDAI_NOTIFY_TIMEOUT=5000",
		"http_proxy="+deadProxy,
		"HTTP_PROXY="+deadProxy,
		"https_proxy="+deadProxy,
		"HTTPS_PROXY="+deadProxy,
		"all_proxy="+deadProxy,
		"ALL_PROXY="+deadProxy,
		"no_proxy=",
		"NO_PROXY=",
		"PYTHONDONTWRITEBYTECODE=1",
	)

	control := exec.Command(pythonBin, "check.py", "control")
	control.Dir = dir
	control.Env = runEnv
	if out, err := control.CombinedOutput(); err == nil && strings.Contains(string(out), "CONTROL-REACHED-SERVER") {
		t.Skipf("本机代理环境变量对回环地址不生效，用例失去对照意义: %s", strings.TrimSpace(string(out)))
	}

	cmd := exec.Command(pythonBin, "check.py", "notify")
	cmd.Dir = dir
	cmd.Env = runEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("managed notify.py 在开代理时没能直连面板: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), wantMessage) {
		t.Fatalf("expected panel response %q in output, got %q", wantMessage, string(out))
	}
}

func TestAppendScriptHelperPathsKeepsExistingEntries(t *testing.T) {
	env := map[string]string{
		"NODE_PATH":    "/tmp/node_modules",
		"PYTHONPATH":   "/tmp/site-packages",
		"NODE_OPTIONS": "--trace-warnings",
	}

	AppendScriptHelperPaths(env, "/tmp/scripts")
	AppendScriptHelperPaths(env, "/tmp/scripts")

	if got := env["NODE_PATH"]; !strings.Contains(got, "/tmp/node_modules") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected NODE_PATH: %q", got)
	}
	if strings.Count(env["NODE_PATH"], "/tmp/scripts") != 1 {
		t.Fatalf("expected deduplicated NODE_PATH, got %q", env["NODE_PATH"])
	}
	if got := env["PYTHONPATH"]; !strings.Contains(got, "/tmp/site-packages") || !strings.Contains(got, "/tmp/scripts") {
		t.Fatalf("unexpected PYTHONPATH: %q", got)
	}
	if got := env["NODE_OPTIONS"]; !strings.Contains(got, "--trace-warnings") || !strings.Contains(got, "/tmp/scripts/sendNotify.js") {
		t.Fatalf("unexpected NODE_OPTIONS: %q", got)
	}
	if strings.Count(env["NODE_OPTIONS"], "/tmp/scripts/sendNotify.js") != 1 {
		t.Fatalf("expected deduplicated NODE_OPTIONS, got %q", env["NODE_OPTIONS"])
	}
}

func TestCleanupManagedHelperCopiesRemovesOnlyManagedNestedHelpers(t *testing.T) {
	scriptsDir := filepath.Join(t.TempDir(), "scripts")
	workDir := filepath.Join(scriptsDir, "nested")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work dir: %v", err)
	}

	managedNested := filepath.Join(workDir, sendNotifyJSFilename)
	customNested := filepath.Join(workDir, notifyPyFilename)
	if err := os.WriteFile(managedNested, []byte("// "+managedNotifyHelperToken+"\nmodule.exports={}\n"), 0o644); err != nil {
		t.Fatalf("write managed nested helper: %v", err)
	}
	if err := os.WriteFile(customNested, []byte("# custom helper\n"), 0o644); err != nil {
		t.Fatalf("write custom nested helper: %v", err)
	}

	if err := cleanupManagedHelperCopies(scriptsDir, workDir); err != nil {
		t.Fatalf("cleanup helper copies: %v", err)
	}

	if _, err := os.Stat(managedNested); !os.IsNotExist(err) {
		t.Fatalf("expected managed nested helper to be removed, err=%v", err)
	}
	if _, err := os.Stat(customNested); err != nil {
		t.Fatalf("expected custom nested helper to be preserved, err=%v", err)
	}
}

func TestCleanupManagedHelperCopiesUnderRootRemovesManagedCopiesInNestedDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scripts")
	firstNested := filepath.Join(root, "a")
	secondNested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(secondNested, 0o755); err != nil {
		t.Fatalf("mkdir nested dirs: %v", err)
	}

	rootHelper := filepath.Join(root, sendNotifyJSFilename)
	firstHelper := filepath.Join(firstNested, sendNotifyJSFilename)
	secondHelper := filepath.Join(secondNested, notifyPyFilename)
	for _, path := range []string{rootHelper, firstHelper, secondHelper} {
		if err := os.WriteFile(path, []byte("// "+managedNotifyHelperToken+"\n"), 0o644); err != nil {
			t.Fatalf("write helper %s: %v", path, err)
		}
	}

	if err := CleanupManagedHelperCopiesUnderRoot(root); err != nil {
		t.Fatalf("cleanup under root: %v", err)
	}

	if _, err := os.Stat(rootHelper); err != nil {
		t.Fatalf("expected root helper to stay, err=%v", err)
	}
	if _, err := os.Stat(firstHelper); !os.IsNotExist(err) {
		t.Fatalf("expected first nested helper removed, err=%v", err)
	}
	if _, err := os.Stat(secondHelper); !os.IsNotExist(err) {
		t.Fatalf("expected second nested helper removed, err=%v", err)
	}
}

// 面板启动时在后台遍历（#158）：某个目录清理失败（这里用「名叫 notify.py 的目录」造出读错误）不能让整棵停下，
// 其余目录照清，根目录的真 helper 不动，遍历完把这个错误交给调用方打日志。
// 它只管「cleanupManagedHelperCopies 出错不中止」这一半：a/notify.py 这个目录本身读得了，WalkDir 的回调拿不到 err，
// 另一半（读不了的目录跳过）见下一条。
func TestCleanupManagedHelperCopiesUnderRootContinuesPastBrokenDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "scripts")
	managedCopy := "// " + managedNotifyHelperToken + "\n"
	// WalkDir 按字典序走，a 在 b 前面。
	if err := os.MkdirAll(filepath.Join(root, "a", notifyPyFilename), 0o755); err != nil {
		t.Fatalf("mkdir a/notify.py: %v", err)
	}
	rootHelper := filepath.Join(root, sendNotifyJSFilename)
	laterCopy := filepath.Join(root, "b", sendNotifyJSFilename)
	writeStartupScriptFile(t, rootHelper, managedCopy)
	writeStartupScriptFile(t, laterCopy, managedCopy)

	err := CleanupManagedHelperCopiesUnderRoot(root)
	if err == nil {
		t.Fatal("a/notify.py 读不了，应把这个错误交给调用方打日志")
	}
	if _, statErr := os.Stat(laterCopy); !os.IsNotExist(statErr) {
		t.Fatalf("a 目录出错后 b 目录里的托管副本仍应清掉，stat err=%v（返回的错误：%v）", statErr, err)
	}
	if _, statErr := os.Stat(rootHelper); statErr != nil {
		t.Fatalf("根目录的 helper 不能动，stat err=%v", statErr)
	}
}

// 后台遍历时读不了的目录跳过，其余目录照清（#158）。遍历途中目录刚被脚本管理删掉走的也是这个分支：
// ReadDir 报错后 WalkDir 带着 err 再回调一次。靠目录权限造读错误，Windows 与 root 上造不出来，跳过（CI 的 GitHub runner 是普通用户，会真跑）。
func TestCleanupManagedHelperCopiesUnderRootSkipsUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("靠目录权限造读错误，Windows 与 root 上造不出来")
	}
	root := filepath.Join(t.TempDir(), "scripts")
	unreadable := filepath.Join(root, "a")
	if err := os.MkdirAll(unreadable, 0o755); err != nil {
		t.Fatalf("mkdir a: %v", err)
	}
	laterCopy := filepath.Join(root, "b", sendNotifyJSFilename)
	writeStartupScriptFile(t, laterCopy, "// "+managedNotifyHelperToken+"\n")
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatalf("chmod a: %v", err)
	}
	// 用例结束先改回来，否则 t.TempDir 删不掉。
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	// 返回值不断言：a 读不了时，cleanupManagedHelperCopies 也可能先报一次错。
	_ = CleanupManagedHelperCopiesUnderRoot(root)

	if _, err := os.Stat(laterCopy); !os.IsNotExist(err) {
		t.Fatalf("a 读不了时 b 里的托管副本仍应清掉，stat err=%v", err)
	}
}
