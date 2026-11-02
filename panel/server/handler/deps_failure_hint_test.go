package handler

import (
	"strings"
	"testing"
)

// buildDependencyFailureHint 是纯函数（不碰 DB / HTTP），直接调用即可。
//
// 这里锁的是「诊断结论」而不是「文案」：
// 依赖装失败时面板会把这条提示直接打进任务日志给用户看，归类错了就是在批量误导排查方向。
// 改这个函数之前，先想清楚下面每条断言对应的真实故障场景。
const (
	hintDNSMarker     = "容器内 DNS 解析失败"
	hintMirrorMarker  = "镜像源不可达"
	hintLockMarker    = "锁冲突"
	hintMirrorKeyword = "镜像源"
	// apt 索引与当前镜像源对不上（issue #146）。它必须排在网络两条之后，理由见下面的用例。
	hintStaleIndexMarker = "索引与当前镜像源对不上"
)

func TestBuildDependencyFailureHintClassifiesFailureCause(t *testing.T) {
	cases := []struct {
		name string
		log  string
		// wantEmpty 为真时要求返回空串（没有可靠结论就不要瞎给建议）
		wantEmpty bool
		// contains / notContains 断言的是「诊断方向」，
		// notContains 尤其重要：把用户引到错误方向比不给提示更糟。
		contains    []string
		notContains []string
	}{
		// ---------- 容器内 DNS 解析失败 ----------
		{
			// 模块版 Debian 容器最典型的一条，注意真实日志里 T 是大写。
			name:        "apt 解析不出域名（大小写混合，真实日志形态）",
			log:         "Err:1 http://deb.debian.org/debian bookworm InRelease\n  Temporary failure resolving 'deb.debian.org'",
			contains:    []string{hintDNSMarker, "/etc/resolv.conf"},
			notContains: []string{hintMirrorKeyword},
		},
		{
			name:        "pip 报 name resolution 失败",
			log:         "WARNING: Retrying after connection broken by 'NewConnectionError: [Errno -3] Temporary failure in name resolution'",
			contains:    []string{hintDNSMarker},
			notContains: []string{hintMirrorKeyword},
		},
		{
			name:        "curl 报 could not resolve host",
			log:         "curl: (6) Could not resolve host: mirrors.nju.edu.cn",
			contains:    []string{hintDNSMarker},
			notContains: []string{hintMirrorKeyword},
		},
		{
			name:        "getaddrinfo 报 name or service not known（全小写）",
			log:         "socket.gaierror: [errno -2] name or service not known",
			contains:    []string{hintDNSMarker},
			notContains: []string{hintMirrorKeyword},
		},
		{
			// 这条是本次修复的核心场景：真实 apt 日志会同时吐 Failed to fetch 与解析失败，
			// 一旦 DNS 分支排到镜像源分支后面，用户就会被指去查「宿主机网络连通性」——
			// 而宿主机其实一切正常，坏的是容器内解析。
			name: "同时出现 Failed to fetch 与解析失败时按 DNS 归类",
			log: "Err:1 http://deb.debian.org/debian bookworm InRelease\n" +
				"  Temporary failure resolving 'deb.debian.org'\n" +
				"E: Failed to fetch http://deb.debian.org/debian/dists/bookworm/InRelease  Temporary failure resolving 'deb.debian.org'\n" +
				"E: Some index files failed to download.",
			contains:    []string{hintDNSMarker},
			notContains: []string{hintMirrorKeyword},
		},

		// ---------- 镜像源 / 宿主网络不可达 ----------
		{
			name:        "连接超时（大小写混合）",
			log:         "Err:1 http://mirrors.nju.edu.cn/debian bookworm InRelease\n  Connection timed out [IP: 203.0.113.10 80]",
			contains:    []string{hintMirrorMarker},
			notContains: []string{hintDNSMarker},
		},
		{
			name:        "代理端口拒绝连接",
			log:         "failed to connect to 127.0.0.1 port 3128 after 0 ms: connection refused",
			contains:    []string{hintMirrorMarker},
			notContains: []string{hintDNSMarker},
		},
		{
			name:        "仅有 failed to fetch（域名解析得出但下载失败）",
			log:         "E: Failed to fetch http://mirrors.nju.edu.cn/debian/pool/main/c/curl/curl_8.5.0-1_amd64.deb  404  Not Found",
			contains:    []string{hintMirrorMarker},
			notContains: []string{hintDNSMarker},
		},

		// ---------- apt 索引过期 / 与镜像源对不上（必须排在上面两类网络故障之后）----------
		{
			// issue #146 报告人贴的那份日志：换了 apt 源、索引还是旧源那批，找不到包。
			name: "只有 Unable to locate package 时归到索引过期",
			log: "Reading package lists...\nBuilding dependency tree...\n" +
				"Reading state information...\nE: Unable to locate package libxdamage1",
			contains:    []string{hintStaleIndexMarker, "apt-get update"},
			notContains: []string{hintDNSMarker, hintMirrorMarker},
		},
		{
			// 这条是新分支唯一的误诊风险，必须钉死：安装脚本用 ; 串联 apt-get update 与 apt-get install，
			// update 因网络失败后 install 照跑，于是一次【纯网络故障】的日志里会同时出现
			// Failed to fetch 与 E: Unable to locate package。索引分支一旦排到网络分支前面，
			// 就会让用户去刷索引、换源，而真正坏掉的是网。
			name: "同时出现 Failed to fetch 与 Unable to locate package 时按网络归类",
			log: "Err:1 https://mirrors.cloud.tencent.com/debian bookworm InRelease\n" +
				"  Connection timed out [IP: 203.0.113.10 443]\n" +
				"E: Failed to fetch https://mirrors.cloud.tencent.com/debian/dists/bookworm/InRelease\n" +
				"E: Some index files failed to download.\n" +
				"E: Unable to locate package libxdamage1",
			contains:    []string{hintMirrorMarker},
			notContains: []string{hintStaleIndexMarker},
		},
		{
			// DNS 也一样：解析不出来时 update 失败、install 照跑，两段报错会并存。
			name: "同时出现解析失败与 Unable to locate package 时按 DNS 归类",
			log: "Temporary failure resolving 'mirrors.cloud.tencent.com'\n" +
				"E: Unable to locate package libxdamage1",
			contains:    []string{hintDNSMarker},
			notContains: []string{hintStaleIndexMarker},
		},

		// ---------- 包管理器锁冲突（必须优先于上面两类）----------
		{
			name:     "dpkg 锁被占用",
			log:      "E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123",
			contains: []string{hintLockMarker},
		},
		{
			// 顺序契约：锁冲突 case 必须排在 DNS / 镜像源之前。
			// 锁没放开时后续网络报错都是次生现象，先让用户去解锁才是对的。
			name: "锁冲突与解析失败同时出现时锁冲突优先",
			log: "E: Could not get lock /var/lib/dpkg/lock-frontend\n" +
				"Temporary failure resolving 'deb.debian.org'",
			contains:    []string{hintLockMarker},
			notContains: []string{hintDNSMarker, hintMirrorKeyword},
		},
		{
			name: "锁冲突与连接超时同时出现时锁冲突优先",
			log: "Unable to acquire the dpkg frontend lock (/var/lib/dpkg/lock-frontend)\n" +
				"Connection timed out [IP: 203.0.113.10 80]",
			contains:    []string{hintLockMarker},
			notContains: []string{hintDNSMarker, hintMirrorMarker},
		},

		// ---------- 无结论 ----------
		{
			name:      "空日志不给结论",
			log:       "",
			wantEmpty: true,
		},
		{
			name:      "只有空白字符不给结论",
			log:       "   \n\t\n",
			wantEmpty: true,
		},
		{
			name:      "无关日志不给结论",
			log:       "npm WARN deprecated left-pad@1.0.0: use String.prototype.padStart instead",
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDependencyFailureHint(tc.log)

			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("期望不给结论，实际返回 %q", got)
				}
				return
			}

			if got == "" {
				t.Fatalf("期望给出失败原因提示，实际返回空串")
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("提示应包含 %q，实际返回 %q", want, got)
				}
			}
			for _, unwanted := range tc.notContains {
				if strings.Contains(got, unwanted) {
					t.Fatalf("提示不应包含 %q（会把用户引到错误的排查方向），实际返回 %q", unwanted, got)
				}
			}
		})
	}
}

