package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daidai-panel/config"
)

// 本机版（Android）通知扩展：local notification 桥接 + 外部 Webhook 渠道。
// 上游 v3.4.0 的 notifier 没有这两个渠道，合并后在这里补齐，并给所有 Webhook 出口加上
// 私网/回环目标拦截（SSRF 防护）。

type androidLocalNotificationPayload struct {
	Title   string            `json:"title"`
	Content string            `json:"content"`
	Channel string            `json:"channel,omitempty"`
	Context map[string]string `json:"context,omitempty"`
}

// androidLocalNotificationSend 由 Android 壳层（mobilecore）替换成真实发送实现。
var androidLocalNotificationSend = func(androidLocalNotificationPayload) error {
	return nil
}

func sendAndroidLocalNotification(cfg map[string]string, title, content string, context map[string]string) error {
	channel := strings.TrimSpace(cfg["channel"])
	if channel == "" {
		channel = "default"
	}
	return androidLocalNotificationSend(androidLocalNotificationPayload{
		Title:   title,
		Content: content,
		Channel: channel,
		Context: context,
	})
}

func sendExternalWebhook(cfg map[string]string, title, content string, context map[string]string) error {
	webhookURL := strings.TrimSpace(cfg["url"])
	if webhookURL == "" {
		return fmt.Errorf("外部 Webhook URL 为空")
	}
	body := map[string]interface{}{
		"title":   title,
		"content": content,
		"context": context,
	}
	if raw := strings.TrimSpace(cfg["body"]); raw != "" {
		parsed, err := parseNotificationJSONTemplateWithContext(raw, title, content, context)
		if err != nil {
			return fmt.Errorf("外部 Webhook 请求体配置无效: %w", err)
		}
		body = map[string]interface{}{"payload": parsed}
		if payload, ok := parsed.(map[string]interface{}); ok {
			body = payload
		}
	}
	headers := map[string]string{}
	if rawHeaders := strings.TrimSpace(cfg["headers"]); rawHeaders != "" {
		parsed, err := parseNotificationJSONTemplateWithContext(rawHeaders, title, content, context)
		if err != nil {
			return fmt.Errorf("外部 Webhook 请求头配置无效: %w", err)
		}
		if headerMap, ok := parsed.(map[string]interface{}); ok {
			for key, value := range headerMap {
				headers[key] = fmt.Sprint(value)
			}
		}
	}
	if token := strings.TrimSpace(cfg["bearer_token"]); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return httpPost(webhookURL, body, headers)
}

func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("Webhook URL 无效: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("Webhook URL 仅支持 http/https")
	}
	if parsed.User != nil {
		return fmt.Errorf("Webhook URL 禁止包含用户信息")
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("Webhook URL 主机为空")
	}
	if ip := net.ParseIP(host); ip != nil {
		return validateWebhookIP(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("Webhook URL 主机解析失败: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("Webhook URL 主机没有可用地址")
	}
	for _, ip := range ips {
		if err := validateWebhookIP(ip); err != nil {
			return err
		}
	}
	return nil
}

func validateWebhookIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("Webhook URL 主机地址无效")
	}
	if configModeIsTest() && ip.IsLoopback() {
		return nil
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return fmt.Errorf("Webhook URL 禁止访问内网或本机地址")
	}
	return nil
}

func configModeIsTest() bool {
	return config.C != nil && strings.EqualFold(config.C.Server.Mode, "test")
}

// webhookHTTPClient 在给定 client 上挂重定向校验与拨号层 IP 校验。
func webhookHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = NewHTTPClient(10 * time.Second)
	}
	clone := *client
	previous := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateWebhookURL(req.URL.String()); err != nil {
			return err
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}

	transport, ok := clone.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		transport = transport.Clone()
	}
	// 仅对直连生效：走 HTTP 代理时 DialContext 收到的是代理地址，不能按 Webhook 目标校验。
	if !transportUsesHTTPProxy(transport) {
		baseDial := transport.DialContext
		if baseDial == nil {
			baseDial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
		}
		transport.DialContext = newWebhookDialContext(baseDial)
	}
	clone.Transport = transport
	return &clone
}

// transportUsesHTTPProxy 判断 transport 当前是否会经由 HTTP 代理发起连接。
func transportUsesHTTPProxy(transport *http.Transport) bool {
	if transport.Proxy == nil {
		return false
	}
	probe, err := http.NewRequest(http.MethodGet, "https://webhook.invalid/", nil)
	if err != nil {
		return true
	}
	proxyURL, err := transport.Proxy(probe)
	return err == nil && proxyURL != nil
}

// newWebhookDialContext 在真正建立连接的拨号层校验目标 IP，消除「先校验 DNS、后连接时
// DNS 重绑定」的 TOCTOU 窗口：每次都重新解析并在每个候选地址上执行私网/回环校验。
func newWebhookDialContext(base func(ctx context.Context, network, addr string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if ip := net.ParseIP(host); ip != nil {
			if err := validateWebhookIP(ip); err != nil {
				return nil, err
			}
			return base(ctx, network, net.JoinHostPort(ip.String(), port))
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("Webhook URL 主机解析失败: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("Webhook URL 主机没有可用地址")
		}
		var lastErr error
		for _, candidate := range ips {
			if err := validateWebhookIP(candidate.IP); err != nil {
				return nil, err
			}
			conn, dialErr := base(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		return nil, lastErr
	}
}