package service

import (
	"strings"
	"testing"
)

// 下面三条是从 handler/deps_package_manager_test.go 原样搬来的（实现 v3.3.1 搬到了 service），断言一字未改。

// 后半段断言的是「已经是默认加速源时不再改写」，所以前半段显式传的这个源必须跟着默认源一起走。
// v3.3.2 默认源从阿里云换成腾讯云（issue #146），这里只改期望值，断言本身一字未动。
func TestRewriteAPTListLine(t *testing.T) {
	line := "deb [arch=amd64] http://archive.ubuntu.com/ubuntu jammy main restricted"
	updated, changed := rewriteAPTListLine(line, "ubuntu", "https://mirrors.cloud.tencent.com/ubuntu")
	if !changed {
		t.Fatalf("expected apt list line to change")
	}
	if updated != "deb [arch=amd64] https://mirrors.cloud.tencent.com/ubuntu jammy main restricted" {
		t.Fatalf("unexpected updated line: %s", updated)
	}

	defaulted, changed := rewriteAPTListLine(updated, "ubuntu", "")
	if changed {
		t.Fatalf("expected apt list line already using default accelerated mirror to remain unchanged")
	}
	if defaulted != "deb [arch=amd64] https://mirrors.cloud.tencent.com/ubuntu jammy main restricted" {
		t.Fatalf("unexpected defaulted line: %s", defaulted)
	}
}

func TestRewriteAPTSourcesContent(t *testing.T) {
	content := "Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble noble-updates\nComponents: main restricted\n"
	updated, changed := rewriteAPTSourcesContent(content, "ubuntu", "https://mirrors.aliyun.com/ubuntu")
	if !changed {
		t.Fatalf("expected apt sources content to change")
	}
	if !strings.Contains(updated, "URIs: https://mirrors.aliyun.com/ubuntu") {
		t.Fatalf("unexpected rewritten sources content: %s", updated)
	}
}

// v3.3.2 默认源换成腾讯云（issue #146）：下面四处只改期望值。
// 最后一条刻意仍用阿里云 —— EffectiveLinuxMirror 不参与旧默认源迁移，
// 用户显式选的阿里云必须被原样保留（迁移只发生在 EnsureDefaultLinuxMirror 那一侧）。
func TestEffectiveLinuxMirrorFallsBackToDefaultAcceleratedMirror(t *testing.T) {
	apkManager := LinuxPackageManager{Name: "apk", Binary: "apk"}
	if got := EffectiveLinuxMirror(apkManager, "", ""); got != "https://mirrors.cloud.tencent.com/alpine" {
		t.Fatalf("expected apk default mirror, got %q", got)
	}
	if got := EffectiveLinuxMirror(apkManager, "", "https://dl-cdn.alpinelinux.org/alpine"); got != "https://mirrors.cloud.tencent.com/alpine" {
		t.Fatalf("expected apk official mirror to fall back to accelerated mirror, got %q", got)
	}

	aptManager := LinuxPackageManager{Name: "apt", Binary: "apt-get"}
	if got := EffectiveLinuxMirror(aptManager, "ubuntu", ""); got != "https://mirrors.cloud.tencent.com/ubuntu" {
		t.Fatalf("expected ubuntu default mirror, got %q", got)
	}
	if got := EffectiveLinuxMirror(aptManager, "debian", "http://deb.debian.org/debian"); got != "https://mirrors.cloud.tencent.com/debian" {
		t.Fatalf("expected debian official mirror to fall back to accelerated mirror, got %q", got)
	}
	if got := EffectiveLinuxMirror(aptManager, "ubuntu", "https://mirrors.aliyun.com/ubuntu"); got != "https://mirrors.aliyun.com/ubuntu" {
		t.Fatalf("expected custom ubuntu mirror to be preserved, got %q", got)
	}
}

// node:*-bookworm-slim（面板 Debian 镜像的底座）自带的 debian.sources 原文。
// 注意 URIs 写在 Suites 前面 —— 逐行读的老实现在处理 URIs 时还没读到 Suites，security 段永远认不出来。
const bookwormDebianSources = "Types: deb\n" +
	"# http://snapshot.debian.org/archive/debian/20230612T000000Z\n" +
	"URIs: http://deb.debian.org/debian\n" +
	"Suites: bookworm bookworm-updates\n" +
	"Components: main\n" +
	"Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg\n" +
	"\n" +
	"Types: deb\n" +
	"# http://snapshot.debian.org/archive/debian-security/20230612T000000Z\n" +
	"URIs: http://deb.debian.org/debian-security\n" +
	"Suites: bookworm-security\n" +
	"Components: main\n" +
	"Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg\n"

