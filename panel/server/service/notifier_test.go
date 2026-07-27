package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"reflect"
	"strings"
	"testing"

	"daidai-panel/model"
	"daidai-panel/testutil"
)

func TestSplitNotificationTargets(t *testing.T) {
	got := splitNotificationTargets("uid-a; uid-b,\nuid-c\tuid-d")
	want := []string{"uid-a", "uid-b", "uid-c", "uid-d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected targets: got %v want %v", got, want)
	}
}

func TestSplitNotificationIntTargets(t *testing.T) {
	got, err := splitNotificationIntTargets("101; 102,103")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []int{101, 102, 103}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected int targets: got %v want %v", got, want)
	}
}

func TestSplitNotificationIntTargetsRejectsInvalidValue(t *testing.T) {
	if _, err := splitNotificationIntTargets("101;abc"); err == nil {
		t.Fatal("expected invalid topic id to return an error")
	}
}

func TestSendEmailSelectsSSLMode(t *testing.T) {
	oldPlain := smtpSendMail
	oldTLS := smtpSendMailWithImplicitTLS
	defer func() {
		smtpSendMail = oldPlain
		smtpSendMailWithImplicitTLS = oldTLS
	}()

	cases := []struct {
		name string
		port string
		ssl  string
		want string
	}{
		{name: "auto 465 uses implicit TLS", port: "465", want: "tls"},
		{name: "explicit true uses implicit TLS", port: "587", ssl: "true", want: "tls"},
		{name: "explicit false keeps plain smtp", port: "465", ssl: "false", want: "plain"},
		{name: "auto non-465 keeps plain smtp", port: "587", ssl: "auto", want: "plain"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := []string{}
			assertPayload := func(addr, host, from string, to []string, msg []byte) {
				if addr != "smtp.example.com:"+tc.port {
					t.Fatalf("unexpected addr: %s", addr)
				}
				if host != "smtp.example.com" {
					t.Fatalf("unexpected host: %s", host)
				}
				if from != "sender@example.com" {
					t.Fatalf("unexpected from: %s", from)
				}
				wantTo := []string{"one@example.com", "two@example.com"}
				if !reflect.DeepEqual(to, wantTo) {
					t.Fatalf("unexpected recipients: got %v want %v", to, wantTo)
				}
				body := string(msg)
				if !strings.Contains(body, "Subject: 标题") || !strings.Contains(body, "正文") {
					t.Fatalf("unexpected message body: %q", body)
				}
			}
			smtpSendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
				calls = append(calls, "plain")
				assertPayload(addr, "smtp.example.com", from, to, msg)
				return nil
			}
			smtpSendMailWithImplicitTLS = func(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
				calls = append(calls, "tls")
				assertPayload(addr, host, from, to, msg)
				return nil
			}

			cfg := map[string]string{
				"smtp_host": "smtp.example.com",
				"smtp_port": tc.port,
				"smtp_user": "sender@example.com",
				"smtp_pass": "secret",
				"from":      "sender@example.com",
				"to":        "one@example.com, two@example.com",
			}
			if tc.ssl != "" {
				cfg["smtp_ssl"] = tc.ssl
			}
			if err := sendEmail(cfg, "标题", "正文"); err != nil {
				t.Fatalf("send email: %v", err)
			}
			if !reflect.DeepEqual(calls, []string{tc.want}) {
				t.Fatalf("unexpected send mode calls: got %v want %v", calls, []string{tc.want})
			}
		})
	}
}

func TestRenderNotificationTemplateWithContext(t *testing.T) {
	got := renderNotificationTemplateWithContext(
		"任务 {{task_name}} 在 {{ended_at}} {{status_text}}，退出码 {{exit_code}}",
		"标题",
		"正文",
		"",
		map[string]string{
			"task_name":   "签到任务",
			"ended_at":    "2026-03-22 12:00:00.000",
			"status_text": "失败",
			"exit_code":   "2",
		},
	)

	want := "任务 签到任务 在 2026-03-22 12:00:00.000 失败，退出码 2"
	if got != want {
		t.Fatalf("unexpected rendered template: got %q want %q", got, want)
	}
}

