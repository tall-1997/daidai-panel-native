package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 探针要能抓住的形状：一个 Promise 只在请求对象上挂了 error 监听，
// 连接却是在响应开始之后断的，于是它永远不 settle。await 它的调用永久卡住，
// 事件循环随后排空，node 以退出码 0 退出 —— 从外面看和「跑完了」一模一样。
const hangingScript = `
console.log('开始处理');
new Promise(() => {}).then(() => console.log('这行永远不会打印'));
setTimeout(() => { console.log('中途还在跑'); }, 10);
`

// 对照组：正常跑完，绝对不能被判成半路结束。
const normalScript = `
(async () => {
  await new Promise((r) => setTimeout(r, 20));
  console.log('正常跑完');
})();
`

// 对照组：正常但慢，且大量 await —— 探针不能因为 Promise 多就误报。
const slowScript = `
(async () => {
  let acc = 0;
  for (let i = 0; i < 5000; i++) acc += await Promise.resolve(1);
  await new Promise((r) => setTimeout(r, 30));
  console.log('慢任务跑完 ' + acc);
})();
`

// 对照组：脚本自己判定失败并设了退出码，探针不能把它覆盖掉。
const failedScript = `
console.log('业务失败');
process.exitCode = 3;
`

func runNodeWithProbe(t *testing.T, nodeBin, script string, detect bool) (string, int) {
	t.Helper()
	return runNodeWithProbeEnv(t, nodeBin, script, detect, nil)
}

func runNodeWithProbeEnv(t *testing.T, nodeBin, script string, detect bool, extraEnv []string) (string, int) {
	t.Helper()

	tempDir := t.TempDir()
	envFile := filepath.Join(tempDir, "env.json")
	if err := os.WriteFile(envFile, []byte(`{"PROBE_TEST":"1"}`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	preloadFile, err := writeNodePreloadScript(tempDir, envFile, map[string]string{}, detect)
	if err != nil {
		t.Fatalf("write node preload: %v", err)
	}
	scriptFile := filepath.Join(tempDir, "target.js")
	if err := os.WriteFile(scriptFile, []byte(script), 0o600); err != nil {
		t.Fatalf("write target script: %v", err)
	}

	cmd := exec.Command(nodeBin, "--require", preloadFile, scriptFile)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run node: %v, output=%s", err, string(out))
		}
		exitCode = exitErr.ExitCode()
	}
	return string(out), exitCode
}

func TestSilentExitProbeCatchesHangingPromise(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	out, exitCode := runNodeWithProbe(t, nodeBin, hangingScript, true)
	if !strings.Contains(out, "[任务疑似半路结束]") {
		t.Fatalf("expected probe to report a silent exit, got output=%q", out)
	}
	// 75 是探针专用的退出码，任务侧据此判失败。
	if exitCode != 75 {
		t.Fatalf("expected exit code 75 from probe, got %d, output=%q", exitCode, out)
	}
	// 脚本前半段确实跑过，说明它是「跑到一半」而不是「压根没起来」。
	if !strings.Contains(out, "开始处理") {
		t.Fatalf("expected script output before the hang, got %q", out)
	}
}

func TestSilentExitProbeStaysQuietOnNormalScripts(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	cases := []struct {
		name   string
		script string
		marker string
	}{
		{"正常结束", normalScript, "正常跑完"},
		{"正常但慢且大量 await", slowScript, "慢任务跑完 5000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, exitCode := runNodeWithProbe(t, nodeBin, tc.script, true)
			if strings.Contains(out, "[任务疑似半路结束]") {
				t.Fatalf("probe misfired on a healthy script, output=%q", out)
			}
			if exitCode != 0 {
				t.Fatalf("expected exit code 0, got %d, output=%q", exitCode, out)
			}
			if !strings.Contains(out, tc.marker) {
				t.Fatalf("expected %q in output, got %q", tc.marker, out)
			}
		})
	}
}

// 脚本自己设过退出码时，探针不能覆盖它 —— 那个码更有信息量。
func TestSilentExitProbeKeepsScriptOwnExitCode(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	out, exitCode := runNodeWithProbe(t, nodeBin, failedScript, true)
	if exitCode != 3 {
		t.Fatalf("expected script's own exit code 3 to survive, got %d, output=%q", exitCode, out)
	}
}

// 配置关掉时，探针连注入都不该发生：挂起脚本仍然退 0（保持旧行为）。
func TestSilentExitProbeDisabledLeavesBehaviorUnchanged(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	out, exitCode := runNodeWithProbe(t, nodeBin, hangingScript, false)
	if strings.Contains(out, "[任务疑似半路结束]") {
		t.Fatalf("probe should not be injected when disabled, output=%q", out)
	}
	if exitCode != 0 {
		t.Fatalf("expected unchanged exit code 0 when probe is off, got %d, output=%q", exitCode, out)
	}
}

// 探针注入与否只影响那一段代码，preload 的其余行为（env 注入）必须不变。
func TestSilentExitProbeDoesNotDisturbEnvInjection(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found")
	}

	out, exitCode := runNodeWithProbe(t, nodeBin, `process.stdout.write(process.env.PROBE_TEST || 'missing');`, true)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, output=%q", exitCode, out)
	}
	if !strings.Contains(out, "1") || strings.Contains(out, "missing") {
		t.Fatalf("expected env injection to still work with probe on, got %q", out)
	}
}
