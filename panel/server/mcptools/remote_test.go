package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTokenPanel 起一个假面板：/api/v1/open-api/token 校验 k/s 后依次签发 tok-1、tok-2……，其余路径交给 api。
func newTokenPanel(t *testing.T, exchanges *atomic.Int32, api http.HandlerFunc) *httptest.Server {
	t.Helper()
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/open-api/token" {
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != http.MethodPost || body["app_key"] != "k" || body["app_secret"] != "s" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"凭证无效"}`))
				return
			}
			n := exchanges.Add(1)
			_, _ = fmt.Fprintf(w, `{"data":{"access_token":"tok-%d","token_type":"Bearer","expires_in":86400}}`, n)
			return
		}
		api(w, r)
	}))
	t.Cleanup(panel.Close)
	return panel
}

func TestRemoteDispatcherExchangesTokenOnceAndReusesIt(t *testing.T) {
	var exchanges atomic.Int32
	panel := newTokenPanel(t, &exchanges, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks" || r.Header.Get("Authorization") != "Bearer tok-1" || r.URL.Query().Get("keyword") != "jd" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"unexpected request"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	})

	// 末尾多一个 / 也要能用：用户照着文档填地址时很容易带上。
	dispatcher := NewRemoteDispatcher(panel.URL+"/", "k", "s", nil)
	for i := 0; i < 2; i++ {
		status, payload, err := dispatcher.Do(context.Background(), http.MethodGet, "/tasks", url.Values{"keyword": {"jd"}}, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("第 %d 次调用失败: status=%d err=%v body=%s", i+1, status, err, payload)
		}
	}
	if got := exchanges.Load(); got != 1 {
		t.Fatalf("令牌应当复用，只换一次，实际换了 %d 次", got)
	}
}

func TestRemoteDispatcherRetriesOnceWithFreshTokenAfter401(t *testing.T) {
	var exchanges, hits atomic.Int32
	panel := newTokenPanel(t, &exchanges, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			// 模拟面板重启换了 JWT 密钥：旧令牌一律 401。
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"令牌无效或已过期"}`))
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("Content-Type") != "application/json" || body["path"] != "a.py" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"请求体没有原样重发"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"run_id":"r1"}`))
	})

	dispatcher := NewRemoteDispatcher(panel.URL, "k", "s", nil)
	status, payload, err := dispatcher.Do(context.Background(), http.MethodPost, "/scripts/run", nil, map[string]any{"path": "a.py"})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("401 后应当换令牌重试成功: status=%d err=%v body=%s", status, err, payload)
	}
	if exchanges.Load() != 2 || hits.Load() != 2 {
		t.Fatalf("应当换 2 次令牌、请求 2 次，实际换 %d 次、请求 %d 次", exchanges.Load(), hits.Load())
	}
}

func TestRemoteDispatcherReportsBadCredentials(t *testing.T) {
	var exchanges atomic.Int32
	panel := newTokenPanel(t, &exchanges, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("凭据错误时不应走到业务接口: %s", r.URL.Path)
	})

	dispatcher := NewRemoteDispatcher(panel.URL, "k", "wrong", nil)
	_, _, err := dispatcher.Do(context.Background(), http.MethodGet, "/tasks", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "换取令牌失败") || !strings.Contains(err.Error(), "凭证无效") {
		t.Fatalf("凭据错误应当给出明确提示，实际 %v", err)
	}
}

func TestRemoteDispatcherReportsUnreachablePanel(t *testing.T) {
	panel := httptest.NewServer(http.NotFoundHandler())
	address := panel.URL
	panel.Close()

	dispatcher := NewRemoteDispatcher(address, "k", "s", &http.Client{Timeout: 10 * time.Second})
	_, _, err := dispatcher.Do(context.Background(), http.MethodGet, "/tasks", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "无法连接面板") {
		t.Fatalf("面板没起时应当返回「无法连接面板」，实际 %v", err)
	}
}
