package service

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
)

var panelStartTime = time.Now()

type ResourceInfo struct {
	Hostname    string  `json:"hostname"`
	MachineCode string  `json:"machine_code"`
	CPUUsage    float64 `json:"cpu_usage"`
	MemoryTotal uint64  `json:"memory_total"`
	MemoryUsed  uint64  `json:"memory_used"`
	MemoryFree  uint64  `json:"memory_free"`
	MemoryUsage float64 `json:"memory_usage"`
	DiskTotal   uint64  `json:"disk_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskFree    uint64  `json:"disk_free"`
	DiskUsage   float64 `json:"disk_usage"`
	Uptime      string  `json:"uptime"`
	GoRoutines  int     `json:"goroutines"`
	GoVersion   string  `json:"go_version"`
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	NumCPU      int     `json:"num_cpu"`
	DataDir     string  `json:"data_dir"`
	NetRxBytes  uint64  `json:"net_rx_bytes"`
	NetTxBytes  uint64  `json:"net_tx_bytes"`
	NetRxSpeed  float64 `json:"net_rx_speed"`
	NetTxSpeed  float64 `json:"net_tx_speed"`
}

func GetResourceInfo() ResourceInfo {
	info := ResourceInfo{
		Hostname:    "-",
		MachineCode: EnsureMachineCode(),
		GoRoutines:  runtime.NumGoroutine(),
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		NumCPU:      runtime.NumCPU(),
		Uptime:      getPanelUptime(),
	}

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		info.Hostname = hostname
	}

	if config.C != nil {
		absDir, err := filepath.Abs(config.C.Data.Dir)
		if err == nil {
			info.DataDir = absDir
		} else {
			info.DataDir = config.C.Data.Dir
		}
	}

	if runtime.GOOS == "linux" {
		info.MemoryTotal, info.MemoryUsed, info.MemoryFree = getLinuxMemory()
		if info.MemoryTotal > 0 {
			info.MemoryUsage = math.Round(float64(info.MemoryUsed)/float64(info.MemoryTotal)*10000) / 100
		}

		info.DiskTotal, info.DiskUsed, info.DiskFree = getLinuxDisk()
		if info.DiskTotal > 0 {
			info.DiskUsage = math.Round(float64(info.DiskUsed)/float64(info.DiskTotal)*10000) / 100
		}

		sample := defaultLinuxResourceSampler.current()
		info.CPUUsage = sample.cpuUsage
		info.NetRxBytes, info.NetTxBytes = sample.netRx, sample.netTx
		info.NetRxSpeed, info.NetTxSpeed = sample.rxSpeed, sample.txSpeed
	}
	if runtime.GOOS == "windows" {
		fillWindowsResourceInfo(&info)
	}

	return info
}

func CountScriptFiles(scriptsDir string) int64 {
	var count int64
	filepath.Walk(scriptsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			// 逐段遍历版本：.git/objects 里成千上万个文件不该算进“脚本文件数”，
			// 顺带修掉子目录里的 node_modules 也被计入的既有缺陷。
			if ShouldHideScriptTreePath(scriptsDir, path) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldHideScriptTreePath(scriptsDir, path) {
			return nil
		}
		count++
		return nil
	})
	return count
}

func getPanelUptime() string {
	dur := time.Since(panelStartTime)
	days := int(dur.Hours() / 24)
	hours := int(dur.Hours()) % 24
	mins := int(dur.Minutes()) % 60

	if days > 0 {
		return strconv.Itoa(days) + "天" + strconv.Itoa(hours) + "时" + strconv.Itoa(mins) + "分"
	}
	if hours > 0 {
		return strconv.Itoa(hours) + "时" + strconv.Itoa(mins) + "分"
	}
	return strconv.Itoa(mins) + "分"
}

func getLinuxMemory() (total, used, free uint64) {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	return parseProcMeminfo(content)
}

func parseProcMeminfo(content []byte) (total, used, free uint64) {
	values := make(map[string]uint64)
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) == 0 {
			continue
		}

		value, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}

		// /proc/meminfo values are reported in KiB.
		values[strings.TrimSpace(parts[0])] = value * 1024
	}

	total = values["MemTotal"]
	if total == 0 {
		return 0, 0, 0
	}

	available := values["MemAvailable"]
	if available == 0 {
		available = values["MemFree"] + values["Buffers"] + values["Cached"] + values["SReclaimable"]
		if shmem := values["Shmem"]; available > shmem {
			available -= shmem
		}
	}
	if available > total {
		available = total
	}

	free = available
	used = total - available
	return total, used, free
}

func getLinuxDisk() (total, used, free uint64) {
	out, err := exec.Command("df", "-B1", "/").Output()
	if err != nil {
		return
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return
	}
	total, _ = strconv.ParseUint(fields[1], 10, 64)
	used, _ = strconv.ParseUint(fields[2], 10, 64)
	free, _ = strconv.ParseUint(fields[3], 10, 64)
	return
}

// procStatCPU 是 /proc/stat 汇总行「cpu 」的一份累计计数快照（单位 jiffies），
// 只留算使用率要用的两个量，免得调用方各自去记字段顺序。
type procStatCPU struct {
	// total 只加 user..steal 前 8 项：guest / guest_nice 内核已经计入 user / nice，
	// 再加一遍会把跑虚拟机的机器的分母和分子一起虚高。
	total uint64
	// idle 含 iowait：CPU 在等盘时其实是空着的，top / htop 和常见面板都不算它忙碌。
	idle uint64
}

// parseProcStatCPU 从 /proc/stat 内容中解析汇总行；找不到或格式不对时返回 false。
func parseProcStatCPU(content []byte) (procStatCPU, bool) {
	for _, line := range strings.Split(string(content), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		// 字段依次是 user nice system idle iowait irq softirq steal guest guest_nice，
		// 老内核只有前 4～7 项，缺的按 0 算。
		fields := strings.Fields(line)[1:]
		if len(fields) < 4 {
			return procStatCPU{}, false
		}
		var values [8]uint64
		for i := 0; i < len(fields) && i < len(values); i++ {
			v, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return procStatCPU{}, false
			}
			values[i] = v
		}
		var snap procStatCPU
		for _, v := range values {
			snap.total += v
		}
		snap.idle = values[3] + values[4]
		return snap, true
	}
	return procStatCPU{}, false
}

// cpuUsagePercent 由前后两份快照算这段时间的平均使用率（百分比，保留两位小数）。
// 口径：忙碌 = 总计 − idle − iowait。
func cpuUsagePercent(prev, cur procStatCPU) float64 {
	// 总计没涨（间隔太短）或倒退（计数回绕、传反了快照）时这一轮没法算，
	// 报 0 而不是让无符号减法下溢成一个离谱的大数。
	if cur.total <= prev.total {
		return 0
	}
	totalDelta := cur.total - prev.total
	prevBusy := prev.total - prev.idle
	curBusy := cur.total - cur.idle
	// 部分内核的 iowait 会回退，忙碌量可能不增反减，同样按 0 处理。
	if curBusy <= prevBusy {
		return 0
	}
	busyDelta := curBusy - prevBusy
	if busyDelta > totalDelta {
		busyDelta = totalDelta
	}
	return math.Round(float64(busyDelta)/float64(totalDelta)*10000) / 100
}

// netBytesPerSecond 按两次采样实际相隔的时间算每秒字节数；
// 计数倒退（网卡重建、计数回绕）时报 0。
func netBytesPerSecond(prev, cur uint64, elapsed time.Duration) float64 {
	if cur < prev || elapsed <= 0 {
		return 0
	}
	return math.Round(float64(cur-prev) / elapsed.Seconds())
}

// linuxResourceSnapshot 是一个采样点：CPU 累计计数、网卡累计字节和采样时刻。
type linuxResourceSnapshot struct {
	cpu   procStatCPU
	cpuOK bool
	rx    uint64
	tx    uint64
	at    time.Time
}

// linuxResourceSample 是两个采样点之间算出来的结果，资源信息接口直接返回它。
type linuxResourceSample struct {
	cpuUsage float64
	netRx    uint64
	netTx    uint64
	rxSpeed  float64
	txSpeed  float64
}

func readLinuxResourceSnapshot() linuxResourceSnapshot {
	snap := linuxResourceSnapshot{at: time.Now()}
	if content, err := os.ReadFile("/proc/stat"); err == nil {
		snap.cpu, snap.cpuOK = parseProcStatCPU(content)
	}
	snap.rx, snap.tx = getLinuxNetBytes()
	return snap
}

func buildLinuxResourceSample(prev, cur linuxResourceSnapshot) linuxResourceSample {
	sample := linuxResourceSample{netRx: cur.rx, netTx: cur.tx}
	if prev.cpuOK && cur.cpuOK {
		sample.cpuUsage = cpuUsagePercent(prev.cpu, cur.cpu)
	}
	elapsed := cur.at.Sub(prev.at)
	sample.rxSpeed = netBytesPerSecond(prev.rx, cur.rx, elapsed)
	sample.txSpeed = netBytesPerSecond(prev.tx, cur.tx, elapsed)
	return sample
}

const (
	// linuxResourceSampleInterval 是后台采样周期，接口给出的是这段时间的平均值。
	linuxResourceSampleInterval = 3 * time.Second
	// linuxResourceFallbackWindow 只用于后台还没出第一份结果时的同步兜底采样。
	linuxResourceFallbackWindow = 500 * time.Millisecond
)

// linuxResourceSampler 在后台定时采样 CPU 与网速，资源信息接口只读缓存。
//
// 为什么不再在请求里现场采（#140）：APP 首页会并发请求资源信息、统计（遍历脚本目录）、
// 仪表盘（聚合一周日志），现场 sleep 500ms 的窗口正好罩住面板自己处理这批请求的开销，
// 2 核机器上读数被抬到 50% 左右，而同机其它面板只有个位数。
type linuxResourceSampler struct {
	read           func() linuxResourceSnapshot
	interval       time.Duration
	fallbackWindow time.Duration
	// stop 只给测试收掉后台循环用；生产环境为 nil，循环随进程存活。
	stop <-chan struct{}

	startOnce sync.Once
	mu        sync.RWMutex
	latest    linuxResourceSample
	hasLatest bool
	// fallbackMu 让首批并发请求只做一次同步采样，其余的直接拿它的结果。
	fallbackMu sync.Mutex
}

var defaultLinuxResourceSampler = &linuxResourceSampler{
	read:           readLinuxResourceSnapshot,
	interval:       linuxResourceSampleInterval,
	fallbackWindow: linuxResourceFallbackWindow,
}

// current 返回最近一次采样结果；还没有结果时同步采样一次兜底。
func (s *linuxResourceSampler) current() linuxResourceSample {
	// 懒启动：第一次有人要资源信息时才起后台循环，不动面板启动流程；
	// ddp status 这类一次性命令只是多一个随进程退出的 goroutine。
	s.startOnce.Do(func() { go s.loop() })

	if sample, ok := s.cached(); ok {
		return sample
	}

	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()
	if sample, ok := s.cached(); ok {
		return sample
	}
	prev := s.read()
	time.Sleep(s.fallbackWindow)
	sample := buildLinuxResourceSample(prev, s.read())
	s.mu.Lock()
	// 后台循环若已先一步写入，它的窗口更长更准，不拿兜底结果覆盖。
	if !s.hasLatest {
		s.latest, s.hasLatest = sample, true
	}
	s.mu.Unlock()
	return sample
}

func (s *linuxResourceSampler) cached() (linuxResourceSample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest, s.hasLatest
}

func (s *linuxResourceSampler) loop() {
	prev := s.read()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cur := s.read()
			sample := buildLinuxResourceSample(prev, cur)
			prev = cur
			s.mu.Lock()
			s.latest, s.hasLatest = sample, true
			s.mu.Unlock()
		case <-s.stop:
			return
		}
	}
}

func getLinuxNetBytes() (rx, tx uint64) {
	content, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") || strings.HasPrefix(line, "lo:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

var (
	resourceCheckOnce sync.Once
	resourceCheckStop chan struct{}
	lastWarnTime      time.Time
)

func StartResourceWatcher() {
	resourceCheckOnce.Do(func() {
		resourceCheckStop = make(chan struct{})
		go resourceWatchLoop()
		log.Println("resource watcher started (interval: 5min)")
	})
}

func StopResourceWatcher() {
	if resourceCheckStop != nil {
		close(resourceCheckStop)
	}
}

func resourceWatchLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	time.Sleep(30 * time.Second)
	checkResourceThresholds()

	for {
		select {
		case <-ticker.C:
			checkResourceThresholds()
		case <-resourceCheckStop:
			return
		}
	}
}

func checkResourceThresholds() {
	if !model.GetRegisteredConfigBool("notify_on_resource_warn") {
		return
	}

	if time.Since(lastWarnTime) < 30*time.Minute {
		return
	}

	info := GetResourceInfo()
	cpuThreshold := float64(model.GetRegisteredConfigInt("cpu_warn"))
	memThreshold := float64(model.GetRegisteredConfigInt("memory_warn"))
	diskThreshold := float64(model.GetRegisteredConfigInt("disk_warn"))

	var warnings []string
	if info.CPUUsage > cpuThreshold {
		warnings = append(warnings, fmt.Sprintf("CPU 使用率 %.1f%% (阈值 %.0f%%)", info.CPUUsage, cpuThreshold))
	}
	if info.MemoryUsage > memThreshold {
		warnings = append(warnings, fmt.Sprintf("内存使用率 %.1f%% (阈值 %.0f%%)", info.MemoryUsage, memThreshold))
	}
	if info.DiskUsage > diskThreshold {
		warnings = append(warnings, fmt.Sprintf("磁盘使用率 %.1f%% (阈值 %.0f%%)", info.DiskUsage, diskThreshold))
	}

	if len(warnings) > 0 {
		lastWarnTime = time.Now()
		content := "以下资源使用超过告警阈值：\n\n" + strings.Join(warnings, "\n")
		go SendNotification("系统资源告警", content)
		log.Printf("resource warn: %s", strings.Join(warnings, "; "))
	}
}
