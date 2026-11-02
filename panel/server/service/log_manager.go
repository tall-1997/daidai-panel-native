package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"daidai-panel/model"
	"daidai-panel/pkg/pathutil"
)

type LogStreamManager struct {
	mu        sync.Mutex
	streams   map[string]*os.File
	fileSizes map[string]int64
	maxSize   int64
}

var logStreamMgr = &LogStreamManager{
	streams:   make(map[string]*os.File),
	fileSizes: make(map[string]int64),
	maxSize:   10 * 1024 * 1024,
}

func GetLogStreamManager() *LogStreamManager {
	return logStreamMgr
}

func (m *LogStreamManager) Write(filePath, data string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, exists := m.streams[filePath]
	if !exists {
		dir := filepath.Dir(filePath)
		os.MkdirAll(dir, 0755)

		var err error
		f, err = os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		m.streams[filePath] = f
		m.fileSizes[filePath] = 0
	}

	if m.fileSizes[filePath] >= m.maxSize {
		return nil
	}

	n, err := f.WriteString(data)
	if err != nil {
		return err
	}
	// 【为什么这里不再 f.Sync()】
	// 原来每写一个输出片段就 fsync 一次，而这一整段是持着【全局】 m.mu 的：
	// 一个输出两万行的脚本 = 两万次串行 fsync 压在脚本输出的同步路径上，
	// 期间所有并发任务的日志写入都要排队等这把锁，慢盘上能把整个面板拖成龟速。
	//
	// 去掉它是安全的：WriteString 走的是 write(2)，数据写完就已经在内核页缓存里，
	// 面板进程自己 panic、被 kill、容器重启都不会丢，只有【整机断电】才可能丢尾巴 ——
	// 任务日志不值得为这个概率付上面那份代价。
	// 任务结束时 task_executor.go 的 defer 会调 CloseStream 把文件 Close 掉，
	// 而那个 defer 注册在结算 defer 之后、按 LIFO 先执行，所以前端收到 done 事件时
	// 文件已经完整关闭，之后再去读文件拿到的内容是全的。
	//
	// O_APPEND 与「超限写一次可见标记后停写」的语义都保持原样，只去掉 fsync。
	m.fileSizes[filePath] += int64(n)

	if m.fileSizes[filePath] >= m.maxSize {
		f.WriteString("\n[日志文件已达到大小限制，停止写入]")
	}

	return nil
}

func (m *LogStreamManager) CloseStream(filePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if f, ok := m.streams[filePath]; ok {
		f.Close()
		delete(m.streams, filePath)
		delete(m.fileSizes, filePath)
	}
}

func (m *LogStreamManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, f := range m.streams {
		f.Close()
	}
	m.streams = make(map[string]*os.File)
	m.fileSizes = make(map[string]int64)
}

func GetLogPath(taskID uint, logDir string) string {
	ts := time.Now().Format("2006-01-02-15-04-05-000")
	dir := filepath.Join(logDir, fmt.Sprintf("task_%d", taskID))
	return filepath.Join(dir, ts+".log")
}

func GetRelativeLogPath(taskID uint) string {
	ts := time.Now().Format("2006-01-02-15-04-05-000")
	return fmt.Sprintf("task_%d/%s.log", taskID, ts)
}

func GetRelativeLogPathForTask(task *model.Task) string {
	if task == nil {
		return GetRelativeLogPath(0)
	}

	ts := time.Now().Format("2006-01-02-15-04-05-000")
	return filepath.ToSlash(filepath.Join(getTaskLogDirName(task), ts+".log"))
}

