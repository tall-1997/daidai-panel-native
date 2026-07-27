package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"daidai-panel/testutil"

	"github.com/gin-gonic/gin"
)

func newSponsorRouter() *gin.Engine {
	engine := gin.New()
	api := engine.Group("/api/v1")
	NewSponsorHandler().RegisterRoutes(api)
	return engine
}

func TestSponsorListReturnsEmptyWhenFeedUnavailable(t *testing.T) {
	testutil.SetupTestEnv(t)
	originalFeedURL := defaultSponsorFeedURL
	defaultSponsorFeedURL = "http://127.0.0.1:1/"
	t.Cleanup(func() {
		defaultSponsorFeedURL = originalFeedURL
	})

	engine := newSponsorRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data struct {
			Sponsors    []any `json:"sponsors"`
			Count       int   `json:"count"`
			Unavailable bool  `json:"unavailable"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode sponsor response: %v", err)
	}
	if payload.Data.Count != 0 || len(payload.Data.Sponsors) != 0 {
		t.Fatalf("expected empty sponsor list, got %+v", payload.Data)
	}
	if !payload.Data.Unavailable {
		t.Fatalf("expected unavailable fallback when feed cannot be reached")
	}
}

func TestSponsorListProxiesRemoteFeed(t *testing.T) {
	testutil.SetupTestEnv(t)

	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"sponsors": [
					{
						"id": 1,
						"name": "赞助用户",
						"amount": 99.99,
						"avatar_url": "/uploads/demo.png"
					}
				],
				"count": 1,
				"total_amount": 99.99,
				"updated_at": "2026-03-24T10:00:00Z",
				"portal_url": "/"
			}
		}`))
	}))
	defer remote.Close()

	originalFeedURL := defaultSponsorFeedURL
	defaultSponsorFeedURL = remote.URL + "/"
	t.Cleanup(func() {
		defaultSponsorFeedURL = originalFeedURL
	})

	engine := newSponsorRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sponsors", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data struct {
			Sponsors []struct {
				Name      string `json:"name"`
				AvatarURL string `json:"avatar_url"`
				Initial   string `json:"initial"`
			} `json:"sponsors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode sponsor response: %v", err)
	}

	if len(payload.Data.Sponsors) != 1 {
		t.Fatalf("expected one sponsor, got %d", len(payload.Data.Sponsors))
	}
	if got := payload.Data.Sponsors[0].AvatarURL; got != remote.URL+"/uploads/demo.png" {
		t.Fatalf("expected avatar url resolved to remote service, got %q", got)
	}
	if got := payload.Data.Sponsors[0].Initial; got != "赞" {
		t.Fatalf("expected initial fallback, got %q", got)
	}
	if strings.Contains(rec.Body.String(), "portal_url") {
		t.Fatalf("expected panel sponsor proxy to strip portal_url, got %s", rec.Body.String())
	}
}

func TestResolveRemoteURLUpgradesSameHostAvatarToHTTPS(t *testing.T) {
	got := resolveRemoteURL(
		"https://dumblist.linzixuan.top/",
		"http://dumblist.linzixuan.top/uploads/demo.png",
	)

	if got != "https://dumblist.linzixuan.top/uploads/demo.png" {
		t.Fatalf("expected same-host avatar url upgraded to https, got %q", got)
	}
}

func TestResolveRemoteURLKeepsThirdPartyAbsoluteAvatarURL(t *testing.T) {
	got := resolveRemoteURL(
		"https://dumblist.linzixuan.top/",
		"http://example.com/uploads/demo.png",
	)

	if got != "http://example.com/uploads/demo.png" {
		t.Fatalf("expected third-party avatar url unchanged, got %q", got)
	}
}
