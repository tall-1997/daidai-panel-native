package mobilecore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	codeAlreadyRunning     = "ALREADY_RUNNING"
	codeInvalidOptions     = "INVALID_OPTIONS"
	codeInvalidDataDir     = "INVALID_DATA_DIR"
	codeInvalidBindHost    = "INVALID_BIND_HOST"
	codeInvalidPort        = "INVALID_PORT"
	codeInvalidLocalToken  = "INVALID_LOCAL_TOKEN"
	codeBootstrapFailed    = "BOOTSTRAP_FAILED"
	codeListenFailed       = "LISTEN_FAILED"
	codeInvalidTimeout     = "INVALID_TIMEOUT"
	codeNotRunning         = "NOT_RUNNING"
	codeShutdownTimeout    = "SHUTDOWN_TIMEOUT"
	codeShutdownFailed     = "SHUTDOWN_FAILED"
	codeDatabaseClose      = "DATABASE_CLOSE_FAILED"
	codeServeFailed        = "SERVE_FAILED"
	codeRecoveryFailed     = "RECOVERY_FAILED"
	currentRuntimeBaseline = "bundled-runtime-v0"
)

type options struct {
	DataDir              string                             `json:"dataDir"`
	BindHost             string                             `json:"bindHost"`
	Port                 int                                `json:"port"`
	LocalToken           string                             `json:"localToken"`
	NativeLibraryDir     string                             `json:"nativeLibraryDir"`
	AndroidFilesDir      string                             `json:"androidFilesDir"`
	AndroidCacheDir      string                             `json:"androidCacheDir"`
	LinuxRootfsDir       string                             `json:"linuxRootfsDir"`
	PRootPath            string                             `json:"prootPath"`
	PRootLoaderPath      string                             `json:"prootLoaderPath"`
	WebDir               string                             `json:"webDir"`
	AndroidKeystoreKey   string                             `json:"androidKeystoreMasterKey"`
	RuntimeManifestPath  string                             `json:"runtimeManifestPath"`
	RuntimeCompatPath    string                             `json:"runtimeCompatibilityPath"`
	RuntimeSmokePath     string                             `json:"runtimeSmokeEvidencePath"`
	RuntimeDepsPath      string                             `json:"runtimeDependenciesPath"`
	PlatformCapabilities router.CapabilitySnapshot          `json:"platformCapabilities"`
	SchedulerGuarantee   service.SchedulerGuaranteeSnapshot `json:"schedulerGuarantee"`
}

type result struct {
	OK                   bool                               `json:"ok"`
	ID                   int64                              `json:"id,omitempty"`
	Running              bool                               `json:"running"`
	Status               string                             `json:"status"`
	Endpoint             string                             `json:"endpoint,omitempty"`
	ErrorCode            string                             `json:"errorCode,omitempty"`
	Error                string                             `json:"error,omitempty"`
	CleanupRequired      bool                               `json:"cleanupRequired"`
	ProcessRequirement   string                             `json:"processRequirement,omitempty"`
	PlatformCapabilities router.CapabilitySnapshot          `json:"platformCapabilities"`
	SchedulerGuarantee   service.SchedulerGuaranteeSnapshot `json:"schedulerGuarantee"`
	RuntimeBaseline      service.RuntimeComponentBaseline   `json:"runtimeBaseline"`
	FailureStage         string                             `json:"failureStage,omitempty"`
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
	runtime      RuntimeContainer
	previous     globalState
	connMu       sync.Mutex
	conns        map[net.Conn]struct{}
	connWait     chan struct{}
	capabilities router.CapabilitySnapshot
	store        *generationStore
	generationID string
	baseline     generationBaseline
	browser      *router.LoopbackBrowserAccess
}

