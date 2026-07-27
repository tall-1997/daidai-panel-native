package mobilecore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/router"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type testResult struct {
	OK                   bool                      `json:"ok"`
	ID                   int64                     `json:"id"`
	Running              bool                      `json:"running"`
	Status               string                    `json:"status"`
	Endpoint             string                    `json:"endpoint"`
	Error                string                    `json:"error"`
	ErrorCode            string                    `json:"errorCode"`
	CleanupRequired      bool                      `json:"cleanupRequired"`
	ProcessRequirement   string                    `json:"processRequirement"`
	PlatformCapabilities router.CapabilitySnapshot `json:"platformCapabilities"`
}

const testLocalToken = "0123456789abcdef0123456789abcdef"

type localTransport struct{ base http.RoundTripper }

func (transport localTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	request.Header.Set("X-Daidai-Local-Token", testLocalToken)
	request.Header.Set("Origin", request.URL.Scheme+"://"+request.URL.Host)
	return transport.base.RoundTrip(request)
}

func localClient() *http.Client {
	return &http.Client{Transport: localTransport{base: http.DefaultTransport}, Timeout: time.Second}
}

func decodeResult(t *testing.T, raw string) testResult {
	t.Helper()
	var result testResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return result
}

func startForTest(t *testing.T, dataDir string) testResult {
	t.Helper()
	options, err := json.Marshal(map[string]any{
		"dataDir":    dataDir,
		"localToken": testLocalToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	result := decodeResult(t, StartCore(string(options)))
	if !result.OK {
		t.Fatalf("start core: %s", result.Error)
	}
	t.Cleanup(func() {
		_ = StopCore(1000)
	})
	return result
}

func TestStartCoreBootstrapsUpstreamApplication(t *testing.T) {
	dataDir := t.TempDir()
	started := startForTest(t, dataDir)

	if started.ID == 0 || started.Status != "running" {
		t.Fatalf("unexpected start result: %+v", started)
	}
	if started.ProcessRequirement != `android:process=":panel"` {
		t.Fatalf("missing dedicated process requirement: %+v", started)
	}
	if started.Endpoint == "" || started.Endpoint != CoreEndpoint() {
		t.Fatalf("unexpected endpoint: result=%q exported=%q", started.Endpoint, CoreEndpoint())
	}
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(started.Endpoint, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%d", config.C.Server.Port); got != portText {
		t.Fatalf("config port=%s endpoint port=%s", got, portText)
	}

	for _, dir := range []string{"scripts", "logs"} {
		if info, err := os.Stat(filepath.Join(dataDir, dir)); err != nil || !info.IsDir() {
			t.Fatalf("expected %s directory: %v", dir, err)
		}
	}
	for _, file := range []string{"daidai.db", ".jwt_secret"} {
		if info, err := os.Stat(filepath.Join(dataDir, file)); err != nil || info.IsDir() {
			t.Fatalf("expected %s file: %v", file, err)
		}
	}

	tests := []struct {
		path       string
		wantStatus int
	}{
		{path: "/api/health", wantStatus: http.StatusOK},
		{path: "/api/auth/check-init", wantStatus: http.StatusOK},
		{path: "/api/tasks", wantStatus: http.StatusUnauthorized},
		{path: "/api/v1/auth/check-init", wantStatus: http.StatusOK},
		{path: "/api/v1/tasks", wantStatus: http.StatusUnauthorized},
	}
	client := localClient()
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			response, err := client.Get(started.Endpoint + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("GET %s status=%d want=%d", tt.path, response.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestLocalTokenNeverAppearsInResultOrLog(t *testing.T) {
	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldWriter) })
	options, err := json.Marshal(map[string]any{"dataDir": t.TempDir(), "localToken": testLocalToken})
	if err != nil {
		t.Fatal(err)
	}
	raw := StartCore(string(options))
	t.Cleanup(func() { _ = StopCore(1000) })
	if strings.Contains(raw, testLocalToken) || strings.Contains(logs.String(), testLocalToken) {
		t.Fatal("local token leaked through JSON or logs")
	}
}

func TestStartCorePublishesImmutableCapabilitySnapshot(t *testing.T) {
	options, err := json.Marshal(map[string]any{
		"dataDir":    t.TempDir(),
		"localToken": testLocalToken,
		"platformCapabilities": map[string]any{
			"version": 1,
			"capabilities": map[string]any{
				router.CapabilityTaskExecution: map[string]any{
					"state":      router.CapabilityEnabled,
					"adapterId":  "android.process-supervisor",
					"reasonCode": "READY",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := decodeResult(t, StartCore(string(options)))
	t.Cleanup(func() { _ = StopCore(1000) })
	if !started.OK || started.PlatformCapabilities.Version != 1 {
		t.Fatalf("unexpected start result: %+v", started)
	}
	state := started.PlatformCapabilities.Capabilities[router.CapabilityTaskExecution]
	if state.State != router.CapabilityEnabled || state.AdapterID != "android.process-supervisor" {
		t.Fatalf("unexpected capability snapshot: %+v", started.PlatformCapabilities)
	}
	if strings.Contains(CoreStatus(), testLocalToken) {
		t.Fatal("local token leaked through capability status")
	}
	started.PlatformCapabilities.Capabilities[router.CapabilityTaskExecution] = router.CapabilityState{State: router.CapabilityDisabled}
	status := decodeResult(t, CoreStatus())
	if status.PlatformCapabilities.Capabilities[router.CapabilityTaskExecution].State != router.CapabilityEnabled {
		t.Fatal("caller mutation changed core-owned capability snapshot")
	}
	if stopped := decodeResult(t, StopCore(1000)); stopped.PlatformCapabilities.Capabilities[router.CapabilityTaskExecution].State != router.CapabilityEnabled {
		t.Fatal("stop status omitted the core capability snapshot")
	}
}

func TestStartCoreRejectsInvalidCapabilityVersionAndState(t *testing.T) {
	for _, capabilities := range []string{
		`{"version":2}`,
		`{"version":1,"capabilities":{"task_execution":{"state":"unknown"}}}`,
	} {
		result := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+testLocalToken+`","platformCapabilities":`+capabilities+`}`))
		if result.OK || result.ErrorCode != codeInvalidOptions {
			t.Fatalf("unexpected capability validation result: %+v", result)
		}
	}
}

func TestMobileCoreCapabilityGuardsExecutionRoutesAndInitializesAdmin(t *testing.T) {
	started := startForTest(t, t.TempDir())
	client := localClient()

	unauthenticated, err := http.NewRequest(http.MethodPut, started.Endpoint+"/api/tasks/1/run", nil)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticatedResponse, err := client.Do(unauthenticated)
	if err != nil {
		t.Fatal(err)
	}
	unauthenticatedResponse.Body.Close()
	if unauthenticatedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("JWT route without JWT status=%d want=401", unauthenticatedResponse.StatusCode)
	}

	body := bytes.NewBufferString(`{"username":"mobile_admin","password":"secret123"}`)
	response, err := client.Post(started.Endpoint+"/api/auth/init", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("admin init status=%d", response.StatusCode)
	}
	accessToken, err := middleware.GenerateAccessToken("mobile_admin", "admin")
	if err != nil {
		t.Fatal(err)
	}

	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPut, path: "/api/tasks/1/run"},
		{method: http.MethodPut, path: "/api/tasks/1/stop"},
		{method: http.MethodPost, path: "/api/tasks/batch/run"},
		{method: http.MethodPost, path: "/api/scripts/run"},
		{method: http.MethodPost, path: "/api/deps"},
		{method: http.MethodPut, path: "/api/subscriptions/1/pull"},
		{method: http.MethodPost, path: "/api/system/restart"},
	} {
		req, err := http.NewRequest(request.method, started.Endpoint+request.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", request.method, request.path, err)
		}
		payload, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusConflict || !bytes.Contains(payload, []byte(`"errorCode":"PLATFORM_CAPABILITY"`)) {
			t.Fatalf("%s %s status=%d want=409 PLATFORM_CAPABILITY body=%s", request.method, request.path, response.StatusCode, payload)
		}
	}

	check, err := client.Get(started.Endpoint + "/api/auth/check-init")
	if err != nil {
		t.Fatal(err)
	}
	defer check.Body.Close()
	payload, err := io.ReadAll(check.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"need_init":false`)) {
		t.Fatalf("admin initialization did not persist: %s", payload)
	}
}

func TestManagementSecurityBoundary(t *testing.T) {
	started := startForTest(t, t.TempDir())
	host := strings.TrimPrefix(started.Endpoint, "http://")
	tests := []struct {
		name, token, host, origin string
		want                      int
	}{
		{name: "missing token", host: host, want: http.StatusUnauthorized},
		{name: "wrong token different length", token: "wrong", host: host, origin: started.Endpoint, want: http.StatusUnauthorized},
		{name: "wrong token same length", token: strings.Repeat("x", 32), host: host, origin: started.Endpoint, want: http.StatusUnauthorized},
		{name: "wrong host", token: testLocalToken, host: "localhost:1", want: http.StatusForbidden},
		{name: "wrong origin", token: testLocalToken, host: host, origin: "http://evil.invalid", want: http.StatusForbidden},
		{name: "empty origin rejected", token: testLocalToken, host: host, want: http.StatusForbidden},
		{name: "exact origin accepted", token: testLocalToken, host: host, origin: started.Endpoint, want: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, started.Endpoint+"/api/auth/check-init", nil)
			req.Host = tt.host
			req.Header.Set("X-Daidai-Local-Token", tt.token)
			req.Header.Set("Origin", tt.origin)
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.want {
				t.Fatalf("status=%d want=%d", response.StatusCode, tt.want)
			}
		})
	}
}

func TestBootstrapLogsRedactPrivatePaths(t *testing.T) {
	dataDir := t.TempDir()
	legacyVenv := filepath.Join(dataDir, "deps", "python", "venv")
	if err := os.MkdirAll(filepath.Join(legacyVenv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	startForTest(t, dataDir)
	output := logs.String()
	for _, privatePath := range []string{
		dataDir,
		filepath.Join(dataDir, "daidai.db"),
		filepath.Join(dataDir, "scripts"),
	} {
		if strings.Contains(output, privatePath) {
			t.Fatalf("private path %q leaked in bootstrap logs: %q", privatePath, output)
		}
	}
}

func TestStartCoreRequiresStrongLocalToken(t *testing.T) {
	for _, token := range []string{"", "short"} {
		result := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+token+`"}`))
		if result.OK || result.ErrorCode != "INVALID_LOCAL_TOKEN" {
			t.Fatalf("unexpected result: %+v", result)
		}
	}
}

func TestStartCoreRejectsDuplicateStart(t *testing.T) {
	started := startForTest(t, t.TempDir())
	duplicate := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+testLocalToken+`"}`))
	if duplicate.OK || !strings.Contains(duplicate.Error, "already running") {
		t.Fatalf("unexpected duplicate result: %+v", duplicate)
	}
	if CoreEndpoint() != started.Endpoint {
		t.Fatalf("duplicate start changed endpoint to %q", CoreEndpoint())
	}
}

