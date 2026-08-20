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