// ---------- 缺 C/C++ 编译工具链 / CMake（issue #120）----------

const (
	hintToolchainMarker = "编译工具链"
	// 「换 Debian 版镜像」本身不是错误结论：opencv-python 在 PyPI 上有 manylinux_2_17 的
	// x86_64/aarch64 wheel、却没有任何 musllinux wheel，换过去 pip 直接下预编译包、根本不用编译。
	// 真正错的是「只给一条出路」——所以这两个词现在只在「该环境下换镜像帮不上忙」的分支里
	// 被 notContains 挡住（apt / RHEL / 探不到包管理器），Alpine 分支反而必须给出它。
	hintSwitchDebianMarker = "切换到 Debian"
	hintDebianImageMarker  = "Debian 版镜像"
)

// 外层 buildDependencyFailureHint 会现读环境（发行版 / 包管理器 / 是否 root），
// 所以这里只断言与平台无关的部分：命中新分支、结论里提到 cmake。
// 具体包名、两条出路是否并列、非 root 出路，由下面的纯函数用例覆盖。
func TestBuildDependencyFailureHintDetectsMissingBuildToolchain(t *testing.T) {
	cases := []struct {
		name string
		log  string
	}{
		{
			// issue #120 的真实形态：Alpine 上 pip 装 opencv-python，回退源码编译后卡在没有编译器。
			name: "pip 报 command 'gcc' failed",
			log: "error: command 'gcc' failed: No such file or directory\n" +
				"ERROR: Failed building wheel for lxml",
		},
		{
			// issue #120 最可能的完整现场：默认镜像是精简档（Dockerfile 有断言把 gcc/g++/make
			// 全排除），而 opencv-python 把 cmake 写进 build-system.requires，pip 构建隔离会
			// 从 PyPI 装一个 musllinux 的 cmake wheel —— 于是「cmake 有、编译器没有」，
			// 报错整段由 CMake 自己吐出来，pip 那层只剩看不出真因的 Failed to build ...。
			name: "CMake 找不到 C++ 编译器（issue #120 完整形态）",
			log: "  CMake Error at CMakeLists.txt:11 (project):\n" +
				"    No CMAKE_CXX_COMPILER could be found.\n" +
				"  ERROR: Failed building wheel for opencv-python\n" +
				"ERROR: Failed to build installable wheels for some pyproject.toml based projects (opencv-python)",
		},
		{
			// 命中的是 identification is unknown 这两句（CMake 自己点名说探不出编译器）。
			// 末尾那句 An error occurred while configuring with CMake. 只是 scikit-build 的
			// 包装语，不参与判定，删掉它这条用例照样命中 —— 见下面的反向用例。
			name: "CMake 探不出编译器身份",
			log: "-- The C compiler identification is unknown\n" +
				"-- The CXX compiler identification is unknown\n" +
				"An error occurred while configuring with CMake.",
		},
		{
			name: "sh 报 gcc: not found",
			log:  "/bin/sh: gcc: not found\nerror: command '/usr/bin/gcc' failed with exit code 127",
		},
		{
			name: "exec 报找不到 gcc",
			log:  `exec: "gcc": executable file not found in $PATH`,
		},
		{
			name: "g++ 缺失",
			log:  "unable to execute 'cc1plus': No such file or directory",
		},
		{
			// 完整档镜像有 build-base，但没有 cmake，opencv 就停在这一步。
			name: "opencv 报 CMake 未安装",
			log: "Problem with the CMake installation, aborting build. CMake executable is cmake\n" +
				"ERROR: Failed building wheel for opencv-python\n" +
				"ERROR: Failed to build installable wheels for some pyproject.toml based projects (opencv-python)",
		},
		{
			name: "setup.py 报 CMake must be installed",
			log:  "RuntimeError: CMake must be installed to build the following extensions: cv2",
		},
		{
			name: "缺 Python 开发头文件",
			log:  "src/lxml/etree.c:96:10: fatal error: Python.h: No such file or directory",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildDependencyFailureHint(tc.log)

			if got == "" {
				t.Fatalf("期望命中「缺编译工具链」分支，实际返回空串")
			}
			if !strings.Contains(got, hintToolchainMarker) {
				t.Fatalf("提示应包含 %q，实际返回 %q", hintToolchainMarker, got)
			}
			// cmake 是这条分支的核心结论之一，各发行版分支里大小写不同，统一小写后比对。
			if !strings.Contains(strings.ToLower(got), "cmake") {
				t.Fatalf("提示应提到 cmake，实际返回 %q", got)
			}
		})
	}
}

