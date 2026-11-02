package service

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sync"
	"unicode/utf8"
)

type TinyLog struct {
	LogID       string
	file        *os.File
	writer      *bufio.Writer
	subscribers []chan []byte
	subLock     sync.RWMutex
	remainder   []byte
	closed      bool
	mu          sync.Mutex
}

func NewTinyLog(logID string) (*TinyLog, error) {
	tmpFile, err := os.CreateTemp("", "daidai-log-"+logID+"-*.log")
	if err != nil {
		return nil, err
	}

	return &TinyLog{
		LogID:       logID,
		file:        tmpFile,
		writer:      bufio.NewWriter(tmpFile),
		subscribers: make([]chan []byte, 0),
		remainder:   make([]byte, 0),
	}, nil
}

func (l *TinyLog) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return 0, io.ErrClosedPipe
	}

	// 【必须用新的底层数组拼，不能写成 append(l.remainder, p...)】
	// 那种写法在 l.remainder 还有富余容量时会【原地】追加，于是 data 与 l.remainder
	// 共用同一块底层数组。接着下面 UTF-8 收尾那段做的是
	// `l.remainder = l.remainder[:0]` 再 `append(l.remainder, data[i:]...)`，
	// 等于把切下来的尾巴拷回同一块数组的开头，把 data 的头部字节直接覆写掉 ——
	// 最终 l.writer.Write(data) 写出去的是被污染的内容（日志前几个字节变成上一段的尾巴）。
	// 只在「上一次留了不完整 UTF-8 尾巴」时才触发，所以平时看不出来，一旦触发就是内容错乱。
	// 这里先分配一块新数组再拼，data 与 l.remainder 从此互不相干，对外行为完全不变。
	data := make([]byte, 0, len(l.remainder)+len(p))
	data = append(data, l.remainder...)
	data = append(data, p...)
	l.remainder = l.remainder[:0]

	if len(data) > 0 && !utf8.Valid(data) {
		for i := len(data) - 1; i >= 0 && i >= len(data)-4; i-- {
			if utf8.RuneStart(data[i]) {
				if !utf8.Valid(data[i:]) {
					l.remainder = append(l.remainder, data[i:]...)
					data = data[:i]
					break
				}
			}
		}
	}

	if len(data) > 0 {
		if _, err := l.writer.Write(data); err != nil {
			return 0, err
		}

		l.broadcast(data)
	}

	return len(p), nil
}

// broadcast 把新写入的数据推给所有实时订阅者（SSE 连接）。
//
// 【这里的 default 分支是有意丢弃，不是 bug】
// 订阅通道有 100 的缓冲，慢消费者（网络差、浏览器标签在后台）撑满之后就丢。
// 换成阻塞发送会更糟：一个卡住的 SSE 连接会把 Write 卡住，而 Write 在脚本输出的
// 同步路径上 —— 等于让一个慢浏览器把整个任务执行拖死。
//
// 丢的只是【运行中那一瞬间的实时观感】，不影响最终结果：
//   - 落盘是同步的（上面 l.writer 那条路径），文件里一个字节都不少；
//   - 任务结束时 Close() 把完整日志压缩存进 task_logs；
//   - 前端在收到 SSE 的 done 事件后会 fetchLatestLog 重新拉一次完整日志
//     （web/src/views/tasks/components/LogViewer.vue 的 onEvent），把实时流覆盖掉。
// 所以这条路径是「最终一致」的。
//
// ⚠️ 注意与 issue #102 区分：那个 bug 是【源头就没读到数据】
// （cmd.Wait() 抢在读协程前关闭了管道，见 script_runner.go 的 pumpAndWait），
// 丢的内容既不在文件里也不在数据库里，重新拉也补不回来。
// 如果又有人报「日志显示不全」，先确认是「刷新后还缺」（源头丢，是真 bug）
// 还是「只有运行中缺、结束后完整」（这里丢，符合预期）。
func (l *TinyLog) broadcast(data []byte) {
	l.subLock.RLock()
	defer l.subLock.RUnlock()

	for _, ch := range l.subscribers {
		select {
		case ch <- data:
		default:
		}
	}
}

func (l *TinyLog) Subscribe() chan []byte {
	l.subLock.Lock()
	defer l.subLock.Unlock()

	ch := make(chan []byte, 100)
	l.subscribers = append(l.subscribers, ch)
	return ch
}

func (l *TinyLog) Unsubscribe(ch chan []byte) {
	l.subLock.Lock()
	defer l.subLock.Unlock()

	for i, sub := range l.subscribers {
		if sub == ch {
			l.subscribers = append(l.subscribers[:i], l.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

func (l *TinyLog) ReadAll() ([]byte, error) {
	l.mu.Lock()
	l.writer.Flush()
	l.mu.Unlock()

	return os.ReadFile(l.file.Name())
}

func (l *TinyLog) ReadLastLines(n int) ([]byte, error) {
	l.mu.Lock()
	l.writer.Flush()
	l.mu.Unlock()

	file, err := os.Open(l.file.Name())
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}

	size := stat.Size()
	if size == 0 {
		return []byte{}, nil
	}

	bufSize := int64(4096)
	if size < bufSize {
		bufSize = size
	}

	buf := make([]byte, bufSize)
	_, err = file.ReadAt(buf, size-bufSize)
	if err != nil && err != io.EOF {
		return nil, err
	}

	lines := bytes.Split(buf, []byte("\n"))
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return bytes.Join(lines, []byte("\n")), nil
}

func (l *TinyLog) Close() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.closed {
		return "", nil
	}

	l.closed = true

	if len(l.remainder) > 0 {
		l.writer.Write(l.remainder)
	}
	l.writer.Flush()

	l.subLock.Lock()
	for _, ch := range l.subscribers {
		close(ch)
	}
	l.subscribers = nil
	l.subLock.Unlock()

	content, err := os.ReadFile(l.file.Name())
	if err != nil {
		return "", err
	}

	l.file.Close()
	os.Remove(l.file.Name())

	compressed := compressToBase64(content)
	return compressed, nil
}

func compressToBase64(data []byte) string {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func DecompressFromBase64(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}

	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	defer r.Close()

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), nil
}

type TinyLogManager struct {
	logs map[string]*TinyLog
	mu   sync.RWMutex
}

var tinyLogManager = &TinyLogManager{
	logs: make(map[string]*TinyLog),
}

func GetTinyLogManager() *TinyLogManager {
	return tinyLogManager
}

func (m *TinyLogManager) Create(logID string) (*TinyLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if log, exists := m.logs[logID]; exists {
		return log, nil
	}

	log, err := NewTinyLog(logID)
	if err != nil {
		return nil, err
	}

	m.logs[logID] = log
	return log, nil
}

func (m *TinyLogManager) Get(logID string) *TinyLog {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.logs[logID]
}

func (m *TinyLogManager) Remove(logID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.logs, logID)
}

func (m *TinyLogManager) FindByTaskID(taskID uint) *TinyLog {
	m.mu.RLock()
	defer m.mu.RUnlock()

	prefix := fmt.Sprintf("%d_", taskID)
	for id, tl := range m.logs {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			return tl
		}
	}
	return nil
}
