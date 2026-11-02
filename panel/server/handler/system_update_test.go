package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"daidai-panel/model"
	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func TestResolveUpdateImageTargetUsesMirrorForDockerHubImage(t *testing.T) {
	pullImage, mirrorHost, registryURL := resolveUpdateImageTarget("linzixuanzz/daidai-panel:latest", "docker.1ms.run")

	if pullImage != "docker.1ms.run/linzixuanzz/daidai-panel:latest" {
		t.Fatalf("expected mirrored pull image, got %q", pullImage)
	}
	if mirrorHost != "docker.1ms.run" {
		t.Fatalf("expected mirror host docker.1ms.run, got %q", mirrorHost)
	}
	if registryURL != "https://docker.1ms.run/v2/" {
		t.Fatalf("expected mirror registry url, got %q", registryURL)
	}
}

func TestResolveBinaryUpdateDownloadURLUsesConfiguredProxy(t *testing.T) {
	testutil.SetupTestEnv(t)

	assetURL := "https://github.com/linzixuanzz/daidai-panel/releases/download/v2.2.17/daidai-linux-amd64.tar.gz"
	if got := resolveBinaryUpdateDownloadURL(assetURL); got != assetURL {
		t.Fatalf("expected direct asset URL without proxy, got %q", got)
	}

	if err := model.SetConfig("binary_update_proxy", "https://gh-proxy.org/"); err != nil {
		t.Fatalf("set binary_update_proxy: %v", err)
	}
	expected := "https://gh-proxy.org/" + assetURL
	if got := resolveBinaryUpdateDownloadURL(assetURL); got != expected {
		t.Fatalf("expected proxied asset URL %q, got %q", expected, got)
	}
}

func TestResolveUpdateImageTargetStripsExplicitDockerHubHost(t *testing.T) {
	pullImage, mirrorHost, registryURL := resolveUpdateImageTarget("docker.io/linzixuanzz/daidai-panel:latest", "docker.1ms.run")

	if pullImage != "docker.1ms.run/linzixuanzz/daidai-panel:latest" {
		t.Fatalf("expected mirrored pull image without explicit docker.io prefix, got %q", pullImage)
	}
	if mirrorHost != "docker.1ms.run" {
		t.Fatalf("expected mirror host docker.1ms.run, got %q", mirrorHost)
	}
	if registryURL != "https://docker.1ms.run/v2/" {
		t.Fatalf("expected mirror registry url, got %q", registryURL)
	}
}

func TestResolveUpdateImageTargetKeepsCustomRegistryDirect(t *testing.T) {
	pullImage, mirrorHost, registryURL := resolveUpdateImageTarget("ghcr.io/acme/panel:latest", "docker.1ms.run")

	if pullImage != "ghcr.io/acme/panel:latest" {
		t.Fatalf("expected custom registry image to remain unchanged, got %q", pullImage)
	}
	if mirrorHost != "" {
		t.Fatalf("expected mirror host to be ignored for custom registry, got %q", mirrorHost)
	}
	if registryURL != "https://ghcr.io/v2/" {
		t.Fatalf("expected ghcr registry url, got %q", registryURL)
	}
}

func TestNormalizePanelUpdateImageNameUsesRollingDebianTag(t *testing.T) {
	got := normalizePanelUpdateImageName("linzixuanzz/daidai-panel:1.9.8-debian")
	if got != "linzixuanzz/daidai-panel:debian" {
		t.Fatalf("expected debian rolling tag, got %q", got)
	}
}

func TestNormalizePanelUpdateImageNameUsesRollingLatestTag(t *testing.T) {
	got := normalizePanelUpdateImageName("docker.io/linzixuanzz/daidai-panel:1.9.8")
	if got != "docker.io/linzixuanzz/daidai-panel:latest" {
		t.Fatalf("expected latest rolling tag, got %q", got)
	}
}

func TestNormalizePanelUpdateImageNameKeepsCustomRepo(t *testing.T) {
	got := normalizePanelUpdateImageName("ghcr.io/acme/panel:1.0.0")
	if got != "ghcr.io/acme/panel:1.0.0" {
		t.Fatalf("expected custom repo to stay unchanged, got %q", got)
	}
}