func ReadLogFile(logPath, logDir string) (string, error) {
	fullPath := logPath
	if !filepath.IsAbs(logPath) {
		fullPath = filepath.Join(logDir, logPath)
	}

	absPath, err := pathutil.ResolveWithinBase(logDir, fullPath, true)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", fmt.Errorf("检测到路径遍历攻击")
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type LogFileInfo struct {
	Filename  string `json:"filename"`
	Path      string `json:"path"`
	LogID     uint   `json:"log_id"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

func ListLogFiles(taskID uint, logDir string) []LogFileInfo {
	files := make([]LogFileInfo, 0)
	for _, taskDir := range listTaskLogDirs(taskID, logDir) {
		entries, err := os.ReadDir(filepath.Join(logDir, taskDir))
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			relPath := filepath.ToSlash(filepath.Join(taskDir, entry.Name()))
			files = append(files, LogFileInfo{
				Filename:  entry.Name(),
				Path:      relPath,
				Size:      info.Size(),
				CreatedAt: info.ModTime().Format(time.RFC3339),
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].CreatedAt == files[j].CreatedAt {
			return files[i].Path > files[j].Path
		}
		return files[i].CreatedAt > files[j].CreatedAt
	})

	return files
}

func ResolveTaskLogPath(taskID uint, filenameOrPath, logDir string) (string, error) {
	name := strings.TrimSpace(filenameOrPath)
	if name == "" || filepath.IsAbs(name) {
		return "", os.ErrNotExist
	}

	normalized := filepath.ToSlash(filepath.Clean(name))
	if strings.Contains(normalized, "/") {
		dir := strings.Split(normalized, "/")[0]
		if !isTaskLogDirForTask(dir, taskID) {
			return "", os.ErrNotExist
		}
		if _, err := pathutil.ResolveWithinBase(logDir, filepath.Join(logDir, normalized), true); err != nil {
			return "", err
		}
		return normalized, nil
	}

	for _, taskDir := range listTaskLogDirs(taskID, logDir) {
		relPath := filepath.ToSlash(filepath.Join(taskDir, normalized))
		if _, err := pathutil.ResolveWithinBase(logDir, filepath.Join(logDir, relPath), true); err == nil {
			return relPath, nil
		}
	}

	return "", os.ErrNotExist
}

func DeleteLogFile(logPath, logDir string) error {
	fullPath := logPath
	if !filepath.IsAbs(logPath) {
		fullPath = filepath.Join(logDir, logPath)
	}

	absPath, err := pathutil.ResolveWithinBase(logDir, fullPath, true)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return fmt.Errorf("检测到路径遍历攻击")
	}

	return os.Remove(absPath)
}

// IsStreamOpen 报告该路径是否仍被某次执行占用（正在写）。
//
// 【为什么清理日志前必须问这一句】
// Linux 上 os.Remove 一个已经 open 的文件不会报错，后续 WriteString 会全部写进被 unlink 的
// inode，任务跑完日志凭空消失、还一句报错都没有；Windows 上则是 Remove 直接失败。
// 两种都是坏结果，所以正在写的日志一律跳过，留给下一轮清理收（issue #144 / v3.3.2）。
//
// 入参必须是「logDir 和 log_path 拼出来的那个路径」：写入方（task_executor.go:775 与
// scheduler.go:291）就是用 filepath.Join(logDir, relPath) 当 map key 的。这里刻意不做
// EvalSymlinks，否则算出来的 key 跟写入方对不上，判定永远返回 false，这道保护就等于没有。
func (m *LogStreamManager) IsStreamOpen(filePath string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, ok := m.streams[filePath]
	return ok
}

// DeleteLogFilesForRecords 按 task_logs.log_path 批量删磁盘日志文件，返回实删数量。
//
// 调用方的顺序固定是「先 Pluck log_path → 删 DB 行 → 调这里删文件」：行一删，
// 路径就再也查不回来了，所以必须先把 log_path 捞出来（issue #144 / v3.3.2）。
//
// 三类会被跳过：
//  1. 空路径 —— 老记录可能压根没有 log_path（正文当年是直接存在 content 列里的）；
//  2. 仍被 LogStreamManager 占用的文件 —— 理由见 IsStreamOpen；
//  3. 解析后跑出 logDir 的路径 —— log_path 平时由机器生成，但 backup_runtime.go 的
//     restoreTaskLogs 会把备份包里的 LogPath 原样写回库，而备份包是外部输入，
//     所以 ResolveWithinBase 这道防线是真需要的，不是走形式。
//
// 文件已经不在了当成功忽略、继续处理下一条：本 issue 用户的处境正是「行和文件对不上」，
// 因为一条对不上就把整批中断没有任何好处。
// 末尾顺带清掉被删空的 task_ 目录，四个删除入口都不用各自再补一次。
func DeleteLogFilesForRecords(logPaths []string, logDir string) int {
	if strings.TrimSpace(logDir) == "" || len(logPaths) == 0 {
		return 0
	}

	mgr := GetLogStreamManager()
	count := 0
	for _, logPath := range logPaths {
		logPath = strings.TrimSpace(logPath)
		if logPath == "" {
			continue
		}

		fullPath := logPath
		if !filepath.IsAbs(logPath) {
			fullPath = filepath.Join(logDir, logPath)
		}
		if mgr.IsStreamOpen(fullPath) {
			continue
		}

		absPath, err := pathutil.ResolveWithinBase(logDir, fullPath, true)
		if err != nil {
			// 不存在（os.IsNotExist）本来就是想要的结果；路径穿越等其余情况一律跳过，
			// 不往上抛错——这是尽力而为的清理，不该让整个删除请求失败。
			continue
		}
		if os.Remove(absPath) == nil {
			count++
		}
	}

	if count > 0 {
		removeEmptyTaskLogDirs(logDir)
	}

	return count
}

// RemoveTaskLogDirs 删掉某个任务名下的全部日志目录（连同目录里的文件）。
// 删任务时调用：任务都没了，留着 task_<ID>_* 只会变成永远没人认领的垃圾
// （issue #144 / v3.3.2）。
//
// 复用 listTaskLogDirs，它能把 task_<ID> 与改过名字留下的 task_<ID>_* 一起收拢，
// 所以同一个任务改过几次名也能清干净。
func RemoveTaskLogDirs(taskID uint, logDir string) {
	if strings.TrimSpace(logDir) == "" {
		return
	}

	mgr := GetLogStreamManager()
	for _, dirName := range listTaskLogDirs(taskID, logDir) {
		absDir, err := pathutil.ResolveWithinBase(logDir, filepath.Join(logDir, dirName), true)
		if err != nil {
			continue
		}

		// 目录里只要还有一个正在写的日志文件，整个目录就先放着不动，理由同 IsStreamOpen。
		// 「任务正在跑的时候被删掉」很罕见，真遇上了残留目录也会被后续按天数的自动清理收走，
		// 比静默把正在跑的那次执行的输出抽掉要好。
		busy := false
		entries, _ := os.ReadDir(absDir)
		for _, entry := range entries {
			if !entry.IsDir() && mgr.IsStreamOpen(filepath.Join(logDir, dirName, entry.Name())) {
				busy = true
				break
			}
		}
		if busy {
			continue
		}

		os.RemoveAll(absDir)
	}
}

// removeEmptyTaskLogDirs 清掉已经空掉的 task_ 一级子目录。
// 原来这段只长在 CleanOldLogs 里，issue #144 之后 DeleteLogFilesForRecords 也要用，
// 所以抽出来共用（v3.3.2）。只删完全为空的目录，不存在误删还有日志的任务。
func removeEmptyTaskLogDirs(logDir string) {
	entries, _ := os.ReadDir(logDir)
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "task_") {
			taskDir := filepath.Join(logDir, entry.Name())
			subEntries, _ := os.ReadDir(taskDir)
			if len(subEntries) == 0 {
				os.Remove(taskDir)
			}
		}
	}
}

// CleanOldLogs 按 ModTime 扫盘删过期 .log 文件，只管文件、不碰 DB 行。
//
// 🔴 语义刻意保持不变：server/cmd/ddp/commands.go 的 `ddp clean-logs` 这条 CLI，
// 以及 README.md 与 cmd/ddp/help.go 里的说明，讲的都是「清理任务日志文件」。
// 要删记录请走 CleanLogsOlderThan，别改这里（issue #144 / v3.3.2）。
//
// 它也没法被按 log_path 删文件取代：DB 行早就被清掉、文件还留在磁盘上的存量垃圾
// （正是本 issue 用户的处境）只有靠扫盘才找得到。
func CleanOldLogs(logDir string, days int) int {
	cutoff := time.Now().AddDate(0, 0, -days)
	count := 0

	filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".log") {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(path) == nil {
				count++
			}
		}
		return nil
	})

	removeEmptyTaskLogDirs(logDir)

	return count
}

func getTaskLogDirName(task *model.Task) string {
	return formatTaskLogDirName(task.ID, resolveTaskLogDirLabel(task))
}

func formatTaskLogDirName(taskID uint, label string) string {
	base := fmt.Sprintf("task_%d", taskID)
	label = sanitizeTaskLogDirLabel(label)
	if label == "" {
		return base
	}
	return base + "_" + label
}

func resolveTaskLogDirLabel(task *model.Task) string {
	for _, candidate := range []string{
		task.Name,
		filepath.Base(extractTaskScriptPath(task.Command)),
	} {
		label := sanitizeTaskLogDirLabel(candidate)
		if label != "" {
			return label
		}
	}
	return "task"
}

func sanitizeTaskLogDirLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	var b strings.Builder
	lastWasSep := false
	runeCount := 0
	for _, r := range value {
		if runeCount >= 48 {
			break
		}
		if isTaskLogDirUnsafeRune(r) {
			if !lastWasSep && b.Len() > 0 {
				b.WriteByte('_')
				lastWasSep = true
			}
			continue
		}
		b.WriteRune(r)
		lastWasSep = false
		runeCount++
	}

	return strings.Trim(strings.TrimSpace(b.String()), "._-")
}

func isTaskLogDirUnsafeRune(r rune) bool {
	switch r {
	case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
		return true
	default:
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}
}

func listTaskLogDirs(taskID uint, logDir string) []string {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return []string{}
	}

	dirs := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() && isTaskLogDirForTask(entry.Name(), taskID) {
			dirs = append(dirs, entry.Name())
		}
	}

	sort.SliceStable(dirs, func(i, j int) bool {
		if dirs[i] == fmt.Sprintf("task_%d", taskID) {
			return false
		}
		if dirs[j] == fmt.Sprintf("task_%d", taskID) {
			return true
		}
		return dirs[i] > dirs[j]
	})

	return dirs
}

func isTaskLogDirForTask(dirName string, taskID uint) bool {
	base := fmt.Sprintf("task_%d", taskID)
	return dirName == base || strings.HasPrefix(dirName, base+"_")
}