var (
	initApp                 = appboot.InitWithConfigWriterBeforeMigrate
	setupRoutes             = router.SetupMobileFull
	listenTCP               = net.Listen
	closeDatabase           = database.Close
	checkpointDB            = database.CheckpointWALAndClose
	openDatabase            = database.InitWithWriter
	integrityDB             = database.IntegrityCheck
	checkpointFlatDatabase  = database.CheckpointWALPath
	resolveListenerPort     = listenerPort
	configureTrustedProxies = func(engine *gin.Engine, proxies []string) error {
		return engine.SetTrustedProxies(proxies)
	}
	configureRoutes = func(engine *gin.Engine, security router.ManagementSecurity, platform router.MobilePlatform) error {
		setupRoutes(engine, security, platform)
		return nil
	}
	generationFilesystemOps = defaultFilesystemOps
	probeCoreReadiness      = probeHealthEndpoint
	newRuntimeContainer     = newServiceRuntimeContainer
	markGenerationReady     = func(store *generationStore, generationID string) error { return store.markReady(generationID) }
	runtimeWorkerStartGate  = func(prepared, recoveryConverged bool) bool {
		return recoveryConverged
	}
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
	cleanup      func() error
}

var lifecycle lifecycleState
var recoveryReady atomic.Bool

func StartCore(optionsJSON string) (response string) {
	lifecycle.operation.Lock()
	defer lifecycle.operation.Unlock()
	var recoveryCleanup func() error
	defer func() {
		var value result
		if json.Unmarshal([]byte(response), &value) == nil && value.ErrorCode == codeRecoveryFailed && recoveryCleanup != nil {
			lifecycle.mu.Lock()
			lifecycle.status = "cleanup_required"
			lifecycle.errorCode = codeRecoveryFailed
			lifecycle.errorText = "core recovery failed"
			lifecycle.cleanup = recoveryCleanup
			lifecycle.mu.Unlock()
		}
	}()

	lifecycle.mu.Lock()
	if lifecycle.status == "cleanup_required" {
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeRecoveryFailed, "core recovery failed", value)
	}
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
	service.ResetRuntimeComponentBaseline()
	recoveryReady.Store(false)
	store := newGenerationStore(parsed.DataDir, generationFilesystemOps())
	if err := store.validateRootComponents(); err != nil {
		if runtime.GOOS == "android" {
			log.Printf("mobilecore: Android dataDir parent trust check degraded: %v", err)
		} else {
			logDiagnostic(codeInvalidDataDir, "data-root")
			return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped"})
		}
	}
	if err := store.ensureRecoveryMetadataNamespace(); err != nil {
		logDiagnostic(codeInvalidDataDir, "recovery-namespace")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "recovery-namespace"})
	}
	probePath := filepath.Join(parsed.DataDir, recoveryMetadataDirName, recoveryMetadataOpsDirName)
	if err := probeRecoveryMetadataPlatform(probePath); err != nil {
		if runtime.GOOS == "android" {
			log.Printf("mobilecore: Android recovery metadata probe degraded: %v", err)
		} else {
			logDiagnostic(codeInvalidDataDir, "recovery-platform")
			return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped"})
		}
	}
	if _, err := os.Stat(filepath.Join(parsed.DataDir, activeGenerationName)); errors.Is(err, os.ErrNotExist) {
		flatDB := filepath.Join(parsed.DataDir, "daidai.db")
		if _, statErr := os.Stat(flatDB); statErr == nil {
			if err := checkpointFlatDatabase(flatDB); err != nil {
				logDiagnostic(codeInvalidDataDir, "flat-checkpoint")
				return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "flat-checkpoint"})
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			logDiagnostic(codeInvalidDataDir, "flat-checkpoint")
			return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "flat-checkpoint"})
		}
	} else if err != nil {
		logDiagnostic(codeInvalidDataDir, "active-pointer")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "active-pointer"})
	}
	activeDataDir, err := store.converge()
	if err != nil {
		logDiagnostic(codeInvalidDataDir, "recovery-converge")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "recovery-converge"})
	}
	if err := createDataLayout(activeDataDir); err != nil {
		logDiagnostic(codeInvalidDataDir, "layout")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "layout"})
	}
	activeID := filepath.Base(activeDataDir)
	secret, err := loadOrCreateJWTSecret(activeDataDir, store.isInitialBootstrap(activeID))
	if err != nil {
		logDiagnostic(codeInvalidDataDir, "secret")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "secret"})
	}
	baseline := generationBaseline{Schema: appboot.SchemaFingerprint(), Runtime: currentRuntimeBaseline}
	needsMigration, err := store.needsMigration(activeID, baseline)
	if err != nil {
		logDiagnostic(codeInvalidDataDir, "generation-baseline")
		return failure(codeInvalidDataDir, "dataDir is unavailable", result{Status: "stopped", FailureStage: "generation-baseline"})
	}

	previous := captureGlobals()
	recoveryCleanup = func() error { return restoreAfterStartFailure(previous) }
	cfg := mobileConfig(parsed, activeDataDir, secret)
	bootstrapWriter := newPrivatePathRedactor(log.Writer(), parsed.DataDir, cfg.Data.Dir, cfg.Database.Path, cfg.Data.ScriptsDir, cfg.Data.LogDir)
	var txn recoveryTransaction
	prepared := false
	prepareMigration := func() error {
		if !needsMigration {
			return nil
		}
		if err := checkpointDB(); err != nil {
			return err
		}
		var err error
		txn, err = store.prepareMigration()
		if err != nil {
			return err
		}
		prepared = true
		candidate := store.generationPath(txn.NewGeneration)
		setConfigDataDir(cfg, candidate)
		return openDatabase(&cfg.Database, bootstrapWriter)
	}
	rollbackMigration := func() error {
		err := rollbackCommittedMigration(store, txn, cfg, bootstrapWriter, previous)
		if err != nil {
			recoveryCleanup = func() error { return rollbackCommittedMigration(store, txn, cfg, bootstrapWriter, previous) }
		}
		return err
	}
	if err := initApp(cfg, bootstrapWriter, prepareMigration); err != nil {
		logDiagnostic(codeBootstrapFailed, "appboot")
		if prepared {
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				logDiagnostic(codeRecoveryFailed, "appboot-rollback")
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
		} else {
			if restoreErr := restoreAfterStartFailure(previous); restoreErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "cleanup_required", CleanupRequired: true})
			}
		}
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	if err := integrityDB(); err != nil {
		logDiagnostic(codeBootstrapFailed, "integrity")
		if prepared {
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				logDiagnostic(codeRecoveryFailed, "integrity-rollback")
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
		} else {
			if restoreErr := restoreAfterStartFailure(previous); restoreErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "cleanup_required", CleanupRequired: true})
			}
		}
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	if prepared {
		if err := checkpointDB(); err != nil {
			logDiagnostic(codeBootstrapFailed, "migration-checkpoint")
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		if err := store.sealGeneration(txn.NewGeneration, baseline); err != nil {
			logDiagnostic(codeBootstrapFailed, "generation-seal")
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		if err := store.commitPointer(txn); err != nil {
			logDiagnostic(codeBootstrapFailed, "pointer")
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		activeID = txn.NewGeneration
		if err := openDatabase(&cfg.Database, bootstrapWriter); err != nil {
			logDiagnostic(codeBootstrapFailed, "database-reopen")
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		if err := integrityDB(); err != nil {
			logDiagnostic(codeBootstrapFailed, "reopen-integrity")
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
	}
	if err := service.InitializeRuntimeSecurityWithKeystoreKey(parsed.DataDir, parsed.AndroidKeystoreKey); err != nil {
		logDiagnostic(codeBootstrapFailed, "runtime-security")
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	nativeLibraryDir := strings.TrimSpace(parsed.NativeLibraryDir)
	if nativeLibraryDir == "" {
		nativeLibraryDir = strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_NATIVE_LIB_DIR"))
	}
	applyRuntimeMetadataPathEnv(parsed)
	applyAndroidTerminalEnv(parsed)
	runtimeManager := service.NewRuntimeComponentManager(nativeLibraryDir)
	runtimeBaseline, runtimeErr := runtimeManager.LoadAndValidate()
	if runtimeErr != nil {
		if !service.AllowRuntimeBaselineFailureBypass() {
			logDiagnostic(codeBootstrapFailed, "runtime-baseline")
			if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		log.Printf("mobilecore: runtime baseline bypassed due to dev flag: %v", runtimeErr)
	}
	listener, err := listenTCP("tcp", "127.0.0.1:0")
	if err != nil {
		logDiagnostic(codeListenFailed, "listen")
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		return failure(codeListenFailed, "core listener failed", result{Status: "stopped"})
	}
	port, err := resolveListenerPort(listener.Addr())
	if err != nil {
		_ = listener.Close()
		logDiagnostic(codeListenFailed, "address")
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		return failure(codeListenFailed, "core listener failed", result{Status: "stopped"})
	}
	config.C.Server.Port = port
	actualHost := listener.Addr().String()
	service.ConfigureScriptNotifyBoundary("http://"+actualHost, parsed.LocalToken)
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	if err := configureTrustedProxies(engine, middleware.CurrentTrustedProxyCIDRs()); err != nil {
		_ = listener.Close()
		logDiagnostic(codeBootstrapFailed, "trusted-proxy")
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	engine.Use(gin.Recovery())
	platform := router.NewMobilePlatform(parsed.PlatformCapabilities)
	browser, err := router.NewLoopbackBrowserAccess(actualHost, parsed.WebDir)
	if err != nil {
		_ = listener.Close()
		logDiagnostic(codeBootstrapFailed, "local-web")
		return failure(codeBootstrapFailed, "local web assets unavailable", result{Status: "stopped"})
	}
	if err := configureRoutes(engine, router.ManagementSecurity{LocalToken: parsed.LocalToken, Host: actualHost, Browser: browser}, platform); err != nil {
		_ = listener.Close()
		logDiagnostic(codeBootstrapFailed, "routes")
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	if prepared {
		if err := checkpointDB(); err != nil {
			_ = listener.Close()
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
		if err := store.sealGeneration(activeID, baseline); err != nil {
			_ = listener.Close()
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		if err := openDatabase(&cfg.Database, bootstrapWriter); err != nil {
			_ = listener.Close()
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		if err := integrityDB(); err != nil {
			_ = listener.Close()
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
		}
	}
	lifecycle.mu.Lock()
	lifecycle.nextID++
	runtime := newRuntimeContainer()
	running := &core{
		id:           lifecycle.nextID,
		endpoint:     "http://" + actualHost,
		listener:     listener,
		runtime:      runtime,
		previous:     previous,
		conns:        make(map[net.Conn]struct{}),
		connWait:     make(chan struct{}),
		capabilities: cloneCapabilitySnapshot(parsed.PlatformCapabilities),
		store:        store,
		generationID: activeID,
		baseline:     baseline,
		browser:      browser,
	}
	running.server = &http.Server{Handler: engine, ConnState: running.trackConnection}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- running.server.Serve(running.listener)
	}()
	if err := probeCoreReadiness(running.endpoint, parsed.LocalToken); err != nil {
		_ = running.server.Close()
		if prepared {
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				lifecycle.mu.Unlock()
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
		} else {
			if restoreErr := restoreAfterStartFailure(previous); restoreErr != nil {
				lifecycle.mu.Unlock()
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "cleanup_required", CleanupRequired: true})
			}
		}
		lifecycle.mu.Unlock()
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	recoveryConverged := !prepared
	if prepared {
		if err := markGenerationReady(store, activeID); err != nil {
			_ = running.server.Close()
			if rollbackErr := rollbackMigration(); rollbackErr != nil {
				lifecycle.mu.Unlock()
				return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
			}
			lifecycle.mu.Unlock()
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		recoveryConverged = true
	}
	if !runtimeWorkerStartGate(prepared, recoveryConverged) {
		_ = running.server.Close()
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			lifecycle.mu.Unlock()
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		lifecycle.mu.Unlock()
		return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
	}
	runtimeCtx, runtimeCancel := context.WithTimeout(context.Background(), runtimeLifecycleTimeout)
	runtimeStartErr := running.runtime.Start(runtimeCtx)
	runtimeCancel()
	if runtimeStartErr != nil {
		_ = running.server.Close()
		if rollbackErr := rollbackStartFailure(store, txn, prepared, cfg, bootstrapWriter, previous); rollbackErr != nil {
			lifecycle.mu.Unlock()
			return failure(codeRecoveryFailed, "core recovery failed", result{Status: "stopped"})
		}
		lifecycle.mu.Unlock()
		logDiagnostic(codeBootstrapFailed, "runtime-start")
		return failure(codeBootstrapFailed, "core bootstrap failed", result{Status: "stopped"})
	}
	lifecycle.core = running
	lifecycle.capabilities = cloneCapabilitySnapshot(parsed.PlatformCapabilities)
	lifecycle.status = "running"
	if runtimeBaseline.State == "degraded-ready" {
		lifecycle.status = "degraded-ready"
	}
	lifecycle.errorCode = ""
	lifecycle.errorText = ""
	recoveryReady.Store(true)
	value := lifecycle.statusResultLocked()
	lifecycle.mu.Unlock()

	go observeServe(running, serveResult)
	return encode(value)
}

func applyRuntimeMetadataPathEnv(parsed options) {
	if value := strings.TrimSpace(parsed.RuntimeManifestPath); value != "" {
		_ = os.Setenv("DAIDAI_RUNTIME_MANIFEST_PATH", value)
	}
	if value := strings.TrimSpace(parsed.RuntimeCompatPath); value != "" {
		_ = os.Setenv("DAIDAI_RUNTIME_COMPATIBILITY_PATH", value)
	}
	if value := strings.TrimSpace(parsed.RuntimeSmokePath); value != "" {
		_ = os.Setenv("DAIDAI_RUNTIME_SMOKE_EVIDENCE_PATH", value)
	}
	if value := strings.TrimSpace(parsed.RuntimeDepsPath); value != "" {
		_ = os.Setenv("DAIDAI_RUNTIME_DEPENDENCIES_PATH", value)
	}
}

func applyAndroidTerminalEnv(parsed options) {
	values := map[string]string{
		"DAIDAI_ANDROID_FILES_DIR":      parsed.AndroidFilesDir,
		"DAIDAI_ANDROID_CACHE_DIR":      parsed.AndroidCacheDir,
		"DAIDAI_LINUX_ROOTFS_DIR":       parsed.LinuxRootfsDir,
		"DAIDAI_PROOT_PATH":             parsed.PRootPath,
		"DAIDAI_PROOT_LOADER_PATH":      parsed.PRootLoaderPath,
		"DAIDAI_ANDROID_NATIVE_LIB_DIR": parsed.NativeLibraryDir,
	}
	for key, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			_ = os.Unsetenv(key)
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func StopCore(timeoutMillis int64) string {
	if timeoutMillis <= 0 || timeoutMillis > int64(time.Duration(1<<63-1)/time.Millisecond) {
		return failure(codeInvalidTimeout, "timeoutMillis must be greater than 0", statusSnapshot())
	}

	lifecycle.operation.Lock()
	defer lifecycle.operation.Unlock()
	router.CloseMobileTerminalSessions()

	lifecycle.mu.Lock()
	if lifecycle.core == nil {
		if lifecycle.status == "cleanup_required" && lifecycle.cleanup != nil {
			cleanup := lifecycle.cleanup
			lifecycle.mu.Unlock()
			if err := cleanup(); err != nil {
				return recoveryFailure(cleanup)
			}
			lifecycle.mu.Lock()
			lifecycle.status = "stopped"
			lifecycle.errorCode = ""
			lifecycle.errorText = ""
			lifecycle.cleanup = nil
			value := lifecycle.statusResultLocked()
			lifecycle.mu.Unlock()
			return encode(value)
		}
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeNotRunning, "core is not running", value)
	}
	running := lifecycle.core
	if running.browser != nil {
		running.browser.Clear()
	}
	lifecycle.status = "stopping"
	recoveryReady.Store(false)
	lifecycle.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMillis)*time.Millisecond)
	if running.runtime != nil {
		runtimeErr := running.runtime.Stop(ctx)
		if runtimeErr != nil {
			logDiagnostic(codeShutdownFailed, "runtime-stop")
			cancel()
			lifecycle.mu.Lock()
			if lifecycle.core == running {
				lifecycle.status = "failed"
				lifecycle.errorCode = codeShutdownFailed
				lifecycle.errorText = "core shutdown failed"
			}
			value := lifecycle.statusResultLocked()
			lifecycle.mu.Unlock()
			return failure(codeShutdownFailed, "core shutdown failed", value)
		}
	}
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
	if err := checkpointDB(); err != nil {
		logDiagnostic(codeDatabaseClose, "close")
		lifecycle.mu.Lock()
		lifecycle.status = "cleanup_required"
		lifecycle.errorCode = codeDatabaseClose
		lifecycle.errorText = "core database close failed"
		value := lifecycle.statusResultLocked()
		lifecycle.mu.Unlock()
		return failure(codeDatabaseClose, "core database close failed", value)
	}
	if err := running.store.sealGeneration(running.generationID, running.baseline); err != nil {
		logDiagnostic(codeDatabaseClose, "generation-seal")
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
	if lifecycle.core == nil || !coreReadyStatus(lifecycle.status) {
		return ""
	}
	return lifecycle.core.endpoint
}

func CreateBrowserURL() string {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.core == nil || !coreReadyStatus(lifecycle.status) || lifecycle.core.browser == nil {
		return ""
	}
	url, err := lifecycle.core.browser.CreateURL()
	if err != nil {
		return ""
	}
	return url
}

// RecoveryConverged gates business workers until recovery and migration commit.
func RecoveryConverged() bool {
	return recoveryReady.Load()
}

func observeServe(running *core, serveResult <-chan error) {
	err := <-serveResult
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
	recoveryReady.Store(false)
	if running.runtime != nil {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeLifecycleTimeout)
		if err := running.runtime.Stop(ctx); err != nil {
			logDiagnostic(codeShutdownFailed, "runtime-stop-observe")
		}
		cancel()
	}
	lifecycle.mu.Unlock()
}

func probeHealthEndpoint(endpoint, token string) error {
	request, err := http.NewRequest(http.MethodGet, endpoint+"/api/health", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Daidai-Local-Token", token)
	request.Header.Set("Origin", endpoint)
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness probe status %d", response.StatusCode)
	}
	return nil
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
		if state.State != router.CapabilityDisabled && state.State != router.CapabilityEnabled && state.State != router.CapabilityUnsupported {
			return parsed, codeInvalidOptions, "platform capability state is invalid", "capability-state"
		}
	}
	parsed.SchedulerGuarantee = normalizeMobileSchedulerGuarantee(parsed.SchedulerGuarantee)
	return parsed, "", "", ""
}

func mobileConfig(parsed options, dataDir, secret string) *config.Config {
	return &config.Config{
		Server:   config.ServerConfig{Mode: gin.ReleaseMode},
		Database: config.DatabaseConfig{Path: filepath.Join(dataDir, "daidai.db")},
		JWT: config.JWTConfig{
			Secret:             secret,
			AccessTokenExpire:  480 * time.Hour,
			RefreshTokenExpire: 1440 * time.Hour,
		},
		Data: config.DataConfig{
			Dir:        dataDir,
			ScriptsDir: filepath.Join(dataDir, "scripts"),
			LogDir:     filepath.Join(dataDir, "logs"),
		},
	}
}

func setConfigDataDir(cfg *config.Config, dataDir string) {
	cfg.Database.Path = filepath.Join(dataDir, "daidai.db")
	cfg.Data.Dir = dataDir
	cfg.Data.ScriptsDir = filepath.Join(dataDir, "scripts")
	cfg.Data.LogDir = filepath.Join(dataDir, "logs")
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

func restoreAfterStartFailure(previous globalState) error {
	if database.DB != nil && database.DB != previous.database {
		if err := closeDatabase(); err != nil {
			return fmt.Errorf("close failed startup database: %w", err)
		}
	}
	restoreGlobals(previous)
	return nil
}

func rollbackStartFailure(store *generationStore, txn recoveryTransaction, prepared bool, cfg *config.Config, writer io.Writer, previous globalState) error {
	if prepared {
		return rollbackCommittedMigration(store, txn, cfg, writer, previous)
	}
	return restoreAfterStartFailure(previous)
}

func rollbackCommittedMigration(store *generationStore, txn recoveryTransaction, cfg *config.Config, writer io.Writer, previous globalState) error {
	if database.DB != nil && database.DB != previous.database {
		if err := closeDatabase(); err != nil {
			return fmt.Errorf("close candidate database: %w", err)
		}
	}
	if err := store.rollback(txn); err != nil {
		restoreGlobals(previous)
		return err
	}
	oldDataDir := store.generationPath(txn.OldGeneration)
	setConfigDataDir(cfg, oldDataDir)
	if err := openDatabase(&cfg.Database, writer); err != nil {
		restoreGlobals(previous)
		return err
	}
	config.C = cfg
	return nil
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
		Running:            state.core != nil && coreReadyStatus(status),
		Status:             status,
		ErrorCode:          state.errorCode,
		Error:              state.errorText,
		CleanupRequired:    status == "cleanup_required" || state.core != nil && !coreReadyStatus(status),
		ProcessRequirement: `android:process=":panel"`,
		SchedulerGuarantee: service.CurrentSchedulerGuarantee(),
		RuntimeBaseline:    service.RuntimeComponentBaselineSnapshot(),
	}
	if state.core != nil {
		value.PlatformCapabilities = cloneCapabilitySnapshot(state.core.capabilities)
		value.ID = state.core.id
		if coreReadyStatus(status) {
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

func coreReadyStatus(status string) bool {
	return status == "ready" || status == "degraded-ready" || status == "running"
}

func normalizeMobileSchedulerGuarantee(snapshot service.SchedulerGuaranteeSnapshot) service.SchedulerGuaranteeSnapshot {
	service.ConfigureSchedulerGuarantee(snapshot)
	return service.CurrentSchedulerGuarantee()
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
	if code == codeRecoveryFailed {
		value.Status = "cleanup_required"
		value.CleanupRequired = true
	}
	return encode(value)
}

func recoveryFailure(cleanup func() error) string {
	lifecycle.mu.Lock()
	lifecycle.status = "cleanup_required"
	lifecycle.errorCode = codeRecoveryFailed
	lifecycle.errorText = "core recovery failed"
	lifecycle.cleanup = cleanup
	value := lifecycle.statusResultLocked()
	lifecycle.mu.Unlock()
	return failure(codeRecoveryFailed, "core recovery failed", value)
}

func createDataLayout(dataDir string) error {
	for _, dir := range []string{dataDir, filepath.Join(dataDir, "scripts"), filepath.Join(dataDir, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func loadOrCreateJWTSecret(dataDir string, allowCreate bool) (string, error) {
	path := filepath.Join(dataDir, ".jwt_secret")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return string(data), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if !allowCreate {
		return "", errors.New("JWT secret is missing from verified generation")
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
