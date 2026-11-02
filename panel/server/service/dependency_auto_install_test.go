package service

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
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

	// issue #166：httpx 缺 h2 时的原文报错，提示里的 extras 必须一起捕获，否则只装了 httpx、h2 仍然缺。
	t.Run("python hint keeps extras from httpx h2 error", func(t *testing.T) {
		output := "ImportError: Using http2=True, but the 'h2' package is not installed. " +
			"Make sure to install httpx using `pip install httpx[http2]`."
		candidate := DetectAutoInstallCandidate(".py", output, t.TempDir())
		if candidate == nil {
			t.Fatal("expected python candidate from httpx h2 hint")
		}
		if candidate.PackageName != "httpx[http2]" || candidate.RecordName != "httpx[http2]" {
			t.Fatalf("expected httpx[http2] with extras kept, got %+v", candidate)
		}
	})

	t.Run("python hint keeps quoted extras", func(t *testing.T) {
		cases := []struct{ output, want string }{
			{`请运行: pip install "httpx[http2]"`, "httpx[http2]"},
			{`请运行: pip3 install 'httpx[http2,socks]'`, "httpx[http2,socks]"},
			{`请运行: pip install "requests"`, "requests"},
		}
		for _, tc := range cases {
			candidate := DetectAutoInstallCandidate(".py", tc.output, t.TempDir())
			if candidate == nil {
				t.Fatalf("expected python candidate for %q", tc.output)
			}
			if candidate.PackageName != tc.want {
				t.Fatalf("output %q: expected %q, got %q", tc.output, tc.want, candidate.PackageName)
			}
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

// issue #166：已有 httpx 记录，自动安装装的是 httpx[http2] 时，登记要连名字一起改成带 extras 的写法，
// 否则重建容器后按旧名字重装又会丢掉 h2。仍是一包一条，不新建记录。
func TestRecordAutoInstalledDepRenamesWhenExtrasNotCovered(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 旧记录带版本锁定：改名要合并成 httpx[http2]==0.27.0，不能整串换成 httpx[http2] 把锁定丢掉。
	existing := model.Dependency{Type: model.DepTypePython, Name: "httpx==0.27.0", PythonVersion: "3.12", Status: model.DepStatusInstalled}
	if err := database.DB.Create(&existing).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	RecordAutoInstalledDepForPythonVersion(model.DepTypePython, "httpx[http2]", "install log", "3.12")

	var stored []model.Dependency
	if err := database.DB.Where("type = ?", model.DepTypePython).Find(&stored).Error; err != nil {
		t.Fatalf("reload dependencies: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("expected still one python dependency row, got %+v", stored)
	}
	if stored[0].ID != existing.ID || stored[0].Name != "httpx[http2]==0.27.0" {
		t.Fatalf("expected existing row renamed to merged httpx[http2]==0.27.0, got %+v", stored[0])
	}
	if stored[0].Status != model.DepStatusInstalled || stored[0].Log != "install log" {
		t.Fatalf("expected status/log updated as before, got %+v", stored[0])
	}
}

// 反向守住：旧名字的 extras 已经覆盖本次写法时不改名，免得把 httpx[http2] 改回 httpx、丢掉 extras。
func TestRecordAutoInstalledDepKeepsNameWhenExtrasCovered(t *testing.T) {
	testutil.SetupTestEnv(t)

	existing := model.Dependency{Type: model.DepTypePython, Name: "httpx[http2]", PythonVersion: "3.12", Status: model.DepStatusFailed}
	if err := database.DB.Create(&existing).Error; err != nil {
		t.Fatalf("seed dependency: %v", err)
	}

	RecordAutoInstalledDepForPythonVersion(model.DepTypePython, "httpx", "install log", "3.12")

	var stored model.Dependency
	if err := database.DB.First(&stored, existing.ID).Error; err != nil {
		t.Fatalf("reload dependency: %v", err)
	}
	if stored.Name != "httpx[http2]" {
		t.Fatalf("expected name kept as httpx[http2], got %q", stored.Name)
	}
	if stored.Status != model.DepStatusInstalled {
		t.Fatalf("expected status installed, got %q", stored.Status)
	}
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
	want := []string{"install", "--no-cache-dir", "--break-system-packages", "--user", "requests"}
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
	//
	// 这条用例以前把 --no-deps 断言成「必须存在」，等于把 #150 的 bug 钉成了契约：pip uninstall 没有
	// 这个选项（pip 24.0 与 26.1.2 实测都报 `no such option: --no-deps`、退出码 2、一个包都不卸），
	// 强制 / 批量卸载因此多年只删了记录。本次有实测依据地反转：逐项断言完整参数，多塞任何选项都会变红。
	got := BuildPipUninstallArgs([]string{"--break-system-packages", "--user"}, "requests")
	want := []string{"uninstall", "-y", "--break-system-packages", "requests"}
	if !slices.Equal(got, want) {
		t.Fatalf("uninstall args mismatch: got=%q want=%q", got, want)
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