func TestNormalizePanelUpdateImageNamePreservesRuntimeVariants(t *testing.T) {
	const repository = "linzixuanzz/daidai-panel:"
	cases := []struct {
		name    string
		input   string
		want    string
		channel string
	}{
		// 十个正式滚动标签必须原样保留。
		{"latest", repository + "latest", repository + "latest", "latest"},
		{"debian", repository + "debian", repository + "debian", "debian"},
		{"latest full", repository + "latest-full", repository + "latest-full", "latest"},
		{"debian full", repository + "debian-full", repository + "debian-full", "debian"},
		{"latest 3.10", repository + "latest-3.10", repository + "latest-3.10", "latest"},
		{"latest 3.11", repository + "latest-3.11", repository + "latest-3.11", "latest"},
		{"debian 3.10", repository + "debian-3.10", repository + "debian-3.10", "debian"},
		{"debian 3.11", repository + "debian-3.11", repository + "debian-3.11", "debian"},
		{"latest all", repository + "latest-all", repository + "latest-all", "latest"},
		{"debian all", repository + "debian-all", repository + "debian-all", "debian"},

		// 六个历史浮动标签继续更新，但统一转向新的连字符标签。
		{"legacy latest 3.10", repository + "latest3.10", repository + "latest-3.10", "latest"},
		{"legacy latest 3.11", repository + "latest3.11", repository + "latest-3.11", "latest"},
		{"legacy latest all", repository + "latestall", repository + "latest-all", "latest"},
		{"legacy debian 3.10", repository + "debian3.10", repository + "debian-3.10", "debian"},
		{"legacy debian 3.11", repository + "debian3.11", repository + "debian-3.11", "debian"},
		{"legacy debian all", repository + "debianall", repository + "debian-all", "debian"},

		// 固定版本号标签更新时必须回到同一镜像族的滚动标签。
		{"version latest", repository + "2.4.0", repository + "latest", "latest"},
		{"version latest with v", repository + "v2.4.0", repository + "latest", "latest"},
		{"version debian", repository + "2.4.0-debian", repository + "debian", "debian"},
		{"version latest full", repository + "2.4.0-full", repository + "latest-full", "latest"},
		{"version debian full", repository + "2.4.0-debian-full", repository + "debian-full", "debian"},
		{"version latest 3.10", repository + "2.4.0-3.10", repository + "latest-3.10", "latest"},
		{"version latest 3.11", repository + "2.4.0-3.11", repository + "latest-3.11", "latest"},
		{"version latest all", repository + "2.4.0-all", repository + "latest-all", "latest"},
		{"version debian 3.10", repository + "2.4.0-debian-3.10", repository + "debian-3.10", "debian"},
		{"version debian 3.11", repository + "2.4.0-debian-3.11", repository + "debian-3.11", "debian"},
		{"version debian all", repository + "2.4.0-debian-all", repository + "debian-all", "debian"},
		{"legacy version debian 3.10", repository + "2.4.0-debian3.10", repository + "debian-3.10", "debian"},
		{"legacy version debian 3.11", repository + "2.4.0-debian3.11", repository + "debian-3.11", "debian"},
		{"legacy version debian all", repository + "2.4.0-debianall", repository + "debian-all", "debian"},

		// 镜像加速域名属于地址前缀，不能被归一化过程删掉。
		{"mirror latest", "docker.1ms.run/" + repository + "2.4.0-3.10", "docker.1ms.run/" + repository + "latest-3.10", "latest"},
		{"mirror debian", "docker.1ms.run/" + repository + "2.4.0-debian-all", "docker.1ms.run/" + repository + "debian-all", "debian"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePanelUpdateImageName(tc.input)
			if got != tc.want {
				t.Fatalf("expected normalized image %q, got %q", tc.want, got)
			}
			if channel := resolvePanelUpdateChannel(got); channel != tc.channel {
				t.Fatalf("expected display channel %q, got %q", tc.channel, channel)
			}
		})
	}
}

func TestNormalizePanelUpdateImageNameKeepsUnknownOfficialTag(t *testing.T) {
	const imageName = "linzixuanzz/daidai-panel:preview-arm64"
	if got := normalizePanelUpdateImageName(imageName); got != imageName {
		t.Fatalf("expected unknown official tag to remain unchanged, got %q", got)
	}
}

func TestNormalizePanelUpdateImageNameKeepsDigestPinned(t *testing.T) {
	const imageName = "linzixuanzz/daidai-panel@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if got := normalizePanelUpdateImageName(imageName); got != imageName {
		t.Fatalf("expected digest-pinned image to remain unchanged, got %q", got)
	}
}

func TestSupportsDockerSocketPanelUpdateOnlyAllowsFullRollingTags(t *testing.T) {
	cases := []struct {
		name      string
		imageName string
		want      bool
	}{
		{"official latest full", "linzixuanzz/daidai-panel:latest-full", true},
		{"official debian full", "linzixuanzz/daidai-panel:debian-full", true},
		{"normalized fixed latest full", normalizePanelUpdateImageName("linzixuanzz/daidai-panel:2.4.0-full"), true},
		{"normalized fixed debian full", normalizePanelUpdateImageName("linzixuanzz/daidai-panel:2.4.0-debian-full"), true},
		{"official latest slim", "linzixuanzz/daidai-panel:latest", false},
		{"official debian slim", "linzixuanzz/daidai-panel:debian", false},
		{"official python variant", "linzixuanzz/daidai-panel:latest-3.12", false},
		{"custom repository slim", "ghcr.io/acme/panel:latest", false},
		{"custom repository full", "ghcr.io/acme/panel:latest-full", true},
		{"custom fixed full", "ghcr.io/acme/panel:2.4.0-full", false},
		{"untagged image", "linzixuanzz/daidai-panel", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := supportsDockerSocketPanelUpdate(tc.imageName); got != tc.want {
				t.Fatalf("expected support=%v for %q, got %v", tc.want, tc.imageName, got)
			}
		})
	}
}

