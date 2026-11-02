package handler

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LogHandler struct{}

// logStreamHeartbeatInterval 是执行日志流发 keepalive 心跳的周期，也是复查登录会话是否已撤销的周期。
// 抽成变量只是为了让测试调短（真等 30 秒太慢），生产上就是 30 秒。
var logStreamHeartbeatInterval = 30 * time.Second

func NewLogHandler() *LogHandler {
	return &LogHandler{}
}

// parseQueryTime 解析 query string 里的 RFC3339 时间，解析不出来就返回 ok=false。
//
// 【为什么要把空格换回加号再试一次】
// RFC3339 的东八区偏移写作 `+08:00`，而 `+` 在 application/x-www-form-urlencoded
// 的 query string 里是【空格的转义】。调用方只要没把参数 percent-encode（手写 curl、
// 拼 URL 的脚本、Open API 的第三方调用方都很容易这样），服务端 c.Query 拿到的就是
// `2026-08-17T00:00:00 08:00`，time.Parse 直接失败，筛选条件被静默忽略——
// 接口照样 200、照样返回数据，只是过滤没生效，排查起来毫无线索。
//
// 前端走 axios 的 params，会正确编码成 %2B，不依赖这条容错；
// 它是给手工调用方兜底的。UTC 的 `Z` 后缀不受影响。
//
// 解析失败一律返回 ok=false 而不是报 400：日期筛选是附加能力，
// 因为一个畸形参数就让整个日志页打不开，代价不对等。
func parseQueryTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if strings.Contains(raw, " ") {
		if t, err := time.Parse(time.RFC3339, strings.ReplaceAll(raw, " ", "+")); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func (h *LogHandler) List(c *gin.Context) {
	taskIDStr := c.Query("task_id")
	statusStr := c.Query("status")
	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := database.DB.Model(&model.TaskLog{}).
		Joins("LEFT JOIN tasks ON tasks.id = task_logs.task_id")

	if taskIDStr != "" {
		taskID, _ := strconv.ParseUint(taskIDStr, 10, 32)
		query = query.Where("task_logs.task_id = ?", taskID)
	}
	if statusStr != "" {
		status, err := strconv.Atoi(statusStr)
		if err == nil {
			query = query.Where("task_logs.status = ?", status)
		}
	}
	if keyword != "" {
		query = query.Where("tasks.name LIKE ?", "%"+keyword+"%")
	}

	// 执行时间范围筛选。前端传 RFC3339（web/src/utils/datetime.ts 的 toDateRangeParams），
	// 两端都已经在前端收拢到「当天 00:00:00.000 ~ 23:59:59.999」，这里直接闭区间比较。
	//
	// 【为什么筛 created_at 而不是 started_at】
	// 列表里给用户看的那一列就是 created_at（web/src/views/logs/index.vue 的时间列），
	// 侧栏「今日失败」角标数的也是 created_at。筛选口径必须跟用户看到的东西一致，
	// 否则会出现「明明列表里写着 8-23，按 8-23 筛却少了几条」这种没法解释的现象。
	// 排序仍沿用 started_at DESC，不在本次改动范围内。
	//
	// 解析失败一律忽略该条件，不报 400：日期筛选是锦上添花的能力，
	// 因为一个畸形参数就让整个日志页打不开，代价不对等。
	if t, ok := parseQueryTime(c.Query("start_time")); ok {
		query = query.Where("task_logs.created_at >= ?", t)
	}
	if t, ok := parseQueryTime(c.Query("end_time")); ok {
		query = query.Where("task_logs.created_at <= ?", t)
	}

	var total int64
	query.Count(&total)

	var logs []model.TaskLog
	query.Select("task_logs.*").
		Preload("Task").
		Order("task_logs.started_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	data := make([]map[string]interface{}, len(logs))
	for i, l := range logs {
		data[i] = l.ToDict()
	}

	response.Paginated(c, data, total, page, pageSize)
}

func (h *LogHandler) Stream(c *gin.Context) {
	taskIDStr := c.Param("id")
	taskID, _ := strconv.ParseUint(taskIDStr, 10, 32)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// 先写一条 SSE 注释行并 Flush，把响应头立刻推给客户端。
	//
	// 【为什么必须放在所有查库逻辑之前】
	// gin 在第一次真正写响应体之前不会发响应头，而下面「等 TinyLog」那段最坏还要等 1.5s。
	// 这期间前端 utils/sse.ts 的 fetch 一直 pending、onOpen 不触发，日志弹窗就是纯白屏转圈——
	// 服务端明明在正常工作，用户看到的却是「点了没反应」。先把头发出去，弹窗骨架能立刻出来。
	//
	// 这一行对前端是零副作用 no-op：sse.ts 的 dispatchEventSegment 对 ":" 开头的行直接 continue，
	// 整段没有 event/data 字段会被丢弃，和下面已有的 keepalive 心跳完全同形状。
	fmt.Fprint(c.Writer, ": open\n\n")
	c.Writer.Flush()

	mgr := service.GetTinyLogManager()
	tl := mgr.FindByTaskID(uint(taskID))

	if tl != nil {
		history, _ := tl.ReadAll()
		if len(history) > 0 {
			writeSSEData(c.Writer, string(history))
			c.Writer.Flush()
		}

		sub := tl.Subscribe()
		defer tl.Unsubscribe(sub)

		// 这条流只在连上时过一次 JWTAuth，而执行日志流能一直推到任务结束，所以每个心跳周期要复查一次撤销。
		// 心跳用固定周期的 ticker，不再是「静默满 30s 才发」：复查挂在心跳上，一直有输出的任务
		// 永远等不到静默心跳，复查也就永远不跑。有输出时多出来的注释心跳，两端都直接跳过。
		claims := c.MustGet("claims").(*middleware.Claims)
		heartbeat := time.NewTicker(logStreamHeartbeatInterval)
		defer heartbeat.Stop()
		ctx := c.Request.Context()
		for {
			select {
			case data, ok := <-sub:
				if !ok {
					fmt.Fprintf(c.Writer, "event: done\ndata: finished\n\n")
					c.Writer.Flush()
					return
				}
				writeSSEData(c.Writer, string(data))
				c.Writer.Flush()
			case <-ctx.Done():
				// 面板关停时请求 ctx 会被取消（main 里的 BaseContext），这里是干净收流：
				// 🔴 不发 done 的话网页日志弹窗只认 done / onError，干净 EOF 一个回调都不触发，会静默卡在「运行中」。
				// 与下面会话撤销那条同口径发 reconnect：网页与 APP 去重连，面板没起来就走各自的出错收口。
				// 客户端自己断开时 ctx 同样会取消，这次写入只是写失败，没有副作用。
				fmt.Fprintf(c.Writer, "event: done\ndata: reconnect\n\n")
				c.Writer.Flush()
				return
			case <-heartbeat.C:
				// 登录会话已被撤销：结束这条流。每条打开的流每个心跳周期多 1 条 SQL，不在每请求路径上。
				// 🔴 必须先发 done:reconnect 再收流：不发 done 直接断开，网页的日志弹窗会静默卡在「运行中」；
				// reconnect 让网页与 APP 重连 → 401 → 续期失败 → 回登录页。
				if middleware.AccessTokenRevoked(claims) {
					fmt.Fprintf(c.Writer, "event: done\ndata: reconnect\n\n")
					c.Writer.Flush()
					return
				}
				// 静默不代表任务结束（慢接口/长计算/下载/sleep 都会无输出）。
				// 这里发一条 SSE keepalive 注释心跳并继续保持连接（不 return）：
				//   - 维持长连接，避免反代/网关空闲超时主动断开；
				//   - 不再断开重连重发整段历史，消除安静任务的周期性全量重渲染卡顿；
				//   - 任务真正结束时 sub 通道会关闭，自然走上面已有的 done:finished 分支。
				// 前端 sse.ts dispatchEventSegment 对 ":" 开头的行直接 continue，注释心跳是无副作用 no-op。
				fmt.Fprintf(c.Writer, ": keepalive\n\n")
				c.Writer.Flush()
			}
		}
	}

	var task model.Task
	database.DB.First(&task, taskID)
	if task.Status != model.TaskStatusRunning {
		// 【短轮询等待 TinyLog，替代原来无条件的 time.Sleep(1500ms)】
		//
		// 要兜的仍是启动竞态：用户点「运行」后前端马上打开日志弹窗，此刻 runTask 可能还没执行到
		// GetTinyLogManager().Create()，上面的 FindByTaskID 就会拿到 nil。
		//
		// 原来的写法是无条件睡满 1.5s 再复查一次，代价是「点日志看上次跑完的结果」这条最常走的路径
		// 也被罚满 1.5s（issue #109-1）——任务处于 Disabled(0)/Enabled(1) 时根本没有任何执行在启动，
		// 等再久也不会有 TinyLog 冒出来，这 1.5s 是纯浪费。
		//
		// 改成：只在任务「可能马上产出实时日志」时才等，每 150ms 探一次、封顶 1.5s，一出现立刻停手。
		// 「可能马上产出」= 排队中(0.5) 或 运行中(2)。反过来，Disabled/Enabled 现在是 0 等待直接收口。
		//
		// 手动运行这条主路径是完备的：SchedulerV2.RunNow（scheduler_v2.go:677-681）先 Enqueue、
		// 再把状态置成 Queued，两步都在 POST /tasks/:id/run 的响应返回之前完成，而前端是拿到该响应
		// 之后才打开日志流的（tasks/index.vue 的 handleRun 先 await run 再 openLogViewer）。
		//
		// ⚠️ 已知的未覆盖窗口（刻意接受，不要当成漏改）：定时触发（scheduler_v2.go:538-542）与
		// RunNow 同形，也是**先 Enqueue、再置 Queued**，两句之间存在一个极短窗口——请求已经在队列里、
		// worker 随时可能建 TinyLog，而 tasks.status 还停在 Enabled。用户恰好在这个窗口打开日志弹窗时，
		// 这里会 0 等待直接判完成。旧的 1.5s 固定 sleep 能兜住它，但代价是所有人每次看日志都罚 1.5s。
		// 后果只是「显示已完成，重开一次弹窗就正常」，不丢数据、不影响任务本身，所以取速度。
		pendingStatus := task.Status
		// waited 记录「本次到底有没有真的等过」。它要回传给前端：
		// 前端在打开弹窗那一刻并行预取了一份 latest-log 作为提速手段（issue #109-1 的后半段），
		// 而只要服务端等过，就说明这条任务当时正在启动或运行 —— 那份预取快照必然比
		// 「等完之后的真实结果」旧（很可能整份是上一次运行的日志），绝不能拿来渲染。
		// 详见下面 streamDoneEvent 的注释。
		waited := false
		deadline := time.Now().Add(1500 * time.Millisecond)
		for tl == nil && time.Now().Before(deadline) &&
			(pendingStatus == model.TaskStatusQueued || pendingStatus == model.TaskStatusRunning) {
			waited = true
			select {
			case <-c.Request.Context().Done():
				// 轮询期间客户端完全可能已经走了（关弹窗、切任务、刷新页面），
				// 不检测的话这条请求还要继续空等到封顶才结束。
				// 面板关停也会走到这里：同样先发 done:reconnect 再收流，理由见上面实时分支的 ctx.Done。
				fmt.Fprintf(c.Writer, "event: done\ndata: reconnect\n\n")
				c.Writer.Flush()
				return
			case <-time.After(150 * time.Millisecond):
			}
			tl = mgr.FindByTaskID(uint(taskID))
			// 状态也要跟着复查：任务被取消、或者执行得极快已经收尾，状态会退回 Enabled/Disabled，
			// 这时继续等下去同样是白等，提前跳出。
			var polled model.Task
			database.DB.Select("status").First(&polled, taskID)
			pendingStatus = polled.Status
		}
		if tl != nil {
			history, _ := tl.ReadAll()
			if len(history) > 0 {
				writeSSEData(c.Writer, string(history))
				c.Writer.Flush()
			}
		}
		// 等待结束后重查真实状态：若已开始运行 → reconnect（前端重连后进入 tl != nil 流式分支），否则才判完成。
		var t model.Task
		database.DB.Select("status").First(&t, taskID)
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", streamDoneEvent(t.Status, waited))
		c.Writer.Flush()
	} else {
		idleCount := 0
		c.Stream(func(w io.Writer) bool {
			// c.Stream 只认客户端断开、不认 ctx：面板关停时不在这里看一眼，要轮询满 60 秒才结束，
			// 把 HTTP 关停拖到超时。先发 done:reconnect 再收流，理由同上面实时分支的 ctx.Done。
			if c.Request.Context().Err() != nil {
				fmt.Fprintf(w, "event: done\ndata: reconnect\n\n")
				c.Writer.Flush()
				return false
			}
			tl = mgr.FindByTaskID(uint(taskID))
			if tl != nil {
				history, _ := tl.ReadAll()
				if len(history) > 0 {
					writeSSEData(w, string(history))
					c.Writer.Flush()
				}
				fmt.Fprintf(w, "event: done\ndata: reconnect\n\n")
				c.Writer.Flush()
				return false
			}

			idleCount++
			if idleCount >= 120 {
				// 等满约 60s（120 * 500ms）TinyLog 始终未出现：该任务没有可流式的实时日志
				// （典型为 conc / SuppressLiveOutput 抑制输出的运行任务）。此处直接发 finished，
				// 不再用 streamDoneEvent —— 否则 status==running 会回 reconnect，
				// 而前端重连后仍是 tl==nil，60s 后又超时，形成无可续流的无限重连风暴。
				fmt.Fprintf(w, "event: done\ndata: finished\n\n")
				c.Writer.Flush()
				return false
			}

			// 等下一轮时也认 ctx：关停时不必睡满这 500ms，下一轮开头会发结束事件。
			select {
			case <-c.Request.Context().Done():
			case <-time.After(500 * time.Millisecond):
			}
			return true
		})
	}
}

// writeSSEData 把一段原始脚本输出写成 SSE 帧。
//
// 【裸 \r 必须原样保留，但绝不能埋在一条 data 行的中间】
//
// 终端进度条用裸 \r 回到行首覆盖当前行，这个语义要一路传到前端，所以不能像 \r\n 那样
// 归一成 \n（.trellis/spec/backend/logging-guidelines.md 明令禁止，
// log_stream_regression_test.go 有断言守着）。
//
// 但 SSE 的线格式里 CR 本身就是【行终止符】：`data: 10%\r20%\r30%` 在标准解析器
// （浏览器原生 EventSource、各语言 SSE 客户端、独立仓库里的 APP）眼里是三行，
// 后两行 `20%` / `30%` 没有字段名，会被静默丢弃 —— 连报错都没有。
// 而这个函数在打开日志弹窗时会被喂进【整份历史日志】，里面往往有成百上千个裸 \r，
// 等于一份带进度条的日志在标准解析器下只剩第一个 \r 之前那一点点。
//
// 修法是不动字符、只改分帧：每碰到一个后面还有内容的裸 \r，就把它作为当前帧最后一条
// data 行的结尾，剩下的内容另起一帧。这样 \r 永远落在行末，谁来解析都不会丢内容。
//
// 对本仓前端完全等价，多帧拼起来与改动前的单帧【逐字节一致】：
//   - web/src/utils/sse.ts 的 dispatchEventSegment 对 data 行末尾的 \r 刻意不做 trim；
//   - LogViewer.vue / logs/index.vue 拿到相邻消息是直接首尾相接 append 的，
//     不会在两条消息之间补换行（LogViewer.vue 里「不把每个 SSE 消息结束当成真实换行」那段注释）；
//   - 这里只做切分，不增删任何字符，所以 append 回去还是原来那串。
//
// ⚠️ 不要图省事改成「一帧里多条 data 行、每条以 \r 结尾」：SSE 规定同一帧的多条 data
// 行要用 \n 拼接，那样裸 \r 会变成 \r\n，前端 ansi.ts 把 \r\n 当真实换行，进度条立刻刷屏。
func writeSSEData(w io.Writer, data string) {
	data = strings.ReplaceAll(data, "\r\n", "\n")

	// 用「先写一帧、再看有没有剩余」的写法（而不是 for len(data) > 0），
	// 是为了让空字符串仍然写出一帧 `data: \n\n`，与改动前的行为保持一致。
	for {
		segment := data
		rest := ""
		// idx+1 == len(data) 表示 \r 已经在整段末尾，本来就落在行末，不用再切。
		if idx := strings.IndexByte(data, '\r'); idx >= 0 && idx+1 < len(data) {
			segment = data[:idx+1]
			rest = data[idx+1:]
		}
		for _, line := range strings.Split(segment, "\n") {
			fmt.Fprintf(w, "data: %s\n", line)
		}
		fmt.Fprint(w, "\n")
		if rest == "" {
			return
		}
		data = rest
	}
}

// streamDoneEvent 根据任务真实状态与「服务端有没有等过」决定 SSE done 事件的 data 值。
// 任务仍在运行 → "reconnect"，让前端 LogViewer 无缝重连续流，避免误判"已完成"；
// 其它状态（已结束/排队/禁用等）→ "finished"，前端正常置为"已完成"。
// streamDoneEvent 决定 done 事件的载荷。三种取值：
//
//	reconnect      任务已经在跑，前端重连后进入流式分支
//	finished       任务早就结束了，服务端一秒没等、直接收口
//	finished-late  服务端在短轮询里真的等过（打开弹窗时任务处于排队中/运行中）
//
// 为什么要把 finished 和 finished-late 分开：
// 前端为了省掉一个串行往返，会在打开弹窗那一刻**并行**预取一份 latest-log。
// 只有 finished 这条路径（服务端 0 等待，说明任务早已结束、日志早已落库）
// 才允许复用那份预取。finished-late 意味着这条任务当时正在启动或运行：
//   - 预取发出时本次运行的日志行可能还没建出来，按 started_at DESC 取到的是**上一次运行**的记录；
//   - 或者刚建出来、content 还是空的。
//
// 两种情况复用预取都会把「上一次的输出」或「日志已过期」渲染成本次结果。
// 靠前端那道 TTL 挡不住：任务在一两百毫秒内跑完时，done 到达时预取仍在有效期内。
//
// 前端对未知载荷的兜底是「当成 finished 处理」，所以新增这个取值对演示站的假流、
// 以及独立仓库里的 APP 都是向后兼容的（最坏是退回旧行为，不会报错）。
func streamDoneEvent(status float64, waited bool) string {
	if status == model.TaskStatusRunning {
		return "reconnect"
	}
	if waited {
		return "finished-late"
	}
	return "finished"
}

func (h *LogHandler) Detail(c *gin.Context) {
	logID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var taskLog model.TaskLog
	if err := database.DB.Preload("Task").First(&taskLog, logID).Error; err != nil {
		response.NotFound(c, "日志不存在")
		return
	}

	result := taskLog.ToDict()
	if taskLog.Content != "" {
		decompressed, err := service.DecompressFromBase64(taskLog.Content)
		if err == nil {
			result["content"] = decompressed
		}
	} else if taskLog.LogPath != nil {
		content, err := service.ReadLogFile(*taskLog.LogPath, config.C.Data.LogDir)
		if err == nil {
			result["content"] = content
		}
	}

	response.Success(c, result)
}

func (h *LogHandler) Delete(c *gin.Context) {
	logID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的日志ID")
		return
	}
	// 删日志一律经 service.DeleteTaskLogs（#158）：同一个事务里先把这一行并进执行趋势的按天计数（仪表板数字不跟着变少），
	// 再取 log_path（行一删路径就查不回来，磁盘上的 .log 会变成没人认领的垃圾，issue #144 / v3.3.2），最后删行。
	// 这里是按 id 删单条，用户点的就是这一条，不需要 status 条件。
	var deleted int64
	var paths []string
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		deleted, paths, err = service.DeleteTaskLogs(tx, "id = ?", logID)
		return err
	}); err != nil {
		// 数据库出错：事务已回滚，行和计数都没动。回 500，不再误报成「日志不存在」。
		response.InternalError(c, "删除日志失败")
		return
	}
	if deleted == 0 {
		response.NotFound(c, "日志不存在")
		return
	}
	// 事务提交之后再按路径删磁盘文件。
	service.DeleteLogFilesForRecords(paths, config.C.Data.LogDir)
	response.Success(c, gin.H{"message": "日志已删除"})
}

