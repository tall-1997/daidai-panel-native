package service

import (
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/testutil"
)

// issue #135：调用方声明正文格式（content_type）后各渠道的映射。
// 「不传 content_type 时报文逐字节不变」由 notifier_legacy_payload_test.go 用改动前抓下的 golden 守。

func TestNormalizeNotifyContentType(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "", want: "", ok: true},
		{raw: "   ", want: "", ok: true},
		{raw: "text", want: NotifyContentText, ok: true},
		{raw: "Plain", want: NotifyContentText, ok: true},
		{raw: " TXT ", want: NotifyContentText, ok: true},
		{raw: "markdown", want: NotifyContentMarkdown, ok: true},
		{raw: "MD", want: NotifyContentMarkdown, ok: true},
		{raw: "HTML", want: NotifyContentHTML, ok: true},
		// MIME 写法：想发 HTML 邮件的人最可能先试的就是 text/html，不能 400。
		{raw: "text/html", want: NotifyContentHTML, ok: true},
		{raw: "Text/HTML; charset=UTF-8", want: NotifyContentHTML, ok: true},
		{raw: "text/plain;charset=utf-8", want: NotifyContentText, ok: true},
		{raw: " text/markdown ", want: NotifyContentMarkdown, ok: true},
		{raw: "application/json", ok: false},
		{raw: "text/csv", ok: false},
		{raw: "; charset=utf-8", ok: false},
		{raw: "json", ok: false},
		{raw: "2", ok: false},
	}
	for _, tc := range cases {
		got, ok := NormalizeNotifyContentType(tc.raw)
		if got != tc.want || ok != tc.ok {
			t.Errorf("NormalizeNotifyContentType(%q) = (%q, %v)，期望 (%q, %v)", tc.raw, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNotifyHTMLToText(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			name: "table cells joined per row",
			html: "<table>\n  <tr><th>账号</th><th>积分</th></tr>\n  <tr><td>user01</td><td>+20</td></tr>\n</table>",
			want: "账号 | 积分\nuser01 | +20",
		},
		{
			name: "paragraphs and br",
			html: "<p>签到成功</p><p>账号: user01<br>积分: +20</p>",
			want: "签到成功\n账号: user01\n积分: +20",
		},
		{
			name: "entities unescaped and nbsp kept",
			html: "A &amp; B &lt;tag&gt; &quot;q&quot;&nbsp;&nbsp;end",
			want: `A & B <tag> "q"  end`,
		},
		{
			name: "script and style dropped",
			html: "<style>.a{color:red}</style><script>alert('<b>x</b>')</script><div>正文</div>",
			want: "正文",
		},
		{
			name: "nested list with source indentation",
			html: "<div>\n  <ul>\n    <li>一</li>\n    <li><b>二</b> 项</li>\n  </ul>\n</div>",
			want: "一\n二 项",
		},
		{
			name: "repeated br collapses to one blank line",
			html: "a<br><br/><br>b",
			want: "a\n\nb",
		},
		{
			name: "pre keeps whitespace",
			html: "<pre>line1\n  line2</pre>after",
			want: "line1\n  line2\nafter",
		},
		{
			name: "full document skips head",
			html: "<html><head><title>T</title></head><body><h1>标题</h1>内容</body></html>",
			want: "标题\n内容",
		},
		{
			// HTML 允许省略 </head>：不能因为 head 没闭合就把后面的正文整段吞掉（曾经输出空串）。
			name: "omitted head end tag keeps body",
			html: "<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>T</title><style>p{}</style><body><p>正文</p></body></html>",
			want: "正文",
		},
		{
			// tokenizer 会把 <script/> 之后到 </script> 的内容当原始文本，浏览器也不认 script 自闭合。
			name: "self-closing script still skips its raw text",
			html: "<script/>var a = 1;</script>正文",
			want: "正文",
		},
		{
			name: "bare newlines follow html whitespace rules",
			html: "第一行\n第二行",
			want: "第一行 第二行",
		},
		{
			// SVG 里的 <title/> 是自闭合的空元素；按 HTML 规则把后文当 title 原始文本读会吞掉整段正文（曾输出空串）。
			name: "self-closing title inside svg keeps following text",
			html: "<svg><title/></svg><p>正文在这里</p>",
			want: "正文在这里",
		},
		{
			name: "svg title desc style are hidden like in a browser",
			html: "<p>前</p><svg viewBox=\"0 0 1 1\"><title>图标</title><desc>说明</desc><style>.a{}</style><path d=\"M0\"/><text>图中字</text></svg><p>后</p>",
			want: "前\n图中字\n后",
		},
		{
			// 没闭合的 svg 遇到 <p> 这类 HTML 标签时浏览器会跳出外来内容，段落照常断行。
			name: "unclosed svg breaks out on html block tags",
			html: "<div>前</div><svg><g><p>段一</p><p>段二</p>",
			want: "前\n段一\n段二",
		},
		{
			name: "html inside svg foreignObject",
			html: "<svg><foreignObject><div>甲</div><div>乙</div></foreignObject></svg>尾",
			want: "甲\n乙\n尾",
		},
		{
			name: "mathml annotation hidden and self-closing tags closed",
			html: "<math><mi>x</mi><mspace/><annotation encoding=\"TeX\">x^2</annotation></math><p>后</p>",
			want: "x\n后",
		},
		{
			name: "html inside mathml annotation-xml stays hidden",
			html: "<math><annotation-xml encoding=\"text/html\"><p>隐藏</p></annotation-xml></math>显示",
			want: "显示",
		},
		{
			// 外来元素的隐藏和 HTML 元素的隐藏分开记：多余的 </script> 不能把 svg title 的隐藏提前结束，也不能让后面的 script 漏出来。
			name: "stray html end tag inside svg title",
			html: "<svg><title>x</script>y</title></svg>正文<script>s</script>尾",
			want: "正文尾",
		},
		{
			name: "cdata inside svg is text",
			html: "<svg><text><![CDATA[a<b]]></text></svg>",
			want: "a<b",
		},
		{
			// svg 的 title 里再写一个 HTML title：内层按原始文本读到自己的 </title>，不能把外层也算乱、吞掉后文。
			name: "html title nested in svg title does not swallow the rest",
			html: "<svg><title><title>x</title></title></svg>正文",
			want: "正文",
		},
		{
			// 浏览器里 iframe 显示的是 src 页面，标签之间的回退文字不显示；开着脚本时 noscript 同样不显示。
			name: "iframe and noscript fallback text hidden",
			html: "<iframe src=\"https://example.com\">fallback</iframe>ok<noscript>开启JS</noscript><p>正文</p>",
			want: "ok\n正文",
		},
		{
			name: "template content hidden",
			html: "<template><p>模板</p></template><p>正文</p>",
			want: "正文",
		},
		{
			// 外来内容里的 </p> 和 </br> 与 <p> 开始标签一样会跳出（HTML 标准），没闭合的 svg style 不能把后文藏掉。
			name: "end p breaks out of unclosed svg style",
			html: "<p><svg><style>.a{fill:red}</p>正文",
			want: "正文",
		},
		{
			name: "end br breaks out of unclosed mathml annotation",
			html: "<math><annotation>x^2</br>后文",
			want: "后文",
		},
		{
			// 跳到集成点为止：svg title 里的 </p> 按 HTML 处理，title 还开着，后文照旧不显示。
			name: "end p stops at svg title",
			html: "<p><svg><title>图标</p>正文",
			want: "",
		},
		{
			// </template> 连同模板里打开的 SVG / MathML 一起关掉，后面的 script 按 HTML 隐藏、正文照常显示。
			name: "end template closes foreign content opened inside",
			html: "<template><svg><style></template>正文<template><math></template><script>var a = 1;</script>尾",
			want: "正文尾",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := notifyHTMLToText(tc.html); got != tc.want {
				t.Fatalf("notifyHTMLToText(%q)\n got  %q\n want %q", tc.html, got, tc.want)
			}
		})
	}
}