// 反向钉死误伤面：工具链齐全、只是缺系统开发包的失败，不许被判成缺编译工具链。
//
// "An error occurred while configuring with CMake." 不是 CMake 自己吐的，而是 scikit-build
// 的 cmaker.configure() 包装：cmake 子进程返回非 0 就抛。所以它出现时 cmake 已经跑起来了，
// 与「cmake / 编译器不存在」互斥。下面这份日志上面两行还明明白白印着
// The CXX compiler identification is GNU 12.2.0（Debian 镜像 / 裸机 Ubuntu，gcc、g++、make 都在），
// 真因是缺 zlib 开发包；一旦把那句包装语收进关键词表，面板就会写下
// 「请安装 build-essential、cmake…把超时调大」，用户照做重跑报错一字不变，真因被彻底盖住。
//
// 上面那条 identification is unknown 的用例把这句和真信号捆在同一份日志里，
// 删掉关键词它照样过，所以必须用这条反向用例守住边界。
func TestMissingBuildToolchainIgnoresGenericCMakeConfigureWrapper(t *testing.T) {
	const log = "-- The CXX compiler identification is GNU 12.2.0\n" +
		"CMake Error at cmake/OpenCVFindLibsGrfmt.cmake:19 (find_package):\n" +
		"  Could NOT find ZLIB (missing: ZLIB_LIBRARY ZLIB_INCLUDE_DIR)\n" +
		"-- Configuring incomplete, errors occurred!\n" +
		"An error occurred while configuring with CMake.\n" +
		"ERROR: Failed building wheel for opencv-python"

	if isMissingBuildToolchain(strings.ToLower(log)) {
		t.Fatalf("编译器明明在（GNU 12.2.0），缺的是 zlib 开发包，不该被判成缺编译工具链：%q", log)
	}
	// 这份日志也不命中 glibc 车道（没有 failed to build installable wheels / manylinux 等词），
	// 所以正确结果是「没结论」——宁可不给，也好过把用户指去装一套本来就装好的工具链。
	if got := buildDependencyFailureHint(log); got != "" {
		t.Fatalf("没有可靠结论时应返回空串，实际返回 %q", got)
	}
}

