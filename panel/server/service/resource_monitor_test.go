package service

import (
	"sync"
	"testing"
	"time"
)

func TestParseProcMeminfoUsesMemAvailable(t *testing.T) {
	total, used, free := parseProcMeminfo([]byte(`
MemTotal:       16384256 kB
MemFree:         1024000 kB
MemAvailable:    8192000 kB
Buffers:          256000 kB
Cached:          2048000 kB
SReclaimable:     128000 kB
Shmem:             64000 kB
`))

	const kib = 1024
	if total != 16384256*kib {
		t.Fatalf("expected total memory from MemTotal, got %d", total)
	}
	if free != 8192000*kib {
		t.Fatalf("expected free memory to follow MemAvailable, got %d", free)
	}
	if used != (16384256-8192000)*kib {
		t.Fatalf("expected used memory to be total-available, got %d", used)
	}
}

func TestParseProcMeminfoFallsBackWithoutMemAvailable(t *testing.T) {
	total, used, free := parseProcMeminfo([]byte(`
MemTotal:        1000 kB
MemFree:          100 kB
Buffers:          200 kB
Cached:           300 kB
SReclaimable:      50 kB
Shmem:             25 kB
`))

	const kib = 1024
	expectedFree := uint64(625 * kib)
	expectedUsed := uint64(375 * kib)

	if total != 1000*kib {
		t.Fatalf("expected total memory from MemTotal, got %d", total)
	}
	if free != expectedFree {
		t.Fatalf("expected fallback available memory %d, got %d", expectedFree, free)
	}
	if used != expectedUsed {
		t.Fatalf("expected used memory %d, got %d", expectedUsed, used)
	}
}

func mustParseProcStatCPU(t *testing.T, content string) procStatCPU {
	t.Helper()
	snap, ok := parseProcStatCPU([]byte(content))
	if !ok {
		t.Fatalf("expected /proc/stat to parse, got failure for %q", content)
	}
	return snap
}

// 口径钉死在「忙碌 = 总计 − idle − iowait，guest 不重复累加」（#140）。
// 每条都注明旧算法会给出的值，防止有人又改回「所有字段求和、只扣 idle」。
func TestCPUUsagePercentFromProcStatSnapshots(t *testing.T) {
	cases := []struct {
		name string
		prev string
		cur  string
		want float64
	}{
		{
			// 只看汇总行，后面的 cpu0 不能干扰解析。
			name: "plain",
			prev: "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\n",
			cur:  "cpu  150 0 150 900 0 0 0 0 0 0\ncpu0 75 0 75 450 0 0 0 0 0 0\n",
			want: 50,
		},
		{
			// irq / softirq / steal 都算忙碌：总计 +200，忙碌 +90。
			name: "irq softirq steal count as busy",
			prev: "cpu  100 20 30 800 10 5 5 30 0 0\n",
			cur:  "cpu  140 30 40 900 20 10 10 50 0 0\n",
			want: 45,
		},
		{
			// iowait 涨了 100：不算忙碌，结果 10%；旧算法把它当忙碌会得 60%。
			name: "iowait is idle",
			prev: "cpu  100 0 100 700 100 0 0 0 0 0\n",
			cur:  "cpu  110 0 110 780 200 0 0 0 0 0\n",
			want: 10,
		},
		{
			// user 里已含 guest 100：结果 50%；旧算法重复累加 guest 会得 66.67%。
			name: "guest already inside user",
			prev: "cpu  1000 0 0 1000 0 0 0 0 0 0\n",
			cur:  "cpu  1100 0 0 1100 0 0 0 0 100 0\n",
			want: 50,
		},
		{
			name: "guest_nice already inside nice",
			prev: "cpu  0 1000 0 1000 0 0 0 0 0 0\n",
			cur:  "cpu  0 1100 0 1100 0 0 0 0 0 100\n",
			want: 50,
		},
		{
			// 老内核只有前 4 项，缺的按 0 算。
			name: "old kernel with four fields",
			prev: "cpu  100 0 100 800\n",
			cur:  "cpu  130 0 130 940\n",
			want: 30,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := mustParseProcStatCPU(t, tc.prev)
			cur := mustParseProcStatCPU(t, tc.cur)
			if got := cpuUsagePercent(prev, cur); got != tc.want {
				t.Fatalf("expected cpu usage %.2f, got %.2f", tc.want, got)
			}
		})
	}
}