// TestNotifyChannelHTMLCapabilityCoversRegistry：能力表必须与注册表的渠道类型一一对应。
// 加渠道时漏填能力表，调用方传 html 就会被默默当成「不支持 HTML」去标签 —— 让它在这里红，逼作者想清楚。
func TestNotifyChannelHTMLCapabilityCoversRegistry(t *testing.T) {
	registered := map[string]bool{}
	for _, def := range model.NotifyChannelDefinitions() {
		registered[def.Type] = true
		if _, ok := notifyChannelAcceptsHTML[def.Type]; !ok {
			t.Errorf("notifyChannelAcceptsHTML 没有声明渠道 %q 能否接收 HTML", def.Type)
		}
	}
	for channelType := range notifyChannelAcceptsHTML {
		if !registered[channelType] {
			t.Errorf("notifyChannelAcceptsHTML 里的 %q 不是注册表里的渠道类型", channelType)
		}
	}
}

func decodeCapturedJSON(t *testing.T, req legacyCapturedRequest) map[string]interface{} {
	t.Helper()
	payload := map[string]interface{}{}
	if err := json.Unmarshal([]byte(req.Body), &payload); err != nil {
		t.Fatalf("decode captured body %q: %v", req.Body, err)
	}
	return payload
}

func sendCapturedWithFormat(t *testing.T, channelType string, config func(string) map[string]string, title, content, contentType string) []legacyCapturedRequest {
	t.Helper()
	return captureNotifyRequests(t, config, channelType, func(ch model.NotifyChannel) error {
		return sendToChannel(ch, title, content, nil, contentType)
	})
}

