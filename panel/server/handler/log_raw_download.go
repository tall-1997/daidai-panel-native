package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/dlticket"
	"daidai-panel/pkg/pathutil"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

// 「下载原始日志」= 服务端把磁盘上的日志文件原样流式吐给浏览器。
//
// 面板里三处日志展示（任务日志 / 执行日志详情 / 日志文件预览）都按终端语义折叠了裸 \r，
// 复制和前端「下载」拿到的都是折叠后的文本。需要按字节比对磁盘文件、或者排查脚本吐出的
// 终端控制序列时，就必须有一条能拿到原始字节的路径，这里提供的就是它。
//
// 鉴权走两步（见 pkg/dlticket 的包注释）：
//  1. `GET .../raw-ticket` 走和其它日志接口完全一致的 JWTAuth + RequireRole("viewer")，
//     校验通过并定位到文件后签发一张短期票据，返回可直接用于浏览器原生下载的 URL；
//  2. `GET .../raw?ticket=...` 只认票据。浏览器原生下载带不了 Authorization 头，
//     所以这条路由不能挂 JWTAuth；票据绑定了具体资源且很快过期，等价于一次性授权。
const (
	// rawLogTicketTTL 只需要覆盖「前端拿到票据 → 浏览器发起下载」这一瞬间，外加浏览器
	// 断线重试 / Range 续传的余量。传输本身多久都不受影响：票据只在请求开始时校验一次。
	// 放长的代价是票据会更久地留在浏览器历史和反代访问日志里，所以取 2 分钟这个折中值。
	rawLogTicketTTL     = 120 * time.Second
	rawTicketPathSuffix = "-ticket"
)

var errRawLogFileMissing = errors.New("原始日志文件不存在或已被清理")

// rawLogTarget 描述一个已经通过路径校验、确认存在的磁盘日志文件。
type rawLogTarget struct {
	absPath      string
	downloadName string
	size         int64
}

// ---------------------------------------------------------------------------
// 执行日志详情：GET /logs/:id/raw-ticket → GET /logs/:id/raw
// ---------------------------------------------------------------------------

// RawDownloadTicket 为一条执行日志记录签发原始日志下载票据。
func (h *LogHandler) RawDownloadTicket(c *gin.Context) {
	logID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的日志ID")
		return
	}

	target, err := resolveTaskLogRecordRawFile(uint(logID))
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	issueRawLogTicket(c, taskLogRecordResource(logID), target, nil)
}

// DownloadRawLog 流式下载一条执行日志记录对应的磁盘原始日志文件。
// 只接受 RawDownloadTicket 签发的票据，没有票据一律 401。
func (h *LogHandler) DownloadRawLog(c *gin.Context) {
	logID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的日志ID")
		return
	}

	// 先验票再查库：避免未授权调用方靠 404 / 401 的差异探测某条日志是否存在。
	if !verifyRawLogTicket(c, taskLogRecordResource(logID)) {
		return
	}

	target, err := resolveTaskLogRecordRawFile(uint(logID))
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	streamRawLogFile(c, target)
}

// ---------------------------------------------------------------------------
// 日志文件预览：GET /tasks/:id/log-files/:filename/raw-ticket → .../raw
// ---------------------------------------------------------------------------

// RawLogFileDownloadTicket 为某个任务日志文件签发原始下载票据。
func (h *TaskHandler) RawLogFileDownloadTicket(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的任务ID")
		return
	}
	filenameOrPath := rawLogFileLocator(c)

	target, err := resolveTaskLogFileRawFile(uint(taskID), filenameOrPath)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	// 下载 URL 必须让 rawLogFileLocator 再算出同一个定位参数，否则资源标识对不上、验签必失败。
	// 定位参数来自查询串时原样带回；等于路径参数时可以省略，下载接口会自动回落到 :filename。
	extra := url.Values{}
	if filenameOrPath != c.Param("filename") {
		extra.Set("path", filenameOrPath)
	}
	issueRawLogTicket(c, taskLogFileResource(taskID, filenameOrPath), target, extra)
}

// DownloadRawLogFile 流式下载某个任务日志文件的原始内容。
// 只接受 RawLogFileDownloadTicket 签发的票据，没有票据一律 401。
func (h *TaskHandler) DownloadRawLogFile(c *gin.Context) {
	taskID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		response.BadRequest(c, "无效的任务ID")
		return
	}
	filenameOrPath := rawLogFileLocator(c)

	// 票据绑定的是「原样的定位参数」，所以 ../ 之类的穿越尝试根本换不到票，
	// 到这里会直接因验签失败被拒；下面的 resolveTaskLogFileRawFile 再兜一层。
	if !verifyRawLogTicket(c, taskLogFileResource(taskID, filenameOrPath)) {
		return
	}

	target, err := resolveTaskLogFileRawFile(uint(taskID), filenameOrPath)
	if err != nil {
		response.NotFound(c, err.Error())
		return
	}

	streamRawLogFile(c, target)
}

