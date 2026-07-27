package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"daidai-panel/testutil"
)

func TestDetectAutoInstallCandidate(t *testing.T) {
	t.Run("python alias", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".py", "ModuleNotFoundError: No module named 'Crypto.Hash'", t.TempDir())
		if candidate == nil {
			t.Fatal("expected python candidate")
		}
		if candidate.Manager != "python" || candidate.PackageName != "pycryptodome" {
			t.Fatalf("unexpected python candidate: %+v", candidate)
		}
	})

	t.Run("python Cryptodome alias maps to pycryptodomex", func(t *testing.T) {
		// 回归：from Cryptodome.PublicKey import RSA 失败时，必须 pip install pycryptodomex，
		// 不能原样 install Cryptodome（PyPI 没这个名字，会直接 404）。
		candidate := DetectAutoInstallCandidate(".py", "ModuleNotFoundError: No module named 'Cryptodome'", t.TempDir())
		if candidate == nil {
			t.Fatal("expected python candidate")
		}
		if candidate.Manager != "python" || candidate.PackageName != "pycryptodomex" {
			t.Fatalf("unexpected python candidate: %+v", candidate)
		}
	})

	t.Run("node package", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".js", "Error: Cannot find module 'axios'", t.TempDir())
		if candidate == nil {
			t.Fatal("expected node candidate")
		}
		if candidate.Manager != "nodejs" || candidate.PackageName != "axios" {
			t.Fatalf("unexpected node candidate: %+v", candidate)
		}
	})

	t.Run("node relative module ignored", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".js", "Error: Cannot find module './local-helper'", t.TempDir())
		if candidate != nil {
			t.Fatalf("expected nil candidate, got %+v", candidate)
		}
	})

	t.Run("go module requires go.mod", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.com/demo\n\ngo 1.25\n"), 0644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		candidate := DetectAutoInstallCandidate(".go", "main.go:5:2: no required module provides package github.com/gin-gonic/gin; to add it:\n\tgo get github.com/gin-gonic/gin", workDir)
		if candidate == nil {
			t.Fatal("expected go candidate")
		}
		if candidate.Manager != "go" || candidate.PackageName != "github.com/gin-gonic/gin" {
			t.Fatalf("unexpected go candidate: %+v", candidate)
		}
	})

	t.Run("go without module manifest is ignored", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".go", "main.go:5:2: no required module provides package github.com/gin-gonic/gin", t.TempDir())
		if candidate != nil {
			t.Fatalf("expected nil candidate, got %+v", candidate)
		}
	})

	t.Run("node hint npm install", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".js", `缺少有效 https-proxy-agent 模块！请运行: npm install https-proxy-agent`, t.TempDir())
		if candidate == nil {
			t.Fatal("expected node candidate from npm install hint")
		}
		if candidate.Manager != "nodejs" || candidate.PackageName != "https-proxy-agent" {
			t.Fatalf("unexpected candidate: %+v", candidate)
		}
	})

	t.Run("python hint pip install", func(t *testing.T) {
		candidate := DetectAutoInstallCandidate(".py", `请先安装依赖: pip install beautifulsoup4`, t.TempDir())
		if candidate == nil {
			t.Fatal("expected python candidate from pip install hint")
		}
		if candidate.Manager != "python" || candidate.PackageName != "beautifulsoup4" {
			t.Fatalf("unexpected candidate: %+v", candidate)
		}
	})

	t.Run("node native error takes priority over hint", func(t *testing.T) {
		output := "Error: Cannot find module 'axios'\nnpm install axios to fix"
		candidate := DetectAutoInstallCandidate(".js", output, t.TempDir())
		if candidate == nil {
			t.Fatal("expected candidate")
		}
		if candidate.PackageName != "axios" {
			t.Fatalf("expected axios, got %s", candidate.PackageName)
		}
	})

	t.Run("python local so not installed", func(t *testing.T) {
		workDir := t.TempDir()
		os.WriteFile(filepath.Join(workDir, "loader_v2.so"), []byte{}, 0644)
		candidate := DetectAutoInstallCandidate(".py", "ModuleNotFoundError: No module named 'loader_v2'", workDir)
		if candidate != nil {
			t.Fatalf("expected nil for local .so file, got %+v", candidate)
		}
	})
}

