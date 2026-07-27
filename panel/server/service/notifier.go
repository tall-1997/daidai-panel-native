package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

var (
	wecomAppTokenURL = "https://qyapi.weixin.qq.com/cgi-bin/gettoken"
	wecomAppSendURL  = "https://qyapi.weixin.qq.com/cgi-bin/message/send"

	smtpSendMail                = smtp.SendMail
	smtpSendMailWithImplicitTLS = sendSMTPMailWithImplicitTLS
)

type NotificationDispatchOptions struct {
	ChannelIDs []uint
	Context    map[string]string
}

type NotificationDispatchResult struct {
	SentCount    int
	FailedCount  int
	ChannelNames []string
	Errors       []string
}

func SendNotification(title, content string) {
	SendNotificationWithOptions(title, content, NotificationDispatchOptions{})
}

func SendNotificationWithOptions(title, content string, options NotificationDispatchOptions) {
	channels, err := loadEnabledNotificationChannels(options.ChannelIDs)
	if err != nil {
		log.Printf("load notification channels failed: %v", err)
		return
	}

	if len(channels) == 0 {
		if len(options.ChannelIDs) > 0 {
			log.Printf("notification skipped: no enabled channels matched ids=%v", options.ChannelIDs)
		}
		return
	}

	for _, ch := range channels {
		go dispatchNotificationToChannel(ch, title, content, options.Context)
	}
}

func SendNotificationToChannel(channel *model.NotifyChannel, title, content string) error {
	return sendToChannel(*channel, title, content, nil)
}

func SendNotificationSyncWithOptions(title, content string, options NotificationDispatchOptions) (NotificationDispatchResult, error) {
	result := NotificationDispatchResult{}

	channels, err := loadEnabledNotificationChannels(options.ChannelIDs)
	if err != nil {
		return result, err
	}
	if len(channels) == 0 {
		if len(options.ChannelIDs) > 0 {
			return result, fmt.Errorf("未找到已启用的通知渠道")
		}
		return result, fmt.Errorf("暂无已启用的通知渠道")
	}

	for _, ch := range channels {
		if err := sendToChannel(ch, title, content, options.Context); err != nil {
			result.FailedCount++
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", ch.Name, err))
			continue
		}
		result.SentCount++
		result.ChannelNames = append(result.ChannelNames, ch.Name)
	}

	return result, nil
}

func dispatchNotificationToChannel(ch model.NotifyChannel, title, content string, context map[string]string) {
	if err := sendToChannel(ch, title, content, context); err != nil {
		log.Printf("send notification via channel %d(%s) failed: %v", ch.ID, ch.Name, err)
	}
}

func loadEnabledNotificationChannels(channelIDs []uint) ([]model.NotifyChannel, error) {
	var channels []model.NotifyChannel
	query := database.DB.Where("enabled = ?", true)
	if ids := uniqueNotificationChannelIDs(channelIDs); len(ids) > 0 {
		query = query.Where("id IN ?", ids)
	}
	if err := query.Order("created_at DESC, id DESC").Find(&channels).Error; err != nil {
		return nil, err
	}
	return channels, nil
}

func uniqueNotificationChannelIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}

	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func recordNotificationSend(channelID uint, sentAt time.Time) {
	if channelID == 0 || database.DB == nil {
		return
	}

	todayKey := sentAt.Format("2006-01-02")
	var channel model.NotifyChannel
	if err := database.DB.Select("id", "today_send_count", "today_send_date").First(&channel, channelID).Error; err != nil {
		log.Printf("load notification channel send stats failed: %v", err)
		return
	}

	nextCount := 1
	if channel.TodaySendDate == todayKey {
		nextCount = channel.TodaySendCount + 1
	}

	if err := database.DB.Model(&model.NotifyChannel{}).
		Where("id = ?", channelID).
		Updates(map[string]interface{}{
			"today_send_count": nextCount,
			"today_send_date":  todayKey,
		}).Error; err != nil {
		log.Printf("update notification channel send stats failed: %v", err)
	}
}