// 计数回绕 / 快照传反 / 两次快照之间总计没涨：都报 0，不能让无符号减法下溢出离谱的值。
func TestCPUUsagePercentReturnsZeroOnWrapOrNoProgress(t *testing.T) {
	prev := procStatCPU{total: 1000, idle: 800}

	if got := cpuUsagePercent(prev, prev); got != 0 {
		t.Fatalf("expected 0 when total did not advance, got %.2f", got)
	}
	if got := cpuUsagePercent(prev, procStatCPU{total: 900, idle: 700}); got != 0 {
		t.Fatalf("expected 0 when counters went backwards, got %.2f", got)
	}
	// 总计在涨但忙碌量倒退（部分内核 iowait 回退）：同样报 0。
	if got := cpuUsagePercent(prev, procStatCPU{total: 1010, idle: 850}); got != 0 {
		t.Fatalf("expected 0 when busy counter went backwards, got %.2f", got)
	}
}

func TestParseProcStatCPURejectsMalformedContent(t *testing.T) {
	for _, content := range []string{
		"",
		"cpu0 100 0 100 800 0 0 0 0 0 0\n",
		"cpu  100 0 100\n",
		"cpu  100 x 100 800 0 0 0 0 0 0\n",
	} {
		if snap, ok := parseProcStatCPU([]byte(content)); ok {
			t.Fatalf("expected %q to be rejected, got %+v", content, snap)
		}
	}
}

func TestBuildLinuxResourceSampleComputesNetSpeedPerSecond(t *testing.T) {
	base := time.Unix(1700000000, 0)
	prev := linuxResourceSnapshot{
		cpu: procStatCPU{total: 1000, idle: 800}, cpuOK: true,
		rx: 1000, tx: 500, at: base,
	}
	cur := linuxResourceSnapshot{
		cpu: procStatCPU{total: 1200, idle: 900}, cpuOK: true,
		rx: 4000, tx: 6500, at: base.Add(3 * time.Second),
	}

	got := buildLinuxResourceSample(prev, cur)
	if got.cpuUsage != 50 {
		t.Fatalf("expected cpu usage 50, got %.2f", got.cpuUsage)
	}
	if got.netRx != 4000 || got.netTx != 6500 {
		t.Fatalf("expected cumulative bytes from the latest snapshot, got rx=%d tx=%d", got.netRx, got.netTx)
	}
	// 按实际间隔 3 秒折算，而不是沿用旧实现写死的「×2」。
	if got.rxSpeed != 1000 || got.txSpeed != 2000 {
		t.Fatalf("expected per-second speed rx=1000 tx=2000, got rx=%.2f tx=%.2f", got.rxSpeed, got.txSpeed)
	}

	// 网卡重建导致计数倒退、或 /proc/stat 读失败：对应读数报 0，不下溢。
	reset := cur
	reset.rx, reset.tx = 10, 20
	reset.cpuOK = false
	reset.at = cur.at.Add(3 * time.Second)
	got = buildLinuxResourceSample(cur, reset)
	if got.rxSpeed != 0 || got.txSpeed != 0 || got.cpuUsage != 0 {
		t.Fatalf("expected zero readings after counter reset, got %+v", got)
	}
}

// fakeResourceSnapshots 每读一次推进固定步长：CPU 总计 +100（其中忙碌 +50）、网卡 +3000 字节、时间 +3 秒。
// 任意两个点之间算出来都是 50% 与 1000 B/s，后台循环和兜底采样怎么交错取点都不影响断言。
type fakeResourceSnapshots struct {
	mu    sync.Mutex
	calls int
	base  time.Time
}