func TestFormatPanelUpdatePullErrorAddsNetworkHint(t *testing.T) {
	plan := &panelUpdatePlan{
		ImageName:     "linzixuanzz/daidai-panel:latest",
		PullImageName: "docker.1ms.run/linzixuanzz/daidai-panel:latest",
		MirrorHost:    "docker.1ms.run",
		RegistryURL:   "https://docker.1ms.run/v2/",
	}

	err := formatPanelUpdatePullError(
		plan,
		errContextDeadlineExceeded,
		[]byte(`Get "https://docker.1ms.run/v2/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`),
	)

	msg := err.Error()
	if !strings.Contains(msg, "宿主机到镜像仓库的网络或 DNS 异常") {
		t.Fatalf("expected network hint in error message, got %q", msg)
	}
	if !strings.Contains(msg, "docker.1ms.run") {
		t.Fatalf("expected mirror host in error message, got %q", msg)
	}
}

func TestCollectVolumeMappingsKeepsCustomBindPath(t *testing.T) {
	info := &dockerInspectInfo{
		HostConfig: dockerInspectHostConfig{
			Binds: []string{
				"/srv/panel-data:/app/Dumb-Panel",
			},
		},
		Mounts: []dockerInspectMount{
			{Type: "bind", Source: "/srv/panel-data", Destination: "/app/Dumb-Panel", RW: true},
			{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
		},
	}

	got := collectVolumeMappings(info)
	if len(got) != 2 {
		t.Fatalf("expected two distinct volume mappings, got %v", got)
	}
	if got[0] != "/srv/panel-data:/app/Dumb-Panel" {
		t.Fatalf("expected custom data bind to be preserved, got %v", got)
	}
	if got[1] != "/var/run/docker.sock:/var/run/docker.sock" {
		t.Fatalf("expected docker socket bind to be preserved, got %v", got)
	}
}

func TestCollectVolumeMappingsPreservesNamedVolumeAlongsideBind(t *testing.T) {
	info := &dockerInspectInfo{
		HostConfig: dockerInspectHostConfig{
			Binds: []string{
				"/var/run/docker.sock:/var/run/docker.sock",
			},
		},
		Mounts: []dockerInspectMount{
			{Type: "volume", Name: "daidai_panel_data", Destination: "/app/Dumb-Panel", RW: true},
			{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
		},
	}

	got := collectVolumeMappings(info)
	if len(got) != 2 {
		t.Fatalf("expected both named volume and bind mount to be preserved, got %v", got)
	}

	gotSet := make(map[string]struct{}, len(got))
	for _, mapping := range got {
		gotSet[mapping] = struct{}{}
	}

	if _, exists := gotSet["daidai_panel_data:/app/Dumb-Panel"]; !exists {
		t.Fatalf("expected named data volume to be preserved, got %v", got)
	}
	if _, exists := gotSet["/var/run/docker.sock:/var/run/docker.sock"]; !exists {
		t.Fatalf("expected docker socket bind to be preserved, got %v", got)
	}
}

func TestCollectVolumeMappingsDeduplicatesEquivalentRWBindings(t *testing.T) {
	info := &dockerInspectInfo{
		HostConfig: dockerInspectHostConfig{
			Binds: []string{
				"/srv/panel-data:/app/Dumb-Panel:rw",
				"/var/run/docker.sock:/var/run/docker.sock:rw",
			},
		},
		Mounts: []dockerInspectMount{
			{Type: "bind", Source: "/srv/panel-data", Destination: "/app/Dumb-Panel", RW: true},
			{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
		},
	}

	got := collectVolumeMappings(info)
	if len(got) != 2 {
		t.Fatalf("expected equivalent rw bindings to be deduplicated, got %v", got)
	}

	gotSet := make(map[string]struct{}, len(got))
	for _, mapping := range got {
		gotSet[mapping] = struct{}{}
	}

	if _, exists := gotSet["/srv/panel-data:/app/Dumb-Panel:rw"]; !exists {
		t.Fatalf("expected original data bind to be preserved, got %v", got)
	}
	if _, exists := gotSet["/var/run/docker.sock:/var/run/docker.sock:rw"]; !exists {
		t.Fatalf("expected original docker socket bind to be preserved, got %v", got)
	}
}

func TestBuildContainerRunArgsPreservesCustomDataDirEnvAndMount(t *testing.T) {
	info := &dockerInspectInfo{
		HostConfig: dockerInspectHostConfig{
			Binds: []string{
				"/opt/daidai-data:/srv/custom-data",
				"/var/run/docker.sock:/var/run/docker.sock",
			},
		},
		Config: dockerInspectConfig{
			Env: []string{
				"TZ=Asia/Shanghai",
				"DATA_DIR=/srv/custom-data",
				"CONTAINER_NAME=daidai-panel",
				"IMAGE_NAME=linzixuanzz/daidai-panel:2.3.5-debianall",
				"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
			},
		},
		Mounts: []dockerInspectMount{
			{Type: "bind", Source: "/opt/daidai-data", Destination: "/srv/custom-data", RW: true},
			{Type: "bind", Source: "/var/run/docker.sock", Destination: "/var/run/docker.sock", RW: true},
		},
	}

	got := buildContainerRunArgs("daidai-panel", "linzixuanzz/daidai-panel:latest", info)

	if !slices.Contains(got, "/opt/daidai-data:/srv/custom-data") {
		t.Fatalf("expected custom data mount to be preserved, got %v", got)
	}
	if !slices.Contains(got, "DATA_DIR=/srv/custom-data") {
		t.Fatalf("expected custom DATA_DIR env to be preserved, got %v", got)
	}
	if slices.Contains(got, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("expected runtime PATH env to be filtered out, got %v", got)
	}
	if slices.Contains(got, "IMAGE_NAME=linzixuanzz/daidai-panel:2.3.5-debianall") {
		t.Fatalf("expected stale IMAGE_NAME env to be removed, got %v", got)
	}
	if !slices.Contains(got, "IMAGE_NAME=linzixuanzz/daidai-panel:latest") {
		t.Fatalf("expected IMAGE_NAME env to match the actual target image, got %v", got)
	}
	if got[len(got)-1] != "linzixuanzz/daidai-panel:latest" {
		t.Fatalf("expected image name to remain the final run arg, got %v", got)
	}
}

func TestResolveBinaryReleaseTargetMatchesReleaseAssets(t *testing.T) {
	cases := []struct {
		goos       string
		goarch     string
		assetName  string
		binaryName string
	}{
		{"windows", "amd64", "daidai-windows-amd64.zip", "daidai-server.exe"},
		{"linux", "amd64", "daidai-linux-amd64.tar.gz", "daidai-linux-amd64"},
		{"linux", "arm64", "daidai-linux-arm64.tar.gz", "daidai-linux-arm64"},
		{"linux", "386", "daidai-linux-386.tar.gz", "daidai-linux-386"},
		{"linux", "arm", "daidai-linux-armv7.tar.gz", "daidai-linux-armv7"},
	}

	for _, tc := range cases {
		assetName, binaryName, err := resolveBinaryReleaseTarget(tc.goos, tc.goarch)
		if err != nil {
			t.Fatalf("unexpected error for %s/%s: %v", tc.goos, tc.goarch, err)
		}
		if assetName != tc.assetName {
			t.Fatalf("expected asset %q for %s/%s, got %q", tc.assetName, tc.goos, tc.goarch, assetName)
		}
		if binaryName != tc.binaryName {
			t.Fatalf("expected binary %q for %s/%s, got %q", tc.binaryName, tc.goos, tc.goarch, binaryName)
		}
	}
}

func TestResolveBinaryReleaseTargetRejectsUnsupportedPlatform(t *testing.T) {
	if _, _, err := resolveBinaryReleaseTarget("darwin", "arm64"); err == nil {
		t.Fatalf("expected unsupported platform error")
	}
}

func TestPanelReleaseFindAssetByName(t *testing.T) {
	release := panelReleaseInfo{
		Assets: []panelReleaseAsset{
			{Name: "daidai-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
		},
	}

	asset, ok := release.findAsset("DAIDAI-LINUX-AMD64.TAR.GZ")
	if !ok {
		t.Fatalf("expected asset to be found case-insensitively")
	}
	if asset.BrowserDownloadURL != "https://example.com/linux" {
		t.Fatalf("unexpected asset url: %s", asset.BrowserDownloadURL)
	}
}

func TestSafeArchiveTargetPathRejectsTraversal(t *testing.T) {
	base := t.TempDir()
	if _, err := safeArchiveTargetPath(base, "../config.yaml"); err == nil {
		t.Fatalf("expected traversal path to be rejected")
	}
	if _, err := safeArchiveTargetPath(base, "web/../../config.yaml"); err == nil {
		t.Fatalf("expected nested traversal path to be rejected")
	}
}

func TestSafeArchiveTargetPathAllowsNestedFile(t *testing.T) {
	base := t.TempDir()
	got, err := safeArchiveTargetPath(base, "web/assets/app.js")
	if err != nil {
		t.Fatalf("expected nested file to be allowed: %v", err)
	}
	want := filepath.Join(base, "web", "assets", "app.js")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

var errContextDeadlineExceeded = errors.New("context deadline exceeded")

func TestNormalizeDockerImageID(t *testing.T) {
	valid := "sha256:" + strings.Repeat("A", 64)
	want := "sha256:" + strings.Repeat("a", 64)
	if got := normalizeDockerImageID(valid); got != want {
		t.Fatalf("expected normalized digest %q, got %q", want, got)
	}

	invalidValues := []string{
		"linzixuanzz/daidai-panel:latest",
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("g", 64),
		"",
	}
	for _, value := range invalidValues {
		if got := normalizeDockerImageID(value); got != "" {
			t.Fatalf("expected invalid image id %q to be rejected, got %q", value, got)
		}
	}
}

func TestBuildPanelUpdateHelperScriptCleansPreviousImageAfterSuccessfulRun(t *testing.T) {
	previousImageID := "sha256:" + strings.Repeat("b", 64)
	plan := &panelUpdatePlan{
		ContainerName:   "daidai-panel",
		PreviousImageID: previousImageID,
		RunArgs: []string{
			"run",
			"-d",
			"--name",
			"daidai-panel",
			"linzixuanzz/daidai-panel:latest",
		},
	}

	script := buildPanelUpdateHelperScript(plan)

	statusIndex := strings.Index(script, "status=$?")
	cleanupIndex := strings.Index(script, "docker image rm '"+previousImageID+"' >/dev/null 2>&1 || true")
	if cleanupIndex < 0 {
		t.Fatalf("expected helper script to clean previous image id, got:\n%s", script)
	}
	if statusIndex < 0 || cleanupIndex < statusIndex {
		t.Fatalf("expected previous image cleanup after docker run status capture, got:\n%s", script)
	}
	if !strings.Contains(script, "if [ \"$status\" -eq 0 ]; then") {
		t.Fatalf("expected cleanup to run only after successful container start, got:\n%s", script)
	}
	if !strings.Contains(script, "exit \"$status\"") {
		t.Fatalf("expected helper script to preserve docker run exit status, got:\n%s", script)
	}
}

func TestBuildPanelUpdateHelperScriptSkipsInvalidPreviousImageID(t *testing.T) {
	plan := &panelUpdatePlan{
		ContainerName:   "daidai-panel",
		PreviousImageID: "linzixuanzz/daidai-panel:latest",
		RunArgs:         []string{"run", "-d", "--name", "daidai-panel", "linzixuanzz/daidai-panel:latest"},
	}

	script := buildPanelUpdateHelperScript(plan)
	if strings.Contains(script, "docker image rm") {
		t.Fatalf("expected invalid previous image id to be ignored, got:\n%s", script)
	}
}

func TestTriggerWatchtowerUpdateAllowsNoContentSuccess(t *testing.T) {
	t.Setenv("CONTAINER_NAME", "panel.demo")
	t.Setenv("IMAGE_NAME", "registry.example.com/team/panel:stable")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/v1/update" || r.URL.Query().Get("async") != "true" {
			t.Fatalf("expected asynchronous Watchtower update request, got %s", r.URL.String())
		}
		if got := r.URL.Query().Get("container"); got != `^panel\.demo$` {
			t.Fatalf("expected request to target current container exactly, got %q", got)
		}
		if r.URL.Query().Has("image") {
			t.Fatalf("expected request to rely only on the exact container filter, got %s", r.URL.String())
		}
		if got := r.Header.Get("Authorization"); got != "Bearer demo-token" {
			t.Fatalf("expected bearer token header, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	result, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
		Managed:                true,
		APIURL:                 server.URL,
		APIToken:               "demo-token",
		ManualTriggerSupported: true,
	})
	if err != nil {
		t.Fatalf("expected 204 no content to be treated as success, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result map on success")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty payload for 204 response, got %#v", result)
	}
}

func TestTriggerWatchtowerUpdateAllowsEmpty200Body(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	result, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
		Managed:                true,
		APIURL:                 server.URL,
		APIToken:               "demo-token",
		ManualTriggerSupported: true,
	})
	if err != nil {
		t.Fatalf("expected 200 empty body to be treated as success, got %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result map on success")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty payload for empty 200 body, got %#v", result)
	}
}

func TestTriggerWatchtowerUpdateAllowsAcceptedText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("Accepted"))
	}))
	defer server.Close()

	result, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
		Managed:                true,
		APIURL:                 server.URL,
		APIToken:               "demo-token",
		ManualTriggerSupported: true,
	})
	if err != nil {
		t.Fatalf("expected 202 Accepted text to be treated as success, got %v", err)
	}
	if result["message"] != "Accepted" {
		t.Fatalf("expected Accepted response message to be preserved, got %#v", result)
	}
}

