package handler

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

// 「日志文件夹打包下载」= 把一个任务的日志文件一次性打成 zip 流式吐给浏览器。
//
// 起因是每 5 分钟跑一次的任务，7 天就有 2000+ 个日志文件，逐个点「下载原始」根本点不完。
//
// 鉴权照抄同目录 log_raw_download.go 的两步换票（见那个文件的头注释），不发明新范式：
//  1. `GET .../log-files/archive-ticket` 走 JWTAuth + RequireRole("viewer")，签一张短期票据；
//  2. `GET .../log-files/archive?ticket=...` 只认票据（浏览器原生下载带不了 Authorization 头）。
//
// 路径必须是 `archive-ticket` → `archive`：issueRawLogTicket 靠
// strings.TrimSuffix(path, "-ticket") 推导下载地址，这样 /api 与 /api/v1 两套前缀自动对上。
const (
	// 打包上限。写死成常量而不做成配置项：它不是用户需要调的旋钮，而是一条保护线
	// （反代超时、容器内存、浏览器没有进度条可看），调大只会把失败推到更难排查的地方。
	//
	// 取值依据：单个 .log 有 10MB 硬上限（service/log_manager.go 的 maxLogFileSize），
	// log_retention_days 默认 7 但可以配到 3650，5 分钟频率的任务 7 天就有 2016 个文件。
	// 2000 个文件刚好压着这个量级，512MB 未压缩总量则覆盖了绝大多数真实场景。
	maxArchiveFiles = 2000
	maxArchiveBytes = 512 << 20
)

// archiveEntry 描述一个已经通过路径校验、确认存在的待打包日志文件。
type archiveEntry struct {
	// relPath 是日志根目录内的相对路径（形如 task_7_签到/2026-08-04-10-00-00-000.log），
	// 同时也直接用作 zip 内路径 —— 任务改过名会留下多个 task_<id>_<名字> 目录并存，
	// 带上目录前缀天然保留层级、不会撞名。
	relPath string
	absPath string
	modTime time.Time
}

// ---------------------------------------------------------------------------
// 签票：GET /tasks/:id/log-files/archive-ticket
// ---------------------------------------------------------------------------

// LogArchiveDownloadTicket 为某个任务的日志文件夹签发打包下载票据。
func (h *TaskHandler) LogArchiveDownloadTicket(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的任务ID")
		return
	}

	entries, totalBytes, ok := resolveTaskLogArchive(c, uint(taskID))
	if !ok {
		return
	}

	// 下载 URL 必须原样带回范围参数，否则下载端算出的资源标识对不上、验签必失败。
	start, end := c.Query("start"), c.Query("end")
	extra := url.Values{}
	if start != "" {
		extra.Set("start", start)
	}
	if end != "" {
		extra.Set("end", end)
	}

	target := &rawLogTarget{
		downloadName: taskLogArchiveDownloadName(uint(taskID)),
		// size 的语义是「未压缩总字节」：流式 zip 事先算不出压缩后大小，
		// 前端拿它来提示体积、判断是否值得下载，不能当成 Content-Length 用。
		size: totalBytes,
	}
	issueRawLogTicket(c, taskLogArchiveResource(taskID, start, end), target, extra, gin.H{
		"file_count": len(entries),
	})
}

// ---------------------------------------------------------------------------
// 下载：GET /tasks/:id/log-files/archive
// ---------------------------------------------------------------------------

