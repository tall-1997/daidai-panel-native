package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"daidai-panel/appboot"
	"daidai-panel/config"
	"daidai-panel/handler"
	"daidai-panel/middleware"
	"daidai-panel/router"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

func buildAccessURLs(port int) []string {
	if port <= 0 {
		return nil
	}

	seen := map[string]struct{}{}
	var urls []string

	addURL := func(host string) {
		host = strings.TrimSpace(host)
		if host == "" {
			return
		}
		url := fmt.Sprintf("http://%s:%d", host, port)
		if _, exists := seen[url]; exists {
			return
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}

	addURL("127.0.0.1")
	addURL("localhost")

	ifaces, err := net.Interfaces()
	if err != nil {
		return urls
	}

	var localIPs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}

			localIPs = append(localIPs, ip.String())
		}
	}

	sort.Strings(localIPs)
	for _, ip := range localIPs {
		addURL(ip)
	}

	return urls
}

func printStartupSummary(port int) {
	urls := buildAccessURLs(port)
	fmt.Println("呆呆面板已经启动")
	if len(urls) == 0 {
		fmt.Printf("访问地址：http://127.0.0.1:%d\n", port)
		return
	}

	fmt.Println("访问地址：")
	for _, url := range urls {
		fmt.Println(url)
	}
	fmt.Printf("请使用上面显示的宿主机访问地址，不要直接使用容器内端口 %d/%d。\n", 5700, 5701)
}

func setupPanelLog(dataDir string) io.Writer {
	logFilePath := filepath.Join(dataDir, "panel.log")
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0o755); err != nil {
		return os.Stdout
	}

	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.Stdout
	}

	switch service.ResolvePanelRuntimeMode() {
	case service.PanelRuntimeModeStdout:
		return io.MultiWriter(os.Stdout, logFile)
	default:
		return logFile
	}
}

func writeServerPIDFile(dataDir string) func() {
	if strings.TrimSpace(dataDir) == "" {
		return func() {}
	}

	pidDir := filepath.Join(dataDir, "run")
	if err := os.MkdirAll(pidDir, 0o755); err != nil {
		log.Printf("write pid dir failed: %v", err)
		return func() {}
	}

	pidFile := filepath.Join(pidDir, "daidai-server.pid")
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o644); err != nil {
		log.Printf("write pid file failed: %v", err)
		return func() {}
	}

	return func() {
		_ = os.Remove(pidFile)
	}
}