func TestTriggerWatchtowerUpdateReturnsErrorPayloadMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "watchtower upstream failed",
		})
	}))
	defer server.Close()

	_, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
		Managed:                true,
		APIURL:                 server.URL,
		APIToken:               "demo-token",
		ManualTriggerSupported: true,
	})
	if err == nil || !strings.Contains(err.Error(), "watchtower upstream failed") {
		t.Fatalf("expected error payload message to surface, got %v", err)
	}
}

// issue #108：Watchtower 低于 v1.20.0 时不会打开 API 端口，面板必须给出能自查的诊断，
// 而不是直接抛一句 dial tcp ...: connection refused。
// 起一个 httptest server 再立刻关掉，就能拿到一个必定连接被拒的地址来复现现场。
func TestTriggerWatchtowerUpdateExplainsUnreachableAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("closed server must not receive any request")
	}))
	apiURL := server.URL
	server.Close()

	_, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
		Managed:                true,
		APIURL:                 apiURL,
		APIToken:               "demo-token",
		ManualTriggerSupported: true,
	})
	if err == nil {
		t.Fatal("expected unreachable Watchtower API to fail")
	}

	message := err.Error()
	for _, keyword := range []string{
		"没有服务在监听",
		"v1.20.0",
		"WATCHTOWER_HTTP_API_ENDPOINTS",
		"com.centurylinklabs.watchtower.enable=false",
		"docker compose logs",
		"docker compose pull",
		"原始错误：",
	} {
		if !strings.Contains(message, keyword) {
			t.Fatalf("expected actionable diagnosis containing %q, got:\n%s", keyword, message)
		}
	}
	if !strings.Contains(message, "dial tcp") {
		t.Fatalf("expected the raw dial error to be preserved for advanced troubleshooting, got:\n%s", message)
	}
	if strings.Contains(message, "调用 Watchtower 更新接口失败") {
		t.Fatalf("expected the bare transport error to be replaced by the diagnosis, got:\n%s", message)
	}
	if strings.Contains(message, "daidai-watchtower") {
		t.Fatalf("expected no hardcoded watchtower container name, got:\n%s", message)
	}
}