func TestStopCoreAllowsRestart(t *testing.T) {
	previousConfig := &config.Config{Server: config.ServerConfig{Mode: "previous"}}
	previousDB := database.DB
	previousProxies := middleware.CurrentTrustedProxyCIDRs()
	config.C = previousConfig
	if err := middleware.ConfigureTrustedProxyCIDRs("203.0.113.0/24"); err != nil {
		t.Fatal(err)
	}
	wantProxies := middleware.CurrentTrustedProxyCIDRs()
	t.Cleanup(func() {
		config.C = nil
		database.DB = previousDB
		_ = middleware.ConfigureTrustedProxyCIDRs(strings.Join(previousProxies, ","))
	})

	first := startForTest(t, t.TempDir())
	stopped := decodeResult(t, StopCore(1000))
	if !stopped.OK || stopped.Status != "stopped" {
		t.Fatalf("unexpected stop result: %+v", stopped)
	}
	if CoreEndpoint() != "" {
		t.Fatalf("endpoint remains after stop: %q", CoreEndpoint())
	}
	status := decodeResult(t, CoreStatus())
	if status.Running || status.Status != "stopped" {
		t.Fatalf("unexpected stopped status: %+v", status)
	}
	if config.C != previousConfig || database.DB != previousDB || !reflect.DeepEqual(middleware.CurrentTrustedProxyCIDRs(), wantProxies) {
		t.Fatal("global state was not restored after normal stop")
	}

	second := startForTest(t, t.TempDir())
	if second.ID == first.ID {
		t.Fatalf("restart reused lifecycle id %d", second.ID)
	}
}

