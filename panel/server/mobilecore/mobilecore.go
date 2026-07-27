package mobilecore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/appboot"
	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/router"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	codeAlreadyRunning    = "ALREADY_RUNNING"
	codeInvalidOptions    = "INVALID_OPTIONS"
	codeInvalidDataDir    = "INVALID_DATA_DIR"
	codeInvalidBindHost   = "INVALID_BIND_HOST"
	codeInvalidPort       = "INVALID_PORT"
	codeInvalidLocalToken = "INVALID_LOCAL_TOKEN"
	codeBootstrapFailed   = "BOOTSTRAP_FAILED"
	codeListenFailed      = "LISTEN_FAILED"
	codeInvalidTimeout    = "INVALID_TIMEOUT"
	codeNotRunning        = "NOT_RUNNING"
	codeShutdownTimeout   = "SHUTDOWN_TIMEOUT"
	codeShutdownFailed    = "SHUTDOWN_FAILED"
	codeDatabaseClose     = "DATABASE_CLOSE_FAILED"
	codeServeFailed       = "SERVE_FAILED"
)

type options struct {
	DataDir              string                    `json:"dataDir"`
	BindHost             string                    `json:"bindHost"`
	Port                 int                       `json:"port"`
	LocalToken           string                    `json:"localToken"`
	PlatformCapabilities router.CapabilitySnapshot `json:"platformCapabilities"`
}

type result struct {
	OK                   bool                      `json:"ok"`
	ID                   int64                     `json:"id,omitempty"`
	Running              bool                      `json:"running"`
	Status               string                    `json:"status"`
	Endpoint             string                    `json:"endpoint,omitempty"`
	ErrorCode            string                    `json:"errorCode,omitempty"`
	Error                string                    `json:"error,omitempty"`
	CleanupRequired      bool                      `json:"cleanupRequired"`
	ProcessRequirement   string                    `json:"processRequirement,omitempty"`
	PlatformCapabilities router.CapabilitySnapshot `json:"platformCapabilities"`
}

type globalState struct {
	config         *config.Config
	database       *gorm.DB
	trustedProxies []string
	timezone       service.PanelTimezoneState
	ginMode        string
}

type core struct {
	id           int64
	endpoint     string
	server       *http.Server
	listener     net.Listener
	previous     globalState
	connMu       sync.Mutex
	conns        map[net.Conn]struct{}
	connWait     chan struct{}
	capabilities router.CapabilitySnapshot
}

var (
	initApp       = appboot.InitWithConfigWriter
	setupRoutes   = router.SetupMobileFull
	listenTCP     = net.Listen
	closeDatabase = database.Close
)

type lifecycleState struct {
	mu           sync.Mutex
	operation    sync.Mutex
	nextID       int64
	core         *core
	capabilities router.CapabilitySnapshot
	status       string
	errorCode    string
	errorText    string
}

var lifecycle lifecycleState