func (h *LogHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	// 同 Delete：并入计数 → 取 log_path → 删行在同一个事务里（#158、#144），提交后再按路径删磁盘文件。
	// 一条都没命中照旧回 200「已删除 0 条日志」；只有数据库出错才回 500。
	var deleted int64
	var paths []string
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		deleted, paths, err = service.DeleteTaskLogs(tx, "id IN ?", req.IDs)
		return err
	}); err != nil {
		response.InternalError(c, "删除日志失败")
		return
	}
	service.DeleteLogFilesForRecords(paths, config.C.Data.LogDir)
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已删除 %d 条日志", deleted),
	})
}

func (h *LogHandler) Clean(c *gin.Context) {
	defaultDays := model.GetRegisteredConfigInt("log_retention_days")
	daysStr := c.DefaultQuery("days", strconv.Itoa(defaultDays))
	days, _ := strconv.Atoi(daysStr)
	if days < 1 {
		days = defaultDays
	}

	// 删记录 + 删磁盘文件 + 清空目录统一交给 service，与自动清理、定时任务页「清理日志」
	// 走完全同一条路径（issue #144 / v3.3.2）。这里原来零文件操作，就是 issue 里
	// 「清理完日志文件还在」的现象。
	records, files := service.CleanLogsOlderThan(days)
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已清理 %d 条日志记录、%d 个日志文件（保留最近 %d 天）", records, files, days),
	})
}