func TestStartCoreRejectsFixedPort(t *testing.T) {
	result := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+testLocalToken+`","port":5701}`))
	if result.OK || result.ErrorCode != "INVALID_PORT" || result.Error != "port must be 0" {
		t.Fatalf("unexpected fixed port result: %+v", result)
	}
}

func TestStopCoreRejectsInvalidTimeout(t *testing.T) {
	started := startForTest(t, t.TempDir())
	result := decodeResult(t, StopCore(0))
	if result.OK || result.ErrorCode != "INVALID_TIMEOUT" || result.Error != "timeoutMillis must be greater than 0" {
		t.Fatalf("unexpected invalid timeout result: %+v", result)
	}
	if CoreEndpoint() != started.Endpoint {
		t.Fatal("invalid timeout changed running core")
	}
}

func TestDatabaseCloseFailureRequiresCleanupAndCanRetry(t *testing.T) {
	startForTest(t, t.TempDir())
	oldClose := closeDatabase
	closeDatabase = func() error { return errors.New("sensitive close detail") }
	result := decodeResult(t, StopCore(1000))
	if result.OK || result.Status != "cleanup_required" || !result.CleanupRequired || result.Running {
		t.Fatalf("unexpected close failure state: %+v", result)
	}
	if CoreEndpoint() != "" {
		t.Fatalf("endpoint exposed during cleanup: %q", CoreEndpoint())
	}
	closeDatabase = oldClose
	t.Cleanup(func() { closeDatabase = oldClose })
	if retried := decodeResult(t, StopCore(1000)); !retried.OK {
		t.Fatalf("cleanup retry failed: %+v", retried)
	}
}