// rawLogFileLocator 与现有 LogFileContent / DeleteLogFile / DownloadLogFile 保持同一套取值口径：
// 优先用 ?path=（日志目录内的相对路径），没有时退回路径参数里的文件名。
// 这里额外做了 TrimSpace 并把空 path 也当作缺省，保证签发端和下载端算出的定位参数完全一致。
func rawLogFileLocator(c *gin.Context) string {
	if queryPath := strings.TrimSpace(c.Query("path")); queryPath != "" {
		return queryPath
	}
	return c.Param("filename")
}

// ---------------------------------------------------------------------------
// 资源标识
// ---------------------------------------------------------------------------

// 资源标识是票据签名原文的一部分，同时也是「这张票只能下这一个文件」的依据。
// 注意用的是请求里的原始定位参数而不是解析后的路径：这样下载接口可以在碰磁盘之前先验签。

func taskLogRecordResource(logID uint64) string {
	return fmt.Sprintf("task-log-record:%d", logID)
}

func taskLogFileResource(taskID uint64, filenameOrPath string) string {
	return fmt.Sprintf("task-log-file:%d:%s", taskID, filenameOrPath)
}

// ---------------------------------------------------------------------------
// 文件定位（路径穿越防护集中在这里）
// ---------------------------------------------------------------------------

// resolveTaskLogRecordRawFile 定位一条执行日志记录对应的磁盘原始日志文件。
func resolveTaskLogRecordRawFile(logID uint) (*rawLogTarget, error) {
	var taskLog model.TaskLog
	if err := database.DB.Preload("Task").First(&taskLog, logID).Error; err != nil {
		return nil, errors.New("日志不存在")
	}
	if taskLog.LogPath == nil || strings.TrimSpace(*taskLog.LogPath) == "" {
		// 短日志会压缩后直接存在 task_logs.content 里，没有独立落盘文件，
		// 这种记录没有「原始日志文件」可下载，明确告诉前端而不是回 500。
		return nil, errors.New("该日志没有独立的原始日志文件（内容仅存于数据库）")
	}

	// LogPath 来自数据库而非请求，但历史数据/备份恢复都可能写进异常值，仍要过一遍校验。
	absPath, size, err := statRawLogFileWithinLogDir(*taskLog.LogPath)
	if err != nil {
		return nil, err
	}

	return &rawLogTarget{
		absPath:      absPath,
		downloadName: rawLogRecordDownloadName(&taskLog),
		size:         size,
	}, nil
}

// resolveTaskLogFileRawFile 定位某个任务的某个日志文件。
func resolveTaskLogFileRawFile(taskID uint, filenameOrPath string) (*rawLogTarget, error) {
	// 复用日志文件浏览 / 删除 / 下载共用的定位逻辑：它会拒绝绝对路径，
	// Clean 之后要求首段必须是本任务的 task_<id>[_label] 目录，再用
	// pathutil.ResolveWithinBase 确认解析（含软链）后的目标仍在日志根目录内。
	relPath, err := service.ResolveTaskLogPath(taskID, filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		return nil, errRawLogFileMissing
	}

	absPath, size, err := statRawLogFileWithinLogDir(relPath)
	if err != nil {
		return nil, err
	}

	return &rawLogTarget{
		absPath:      absPath,
		downloadName: path.Base(filepath.ToSlash(relPath)),
		size:         size,
	}, nil
}

// statRawLogFileWithinLogDir 把日志目录内的相对路径解析成绝对路径，并确认它是一个存在的普通文件。
// 所有原始日志下载入口都必须经过这里：pathutil.ResolveWithinBase 会在展开软链之后
// 再次确认目标仍位于 config.C.Data.LogDir 内，`../`、绝对路径、软链逃逸都会被拒绝。
func statRawLogFileWithinLogDir(logPath string) (string, int64, error) {
	logDir := strings.TrimSpace(config.C.Data.LogDir)
	if logDir == "" {
		return "", 0, errRawLogFileMissing
	}

	fullPath := logPath
	if !filepath.IsAbs(logPath) {
		fullPath = filepath.Join(logDir, logPath)
	}

	absPath, err := pathutil.ResolveWithinBase(logDir, fullPath, true)
	if err != nil {
		return "", 0, errRawLogFileMissing
	}

	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		return "", 0, errRawLogFileMissing
	}
	return absPath, info.Size(), nil
}

// ---------------------------------------------------------------------------
// 票据签发 / 校验
// ---------------------------------------------------------------------------

