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

	data := append(l.remainder, p...)
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