func sendToChannel(ch model.NotifyChannel, title, content string, context map[string]string) error {
	var cfg map[string]string
	if err := json.Unmarshal([]byte(ch.Config), &cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// 多面板共用同一通知渠道时，可在标题前缀附带面板名称以便区分；留空则不附带。
	if label := strings.TrimSpace(model.GetRegisteredConfig("notify_panel_label")); label != "" {
		title = "【" + label + "】" + title
	}

	var err error
	switch ch.Type {
	case "webhook":
		err = sendWebhook(cfg, title, content)
	case "email":
		err = sendEmail(cfg, title, content)
	case "telegram":
		err = sendTelegram(cfg, title, content)
	case "dingtalk":
		err = sendDingtalk(cfg, title, content)
	case "wecom":
		err = sendWecomWithContext(cfg, title, content, context)
	case "wecom_app":
		err = sendWecomAppWithContext(cfg, title, content, context)
	case "bark":
		err = sendBarkWithContext(cfg, title, content, context)
	case "pushplus":
		err = sendPushplus(cfg, title, content)
	case "serverchan":
		err = sendServerchan(cfg, title, content)
	case "feishu":
		err = sendFeishu(cfg, title, content)
	case "gotify":
		err = sendGotify(cfg, title, content)
	case "pushdeer":
		err = sendPushdeer(cfg, title, content)
	case "pushme":
		err = sendPushMe(cfg, title, content)
	case "chanify":
		err = sendChanify(cfg, title, content)
	case "igot":
		err = sendIgot(cfg, title, content)
	case "qmsg":
		err = sendQmsg(cfg, title, content)
	case "pushover":
		err = sendPushover(cfg, title, content)
	case "discord":
		err = sendDiscord(cfg, title, content)
	case "slack":
		err = sendSlack(cfg, title, content)
	case "ntfy":
		err = sendNtfy(cfg, title, content)
	case "wxpusher":
		err = sendWxPusher(cfg, title, content)
	case "custom":
		err = sendCustomWebhook(cfg, title, content)
	default:
		err = fmt.Errorf("未知的通知渠道类型: %s", ch.Type)
	}

	if err != nil {
		return err
	}

	recordNotificationSend(ch.ID, time.Now())
	return nil
}

func httpPost(url string, body interface{}, headers map[string]string) error {
	return httpPostWithClient(NewHTTPClient(10*time.Second), url, body, headers)
}

func httpPostWithClient(client *http.Client, url string, body interface{}, headers map[string]string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func sendWebhook(cfg map[string]string, title, content string) error {
	webhookURL := cfg["url"]
	if webhookURL == "" {
		return fmt.Errorf("Webhook URL 为空")
	}
	body := map[string]string{"title": title, "content": content}
	return httpPost(webhookURL, body, nil)
}

func sendEmail(cfg map[string]string, title, content string) error {
	host := strings.TrimSpace(cfg["smtp_host"])
	port := strings.TrimSpace(cfg["smtp_port"])
	user := strings.TrimSpace(cfg["smtp_user"])
	pass := cfg["smtp_pass"]
	to := strings.TrimSpace(cfg["to"])
	from := strings.TrimSpace(cfg["from"])
	if from == "" {
		from = user
	}
	if host == "" {
		return fmt.Errorf("SMTP 主机为空")
	}
	if port == "" {
		return fmt.Errorf("SMTP 端口为空")
	}
	recipients := splitEmailRecipients(to)
	if len(recipients) == 0 {
		return fmt.Errorf("收件人为空")
	}
	if from == "" {
		return fmt.Errorf("发件人为空")
	}

	addr := net.JoinHostPort(host, port)
	auth := smtp.PlainAuth("", user, pass, host)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, to, title, content)

	if smtpImplicitSSLEnabled(cfg, port) {
		return smtpSendMailWithImplicitTLS(addr, host, auth, from, recipients, []byte(msg))
	}
	return smtpSendMail(addr, auth, from, recipients, []byte(msg))
}

func sendSMTPMailWithImplicitTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()

	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp: server doesn't support AUTH")
		}
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(msg); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func smtpImplicitSSLEnabled(cfg map[string]string, port string) bool {
	for _, key := range []string{"smtp_ssl", "smtp_use_ssl", "use_ssl", "enable_ssl", "ssl"} {
		raw, exists := cfg[key]
		if !exists {
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "auto") {
			return strings.TrimSpace(port) == "465"
		}
		return notificationConfigBool(raw, false)
	}
	return strings.TrimSpace(port) == "465"
}

func splitEmailRecipients(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t'
	})

	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func sendTelegram(cfg map[string]string, title, content string) error {
	token := cfg["token"]
	chatID := cfg["chat_id"]
	if token == "" || chatID == "" {
		return fmt.Errorf("Telegram token 或 chat_id 为空")
	}
	apiHost := "https://api.telegram.org"
	if v := cfg["api_host"]; v != "" {
		apiHost = strings.TrimRight(v, "/")
	}
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", apiHost, token)
	client := NewHTTPClientWithProxy(10*time.Second, strings.TrimSpace(cfg["proxy"]))

	messages := buildTelegramMessages(title, content)
	for _, message := range messages {
		body := map[string]interface{}{
			"chat_id": chatID,
			"text":    message,
		}
		if v := strings.TrimSpace(cfg["message_thread_id"]); v != "" {
			if threadID, err := strconv.Atoi(v); err == nil {
				body["message_thread_id"] = threadID
			}
		}
		if err := httpPostWithClient(client, apiURL, body, nil); err != nil {
			return err
		}
	}
	return nil
}