func TestBuildTelegramMessagesSplitsLongContent(t *testing.T) {
	content := strings.Repeat("日志内容\n", 900)
	messages := buildTelegramMessages("任务执行失败", content)
	if len(messages) < 2 {
		t.Fatalf("expected long telegram content to be split, got %d message(s)", len(messages))
	}

	for i, message := range messages {
		if !strings.Contains(message, "任务执行失败") {
			t.Fatalf("expected message %d to contain title, got %q", i, message)
		}
		if len([]rune(message)) > 3600 {
			t.Fatalf("expected telegram message %d to stay under safe limit, got %d runes", i, len([]rune(message)))
		}
	}
}

func TestSendDingtalkText(t *testing.T) {
	testutil.SetupTestEnv(t)

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendDingtalk(map[string]string{
		"webhook":  server.URL,
		"msg_type": "Text",
	}, "标题", "第一行\n第二行")
	if err != nil {
		t.Fatalf("send dingtalk text: %v", err)
	}

	if got := body["msgtype"]; got != "text" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	textBody, ok := body["text"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected text payload: %#v", body["text"])
	}
	if got := textBody["content"]; got != "标题\n第一行\n第二行" {
		t.Fatalf("unexpected text content: %#v", got)
	}
	if _, exists := body["markdown"]; exists {
		t.Fatalf("text message should not include markdown payload")
	}
}

func TestSendDingtalkMarkdownFallback(t *testing.T) {
	testutil.SetupTestEnv(t)

	cases := []struct {
		name    string
		msgType string
	}{
		{name: "explicit markdown", msgType: "markdown"},
		{name: "empty falls back to markdown", msgType: ""},
		{name: "unknown falls back to markdown", msgType: "html"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			cfg := map[string]string{"webhook": server.URL}
			if tc.msgType != "" {
				cfg["msg_type"] = tc.msgType
			}

			if err := sendDingtalk(cfg, "标题", "第一行\n第二行"); err != nil {
				t.Fatalf("send dingtalk markdown: %v", err)
			}

			if got := body["msgtype"]; got != "markdown" {
				t.Fatalf("unexpected msgtype: %#v", got)
			}
			markdown, ok := body["markdown"].(map[string]interface{})
			if !ok {
				t.Fatalf("unexpected markdown payload: %#v", body["markdown"])
			}
			if got := markdown["title"]; got != "标题" {
				t.Fatalf("unexpected markdown title: %#v", got)
			}
			if got := markdown["text"]; got != "### 标题  \n第一行  \n第二行" {
				t.Fatalf("unexpected markdown text: %#v", got)
			}
			if _, exists := body["text"]; exists {
				t.Fatalf("markdown message should not include text payload")
			}
		})
	}
}

