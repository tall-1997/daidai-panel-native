package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"daidai-panel/model"
	"daidai-panel/testutil"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST request, got %s", r.Method)
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

	release := &panelReleaseInfo{
		TagName: "v2.2.19",
		Name:    "v2.2.19",
		Assets: []panelReleaseAsset{
			{
				Name:               "daidai-windows-amd64.zip",
				BrowserDownloadURL: "https://example.com/daidai-windows-amd64.zip",
			},
		},
	}

	plan, err := buildPanelUpdatePlanForRelease(release)
	if err != nil {
		t.Fatalf("expected binary fallback when only docker env vars leak, got %v", err)
	}
	if plan.DeploymentType != panelUpdateDeploymentBinary {
		t.Fatalf("expected binary fallback plan, got %#v", plan)
	}
	if plan.AssetName != "daidai-windows-amd64.zip" {
		t.Fatalf("expected windows binary asset fallback, got %#v", plan)
	}
}
