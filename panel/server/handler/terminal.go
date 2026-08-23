package handler

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"daidai-panel/middleware"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	terminalMaxRunning  = 3
	terminalMaxInput    = 64 << 10
	terminalMaxOutput   = 1 << 20
	terminalMaxRetained = 20
)

type terminalChunk struct {
	Cursor int64  `json:"cursor"`
	Data   string `json:"data"`
	Format string `json:"encoding"`
	size   int
}

type terminalSession struct {
	mu        sync.Mutex
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Shell     string    `json:"shell"`
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
	ExitCode  *int      `json:"exit_code"`
	cmd       *exec.Cmd
	ptmx      *os.File
	chunks    []terminalChunk
	bytes     int
	cursor    int64
	closed    bool
}

type terminalRegistry struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
}

var androidTerminals = terminalRegistry{sessions: make(map[string]*terminalSession)}

type TerminalHandler struct{}

func NewTerminalHandler() *TerminalHandler { return &TerminalHandler{} }

func (h *TerminalHandler) RegisterRoutes(r *gin.RouterGroup) {
	group := r.Group("/terminal", middleware.JWTAuth(), middleware.RequireRole("operator"))
	group.POST("/sessions", h.Create)
	group.GET("/sessions/:id", h.Get)
	group.POST("/sessions/:id/input", h.Input)
	group.PUT("/sessions/:id/resize", h.Resize)
	group.PUT("/sessions/:id/stop", h.Stop)
	group.DELETE("/sessions/:id", h.Delete)
}