func TestSendEmailHTMLContentTypeUsesTextHTML(t *testing.T) {
	testutil.SetupTestEnv(t)

	// 一整段不换行、远超 998 字节的 HTML：quoted-printable 必须把它折成短行。
	htmlBody := "<table><tr>" + strings.Repeat("<td>很长的单元格</td>", 120) + "</tr></table>"
	got := sendCapturedWithFormat(t, "email", legacyPayloadConfig(t, "email"), "日报", htmlBody, NotifyContentHTML)
	if len(got) != 1 {
		t.Fatalf("expected one smtp message, got %#v", got)
	}

	message := got[0].Body
	headerEnd := strings.Index(message, "\r\n\r\n")
	if headerEnd < 0 {
		t.Fatalf("message has no header separator: %q", message)
	}
	header := message[:headerEnd]
	wantHeader := "From: a@example.com\r\nTo: b@example.com\r\nSubject: 日报\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable"
	if header != wantHeader {
		t.Fatalf("unexpected html mail header:\n got  %q\n want %q", header, wantHeader)
	}

	encoded := message[headerEnd+4:]
	for _, line := range strings.Split(encoded, "\r\n") {
		if len(line) > 998 {
			t.Fatalf("quoted-printable line too long (%d bytes)", len(line))
		}
	}
	decoded, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(encoded)))
	if err != nil {
		t.Fatalf("decode quoted-printable: %v", err)
	}
	if string(decoded) != htmlBody {
		t.Fatalf("decoded html body mismatch:\n got  %q\n want %q", decoded, htmlBody)
	}
}

func TestSendWxPusherContentTypeOverridesChannelFormat(t *testing.T) {
	testutil.SetupTestEnv(t)

	wxConfig := func(channelContentType string) func(string) map[string]string {
		return func(u string) map[string]string {
			cfg := map[string]string{"app_token": "AT", "uids": "U1", "server": u + "/wx"}
			if channelContentType != "" {
				cfg["content_type"] = channelContentType
			}
			return cfg
		}
	}
	const table = "<table><tr><td>账号</td><td>积分</td></tr></table>"

	cases := []struct {
		name               string
		channelContentType string
		callerContentType  string
		wantType           float64
		wantContent        string
	}{
		{
			// 调用方声明 html：正文不转义，表格才渲染得出来；标题照旧转义。
			name: "html keeps markup", channelContentType: "1", callerContentType: NotifyContentHTML,
			wantType: 2, wantContent: "<h1>日报&lt;1&gt;</h1><br/>" + table,
		},
		{
			// 渠道配成 HTML、调用方没声明：必须继续转义（任务通知里嵌着脚本日志）。
			name: "channel html without caller format still escapes", channelContentType: "2", callerContentType: "",
			wantType: 2, wantContent: "<h1>日报&lt;1&gt;</h1><br/><div style='white-space: pre-wrap;'>&lt;table&gt;&lt;tr&gt;&lt;td&gt;账号&lt;/td&gt;&lt;td&gt;积分&lt;/td&gt;&lt;/tr&gt;&lt;/table&gt;</div>",
		},
		{
			name: "text overrides channel html", channelContentType: "2", callerContentType: NotifyContentText,
			wantType: 1, wantContent: "日报<1>\n" + table,
		},
		{
			name: "markdown maps to 3", channelContentType: "", callerContentType: NotifyContentMarkdown,
			wantType: 3, wantContent: "## 日报<1>\n\n" + table,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sendCapturedWithFormat(t, "wxpusher", wxConfig(tc.channelContentType), "日报<1>", table, tc.callerContentType)
			if len(got) != 1 {
				t.Fatalf("expected one request, got %#v", got)
			}
			payload := decodeCapturedJSON(t, got[0])
			if payload["contentType"] != tc.wantType {
				t.Fatalf("contentType = %#v, want %v", payload["contentType"], tc.wantType)
			}
			if payload["content"] != tc.wantContent {
				t.Fatalf("content mismatch:\n got  %q\n want %q", payload["content"], tc.wantContent)
			}
		})
	}
}

