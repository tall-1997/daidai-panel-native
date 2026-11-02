package handler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Node 版本对齐门禁。
//
// 面板里钉死 Node 版本的地方散在六个文件、四种写法里：Dockerfile 的镜像 tag、
// GitHub Actions 的 node-version、Magisk Debian 装依赖脚本的 NODE_VERSION=、一键安装预置的下载地址。
// 升级时漏改任何一处都不会有报错 —— CI 拿一个版本构建、镜像里跑的是另一个版本，
// Magisk Debian 与一键安装各装各的，只有用户碰上版本差异才会暴露。
//
// 每一处「抽不到」都直接判失败：写法一变正则就会失配，
// 失配时若把「没抽到」当成「没有不一致」，门禁会恒绿空转，和没有一样。

type nodeVersionPin struct {
	source  string // 文件:行号，失败时用来指出具体是哪一处漂移
	version string
}

var (
	// 整份 Dockerfile（含注释）里的 node:X.Y.Z-bookworm-slim：注释里写着过期版本，同样会误导下一个升级的人
	nodeAlignDockerImageRe = regexp.MustCompile(`node:(\d+\.\d+\.\d+)-bookworm-slim`)
	// FROM 行里的 node 官方镜像，兼容 docker.io/library/node: 这种带仓库前缀的写法
	nodeAlignFromNodeImageRe = regexp.MustCompile(`(^|/)node:`)
	nodeAlignPinnedTagRe     = regexp.MustCompile(`^(\d+\.\d+\.\d+)-bookworm-slim$`)
	// 冒号紧跟在 node-version 后面，node-version-file: 不会被勾到
	nodeAlignWorkflowRe      = regexp.MustCompile(`^(?:-\s*)?node-version:(.*)$`)
	nodeAlignMagiskVersionRe = regexp.MustCompile(`^NODE_VERSION=(\S*)$`)
	nodeAlignMagiskMajorRe   = regexp.MustCompile(`^REQUIRED_NODE_MAJOR=(\S*)$`)
	// 目录版本与文件名版本分开抓：只改了其中一个的地址会 404，这里顺手拦下
	nodeAlignPresetURLRe = regexp.MustCompile(`^https://\S+/v(\d+\.\d+\.\d+)/node-v(\d+\.\d+\.\d+)-linux-[a-z0-9]+\.tar\.gz$`)
	nodeAlignSemverRe    = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
)

// readRepoTextFile 按相对 server/handler 的路径读仓库文件，统一去掉 CR（理由同 readMagiskScript）。
func readRepoTextFile(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", ".."}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func nodeAlignIsComment(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "#")
}

// nodeAlignDockerfilePins 抽出 Dockerfile 里全部 node:X.Y.Z-bookworm-slim。
// 另外要求每一条 FROM node 都钉到补丁版本：写成 node:24-bookworm-slim 这类浮动 tag 时，
// 只要文件里还剩一处钉死的写法，版本比对照样能过，漂移就从这里漏掉了。
func nodeAlignDockerfilePins(t *testing.T, name, text string) []nodeVersionPin {
	t.Helper()
	var pins []nodeVersionPin
	fromNode := 0
	for i, line := range strings.Split(text, "\n") {
		source := fmt.Sprintf("%s:%d", name, i+1)
		for _, m := range nodeAlignDockerImageRe.FindAllStringSubmatch(line, -1) {
			pins = append(pins, nodeVersionPin{source: source, version: m[1]})
		}
		if nodeAlignIsComment(line) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		image := ""
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "--") {
				continue
			}
			image = field
			break
		}
		loc := nodeAlignFromNodeImageRe.FindStringIndex(image)
		if loc == nil {
			continue
		}
		fromNode++
		if !nodeAlignPinnedTagRe.MatchString(image[loc[1]:]) {
			t.Fatalf("%s 的 FROM 用了 %q：node 镜像必须写成 node:X.Y.Z-bookworm-slim，浮动 tag 会逃过版本对齐（真要换发行版，请同步改这条门禁）", source, image)
		}
	}
	if fromNode == 0 || len(pins) == 0 {
		t.Fatalf("%s 里抽不到 FROM node:X.Y.Z-bookworm-slim（FROM node 行=%d，版本=%d）：写法变了请同步改这条门禁的抽取规则，不能让它空转", name, fromNode, len(pins))
	}
	return pins
}

// nodeAlignWorkflowPins 抽出 workflow 里全部 node-version 的值（去掉引号与行尾注释）。
// 取到空值也照样记下来，让它在比对里以不一致的形式暴露，而不是被悄悄跳过。
func nodeAlignWorkflowPins(t *testing.T, name, text string) []nodeVersionPin {
	t.Helper()
	var pins []nodeVersionPin
	for i, line := range strings.Split(text, "\n") {
		if nodeAlignIsComment(line) {
			continue
		}
		m := nodeAlignWorkflowRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		value := m[1]
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = value[:idx]
		}
		value = strings.Trim(strings.TrimSpace(value), `'"`)
		pins = append(pins, nodeVersionPin{source: fmt.Sprintf("%s:%d", name, i+1), version: value})
	}
	if len(pins) == 0 {
		t.Fatalf("%s 里抽不到 node-version：写法变了（比如改成 node-version-file）请同步改这条门禁的抽取规则，不能让它空转", name)
	}
	return pins
}

