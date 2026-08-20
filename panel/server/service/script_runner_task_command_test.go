package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"daidai-panel/config"
	"daidai-panel/testutil"
)

func TestParseCommandExecutionPlanSupportsTaskModesAndArgs(t *testing.T) {
	testutil.SetupTestEnv(t)

	spacedScript := filepath.Join(config.C.Data.ScriptsDir, "demo folder", "my script.py")
	if err := os.MkdirAll(filepath.Dir(spacedScript), 0755); err != nil {
		t.Fatalf("mkdir spaced script dir: %v", err)
	}
	if err := os.WriteFile(spacedScript, []byte("print('ok')\n"), 0644); err != nil {
		t.Fatalf("write spaced script: %v", err)
	}

	simpleScript := filepath.Join(config.C.Data.ScriptsDir, "simple.sh")
	if err := os.WriteFile(simpleScript, []byte("echo ok\n"), 0755); err != nil {
		t.Fatalf("write simple script: %v", err)
	}

	goScript := filepath.Join(config.C.Data.ScriptsDir, "worker.go")
	if err := os.WriteFile(goScript, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("write go script: %v", err)
	}

	mjsScript := filepath.Join(config.C.Data.ScriptsDir, "esm-demo.mjs")
	if err := os.WriteFile(mjsScript, []byte("console.log('esm ok')\n"), 0644); err != nil {
		t.Fatalf("write mjs script: %v", err)
	}

	t.Run("parses now mode with timeout override and passthrough args", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`task -m 5m demo folder/my script.py now -- -u whyour -p password`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse task now plan: %v", err)
		}

		if plan.Interpreter != "python3" {
			t.Fatalf("expected python3 interpreter, got %q", plan.Interpreter)
		}
		expectedInfo, err := os.Stat(spacedScript)
		if err != nil {
			t.Fatalf("stat expected spaced script: %v", err)
		}
		actualInfo, err := os.Stat(plan.FullPath)
		if err != nil {
			t.Fatalf("stat actual spaced script: %v", err)
		}
		if !os.SameFile(expectedInfo, actualInfo) {
			t.Fatalf("expected plan path %q to reference %q", plan.FullPath, spacedScript)
		}
		if plan.TimeoutOverride == nil || *plan.TimeoutOverride != 300 {
			t.Fatalf("expected timeout override 300, got %#v", plan.TimeoutOverride)
		}
		if !plan.SkipRandomDelay {
			t.Fatal("expected now mode to skip random delay")
		}
		if plan.Mode != commandModeNow {
			t.Fatalf("expected now mode, got %q", plan.Mode)
		}
		if !reflect.DeepEqual(plan.ScriptArgs, []string{"-u", "whyour", "-p", "password"}) {
			t.Fatalf("unexpected script args: %#v", plan.ScriptArgs)
		}
	})

	t.Run("parses conc mode with env and account spec", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`task simple.sh conc JD_COOKIE 1-2`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse task conc plan: %v", err)
		}

		if plan.Mode != commandModeConc {
			t.Fatalf("expected conc mode, got %q", plan.Mode)
		}
		if !plan.SuppressLiveOutput {
			t.Fatal("expected conc mode to suppress live output")
		}
		if plan.EnvName != "JD_COOKIE" {
			t.Fatalf("expected env name JD_COOKIE, got %q", plan.EnvName)
		}
		if plan.AccountSpec != "1-2" {
			t.Fatalf("expected account spec 1-2, got %q", plan.AccountSpec)
		}
	})

	t.Run("parses designated env selection", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`task simple.sh desi JD_COOKIE 2`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse task desi plan: %v", err)
		}

		if plan.Mode != commandModeDesi {
			t.Fatalf("expected desi mode, got %q", plan.Mode)
		}
		if plan.EnvName != "JD_COOKIE" {
			t.Fatalf("expected env name JD_COOKIE, got %q", plan.EnvName)
		}
		if plan.AccountSpec != "2" {
			t.Fatalf("expected account spec 2, got %q", plan.AccountSpec)
		}
	})

	t.Run("parses go task script", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`task worker.go now`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse go task plan: %v", err)
		}

		if plan.Interpreter != RuntimeIDYaegiGo {
			t.Fatalf("expected go runtime id, got %q", plan.Interpreter)
		}
		if plan.Mode != commandModeNow {
			t.Fatalf("expected now mode, got %q", plan.Mode)
		}
		if !plan.SkipRandomDelay {
			t.Fatal("expected go now mode to skip random delay")
		}
	})

	t.Run("parses direct go command", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`go worker.go`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse direct go plan: %v", err)
		}

		if plan.Interpreter != RuntimeIDYaegiGo {
			t.Fatalf("expected go runtime id, got %q", plan.Interpreter)
		}
		if filepath.Base(plan.FullPath) != "worker.go" {
			t.Fatalf("expected worker.go path, got %q", plan.FullPath)
		}
	})

	t.Run("parses mjs task script", func(t *testing.T) {
		plan, err := ParseCommandExecutionPlan(`task esm-demo.mjs now`, config.C.Data.ScriptsDir)
		if err != nil {
			t.Fatalf("parse mjs task plan: %v", err)
		}

		if plan.Interpreter != "node" {
			t.Fatalf("expected node interpreter, got %q", plan.Interpreter)
		}
		if filepath.Base(plan.FullPath) != "esm-demo.mjs" {
			t.Fatalf("expected esm-demo.mjs path, got %q", plan.FullPath)
		}
		if plan.Mode != commandModeNow {
			t.Fatalf("expected now mode, got %q", plan.Mode)
		}
	})

}