func sendDingtalk(cfg map[string]string, title, content string) error {
	webhook := cfg["webhook"]
	if webhook == "" {
		return fmt.Errorf("钉钉 Webhook URL 为空")
	}
	if secret := cfg["secret"]; secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
		stringToSign := timestamp + "\n" + secret
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		sep := "&"
		if !strings.Contains(webhook, "?") {
			sep = "?"
		}
		webhook = webhook + sep + "timestamp=" + timestamp + "&sign=" + sign
	}

	var body map[string]interface{}
	if strings.ToLower(strings.TrimSpace(cfg["msg_type"])) == "text" {
		body = map[string]interface{}{
			"msgtype": "text",
			"text": map[string]string{
				"content": title + "\n" + content,
			},
		}
	} else {
		mdContent := strings.ReplaceAll(content, "\n", "  \n")
		body = map[string]interface{}{
			"msgtype": "markdown",
			"markdown": map[string]string{
				"title": title,
				"text":  fmt.Sprintf("### %s  \n%s", title, mdContent),
			},
		}
	}
	return httpPost(webhook, body, nil)
}

func sendWecom(cfg map[string]string, title, content string) error {
	return sendWecomWithContext(cfg, title, content, nil)
}

func sendWecomWithContext(cfg map[string]string, title, content string, context map[string]string) error {
	webhook := cfg["webhook"]
	if webhook == "" {
		return fmt.Errorf("企业微信机器人 Webhook URL 为空")
	}

	msgType := strings.ToLower(strings.TrimSpace(cfg["msg_type"]))
	if msgType == "" {
		msgType = "text"
	}

	body := map[string]interface{}{"msgtype": msgType}
	switch msgType {
	case "text":
		textBody := map[string]interface{}{
			"content": renderNotificationTemplateWithContext(cfg["content_template"], title, content, "{{title}}\n{{content}}", context),
		}
		if mentioned := splitNotificationTargets(cfg["mentioned_list"]); len(mentioned) > 0 {
			textBody["mentioned_list"] = mentioned
		}
		if mobiles := splitNotificationTargets(cfg["mentioned_mobile_list"]); len(mobiles) > 0 {
			textBody["mentioned_mobile_list"] = mobiles
		}
		body["text"] = textBody
	case "markdown", "markdown_v2":
		body[msgType] = map[string]string{
			"content": renderNotificationTemplateWithContext(cfg["content_template"], title, content, "**{{title}}**\n{{content}}", context),
		}
	case "image":
		base64Data := strings.TrimSpace(cfg["image_base64"])
		md5Value := strings.TrimSpace(cfg["image_md5"])
		if base64Data == "" || md5Value == "" {
			return fmt.Errorf("企业微信机器人图片消息需要 image_base64 和 image_md5")
		}
		body["image"] = map[string]string{
			"base64": base64Data,
			"md5":    md5Value,
		}
	case "news":
		articles, err := parseNotificationJSONTemplateWithContext(cfg["news_articles"], title, content, context)
		if err != nil {
			return fmt.Errorf("企业微信机器人图文消息配置无效: %w", err)
		}
		articleList, ok := articles.([]interface{})
		if !ok || len(articleList) == 0 {
			return fmt.Errorf("企业微信机器人图文消息需要至少一条 articles")
		}
		body["news"] = map[string]interface{}{
			"articles": articleList,
		}
	case "template_card":
		cardPayload, err := parseNotificationJSONTemplateWithContext(cfg["template_card_payload"], title, content, context)
		if err != nil {
			return fmt.Errorf("企业微信机器人模版卡片配置无效: %w", err)
		}
		cardBody, ok := cardPayload.(map[string]interface{})
		if !ok || len(cardBody) == 0 {
			return fmt.Errorf("企业微信机器人模版卡片配置不能为空对象")
		}
		body["template_card"] = cardBody
	default:
		return fmt.Errorf("不支持的企业微信机器人消息类型: %s", msgType)
	}

	return httpPost(webhook, body, nil)
}

func sendWecomApp(cfg map[string]string, title, content string) error {
	return sendWecomAppWithContext(cfg, title, content, nil)
}

