package service

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"daidai-panel/config"
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

// loopbackNoProxyHosts 是注入给任务运行时的回环直连白名单（#111）。
//
// 面板开了代理之后，脚本回调面板自身（DAIDAI_API_BASE 指向 127.0.0.1:5701）的请求
// 也会被 Python 的 urllib 交给代理，代理连不上内网回环就回 HTTP 502。
//
// 值必须是**纯主机名**：Python 只按主机名/域名后缀做字符串匹配，
// `127.0.0.1/32` 这种 CIDR 写法它一律匹配不上（已实测），写了等于没写。
// 三条分别覆盖 localhost、IPv4 回环、IPv6 回环三种可能出现的写法。
var loopbackNoProxyHosts = []string{"localhost", "127.0.0.1", "::1"}

// loopbackNoProxyHostList 在 loopbackNoProxyHosts 之外补上面板自身回调地址：
// 指定了非回环监听地址（#162）时脚本回调的是那个地址，它也得绕开代理。
func loopbackNoProxyHostList() []string {
	if config.C == nil {
		return loopbackNoProxyHosts
	}
	selfHost := config.C.Server.SelfHost()
	for _, host := range loopbackNoProxyHosts {
		if strings.EqualFold(host, selfHost) {
			return loopbackNoProxyHosts
		}
	}
	return append(append([]string{}, loopbackNoProxyHosts...), selfHost)
}

func AppendProxyEnv(env []string) []string {
	// 回环直连兜底（#111）。主修落点在 BuildManagedRuntimeEnvMapWithScriptToken 的 envMap，
	// 那里才是子进程里最后落地的一层；这里是第二道保险：
	// bash 任务的 env.sh 有 512KB 导出预算，变量一多 NO_PROXY 就可能被挤成
	// 「只赋值不 export」，子进程里实际拿不到 —— 而进程环境这一层不受那个预算约束。
	//
	// 故意放在 proxy_url 空值早退**之前**：面板自己没配代理时，容器/宿主机上
	// 外部设置的 HTTP_PROXY 一样会把回环请求送进代理，这条兜底同样有意义。
	//
	// 只在传入 env 里完全没有 NO_PROXY / no_proxy 时才追加，绝不覆盖调用方
	// （os.Environ()、或已经带上用户 envVars 的 slice）里已有的值。
	hasNoProxy := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "NO_PROXY=") || strings.HasPrefix(entry, "no_proxy=") {
			hasNoProxy = true
			break
		}
	}
	if !hasNoProxy {
		joined := strings.Join(loopbackNoProxyHostList(), ",")
		env = append(env, "NO_PROXY="+joined, "no_proxy="+joined)
	}

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