func TestSendPushplusContentTypeMapsTemplate(t *testing.T) {
	testutil.SetupTestEnv(t)

	var bodies []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode pushplus body: %v", err)
		}
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok"}`))
	}))
	defer server.Close()

	oldURL := pushplusSendURL
	pushplusSendURL = server.URL
	defer func() { pushplusSendURL = oldURL }()

	cases := []struct {
		name         string
		template     string
		contentType  string
		wantTemplate string // 空串表示请求体里不应出现 template 键
	}{
		{name: "legacy empty template sends no template key", template: "", contentType: "", wantTemplate: ""},
		{name: "legacy configured template kept", template: "txt", contentType: "", wantTemplate: "txt"},
		{name: "html", template: "", contentType: NotifyContentHTML, wantTemplate: "html"},
		{name: "markdown overrides txt", template: "txt", contentType: NotifyContentMarkdown, wantTemplate: "markdown"},
		{name: "text maps to txt", template: "html", contentType: NotifyContentText, wantTemplate: "txt"},
		{name: "json template never overridden", template: "json", contentType: NotifyContentHTML, wantTemplate: "json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bodies = nil
			cfg := map[string]string{"token": "tk"}
			if tc.template != "" {
				cfg["template"] = tc.template
			}
			raw, _ := json.Marshal(cfg)
			ch := model.NotifyChannel{Type: "pushplus", Config: string(raw)}
			if err := sendToChannel(ch, "标题", "<b>正文</b>", nil, tc.contentType); err != nil {
				t.Fatalf("send pushplus: %v", err)
			}
			if len(bodies) != 1 {
				t.Fatalf("expected one request, got %d", len(bodies))
			}
			want := map[string]string{"token": "tk", "title": "标题", "content": "<b>正文</b>"}
			if tc.wantTemplate != "" {
				want["template"] = tc.wantTemplate
			}
			if !reflect.DeepEqual(bodies[0], want) {
				t.Fatalf("pushplus body = %#v, want %#v", bodies[0], want)
			}
		})
	}
}