func sendWecomAppWithContext(cfg map[string]string, title, content string, context map[string]string) error {
	corpID := strings.TrimSpace(cfg["corp_id"])
	secret := strings.TrimSpace(cfg["secret"])
	agentID := strings.TrimSpace(cfg["agent_id"])
	if corpID == "" || secret == "" || agentID == "" {
		return fmt.Errorf("企业微信应用 corp_id、secret 或 agent_id 为空")
	}

	agentIDInt, err := strconv.Atoi(agentID)
	if err != nil || agentIDInt <= 0 {
		return fmt.Errorf("企业微信应用 agent_id 无效")
	}

	tokenURL := fmt.Sprintf(
		"%s?corpid=%s&corpsecret=%s",
		resolveWecomAppEndpoint(cfg, wecomAppTokenURL, "/cgi-bin/gettoken"),
		url.QueryEscape(corpID),
		url.QueryEscape(secret),
	)
	client := NewHTTPClient(10 * time.Second)
	tokenResp, err := client.Get(tokenURL)
	if err != nil {
		return fmt.Errorf("获取企业微信应用 access_token 失败: %w", err)
	}
	defer tokenResp.Body.Close()

	tokenBody, _ := io.ReadAll(tokenResp.Body)
	if tokenResp.StatusCode >= 400 {
		return fmt.Errorf("获取企业微信应用 access_token 失败: HTTP %d: %s", tokenResp.StatusCode, strings.TrimSpace(string(tokenBody)))
	}

	var tokenPayload struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(tokenBody, &tokenPayload); err != nil {
		return fmt.Errorf("解析企业微信应用 access_token 响应失败: %w", err)
	}
	if tokenPayload.ErrCode != 0 {
		return fmt.Errorf("获取企业微信应用 access_token 失败: %s", tokenPayload.ErrMsg)
	}
	if strings.TrimSpace(tokenPayload.AccessToken) == "" {
		return fmt.Errorf("企业微信应用 access_token 为空")
	}

	sendURL := fmt.Sprintf(
		"%s?access_token=%s",
		resolveWecomAppEndpoint(cfg, wecomAppSendURL, "/cgi-bin/message/send"),
		url.QueryEscape(tokenPayload.AccessToken),
	)
	msgType := strings.ToLower(strings.TrimSpace(cfg["msg_type"]))
	if msgType == "" {
		msgType = "text"
	}

	receivers := map[string]string{
		"touser":  strings.TrimSpace(cfg["to_user"]),
		"toparty": strings.TrimSpace(cfg["to_party"]),
		"totag":   strings.TrimSpace(cfg["to_tag"]),
	}
	if receivers["touser"] == "" && receivers["toparty"] == "" && receivers["totag"] == "" {
		receivers["touser"] = "@all"
	}

	enableDuplicateCheck := notificationConfigInt(cfg["enable_duplicate_check"], 0)
	duplicateCheckInterval := notificationConfigInt(cfg["duplicate_check_interval"], 1800)
	if duplicateCheckInterval <= 0 {
		duplicateCheckInterval = 1800
	}
	if duplicateCheckInterval > 4*3600 {
		duplicateCheckInterval = 4 * 3600
	}

	body := map[string]interface{}{
		"msgtype":                  msgType,
		"agentid":                  agentIDInt,
		"touser":                   receivers["touser"],
		"toparty":                  receivers["toparty"],
		"totag":                    receivers["totag"],
		"enable_duplicate_check":   enableDuplicateCheck,
		"duplicate_check_interval": duplicateCheckInterval,
	}

	switch msgType {
	case "text":
		body["safe"] = notificationConfigInt(cfg["safe"], 0)
		body["enable_id_trans"] = notificationConfigInt(cfg["enable_id_trans"], 0)
		body["text"] = map[string]string{
			"content": renderNotificationTemplateWithContext(cfg["content_template"], title, content, "{{title}}\n{{content}}", context),
		}
	case "markdown":
		body["markdown"] = map[string]string{
			"content": renderNotificationTemplateWithContext(cfg["content_template"], title, content, "**{{title}}**\n{{content}}", context),
		}
	case "image", "file", "video":
		body["safe"] = notificationConfigInt(cfg["safe"], 0)
		mediaID := strings.TrimSpace(cfg["media_id"])
		if mediaID == "" {
			return fmt.Errorf("企业微信应用 %s 消息需要 media_id", msgType)
		}
		body[msgType] = map[string]string{
			"media_id": mediaID,
		}
	case "news":
		articles, err := parseNotificationJSONTemplateWithContext(cfg["news_articles"], title, content, context)
		if err != nil {
			return fmt.Errorf("企业微信应用图文消息配置无效: %w", err)
		}
		articleList, ok := articles.([]interface{})
		if !ok || len(articleList) == 0 {
			return fmt.Errorf("企业微信应用图文消息需要至少一条 articles")
		}
		body["news"] = map[string]interface{}{
			"articles": articleList,
		}
	case "mpnews":
		body["safe"] = notificationConfigInt(cfg["safe"], 0)
		body["enable_id_trans"] = notificationConfigInt(cfg["enable_id_trans"], 0)
		articles, err := parseNotificationJSONTemplateWithContext(cfg["mpnews_articles"], title, content, context)
		if err != nil {
			return fmt.Errorf("企业微信应用 mpnews 配置无效: %w", err)
		}
		articleList, ok := articles.([]interface{})
		if !ok || len(articleList) == 0 {
			return fmt.Errorf("企业微信应用 mpnews 需要至少一条 articles")
		}
		// mpnews 正文按 HTML 渲染，纯 \n 不会换行；仅把 content 键的换行替换为 <br>，
		// 一次处理 \r\n 与 \n 避免残留孤立 \r。不触碰 digest/title（纯文本路径，\n 正常）。
		contentLineBreakReplacer := strings.NewReplacer("\r\n", "<br>", "\n", "<br>")
		for _, item := range articleList {
			article, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if raw, ok := article["content"].(string); ok {
				article["content"] = contentLineBreakReplacer.Replace(raw)
			}
		}
		body["mpnews"] = map[string]interface{}{
			"articles": articleList,
		}
	case "template_card":
		body["enable_id_trans"] = notificationConfigInt(cfg["enable_id_trans"], 0)
		cardPayload, err := parseNotificationJSONTemplateWithContext(cfg["template_card_payload"], title, content, context)
		if err != nil {
			return fmt.Errorf("企业微信应用模版卡片配置无效: %w", err)
		}
		cardBody, ok := cardPayload.(map[string]interface{})
		if !ok || len(cardBody) == 0 {
			return fmt.Errorf("企业微信应用模版卡片配置不能为空对象")
		}
		body["template_card"] = cardBody
	default:
		return fmt.Errorf("不支持的企业微信应用消息类型: %s", msgType)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, sendURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	sendResp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送企业微信应用消息失败: %w", err)
	}
	defer sendResp.Body.Close()

	sendBody, _ := io.ReadAll(sendResp.Body)
	if sendResp.StatusCode >= 400 {
		return fmt.Errorf("发送企业微信应用消息失败: HTTP %d: %s", sendResp.StatusCode, strings.TrimSpace(string(sendBody)))
	}

	var sendPayload struct {
		ErrCode        int    `json:"errcode"`
		ErrMsg         string `json:"errmsg"`
		InvalidUser    string `json:"invaliduser"`
		InvalidParty   string `json:"invalidparty"`
		InvalidTag     string `json:"invalidtag"`
		UnlicensedUser string `json:"unlicenseduser"`
	}
	if err := json.Unmarshal(sendBody, &sendPayload); err != nil {
		return fmt.Errorf("解析企业微信应用发送响应失败: %w", err)
	}
	if sendPayload.ErrCode != 0 {
		var details []string
		if v := strings.TrimSpace(sendPayload.InvalidUser); v != "" {
			details = append(details, "invaliduser="+v)
		}
		if v := strings.TrimSpace(sendPayload.InvalidParty); v != "" {
			details = append(details, "invalidparty="+v)
		}
		if v := strings.TrimSpace(sendPayload.InvalidTag); v != "" {
			details = append(details, "invalidtag="+v)
		}
		if v := strings.TrimSpace(sendPayload.UnlicensedUser); v != "" {
			details = append(details, "unlicenseduser="+v)
		}
		if len(details) > 0 {
			return fmt.Errorf("发送企业微信应用消息失败: %s (%s)", sendPayload.ErrMsg, strings.Join(details, ", "))
		}
		return fmt.Errorf("发送企业微信应用消息失败: %s", sendPayload.ErrMsg)
	}

	return nil
}

