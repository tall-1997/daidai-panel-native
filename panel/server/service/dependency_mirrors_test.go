package service

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizePipEnvStripsConflictingVars(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"PIP_HOME=/opt/pip-home",
		"PIP_PREFIX=/opt/pip-prefix",
		"PIP_TARGET=/opt/pip-target",
		"PIP_ROOT=/opt/pip-root",
		"PIP_USER=true",
		"PIP_INSTALL_OPTION=--home=/opt/foo",
		"PYTHONUSERBASE=/opt/userbase",
		"PYTHONPATH=/old/site-packages",
		"PYTHONHOME=/old/python",
		"PIP_INDEX_URL=https://example.com/simple",
		"PIP_TRUSTED_HOST=example.com",
		"LANG=zh_CN.UTF-8",
		"NOT_A_KV_PAIR",
	}

	cleaned := SanitizePipEnv(base)

	mustKeep := []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"PIP_INDEX_URL=https://example.com/simple",
		"PIP_TRUSTED_HOST=example.com",
		"LANG=zh_CN.UTF-8",
		"NOT_A_KV_PAIR",
	}
	for _, want := range mustKeep {
		found := false
		for _, entry := range cleaned {
			if entry == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected sanitized env to retain %q, got %v", want, cleaned)
		}
	}

	mustDropPrefixes := []string{
		"PIP_HOME=",
		"PIP_PREFIX=",
		"PIP_TARGET=",
		"PIP_ROOT=",
		"PIP_USER=",
		"PIP_INSTALL_OPTION=",
		"PYTHONUSERBASE=",
		"PYTHONPATH=",
		"PYTHONHOME=",
	}
	for _, prefix := range mustDropPrefixes {
		for _, entry := range cleaned {
			if strings.HasPrefix(entry, prefix) {
				t.Errorf("expected %q to be stripped, but found %q", prefix, entry)
			}
		}
	}
}

func TestSanitizePipEnvIsCaseInsensitiveForKeys(t *testing.T) {
	cleaned := SanitizePipEnv([]string{"pip_prefix=/opt/x", "Pip_Home=/opt/y"})
	for _, entry := range cleaned {
		if strings.Contains(strings.ToLower(entry), "pip_prefix=") || strings.Contains(strings.ToLower(entry), "pip_home=") {
			t.Fatalf("expected case-insensitive strip, got entry %q", entry)
		}
	}
}

func TestPipInstallEnvRemovesConflictingVarsAndInjectsMirror(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"PIP_PREFIX=/opt/pip-prefix",
		"PIP_HOME=/opt/pip-home",
	}
	env := PipInstallEnv(base, "https://example.com/simple")

	for _, entry := range env {
		if strings.HasPrefix(entry, "PIP_PREFIX=") || strings.HasPrefix(entry, "PIP_HOME=") {
			t.Fatalf("expected PIP_PREFIX/PIP_HOME stripped, got %q", entry)
		}
	}

	hasIndex := false
	for _, entry := range env {
		if entry == "PIP_INDEX_URL=https://example.com/simple" {
			hasIndex = true
		}
	}
	if !hasIndex {
		t.Fatalf("expected PIP_INDEX_URL to be injected, env=%v", env)
	}
}

func TestEffectivePipMirrorFallsBackToDefaultAcceleratedMirror(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty uses default", input: "", want: DefaultPipMirror},
		{name: "official uses default", input: "https://pypi.org/simple", want: DefaultPipMirror},
		{name: "custom mirror preserved", input: "https://pypi.tuna.tsinghua.edu.cn/simple", want: "https://pypi.tuna.tsinghua.edu.cn/simple"},
	}

	for _, tc := range cases {
		if got := EffectivePipMirror(tc.input); got != tc.want {
			t.Fatalf("%s: EffectivePipMirror(%q) = %q, want %q", tc.name, tc.input, got, tc.want)
		}
	}
}

func TestEffectiveNpmMirrorFallsBackToDefaultAcceleratedMirror(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty uses default", input: "", want: "https://registry.npmmirror.com/"},
		{name: "official uses default", input: "https://registry.npmjs.org/", want: "https://registry.npmmirror.com/"},
		{name: "custom mirror preserved", input: "https://mirrors.cloud.tencent.com/npm/", want: "https://mirrors.cloud.tencent.com/npm/"},
	}

	for _, tc := range cases {
		if got := EffectiveNpmMirror(tc.input); got != tc.want {
			t.Fatalf("%s: EffectiveNpmMirror(%q) = %q, want %q", tc.name, tc.input, got, tc.want)
		}
	}
}

func TestSetPipMirrorWritesAndClearsConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if err := SetPipMirror("https://mirrors.aliyun.com/pypi/simple"); err != nil {
		t.Fatalf("set pip mirror: %v", err)
	}
	if got := CurrentPipMirror(); got != "https://mirrors.aliyun.com/pypi/simple" {
		t.Fatalf("expected saved pip mirror, got %q", got)
	}

	if err := SetPipMirror(""); err != nil {
		t.Fatalf("clear pip mirror: %v", err)
	}
	if got := CurrentPipMirror(); got != "" {
		t.Fatalf("expected cleared pip mirror, got %q", got)
	}
}

func TestSetNpmMirrorWritesAndClearsConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)

	if err := SetNpmMirror("https://mirrors.cloud.tencent.com/npm/"); err != nil {
		t.Fatalf("set npm mirror: %v", err)
	}
	if got := CurrentNpmMirror(); got != "https://mirrors.cloud.tencent.com/npm/" {
		t.Fatalf("expected saved npm mirror, got %q", got)
	}

	if err := SetNpmMirror(""); err != nil {
		t.Fatalf("clear npm mirror: %v", err)
	}
	if got := CurrentNpmMirror(); got != "" {
		t.Fatalf("expected cleared npm mirror, got %q", got)
	}
}
