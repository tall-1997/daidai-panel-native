package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/testutil"
)

// Server酱³（APP #14 的答复依据）：sctp 开头的 SendKey 发到 https://<uid>.push.ft07.com/send/<sendkey>.send，
// SCT 开头的 Turbo 版老 key 仍发 https://sctapi.ftqq.com/<sendkey>.send，请求与改动前一致。
//
// 后两条用例会把两个地址格式指向 httptest，所以第一条先单独锁住线上的真实地址：格式串写错时只有它会红。

func TestServerchanSendURLFormatsMatchOfficialDocs(t *testing.T) {
	// 期望值照官方文档（https://doc.sc3.ft07.com/zh/serverchan3/server/api）拼出：uid 做子域名，路径里是完整 SendKey。
	if got, want := fmt.Sprintf(serverchan3SendURLFormat, "12345", "sctp12345tabc"), "https://12345.push.ft07.com/send/sctp12345tabc.send"; got != want {
		t.Fatalf("Server酱³ 地址 = %q，期望 %q", got, want)
	}
	// Turbo 版的地址就是改动前写死的那一个。
	if got, want := fmt.Sprintf(serverchanTurboSendURLFormat, "SCT123456TABC"), "https://sctapi.ftqq.com/SCT123456TABC.send"; got != want {
		t.Fatalf("Turbo 版地址 = %q，期望 %q", got, want)
	}
}

func TestSendServerchanPicksEndpointBySendKey(t *testing.T) {
	testutil.SetupTestEnv(t)

	type capturedRequest struct {
		method string
		path   string
		body   map[string]string
	}
	var requests []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode serverchan body: %v", err)
		}
		requests = append(requests, capturedRequest{method: r.Method, path: r.URL.Path, body: body})
		_, _ = w.Write([]byte(`{"code":0,"message":"","data":{"pushid":"1"}}`))
	}))
	defer server.Close()

	oldTurboFormat, old3Format := serverchanTurboSendURLFormat, serverchan3SendURLFormat
	// 线上 uid 是子域名，而 httptest 只有一个主机，所以把 uid 挪到路径第一段，照样能断言取出来的 uid；
	// 两个格式用不同的路径前缀，看路径就知道走的是哪一代接口。
	serverchanTurboSendURLFormat = server.URL + "/turbo/%s.send"
	serverchan3SendURLFormat = server.URL + "/sc3/%s/send/%s.send"
	defer func() { serverchanTurboSendURLFormat, serverchan3SendURLFormat = oldTurboFormat, old3Format }()

	cases := []struct {
		name     string
		key      string
		wantPath string
	}{
		{name: "lowercase sctp goes to serverchan3 with uid", key: "sctp12345tAbCdEf", wantPath: "/sc3/12345/send/sctp12345tAbCdEf.send"},
		// 前缀大小写不影响归属：写成大写 SCTP 的也是 Server酱³ 的 key。
		{name: "uppercase SCTP goes to serverchan3", key: "SCTP12345TAbCdEf", wantPath: "/sc3/12345/send/SCTP12345TAbCdEf.send"},
		// Turbo 版老 key 的请求路径与改动前一致。
		{name: "turbo SCT key keeps legacy endpoint", key: "SCT123456TAbCdEf", wantPath: "/turbo/SCT123456TAbCdEf.send"},
		// sctp 开头却没有 sctp{uid}t 那一段，拼不出 Server酱³ 的地址，照旧发老地址。
		{name: "sctp without uid falls back to legacy endpoint", key: "sctpAbCdEf", wantPath: "/turbo/sctpAbCdEf.send"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requests = nil
			if err := sendServerchan(map[string]string{"key": tc.key}, "任务<通知>", "第一行\n第二行"); err != nil {
				t.Fatalf("send serverchan: %v", err)
			}
			if len(requests) != 1 {
				t.Fatalf("expected one request, got %#v", requests)
			}
			got := requests[0]
			if got.method != http.MethodPost || got.path != tc.wantPath {
				t.Fatalf("request = %s %s，期望 POST %s", got.method, got.path, tc.wantPath)
			}
			// 两代接口的参数都是 title / desp，请求体里不能多出别的键。
			want := map[string]string{"title": "任务<通知>", "desp": "第一行\n第二行"}
			if !reflect.DeepEqual(got.body, want) {
				t.Fatalf("body = %#v，期望 %#v", got.body, want)
			}
		})
	}
}

func TestSendServerchanReportsServerchan3ErrorText(t *testing.T) {
	testutil.SetupTestEnv(t)

	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server酱³ 出错时也回 HTTP 200，只在响应体里带业务码。
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	// 两个格式都指过来：选址一旦退化，请求也只会落到本机，不会真的打到官方接口（选址由上一条用例守）。
	oldTurboFormat, old3Format := serverchanTurboSendURLFormat, serverchan3SendURLFormat
	serverchanTurboSendURLFormat = server.URL + "/turbo/%s.send"
	serverchan3SendURLFormat = server.URL + "/sc3/%s/send/%s.send"
	defer func() { serverchanTurboSendURLFormat, serverchan3SendURLFormat = oldTurboFormat, old3Format }()

	cfg := map[string]string{"key": "sctp12345tAbCdEf"}

	// 用假 key 实测 Server酱³ 拿到的原文：原因在 error 字段，报错必须把它带出来，不能只剩「code=10003」。
	response = `{"error":"sendkey not found","code":10003}`
	err := sendServerchan(cfg, "标题", "正文")
	if err == nil {
		t.Fatal("Server酱³ 返回 code=10003 应判定为失败，却放行了")
	}
	if !strings.Contains(err.Error(), "sendkey not found") {
		t.Fatalf("报错里应带上 error 字段的原文，实际为：%v", err)
	}

	response = `{"code":0}`
	if err := sendServerchan(cfg, "标题", "正文"); err != nil {
		t.Fatalf("code=0 应视为成功，却返回：%v", err)
	}
}