func sendBark(cfg map[string]string, title, content string) error {
	return sendBarkWithContext(cfg, title, content, nil)
}

func sendBarkWithContext(cfg map[string]string, title, content string, context map[string]string) error {
	server := cfg["server"]
	key := cfg["key"]
	if key == "" {
		return fmt.Errorf("Bark Key 为空")
	}
	if server == "" {
		server = "https://api.day.app"
	}
	apiURL := fmt.Sprintf("%s/%s", strings.TrimRight(server, "/"), key)
	body := map[string]string{
		"title": title,
		"body":  content,
	}
	if v := cfg["sound"]; v != "" {
		body["sound"] = v
	}
	if v := cfg["group"]; v != "" {
		body["group"] = v
	}
	if v := cfg["icon"]; v != "" {
		body["icon"] = v
	}
	if v := cfg["level"]; v != "" {
		body["level"] = v
	}
	jumpURL := strings.TrimSpace(context["url"])
	if jumpURL == "" {
		jumpURL = cfg["url"]
	}
	if jumpURL != "" {
		body["url"] = jumpURL
	}
	return httpPost(apiURL, body, nil)
}

func sendPushplus(cfg map[string]string, title, content string) error {
	token := cfg["token"]
	if token == "" {
		return fmt.Errorf("PushPlus Token 为空")
	}
	apiURL := "http://www.pushplus.plus/send"
	body := map[string]string{
		"token":   token,
		"title":   title,
		"content": content,
	}
	if v := cfg["topic"]; v != "" {
		body["topic"] = v
	}
	if v := cfg["template"]; v != "" {
		body["template"] = v
	}
	return httpPost(apiURL, body, nil)
}