// issue #120 正文里用户原样贴出的那份日志。
//
// 它一条工具链关键词都不命中（连 gcc / cmake 字样都没有），唯一的命中项是 glibc 车道的
// failed to build installable wheels —— 而对 opencv-python 来说，「换 Debian 版镜像」
// 恰恰是正确解（它有 manylinux_2_17 wheel、没有 musllinux wheel，换过去秒装）。
// 所以这里钉两件事：
//  1. 这份日志必须仍然落在 glibc 车道（关键词被删掉的话用户就只剩一条空提示）；
//  2. glibc 车道的结论必须同时给出「换镜像」和「装 cmake / 编译器」两条出路。
func TestIssue120RawLogStillLandsInGlibcLane(t *testing.T) {
	const issue120Log = "  note: This error originates from a subprocess, and is likely not a problem with pip.\n" +
		"  ERROR: Failed building wheel for opencv-python\n" +
		"Failed to build opencv-python\n" +
		"[notice] A new release of pip is available: 25.0.1 -> 26.2.1\n" +
		"ERROR: Failed to build installable wheels for some pyproject.toml based projects (opencv-python)"

	lower := strings.ToLower(issue120Log)
	if isMissingBuildToolchain(lower) {
		t.Fatalf("这份日志没有任何工具链缺失信号，不该被判成缺编译工具链：%q", issue120Log)
	}
	if !matchesAlpineGlibcHints(lower) {
		t.Fatalf("这份日志唯一的命中项被删掉了，Alpine 上的用户会拿到一条空提示：%q", issue120Log)
	}

	// isAlpineGlibcIncompatible 现读 /etc/os-release，非 Alpine 机器上恒 false，
	// 所以结论文案直接按 root 场景调纯函数断言。
	rooted := buildAlpineGlibcHint(dependencyToolchainEnv{})
	if !strings.Contains(rooted, hintDebianImageMarker) {
		t.Fatalf("glibc 结论必须给出换 Debian 版镜像这条出路，实际为 %q", rooted)
	}
	if !strings.Contains(strings.ToLower(rooted), "cmake") {
		t.Fatalf("glibc 结论必须给出「换镜像也没用时装 cmake 现场编」这条退路，实际为 %q", rooted)
	}
	if !strings.Contains(rooted, "依赖管理 → Linux") {
		t.Fatalf("glibc 结论的退路要指到装系统包的地方，实际为 %q", rooted)
	}
}