func TestBuildCmdForGoUsesYaegiRuntimeAndNeverGoRun(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	yaegi := filepath.Join(nativeDir, "libyaegi_go_exec.so")
	if err := os.WriteFile(yaegi, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write yaegi runtime: %v", err)
	}
	restore := SetRuntimeLocatorForTest(NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "libyaegi_go_exec.so"}}}))
	defer restore()

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "worker.go")
	if err := os.WriteFile(scriptPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go script: %v", err)
	}

	cmd, cleanup, err := buildCmd(&CommandExecutionPlan{
		Interpreter: RuntimeIDYaegiGo,
		FullPath:    scriptPath,
		WorkDir:     config.C.Data.ScriptsDir,
		ScriptArgs:  []string{"--flag"},
	}, config.C.Data.ScriptsDir, map[string]string{"A": "B"})
	if err != nil {
		t.Fatalf("build managed go command: %v", err)
	}
	defer cleanup()
	if cmd.Args[0] != yaegi {
		t.Fatalf("expected yaegi runtime executable %q, got args=%#v", yaegi, cmd.Args)
	}
	for _, arg := range cmd.Args {
		if arg == "run" {
			t.Fatalf("go runtime command must not use go run: %#v", cmd.Args)
		}
	}
}

func TestBuildCmdForGoPlanUsesRuntimeLocatorEntrypoint(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	yaegi := filepath.Join(nativeDir, "libyaegi_go_exec.so")
	if err := os.WriteFile(yaegi, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write yaegi runtime: %v", err)
	}
	restore := SetRuntimeLocatorForTest(NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDYaegiGo, Entrypoint: "libyaegi_go_exec.so", Capabilities: []string{"run-code"}}}}))
	defer restore()

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "worker.go")
	if err := os.WriteFile(scriptPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go script: %v", err)
	}

	cmd, cleanup, err := buildCmd(&CommandExecutionPlan{
		Interpreter: RuntimeIDYaegiGo,
		FullPath:    scriptPath,
		WorkDir:     config.C.Data.ScriptsDir,
		ScriptArgs:  []string{"--flag"},
	}, config.C.Data.ScriptsDir, map[string]string{"A": "B"})
	if err != nil {
		t.Fatalf("build go runtime command: %v", err)
	}
	defer cleanup()
	if cmd.Args[0] != yaegi {
		t.Fatalf("expected runtime entrypoint %q, got %#v", yaegi, cmd.Args)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "go run") {
		t.Fatalf("go runtime command must not use go run: %#v", cmd.Args)
	}
}