func (h *LogHandler) RegisterRoutes(r *gin.RouterGroup) {
	logs := r.Group("/logs")
	{
		logs.GET("", middleware.JWTAuth(), middleware.OpenAPIAccess("logs"), middleware.RequireRole("viewer"), h.List)
		logs.DELETE("/batch", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.BatchDelete)
		logs.POST("/batch-delete", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.BatchDelete)
		logs.DELETE("/clean", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.Clean)
		logs.GET("/:id/stream", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("viewer"), h.Stream)
		logs.GET("/:id/raw-ticket", middleware.JWTAuth(), middleware.OpenAPIAccess("logs"), middleware.RequireRole("viewer"), h.RawDownloadTicket)
		// 原始日志直传下载：浏览器原生下载无法携带 Authorization 头，所以这条路由不能挂 JWTAuth，
		// 改由上面的 raw-ticket 接口（鉴权与 /logs/:id 完全一致）签发短期票据，handler 内部验票。
		logs.GET("/:id/raw", h.DownloadRawLog)
		logs.GET("/:id", middleware.JWTAuth(), middleware.OpenAPIAccess("logs"), middleware.RequireRole("viewer"), h.Detail)
		logs.DELETE("/:id", middleware.JWTAuth(), middleware.RequireUserToken(), middleware.RequireRole("operator"), h.Delete)
	}
}