// glibc 车道的退路同样要跟着运行身份走。
//
// 这条退路是本轮新加的（「换镜像也没用时去 Linux 页签装 cmake」），但它一开始是个 const、
// 不接 detectDependencyToolchainEnv()，于是降权部署（PUID/PGID）下 issue #120 那份日志
// ——唯一命中项是 failed to build installable wheels，正好走 glibc 车道——
// 会把用户指去一个他点了就必被 EnsureLinuxPackageManagerPrivilege 拒掉的页签：
// 先建 3 条依赖记录、再 3 条全部 failed、侧栏角标 +3。
// 这正是 buildMissingToolchainHint 那边已经修好的坑，两条车道必须给同样的待遇。
func TestAlpineGlibcHintCarriesPrivilegeEscape(t *testing.T) {
	const privilege = "当前面板以非 root 用户（uid=1000）运行，无法安装或卸载 Linux 系统依赖 —— " +
		"可选做法：在宿主机执行 docker exec -u 0 <容器名> apk add <包名>"

	rooted := buildAlpineGlibcHint(dependencyToolchainEnv{Distribution: "alpine", PackageManager: "apk"})
	nonRoot := buildAlpineGlibcHint(dependencyToolchainEnv{
		Distribution:   "alpine",
		PackageManager: "apk",
		PrivilegeHint:  privilege,
	})

	if strings.Contains(rooted, privilege) {
		t.Fatalf("root 场景不该塞提权说明，实际返回 %q", rooted)
	}
	if !strings.Contains(rooted, "请到「依赖管理 → Linux」页签装好") {
		t.Fatalf("root 场景那条页签是能用的，指引不能丢，实际返回 %q", rooted)
	}

	if !strings.Contains(nonRoot, privilege) {
		t.Fatalf("非 root 场景必须带上提权出路，实际返回 %q", nonRoot)
	}
	if strings.Contains(nonRoot, "请到「依赖管理 → Linux」页签装好") {
		t.Fatalf("非 root 场景不能再指路去那个装不了系统包的页签，实际返回 %q", nonRoot)
	}
	// 主出路（换 Debian 版镜像）与要装哪些包都不能被提权说明顶掉。
	if !strings.Contains(nonRoot, hintDebianImageMarker) {
		t.Fatalf("非 root 场景仍应给出换 Debian 版镜像这条主出路，实际返回 %q", nonRoot)
	}
	if !strings.Contains(nonRoot, "build-base") {
		t.Fatalf("非 root 场景仍应给出要装哪些包，实际返回 %q", nonRoot)
	}
	// 与其他归因一样是单行方括号文案。
	for _, got := range []string{rooted, nonRoot} {
		if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
			t.Fatalf("提示应与其他归因一样是单行方括号文案，实际返回 %q", got)
		}
		if strings.Contains(got, "\n") {
			t.Fatalf("提示会被整行写进依赖日志，不能带换行，实际返回 %q", got)
		}
	}
}