func TestSendDingtalkAndWecomContentTypeSwitchesTextMessages(t *testing.T) {
	testutil.SetupTestEnv(t)

	withWebhook := func(extra map[string]string) func(string) map[string]string {
		return func(u string) map[string]string {
			cfg := map[string]string{"webhook": u + "/robot"}
			for k, v := range extra {
				cfg[k] = v
			}
			return cfg
		}
	}

	cases := []struct {
		name        string
		channelType string
		config      func(string) map[string]string
		contentType string
		wantMsgType string
	}{
		{name: "dingtalk text config + markdown", channelType: "dingtalk", config: withWebhook(map[string]string{"msg_type": "text"}), contentType: NotifyContentMarkdown, wantMsgType: "markdown"},
		{name: "dingtalk default markdown + text", channelType: "dingtalk", config: withWebhook(nil), contentType: NotifyContentText, wantMsgType: "text"},
		{name: "wecom text + markdown", channelType: "wecom", config: withWebhook(nil), contentType: NotifyContentMarkdown, wantMsgType: "markdown"},
		{name: "wecom markdown_v2 stays v2", channelType: "wecom", config: withWebhook(map[string]string{"msg_type": "markdown_v2"}), contentType: NotifyContentMarkdown, wantMsgType: "markdown_v2"},
		{name: "wecom markdown_v2 + text", channelType: "wecom", config: withWebhook(map[string]string{"msg_type": "markdown_v2"}), contentType: NotifyContentText, wantMsgType: "text"},
		{
			name: "wecom news untouched", channelType: "wecom",
			config:      withWebhook(map[string]string{"msg_type": "news", "news_articles": `[{"title":"{{title}}","url":"https://example.com"}]`}),
			contentType: NotifyContentMarkdown, wantMsgType: "news",
		},
		{
			// html 在分发前已去成纯文本、格式交还为空：按渠道配置的消息类型发，不被强制改成 text。
			name: "dingtalk html stripped keeps configured markdown", channelType: "dingtalk", config: withWebhook(nil),
			contentType: NotifyContentHTML, wantMsgType: "markdown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sendCapturedWithFormat(t, tc.channelType, tc.config, "标题", "<p>第一行</p><p>第二行</p>", tc.contentType)
			if len(got) != 1 {
				t.Fatalf("expected one request, got %#v", got)
			}
			payload := decodeCapturedJSON(t, got[0])
			if payload["msgtype"] != tc.wantMsgType {
				t.Fatalf("msgtype = %#v, want %q (body=%s)", payload["msgtype"], tc.wantMsgType, got[0].Body)
			}
			// 请求体是 json.Marshal 出来的，< 默认被转义成 \u003c，所以两种写法都要查，只查 "<p>" 永远查不到。
			if tc.contentType == NotifyContentHTML {
				if strings.Contains(got[0].Body, "<p>") || strings.Contains(got[0].Body, "\\u003cp\\u003e") {
					t.Fatalf("html should be stripped before reaching %s: %s", tc.channelType, got[0].Body)
				}
				text := payload["markdown"].(map[string]interface{})["text"]
				if text != "### 标题  \n第一行  \n第二行" {
					t.Fatalf("stripped markdown text = %#v", text)
				}
			}
		})
	}
}

// TestSendWecomBotContentTypeKeepsMentionsAndSwitchedTemplate：企业微信机器人配了 @ 成员时不因 markdown 切成 markdown 消息
// （markdown 消息没有 mentioned_list，@ 会静默丢掉）；调用方的格式改了消息类型时不套渠道里按原类型写的 content_template。
func TestSendWecomBotContentTypeKeepsMentionsAndSwitchedTemplate(t *testing.T) {
	testutil.SetupTestEnv(t)

	botConfig := func(extra map[string]string) func(string) map[string]string {
		return func(u string) map[string]string {
			cfg := map[string]string{"webhook": u + "/robot"}
			for k, v := range extra {
				cfg[k] = v
			}
			return cfg
		}
	}
	const markdownTemplate = "**{{title}}** 模板\n{{content}}"

	cases := []struct {
		name         string
		extra        map[string]string
		contentType  string
		wantMsgType  string
		wantContent  string
		wantMentions []interface{}
		wantMobiles  []interface{}
	}{
		{
			name: "mentioned_list keeps text", extra: map[string]string{"mentioned_list": "@all"}, contentType: NotifyContentMarkdown,
			wantMsgType: "text", wantContent: "标题\n正文", wantMentions: []interface{}{"@all"},
		},
		{
			name: "mentioned_mobile_list keeps text", extra: map[string]string{"mentioned_mobile_list": "13800000000"}, contentType: NotifyContentMarkdown,
			wantMsgType: "text", wantContent: "标题\n正文", wantMobiles: []interface{}{"13800000000"},
		},
		{
			name: "markdown template not applied after switching to text", extra: map[string]string{"msg_type": "markdown", "content_template": markdownTemplate},
			contentType: NotifyContentText, wantMsgType: "text", wantContent: "标题\n正文",
		},
		{
			name: "text template not applied after switching to markdown", extra: map[string]string{"content_template": "【{{title}}】{{content}}"},
			contentType: NotifyContentMarkdown, wantMsgType: "markdown", wantContent: "**标题**\n正文",
		},
		{
			name: "template kept when type unchanged", extra: map[string]string{"msg_type": "markdown", "content_template": markdownTemplate},
			contentType: NotifyContentMarkdown, wantMsgType: "markdown", wantContent: "**标题** 模板\n正文",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sendCapturedWithFormat(t, "wecom", botConfig(tc.extra), "标题", "正文", tc.contentType)
			if len(got) != 1 {
				t.Fatalf("expected one request, got %#v", got)
			}
			payload := decodeCapturedJSON(t, got[0])
			if payload["msgtype"] != tc.wantMsgType {
				t.Fatalf("msgtype = %#v, want %q (body=%s)", payload["msgtype"], tc.wantMsgType, got[0].Body)
			}
			message, _ := payload[tc.wantMsgType].(map[string]interface{})
			if message["content"] != tc.wantContent {
				t.Fatalf("content = %#v, want %q", message["content"], tc.wantContent)
			}
			if !reflect.DeepEqual(message["mentioned_list"], interfaceSliceOrNil(tc.wantMentions)) {
				t.Fatalf("mentioned_list = %#v, want %#v", message["mentioned_list"], tc.wantMentions)
			}
			if !reflect.DeepEqual(message["mentioned_mobile_list"], interfaceSliceOrNil(tc.wantMobiles)) {
				t.Fatalf("mentioned_mobile_list = %#v, want %#v", message["mentioned_mobile_list"], tc.wantMobiles)
			}
		})
	}
}

