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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"daidai-panel/appboot"
	"daidai-panel/config"
	"daidai-panel/database"
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

func printStartupSummary(host string, port int) {
	fmt.Println("呆呆面板已经启动")
	// 指定了具体监听地址（#162）时只有这一个地址能访问，别再列全部网卡
	if host != "" {
		fmt.Printf("访问地址：http://%s\n", net.JoinHostPort(host, strconv.Itoa(port)))
		return
	}
	urls := buildAccessURLs(port)
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

	// 下面几处 stepStarted / logSlowStartupStep 只是给慢启动（#156）留时间线：某一步超过 3 秒才打一行。
	// 被计时的函数必须保持直接调用（startup_wiring_test.go 按 AST 核对 main 里的调用顺序），不要包成传函数值的写法。
	stepStarted := time.Now()
	if err := appboot.InitWithConfig(cfg); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
	logSlowStartupStep("初始化配置与数据库", stepStarted)

	// 容器部署下把 PLAYWRIGHT_BROWSERS_PATH 钉到数据卷（#142），系统命令行、依赖安装、自动装依赖
	// 这些直接继承 os.Environ 的子进程才看得到。必须在 config 与数据库就绪之后（要读 data.dir、
	// 要查环境变量页决定是否搬迁 PUID 存量浏览器），并赶在启动校验排队重装依赖之前。
	service.ApplyPlaywrightBrowsersPathProcessEnv()
	stepStarted = time.Now()
	verifyInstalledDeps()
	logSlowStartupStep("启动校验已安装依赖", stepStarted)
	// Node 换了大版本（刷新版 Magisk 模块、换 Docker 镜像）后 deps/nodejs 里原生扩展的 ABI 会对不上。
	// 排在启动校验之后：它排队的 Node 依赖重装与这里的 npm rebuild 共用同一把包操作锁，后台串行、不阻塞启动。
	service.RebuildNodeDependenciesIfABIChanged()
	// Docker 在 Alpine（musl）与 Debian（glibc）镜像之间切换后，数据卷里 venv 的原生扩展链接的是另一种 C 库，
	// 「已安装」却加载不了，重装按钮也修不好（#150）。同样排在启动校验之后，后台按原版本定点重装，不阻塞启动。
	service.RepairPythonPackagesForLibcChange()
	handler.FinalizePendingAutoUpdateOnStartup()
	// 根目录那两份通知辅助脚本（notify.py / sendNotify.js）只读写两个文件，照旧同步准备好，开机任务一跑就能用。
	if err := service.EnsureBuiltinNotifyHelpers(cfg.Data.ScriptsDir); err != nil {
		log.Printf("prepare builtin notify helpers failed: %v", err)
	}
	// 两遍整棵脚本目录的遍历放到后台（#158）：隔离顶层污染目录并清悬空的 node_modules 软链、清子目录里旧版面板留下的通知脚本副本。
	// 慢盘上每遍几秒，以前挡在监听前面。只挪一遍没用：时间几乎全花在第一次读目录上，另一遍会变成冷缓存上的第一遍，照样几秒。
	// 两遍放在同一个协程里、软链那遍排前面：悬空的面板软链会挡住那个目录里 Node 任务的 ESM import，越早清越好；
	// 通知脚本副本每个任务运行前还会清自己的目录（cleanupManagedHelperCopies），这里只是兜底。
	// 代价：悬空软链不再保证早于开机任务清掉，改为启动后几秒内清掉。
	// 只删文件 / 软链、搬目录，不碰数据库；关停不等它，进程退出时停在哪一步都不留半成品。
	go func() {
		service.QuarantineUnexpectedScriptEntriesOnStartup()
		if err := service.CleanupManagedHelperCopiesUnderRoot(cfg.Data.ScriptsDir); err != nil {
			log.Printf("cleanup duplicated notify helpers failed: %v", err)
		}
	}()
	// 每次启动都重建 /ql 兼容层：Magisk 重刷 zip、容器重建都会让它整个消失，
	// 带「只跑一次」的标记反而会让重建后永远修不回来。全程 best-effort，不会阻塞启动。
	service.EnsureQingLongCompatLayout()
	service.CleanupManagedPythonArtifactsOnStartup()
	// 上次被 SIGKILL / os.Exit / 总兜底强退时没来得及删的任务临时文件（装着全部环境变量与脚本凭据）。
	// 必须排在调度器启动之前：这时本进程还没有任何任务在跑，命中前缀的只可能是上次留下的。
	service.CleanupLeakedTaskTempEntriesOnStartup()

	// 面板自己发起的退出（「重启面板」、Magisk「停止面板服务」、二进制 / Magisk 在线升级）改为请求 main
	// 走下面 shutdownPanel 的完整关停后再按原退出码退出，不再在 handler 里直接 os.Exit：
	// 否则运行中的任务、软链、数据库都和被 SIGKILL 一样没人收尾，不输出的任务还会变成孤儿继续跑。
	// 必须在任何可能触发退出的协程起来之前接上（自动更新检查、HTTP 服务都在后面），之后再接就有数据竞争。
	panelExitRequests := make(chan int, 1)
	handler.SetPanelExitRequester(func(code int) {
		select {
		case panelExitRequests <- code:
		default:
			// 已经有一个退出请求在排队（例如连点两次重启），以第一个为准。
		}
	})

	// 停止信号在调度器起来之前接管：从这里起可能已经有开机任务在跑，这之后收到的 SIGTERM / SIGINT
	// 都要走 shutdownPanel 收尾（信号先存在通道里，HTTP 服务起来后立刻处理）。
	// 更早的启动步骤（启动校验可能很慢，#156）里收到信号仍按默认方式立即退出：那时还没有任务在跑，没什么要收尾的。
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	// 这些后台组件的收尾不再用 defer：关停顺序与每一步的时间上限由 shutdownPanel 一处决定。
	stepStarted = time.Now()
	service.InitSchedulerV2()
	logSlowStartupStep("初始化任务调度器", stepStarted)
	service.InitSubscriptionScheduler()
	service.InitBackupScheduler()
	service.StartResourceWatcher()
	service.StartLogCleanupWorker()
	handler.StartPanelAutoUpdateWatcher()

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

	listener, err := net.Listen("tcp", cfg.Server.ListenAddr())
	if err != nil {
		log.Fatalf("server failed: %v", err)
	}

	log.SetOutput(service.NewPanelLogFilterWriter(panelWriter))
	printStartupSummary(cfg.Server.SpecificHost(), cfg.Server.Port)

	// 所有请求的 ctx 都派生自 requestsCtx，关停一开始（Shutdown 关掉监听后）就取消它：
	// 实时日志、依赖安装输出、订阅拉取输出这些 SSE 长连接都在 select 请求 ctx，会立刻发结束事件收流；
	// 不取消的话 Shutdown 要一直等到超时（原来开着实时日志停容器要等满 15 秒，Docker 只给 10 秒）。
	requestsCtx, cancelRequests := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:     engine,
		BaseContext: func(net.Listener) context.Context { return requestsCtx },
	}
	server.RegisterOnShutdown(cancelRequests)
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	exitCode := 0
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			// 监听意外断了：照样走一遍关停把任务结算掉，再按失败退出（原来是 log.Fatalf，什么都不收尾）。
			log.Printf("server failed: %v", err)
			exitCode = 1
		}
	case sig := <-shutdownSignals:
		log.Printf("received %s, shutting down panel", sig)
	case exitCode = <-panelExitRequests:
		log.Printf("panel exit requested (code=%d), shutting down panel", exitCode)
	}
	shutdownPanel(server)
	cleanupPIDFile()
	if exitCode != 0 {
		// 重启面板 = 1：Docker 入口脚本的重启循环、systemd 的 Restart=on-failure、Magisk 存活守护都按原来的方式拉起。
		os.Exit(exitCode)
	}
}