func sendServerchan(cfg map[string]string, title, content string) error {
	key := cfg["key"]
	apiURL := fmt.Sprintf("https://sctapi.ftqq.com/%s.send", key)
	body := map[string]string{
		"title": title,
		"desp":  content,
	}
	return httpPost(apiURL, body, nil)
}

func sendFeishu(cfg map[string]string, title, content string) error {
	webhook := cfg["webhook"]
	if webhook == "" {
		return fmt.Errorf("飞书 Webhook URL 为空")
	}
	body := map[string]interface{}{
		"msg_type": "text",
		"content":  map[string]string{"text": fmt.Sprintf("%s\n%s", title, content)},
	}
	if secret := cfg["secret"]; secret != "" {
		timestamp := time.Now().Unix()
		stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
		mac := hmac.New(sha256.New, []byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
		body["timestamp"] = fmt.Sprintf("%d", timestamp)
		body["sign"] = sign
	}
	return httpPost(webhook, body, nil)
}

func sendGotify(cfg map[string]string, title, content string) error {
	server := cfg["server"]
	token := cfg["token"]
	if server == "" || token == "" {
		return fmt.Errorf("Gotify 服务器地址或 Token 为空")
	}
	apiURL := fmt.Sprintf("%s/message", strings.TrimRight(server, "/"))
	priority := 5
	if v := cfg["priority"]; v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			priority = p
		}
	}
	body := map[string]interface{}{
		"title":    title,
		"message":  content,
		"priority": priority,
	}
	return httpPost(apiURL, body, map[string]string{"X-Gotify-Key": token})
}

func sendPushdeer(cfg map[string]string, title, content string) error {
	server := cfg["server"]
	key := cfg["key"]
	if server == "" {
		server = "https://api2.pushdeer.com"
	}
	apiURL := fmt.Sprintf("%s/message/push", strings.TrimRight(server, "/"))
	body := map[string]string{
		"pushkey": key,
		"text":    title,
		"desp":    content,
	}
	return httpPost(apiURL, body, nil)
}

