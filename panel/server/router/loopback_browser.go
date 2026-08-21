package router

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const localBrowserCookie = "daidai_local_session"

type LoopbackBrowserAccess struct {
	host     string
	webDir   string
	now      func() time.Time
	mu       sync.Mutex
	tickets  map[[sha256.Size]byte]time.Time
	sessions map[[sha256.Size]byte]time.Time
}

func NewLoopbackBrowserAccess(host, webDir string) (*LoopbackBrowserAccess, error) {
	access := &LoopbackBrowserAccess{
		host: host, webDir: strings.TrimSpace(webDir), now: time.Now,
		tickets: make(map[[sha256.Size]byte]time.Time), sessions: make(map[[sha256.Size]byte]time.Time),
	}
	if access.webDir == "" {
		return access, nil
	}
	abs, err := filepath.Abs(access.webDir)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(filepath.Join(abs, "index.html")); err != nil || info.IsDir() {
		return nil, errors.New("local web index is missing")
	}
	access.webDir = abs
	return access, nil
}

func (a *LoopbackBrowserAccess) Enabled() bool { return a != nil && a.webDir != "" }

func (a *LoopbackBrowserAccess) CreateURL() (string, error) {
	if !a.Enabled() {
		return "", errors.New("local web access is unavailable")
	}
	ticket, err := secureBrowserToken()
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.purgeLocked()
	a.tickets[sha256.Sum256([]byte(ticket))] = a.now().Add(30 * time.Second)
	a.mu.Unlock()
	return "http://" + a.host + "/local-ui/#ticket=" + ticket, nil
}

func (a *LoopbackBrowserAccess) HasSession(r *http.Request) bool {
	if !a.Enabled() {
		return false
	}
	cookie, err := r.Cookie(localBrowserCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	digest := sha256.Sum256([]byte(cookie.Value))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.purgeLocked()
	expires, ok := a.sessions[digest]
	return ok && expires.After(a.now())
}

func (a *LoopbackBrowserAccess) Register(engine *gin.Engine) {
	if !a.Enabled() {
		return
	}
	engine.GET("/local-ui", func(c *gin.Context) {
		if a.validBrowserBoundary(c) {
			c.Redirect(http.StatusTemporaryRedirect, "/local-ui/")
		}
	})
	engine.GET("/local-ui/*path", func(c *gin.Context) {
		a.static(c)
	})
	engine.POST("/local-ui/session", a.exchange)
}

func (a *LoopbackBrowserAccess) Clear() {
	if a == nil {
		return
	}
	a.mu.Lock()
	clear(a.tickets)
	clear(a.sessions)
	a.mu.Unlock()
}

func (a *LoopbackBrowserAccess) exchange(c *gin.Context) {
	if !a.validBrowserBoundary(c) {
		return
	}
	ticket, err := io.ReadAll(io.LimitReader(c.Request.Body, 1024))
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256(ticket)
	a.mu.Lock()
	a.purgeLocked()
	expires, ok := a.tickets[digest]
	delete(a.tickets, digest)
	if !ok || !expires.After(a.now()) {
		a.mu.Unlock()
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	session, err := secureBrowserToken()
	if err != nil {
		a.mu.Unlock()
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	a.sessions[sha256.Sum256([]byte(session))] = a.now().Add(15 * time.Minute)
	a.mu.Unlock()
	http.SetCookie(c.Writer, &http.Cookie{Name: localBrowserCookie, Value: session, Path: "/", MaxAge: 900, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	secureLocalWebHeaders(c.Writer.Header())
	c.Redirect(http.StatusFound, "/local-ui/")
}

func (a *LoopbackBrowserAccess) static(c *gin.Context) {
	if !a.validBrowserBoundary(c) {
		return
	}
	relative := strings.TrimPrefix(c.Param("path"), "/")
	if relative == "" {
		relative = "index.html"
	}
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	file := filepath.Join(a.webDir, clean)
	if info, err := os.Stat(file); err != nil || info.IsDir() {
		if filepath.Ext(clean) != "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		file = filepath.Join(a.webDir, "index.html")
	}
	secureLocalWebHeaders(c.Writer.Header())
	c.File(file)
}

func (a *LoopbackBrowserAccess) validBrowserBoundary(c *gin.Context) bool {
	if c.Request.Host != a.host {
		c.AbortWithStatus(http.StatusBadRequest)
		return false
	}
	origin := c.GetHeader("Origin")
	if origin != "" && origin != "http://"+a.host {
		c.AbortWithStatus(http.StatusForbidden)
		return false
	}
	return true
}

func (a *LoopbackBrowserAccess) purgeLocked() {
	now := a.now()
	for digest, expiry := range a.tickets {
		if !expiry.After(now) {
			delete(a.tickets, digest)
		}
	}
	for digest, expiry := range a.sessions {
		if !expiry.After(now) {
			delete(a.sessions, digest)
		}
	}
}

func secureBrowserToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func secureLocalWebHeaders(headers http.Header) {
	headers.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	headers.Set("X-Content-Type-Options", "nosniff")
	headers.Set("X-Frame-Options", "DENY")
	headers.Set("Referrer-Policy", "no-referrer")
	headers.Set("Cache-Control", "no-store")
}