func issueRawLogTicket(c *gin.Context, resource string, target *rawLogTarget, extraQuery url.Values) {
	ticket, expiresAt, err := dlticket.Issue(config.C.JWT.Secret, resource, c.GetString("username"), rawLogTicketTTL)
	if err != nil {
		response.InternalError(c, "签发下载票据失败")
		return
	}

	query := url.Values{}
	for key, values := range extraQuery {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	query.Set("ticket", ticket)

	// 直接从当前请求路径推导下载地址（`/raw-ticket` → `/raw`），
	// 这样 /api 与 /api/v1 两套前缀都能自动对上，不用在这里硬编码前缀。
	downloadPath := strings.TrimSuffix(c.Request.URL.EscapedPath(), rawTicketPathSuffix)

	response.Success(c, gin.H{
		"url":        downloadPath + "?" + query.Encode(),
		"filename":   target.downloadName,
		"size":       target.size,
		"expires_at": expiresAt.Format(time.RFC3339),
		"expires_in": int(rawLogTicketTTL.Seconds()),
	})
}

// verifyRawLogTicket 校验 ?ticket= 是否为 resource 签发且未过期。
// 返回 false 时响应已经写好，调用方直接 return 即可。
func verifyRawLogTicket(c *gin.Context, resource string) bool {
	ticket := strings.TrimSpace(c.Query("ticket"))
	if ticket == "" {
		response.Unauthorized(c, "缺少下载票据")
		return false
	}

	if _, err := dlticket.Verify(config.C.JWT.Secret, ticket, resource); err != nil {
		if errors.Is(err, dlticket.ErrExpired) {
			response.Unauthorized(c, "下载票据已过期，请重新发起下载")
			return false
		}
		response.Unauthorized(c, "下载票据无效")
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// 流式响应
// ---------------------------------------------------------------------------

func streamRawLogFile(c *gin.Context, target *rawLogTarget) {
	file, err := os.Open(target.absPath)
	if err != nil {
		response.NotFound(c, errRawLogFileMissing.Error())
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		response.NotFound(c, errRawLogFileMissing.Error())
		return
	}

	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", attachmentContentDisposition(target.downloadName))

	// http.ServeContent 按 32KB 分块从 io.ReadSeeker 拷到响应，不会把整个文件读进内存
	// （日志文件可以到 10MB 上限，容器内存很紧张时全量读会很危险）。
	// modTime 传零值以关闭 Last-Modified / If-Modified-Since 协商，避免下载被 304 掉。
	http.ServeContent(c.Writer, c.Request, info.Name(), time.Time{}, file)
}

// ---------------------------------------------------------------------------
// 文件名
// ---------------------------------------------------------------------------

// rawLogRecordDownloadName 生成执行日志详情下载的文件名。
// 带 -raw 后缀，好和前端那份「折叠后」的下载（<任务名>-<日志ID>.log）区分开。
func rawLogRecordDownloadName(taskLog *model.TaskLog) string {
	name := ""
	if taskLog.Task != nil {
		name = strings.TrimSpace(taskLog.Task.Name)
	}
	if name == "" {
		name = fmt.Sprintf("task_%d", taskLog.TaskID)
	}
	return sanitizeDownloadFilename(fmt.Sprintf("%s-%d-raw.log", name, taskLog.ID))
}

// sanitizeDownloadFilename 去掉会破坏文件名的字符，但保留中文等非 ASCII 字符
// （它们由 Content-Disposition 的 filename*=UTF-8'' 负责传输）。
func sanitizeDownloadFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteByte('_')
		case strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		return "raw.log"
	}
	return result
}

// attachmentContentDisposition 生成兼容中文 / 特殊字符文件名的 Content-Disposition。
//
// 两个参数必须都给：
//   - filename="..."  纯 ASCII 兜底名，给只认 RFC 2616 quoted-string 的老客户端；
//     直接把 UTF-8 字节塞进 quoted-string 会让浏览器按 latin-1 解出乱码。
//   - filename*=UTF-8''...  RFC 5987 / RFC 6266 扩展参数，现代浏览器优先使用，
//     中文名不会乱码，也不会在遇到空格或逗号时被截断。
func attachmentContentDisposition(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "raw.log"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiFallbackFilename(filename),
		encodeRFC5987(filename),
	)
}

func asciiFallbackFilename(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		// 非 ASCII、双引号、反斜杠、控制字符都会破坏 quoted-string，统一换成下划线。
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	fallback := strings.TrimSpace(b.String())
	if strings.Trim(fallback, "_") == "" {
		return "raw.log"
	}
	return fallback
}

// encodeRFC5987 按 RFC 5987 的 attr-char 集合做百分号编码（逐字节，输入是 UTF-8）。
// 不能用 url.QueryEscape：它会把空格编成 '+'，也不能用 url.PathEscape：
// 它会放行 '$' ':' '@' 等不在 attr-char 里的字符。
func encodeRFC5987(value string) string {
	const safe = "!#$&+-.^_`|~"

	var b strings.Builder
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') ||
			strings.IndexByte(safe, ch) >= 0 {
			b.WriteByte(ch)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", ch)
	}
	return b.String()
}