func sendPushMe(cfg map[string]string, title, content string) error {
	server := strings.TrimSpace(cfg["server"])
	if server == "" {
		server = "https://push.i-i.me"
	}

	pushKey := strings.TrimSpace(cfg["key"])
	if pushKey == "" {
		return fmt.Errorf("PushMe push_key 为空")
	}

	form := url.Values{}
	form.Set("push_key", pushKey)
	form.Set("title", title)
	form.Set("content", content)
	if messageType := strings.TrimSpace(cfg["message_type"]); messageType != "" {
		form.Set("type", messageType)
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(server, "/"), strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	responseText := strings.TrimSpace(string(body))
	if responseText != "" && responseText != "success" && !strings.HasPrefix(responseText, "{") {
		return fmt.Errorf("PushMe 返回异常: %s", responseText)
	}

	return nil
}

func sendChanify(cfg map[string]string, title, content string) error {
	server := cfg["server"]
	token := cfg["token"]
	if server == "" {
		server = "https://api.chanify.net"
	}
	apiURL := fmt.Sprintf("%s/v1/sender/%s", strings.TrimRight(server, "/"), token)
	body := map[string]string{
		"title": title,
		"text":  content,
	}
	return httpPost(apiURL, body, nil)
}

func sendIgot(cfg map[string]string, title, content string) error {
	key := cfg["key"]
	apiURL := fmt.Sprintf("https://push.hellyw.com/%s", key)
	body := map[string]string{
		"title":   title,
		"content": content,
	}
	return httpPost(apiURL, body, nil)
}

func sendQmsg(cfg map[string]string, title, content string) error {
	key := strings.TrimSpace(cfg["key"])
	if key == "" {
		return fmt.Errorf("Qmsg Key 为空")
	}

	mode := strings.ToLower(strings.TrimSpace(cfg["mode"]))
	path := "send"
	if mode == "group" {
		path = "group"
	}

	apiURL := fmt.Sprintf("https://qmsg.zendee.cn/%s/%s", path, key)
	form := url.Values{}
	form.Set("msg", fmt.Sprintf("%s\n%s", title, content))
	if qq := strings.TrimSpace(cfg["qq"]); qq != "" {
		form.Set("qq", qq)
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool   `json:"success"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("Qmsg 返回无法解析: %s", strings.TrimSpace(string(body)))
	}
	if !result.Success {
		return fmt.Errorf("Qmsg 发送失败: %s", strings.TrimSpace(result.Reason))
	}

	return nil
}

func sendPushover(cfg map[string]string, title, content string) error {
	token := cfg["token"]
	user := cfg["user"]
	apiURL := "https://api.pushover.net/1/messages.json"
	body := map[string]string{
		"token":   token,
		"user":    user,
		"title":   title,
		"message": content,
	}
	return httpPost(apiURL, body, nil)
}

func sendDiscord(cfg map[string]string, title, content string) error {
	webhook := cfg["webhook"]
	body := map[string]interface{}{
		"embeds": []map[string]interface{}{
			{
				"title":       title,
				"description": content,
				"color":       3447003,
			},
		},
	}
	return httpPost(webhook, body, nil)
}

func sendSlack(cfg map[string]string, title, content string) error {
	webhook := cfg["webhook"]
	body := map[string]interface{}{
		"text": fmt.Sprintf("*%s*\n\n%s", title, content),
	}
	return httpPost(webhook, body, nil)
}

func sendNtfy(cfg map[string]string, title, content string) error {
	server := cfg["server"]
	topic := cfg["topic"]
	if topic == "" {
		return fmt.Errorf("ntfy Topic 为空")
	}
	if server == "" {
		server = "https://ntfy.sh"
	}
	apiURL := fmt.Sprintf("%s/%s", strings.TrimRight(server, "/"), topic)

	req, err := http.NewRequest("POST", apiURL, strings.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	if v := cfg["priority"]; v != "" {
		req.Header.Set("Priority", v)
	}
	if v := cfg["token"]; v != "" {
		req.Header.Set("Authorization", "Bearer "+v)
	}

	client := NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func sendWxPusher(cfg map[string]string, title, content string) error {
	appToken := strings.TrimSpace(cfg["app_token"])
	if appToken == "" {
		return fmt.Errorf("WxPusher appToken 为空")
	}

	uids := splitNotificationTargets(cfg["uids"])
	topicIDs, err := splitNotificationIntTargets(cfg["topic_ids"])
	if err != nil {
		return fmt.Errorf("WxPusher Topic ID 格式错误: %w", err)
	}
	if len(uids) == 0 && len(topicIDs) == 0 {
		return fmt.Errorf("WxPusher 至少需要一个 UID 或 Topic ID")
	}

	contentType := 1
	if raw := strings.TrimSpace(cfg["content_type"]); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			contentType = parsed
		}
	}

	messageContent := fmt.Sprintf("%s\n%s", title, content)
	switch contentType {
	case 2:
		messageContent = fmt.Sprintf(
			"<h1>%s</h1><br/><div style='white-space: pre-wrap;'>%s</div>",
			html.EscapeString(title),
			html.EscapeString(content),
		)
	case 3:
		messageContent = fmt.Sprintf("## %s\n\n%s", title, content)
	}

	body := map[string]interface{}{
		"appToken":    appToken,
		"content":     messageContent,
		"summary":     title,
		"contentType": contentType,
	}
	if jumpURL := strings.TrimSpace(cfg["url"]); jumpURL != "" {
		body["url"] = jumpURL
	}
	if verifyPayType := strings.TrimSpace(cfg["verify_pay_type"]); verifyPayType != "" {
		if parsed, err := strconv.Atoi(verifyPayType); err == nil {
			body["verifyPayType"] = parsed
		}
	}
	if len(uids) > 0 {
		body["uids"] = uids
	}
	if len(topicIDs) > 0 {
		body["topicIds"] = topicIDs
	}

	apiURL := "https://wxpusher.zjiecode.com/api/send/message"
	if server := strings.TrimSpace(cfg["server"]); server != "" {
		apiURL = strings.TrimRight(server, "/")
	}

	client := NewHTTPClient(10 * time.Second)
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Success bool   `json:"success"`
		Code    int    `json:"code"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &result); err == nil {
		if !result.Success && result.Code != 1000 {
			return fmt.Errorf("WxPusher 发送失败: %s", strings.TrimSpace(result.Msg))
		}
	}

	return nil
}

func resolveWecomAppEndpoint(cfg map[string]string, fallbackURL, path string) string {
	baseURL := strings.TrimSpace(cfg["base_url"])
	if baseURL == "" {
		return fallbackURL
	}

	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/cgi-bin") {
		return baseURL + strings.TrimPrefix(path, "/cgi-bin")
	}
	return baseURL + path
}