func StartCore(optionsJSON string) string {
	lifecycle.operation.Lock()
	defer lifecycle.operation.Unlock()

	lifecycle.mu.Lock()
	if lifecycle.core != nil {
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeAlreadyRunning, "core already running", value)
	}
	lifecycle.mu.Unlock()

	parsed, code, message, diagnostic := parseOptions(optionsJSON)
	if code != "" {
		logDiagnostic(code, diagnostic)
		return failure(code, message, result{Status: "stopped"})
	}
	if err := createDataLayout(parsed.DataDir); err != nil {
		logDiagnostic(codeInvalidDataDir, "layout")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped"})
	}
	secret, err := loadOrCreateJWTSecret(parsed.DataDir)
	if err != nil {
		logDiagnostic(codeInvalidDataDir, "secret")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped"})
	}

	previous := captureGlobals()
	cfg := mobileConfig(parsed, secret)
	bootstrapWriter := newPrivatePathRedactor(log.Writer(), cfg.Data.Dir, cfg.Database.Path, cfg.Data.ScriptsDir, cfg.Data.LogDir)
	if err := initApp(cfg, bootstrapWriter); err != nil {
		logDiagnostic(codeBootstrapFailed, "appboot")
		restoreAfterStartFailure(previous)
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}

	listener, err := listenTCP("tcp", "127.0.0.1:0")
	if err != nil {
		logDiagnostic(codeListenFailed, "listen")
		restoreAfterStartFailure(previous)
		return failure(codeListenFailed, "core listener failed", result{Status: "stopped"})
	}
	port, err := listenerPort(listener.Addr())
	if err != nil {
		_ = listener.Close()
		logDiagnostic(codeListenFailed, "address")
		restoreAfterStartFailure(previous)
		return failure(codeListenFailed, "core listener failed", result{Status: "stopped"})
	}
	config.C.Server.Port = port
	actualHost := listener.Addr().String()
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	if err := engine.SetTrustedProxies(middleware.CurrentTrustedProxyCIDRs()); err != nil {
		_ = listener.Close()
		logDiagnostic(codeBootstrapFailed, "trusted-proxy")
		restoreAfterStartFailure(previous)
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	engine.Use(gin.Recovery())
	platform := router.NewMobilePlatform(parsed.PlatformCapabilities)
	setupRoutes(engine, router.ManagementSecurity{LocalToken: parsed.LocalToken, Host: actualHost}, platform)
	lifecycle.mu.Lock()
	lifecycle.nextID++
	running := &core{
		id:           lifecycle.nextID,
		endpoint:     "http://" + actualHost,
		listener:     listener,
		previous:     previous,
		conns:        make(map[net.Conn]struct{}),
		connWait:     make(chan struct{}),
		capabilities: cloneCapabilitySnapshot(parsed.PlatformCapabilities),
	}
	running.server = &http.Server{Handler: engine, ConnState: running.trackConnection}
	lifecycle.core = running
	lifecycle.capabilities = cloneCapabilitySnapshot(parsed.PlatformCapabilities)
	lifecycle.status = "running"
	lifecycle.errorCode = ""
	lifecycle.errorText = ""
	value := lifecycle.statusResultLocked()
	lifecycle.mu.Unlock()

	go observeServe(running)
	return encode(value)
}

func StopCore(timeoutMillis int64) string {
	if timeoutMillis <= 0 || timeoutMillis > int64(time.Duration(1<<63-1)/time.Millisecond) {
		return failure(codeInvalidTimeout, "timeoutMillis must be greater than 0", statusSnapshot())
	}

	lifecycle.operation.Lock()
	defer lifecycle.operation.Unlock()

	lifecycle.mu.Lock()
	if lifecycle.core == nil {
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeNotRunning, "core is not running", value)
	}
	running := lifecycle.core
	lifecycle.status = "stopping"
	lifecycle.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMillis)*time.Millisecond)
	err := running.server.Shutdown(ctx)
	if err == nil {
		err = running.waitForConnections(ctx)
	}
	cancel()
	if err != nil {
		lifecycle.mu.Lock()
		if lifecycle.core == running {
			lifecycle.status = "stopping"
			lifecycle.errorCode = codeShutdownTimeout
			lifecycle.errorText = "core shutdown timed out"
		}
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		if errors.Is(err, context.DeadlineExceeded) {
			return failure(codeShutdownTimeout, "core shutdown timed out", value)
		}
		logDiagnostic(codeShutdownFailed, "shutdown")
		return failure(codeShutdownFailed, "core shutdown failed", value)
	}
	if err := closeDatabase(); err != nil {
		logDiagnostic(codeDatabaseClose, "close")
		lifecycle.mu.Lock()
		lifecycle.status = "cleanup_required"
		lifecycle.errorCode = codeDatabaseClose
		lifecycle.errorText = "core database close failed"
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeDatabaseClose, "core database close failed", value)
	}
	restoreGlobals(running.previous)
	lifecycle.mu.Lock()
	lifecycle.core = nil
	lifecycle.status = "stopped"
	lifecycle.errorCode = ""
	lifecycle.errorText = ""
	value := lifecycle.statusResultLocked()
	lifecycle.mu.Unlock()
	return encode(value)
}