func TestBuildCmdRejectsGoBuilderRuntimeExecution(t *testing.T) {
	root := testutil.SetupTestEnv(t)
	nativeDir := filepath.Join(root, "native")
	if err := os.MkdirAll(nativeDir, 0o755); err != nil {
		t.Fatalf("mkdir native dir: %v", err)
	}
	builder := filepath.Join(nativeDir, "libgo_builder.so")
	if err := os.WriteFile(builder, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write go builder runtime: %v", err)
	}
	restore := SetRuntimeLocatorForTest(NewManifestRuntimeLocator(nativeDir, RuntimeManifest{Components: []RuntimeManifestComponent{{ID: RuntimeIDGoBuilderAndroidARM, Entrypoint: "libgo_builder.so", Capabilities: []string{"build-export"}}}}))
	defer restore()

	scriptPath := filepath.Join(config.C.Data.ScriptsDir, "worker.go")
	if err := os.WriteFile(scriptPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go script: %v", err)
	}

	_, _, err := buildCmd(&CommandExecutionPlan{
		Interpreter: RuntimeIDGoBuilderAndroidARM,
		FullPath:    scriptPath,
		WorkDir:     config.C.Data.ScriptsDir,
	}, config.C.Data.ScriptsDir, nil)
	if err == nil || !strings.Contains(err.Error(), "only supports build export") {
		t.Fatalf("expected go builder export-only error, got %v", err)
	}
}

func TestParseCommandExecutionPlanSupportsManagedDependencyCommands(t *testing.T) {
	testutil.SetupTestEnv(t)

	tests := []struct {
		name          string
		command       string
		wantCommand   string
		wantArgs      []string
		wantMode      commandExecutionMode
		wantNoDelay   bool
		wantPythonMod string
	}{
		{
			name:        "direct dependency command",
			command:     `dailycheckin --help`,
			wantCommand: "dailycheckin",
			wantArgs:    []string{"--help"},
			wantMode:    commandModeNormal,
		},
		{
			name:        "task dependency command keeps now mode",
			command:     `task dailycheckin now`,
			wantCommand: "dailycheckin",
			wantMode:    commandModeNow,
			wantNoDelay: true,
		},
		{
			name:        "task dependency command keeps passthrough args",
			command:     `task dailycheckin -- --config config.json`,
			wantCommand: "dailycheckin",
			wantArgs:    []string{"--config", "config.json"},
			wantMode:    commandModeNormal,
		},
		{
			name:          "python module command",
			command:       `python3 -m dailycheckin --help`,
			wantPythonMod: "dailycheckin",
			wantArgs:      []string{"--help"},
			wantMode:      commandModeNormal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := ParseCommandExecutionPlan(tt.command, config.C.Data.ScriptsDir)
			if err != nil {
				t.Fatalf("parse command plan: %v", err)
			}
			if plan.ManagedCommand != tt.wantCommand {
				t.Fatalf("expected managed command %q, got %q", tt.wantCommand, plan.ManagedCommand)
			}
			if plan.PythonModule != tt.wantPythonMod {
				t.Fatalf("expected python module %q, got %q", tt.wantPythonMod, plan.PythonModule)
			}
			if !reflect.DeepEqual(plan.ScriptArgs, tt.wantArgs) {
				t.Fatalf("unexpected command args: got=%#v want=%#v", plan.ScriptArgs, tt.wantArgs)
			}
			if plan.Mode != tt.wantMode {
				t.Fatalf("expected mode %q, got %q", tt.wantMode, plan.Mode)
			}
			if plan.SkipRandomDelay != tt.wantNoDelay {
				t.Fatalf("expected skip random delay %v, got %v", tt.wantNoDelay, plan.SkipRandomDelay)
			}
		})
	}
}

func TestRunCommandSupportsManagedDependencyCommand(t *testing.T) {
	testutil.SetupTestEnv(t)

	pythonVersion := DefaultPythonVersion()
	venvBin := resolveManagedVenvBin(ManagedPythonVenvDir(pythonVersion))
	envEcho := "echo daily:$DD_TEST_VALUE"
	argEcho := "echo arg:$1"
	if runtime.GOOS == "windows" {
		envEcho = "echo daily:%DD_TEST_VALUE%"
		argEcho = "echo arg:%1"
	}
	writeFakeExecutable(t, venvBin, "dailycheckin", []string{envEcho, argEcho})

	result, _, err := RunCommand(
		`dailycheckin --flag`,
		config.C.Data.ScriptsDir,
		5,
		map[string]string{
			"DAIDAI_PYTHON_VERSION": pythonVersion,
			"DD_TEST_VALUE":         "ok",
		},
		1024,
		nil,
	)
	if err != nil {
		t.Fatalf("run managed dependency command: %v", err)
	}
	if result.ReturnCode != 0 {
		t.Fatalf("expected return code 0, got %d output=%s", result.ReturnCode, result.Output)
	}
	if !strings.Contains(result.Output, "daily:ok") {
		t.Fatalf("expected dependency command to receive task env, output=%q", result.Output)
	}
	if !strings.Contains(result.Output, "arg:--flag") {
		t.Fatalf("expected dependency command to receive args, output=%q", result.Output)
	}
}