// 「地址不可达」和「服务端返回非 2xx」是两类失败，后者的文案一个字都不该被改动。
func TestTriggerWatchtowerUpdateKeepsHTTPStatusFailureMessage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, "Unauthorized"},
		{"server error", http.StatusInternalServerError, "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			_, err := triggerWatchtowerUpdate(watchtowerRuntimeConfig{
				Managed:                true,
				APIURL:                 server.URL,
				APIToken:               "demo-token",
				ManualTriggerSupported: true,
			})
			if err == nil {
				t.Fatalf("expected HTTP %d to fail", tc.status)
			}
			expected := fmt.Sprintf("Watchtower 更新触发失败: HTTP %d: %s", tc.status, tc.body)
			if err.Error() != expected {
				t.Fatalf("expected non-2xx message to stay unchanged as %q, got %q", expected, err.Error())
			}
			if strings.Contains(err.Error(), "v1.20.0") {
				t.Fatalf("expected only unreachable failures to get the version diagnosis, got %q", err.Error())
			}
		})
	}
}

func TestWatchtowerUnreachableHintUsesConfiguredServiceName(t *testing.T) {
	// 用户可能把服务名改成别的，自查命令必须跟着 WATCHTOWER_HTTP_API_URL 的 host 走。
	hint := watchtowerUnreachableHint("http://my-wt:8080", errors.New("dial tcp 172.20.0.2:8080: connect: connection refused"))
	if !strings.Contains(hint, "docker compose logs my-wt") || !strings.Contains(hint, "docker compose pull my-wt") {
		t.Fatalf("expected self-check commands to use the configured service name, got:\n%s", hint)
	}
	if !strings.Contains(hint, "dial tcp 172.20.0.2:8080") {
		t.Fatalf("expected the original error to be preserved, got:\n%s", hint)
	}

	// host 是 IP / localhost 时推不出服务名，必须退回通用说法而不是拼出没法执行的命令。
	for _, apiURL := range []string{"http://172.20.0.2:8080", "http://localhost:8080"} {
		fallback := watchtowerUnreachableHint(apiURL, nil)
		if !strings.Contains(fallback, "<你的 watchtower 服务名>") {
			t.Fatalf("expected generic wording for %q, got:\n%s", apiURL, fallback)
		}
		if strings.Contains(fallback, "原始错误：") {
			t.Fatalf("expected no empty cause line when there is no underlying error, got:\n%s", fallback)
		}
	}
}