// interfaceSliceOrNil 让「期望没有这个键」写成 nil 切片时能和 map 取不到键得到的 nil interface 比上。
func interfaceSliceOrNil(values []interface{}) interface{} {
	if values == nil {
		return nil
	}
	return values
}

// TestSendWecomAppContentTypeSwitchesTextMessages：企业微信应用不因调用方的 markdown 把文本消息切成 markdown 消息 ——
// markdown 应用消息在微信插件（微工作台）里不显示，也没有 safe / enable_id_trans，保密消息会被脚本一个参数变成普通消息。
// text 仍会把渠道配置的 markdown 切回文本，且不套按 markdown 写的 content_template。
func TestSendWecomAppContentTypeSwitchesTextMessages(t *testing.T) {
	testutil.SetupTestEnv(t)

	appConfig := func(extra map[string]string) func(string) map[string]string {
		return func(u string) map[string]string {
			cfg := map[string]string{"corp_id": "ww", "secret": "sec", "agent_id": "1000001", "base_url": u}
			for k, v := range extra {
				cfg[k] = v
			}
			return cfg
		}
	}
	const markdownTemplate = "**{{title}}** 模板\n{{content}}"

	cases := []struct {
		name        string
		extra       map[string]string
		content     string
		contentType string
		wantMsgType string
		wantContent string
		wantSafe    interface{} // nil 表示请求体里不该有 safe
		wantIDTrans interface{}
	}{
		{
			name: "default text stays text on markdown", contentType: NotifyContentMarkdown,
			wantMsgType: "text", wantContent: "标题\n正文", wantSafe: float64(0), wantIDTrans: float64(0),
		},
		{
			name: "safe kept on markdown", extra: map[string]string{"safe": "1"}, contentType: NotifyContentMarkdown,
			wantMsgType: "text", wantContent: "标题\n正文", wantSafe: float64(1), wantIDTrans: float64(0),
		},
		{
			name: "enable_id_trans kept on markdown", extra: map[string]string{"msg_type": "text", "enable_id_trans": "1"}, contentType: NotifyContentMarkdown,
			wantMsgType: "text", wantContent: "标题\n正文", wantSafe: float64(0), wantIDTrans: float64(1),
		},
		{
			name: "html stripped and sent as text with safe", extra: map[string]string{"safe": "1"}, content: "<b>正文</b>", contentType: NotifyContentHTML,
			wantMsgType: "text", wantContent: "标题\n正文", wantSafe: float64(1), wantIDTrans: float64(0),
		},
		{
			name: "markdown config switched to text without markdown template", extra: map[string]string{"msg_type": "markdown", "content_template": markdownTemplate},
			contentType: NotifyContentText, wantMsgType: "text", wantContent: "标题\n正文", wantSafe: float64(0), wantIDTrans: float64(0),
		},
		{
			name: "markdown config kept with its template", extra: map[string]string{"msg_type": "markdown", "content_template": markdownTemplate},
			contentType: NotifyContentMarkdown, wantMsgType: "markdown", wantContent: "**标题** 模板\n正文",
		},
		{
			name: "text config keeps its template on text", extra: map[string]string{"content_template": "【{{title}}】{{content}}"},
			contentType: NotifyContentText, wantMsgType: "text", wantContent: "【标题】正文", wantSafe: float64(0), wantIDTrans: float64(0),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := tc.content
			if content == "" {
				content = "正文"
			}
			got := sendCapturedWithFormat(t, "wecom_app", appConfig(tc.extra), "标题", content, tc.contentType)
			if len(got) != 2 {
				t.Fatalf("expected gettoken + send, got %#v", got)
			}
			payload := decodeCapturedJSON(t, got[1])
			if payload["msgtype"] != tc.wantMsgType {
				t.Fatalf("msgtype = %#v, want %q (body=%s)", payload["msgtype"], tc.wantMsgType, got[1].Body)
			}
			message, _ := payload[tc.wantMsgType].(map[string]interface{})
			if message["content"] != tc.wantContent {
				t.Fatalf("content = %#v, want %q", message["content"], tc.wantContent)
			}
			if payload["safe"] != tc.wantSafe || payload["enable_id_trans"] != tc.wantIDTrans {
				t.Fatalf("safe / enable_id_trans = %#v / %#v, want %#v / %#v (body=%s)",
					payload["safe"], payload["enable_id_trans"], tc.wantSafe, tc.wantIDTrans, got[1].Body)
			}
		})
	}

	t.Run("image untouched", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "wecom_app", appConfig(map[string]string{"msg_type": "image", "media_id": "MEDIA"}), "标题", "正文", NotifyContentMarkdown)
		if len(got) != 2 {
			t.Fatalf("expected gettoken + send, got %#v", got)
		}
		if payload := decodeCapturedJSON(t, got[1]); payload["msgtype"] != "image" {
			t.Fatalf("msgtype = %#v, want image", payload["msgtype"])
		}
	})
}