func (f *fakeResourceSnapshots) read() linuxResourceSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	n := uint64(f.calls)
	return linuxResourceSnapshot{
		cpu:   procStatCPU{total: n * 100, idle: n * 50},
		cpuOK: true,
		rx:    n * 3000,
		tx:    n * 3000,
		at:    f.base.Add(time.Duration(n) * 3 * time.Second),
	}
}

func (f *fakeResourceSnapshots) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func assertFakeResourceSample(t *testing.T, got linuxResourceSample) {
	t.Helper()
	if got.cpuUsage != 50 || got.rxSpeed != 1000 || got.txSpeed != 1000 {
		t.Fatalf("expected cpu=50 rx=1000 tx=1000, got %+v", got)
	}
}

// 有缓存时接口必须直接返回，不能再在请求里 sleep 采样 —— 这正是 #140 读数虚高的来源。
func TestLinuxResourceSamplerReturnsCachedSampleWithoutBlocking(t *testing.T) {
	fake := &fakeResourceSnapshots{base: time.Unix(0, 0)}
	s := &linuxResourceSampler{read: fake.read, interval: time.Hour, fallbackWindow: time.Hour}
	// 本用例只验读缓存，不让后台循环掺进来。
	s.startOnce.Do(func() {})
	s.latest, s.hasLatest = linuxResourceSample{cpuUsage: 12.5}, true

	done := make(chan linuxResourceSample, 1)
	go func() { done <- s.current() }()
	select {
	case got := <-done:
		if got.cpuUsage != 12.5 {
			t.Fatalf("expected cached cpu usage 12.5, got %.2f", got.cpuUsage)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("有缓存时 current 仍在同步采样（兜底窗口设成了 1 小时）")
	}
	if calls := fake.count(); calls != 0 {
		t.Fatalf("expected no snapshot reads when cache is present, got %d", calls)
	}
}

// 没有缓存时同步兜底：首批并发请求只采一次（读两个点），结果写进缓存，后续请求不再阻塞。
func TestLinuxResourceSamplerFallsBackOnceWhenCacheEmpty(t *testing.T) {
	fake := &fakeResourceSnapshots{base: time.Unix(0, 0)}
	s := &linuxResourceSampler{read: fake.read, interval: time.Hour, fallbackWindow: 20 * time.Millisecond}
	s.startOnce.Do(func() {})

	results := make([]linuxResourceSample, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = s.current()
		}(i)
	}
	wg.Wait()

	if calls := fake.count(); calls != 2 {
		t.Fatalf("expected a single fallback sample (2 reads) for concurrent first requests, got %d reads", calls)
	}
	for _, got := range results {
		assertFakeResourceSample(t, got)
	}

	assertFakeResourceSample(t, s.current())
	if calls := fake.count(); calls != 2 {
		t.Fatalf("expected later requests to hit the cache, got %d reads", calls)
	}
}

// 首次 current 懒启动后台循环，之后由循环定时刷新缓存。
func TestLinuxResourceSamplerLoopRefreshesCache(t *testing.T) {
	fake := &fakeResourceSnapshots{base: time.Unix(0, 0)}
	stop := make(chan struct{})
	defer close(stop)
	s := &linuxResourceSampler{
		read:           fake.read,
		interval:       10 * time.Millisecond,
		fallbackWindow: time.Millisecond,
		stop:           stop,
	}

	assertFakeResourceSample(t, s.current())

	// 清掉兜底写入的结果，接下来缓存只可能由后台循环写回。
	s.mu.Lock()
	s.hasLatest = false
	s.mu.Unlock()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if got, ok := s.cached(); ok {
			assertFakeResourceSample(t, got)
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("后台采样循环没有刷新缓存")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