// debian-security 段必须改到 <镜像>/debian-security，不能跟主仓库一起改成 <镜像>/debian：
// 镜像站的 /debian 下没有 bookworm-security，apt-get update 会报 E:，安全更新源静默失效。
func TestRewriteAPTSourcesKeepsDebianSecuritySuffix(t *testing.T) {
	updated, changed := rewriteAPTSourcesContent(bookwormDebianSources, "debian", "")
	if !changed {
		t.Fatal("官方源应被改写成默认加速源")
	}
	if !strings.Contains(updated, "URIs: https://mirrors.cloud.tencent.com/debian\n") {
		t.Fatalf("主仓库应改到默认加速源的 /debian，实际：\n%s", updated)
	}
	if !strings.Contains(updated, "URIs: https://mirrors.cloud.tencent.com/debian-security\n") {
		t.Fatalf("security 段应改到默认加速源的 /debian-security，实际：\n%s", updated)
	}
	// 其余行（注释、Suites、Signed-By、段间空行）一个字节都不能动。
	for _, keep := range []string{
		"# http://snapshot.debian.org/archive/debian-security/20230612T000000Z\n",
		"Suites: bookworm-security\n",
		"Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg\n\nTypes: deb\n",
	} {
		if !strings.Contains(updated, keep) {
			t.Fatalf("改写不应碰到 %q，实际：\n%s", keep, updated)
		}
	}

	// 幂等：再跑一遍不应再有改动（EnsureDefaultLinuxMirror 每装一个包都会走一次）。
	// 这里显式传的必须与上一步用默认源改出来的结果一致，v3.3.2 起是腾讯云（issue #146）。
	if again, changedAgain := rewriteAPTSourcesContent(updated, "debian", "https://mirrors.cloud.tencent.com/debian"); changedAgain || again != updated {
		t.Fatalf("已是目标镜像时不应再改写，实际：\n%s", again)
	}
}

// 用户在页面上指定了别的镜像（例如清华）时，security 段同样要跟到 <镜像>-security。
func TestRewriteAPTSourcesSecurityFollowsRequestedMirror(t *testing.T) {
	updated, _ := rewriteAPTSourcesContent(bookwormDebianSources, "debian", "https://mirrors.tuna.tsinghua.edu.cn/debian/")
	if !strings.Contains(updated, "URIs: https://mirrors.tuna.tsinghua.edu.cn/debian\n") {
		t.Fatalf("主仓库应改到清华 /debian，实际：\n%s", updated)
	}
	if !strings.Contains(updated, "URIs: https://mirrors.tuna.tsinghua.edu.cn/debian-security\n") {
		t.Fatalf("security 段应改到清华 /debian-security，实际：\n%s", updated)
	}
}

// 单行 .list 格式（Debian 11 及更早的 sources.list）走同一套判据。
func TestRewriteAPTListLineKeepsDebianSecuritySuffix(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{
			name: "路径以 -security 结尾",
			line: "deb http://security.debian.org/debian-security bullseye-security main",
			want: "deb https://mirrors.cloud.tencent.com/debian-security bullseye-security main",
		},
		{
			// buster 及更早的写法：路径不带 -security，靠 Suites 的 /updates 认出来。
			name: "buster 的 /updates 写法",
			line: "deb http://security.debian.org buster/updates main",
			want: "deb https://mirrors.cloud.tencent.com/debian-security buster/updates main",
		},
		{
			name: "普通主仓库不加后缀",
			line: "deb http://deb.debian.org/debian bullseye-updates main",
			want: "deb https://mirrors.cloud.tencent.com/debian bullseye-updates main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, changed := rewriteAPTListLine(tc.line, "debian", "")
			if !changed || got != tc.want {
				t.Fatalf("期望 %q，实际 %q（changed=%v）", tc.want, got, changed)
			}
		})
	}
}

// Ubuntu 的 noble-security 就在 /ubuntu 下（官方 security.ubuntu.com/ubuntu），绝不能加 -security 后缀，
// 否则会指到一个不存在的 ubuntu-security 路径。Suites 这条判据因此只对 Debian 生效。
func TestRewriteAPTSourcesLeavesUbuntuSecurityUnderUbuntu(t *testing.T) {
	content := "Types: deb\nURIs: http://security.ubuntu.com/ubuntu/\nSuites: noble-security\nComponents: main restricted\n"
	updated, changed := rewriteAPTSourcesContent(content, "ubuntu", "")
	if !changed {
		t.Fatal("官方 security 源应被改写成默认加速源")
	}
	if !strings.Contains(updated, "URIs: https://mirrors.cloud.tencent.com/ubuntu\n") {
		t.Fatalf("Ubuntu 的 security 段应仍指向 /ubuntu，实际：\n%s", updated)
	}
	if strings.Contains(updated, "ubuntu-security") {
		t.Fatalf("Ubuntu 不应被加上 -security 后缀，实际：\n%s", updated)
	}
}