func TestWatchtowerAPIUnreachableOnlyMatchesTransportFailures(t *testing.T) {
	// 连接层失败：httptest 起完立刻关掉，直接请求就是 connection refused。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := server.URL
	server.Close()

	client := &http.Client{Timeout: 3 * time.Second}
	if _, err := client.Get(closedURL); !watchtowerAPIUnreachable(err) {
		t.Fatalf("expected connection refused to be treated as unreachable, got %v", err)
	}

	// 下面几种形态不依赖真实网络，直接按 net 包的错误类型构造，避免测试受环境 DNS 影响。
	cases := []struct {
		name        string
		err         error
		unreachable bool
	}{
		{
			// DNS 解析不了（no such host）
			name:        "dns failure",
			err:         &url.Error{Op: "Post", URL: "http://watchtower:8080", Err: &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "watchtower", IsNotFound: true}}},
			unreachable: true,
		},
		{
			// 拨号超时（i/o timeout）
			name:        "dial timeout",
			err:         &url.Error{Op: "Post", URL: "http://watchtower:8080", Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}},
			unreachable: true,
		},
		{
			// 连上之后读写才出错 —— 说明 API 其实在监听，不该套用「端口没打开」的诊断
			name:        "read failure after connect",
			err:         &url.Error{Op: "Post", URL: "http://watchtower:8080", Err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset by peer")}},
			unreachable: false,
		},
		{
			// 普通业务错误不能被误判成「端口没监听」
			name:        "plain business error",
			err:         errors.New("Watchtower 更新触发失败: HTTP 401"),
			unreachable: false,
		},
		{
			name:        "no error",
			err:         nil,
			unreachable: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchtowerAPIUnreachable(tc.err); got != tc.unreachable {
				t.Fatalf("expected unreachable=%v for %v, got %v", tc.unreachable, tc.err, got)
			}
		})
	}
}

func TestCheckWatchtowerAPIReachableCountsAnyHTTPResponse(t *testing.T) {
	// 探活只关心端口有没有人应答：401 也算通过，且绝不能碰到 /v1/update 真的触发一次更新。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1/update") {
			t.Fatalf("probe must not trigger an update, got %s", r.URL.String())
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", server.URL)
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")

	reachable, detail := CheckWatchtowerAPIReachable(3 * time.Second)
	if !reachable {
		t.Fatalf("expected an HTTP response to count as a listening API, got %q", detail)
	}
	if detail != "" {
		t.Fatalf("expected no detail on success, got %q", detail)
	}
}