func TestStartFailureRestoresGlobalState(t *testing.T) {
	previousConfig := &config.Config{Server: config.ServerConfig{Mode: "previous"}}
	previousDB := database.DB
	previousProxies := middleware.CurrentTrustedProxyCIDRs()
	config.C = previousConfig
	if err := middleware.ConfigureTrustedProxyCIDRs("203.0.113.0/24"); err != nil {
		t.Fatal(err)
	}
	wantProxies := middleware.CurrentTrustedProxyCIDRs()
	oldInit := initApp
	initApp = func(cfg *config.Config, _ io.Writer) error {
		config.C = cfg
		database.DB = nil
		if err := middleware.ConfigureTrustedProxyCIDRs(""); err != nil {
			t.Fatal(err)
		}
		return errors.New("sensitive /tmp/database detail")
	}
	t.Cleanup(func() {
		initApp = oldInit
		config.C = nil
		database.DB = previousDB
		_ = middleware.ConfigureTrustedProxyCIDRs(strings.Join(previousProxies, ","))
	})

	result := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+testLocalToken+`"}`))
	if result.OK || result.ErrorCode != "BOOTSTRAP_FAILED" || result.Error != "core bootstrap failed" {
		t.Fatalf("unexpected bootstrap result: %+v", result)
	}
	if config.C != previousConfig || database.DB != previousDB || !reflect.DeepEqual(middleware.CurrentTrustedProxyCIDRs(), wantProxies) {
		t.Fatal("global state was not restored after start failure")
	}
	if strings.Contains(result.Error, "/tmp") || strings.Contains(result.Error, "database") {
		t.Fatalf("unsafe detail leaked: %q", result.Error)
	}
}