func main() {
	// 用 ResolveConfigPath 而不是硬编码 "config.yaml"：
	// 二进制部署若 cwd ≠ exe 目录（Windows 双击、用户 cd 到其他目录后绝对路径启动、
	// systemd WorkingDirectory 漏配等场景），硬编码相对路径会找不到 config 直接 fatal。
	cfg, err := config.Load(appboot.ResolveConfigPath())
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	panelWriter := setupPanelLog(cfg.Data.Dir)
	log.SetOutput(service.NewPanelLogFilterWriter(panelWriter))
	gin.DefaultWriter = service.NewPanelLogFilterWriter(panelWriter)
	gin.DefaultErrorWriter = service.NewPanelLogFilterWriter(panelWriter)
	cleanupPIDFile := writeServerPIDFile(cfg.Data.Dir)
	defer cleanupPIDFile()

	if err := appboot.InitWithConfig(cfg); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}

	verifyInstalledDeps()
	handler.FinalizePendingAutoUpdateOnStartup()
	if err := service.EnsureBuiltinNotifyHelpers(cfg.Data.ScriptsDir); err != nil {
		log.Printf("prepare builtin notify helpers failed: %v", err)
	}
	if err := service.CleanupManagedHelperCopiesUnderRoot(cfg.Data.ScriptsDir); err != nil {
		log.Printf("cleanup duplicated notify helpers failed: %v", err)
	}
	// 启动时先隔离脚本目录中的异常污染目录，避免继续影响脚本管理、备份和统计链路。
	service.QuarantineUnexpectedScriptEntriesOnStartup()
	service.CleanupManagedPythonArtifactsOnStartup()

	service.InitSchedulerV2()
	defer service.ShutdownSchedulerV2()

	service.InitSubscriptionScheduler()
	defer service.ShutdownSubscriptionScheduler()

	service.InitBackupScheduler()
	defer service.ShutdownBackupScheduler()

	service.StartResourceWatcher()
	defer service.StopResourceWatcher()

	service.StartLogCleanupWorker()
	defer service.StopLogCleanupWorker()

	handler.StartPanelAutoUpdateWatcher()
	defer handler.StopPanelAutoUpdateWatcher()

	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	if err := engine.SetTrustedProxies(middleware.CurrentTrustedProxyCIDRs()); err != nil {
		log.Fatalf("failed to apply trusted proxies to gin engine: %v", err)
	}
	engine.RemoteIPHeaders = []string{"X-Real-IP", "X-Forwarded-For"}
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output:    service.NewGINLoggerWriter(service.NewPanelLogFilterWriter(panelWriter)),
		SkipPaths: []string{"/api/v1/health", "/api/health"},
	}))
	engine.Use(gin.Recovery())

	router.Setup(engine)
	setupStaticFrontend(engine, cfg.Server.WebDir)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("server failed: %v", err)
	}

	log.SetOutput(service.NewPanelLogFilterWriter(panelWriter))
	printStartupSummary(cfg.Server.Port)

	server := &http.Server{Handler: engine}
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	case sig := <-shutdownSignals:
		log.Printf("received %s, shutting down panel", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("server graceful shutdown failed: %v", err)
			_ = server.Close()
		}
	}
}

func verifyInstalledDeps() {
	service.ReconcileDependenciesAfterRestart()
}

// setupStaticFrontend lets the Go backend double as a frontend host when a
// web directory is configured (e.g. the Magisk module bundles `web/` next to
// the binary and has no nginx). Docker deployments leave WebDir empty and
// keep using nginx.
func setupStaticFrontend(engine *gin.Engine, webDir string) {
	if strings.TrimSpace(webDir) == "" {
		webDir = autoDetectWebDir()
		if webDir == "" {
			return
		}
	}

	absDir, err := filepath.Abs(webDir)
	if err != nil {
		log.Printf("web_dir 解析失败: %v", err)
		return
	}

	indexPath := filepath.Join(absDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		log.Printf("web_dir=%s 缺少 index.html，跳过前端托管", absDir)
		return
	}

	engine.StaticFile("/", indexPath)
	engine.StaticFile("/favicon.svg", filepath.Join(absDir, "favicon.svg"))

	for _, sub := range []string{"assets", "monaco", "sponsor-portal"} {
		subDir := filepath.Join(absDir, sub)
		if info, err := os.Stat(subDir); err == nil && info.IsDir() {
			engine.Static("/"+sub, subDir)
		}
	}

	// SPA fallback: 非 API 的路径在后端没有命中时一律回 index.html，
	// 交给前端 vue-router 处理。
	engine.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if strings.HasPrefix(p, "/api/") {
			c.JSON(404, gin.H{"error": "route not found"})
			return
		}
		c.File(indexPath)
	})

	log.Printf("前端静态目录已挂载: %s", absDir)
}

func autoDetectWebDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	exeDir := filepath.Dir(exePath)

	candidates := []string{
		filepath.Join(exeDir, "web"),
		filepath.Join(exeDir, "dist"),
		filepath.Join(".", "web"),
		filepath.Join(".", "dist"),
	}

	for _, dir := range candidates {
		index := filepath.Join(dir, "index.html")
		if _, err := os.Stat(index); err == nil {
			log.Printf("自动检测到前端目录: %s", dir)
			return dir
		}
	}
	return ""
}