func splitNotificationTargets(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func splitNotificationIntTargets(raw string) ([]int, error) {
	fields := splitNotificationTargets(raw)
	result := make([]int, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return nil, fmt.Errorf("无效整数 %q", field)
		}
		result = append(result, value)
	}
	return result, nil
}

func buildTelegramMessages(title, content string) []string {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)

	contentChunks := splitNotificationContentChunks(content, 3200)
	if len(contentChunks) == 0 {
		contentChunks = []string{""}
	}

	messages := make([]string, 0, len(contentChunks))
	total := len(contentChunks)
	for index, chunk := range contentChunks {
		header := title
		if total > 1 {
			header = fmt.Sprintf("%s (%d/%d)", title, index+1, total)
		}
		if strings.TrimSpace(chunk) == "" {
			messages = append(messages, header)
			continue
		}
		messages = append(messages, header+"\n"+chunk)
	}

	return messages
}

func splitNotificationContentChunks(content string, limit int) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if limit <= 0 {
		return []string{content}
	}

	runes := []rune(content)
	chunks := make([]string, 0, len(runes)/limit+1)
	for start := 0; start < len(runes); {
		end := start + limit
		if end >= len(runes) {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}

		splitAt := end
		for idx := end; idx > start+limit/2; idx-- {
			if runes[idx-1] == '\n' {
				splitAt = idx
				break
			}
		}
		if splitAt <= start {
			splitAt = end
		}
		chunks = append(chunks, strings.TrimSpace(string(runes[start:splitAt])))
		start = splitAt
	}

	return chunks
}

func notificationConfigInt(raw string, defaultValue int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return value
}

func notificationConfigBool(raw string, defaultValue bool) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "1", "true", "yes", "on", "enable", "enabled":
		return true
	case "0", "false", "no", "off", "disable", "disabled":
		return false
	default:
		return defaultValue
	}
}

func renderNotificationTemplate(template, title, content, fallback string) string {
	return renderNotificationTemplateWithContext(template, title, content, fallback, nil)
}

func renderNotificationTemplateWithContext(template, title, content, fallback string, context map[string]string) string {
	template = strings.TrimSpace(template)
	if template == "" {
		template = fallback
	}
	template = strings.ReplaceAll(template, "{{title}}", title)
	template = strings.ReplaceAll(template, "{{content}}", content)
	for key, value := range context {
		placeholder := "{{" + strings.TrimSpace(key) + "}}"
		if placeholder == "{{}}" {
			continue
		}
		template = strings.ReplaceAll(template, placeholder, value)
	}
	return template
}

func parseNotificationJSONTemplate(raw, title, content string) (interface{}, error) {
	return parseNotificationJSONTemplateWithContext(raw, title, content, nil)
}

func parseNotificationJSONTemplateWithContext(raw, title, content string, context map[string]string) (interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("JSON 模板为空")
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return renderNotificationJSONValueWithContext(payload, title, content, context), nil
}

func renderNotificationJSONValue(value interface{}, title, content string) interface{} {
	return renderNotificationJSONValueWithContext(value, title, content, nil)
}

func renderNotificationJSONValueWithContext(value interface{}, title, content string, context map[string]string) interface{} {
	switch v := value.(type) {
	case string:
		return renderNotificationTemplateWithContext(v, title, content, v, context)
	case []interface{}:
		items := make([]interface{}, 0, len(v))
		for _, item := range v {
			items = append(items, renderNotificationJSONValueWithContext(item, title, content, context))
		}
		return items
	case map[string]interface{}:
		result := make(map[string]interface{}, len(v))
		for key, item := range v {
			result[key] = renderNotificationJSONValueWithContext(item, title, content, context)
		}
		return result
	default:
		return value
	}
}

func sendCustomWebhook(cfg map[string]string, title, content string) error {
	webhookURL := cfg["url"]
	method := cfg["method"]
	if method == "" {
		method = "POST"
	}

	bodyTemplate := cfg["body"]
	if bodyTemplate == "" {
		bodyTemplate = `{"title":"{{title}}","content":"{{content}}"}`
	}
	bodyStr := strings.ReplaceAll(bodyTemplate, "{{title}}", title)
	bodyStr = strings.ReplaceAll(bodyStr, "{{content}}", content)

	req, err := http.NewRequest(method, webhookURL, strings.NewReader(bodyStr))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", cfg["content_type"])
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if headerStr := cfg["headers"]; headerStr != "" {
		var headers map[string]string
		if json.Unmarshal([]byte(headerStr), &headers) == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	client := NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