// 关停时间预算。宽限最短的是 Docker：docker stop / compose down / watchtower 默认只等 10 秒，到点直接 SIGKILL
// （Windows 关控制台窗口约 5 秒、Magisk 动作按钮 2 秒，正常路径几十毫秒就能走完；Magisk 下关停终止任务是立即 KILL，见下）。
// 每一步都有上限，再加一个总兜底，保证 8 秒内退出，给 entrypoint 与 Docker 留出余量。
// 终止任务时先 SIGTERM、所有进程组共用最多 2 秒的宽限再 SIGKILL（service.HaltSchedulerV2，#159）；
// Magisk 部署例外：关停时不给 TERM 宽限，所有进程组立即整组 SIGKILL（与 v3.3.5 一致）。动作按钮「停止」是
// kill -TERM 面板、sleep 2、还在就 kill -KILL（Magisk/action.sh），SignalStop 的 1 秒加 2 秒宽限装不下，面板先被 KILL 的话
// 忽略 TERM 的任务会成孤儿；action.sh 是模块外壳，在线升级改不到，只能面板这边让步。
// 任务结算的等待上限（3 秒）在 service.ShutdownSchedulerV2 里，与 HTTP 关停同时进行。
// 总账：max(1+2+3, 6.5, 5) + 1 = 7.5 秒。
const (
	shutdownHardLimit = 8 * time.Second // 总兜底：任何一步卡住（例如唯一的数据库连接被恢复备份的长事务占着）都强制退出
	shutdownHTTPWait  = 5 * time.Second // HTTP：SSE 已被取消，剩下的只有上传下载、恢复备份这类长请求，等不完就 Close
	// 备份调度、订阅调度共用的截止时间（从关停开始算）：正在跑的定时备份 / 定时拉取最多等到这一刻，不取消它们。
	// 不各写死 1 秒：走到这一步时任务已经结算完，多等一会儿不影响别的，能让本来来得及跑完的拉取跑完
	// （订阅的 git 进程在 stash push 与 pop 之间被杀，会把用户改动留在 stash 里）。
	shutdownSchedulersDeadline = 6500 * time.Millisecond
	shutdownDBWait             = time.Second // 关库：SQLite 关最后一个连接时会自动 checkpoint 并删掉 -wal / -shm
)

