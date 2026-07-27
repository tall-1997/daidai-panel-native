package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daidai-panel/model"

	xproxy "golang.org/x/net/proxy"
)

func NewHTTPClient(timeout time.Duration) *http.Client {
	return NewHTTPClientWithProxy(timeout, "")
}

func NewHTTPClientWithProxy(timeout time.Duration, proxyOverride string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	proxyURL := strings.TrimSpace(proxyOverride)
	if proxyURL == "" {
		proxyURL = strings.TrimSpace(model.GetRegisteredConfig("proxy_url"))
	}

	if proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
			if scheme == "socks5" || scheme == "socks5h" {
				dialURL := *parsed
				dialURL.Scheme = "socks5"
				dialer, dialErr := xproxy.FromURL(&dialURL, &net.Dialer{Timeout: timeout})
				if dialErr == nil {
					transport.Proxy = nil
					transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						type contextDialer interface {
							DialContext(context.Context, string, string) (net.Conn, error)
						}
						if typed, ok := dialer.(contextDialer); ok {
							return typed.DialContext(ctx, network, addr)
						}
						return dialer.Dial(network, addr)
					}
				}
			} else {
				transport.Proxy = http.ProxyURL(parsed)
			}
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func AppendProxyEnv(env []string) []string {
	proxyURL := strings.TrimSpace(model.GetRegisteredConfig("proxy_url"))
	if proxyURL == "" {
		return env
	}

	keys := []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"ALL_PROXY",
		"http_proxy",
		"https_proxy",
		"all_proxy",
	}

	for _, key := range keys {
		env = append(env, key+"="+proxyURL)
	}
	return env
}