func TestSendToChannelPrefixesPanelLabel(t *testing.T) {
	testutil.SetupTestEnv(t)

	if err := model.SetConfig("notify_panel_label", "家里NAS"); err != nil {
		t.Fatalf("set notify_panel_label: %v", err)
	}

	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := model.NotifyChannel{
		Type:   "webhook",
		Config: fmt.Sprintf(`{"url":%q}`, server.URL),
	}
	if err := sendToChannel(ch, "原始标题", "正文", nil); err != nil {
		t.Fatalf("send to channel: %v", err)
	}

	if got := body["title"]; got != "【家里NAS】原始标题" {
		t.Fatalf("unexpected prefixed title: %q", got)
	}
	if got := body["content"]; got != "正文" {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestSendToChannelKeepsTitleWhenPanelLabelEmpty(t *testing.T) {
	testutil.SetupTestEnv(t)

	var body map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := model.NotifyChannel{
		Type:   "webhook",
		Config: fmt.Sprintf(`{"url":%q}`, server.URL),
	}
	if err := sendToChannel(ch, "原始标题", "正文", nil); err != nil {
		t.Fatalf("send to channel: %v", err)
	}

	if got := body["title"]; got != "原始标题" {
		t.Fatalf("expected title unchanged when label empty, got %q", got)
	}
}

func TestSendWecomTextWithMentions(t *testing.T) {
	testutil.SetupTestEnv(t)

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendWecom(map[string]string{
		"webhook":               server.URL,
		"msg_type":              "text",
		"content_template":      "{{title}}\n{{content}}",
		"mentioned_list":        "wangqing,@all",
		"mentioned_mobile_list": "13800001111",
	}, "告警标题", "告警内容")
	if err != nil {
		t.Fatalf("send wecom text: %v", err)
	}

	if got := body["msgtype"]; got != "text" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}

	textBody, ok := body["text"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected text payload: %#v", body["text"])
	}
	if got := textBody["content"]; got != "告警标题\n告警内容" {
		t.Fatalf("unexpected text content: %#v", got)
	}

	mentionedList, ok := textBody["mentioned_list"].([]interface{})
	if !ok || len(mentionedList) != 2 {
		t.Fatalf("unexpected mentioned_list: %#v", textBody["mentioned_list"])
	}
}

func TestSendWecomTemplateCard(t *testing.T) {
	testutil.SetupTestEnv(t)

	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := sendWecom(map[string]string{
		"webhook":  server.URL,
		"msg_type": "template_card",
		"template_card_payload": `{
			"card_type":"text_notice",
			"main_title":{"title":"{{title}}","desc":"{{content}}"}
		}`,
	}, "系统通知", "任务执行完成")
	if err != nil {
		t.Fatalf("send wecom template card: %v", err)
	}

	if got := body["msgtype"]; got != "template_card" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	cardBody, ok := body["template_card"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected template_card payload: %#v", body["template_card"])
	}
	mainTitle, ok := cardBody["main_title"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected main_title: %#v", cardBody["main_title"])
	}
	if got := mainTitle["title"]; got != "系统通知" {
		t.Fatalf("unexpected template title: %#v", got)
	}
	if got := mainTitle["desc"]; got != "任务执行完成" {
		t.Fatalf("unexpected template desc: %#v", got)
	}
}

func TestSendWecomAppMarkdown(t *testing.T) {
	testutil.SetupTestEnv(t)

	var (
		tokenRequested bool
		messageBody    map[string]interface{}
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenRequested = true
			if got := r.URL.Query().Get("corpid"); got != "ww-demo" {
				t.Fatalf("unexpected corp id: %s", got)
			}
			if got := r.URL.Query().Get("corpsecret"); got != "secret-demo" {
				t.Fatalf("unexpected corp secret: %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/cgi-bin/message/send":
			if got := r.URL.Query().Get("access_token"); got != "token-demo" {
				t.Fatalf("unexpected access_token: %s", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldTokenURL := wecomAppTokenURL
	oldSendURL := wecomAppSendURL
	wecomAppTokenURL = server.URL + "/cgi-bin/gettoken"
	wecomAppSendURL = server.URL + "/cgi-bin/message/send"
	defer func() {
		wecomAppTokenURL = oldTokenURL
		wecomAppSendURL = oldSendURL
	}()

	err := sendWecomApp(map[string]string{
		"corp_id":  "ww-demo",
		"secret":   "secret-demo",
		"agent_id": "1000001",
		"to_user":  "@all",
		"msg_type": "markdown",
	}, "标题", "正文")
	if err != nil {
		t.Fatalf("send wecom app: %v", err)
	}
	if !tokenRequested {
		t.Fatal("expected token endpoint to be requested")
	}
	if got := messageBody["msgtype"]; got != "markdown" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	if got := messageBody["touser"]; got != "@all" {
		t.Fatalf("unexpected touser: %#v", got)
	}

	markdown, ok := messageBody["markdown"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected markdown payload, got %#v", messageBody["markdown"])
	}
	if got := markdown["content"]; got != "**标题**\n正文" {
		t.Fatalf("unexpected markdown content: %#v", got)
	}
}

func TestSendWecomAppTextWithAdvancedOptions(t *testing.T) {
	testutil.SetupTestEnv(t)

	var (
		tokenRequested bool
		messageBody    map[string]interface{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokenRequested = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/cgi-bin/message/send":
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldTokenURL := wecomAppTokenURL
	oldSendURL := wecomAppSendURL
	wecomAppTokenURL = server.URL + "/cgi-bin/gettoken"
	wecomAppSendURL = server.URL + "/cgi-bin/message/send"
	defer func() {
		wecomAppTokenURL = oldTokenURL
		wecomAppSendURL = oldSendURL
	}()

	err := sendWecomApp(map[string]string{
		"corp_id":                  "ww-demo",
		"secret":                   "secret-demo",
		"agent_id":                 "1000001",
		"to_user":                  "zhangsan|lisi",
		"msg_type":                 "text",
		"content_template":         "{{title}}\n{{content}}",
		"safe":                     "1",
		"enable_id_trans":          "1",
		"enable_duplicate_check":   "1",
		"duplicate_check_interval": "7200",
	}, "标题", "正文")
	if err != nil {
		t.Fatalf("send wecom app text: %v", err)
	}

	if !tokenRequested {
		t.Fatal("expected token endpoint to be requested")
	}
	if got := messageBody["msgtype"]; got != "text" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	if got := messageBody["touser"]; got != "zhangsan|lisi" {
		t.Fatalf("unexpected touser: %#v", got)
	}
	if got := messageBody["safe"]; got != float64(1) {
		t.Fatalf("unexpected safe: %#v", got)
	}
	if got := messageBody["enable_id_trans"]; got != float64(1) {
		t.Fatalf("unexpected enable_id_trans: %#v", got)
	}
	if got := messageBody["enable_duplicate_check"]; got != float64(1) {
		t.Fatalf("unexpected enable_duplicate_check: %#v", got)
	}
	if got := messageBody["duplicate_check_interval"]; got != float64(7200) {
		t.Fatalf("unexpected duplicate_check_interval: %#v", got)
	}

	textBody, ok := messageBody["text"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected text payload: %#v", messageBody["text"])
	}
	if got := textBody["content"]; got != "标题\n正文" {
		t.Fatalf("unexpected text content: %#v", got)
	}
}

func TestSendWecomAppTemplateCard(t *testing.T) {
	testutil.SetupTestEnv(t)

	var messageBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/cgi-bin/message/send":
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldTokenURL := wecomAppTokenURL
	oldSendURL := wecomAppSendURL
	wecomAppTokenURL = server.URL + "/cgi-bin/gettoken"
	wecomAppSendURL = server.URL + "/cgi-bin/message/send"
	defer func() {
		wecomAppTokenURL = oldTokenURL
		wecomAppSendURL = oldSendURL
	}()

	err := sendWecomApp(map[string]string{
		"corp_id":  "ww-demo",
		"secret":   "secret-demo",
		"agent_id": "1000001",
		"to_user":  "@all",
		"msg_type": "template_card",
		"template_card_payload": `{
			"card_type":"text_notice",
			"main_title":{"title":"{{title}}","desc":"{{content}}"}
		}`,
	}, "系统通知", "任务执行完成")
	if err != nil {
		t.Fatalf("send wecom app template card: %v", err)
	}

	if got := messageBody["msgtype"]; got != "template_card" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	cardBody, ok := messageBody["template_card"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected template_card payload: %#v", messageBody["template_card"])
	}
	mainTitle, ok := cardBody["main_title"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected main_title: %#v", cardBody["main_title"])
	}
	if got := mainTitle["title"]; got != "系统通知" {
		t.Fatalf("unexpected template title: %#v", got)
	}
	if got := mainTitle["desc"]; got != "任务执行完成" {
		t.Fatalf("unexpected template desc: %#v", got)
	}
}

func TestSendWecomAppMpnews(t *testing.T) {
	testutil.SetupTestEnv(t)

	var messageBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/cgi-bin/message/send":
			if err := json.NewDecoder(r.Body).Decode(&messageBody); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldTokenURL := wecomAppTokenURL
	oldSendURL := wecomAppSendURL
	wecomAppTokenURL = server.URL + "/cgi-bin/gettoken"
	wecomAppSendURL = server.URL + "/cgi-bin/message/send"
	defer func() {
		wecomAppTokenURL = oldTokenURL
		wecomAppSendURL = oldSendURL
	}()

	content := "任务执行完成\n第二行输出"

	err := sendWecomApp(map[string]string{
		"corp_id":         "ww-demo",
		"secret":          "secret-demo",
		"agent_id":        "1000001",
		"to_user":         "@all",
		"msg_type":        "mpnews",
		"safe":            "2",
		"enable_id_trans": "1",
		"mpnews_articles": `[
			{
				"title":"{{title}}",
				"thumb_media_id":"MEDIA_ID",
				"author":"Author",
				"content_source_url":"https://example.com/article",
				"content":"<p>{{content}}</p>",
				"digest":"{{content}}"
			}
		]`,
	}, "系统通知", content)
	if err != nil {
		t.Fatalf("send wecom app mpnews: %v", err)
	}

	if got := messageBody["msgtype"]; got != "mpnews" {
		t.Fatalf("unexpected msgtype: %#v", got)
	}
	if got := messageBody["safe"]; got != float64(2) {
		t.Fatalf("unexpected safe: %#v", got)
	}
	if got := messageBody["enable_id_trans"]; got != float64(1) {
		t.Fatalf("unexpected enable_id_trans: %#v", got)
	}
	mpnews, ok := messageBody["mpnews"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected mpnews payload: %#v", messageBody["mpnews"])
	}
	articles, ok := mpnews["articles"].([]interface{})
	if !ok || len(articles) != 1 {
		t.Fatalf("unexpected mpnews articles: %#v", mpnews["articles"])
	}
	article, ok := articles[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected article payload: %#v", articles[0])
	}
	if got := article["title"]; got != "系统通知" {
		t.Fatalf("unexpected article title: %#v", got)
	}
	if got := article["thumb_media_id"]; got != "MEDIA_ID" {
		t.Fatalf("unexpected thumb_media_id: %#v", got)
	}
	// mpnews content 走 HTML 渲染，换行需转成 <br> 才能生效。
	if got := article["content"]; got != "<p>任务执行完成<br>第二行输出</p>" {
		t.Fatalf("unexpected article content: %#v", got)
	}
	// digest 是纯文本路径，同样的 {{content}} 渲染结果不应被换行转换影响，保持原始 \n。
	if got := article["digest"]; got != content {
		t.Fatalf("unexpected article digest: %#v", got)
	}
}

func TestSendWecomAppUsesReverseProxyBaseURL(t *testing.T) {
	testutil.SetupTestEnv(t)

	var (
		tokenRequested bool
		sendRequested  bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxy-qyapi/cgi-bin/gettoken":
			tokenRequested = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/proxy-qyapi/cgi-bin/message/send":
			sendRequested = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		default:
			t.Fatalf("unexpected reverse proxy path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	err := sendWecomApp(map[string]string{
		"corp_id":  "ww-demo",
		"secret":   "secret-demo",
		"agent_id": "1000001",
		"to_user":  "@all",
		"msg_type": "text",
		"base_url": server.URL + "/proxy-qyapi",
	}, "标题", "正文")
	if err != nil {
		t.Fatalf("send wecom app via reverse proxy: %v", err)
	}
	if !tokenRequested || !sendRequested {
		t.Fatalf("expected both reverse proxy endpoints to be used, token=%v send=%v", tokenRequested, sendRequested)
	}
}

func TestSendWxPusherIncludesOptionalFields(t *testing.T) {
	testutil.SetupTestEnv(t)

	var payload map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1000,"msg":"处理成功","success":true}`))
	}))
	defer server.Close()

	err := sendWxPusher(map[string]string{
		"app_token":       "AT_demo",
		"uids":            "UID_demo",
		"content_type":    "3",
		"url":             "https://example.com/detail",
		"verify_pay_type": "2",
		"server":          server.URL,
	}, "标题", "正文")
	if err != nil {
		t.Fatalf("send wxpusher: %v", err)
	}

	if got := payload["url"]; got != "https://example.com/detail" {
		t.Fatalf("unexpected wxpusher url: %#v", got)
	}
	if got := payload["verifyPayType"]; got != float64(2) {
		t.Fatalf("unexpected verifyPayType: %#v", got)
	}
	if got := payload["contentType"]; got != float64(3) {
		t.Fatalf("unexpected contentType: %#v", got)
	}
}

func TestSendWecomAppReturnsEnterpriseError(t *testing.T) {
	testutil.SetupTestEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"token-demo"}`))
		case "/cgi-bin/message/send":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"errcode":40003,"errmsg":"invalid user","invaliduser":"zhangsan|lisi"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	oldTokenURL := wecomAppTokenURL
	oldSendURL := wecomAppSendURL
	wecomAppTokenURL = server.URL + "/cgi-bin/gettoken"
	wecomAppSendURL = server.URL + "/cgi-bin/message/send"
	defer func() {
		wecomAppTokenURL = oldTokenURL
		wecomAppSendURL = oldSendURL
	}()

	err := sendWecomApp(map[string]string{
		"corp_id":  "ww-demo",
		"secret":   "secret-demo",
		"agent_id": "1000001",
		"to_user":  "@all",
		"msg_type": "text",
	}, "标题", "正文")
	if err == nil {
		t.Fatal("expected enterprise error")
	}
	if got := err.Error(); got != "发送企业微信应用消息失败: invalid user (invaliduser=zhangsan|lisi)" {
		t.Fatalf("unexpected error: %s", got)
	}
}