// TestSendToChannelStripsHTMLForTextOnlyChannels：不认 HTML 的渠道收到去标签后的纯文本，
// 而且不因为调用方声明了格式就去打开第三方的 markdown / parse_mode 开关（老版本服务端会静默忽略或报错）。
func TestSendToChannelStripsHTMLForTextOnlyChannels(t *testing.T) {
	testutil.SetupTestEnv(t)

	const htmlContent = "<p>签到成功</p><table><tr><td>账号</td><td>积分</td></tr></table>"
	const plain = "签到成功\n账号 | 积分"

	t.Run("telegram", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "telegram", legacyPayloadConfig(t, "telegram"), "日报", htmlContent, NotifyContentHTML)
		payload := decodeCapturedJSON(t, got[0])
		if payload["text"] != "日报\n"+plain {
			t.Fatalf("telegram text = %q", payload["text"])
		}
		if _, exists := payload["parse_mode"]; exists {
			t.Fatalf("telegram must not set parse_mode: %s", got[0].Body)
		}
	})
	t.Run("feishu", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "feishu", legacyPayloadConfig(t, "feishu"), "日报", htmlContent, NotifyContentHTML)
		payload := decodeCapturedJSON(t, got[0])
		content, _ := payload["content"].(map[string]interface{})
		if content["text"] != "日报\n"+plain {
			t.Fatalf("feishu text = %#v", payload["content"])
		}
	})
	t.Run("ntfy", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "ntfy", legacyPayloadConfig(t, "ntfy"), "日报", htmlContent, NotifyContentHTML)
		if got[0].Body != plain {
			t.Fatalf("ntfy body = %q", got[0].Body)
		}
		for _, header := range []string{"Markdown", "X-Markdown"} {
			if _, exists := got[0].Headers[header]; exists {
				t.Fatalf("ntfy must not enable %s header", header)
			}
		}
	})
	t.Run("ntfy markdown does not enable markdown header", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "ntfy", legacyPayloadConfig(t, "ntfy"), "日报", "**加粗**", NotifyContentMarkdown)
		for _, header := range []string{"Markdown", "X-Markdown"} {
			if _, exists := got[0].Headers[header]; exists {
				t.Fatalf("ntfy must not enable %s header", header)
			}
		}
	})
	t.Run("bark markdown sent as-is without markdown field", func(t *testing.T) {
		got := sendCapturedWithFormat(t, "bark", legacyPayloadConfig(t, "bark"), "日报", "**加粗**", NotifyContentMarkdown)
		payload := decodeCapturedJSON(t, got[0])
		if payload["body"] != "**加粗**" {
			t.Fatalf("bark body = %#v", payload["body"])
		}
		if _, exists := payload["markdown"]; exists {
			t.Fatalf("bark must not use the markdown field: %s", got[0].Body)
		}
	})
}