// 新分支的边界：只认「工具本身不存在」，不许去抢 glibc 分支那些更宽的关键词。
// isAlpineGlibcIncompatible 依赖 /etc/os-release，在 Windows 开发机上恒 false、
// 没法直接断言它命中，所以这里从两头夹：关键词判定必须为假，外层结论也只能是
// 「glibc 车道那条固定文案」或「没结论」。
func TestMissingBuildToolchainDoesNotStealGlibcCases(t *testing.T) {
	glibcLogs := []string{
		"ERROR: Could not find a version that satisfies the requirement grpcio\nERROR: No matching distribution found for grpcio",
		"ERROR: grpcio-1.60.0-cp311-cp311-manylinux_2_17_x86_64.whl is not a supported wheel on this platform",
		"ERROR: Failed to build installable wheels for some pyproject.toml based projects (grpcio)",
	}

	for _, log := range glibcLogs {
		if isMissingBuildToolchain(strings.ToLower(log)) {
			t.Fatalf("纯 glibc 不兼容日志不该被判成缺编译工具链：%q", log)
		}
		// 结论只能是这两种之一：Alpine 上是 glibc 车道的文案，其他平台没结论。
		// （不能再断言「不含编译工具链字样」——glibc 文案本身现在也带了装 cmake 的退路。）
		// glibc 文案已按运行身份分岔，所以这里跟调用点一样现读环境再比对，不能拿死文本对。
		if got := buildDependencyFailureHint(log); got != "" && got != buildAlpineGlibcHint(detectDependencyToolchainEnv()) {
			t.Fatalf("纯 glibc 不兼容日志的归因不该落到编译工具链分支，实际返回 %q", got)
		}
	}
}

// 顺序契约：锁冲突仍然优先。锁没放开时，编译器报错只是次生现象。
func TestBuildDependencyFailureHintKeepsLockPriorityOverToolchain(t *testing.T) {
	got := buildDependencyFailureHint(
		"E: Could not get lock /var/lib/dpkg/lock-frontend. It is held by process 123\n" +
			"/bin/sh: gcc: not found")

	if !strings.Contains(got, hintLockMarker) {
		t.Fatalf("提示应包含 %q，实际返回 %q", hintLockMarker, got)
	}
	if strings.Contains(got, hintToolchainMarker) {
		t.Fatalf("锁冲突必须优先于缺编译工具链，实际返回 %q", got)
	}
}

// 文案内核是纯函数，环境事实全部由入参给出，因此 Windows 开发机上也能把每个分支跑全。
func TestBuildMissingToolchainHintByEnvironment(t *testing.T) {
	cases := []struct {
		name        string
		env         dependencyToolchainEnv
		contains    []string
		notContains []string
	}{
		{
			// apk 场景就是 issue #120 的原始现场，两条出路必须并列给：
			// 先给成本最低的「换 Debian 版镜像直接拿 manylinux wheel」，
			// 再给兜底的「装 build-base + linux-headers + cmake 现场编，记得调大超时」。
			name:     "Alpine / apk 并列给出换镜像与现场编译两条出路",
			env:      dependencyToolchainEnv{Distribution: "alpine", PackageManager: "apk"},
			contains: []string{hintDebianImageMarker, "依赖管理 → Linux", "build-base", "linux-headers", "cmake", "依赖安装超时"},
			// 只挡包名串台，不再挡 Debian 镜像。
			notContains: []string{"build-essential"},
		},
		{
			name:        "Debian / apt 给 build-essential + cmake",
			env:         dependencyToolchainEnv{Distribution: "debian", PackageManager: "apt"},
			contains:    []string{"依赖管理 → Linux", "build-essential", "cmake"},
			notContains: []string{hintSwitchDebianMarker, hintDebianImageMarker, "build-base"},
		},
		{
			// 包管理器探测不到时按 /etc/os-release 兜底，避免掉进最泛的那条。
			name:     "只探到发行版也能给出对应包名",
			env:      dependencyToolchainEnv{Distribution: "ubuntu"},
			contains: []string{"build-essential", "cmake"},
		},
		{
			name:        "RHEL 系给 gcc-c++",
			env:         dependencyToolchainEnv{Distribution: "centos", PackageManager: "yum"},
			contains:    []string{"依赖管理 → Linux", "gcc-c++", "cmake"},
			notContains: []string{"build-base", "build-essential"},
		},
		{
			// Windows / 裸机：探不到发行版就只给通用结论，不编造包名。
			name:        "探测不到发行版时给通用提示",
			env:         dependencyToolchainEnv{},
			contains:    []string{hintToolchainMarker, "CMake"},
			notContains: []string{"build-base", "build-essential", hintSwitchDebianMarker, hintDebianImageMarker},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildMissingToolchainHint(tc.env)

			if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
				t.Fatalf("提示应与其他归因一样是单行方括号文案，实际返回 %q", got)
			}
			if strings.Contains(got, "\n") {
				t.Fatalf("提示会被整行写进依赖日志，不能带换行，实际返回 %q", got)
			}
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("提示应包含 %q，实际返回 %q", want, got)
				}
			}
			for _, unwanted := range tc.notContains {
				if strings.Contains(got, unwanted) {
					t.Fatalf("提示不应包含 %q，实际返回 %q", unwanted, got)
				}
			}
		})
	}
}