// shutdownPanel 是面板唯一的关停流程：收到 SIGTERM / SIGINT，或面板自己请求退出（重启 / 停止 / 在线升级）都走这里。
// 顺序（时间预算见上面的常量）：
//  1. 停调度、终止任务（HaltSchedulerV2，先 TERM、共用 ≤2 秒宽限再 KILL）与 2. HTTP 关停同时开始，互不依赖：第 1 步卡住时 SSE 照样断开；
//  3. 等任务结算（ShutdownSchedulerV2，≤3 秒），没结算完的标成中断；
//  4. 自动更新 / 日志清理 / 资源监控关通道，备份、订阅两个调度等到共用的截止时间；
//  5. 等 HTTP 关停结束；
//  6. 最后关库：之后任何读写都会报 database is closed，所以必须排在所有收尾之后。
func shutdownPanel(server *http.Server) {
	started := time.Now()
	hardStop := time.AfterFunc(shutdownHardLimit, func() {
		log.Printf("面板关停超过 %s 仍未完成，强制退出", shutdownHardLimit)
		os.Exit(1)
	})
	defer hardStop.Stop()

	// 2. HTTP 关停先起协程：Shutdown 关掉监听后调用 RegisterOnShutdown 注册的 cancelRequests，所有 SSE 立刻收流。
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownHTTPWait)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("server graceful shutdown failed: %v", err)
			_ = server.Close()
		}
	}()

	// 1. 不再触发、不再接新任务，并按进程组终止运行中的任务（runStopHalt）。
	service.HaltSchedulerV2()

	// 3. 等任务结算；超时还没结算完的，统一标成「面板正在关闭或重启，任务已被中断」。
	service.ShutdownSchedulerV2()

	// 4. 其它后台组件。两个 cron 同时停（不再触发新作业），正在跑的作业共用同一个截止时间等，不取消它们；
	//    串行调用的话，后一个 cron 要等前一个等完才停，这段时间里它还可能再触发一次新作业。
	handler.StopPanelAutoUpdateWatcher()
	service.StopLogCleanupWorker()
	service.StopResourceWatcher()
	schedulersDeadline := started.Add(shutdownSchedulersDeadline)
	var schedulersStopped sync.WaitGroup
	schedulersStopped.Add(2)
	go func() {
		defer schedulersStopped.Done()
		service.ShutdownBackupScheduler(schedulersDeadline)
	}()
	go func() {
		defer schedulersStopped.Done()
		service.ShutdownSubscriptionScheduler(schedulersDeadline)
	}()
	schedulersStopped.Wait()

	// 5. 等 HTTP 关停结束（SSE 早已断开，这里通常是立刻返回）。
	<-httpDone

	// 6. 关库。超时就不管了：SQLite 本身崩溃安全，没提交的事务下次打开时自动回滚。
	if err := database.Close(shutdownDBWait); err != nil {
		log.Printf("close database failed: %v", err)
	}
	log.Printf("panel shutdown finished in %s", time.Since(started).Round(time.Millisecond))
}

// startupStepSlowThreshold 是启动步骤打耗时日志的门槛：正常启动一行都不多打，慢的时候（#156）能对上是哪一步。
const startupStepSlowThreshold = 3 * time.Second

func logSlowStartupStep(step string, started time.Time) {
	if elapsed := time.Since(started); elapsed >= startupStepSlowThreshold {
		log.Printf("[启动耗时] %s 用时 %s", step, elapsed.Round(10*time.Millisecond))
	}
}

func verifyInstalledDeps() {
	service.ReconcileDependenciesAfterRestart()
}

// setupStaticFrontend lets the Go backend double as a frontend host when a
// web directory is configured (e.g. the Magisk module bundles `web/` next to
// the binary and has no nginx). Docker deployments leave WebDir empty and
// keep using nginx.
//
// 二进制 / Windows / Magisk 三种内嵌部署都走这里。路由、缓存头、缺失资源 404、/assets 的 gzip
// 都在 static_frontend.go（Docker 的对应规则在 docker/nginx.conf）。
// 返回值只给测试用；没有挂载前端时返回 nil。
func setupStaticFrontend(engine *gin.Engine, webDir string) *staticFrontend {
	if strings.TrimSpace(webDir) == "" {
		webDir = autoDetectWebDir()
		if webDir == "" {
			return nil
		}
	}

	absDir, err := filepath.Abs(webDir)
	if err != nil {
		log.Printf("web_dir 解析失败: %v", err)
		return nil
	}

	indexPath := filepath.Join(absDir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		log.Printf("web_dir=%s 缺少 index.html，跳过前端托管", absDir)
		return nil
	}

	sf := newStaticFrontend(absDir)
	sf.register(engine)

	log.Printf("前端静态目录已挂载: %s", absDir)
	return sf
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
