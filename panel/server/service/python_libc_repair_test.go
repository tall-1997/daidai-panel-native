package service

import (
	"encoding/csv"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"daidai-panel/testutil"
)

// pythonLibcRepairFixture 描述注入的解释器 C 库与 pip 行为，全程不读真 ELF、不真跑 pip。
// 假 .so 的文件内容就是它的 DT_NEEDED 列表（空格分隔），由注入的 elfImportedLibrariesFunc 读出。
type pythonLibcRepairFixture struct {
	libc    string   // 注入的当前解释器 C 库，空串表示判不出
	failing []string // 这些 requirement 的假 pip 以非 0 退出
}

// pythonLibcRepairCalls 记录注入的函数被怎样调用。repairPythonVenvForLibcChange 是同步的，不存在并发访问。
type pythonLibcRepairCalls struct {
	libcProbes []string // 传给解释器判定的路径
	elfReads   int
	commands   []string // "<Python 版本> <requirement>"
}

func stubPythonLibcRepair(t *testing.T, f pythonLibcRepairFixture) *pythonLibcRepairCalls {
	t.Helper()
	calls := &pythonLibcRepairCalls{}
	okPip := writeFakeExecutable(t, t.TempDir(), "fake-pip-ok", []string{"echo reinstalled"})
	failPip := writeFakeExecutable(t, t.TempDir(), "fake-pip-fail", []string{"echo no matching distribution", "exit 1"})

	originalGOOS, originalLibc, originalELF, originalCmd := pythonLibcRepairGOOS, pythonInterpreterLibcFunc, elfImportedLibrariesFunc, newPythonLibcRepairCommandFunc
	t.Cleanup(func() {
		pythonLibcRepairGOOS, pythonInterpreterLibcFunc, elfImportedLibrariesFunc, newPythonLibcRepairCommandFunc = originalGOOS, originalLibc, originalELF, originalCmd
	})
	pythonInterpreterLibcFunc = func(path string) string {
		calls.libcProbes = append(calls.libcProbes, path)
		return f.libc
	}
	elfImportedLibrariesFunc = func(path string) ([]string, error) {
		calls.elfReads++
		data, err := os.ReadFile(path)
		return strings.Fields(string(data)), err
	}
	newPythonLibcRepairCommandFunc = func(version, requirement string) (*exec.Cmd, error) {
		calls.commands = append(calls.commands, version+" "+requirement)
		if slices.Contains(f.failing, requirement) {
			return exec.Command(failPip), nil
		}
		return exec.Command(okPip), nil
	}
	return calls
}

// seedPythonLibcVenv 在 deps/python/<version> 下搭一个假 venv：可执行的 bin/python 与空的 site-packages。
func seedPythonLibcVenv(t *testing.T, version string) string {
	t.Helper()
	venvDir := ManagedPythonVenvDir(version)
	sitePackages := filepath.Join(venvDir, "lib", "python"+version, "site-packages")
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		t.Fatalf("mkdir site-packages: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(venvDir, "bin"), 0o755); err != nil {
		t.Fatalf("mkdir venv bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(venvDir, "bin", "python"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake python: %v", err)
	}
	return sitePackages
}

// seedPythonLibcDist 放一个发行包：files 是「相对 site-packages 的斜杠路径 → 假 .so 的 NEEDED 列表」。
// distInfo 非空时用 encoding/csv 写 RECORD（路径带逗号会被加上引号）；为空表示孤儿文件，不进任何 RECORD。
func seedPythonLibcDist(t *testing.T, sitePackages, distInfo string, files map[string]string) {
	t.Helper()
	var rows [][]string
	for rel, needed := range files {
		path := filepath.Join(sitePackages, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(needed), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
		rows = append(rows, []string{rel, "sha256=test", "1"})
	}
	if distInfo == "" {
		return
	}
	dir := filepath.Join(sitePackages, distInfo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", distInfo, err)
	}
	record, err := os.Create(filepath.Join(dir, "RECORD"))
	if err != nil {
		t.Fatalf("create RECORD: %v", err)
	}
	writer := csv.NewWriter(record)
	_ = writer.WriteAll(append(rows, []string{distInfo + "/RECORD", "", ""}))
	if err := record.Close(); err != nil {
		t.Fatalf("close RECORD: %v", err)
	}
}

func readTestPythonLibcMarker(t *testing.T, version string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ManagedPythonVenvDir(version), pythonLibcMarkerFileName))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read python libc marker: %v", err)
	}
	return strings.TrimSpace(string(data)), true
}