func requireUsableBash(t *testing.T) {
	t.Helper()

	// 同 handler/script_runtime_test.go 的那份：Windows 上的 Git Bash 会把
	// 路径里的反斜杠当转义符吃掉，脚本里的相对重定向落不了盘，用例必失败。
	// 这些用例验的是 POSIX shell 行为，只在 Linux 上有意义。
	if runtime.GOOS == "windows" {
		t.Skip("windows 下的 bash 不提供等价的 POSIX 路径语义，该用例只在 Linux 有意义")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
	if err := exec.Command(bashPath, "--version").Run(); err != nil {
		t.Skipf("bash is present but not usable: %v", err)
	}
}

func TestHookScriptsReceiveTaskScriptArgs(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)

	outputFile := filepath.Join(config.C.Data.ScriptsDir, "hook-args.out")
	hookPath := filepath.Join(config.C.Data.ScriptsDir, "task_before.sh")
	hookContent := []byte(`printf '%s|%s|%s' "$1" "$2" "$3" > hook-args.out` + "\n")
	if err := os.WriteFile(hookPath, hookContent, 0755); err != nil {
		t.Fatalf("write hook script: %v", err)
	}

	RunHookScript("task_before.sh", config.C.Data.ScriptsDir, nil, nil, "http://127.0.0.1:7890", "two words", "value3")

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read hook args output: %v", err)
	}
	if got, want := string(content), "http://127.0.0.1:7890|two words|value3"; got != want {
		t.Fatalf("expected hook args %q, got %q", want, got)
	}
}

func TestRunHookScriptHandlesLargeEnvWithoutExecArgLimit(t *testing.T) {
	testutil.SetupTestEnv(t)

	requireUsableBash(t)

	outputFile := filepath.Join(config.C.Data.ScriptsDir, "hook-large-env.out")
	hookPath := filepath.Join(config.C.Data.ScriptsDir, "task_before.sh")
	hookContent := []byte(`printf '%s|%s|%s' "${#BIG_ENV}" "$1" "$2" > hook-large-env.out` + "\n")
	if err := os.WriteFile(hookPath, hookContent, 0755); err != nil {
		t.Fatalf("write hook script: %v", err)
	}

	RunHookScript(
		"task_before.sh",
		config.C.Data.ScriptsDir,
		map[string]string{"BIG_ENV": strings.Repeat("x", 3*1024*1024)},
		nil,
		"arg-one",
		"arg two",
	)

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	if got, want := string(content), "3145728|arg-one|arg two"; got != want {
		t.Fatalf("expected large env and args %q, got %q", want, got)
	}
}

// desi / conc 的存在意义就是「只跑选中的账号」。如果它们仍然把全量 cookie 塞进
// 子进程环境，账号越多越容易撞上 execve 的 MAX_ARG_STRLEN，分批执行也就白分了。
func TestDesiAndConcNarrowEnvToSelectedAccounts(t *testing.T) {
	full := "cookie-one&cookie-two&cookie-three&cookie-four"
	envVars := map[string]string{
		"JD_COOKIE": full,
		"OTHER":     "keep-me",
	}

	desiPlan := &CommandExecutionPlan{Mode: commandModeDesi, EnvName: "JD_COOKIE", AccountSpec: "2 3"}
	desiEnv, err := applyCommandEnvOverrides(desiPlan, envVars)
	if err != nil {
		t.Fatalf("apply desi env overrides: %v", err)
	}
	if got, want := desiEnv["JD_COOKIE"], "cookie-two&cookie-three"; got != want {
		t.Fatalf("expected desi env narrowed to %q, got %q", want, got)
	}
	if desiEnv["OTHER"] != "keep-me" {
		t.Fatalf("expected unrelated env untouched, got %q", desiEnv["OTHER"])
	}
	if envVars["JD_COOKIE"] != full {
		t.Fatalf("expected source env map untouched, got %q", envVars["JD_COOKIE"])
	}

	concPlan := &CommandExecutionPlan{Mode: commandModeConc, EnvName: "JD_COOKIE", AccountSpec: "1-max"}
	selections, err := resolveTaskAccountSelections(envVars, concPlan.EnvName, concPlan.AccountSpec)
	if err != nil {
		t.Fatalf("resolve conc selections: %v", err)
	}
	if len(selections) != 4 {
		t.Fatalf("expected 4 conc selections, got %d", len(selections))
	}
	concEnv := applyConcurrentAccountEnv(concPlan, envVars, selections[2])
	if got, want := concEnv["JD_COOKIE"], "cookie-three"; got != want {
		t.Fatalf("expected conc env narrowed to %q, got %q", want, got)
	}
}