func CoreStatus() string {
	return encode(statusSnapshot())
}

func CoreEndpoint() string {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.core == nil || lifecycle.status != "running" {
		return ""
	}
	return lifecycle.core.endpoint
}

func observeServe(running *core) {
	err := running.server.Serve(running.listener)
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}

	lifecycle.operation.Lock()
	defer lifecycle.operation.Unlock()
	logDiagnostic(codeServeFailed, "serve")
	_ = running.listener.Close()
	lifecycle.mu.Lock()
	if lifecycle.core != running {
		lifecycle.mu.Unlock()
		return
	}
	lifecycle.status = "failed"
	lifecycle.errorCode = codeServeFailed
	lifecycle.errorText = "core server failed"
	lifecycle.mu.Unlock()
}

func (running *core) trackConnection(connection net.Conn, state http.ConnState) {
	running.connMu.Lock()
	defer running.connMu.Unlock()
	switch state {
	case http.StateNew:
		running.conns[connection] = struct{}{}
	case http.StateHijacked, http.StateClosed:
		delete(running.conns, connection)
		close(running.connWait)
		running.connWait = make(chan struct{})
	}
}

func (running *core) waitForConnections(ctx context.Context) error {
	for {
		running.connMu.Lock()
		if len(running.conns) == 0 {
			running.connMu.Unlock()
			return nil
		}
		wait := running.connWait
		running.connMu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func parseOptions(raw string) (options, string, string, string) {
	parsed := options{BindHost: "127.0.0.1", PlatformCapabilities: router.CapabilitySnapshot{Version: 1}}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return parsed, codeInvalidOptions, "invalid options JSON", "json"
	}
	if strings.TrimSpace(parsed.DataDir) == "" {
		return parsed, codeInvalidDataDir, "dataDir is required", "missing"
	}
	absDataDir, err := filepath.Abs(parsed.DataDir)
	if err != nil {
		return parsed, codeInvalidDataDir, "dataDir is unavailable", "resolve"
	}
	parsed.DataDir = absDataDir
	if parsed.BindHost != "127.0.0.1" {
		return parsed, codeInvalidBindHost, "bindHost must be 127.0.0.1", "host"
	}
	if parsed.Port != 0 {
		return parsed, codeInvalidPort, "port must be 0", "port"
	}
	if len([]byte(parsed.LocalToken)) < 32 {
		return parsed, codeInvalidLocalToken, "localToken must be at least 32 bytes", "local-token"
	}
	if parsed.PlatformCapabilities.Version != 1 {
		return parsed, codeInvalidOptions, "platformCapabilities.version must be 1", "capability-version"
	}
	if parsed.PlatformCapabilities.Capabilities == nil {
		parsed.PlatformCapabilities.Capabilities = map[string]router.CapabilityState{}
	}
	for _, state := range parsed.PlatformCapabilities.Capabilities {
		if state.State != router.CapabilityDisabled && state.State != router.CapabilityEnabled {
			return parsed, codeInvalidOptions, "platform capability state is invalid", "capability-state"
		}
	}
	return parsed, "", "", ""
}

func mobileConfig(parsed options, secret string) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Mode: gin.ReleaseMode},
		Database: config.DatabaseConfig{Path: filepath.Join(parsed.DataDir, "daidai.db")},
		JWT: config.JWTConfig{
			Secret:             secret,
			AccessTokenExpire:  480 * time.Hour,
			RefreshTokenExpire: 1440 * time.Hour,
		},
		Data: config.DataConfig{
			Dir:        parsed.DataDir,
			ScriptsDir: filepath.Join(parsed.DataDir, "scripts"),
			LogDir:     filepath.Join(parsed.DataDir, "logs"),
		},
	}
}

func captureGlobals() globalState {
	return globalState{
		config:         config.C,
		database:       database.DB,
		trustedProxies: middleware.CurrentTrustedProxyCIDRs(),
		timezone:       service.CapturePanelTimezoneState(),
		ginMode:        gin.Mode(),
	}
}