const (
	testMuslNeeded  = "libc.musl-x86_64.so.1"
	testGlibcNeeded = "libpthread.so.0 libc.so.6"
)

func TestPythonLibcRepairFixesOnlyForeignPackages(t *testing.T) {
	cases := []struct {
		name         string
		fixture      pythonLibcRepairFixture
		marker       string // 预先写入的标记，空串表示没有
		seed         func(t *testing.T, sp string)
		wantCommands []string
		wantMarker   string // 期望最终的标记内容，空串表示不应写标记
		wantLog      string
	}{
		{
			name:    "marker matches current libc: nothing scanned",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			marker:  "glibc",
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
			},
			wantMarker: "glibc",
		},
		{
			name:    "no foreign .so: marker written without pip",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "numpy-2.1.0.dist-info", map[string]string{"numpy/core/_m.cpython-312-x86_64-linux-gnu.so": testGlibcNeeded})
				seedPythonLibcDist(t, sp, "requests-2.32.3.dist-info", map[string]string{"requests/__init__.py": ""})
			},
			wantMarker: "glibc",
		},
		{
			name:    "several foreign .so of one package: repaired once",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			marker:  "musl",
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{
					"Crypto/Util/_cpuid_c.abi3.so":   testMuslNeeded,
					"Crypto/Cipher/_raw_aes.abi3.so": testMuslNeeded,
					"Crypto/Cipher/_pure.abi3.so":    "",
				})
			},
			wantCommands: []string{"3.12 pycryptodome==3.23.0"},
			wantMarker:   "glibc",
			wantLog:      "pycryptodome==3.23.0",
		},
		{
			name:    "two foreign packages repaired one by one, healthy package untouched",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
				// musl 工具链编出来的扩展可能 NEEDED 的是 libc.so，glibc 下同样算外来。
				seedPythonLibcDist(t, sp, "charset_normalizer-3.5.1.dist-info", map[string]string{"charset_normalizer/md.cpython-312-x86_64-linux-musl.so": "libc.so"})
				seedPythonLibcDist(t, sp, "numpy-2.1.0.dist-info", map[string]string{"numpy/_m.so": testGlibcNeeded})
			},
			wantCommands: []string{"3.12 charset_normalizer==3.5.1", "3.12 pycryptodome==3.23.0"},
			wantMarker:   "glibc",
		},
		{
			name:    "current musl: glibc extension counts as foreign",
			fixture: pythonLibcRepairFixture{libc: "musl"},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "lxml-5.3.0.dist-info", map[string]string{"lxml/etree.cpython-312-x86_64-linux-gnu.so": testGlibcNeeded})
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
			},
			wantCommands: []string{"3.12 lxml==5.3.0"},
			wantMarker:   "musl",
		},
		{
			name:    "one package fails: others still tried, marker not written",
			fixture: pythonLibcRepairFixture{libc: "glibc", failing: []string{"charset_normalizer==3.5.1"}},
			marker:  "musl",
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "charset_normalizer-3.5.1.dist-info", map[string]string{"charset_normalizer/md.so": testMuslNeeded})
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
			},
			wantCommands: []string{"3.12 charset_normalizer==3.5.1", "3.12 pycryptodome==3.23.0"},
			wantMarker:   "musl",
			wantLog:      "no matching distribution",
		},
		{
			name:    "orphan foreign .so: logged only, marker written",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "", map[string]string{"leftover/_old.abi3.so": testMuslNeeded})
				seedPythonLibcDist(t, sp, "requests-2.32.3.dist-info", map[string]string{"requests/__init__.py": ""})
			},
			wantMarker: "glibc",
			wantLog:    "1 个按另一种 C 库编译的 .so 不属于任何已安装的包",
		},
		{
			name:    "RECORD path quoted because of comma",
			fixture: pythonLibcRepairFixture{libc: "glibc"},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "weird_pkg-1.0.dist-info", map[string]string{"weird,dir/_x.abi3.so": testMuslNeeded})
				record, err := os.ReadFile(filepath.Join(sp, "weird_pkg-1.0.dist-info", "RECORD"))
				if err != nil || !strings.Contains(string(record), `"weird,dir/_x.abi3.so"`) {
					t.Fatalf("expected quoted path in RECORD, got %q (err=%v)", record, err)
				}
			},
			wantCommands: []string{"3.12 weird_pkg==1.0"},
			wantMarker:   "glibc",
		},
		{
			name:    "interpreter libc unknown: nothing done",
			fixture: pythonLibcRepairFixture{libc: ""},
			seed: func(t *testing.T, sp string) {
				seedPythonLibcDist(t, sp, "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.SetupTestEnv(t)
			calls := stubPythonLibcRepair(t, tc.fixture)
			logs := captureStandardLog(t)
			sitePackages := seedPythonLibcVenv(t, "3.12")
			tc.seed(t, sitePackages)
			if tc.marker != "" {
				if err := os.WriteFile(filepath.Join(ManagedPythonVenvDir("3.12"), pythonLibcMarkerFileName), []byte(tc.marker+"\n"), 0o644); err != nil {
					t.Fatalf("write marker: %v", err)
				}
			}

			repairPythonVenvForLibcChange("3.12")

			if len(calls.libcProbes) != 1 || filepath.Base(calls.libcProbes[0]) != "python" {
				t.Fatalf("expected one libc probe on the venv python, got %v", calls.libcProbes)
			}
			if !slices.Equal(calls.commands, tc.wantCommands) {
				t.Fatalf("expected pip commands %v, got %v\nlogs:\n%s", tc.wantCommands, calls.commands, logs.String())
			}
			if (tc.marker == tc.fixture.libc || tc.fixture.libc == "") && calls.elfReads != 0 {
				t.Fatalf("expected no .so to be scanned, got %d ELF reads", calls.elfReads)
			}
			got, ok := readTestPythonLibcMarker(t, "3.12")
			if tc.wantMarker == "" && ok {
				t.Fatalf("expected no marker, got %q", got)
			}
			if tc.wantMarker != "" && got != tc.wantMarker {
				t.Fatalf("expected marker %q, got %q (exists=%v)", tc.wantMarker, got, ok)
			}
			if tc.wantLog != "" && !strings.Contains(logs.String(), tc.wantLog) {
				t.Fatalf("expected log to contain %q, got:\n%s", tc.wantLog, logs.String())
			}
		})
	}
}

