package mcptools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RemoteDispatcher 是 ddp mcp（stdio 模式）用的 Dispatcher：通过 HTTP 调本机正在运行的面板。
//
// 用开放 API 应用的 app_key / app_secret 换令牌（POST /api/v1/open-api/token），
// 令牌快过期或被面板拒绝（401）时重换一次。面板没起时返回清楚的中文错误，绝不绕去直连数据库写。
type RemoteDispatcher struct {
	baseURL   string
	appKey    string
	appSecret string
	client    *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

const (
	// remoteResponseLimit 防止一次异常大的响应把 ddp 进程内存吃光；正常接口远小于这个值。
	remoteResponseLimit = 32 << 20
	// tokenRefreshMargin：令牌剩余有效期不足这个值时提前重换，避免调用途中过期。
	tokenRefreshMargin = 5 * time.Minute
)

// NewRemoteDispatcher 创建连 baseURL（例如 http://127.0.0.1:5701）的 Dispatcher。client 为 nil 时用默认客户端。
func NewRemoteDispatcher(baseURL, appKey, appSecret string, client *http.Client) *RemoteDispatcher {
	if client == nil {
		// 单次调用最长的是 run_script 的轮询（每次只是一问一答），90 秒足够覆盖慢机器上的大列表。
		client = &http.Client{Timeout: 90 * time.Second}
	}
	return &RemoteDispatcher{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		appKey:    appKey,
		appSecret: appSecret,
		client:    client,
	}
}

func (d *RemoteDispatcher) Do(ctx context.Context, method, path string, query url.Values, body any) (int, []byte, error) {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("序列化请求体失败：%w", err)
		}
		payload = encoded
	}

	token, err := d.currentToken(ctx, false)
	if err != nil {
		return 0, nil, err
	}
	status, respBody, err := d.send(ctx, method, path, query, payload, token)
	if err != nil {
		return 0, nil, err
	}
	if status != http.StatusUnauthorized {
		return status, respBody, nil
	}

	// 401 说明请求在鉴权阶段就被拒了、业务没执行（面板重启换了 JWT 密钥、令牌被吊销等），
	// 换一枚新令牌重试一次是安全的，POST 也不会重复执行。
	token, err = d.currentToken(ctx, true)
	if err != nil {
		return 0, nil, err
	}
	return d.send(ctx, method, path, query, payload, token)
}

func (d *RemoteDispatcher) currentToken(ctx context.Context, force bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !force && d.token != "" && time.Now().Before(d.expiresAt.Add(-tokenRefreshMargin)) {
		return d.token, nil
	}
	token, lifetime, err := d.exchangeToken(ctx)
	if err != nil {
		return "", err
	}
	d.token = token
	d.expiresAt = time.Now().Add(lifetime)
	return token, nil
}

func (d *RemoteDispatcher) exchangeToken(ctx context.Context) (string, time.Duration, error) {
	body, _ := json.Marshal(map[string]string{"app_key": d.appKey, "app_secret": d.appSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/api/v1/open-api/token", bytes.NewReader(body))
	if err != nil {
		return "", 0, fmt.Errorf("面板地址无效：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return "", 0, d.connectionError(ctx, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, remoteResponseLimit))
	if err != nil {
		return "", 0, fmt.Errorf("读取面板响应失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("用应用凭据换取令牌失败：%v", apiError(resp.StatusCode, respBody))
	}

	var parsed struct {
		Data struct {
			AccessToken string  `json:"access_token"`
			ExpiresIn   float64 `json:"expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil || parsed.Data.AccessToken == "" {
		return "", 0, errors.New("面板的令牌接口返回了无法识别的内容，请确认 --url 指向的是呆呆面板后端")
	}
	lifetime := time.Duration(parsed.Data.ExpiresIn) * time.Second
	if lifetime <= tokenRefreshMargin {
		// 面板目前固定签 24 小时；万一以后改短，也至少缓存一小段，别每次调用都去换。
		lifetime = tokenRefreshMargin + time.Minute
	}
	return parsed.Data.AccessToken, lifetime, nil
}

func (d *RemoteDispatcher) send(ctx context.Context, method, path string, query url.Values, payload []byte, token string) (int, []byte, error) {
	target := d.baseURL + "/api/v1" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader = http.NoBody
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("构造请求失败：%w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, d.connectionError(ctx, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, remoteResponseLimit))
	if err != nil {
		return 0, nil, fmt.Errorf("读取面板响应失败：%w", err)
	}
	return resp.StatusCode, respBody, nil
}

func (d *RemoteDispatcher) connectionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("无法连接面板 %s：%v。请确认面板正在运行，或用 --url（环境变量 DDP_MCP_URL）指定面板后端地址", d.baseURL, err)
}