func TestCheckWatchtowerAPIReachableSharesDiagnosisWithUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("closed server must not receive any request")
	}))
	apiURL := server.URL
	server.Close()

	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", apiURL)
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")

	reachable, detail := CheckWatchtowerAPIReachable(2 * time.Second)
	if reachable {
		t.Fatal("expected a closed port to fail the probe")
	}
	// ddp check 与面板一键更新必须给出同一份文案。
	expected := watchtowerUnreachableHint(apiURL, errors.New("placeholder"))
	head, _, _ := strings.Cut(expected, "\n原始错误：")
	if !strings.HasPrefix(detail, head) {
		t.Fatalf("expected the probe to reuse the shared diagnosis, got:\n%s", detail)
	}
	if !strings.Contains(detail, "v1.20.0") || !strings.Contains(detail, "docker compose pull") {
		t.Fatalf("expected actionable diagnosis from the probe, got:\n%s", detail)
	}
}

func TestCurrentWatchtowerRuntimeConfigRouting(t *testing.T) {
	cases := []struct {
		name          string
		manager       string
		apiURL        string
		apiToken      string
		managed       bool
		manualTrigger bool
	}{
		{"not configured", "", "", "", false, false},
		{"manager only", "watchtower", "", "", true, false},
		{"api url only", "", "http://watchtower:8080", "", false, false},
		{"complete", "watchtower", "http://watchtower:8080", "demo-token", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PANEL_UPDATE_MANAGER", tc.manager)
			t.Setenv("WATCHTOWER_HTTP_API_URL", tc.apiURL)
			t.Setenv("WATCHTOWER_HTTP_API_TOKEN", tc.apiToken)

			cfg := currentWatchtowerRuntimeConfig()
			if cfg.Managed != tc.managed {
				t.Fatalf("expected managed=%v, got %v", tc.managed, cfg.Managed)
			}
			if cfg.ManualTriggerSupported != tc.manualTrigger {
				t.Fatalf("expected manual trigger=%v, got %v", tc.manualTrigger, cfg.ManualTriggerSupported)
			}
		})
	}
}

func TestBuildPanelUpdatePlanInfoPrefersWatchtowerAndKeepsDebianFamily(t *testing.T) {
	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", "http://watchtower:8080")
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")
	t.Setenv("CONTAINER_NAME", "daidai-panel")
	t.Setenv("IMAGE_NAME", "docker.1ms.run/linzixuanzz/daidai-panel:debian-all")

	plan, err := BuildPanelUpdatePlanInfo()
	if err != nil {
		t.Fatalf("expected Watchtower plan, got %v", err)
	}
	if plan.UpdateManager != panelUpdateManagerWatchtower {
		t.Fatalf("expected Watchtower update manager, got %#v", plan)
	}
	if !plan.WatchtowerManualTriggerSupported {
		t.Fatalf("expected Watchtower manual trigger to be available, got %#v", plan)
	}
	if plan.ImageName != "docker.1ms.run/linzixuanzz/daidai-panel:debian-all" {
		t.Fatalf("expected Watchtower target to keep the actual rolling image tag, got %#v", plan)
	}
	if plan.Channel != "debian" {
		t.Fatalf("expected frontend display channel to remain debian, got %#v", plan)
	}

	// 静默更新会携带 release 调用同一个计划入口，也必须保持 Watchtower 分流。
	autoPlan, err := buildPanelUpdatePlanForRelease(&panelReleaseInfo{TagName: "v2.4.0"})
	if err != nil {
		t.Fatalf("expected auto update to build Watchtower plan, got %v", err)
	}
	if autoPlan.UpdateManager != panelUpdateManagerWatchtower || autoPlan.ImageName != plan.ImageName {
		t.Fatalf("expected auto update to keep Watchtower Debian target, got %#v", autoPlan)
	}
}

func TestBuildPanelUpdatePlanInfoRejectsPinnedWatchtowerImage(t *testing.T) {
	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", "http://watchtower:8080")
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")
	t.Setenv("IMAGE_NAME", "linzixuanzz/daidai-panel:2.4.0-debian-full")

	_, err := BuildPanelUpdatePlanInfo()
	if err == nil || !strings.Contains(err.Error(), "浮动标签") {
		t.Fatalf("expected pinned Watchtower image to explain rolling-tag requirement, got %v", err)
	}
}

func TestExecutePanelUpdateForCLIPrefersWatchtower(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", server.URL)
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")
	t.Setenv("IMAGE_NAME", "linzixuanzz/daidai-panel:latest-full")
	panelUpdater = newPanelUpdateManager()
	t.Cleanup(func() { panelUpdater = newPanelUpdateManager() })

	for attempt := 1; attempt <= 2; attempt++ {
		status, err := ExecutePanelUpdateForCLI()
		if err != nil {
			t.Fatalf("expected CLI Watchtower trigger %d to succeed, got %v", attempt, err)
		}
		if status.Status != "completed" || status.UpdateManager != panelUpdateManagerWatchtower || status.Phase != "watchtower-triggered" {
			t.Fatalf("expected terminal Watchtower CLI status, got %#v", status)
		}
		if status.PullImageName != "linzixuanzz/daidai-panel:latest-full" {
			t.Fatalf("expected Watchtower to keep the actual rolling full image tag, got %#v", status)
		}
	}
	if requests != 2 {
		t.Fatalf("expected completed status to allow a second Watchtower request, got %d requests", requests)
	}
}