// 回归：v2.2.4 重构托管 venv bootstrap 时漏了 venvDir 参数，导致 venv 永远建不出来，
// 自动安装走到系统 pip 触发 Alpine/Debian 的 PEP 668 "externally-managed-environment"。
// 修复后 ResolvePipInstallCommand 必须在 venv 缺失时回 fallback flag。
func TestIsExternallyManagedErrorMatches(t *testing.T) {
	samples := []string{
		"error: externally-managed-environment\n\nThis environment is externally managed",
		"× This environment is externally managed",
		"  externally-managed-environment\n",
	}
	for _, s := range samples {
		if !IsExternallyManagedError([]byte(s)) {
			t.Fatalf("should detect PEP 668 in: %q", s)
		}
	}

	negatives := []string{
		"ERROR: Could not find a version that satisfies the requirement foo",
		"WARNING: pip is configured with locations that require TLS/SSL",
		"",
	}
	for _, s := range negatives {
		if IsExternallyManagedError([]byte(s)) {
			t.Fatalf("should NOT detect PEP 668 in: %q", s)
		}
	}
}

func TestBuildPipInstallArgsKeepsOrder(t *testing.T) {
	got := BuildPipInstallArgs([]string{"--break-system-packages", "--user"}, "requests")
	want := []string{"install", "--break-system-packages", "--user", "requests"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got=%v want=%v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("arg %d mismatch: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestBuildPipUninstallArgsDropsUserFlag(t *testing.T) {
	// --user 在 uninstall 时是非法的，必须被剥离；--break-system-packages 仍要保留。
	got := BuildPipUninstallArgs([]string{"--break-system-packages", "--user"}, "requests", "--no-deps")
	for _, arg := range got {
		if arg == "--user" {
			t.Fatalf("--user should be stripped from uninstall args, got %v", got)
		}
	}
	if got[0] != "uninstall" || got[1] != "-y" {
		t.Fatalf("uninstall args should start with `uninstall -y`, got %v", got)
	}

	hasBreak := false
	hasNoDeps := false
	hasPkg := false
	for _, arg := range got {
		switch arg {
		case "--break-system-packages":
			hasBreak = true
		case "--no-deps":
			hasNoDeps = true
		case "requests":
			hasPkg = true
		}
	}
	if !hasBreak || !hasNoDeps || !hasPkg {
		t.Fatalf("expected --break-system-packages, --no-deps, requests all present, got %v", got)
	}
}

func TestPipCommandForRequestedVersionDoesNotFallbackToPip3(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("PATH", t.TempDir())

	if _, err := NewPipCommandForPythonVersion("3.10", []string{"list"}); err == nil {
		t.Fatal("expected missing Python 3.10 to return an error instead of falling back to pip3")
	}
	if binary, _, _ := ResolvePipInstallCommandForPythonVersion("3.10"); binary != "" {
		t.Fatalf("expected no pip binary for missing Python 3.10, got %q", binary)
	}
}

func TestPipCommandRejectsUnsupportedSingleRuntimeVersion(t *testing.T) {
	testutil.SetupTestEnv(t)
	t.Setenv("DAIDAI_PYTHON_RUNTIME_MODE", "single")
	t.Setenv("DAIDAI_PYTHON_VERSION", "3.12")

	// 单版本镜像里不能偷偷把 3.10 的安装请求 fallback 到 pip3，
	// 否则用户看到的是 3.10 依赖，实际却装进了 3.12 环境。
	_, err := NewPipCommandForPythonVersion("3.10", []string{"list"})
	if err == nil {
		t.Fatal("expected unsupported Python 3.10 to return an error in single 3.12 image")
	}
	if !strings.Contains(err.Error(), "当前镜像不支持 Python 3.10") {
		t.Fatalf("expected unsupported runtime error, got %v", err)
	}
}