// 非 root 时如果只说「去 Linux 页签装」，用户一点安装就会撞上 Linux 依赖的非 root 拦截：
// BuildLinuxPackageCommand 第一行就被 EnsureLinuxPackageManagerPrivilege 拒掉，
// 凭空多出几条 failed 依赖记录、侧栏角标 +3，提示本身也自相矛盾。
// 所以必须把提权出路一并带上，且不能再指路去那个装不了的页签。
func TestBuildMissingToolchainHintCarriesPrivilegeEscape(t *testing.T) {
	const privilege = "当前面板以非 root 用户（uid=1000）运行，无法安装或卸载 Linux 系统依赖 —— " +
		"可选做法：在宿主机执行 docker exec -u 0 <容器名> apk add <包名>"

	rooted := buildMissingToolchainHint(dependencyToolchainEnv{Distribution: "alpine", PackageManager: "apk"})
	nonRoot := buildMissingToolchainHint(dependencyToolchainEnv{
		Distribution:   "alpine",
		PackageManager: "apk",
		PrivilegeHint:  privilege,
	})

	if strings.Contains(rooted, privilege) {
		t.Fatalf("root 场景不该塞提权说明，实际返回 %q", rooted)
	}
	if !strings.Contains(nonRoot, privilege) {
		t.Fatalf("非 root 场景必须带上提权出路，实际返回 %q", nonRoot)
	}
	// 提权说明只是补充，安装指引本身不能被顶掉。
	if !strings.Contains(nonRoot, "build-base") {
		t.Fatalf("非 root 场景仍应给出要装哪些包，实际返回 %q", nonRoot)
	}
	// 「去哪儿装」必须跟着运行身份变：非 root 时不能一边指路去 Linux 页签，
	// 一边在同一行里说那里装不了。
	if strings.Contains(nonRoot, "请到「依赖管理 → Linux」页签安装") &&
		strings.Contains(nonRoot, "无法安装或卸载 Linux 系统依赖") {
		t.Fatalf("非 root 场景不能既指路去 Linux 页签、又说那里装不了，实际返回 %q", nonRoot)
	}
	// root 场景反过来：那条页签是能用的，指引不能丢。
	if !strings.Contains(rooted, "请到「依赖管理 → Linux」页签安装") {
		t.Fatalf("root 场景应直接指路到「依赖管理 → Linux」页签，实际返回 %q", rooted)
	}
}

// DNS 与镜像源必须是两条互不相同的结论。
// 合并回一条（或让其中一条去 return 另一条）时，上面的 notContains 已经能报警；
// 这里再补一刀，防止有人把两条文案改成同一个字符串常量。
func TestBuildDependencyFailureHintKeepsDNSAndMirrorSeparate(t *testing.T) {
	dns := buildDependencyFailureHint("Temporary failure resolving 'deb.debian.org'")
	mirror := buildDependencyFailureHint("Connection timed out [IP: 203.0.113.10 80]")

	if dns == "" || mirror == "" {
		t.Fatalf("两类失败都应给出提示，dns=%q mirror=%q", dns, mirror)
	}
	if dns == mirror {
		t.Fatalf("DNS 解析失败与镜像源不可达的排查方向相反，不能共用同一条提示：%q", dns)
	}
}