func TestSendWebhookForwardsDeclaredContentType(t *testing.T) {
	testutil.SetupTestEnv(t)

	const htmlContent = "<b>正文</b>"
	got := sendCapturedWithFormat(t, "webhook", legacyPayloadConfig(t, "webhook"), "标题", htmlContent, NotifyContentHTML)
	payload := decodeCapturedJSON(t, got[0])
	want := map[string]interface{}{"title": "标题", "content": htmlContent, "content_type": NotifyContentHTML}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("webhook body = %#v, want %#v", payload, want)
	}
}

// TestSendNotificationSyncFiltersByChannelType：类型过滤叠加在原有两种集合上，不改变 push_scope 语义。
func TestSendNotificationSyncFiltersByChannelType(t *testing.T) {
	testutil.SetupTestEnv(t)

	var mu sync.Mutex
	hits := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	create := func(name, channelType, path, scope string) *model.NotifyChannel {
		ch := &model.NotifyChannel{
			Name:      name,
			Type:      channelType,
			Config:    `{"url":"` + server.URL + path + `"}`,
			PushScope: scope,
			Enabled:   true,
		}
		if err := database.DB.Create(ch).Error; err != nil {
			t.Fatalf("create channel %s: %v", name, err)
		}
		return ch
	}
	create("广播 webhook", "webhook", "/default-webhook", model.NotifyPushScopeDefault)
	boundWebhook := create("绑定 webhook", "webhook", "/bound-webhook", model.NotifyPushScopeBound)
	defaultCustom := create("广播 custom", "custom", "/default-custom", model.NotifyPushScopeDefault)

	sentPaths := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var paths []string
		for path, count := range hits {
			for i := 0; i < count; i++ {
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		for path := range hits {
			delete(hits, path)
		}
		return paths
	}

	if _, err := SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{ChannelTypes: []string{"webhook"}}); err != nil {
		t.Fatalf("broadcast by type: %v", err)
	}
	if got := sentPaths(); !reflect.DeepEqual(got, []string{"/default-webhook"}) {
		t.Fatalf("广播 + 类型过滤只该命中默认推送的 webhook，实际 %v", got)
	}

	if _, err := SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{
		ChannelIDs:   []uint{boundWebhook.ID, defaultCustom.ID},
		ChannelTypes: []string{"webhook"},
	}); err != nil {
		t.Fatalf("targeted by type: %v", err)
	}
	if got := sentPaths(); !reflect.DeepEqual(got, []string{"/bound-webhook"}) {
		t.Fatalf("点名 + 类型过滤应取交集（且点名忽略 push_scope），实际 %v", got)
	}

	_, err := SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{
		ChannelIDs:   []uint{defaultCustom.ID},
		ChannelTypes: []string{"webhook"},
	})
	if err == nil || !strings.Contains(err.Error(), "未找到已启用的通知渠道") {
		t.Fatalf("交集为空时应沿用「未找到已启用的通知渠道」，实际 %v", err)
	}

	_, err = SendNotificationSyncWithOptions("t", "c", NotificationDispatchOptions{ChannelTypes: []string{"email"}})
	if err == nil || !strings.Contains(err.Error(), "暂无参与广播的默认推送渠道") || !strings.Contains(err.Error(), "email") {
		t.Fatalf("广播按类型无命中时应说明类型，实际 %v", err)
	}
	if got := sentPaths(); len(got) != 0 {
		t.Fatalf("无命中时不应发出任何请求，实际 %v", got)
	}
}