func restoreAfterStartFailure(previous globalState) {
	if database.DB != nil && database.DB != previous.database {
		if err := database.Close(); err != nil {
			logDiagnostic(codeDatabaseClose, "rollback")
		}
	}
	restoreGlobals(previous)
}

func restoreGlobals(previous globalState) {
	config.C = previous.config
	database.DB = previous.database
	if err := middleware.ConfigureTrustedProxyCIDRs(strings.Join(previous.trustedProxies, ",")); err != nil {
		logDiagnostic(codeBootstrapFailed, "restore-proxy")
	}
	if err := service.RestorePanelTimezoneState(previous.timezone); err != nil {
		logDiagnostic(codeBootstrapFailed, "restore-timezone")
	}
	gin.SetMode(previous.ginMode)
}

func listenerPort(address net.Addr) (int, error) {
	_, portText, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(portText)
}

func statusSnapshot() result {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.statusResultLocked()
}

func (state *lifecycleState) statusResultLocked() result {
	status := state.status
	if status == "" {
		status = "stopped"
	}
	value := result{
		OK:                 state.errorCode == "",
		Running:            state.core != nil && status == "running",
		Status:             status,
		ErrorCode:          state.errorCode,
		Error:              state.errorText,
		CleanupRequired:    state.core != nil && status != "running",
		ProcessRequirement: `android:process=":panel"`,
	}
	if state.core != nil {
		value.PlatformCapabilities = cloneCapabilitySnapshot(state.core.capabilities)
		value.ID = state.core.id
		if status == "running" {
			value.Endpoint = state.core.endpoint
		}
	}
	if value.PlatformCapabilities.Version == 0 {
		value.PlatformCapabilities = cloneCapabilitySnapshot(state.capabilities)
	}
	if value.PlatformCapabilities.Version == 0 {
		value.PlatformCapabilities = router.CapabilitySnapshot{Version: 1, Capabilities: map[string]router.CapabilityState{}}
	}
	return value
}

func cloneCapabilitySnapshot(snapshot router.CapabilitySnapshot) router.CapabilitySnapshot {
	cloned := router.CapabilitySnapshot{Version: snapshot.Version, Capabilities: make(map[string]router.CapabilityState, len(snapshot.Capabilities))}
	for id, state := range snapshot.Capabilities {
		cloned.Capabilities[id] = state
	}
	return cloned
}

func failure(code, message string, value result) string {
	value.OK = false
	value.ErrorCode = code
	value.Error = message
	return encode(value)
}

func createDataLayout(dataDir string) error {
	for _, dir := range []string{dataDir, filepath.Join(dataDir, "scripts"), filepath.Join(dataDir, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func loadOrCreateJWTSecret(dataDir string) (string, error) {
	path := filepath.Join(dataDir, ".jwt_secret")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return string(data), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(bytes)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

func logDiagnostic(code, context string) {
	log.Printf("mobile core diagnostic=%s context=%s", code, context)
}

func encode(value result) string {
	if value.ProcessRequirement == "" {
		value.ProcessRequirement = `android:process=":panel"`
	}
	data, _ := json.Marshal(value)
	return string(data)
}

type privatePathRedactor struct {
	destination io.Writer
	paths       []string
}

func newPrivatePathRedactor(destination io.Writer, paths ...string) io.Writer {
	cleaned := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(path))
		if slashPath := filepath.ToSlash(path); slashPath != path {
			cleaned = append(cleaned, slashPath)
		}
	}
	sort.Slice(cleaned, func(i, j int) bool { return len(cleaned[i]) > len(cleaned[j]) })
	return &privatePathRedactor{destination: destination, paths: cleaned}
}

func (writer *privatePathRedactor) Write(data []byte) (int, error) {
	redacted := string(data)
	for _, path := range writer.paths {
		redacted = strings.ReplaceAll(redacted, path, "<mobile-private-path>")
	}
	if _, err := io.WriteString(writer.destination, redacted); err != nil {
		return 0, err
	}
	return len(data), nil
}