func TestUpdatePanelReturnsCompletedWatchtowerStatus(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("async") != "true" || r.URL.Query().Get("container") != "^daidai-panel$" {
			t.Fatalf("expected targeted asynchronous request, got %s", r.URL.String())
		}
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("Accepted"))
	}))
	defer server.Close()

	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", server.URL)
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "demo-token")
	t.Setenv("CONTAINER_NAME", "daidai-panel")
	t.Setenv("IMAGE_NAME", "linzixuanzz/daidai-panel:debian-full")
	panelUpdater = newPanelUpdateManager()
	t.Cleanup(func() { panelUpdater = newPanelUpdateManager() })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/system/update", (&SystemHandler{}).UpdatePanel)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/system/update", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	var responseBody struct {
		Data panelUpdateStatusSnapshot `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &responseBody); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if responseBody.Data.Status != "completed" || responseBody.Data.UpdateManager != panelUpdateManagerWatchtower {
		t.Fatalf("expected completed Watchtower response, got %#v", responseBody.Data)
	}
	if requests != 1 {
		t.Fatalf("expected one Watchtower request, got %d", requests)
	}
}

func TestExecutePanelUpdateForCLIExplainsIncompleteWatchtowerConfig(t *testing.T) {
	t.Setenv("PANEL_UPDATE_MANAGER", "watchtower")
	t.Setenv("WATCHTOWER_HTTP_API_URL", "")
	t.Setenv("WATCHTOWER_HTTP_API_TOKEN", "")
	panelUpdater = newPanelUpdateManager()
	t.Cleanup(func() { panelUpdater = newPanelUpdateManager() })

	_, err := ExecutePanelUpdateForCLI()
	if err == nil {
		t.Fatal("expected incomplete Watchtower configuration to fail")
	}
	if !strings.Contains(err.Error(), "WATCHTOWER_HTTP_API_URL") || !strings.Contains(err.Error(), "WATCHTOWER_HTTP_API_TOKEN") {
		t.Fatalf("expected clear missing Watchtower configuration message, got %v", err)
	}
}

func TestShouldRequireDockerPanelUpdateIgnoresDockerEnvVarsOutsideContainer(t *testing.T) {
	t.Setenv("IMAGE_NAME", "linzixuanzz/daidai-panel:latest")
	t.Setenv("CONTAINER_NAME", "daidai-panel")

	if shouldRequireDockerPanelUpdate() {
		t.Fatal("expected docker env vars alone to not force docker-only update path outside container")
	}
}

func TestBuildPanelUpdatePlanForReleaseFallsBackToBinaryWhenDockerEnvVarsLeak(t *testing.T) {
	t.Setenv("IMAGE_NAME", "linzixuanzz/daidai-panel:latest")
	t.Setenv("CONTAINER_NAME", "daidai-panel")

	// 本用例验证的语义是「只有 Docker 环境变量泄漏、但并不在容器里时回退到二进制更新」，
	// 与宿主机是什么平台无关。因此 fixture 按 release.yml 实际发布的全部制品构造，
	// 保证任何受支持平台都能命中；断言目标再按当前 GOOS/GOARCH 推导，不写死单一平台制品名。
	expectedAsset, _, err := resolveBinaryReleaseTarget(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skipf("平台 %s/%s 本就没有二进制更新包，不存在可回退的方案: %v", runtime.GOOS, runtime.GOARCH, err)
	}

	release := &panelReleaseInfo{
		TagName: "v2.2.19",
		Name:    "v2.2.19",
	}
	for _, name := range []string{
		"daidai-windows-amd64.zip",
		"daidai-linux-amd64.tar.gz",
		"daidai-linux-arm64.tar.gz",
		"daidai-linux-386.tar.gz",
		"daidai-linux-armv7.tar.gz",
	} {
		release.Assets = append(release.Assets, panelReleaseAsset{
			Name:               name,
			BrowserDownloadURL: "https://example.com/" + name,
		})
	}

	plan, err := buildPanelUpdatePlanForRelease(release)
	if err != nil {
		t.Fatalf("expected binary fallback when only docker env vars leak, got %v", err)
	}
	if plan.DeploymentType != panelUpdateDeploymentBinary {
		t.Fatalf("expected binary fallback plan, got %#v", plan)
	}
	if plan.UpdateManager != panelUpdateManagerPanel {
		t.Fatalf("expected binary fallback to be managed by the panel itself, got %#v", plan)
	}
	if plan.AssetName != expectedAsset {
		t.Fatalf("expected current platform asset %q, got %#v", expectedAsset, plan)
	}
	if plan.AssetURL != "https://example.com/"+expectedAsset {
		t.Fatalf("expected plan to carry the matching asset url, got %#v", plan)
	}
}