// 没建 venv 的版本直接跳过，连解释器判定都不做。
func TestPythonLibcRepairSkipsMissingVenv(t *testing.T) {
	testutil.SetupTestEnv(t)
	calls := stubPythonLibcRepair(t, pythonLibcRepairFixture{libc: "glibc"})

	repairPythonVenvForLibcChange("3.12")

	if len(calls.libcProbes) != 0 || len(calls.commands) != 0 {
		t.Fatalf("expected nothing done for missing venv, got %+v", calls)
	}
}

// 非 Linux（Windows / 二进制版用户自己管 Python）入口直接返回，不起后台任务。
func TestPythonLibcRepairSkipsNonLinux(t *testing.T) {
	testutil.SetupTestEnv(t)
	calls := stubPythonLibcRepair(t, pythonLibcRepairFixture{libc: "glibc"})
	seedPythonLibcDist(t, seedPythonLibcVenv(t, "3.12"), "pycryptodome-3.23.0.dist-info", map[string]string{"Crypto/_x.abi3.so": testMuslNeeded})
	pythonLibcRepairGOOS = "windows"

	RepairPythonPackagesForLibcChange()

	if len(calls.libcProbes) != 0 || len(calls.commands) != 0 {
		t.Fatalf("expected nothing done on non-linux, got %+v", calls)
	}
	if _, ok := readTestPythonLibcMarker(t, "3.12"); ok {
		t.Fatal("expected no marker on non-linux")
	}
}