func (h *TerminalHandler) Create(c *gin.Context) {
	var request struct {
		Rows    uint16 `json:"rows"`
		Columns uint16 `json:"columns"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid terminal request"})
		return
	}
	request.Rows = clampTerminalSize(request.Rows, 24, 2, 200)
	request.Columns = clampTerminalSize(request.Columns, 80, 10, 400)
	session, err := androidTerminals.create(request.Rows, request.Columns)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": terminalSnapshot(session, 0)})
}

func (h *TerminalHandler) Get(c *gin.Context) {
	session := androidTerminals.get(c.Param("id"))
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "terminal session not found"})
		return
	}
	after, _ := strconv.ParseInt(c.Query("cursor"), 10, 64)
	c.JSON(http.StatusOK, gin.H{"data": terminalSnapshot(session, after)})
}

func (h *TerminalHandler) Input(c *gin.Context) {
	session := androidTerminals.get(c.Param("id"))
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "terminal session not found"})
		return
	}
	var request struct{ Data, Encoding string }
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid terminal input"})
		return
	}
	data := []byte(request.Data)
	if request.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(request.Data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid base64 terminal input"})
			return
		}
		data = decoded
	}
	if len(data) > terminalMaxInput {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "terminal input exceeds limit"})
		return
	}
	session.mu.Lock()
	if session.closed || session.Status != "running" {
		session.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "terminal session has ended"})
		return
	}
	ptmx := session.ptmx
	session.mu.Unlock()
	if _, err := ptmx.Write(data); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "terminal input failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "accepted"})
}

func (h *TerminalHandler) Resize(c *gin.Context) {
	session := androidTerminals.get(c.Param("id"))
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "terminal session not found"})
		return
	}
	var request struct{ Rows, Columns uint16 }
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid terminal size"})
		return
	}
	request.Rows = clampTerminalSize(request.Rows, 24, 2, 200)
	request.Columns = clampTerminalSize(request.Columns, 80, 10, 400)
	session.mu.Lock()
	err := resizeAndroidPTY(session.ptmx, request.Rows, request.Columns)
	session.mu.Unlock()
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "terminal resize failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "resized"})
}

func (h *TerminalHandler) Stop(c *gin.Context) {
	session := androidTerminals.get(c.Param("id"))
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "terminal session not found"})
		return
	}
	stopTerminal(session)
	c.JSON(http.StatusOK, gin.H{"data": terminalSnapshot(session, 0)})
}

func (h *TerminalHandler) Delete(c *gin.Context) {
	session := androidTerminals.remove(c.Param("id"))
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "terminal session not found"})
		return
	}
	stopTerminal(session)
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (registry *terminalRegistry) create(rows, columns uint16) (*terminalSession, error) {
	proot := strings.TrimSpace(os.Getenv("DAIDAI_PROOT_PATH"))
	prootLoader := strings.TrimSpace(os.Getenv("DAIDAI_PROOT_LOADER_PATH"))
	rootfs := strings.TrimSpace(os.Getenv("DAIDAI_LINUX_ROOTFS_DIR"))
	files := strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_FILES_DIR"))
	cache := strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_CACHE_DIR"))
	if !regularFile(proot) || !regularFile(prootLoader) || !directory(rootfs) || !regularFile(filepath.Join(rootfs, "bin/bash")) || !directory(files) || !directory(cache) {
		return nil, errors.New("ROOTFS_TERMINAL_UNAVAILABLE")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	running := 0
	for _, session := range registry.sessions {
		session.mu.Lock()
		if session.Status == "running" {
			running++
		}
		session.mu.Unlock()
	}
	if running >= terminalMaxRunning {
		return nil, errors.New("TERMINAL_SESSION_LIMIT")
	}
	args := []string{"--link2symlink", "--kill-on-exit", "-k", "4.14.0", "-r", rootfs, "-w", "/host-files", "-b", files + ":/host-files", "-b", cache + ":/tmp/host-cache", "-0"}
	for _, path := range []string{"/proc", "/dev", "/sys", "/sdcard", "/storage"} {
		if _, err := os.Stat(path); err == nil {
			args = append(args, "-b", path+":"+path)
		}
	}
	args = append(args, "/bin/bash", "-l")
	cmd := exec.Command(proot, args...)
	cmd.Dir = files
	cmd.Env = androidTerminalEnvironment(os.Environ(), files, cache, prootLoader, strings.TrimSpace(os.Getenv("DAIDAI_ANDROID_NATIVE_LIB_DIR")))
	ptmx, err := startAndroidPTY(cmd, rows, columns)
	if err != nil {
		return nil, err
	}
	session := &terminalSession{ID: uuid.NewString(), Status: "running", Shell: "/bin/bash", PID: cmd.Process.Pid, CreatedAt: time.Now().UTC(), cmd: cmd, ptmx: ptmx}
	registry.sessions[session.ID] = session
	go readTerminal(session)
	registry.trimLocked()
	return session, nil
}

func androidTerminalEnvironment(base []string, files, cache, prootLoader, nativeDir string) []string {
	environment := append([]string{}, base...)
	environment = append(environment, "HOME="+files, "TERM=xterm-256color", "PROOT_NO_SECCOMP=1", "PROOT_TMP_DIR="+cache, "PROOT_LOADER="+prootLoader)
	if nativeDir != "" {
		environment = append(environment, "LD_LIBRARY_PATH="+nativeDir)
	}
	return environment
}

func (registry *terminalRegistry) get(id string) *terminalSession {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return registry.sessions[id]
}

func (registry *terminalRegistry) remove(id string) *terminalSession {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	session := registry.sessions[id]
	delete(registry.sessions, id)
	return session
}

func (registry *terminalRegistry) trimLocked() {
	if len(registry.sessions) <= terminalMaxRetained {
		return
	}
	for id, session := range registry.sessions {
		session.mu.Lock()
		finished := session.Status != "running"
		session.mu.Unlock()
		if finished {
			delete(registry.sessions, id)
		}
		if len(registry.sessions) <= terminalMaxRetained {
			return
		}
	}
}

func readTerminal(session *terminalSession) {
	buffer := make([]byte, 8192)
	for {
		count, err := session.ptmx.Read(buffer)
		if count > 0 {
			encoded := base64.StdEncoding.EncodeToString(buffer[:count])
			session.mu.Lock()
			session.cursor++
			session.chunks = append(session.chunks, terminalChunk{Cursor: session.cursor, Data: encoded, Format: "base64", size: count})
			session.bytes += count
			for session.bytes > terminalMaxOutput && len(session.chunks) > 0 {
				session.bytes -= session.chunks[0].size
				session.chunks = session.chunks[1:]
			}
			session.mu.Unlock()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) { /* terminal exit may return EIO */
			}
			break
		}
	}
	finishTerminal(session)
}

func finishTerminal(session *terminalSession) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.closed = true
	session.mu.Unlock()
	err := session.cmd.Wait()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			code = 1
		}
	}
	session.mu.Lock()
	session.ExitCode = &code
	if session.Status == "running" {
		session.Status = "exited"
	}
	_ = session.ptmx.Close()
	session.mu.Unlock()
}

func stopTerminal(session *terminalSession) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.Status = "stopped"
	process := session.cmd.Process
	_ = session.ptmx.Close()
	session.mu.Unlock()
	if process != nil {
		_ = syscall.Kill(-process.Pid, syscall.SIGTERM)
		_ = process.Signal(syscall.SIGTERM)
		time.AfterFunc(500*time.Millisecond, func() {
			session.mu.Lock()
			stillRunning := session.ExitCode == nil
			session.mu.Unlock()
			if stillRunning {
				_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
				_ = process.Kill()
			}
		})
	}
}

func terminalSnapshot(session *terminalSession, after int64) gin.H {
	session.mu.Lock()
	defer session.mu.Unlock()
	chunks := make([]terminalChunk, 0)
	for _, chunk := range session.chunks {
		if chunk.Cursor > after {
			chunks = append(chunks, chunk)
		}
	}
	return gin.H{"id": session.ID, "status": session.Status, "shell": session.Shell, "pid": session.PID, "created_at": session.CreatedAt, "exit_code": session.ExitCode, "cursor": session.cursor, "output": chunks}
}

func CloseTerminalSessions() {
	androidTerminals.mu.Lock()
	sessions := make([]*terminalSession, 0, len(androidTerminals.sessions))
	for _, session := range androidTerminals.sessions {
		sessions = append(sessions, session)
	}
	androidTerminals.sessions = make(map[string]*terminalSession)
	androidTerminals.mu.Unlock()
	for _, session := range sessions {
		stopTerminal(session)
	}
}

func clampTerminalSize(value, fallback, minimum, maximum uint16) uint16 {
	if value == 0 {
		return fallback
	}
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
func directory(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