// nodeAlignShellAssignPins 抽出 shell 脚本里「整行赋值」的变量值，注释行不算。
// NODE_VERSION_LINE= 这类前缀相同的变量被正则里的 = 紧邻约束排除在外。
func nodeAlignShellAssignPins(name, text string, re *regexp.Regexp) []nodeVersionPin {
	var pins []nodeVersionPin
	for i, line := range strings.Split(text, "\n") {
		if nodeAlignIsComment(line) {
			continue
		}
		m := re.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		pins = append(pins, nodeVersionPin{
			source:  fmt.Sprintf("%s:%d", name, i+1),
			version: strings.Trim(m[1], `'"`),
		})
	}
	return pins
}

// nodeAlignPresetPins 从一键安装预置里抽出 node 各架构下载地址的版本，展示文案的大版本也要跟着对上。
func nodeAlignPresetPins(t *testing.T) []nodeVersionPin {
	t.Helper()
	var pins []nodeVersionPin
	for _, p := range androidRuntimePresets {
		if p.Name != "node" {
			continue
		}
		source := fmt.Sprintf("android_runtime.go 一键安装预置 node/%s", p.Arch)
		m := nodeAlignPresetURLRe.FindStringSubmatch(p.URL)
		if m == nil {
			t.Fatalf("%s 的下载地址抽不出版本（期望 .../vX.Y.Z/node-vX.Y.Z-linux-<arch>.tar.gz）: %s", source, p.URL)
		}
		if m[1] != m[2] {
			t.Fatalf("%s 的下载地址目录版本 v%s 与文件名版本 v%s 不一致: %s", source, m[1], m[2], p.URL)
		}
		major := strings.SplitN(m[1], ".", 2)[0]
		if !strings.Contains(p.Label, "v"+major+" ") {
			t.Fatalf("%s 的展示文案 %q 与下载地址的大版本 v%s 不一致", source, p.Label, major)
		}
		pins = append(pins, nodeVersionPin{source: source, version: m[1]})
	}
	if len(pins) == 0 {
		t.Fatal("androidRuntimePresets 里抽不到 node 预置：预置被删或改名的话请同步改这条门禁")
	}
	return pins
}

func nodeAlignDescribe(pins []nodeVersionPin) string {
	byVersion := map[string][]string{}
	for _, p := range pins {
		byVersion[p.version] = append(byVersion[p.version], p.source)
	}
	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	var b strings.Builder
	for _, v := range versions {
		fmt.Fprintf(&b, "  %q: %s\n", v, strings.Join(byVersion[v], ", "))
	}
	return b.String()
}

// R8：Dockerfile、Dockerfile.debian、checks.yml、deploy-demo.yml、Magisk Debian 下载的官方包、
// 一键安装预置，这六处钉的 Node 必须是同一个 X.Y.Z。
// release.yml 只写大版本（setup-node 取该大版本的最新补丁），与上面的大版本一致即可；
// Magisk 安装验证的 REQUIRED_NODE_MAJOR 同理 —— 它没跟上的话，Debian 装上的新 Node 会被判成版本不符、整个安装中止。
func TestNodeVersionPinsAlignedAcrossChannels(t *testing.T) {
	var pins []nodeVersionPin
	for _, name := range []string{"Dockerfile", "Dockerfile.debian"} {
		pins = append(pins, nodeAlignDockerfilePins(t, name, readRepoTextFile(t, name))...)
	}
	for _, name := range []string{"checks.yml", "deploy-demo.yml"} {
		pins = append(pins, nodeAlignWorkflowPins(t, ".github/workflows/"+name, readRepoTextFile(t, ".github", "workflows", name))...)
	}

	customize := readMagiskCustomizeScript(t)
	magiskPins := nodeAlignShellAssignPins("Magisk/customize.sh", customize, nodeAlignMagiskVersionRe)
	if len(magiskPins) == 0 {
		t.Fatal("Magisk/customize.sh 里抽不到 NODE_VERSION= 整行赋值：写法变了请同步改这条门禁的抽取规则，不能让它空转")
	}
	pins = append(pins, magiskPins...)
	pins = append(pins, nodeAlignPresetPins(t)...)

	want := pins[0].version
	for _, p := range pins[1:] {
		if p.version != want {
			t.Fatalf("Node 版本没有对齐，各处实际取值如下（升级 Node 时这些位置要一起改；Magisk 还要同步换 NODE_SHA256）：\n%s", nodeAlignDescribe(pins))
		}
	}
	if !nodeAlignSemverRe.MatchString(want) {
		t.Fatalf("对齐后的 Node 版本 %q 不是 X.Y.Z 形式，大版本比对无从谈起", want)
	}
	major := strings.SplitN(want, ".", 2)[0]

	releasePins := nodeAlignWorkflowPins(t, ".github/workflows/release.yml", readRepoTextFile(t, ".github", "workflows", "release.yml"))
	for _, p := range releasePins {
		if strings.SplitN(p.version, ".", 2)[0] != major {
			t.Fatalf("%s 的 node-version %q 与其它位置的大版本 %s（%s）不一致，全部取值：\n%s",
				p.source, p.version, major, want, nodeAlignDescribe(releasePins))
		}
	}

	majorPins := nodeAlignShellAssignPins("Magisk/customize.sh", customize, nodeAlignMagiskMajorRe)
	if len(majorPins) == 0 {
		t.Fatal("Magisk/customize.sh 里抽不到 REQUIRED_NODE_MAJOR= 整行赋值：写法变了请同步改这条门禁的抽取规则，不能让它空转")
	}
	for _, p := range majorPins {
		if p.version != major {
			t.Fatalf("%s 的 REQUIRED_NODE_MAJOR=%s 与钉住的 Node %s 大版本不一致：安装验证会把装上的 Node 判成版本不符并中止安装", p.source, p.version, want)
		}
	}
}