// DownloadLogArchive 把一个任务的日志文件流式打包成 zip 吐给浏览器。
// 只接受 LogArchiveDownloadTicket 签发的票据，没有票据一律 401。
func (h *TaskHandler) DownloadLogArchive(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的任务ID")
		return
	}

	// 先验票再碰磁盘：资源标识里带的是请求里原样的 start/end，
	// 改动其中任何一个参数都换不到同一张票，扩大下载范围的尝试在这里就被挡掉。
	if !verifyRawLogTicket(c, taskLogArchiveResource(taskID, c.Query("start"), c.Query("end"))) {
		return
	}

	// 票据 120 秒内可以重放，这期间日志清理可能已经删掉一批文件，
	// 所以收集 + 上限校验要完整重跑一遍，不能沿用签票时的结果。
	entries, _, ok := resolveTaskLogArchive(c, uint(taskID))
	if !ok {
		return
	}

	c.Header("Content-Type", "application/zip")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", attachmentContentDisposition(taskLogArchiveDownloadName(uint(taskID))))
	// 刻意不设 Accept-Ranges：zip 是边压边发的，事先既算不出 Content-Length
	// 也没法定位任意偏移，声称支持 Range 只会让断线续传拿到一个截断的包。
	c.Status(http.StatusOK)

	zw := zip.NewWriter(c.Writer)
	skipped := 0
	for _, entry := range entries {
		err := writeArchiveEntry(zw, entry)
		if err == nil {
			continue
		}

		// 情况一：文件在被打开之前就已经不在了 —— 良性竞态，跳过它继续打包后面的。
		// 票据 120 秒内可以重放，收集列表到真正 os.Open 之间也有真实窗口，
		// 日志保留清理（service/log_manager.go）随时可能删掉其中几个文件。
		// 口径与 collectTaskLogArchiveEntries 一致：那里对「列举之后被清理掉」的文件
		// 也是直接过滤掉，所以这里同样按「少了那几个已经不存在的文件」处理，
		// 而不是让整包失败 —— 此时 zip 里还没写入这个 entry 的任何字节，包仍然是完好的。
		if errors.Is(err, errArchiveEntryMissing) {
			skipped++
			log.Printf("打包任务 %d 时跳过已被清理的日志文件 %s", taskID, entry.relPath)
			continue
		}

		// 情况二：其它错误（io.Copy 中途出错、磁盘读失败……）—— 包已经写坏了，直接放弃。
		//
		// 这里【绝对不能】写成「break 之后照常 zw.Close()」：Close() 会把中央目录正常写出去，
		// 客户端于是拿到一个能双击打开、结构完好、却静默少了后面全部文件的 zip，
		// 用户毫无察觉，事后才发现日志缺了一大截。
		//
		// 直接 return、不调用 Close() 并不会让这次下载「看起来就失败」——
		// 实测（gin v1.10.0 + gin.Recovery()，与 main.go 同配置，用足够大的内容强制进入真 chunked）：
		// handler 一 return，net/http 就会把 chunked 结束块正常写出去，客户端读 body 不报错，
		// 拿到的是一个完整良构的 HTTP 200，浏览器照样显示「下载完成」。
		// 真正兜住用户的是【zip 缺了中央目录必然打不开】（解压器直接报 not a valid zip file）：
		// 相比 break + Close() 产出的那个「结构完好却静默少文件」的包，这是一个能被用户发现的失败，
		// 代价只是发现时机推迟到双击解压那一刻，而不是下载的当下。
		//
		// 也不要为了「掐断连接」换成 panic(http.ErrAbortHandler)：实测 gin.Recovery() 会把它吞掉，
		// 结果是连接照样正常收尾、面板日志里还多出整段 panic 堆栈，比现在这个方案更差。
		// 响应头早就发出去了，状态码已经改不了，能做的就只有「不给假成功」+ 留日志。
		log.Printf("打包任务 %d 的日志文件 %s 失败，中止本次打包（刻意不写中央目录，避免客户端拿到一个静默残缺的 zip）: %v",
			taskID, entry.relPath, err)
		return
	}

	if skipped > 0 {
		// 汇总记一条，方便事后对上「用户说少了几个文件」和「那会儿正好在清理日志」。
		log.Printf("打包任务 %d 的日志压缩包时共跳过 %d 个已被清理的文件", taskID, skipped)
	}

	if err := zw.Close(); err != nil {
		// 同上：中央目录写失败时客户端拿到的是一个不完整的 zip，
		// 但此刻已经无法回退成 JSON 错误，只能留在服务端日志里供排查。
		log.Printf("关闭任务 %d 的日志压缩包失败: %v", taskID, err)
	}
}

// errArchiveEntryMissing 表示「还没往 zip 里写这个 entry 的任何字节，文件就已经不在了」。
//
// 单独包一层、而不是让调用方直接 errors.Is(err, fs.ErrNotExist)：只有 os.Open 这一步的
// not-exist 才是可以安全跳过的良性竞态；一旦 CreateHeader / io.Copy 已经开始往流里写，
// 任何错误（哪怕碰巧也是 not-exist）都意味着这个包已经废了，必须走中止分支。
var errArchiveEntryMissing = errors.New("日志文件已不存在")

func writeArchiveEntry(zw *zip.Writer, entry archiveEntry) error {
	file, err := os.Open(entry.absPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %w", errArchiveEntryMissing, err)
		}
		return err
	}
	defer file.Close()

	writer, err := zw.CreateHeader(&zip.FileHeader{
		Name:     entry.relPath,
		Method:   zip.Deflate,
		Modified: entry.modTime,
	})
	if err != nil {
		return err
	}

	// io.Copy 走 32KB 缓冲，不会把整个文件读进内存（容器内存很紧张）。
	_, err = io.Copy(writer, file)
	return err
}

