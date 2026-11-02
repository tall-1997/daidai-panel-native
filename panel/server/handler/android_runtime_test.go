package handler

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAndroidSupportedRecognizesMagiskEnvMarker(t *testing.T) {
	t.Setenv("DAIDAI_MAGISK_MODULE", "1")

	if !androidSupported() {
		t.Fatal("expected androidSupported to recognize Magisk env marker")
	}
}

func TestResolveAndroidRuntimeBinDirPrefersEnvOverride(t *testing.T) {
	t.Setenv("DAIDAI_ANDROID_RUNTIME_BIN_DIR", "/data/adb/daidai-panel/custom-bin")

	got := resolveAndroidRuntimeBinDir()
	if got != "/data/adb/daidai-panel/custom-bin" {
		t.Fatalf("expected env override bin dir, got %q", got)
	}
}

func TestResolveAndroidRuntimeBinDirFallsBackToDefault(t *testing.T) {
	got := resolveAndroidRuntimeBinDir()
	if got != defaultAndroidRuntimeBinDir {
		t.Fatalf("expected default android runtime bin dir %q, got %q", defaultAndroidRuntimeBinDir, got)
	}
}

func androidRuntimeCandidateIndex(candidates []string, want string) int {
	for i, c := range candidates {
		if c == want {
			return i
		}
	}
	return -1
}

// 只测候选列表本身，不在文件系统上造布局：候选是写死的系统绝对路径，没法指到临时目录；
// Windows 上 os.Stat 也读不出可执行位，造了文件 probeRuntime 照样会跳过。
// 期望值一律用 filepath.Join 拼，Windows 与 Linux 上都和实现的拼法一致。
func TestAndroidRuntimeCandidatesProbeUsrLocalBinBeforeUsrBin(t *testing.T) {
	binDir := defaultAndroidRuntimeBinDir
	candidates := androidRuntimeCandidates(binDir, "node")

	// 一键安装的那份在 service.sh 的 PATH 里排最前，装了就是它在生效，卡片也必须先认它
	if len(candidates) == 0 || candidates[0] != filepath.Join(binDir, "node", "bin", "node") {
		t.Fatalf("一键安装目录 %s 必须是 node 的第一个候选，实际 %q", filepath.Join(binDir, "node", "bin", "node"), candidates)
	}

	// Magisk Debian 版的 Node 装在 /usr/local/bin：漏了它，卡片会把容器里现成的 Node 判成「未安装」
	usrLocal := androidRuntimeCandidateIndex(candidates, filepath.Join("/usr/local/bin", "node"))
	if usrLocal < 0 {
		t.Fatalf("node 候选里缺少 %s（Magisk Debian 版的安装位置），实际 %q", filepath.Join("/usr/local/bin", "node"), candidates)
	}
	usrBin := androidRuntimeCandidateIndex(candidates, filepath.Join("/usr/bin", "node"))
	if usrBin < 0 {
		t.Fatalf("node 候选里缺少 %s（Alpine 版 apk 的安装位置），实际 %q", filepath.Join("/usr/bin", "node"), candidates)
	}
	// 与 service.sh 的 PATH 顺序一致：两处都有时卡片显示的必须是实际运行的那个
	if usrLocal > usrBin {
		t.Fatalf("/usr/local/bin/node 必须排在 /usr/bin/node 之前（与容器 PATH 顺序一致），实际 %q", candidates)
	}
}

var (
	androidRuntimeTarCmdRe   = regexp.MustCompile(`(^|[\s;&|(])tar\s`)
	androidRuntimeTarDirRe   = regexp.MustCompile(`(?:^|\s)-C\s+"?([^"\s]+)"?`)
	androidRuntimeTarStripRe = regexp.MustCompile(`--strip-components=(\d+)`)
)

// 漂移门禁：Magisk Debian 版把 Node 官方包解压到哪，一键安装卡片就得去哪找。
// 以后改了 customize.sh 的解压目录却没同步 androidRuntimeCandidates，卡片会再次把容器里的 Node 判成「未安装」。
// 抽不到解压命令直接判失败，不能让门禁空转。
func TestAndroidRuntimeCandidatesCoverMagiskDebianNodeInstallDir(t *testing.T) {
	text := readMagiskCustomizeScript(t)
	candidates := androidRuntimeCandidates(defaultAndroidRuntimeBinDir, "node")
	usrBin := androidRuntimeCandidateIndex(candidates, filepath.Join("/usr/bin", "node"))

	found := 0
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "${NODE_TARBALL}") || !androidRuntimeTarCmdRe.MatchString(trimmed) {
			continue
		}
		found++
		source := fmt.Sprintf("Magisk/customize.sh:%d", i+1)

		dirMatch := androidRuntimeTarDirRe.FindStringSubmatch(trimmed)
		if dirMatch == nil || !strings.HasPrefix(dirMatch[1], "/") {
			t.Fatalf("%s 的 Node 解压命令抽不出绝对路径的 -C 目标目录：写法变了请同步改这条门禁 %q", source, trimmed)
		}
		stripMatch := androidRuntimeTarStripRe.FindStringSubmatch(trimmed)
		if stripMatch == nil || stripMatch[1] != "1" {
			t.Fatalf("%s 的 Node 解压命令不是 --strip-components=1，node 不再落在 <目标目录>/bin/node：请同步改 androidRuntimeCandidates 与这条门禁 %q", source, trimmed)
		}

		want := filepath.Join(dirMatch[1], "bin", "node")
		idx := androidRuntimeCandidateIndex(candidates, want)
		if idx < 0 {
			t.Fatalf("%s 把 Node 解压到 %s，node 落在 %s，但 androidRuntimeCandidates 不查这里：Debian 版卡片会显示「未安装」。实际候选 %q", source, dirMatch[1], want, candidates)
		}
		if usrBin >= 0 && idx > usrBin {
			t.Fatalf("%s 装出的 %s 排在 %s 之后，与容器 PATH 顺序不一致。实际候选 %q", source, want, filepath.Join("/usr/bin", "node"), candidates)
		}
	}
	if found == 0 {
		t.Fatal("Magisk/customize.sh 里抽不到解压 ${NODE_TARBALL} 的 tar 命令：写法变了请同步改这条门禁的抽取规则，不能让它空转")
	}
}