func TestResolveTaskAccountSelections(t *testing.T) {
	envVars := map[string]string{
		"JD_COOKIE": "a&b&c",
	}

	selections, err := resolveTaskAccountSelections(envVars, "JD_COOKIE", "1-2 3")
	if err != nil {
		t.Fatalf("resolve task account selections: %v", err)
	}

	got := make([]string, 0, len(selections))
	for _, selection := range selections {
		got = append(got, selection.Value)
	}

	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("unexpected selected values: %#v", got)
	}
}

func TestResolveTaskAccountSelectionsSupportsDoubleAmpersandSeparator(t *testing.T) {
	envVars := map[string]string{
		"JD_COOKIE": "pt_key=one&a=1&&pt_key=two&b=2",
	}

	selections, err := resolveTaskAccountSelections(envVars, "JD_COOKIE", "1-2")
	if err != nil {
		t.Fatalf("resolve task account selections with double ampersand: %v", err)
	}

	got := make([]string, 0, len(selections))
	for _, selection := range selections {
		got = append(got, selection.Value)
	}

	if !reflect.DeepEqual(got, []string{"pt_key=one&a=1", "pt_key=two&b=2"}) {
		t.Fatalf("unexpected selected values: %#v", got)
	}
}

func TestResolveTaskAccountSelectionsSupportsEscapedAmpersands(t *testing.T) {
	envVars := map[string]string{
		"JD_COOKIE": `pt_key=one\&a=1&pt_key=two\&b=2`,
	}

	selections, err := resolveTaskAccountSelections(envVars, "JD_COOKIE", "2")
	if err != nil {
		t.Fatalf("resolve task account selections with escaped ampersands: %v", err)
	}

	if len(selections) != 1 || selections[0].Value != "pt_key=two&b=2" {
		t.Fatalf("unexpected selected values: %#v", selections)
	}
}

func TestApplyCommandEnvOverridesForDesi(t *testing.T) {
	plan := &CommandExecutionPlan{
		Mode:        commandModeDesi,
		EnvName:     "JD_COOKIE",
		AccountSpec: "2-3",
	}
	envVars := map[string]string{
		"JD_COOKIE": "a&b&c",
	}

	overridden, err := applyCommandEnvOverrides(plan, envVars)
	if err != nil {
		t.Fatalf("apply designated env overrides: %v", err)
	}
	if overridden["JD_COOKIE"] != "b&c" {
		t.Fatalf("expected designated env values b&c, got %q", overridden["JD_COOKIE"])
	}
	if overridden["envParam"] != "JD_COOKIE" {
		t.Fatalf("expected envParam JD_COOKIE, got %q", overridden["envParam"])
	}
	if overridden["numParam"] != "2 3" {
		t.Fatalf("expected numParam '2 3', got %q", overridden["numParam"])
	}
}

func TestApplyCommandEnvOverridesForDesiPreservesAmpersandsInSelectedValues(t *testing.T) {
	plan := &CommandExecutionPlan{
		Mode:        commandModeDesi,
		EnvName:     "JD_COOKIE",
		AccountSpec: "1-2",
	}
	envVars := map[string]string{
		"JD_COOKIE": "pt_key=one&a=1&&pt_key=two&b=2",
	}

	overridden, err := applyCommandEnvOverrides(plan, envVars)
	if err != nil {
		t.Fatalf("apply designated env overrides with ampersands: %v", err)
	}
	if overridden["JD_COOKIE"] != "pt_key=one&a=1&&pt_key=two&b=2" {
		t.Fatalf("expected designated env values to preserve embedded ampersands, got %q", overridden["JD_COOKIE"])
	}
}