func TestStopTimeoutKeepsDatabaseAvailableAndCanRetry(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	oldSetup := setupRoutes
	setupRoutes = func(engine *gin.Engine, security router.ManagementSecurity, platform router.MobilePlatform) {
		oldSetup(engine, security, platform)
		engine.GET("/test/block", func(c *gin.Context) {
			close(entered)
			<-release
			var value int64
			if err := database.DB.Raw("SELECT 1").Scan(&value).Error; err != nil || value != 1 {
				c.Status(http.StatusInternalServerError)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
	t.Cleanup(func() { setupRoutes = oldSetup })

	started := startForTest(t, t.TempDir())
	requestDone := make(chan error, 1)
	go func() {
		response, err := localClient().Get(started.Endpoint + "/test/block")
		if response != nil {
			if response.StatusCode != http.StatusNoContent {
				err = errors.New("blocking handler database query failed")
			}
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-entered

	stopDone := make(chan testResult, 1)
	go func() { stopDone <- decodeResult(t, StopCore(20)) }()
	deadline := time.After(time.Second)
	for {
		status := decodeResult(t, CoreStatus())
		if status.Status == "stopping" {
			break
		}
		select {
		case <-deadline:
			t.Fatal("status did not become stopping")
		default:
		}
	}
	timedOut := <-stopDone
	if timedOut.OK || timedOut.ErrorCode != "SHUTDOWN_TIMEOUT" || timedOut.Error != "core shutdown timed out" {
		t.Fatalf("unexpected timeout result: %+v", timedOut)
	}
	if timedOut.Status != "stopping" || timedOut.Running || !timedOut.CleanupRequired || CoreEndpoint() != "" {
		t.Fatalf("timeout did not restore retryable running state: %+v", timedOut)
	}
	if database.DB == nil {
		t.Fatal("database cleared after shutdown timeout")
	}
	var count int64
	if err := database.DB.Raw("SELECT 1").Scan(&count).Error; err != nil || count != 1 {
		t.Fatalf("database unavailable after timeout: count=%d err=%v", count, err)
	}

	close(release)
	if err := <-requestDone; err != nil {
		t.Fatalf("blocking request failed: %v", err)
	}
	stopped := decodeResult(t, StopCore(1000))
	if !stopped.OK {
		t.Fatalf("retry stop failed: %+v", stopped)
	}
}

type failingListener struct {
	base     net.Listener
	closed   chan struct{}
	fail     chan struct{}
	accepted chan struct{}
	once     sync.Once
}

func (listener *failingListener) Accept() (net.Conn, error) {
	select {
	case <-listener.accepted:
		select {
		case <-listener.fail:
			return nil, errors.New("sensitive /tmp/accept detail")
		case <-listener.closed:
			return nil, net.ErrClosed
		}
	default:
	}
	connection, err := listener.base.Accept()
	if err == nil {
		listener.once.Do(func() { close(listener.accepted) })
	}
	return connection, err
}

func (listener *failingListener) Close() error {
	select {
	case <-listener.closed:
	default:
		close(listener.closed)
	}
	return listener.base.Close()
}

func (listener *failingListener) Addr() net.Addr {
	return listener.base.Addr()
}

func TestServeFailureIsObservableAndSafeDuringStop(t *testing.T) {
	oldListen := listenTCP
	var listener *failingListener
	listenTCP = func(network, address string) (net.Listener, error) {
		base, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		listener = &failingListener{base: base, closed: make(chan struct{}), fail: make(chan struct{}), accepted: make(chan struct{})}
		return listener, nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	oldSetup := setupRoutes
	setupRoutes = func(engine *gin.Engine, security router.ManagementSecurity, platform router.MobilePlatform) {
		oldSetup(engine, security, platform)
		engine.GET("/test/serve-failure-block", func(c *gin.Context) {
			close(entered)
			<-release
			c.Status(http.StatusNoContent)
		})
	}
	t.Cleanup(func() {
		listenTCP = oldListen
		setupRoutes = oldSetup
		_ = StopCore(1000)
	})

	started := startForTest(t, t.TempDir())
	var logs bytes.Buffer
	oldLogWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogWriter) })
	requestDone := make(chan error, 1)
	go func() {
		response, err := localClient().Get(started.Endpoint + "/test/serve-failure-block")
		if response != nil {
			response.Body.Close()
		}
		requestDone <- err
	}()
	<-entered
	close(listener.fail)
	for attempt := 0; attempt < 1000; attempt++ {
		status := decodeResult(t, CoreStatus())
		if status.Status == "failed" {
			if status.ErrorCode != "SERVE_FAILED" || status.Error != "core server failed" || status.Running || !status.CleanupRequired {
				t.Fatalf("unexpected failed status: %+v", status)
			}
			if CoreEndpoint() != "" || started.Endpoint == "" {
				t.Fatal("failed serve endpoint state is invalid")
			}
			if strings.Contains(logs.String(), "/tmp") || strings.Contains(logs.String(), "accept detail") {
				t.Fatalf("unsafe diagnostic detail leaked to log: %q", logs.String())
			}
			if !strings.Contains(logs.String(), "SERVE_FAILED") {
				t.Fatalf("stable diagnostic code missing: %q", logs.String())
			}
			if database.DB == nil {
				t.Fatal("serve failure closed database before StopCore")
			}
			var value int64
			if err := database.DB.Raw("SELECT 1").Scan(&value).Error; err != nil || value != 1 {
				t.Fatalf("database unavailable after serve failure: value=%d err=%v", value, err)
			}
			close(release)
			if err := <-requestDone; err != nil {
				t.Fatalf("blocking request failed: %v", err)
			}
			if stopped := decodeResult(t, StopCore(1000)); !stopped.OK {
				t.Fatalf("StopCore did not converge serve failure: %+v", stopped)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("serve failure was not observable: %s", CoreStatus())
}

func TestServeFailureConcurrentWithStopLeavesNoCore(t *testing.T) {
	oldListen := listenTCP
	var listener *failingListener
	listenTCP = func(network, address string) (net.Listener, error) {
		base, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		listener = &failingListener{base: base, closed: make(chan struct{}), fail: make(chan struct{}), accepted: make(chan struct{})}
		return listener, nil
	}
	t.Cleanup(func() {
		listenTCP = oldListen
		_ = StopCore(1000)
	})

	startForTest(t, t.TempDir())
	client := localClient()
	response, err := client.Get(CoreEndpoint() + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	stopDone := make(chan testResult, 1)
	go func() { stopDone <- decodeResult(t, StopCore(1000)) }()
	close(listener.fail)
	select {
	case result := <-stopDone:
		if !result.OK && result.ErrorCode != "NOT_RUNNING" {
			t.Fatalf("unexpected concurrent stop result: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent serve failure and stop deadlocked")
	}
	for attempt := 0; attempt < 1000 && decodeResult(t, CoreStatus()).Running; attempt++ {
		time.Sleep(time.Millisecond)
	}
	if decodeResult(t, CoreStatus()).Running {
		t.Fatal("core remained running after concurrent failure and stop")
	}
}

func TestStopRestoresTimezoneEnvironmentAndGinMode(t *testing.T) {
	oldLocal := time.Local
	oldTZ, oldTZSet := os.LookupEnv("TZ")
	oldTimezone := service.CurrentPanelTimezone()
	oldGinMode := gin.Mode()
	if err := service.ApplyPanelTimezone("Asia/Tokyo"); err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		_ = service.RestorePanelTimezoneState(service.PanelTimezoneState{
			Location: oldLocal,
			Name:     oldTimezone,
			TZSet:    oldTZSet,
			TZ:       oldTZ,
		})
		gin.SetMode(oldGinMode)
	})

	startForTest(t, t.TempDir())
	if result := decodeResult(t, StopCore(1000)); !result.OK {
		t.Fatalf("stop core: %+v", result)
	}
	if time.Local.String() != "Asia/Tokyo" || service.CurrentPanelTimezone() != "Asia/Tokyo" {
		t.Fatal("timezone state was not restored")
	}
	if value, exists := os.LookupEnv("TZ"); !exists || value != "Asia/Tokyo" {
		t.Fatalf("TZ was not restored: exists=%v value=%q", exists, value)
	}
	if gin.Mode() != gin.TestMode {
		t.Fatalf("gin mode=%q want=%q", gin.Mode(), gin.TestMode)
	}
}

func TestHTTPClientTimeout(t *testing.T) {
	var once sync.Once
	oldSetup := setupRoutes
	setupRoutes = func(engine *gin.Engine, security router.ManagementSecurity, platform router.MobilePlatform) {
		oldSetup(engine, security, platform)
		engine.GET("/test/wait", func(c *gin.Context) {
			once.Do(func() {})
			<-c.Request.Context().Done()
		})
	}
	t.Cleanup(func() { setupRoutes = oldSetup })

	started := startForTest(t, t.TempDir())
	client := localClient()
	client.Timeout = 20 * time.Millisecond
	_, err := client.Get(started.Endpoint + "/test/wait")
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected client timeout, got %v", err)
	}
}

func TestStartCoreRejectsNonLoopbackHost(t *testing.T) {
	result := decodeResult(t, StartCore(`{"dataDir":"`+t.TempDir()+`","localToken":"`+testLocalToken+`","bindHost":"0.0.0.0"}`))
	if result.OK || !strings.Contains(result.Error, "127.0.0.1") {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	if decodeResult(t, CoreStatus()).Running {
		t.Fatal("core running after rejected options")
	}
}