// ---------------------------------------------------------------------------
// 资源标识
// ---------------------------------------------------------------------------

// taskLogArchiveResource 与 taskLogFileResource 同口径：用请求里【原样】的参数，
// 这样下载接口可以在碰磁盘之前先验签。
func taskLogArchiveResource(taskID uint64, start, end string) string {
	return fmt.Sprintf("task-log-archive:%d:%s:%s", taskID, start, end)
}

// ---------------------------------------------------------------------------
// 收集 + 校验（必须在写 zip 的第一个字节之前全部做完）
// ---------------------------------------------------------------------------

// resolveTaskLogArchive 收集待打包文件并做上限校验。
// 返回 false 时响应已经写好，调用方直接 return 即可。
func resolveTaskLogArchive(c *gin.Context, taskID uint) ([]archiveEntry, int64, bool) {
	entries, totalBytes := collectTaskLogArchiveEntries(taskID, c.Query("start"), c.Query("end"))

	if len(entries) == 0 {
		response.NotFound(c, "该任务暂无日志文件")
		return nil, 0, false
	}
	if len(entries) > maxArchiveFiles || totalBytes > maxArchiveBytes {
		// 文案必须给出路：用户看到「超限」之后要知道下一步点哪里，
		// 前端的「最近 7 天 / 最近 30 天」就是为这句话准备的入口。
		response.BadRequest(c, fmt.Sprintf(
			"本次共 %d 个文件 / %s，超过打包上限（最多 %d 个文件 / %s），请改用「最近 7 天」或「最近 30 天」后重试",
			len(entries), humanizeArchiveBytes(totalBytes), maxArchiveFiles, humanizeArchiveBytes(maxArchiveBytes)))
		return nil, 0, false
	}

	return entries, totalBytes, true
}

// collectTaskLogArchiveEntries 收集一个任务在 [start, end] 内的全部日志文件。
//
// 数据源直接复用列表接口那份 service.ListLogFiles，不另写一套目录遍历 ——
// 这样才能保证「下载到的 = 列表里看到的」，两边的口径不会各自漂移。
func collectTaskLogArchiveEntries(taskID uint, startRaw, endRaw string) ([]archiveEntry, int64) {
	// parseQueryTime（handler/log.go）会把未 percent-encode 的 `+08:00` 被解成空格
	// 这个经典坑兜住；解析不出来一律当作「没传这个条件」，与 GET /logs 的口径一致。
	start, hasStart := parseQueryTime(startRaw)
	end, hasEnd := parseQueryTime(endRaw)

	entries := make([]archiveEntry, 0)
	var totalBytes int64

	for _, file := range service.ListLogFiles(taskID, config.C.Data.LogDir) {
		// CreatedAt 就是磁盘 ModTime（service.ListLogFiles 里格式化的），
		// 与列表「时间」列同一字段 —— 筛选口径必须跟用户看到的东西一致。
		modTime, modErr := time.Parse(time.RFC3339, file.CreatedAt)
		if hasStart || hasEnd {
			if modErr != nil {
				continue
			}
			if hasStart && modTime.Before(start) {
				continue
			}
			if hasEnd && modTime.After(end) {
				continue
			}
		}
		if modErr != nil {
			modTime = time.Now()
		}

		// statRawLogFileWithinLogDir 就是单文件下载那条路已有的路径穿越防护
		// （pathutil.ResolveWithinBase 会在展开软链之后再确认目标仍在日志根目录内），
		// 一行都不重写；顺带把「列举之后已被日志清理删掉」的文件过滤掉。
		absPath, size, err := statRawLogFileWithinLogDir(file.Path)
		if err != nil {
			continue
		}

		entries = append(entries, archiveEntry{
			relPath: file.Path,
			absPath: absPath,
			modTime: modTime,
		})
		totalBytes += size
	}

	return entries, totalBytes
}

// ---------------------------------------------------------------------------
// 文件名与体积文案
// ---------------------------------------------------------------------------

// taskLogArchiveDownloadName 生成 `<任务名>-logs-<YYYYMMDD>.zip`。
// 中文任务名原样保留，由 attachmentContentDisposition 的 filename*=UTF-8'' 负责传输。
func taskLogArchiveDownloadName(taskID uint) string {
	name := ""
	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err == nil {
		name = strings.TrimSpace(task.Name)
	}
	if name == "" {
		name = fmt.Sprintf("task_%d", taskID)
	}
	return sanitizeDownloadFilename(fmt.Sprintf("%s-logs-%s.zip", name, time.Now().Format("20060102")))
}

func humanizeArchiveBytes(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
