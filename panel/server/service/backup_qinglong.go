package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/model"
	panelcron "daidai-panel/pkg/cron"

	_ "github.com/glebarez/sqlite"
)

func buildQingLongManifest(extractedDir string) (BackupManifest, error) {
	dataDir, err := resolveQingLongDataDir(extractedDir)
	if err != nil {
		return BackupManifest{}, err
	}

	manifest := BackupManifest{
		Format:    "daidai-panel-backup",
		Version:   "0.4.0",
		Source:    "qinglong",
		CreatedAt: time.Now(),
	}

	configPath := filepath.Join(dataDir, "config", "config.sh")
	if _, err := os.Stat(configPath); err == nil {
		configs, channels, err := parseQingLongConfig(configPath)
		if err != nil {
			return BackupManifest{}, err
		}
		if len(configs) > 0 {
			manifest.Selection.Configs = true
			manifest.Data.Configs.SystemConfigs = append(manifest.Data.Configs.SystemConfigs, configs...)
		}
		if len(channels) > 0 {
			manifest.Selection.Configs = true
			manifest.Data.Configs.NotifyChannels = append(manifest.Data.Configs.NotifyChannels, channels...)
		}
	}

	dbPath := filepath.Join(dataDir, "db", "database.sqlite")
	if _, err := os.Stat(dbPath); err == nil {
		if err := enrichManifestFromQingLongDB(dbPath, &manifest); err != nil {
			return BackupManifest{}, err
		}
	}

	if hasAnyQingLongScriptFiles(dataDir) {
		manifest.Selection.Scripts = true
	}
	if hasAnyQingLongLogFiles(dataDir) {
		manifest.Selection.Logs = true
	}
	if !manifest.Selection.Any() {
		return BackupManifest{}, fmt.Errorf("未识别到可导入的青龙备份内容")
	}

	return manifest, nil
}

func resolveQingLongDataDir(extractedDir string) (string, error) {
	candidates := []string{
		filepath.Join(extractedDir, "data"),
		extractedDir,
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "config")); err == nil {
			if _, err := os.Stat(filepath.Join(candidate, "db")); err == nil {
				return candidate, nil
			}
		}
	}

	var nested []string
	_ = filepath.Walk(extractedDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		if path == extractedDir {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "config")); err == nil {
			if _, err := os.Stat(filepath.Join(path, "db")); err == nil {
				nested = append(nested, path)
			}
		}
		return nil
	})
	if len(nested) > 0 {
		sort.Slice(nested, func(i, j int) bool {
			return len(nested[i]) < len(nested[j])
		})
		return nested[0], nil
	}
	return "", fmt.Errorf("未检测到青龙备份目录结构")
}

func parseQingLongConfig(configPath string) ([]model.SystemConfig, []BackupNotifyChannel, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("读取青龙配置失败: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	configs := make([]model.SystemConfig, 0)
	exportedVars := make(map[string]string)
	seenConfigKeys := map[string]struct{}{}

	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		isExport := false
		if strings.HasPrefix(line, "export ") {
			isExport = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := normalizeQingLongShellValue(parts[1])
		if key == "" {
			continue
		}

		mappedKey, mappedValue, ok := mapQingLongConfigToSystemConfig(key, value)
		if ok {
			if _, exists := seenConfigKeys[mappedKey]; exists {
				continue
			}
			seenConfigKeys[mappedKey] = struct{}{}
			configs = append(configs, model.SystemConfig{Key: mappedKey, Value: mappedValue})
			continue
		}

		if !isExport {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		exportedVars[key] = value
	}

	channels := buildQingLongNotificationChannels(exportedVars)

	return configs, channels, nil
}

func normalizeQingLongShellValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

func mapQingLongConfigToSystemConfig(key, value string) (string, string, bool) {
	switch key {
	case "AutoAddCron":
		return buildNormalizedSystemConfig("auto_add_cron", value)
	case "AutoDelCron":
		return buildNormalizedSystemConfig("auto_del_cron", value)
	case "DefaultCronRule":
		return buildNormalizedSystemConfig("default_cron_rule", value)
	case "RepoFileExtensions":
		return buildNormalizedSystemConfig("repo_file_extensions", value)
	case "ProxyUrl":
		return buildNormalizedSystemConfig("proxy_url", value)
	case "CpuWarn":
		return buildNormalizedSystemConfig("cpu_warn", value)
	case "MemoryWarn":
		return buildNormalizedSystemConfig("memory_warn", value)
	case "DiskWarn":
		return buildNormalizedSystemConfig("disk_warn", value)
	case "RandomDelay":
		return buildNormalizedSystemConfig("random_delay", value)
	case "RandomDelayFileExtensions":
		return buildNormalizedSystemConfig("random_delay_extensions", value)
	default:
		return "", "", false
	}
}

func buildNormalizedSystemConfig(key, value string) (string, string, bool) {
	normalized, err := model.NormalizeSystemConfigValue(key, value)
	if err != nil {
		return "", "", false
	}
	return key, normalized, true
}

func parseQingLongDurationSeconds(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days <= 0 {
			return 0, false
		}
		return days * 24 * 3600, true
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, false
	}
	return int(duration.Seconds()), true
}

func buildQingLongNotificationChannels(env map[string]string) []BackupNotifyChannel {
	channels := make([]BackupNotifyChannel, 0)

	appendChannel := func(name, channelType string, cfg map[string]string) {
		if len(cfg) == 0 {
			return
		}
		configJSON, err := json.Marshal(cfg)
		if err != nil {
			return
		}
		channels = append(channels, BackupNotifyChannel{
			Name:    name,
			Type:    channelType,
			Config:  string(configJSON),
			Enabled: true,
		})
	}

	if key := strings.TrimSpace(env["PUSH_KEY"]); key != "" {
		appendChannel("青龙导入 - Server酱", "serverchan", map[string]string{
			"key": key,
		})
	}

	if barkCfg := buildQingLongBarkConfig(env); len(barkCfg) > 0 {
		appendChannel("青龙导入 - Bark", "bark", barkCfg)
	}

	if token := strings.TrimSpace(env["TG_BOT_TOKEN"]); token != "" {
		if chatID := strings.TrimSpace(env["TG_USER_ID"]); chatID != "" {
			cfg := map[string]string{
				"token":   token,
				"chat_id": chatID,
			}
			if apiHost := strings.TrimSpace(env["TG_API_HOST"]); apiHost != "" {
				if !strings.HasPrefix(apiHost, "http://") && !strings.HasPrefix(apiHost, "https://") {
					apiHost = "https://" + apiHost
				}
				cfg["api_host"] = apiHost
			}
			appendChannel("青龙导入 - Telegram", "telegram", cfg)
		}
	}

	if token := strings.TrimSpace(env["DD_BOT_TOKEN"]); token != "" {
		cfg := map[string]string{
			"webhook": fmt.Sprintf("https://oapi.dingtalk.com/robot/send?access_token=%s", token),
		}
		if secret := strings.TrimSpace(env["DD_BOT_SECRET"]); secret != "" {
			cfg["secret"] = secret
		}
		appendChannel("青龙导入 - 钉钉", "dingtalk", cfg)
	}

	if key := strings.TrimSpace(env["QYWX_KEY"]); key != "" {
		appendChannel("青龙导入 - 企业微信机器人", "wecom", map[string]string{
			"webhook": fmt.Sprintf("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=%s", key),
		})
	}

	if appCfg := buildQingLongWecomAppConfig(env); len(appCfg) > 0 {
		appendChannel("青龙导入 - 企业微信应用", "wecom_app", appCfg)
	}

	if token := strings.TrimSpace(env["PUSH_PLUS_TOKEN"]); token != "" {
		cfg := map[string]string{
			"token": token,
		}
		if topic := strings.TrimSpace(env["PUSH_PLUS_USER"]); topic != "" {
			cfg["topic"] = topic
		}
		if template := strings.TrimSpace(env["PUSH_PLUS_TEMPLATE"]); template != "" {
			cfg["template"] = template
		}
		appendChannel("青龙导入 - PushPlus", "pushplus", cfg)
	}

	if gotifyURL := strings.TrimSpace(env["GOTIFY_URL"]); gotifyURL != "" {
		if token := strings.TrimSpace(env["GOTIFY_TOKEN"]); token != "" {
			cfg := map[string]string{
				"server": gotifyURL,
				"token":  token,
			}
			if priority := strings.TrimSpace(env["GOTIFY_PRIORITY"]); priority != "" {
				cfg["priority"] = priority
			}
			appendChannel("青龙导入 - Gotify", "gotify", cfg)
		}
	}

	if key := strings.TrimSpace(env["DEER_KEY"]); key != "" {
		cfg := map[string]string{
			"key": key,
		}
		if server := strings.TrimSpace(env["DEER_URL"]); server != "" {
			cfg["server"] = server
		}
		appendChannel("青龙导入 - PushDeer", "pushdeer", cfg)
	}

	if key := strings.TrimSpace(env["PUSHME_KEY"]); key != "" {
		cfg := map[string]string{
			"key": key,
		}
		if server := strings.TrimSpace(env["PUSHME_URL"]); server != "" {
			cfg["server"] = server
		}
		appendChannel("青龙导入 - PushMe", "pushme", cfg)
	}

	if key := strings.TrimSpace(env["IGOT_PUSH_KEY"]); key != "" {
		appendChannel("青龙导入 - iGot", "igot", map[string]string{
			"key": key,
		})
	}

	if key := strings.TrimSpace(env["QMSG_KEY"]); key != "" {
		cfg := map[string]string{
			"key": key,
		}
		if mode := strings.ToLower(strings.TrimSpace(env["QMSG_TYPE"])); mode != "" {
			cfg["mode"] = mode
		}
		appendChannel("青龙导入 - Qmsg", "qmsg", cfg)
	}

	if key := strings.TrimSpace(env["FSKEY"]); key != "" {
		cfg := map[string]string{
			"webhook": fmt.Sprintf("https://open.feishu.cn/open-apis/bot/v2/hook/%s", key),
		}
		if secret := strings.TrimSpace(env["FSSECRET"]); secret != "" {
			cfg["secret"] = secret
		}
		appendChannel("青龙导入 - 飞书", "feishu", cfg)
	}

	if topic := strings.TrimSpace(env["NTFY_TOPIC"]); topic != "" {
		cfg := map[string]string{
			"topic": topic,
		}
		if server := strings.TrimSpace(env["NTFY_URL"]); server != "" {
			cfg["server"] = server
		}
		if priority := strings.TrimSpace(env["NTFY_PRIORITY"]); priority != "" {
			cfg["priority"] = priority
		}
		if token := strings.TrimSpace(env["NTFY_TOKEN"]); token != "" {
			cfg["token"] = token
		}
		appendChannel("青龙导入 - ntfy", "ntfy", cfg)
	}

	if appToken := strings.TrimSpace(env["WXPUSHER_APP_TOKEN"]); appToken != "" {
		cfg := map[string]string{
			"app_token":    appToken,
			"content_type": "2",
		}
		copyIfNotEmpty(cfg, "topic_ids", env["WXPUSHER_TOPIC_IDS"])
		copyIfNotEmpty(cfg, "uids", env["WXPUSHER_UIDS"])
		if cfg["topic_ids"] != "" || cfg["uids"] != "" {
			appendChannel("青龙导入 - WxPusher", "wxpusher", cfg)
		}
	}

	if customCfg := buildQingLongCustomWebhookConfig(env); len(customCfg) > 0 {
		appendChannel("青龙导入 - 自定义通知", "custom", customCfg)
	}

	if emailCfg := buildQingLongEmailConfig(env); len(emailCfg) > 0 {
		appendChannel("青龙导入 - SMTP 邮件", "email", emailCfg)
	}

	return channels
}

func buildQingLongBarkConfig(env map[string]string) map[string]string {
	pushValue := strings.TrimSpace(env["BARK_PUSH"])
	if pushValue == "" {
		return nil
	}

	cfg := map[string]string{}
	if strings.HasPrefix(pushValue, "http://") || strings.HasPrefix(pushValue, "https://") {
		if parsed, err := url.Parse(pushValue); err == nil {
			key := strings.Trim(strings.TrimSpace(parsed.Path), "/")
			if key != "" {
				cfg["key"] = filepath.Base(key)
				serverPath := strings.TrimSuffix(parsed.Path, "/"+cfg["key"])
				cfg["server"] = strings.TrimRight(parsed.Scheme+"://"+parsed.Host+serverPath, "/")
			}
		}
	}
	if cfg["key"] == "" {
		cfg["key"] = pushValue
	}
	if cfg["server"] == "" {
		delete(cfg, "server")
	}

	copyIfNotEmpty(cfg, "icon", env["BARK_ICON"])
	copyIfNotEmpty(cfg, "sound", env["BARK_SOUND"])
	copyIfNotEmpty(cfg, "group", env["BARK_GROUP"])
	copyIfNotEmpty(cfg, "level", env["BARK_LEVEL"])
	copyIfNotEmpty(cfg, "url", env["BARK_URL"])

	return cfg
}

func buildQingLongCustomWebhookConfig(env map[string]string) map[string]string {
	webhookURL := strings.TrimSpace(env["WEBHOOK_URL"])
	if webhookURL == "" {
		return nil
	}

	cfg := map[string]string{
		"url": webhookURL,
	}
	copyIfNotEmpty(cfg, "method", env["WEBHOOK_METHOD"])
	copyIfNotEmpty(cfg, "content_type", env["WEBHOOK_CONTENT_TYPE"])
	copyIfNotEmpty(cfg, "body", env["WEBHOOK_BODY"])

	if headers := normalizeQingLongWebhookHeaders(env["WEBHOOK_HEADERS"]); headers != "" {
		cfg["headers"] = headers
	}

	return cfg
}

func buildQingLongEmailConfig(env map[string]string) map[string]string {
	server := strings.TrimSpace(env["SMTP_SERVER"])
	email := strings.TrimSpace(env["SMTP_EMAIL"])
	password := strings.TrimSpace(env["SMTP_PASSWORD"])
	if server == "" || email == "" || password == "" {
		return nil
	}

	host := server
	port := "25"
	if strings.Contains(server, ":") {
		host, port, _ = strings.Cut(server, ":")
		host = strings.TrimSpace(host)
		port = strings.TrimSpace(port)
	}
	if host == "" || port == "" {
		return nil
	}

	cfg := map[string]string{
		"smtp_host": host,
		"smtp_port": port,
		"smtp_user": email,
		"smtp_pass": password,
		"to":        email,
		"from":      email,
	}
	if port == "465" {
		cfg["smtp_ssl"] = "true"
	}
	return cfg
}

func buildQingLongWecomAppConfig(env map[string]string) map[string]string {
	raw := strings.TrimSpace(env["QYWX_AM"])
	if raw == "" {
		return nil
	}

	parts := splitQingLongNotifyParts(raw)
	if len(parts) < 4 {
		return nil
	}

	cfg := map[string]string{
		"corp_id":  parts[0],
		"secret":   parts[1],
		"to_user":  parts[2],
		"agent_id": parts[3],
	}
	if len(parts) >= 5 && strings.TrimSpace(parts[4]) != "" {
		cfg["msg_type"] = strings.TrimSpace(parts[4])
	}
	return cfg
}

func splitQingLongNotifyParts(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func normalizeQingLongWebhookHeaders(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		return raw
	}

	headers := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(raw, `\n`, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if len(headers) == 0 {
		return ""
	}

	data, err := json.Marshal(headers)
	if err != nil {
		return ""
	}
	return string(data)
}

func copyIfNotEmpty(target map[string]string, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		target[key] = value
	}
}

func enrichManifestFromQingLongDB(dbPath string, manifest *BackupManifest) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("打开青龙数据库失败: %w", err)
	}
	defer db.Close()

	envs, err := loadQingLongEnvVars(db)
	if err != nil {
		return err
	}
	if len(envs) > 0 {
		manifest.Selection.EnvVars = true
		for _, env := range envs {
			manifest.Data.EnvVars = append(manifest.Data.EnvVars, backupEnvVarFromModel(env))
		}
	}

	tasks, err := loadQingLongTasks(db)
	if err != nil {
		return err
	}
	if len(tasks) > 0 {
		manifest.Selection.Tasks = true
		manifest.Data.Tasks = tasks
	}

	subs, err := loadQingLongSubscriptions(db)
	if err != nil {
		return err
	}
	if len(subs) > 0 {
		manifest.Selection.Subscriptions = true
		manifest.Data.Subscriptions = subs
	}

	deps, err := loadQingLongDependencies(db)
	if err != nil {
		return err
	}
	if len(deps) > 0 {
		manifest.Selection.Dependencies = true
		manifest.Data.Dependencies = deps
	}

	apps, err := loadQingLongApps(db)
	if err != nil {
		return err
	}
	if len(apps) > 0 {
		manifest.Selection.Configs = true
		manifest.Data.Configs.OpenApps = apps
	}

	return nil
}

func loadQingLongEnvVars(db *sql.DB) ([]model.EnvVar, error) {
	rows, err := loadSQLiteTableRows(db, "Envs")
	if err != nil {
		return nil, fmt.Errorf("读取青龙环境变量失败: %w", err)
	}

	type qlEnv struct {
		id       int64
		name     string
		value    string
		remarks  string
		status   int64
		position float64
	}
	envs := make([]qlEnv, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(sqliteRowString(row, "name"))
		value := sqliteRowString(row, "value")
		if name == "" {
			continue
		}
		envs = append(envs, qlEnv{
			id:       sqliteRowInt(row, "id"),
			name:     name,
			value:    value,
			remarks:  sqliteRowString(row, "remarks"),
			status:   sqliteRowInt(row, "status"),
			position: sqliteRowFloat(row, "position"),
		})
	}

	sort.SliceStable(envs, func(i, j int) bool {
		if envs[i].position == envs[j].position {
			return envs[i].id < envs[j].id
		}
		return envs[i].position < envs[j].position
	})

	result := make([]model.EnvVar, 0, len(envs))
	for i, item := range envs {
		position := item.position
		if position <= 0 {
			position = float64(i + 1)
		}
		result = append(result, model.EnvVar{
			Name:      item.name,
			Value:     item.value,
			Remarks:   item.remarks,
			Enabled:   item.status == 0,
			Position:  position,
			SortOrder: 0,
		})
	}

	return result, nil
}

func loadQingLongTasks(db *sql.DB) ([]model.Task, error) {
	rows, err := loadSQLiteTableRows(db, "Crontabs")
	if err != nil {
		return nil, fmt.Errorf("读取青龙定时任务失败: %w", err)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return sqliteRowInt(rows[i], "id") < sqliteRowInt(rows[j], "id")
	})

	var result []model.Task
	for _, row := range rows {
		schedule := strings.TrimSpace(firstNonEmptySQLiteString(row, "schedule", "cron"))
		if !panelcron.Parse(schedule).Valid {
			continue
		}

		command := normalizeQingLongTaskCommand(firstNonEmptySQLiteString(row, "command"))
		name := strings.TrimSpace(firstNonEmptySQLiteString(row, "name"))
		task := model.Task{
			ID:                     uint(sqliteRowInt(row, "id")),
			Name:                   name,
			Command:                command,
			CronExpression:         schedule,
			TaskType:               model.TaskTypeCron,
			Status:                 model.TaskStatusEnabled,
			Timeout:                0,
			MaxRetries:             0,
			RetryInterval:          60,
			NotifyOnFailure:        true,
			NotifyOnSuccess:        false,
			AllowMultipleInstances: sqliteRowBool(row, "allow_multiple_instances"),
		}
		if task.Name == "" {
			task.Name = deriveTaskNameFromCommand(task.Command)
		}
		if sqliteRowBool(row, "isDisabled") || sqliteRowBool(row, "is_disabled") {
			task.Status = model.TaskStatusDisabled
		}
		if value := strings.TrimSpace(firstNonEmptySQLiteString(row, "task_before")); value != "" {
			task.TaskBefore = &value
		}
		if value := strings.TrimSpace(firstNonEmptySQLiteString(row, "task_after")); value != "" {
			task.TaskAfter = &value
		}
		labelsJSON := strings.TrimSpace(firstNonEmptySQLiteString(row, "labels"))
		if labelsJSON != "" {
			var labels []string
			if err := json.Unmarshal([]byte(labelsJSON), &labels); err == nil {
				task.SetLabelsFromSlice(labels)
			}
		}

		result = append(result, task)
	}

	return result, nil
}

// 面板命令文法（见 ParseCommandExecutionPlan）要求首 token 必须是 task / desi / 已知解释器之一，
// 否则无法运行。青龙 Crontabs.command 存的命令不一定满足该规范：
//   - 有的是裸脚本（"123.py" / "/ql/scripts/123.py"），缺 task 前缀，导入后面板跑不了；
//   - 有的带青龙绝对路径前缀（"task /ql/scripts/123.py"），路径在面板脚本目录里找不到。
//
// 这里在导入时把命令归一化成面板可执行的 "task <相对脚本目录路径>"。
var qingLongRecognizedCommandHeads = map[string]struct{}{
	"task": {}, "desi": {},
	"python": {}, "python3": {}, "python3.10": {}, "python3.11": {}, "python3.12": {},
	"node": {}, "ts-node": {}, "bash": {}, "go": {},
}

// 青龙脚本根目录的常见前缀，导入面板后脚本会落到面板脚本目录，需要剥成相对路径。
var qingLongScriptPathPrefixes = []string{
	"/ql/data/scripts/",
	"/ql/scripts/",
	"ql/data/scripts/",
	"ql/scripts/",
	"/scripts/",
	"scripts/",
}

func stripQingLongScriptPrefix(token string) string {
	for _, prefix := range qingLongScriptPathPrefixes {
		if strings.HasPrefix(token, prefix) {
			return strings.TrimPrefix(token, prefix)
		}
	}
	return token
}

func normalizeQingLongTaskCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}

	tokens := strings.Fields(command)
	if len(tokens) == 0 {
		return command
	}

	if _, recognized := qingLongRecognizedCommandHeads[tokens[0]]; recognized {
		// 已是面板可识别格式：只清理后续 token 里的青龙路径前缀，首 token（task/解释器）保持不动。
		for i := 1; i < len(tokens); i++ {
			tokens[i] = stripQingLongScriptPrefix(tokens[i])
		}
		return strings.Join(tokens, " ")
	}

	// 裸命令：剥脚本路径前缀后补 task 前缀。
	for i := range tokens {
		tokens[i] = stripQingLongScriptPrefix(tokens[i])
	}
	return "task " + strings.Join(tokens, " ")
}

func deriveTaskNameFromCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "导入任务"
	}
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "导入任务"
	}
	last := parts[len(parts)-1]
	base := filepath.Base(last)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func loadQingLongSubscriptions(db *sql.DB) ([]model.Subscription, error) {
	rows, err := loadSQLiteTableRows(db, "Subscriptions")
	if err != nil {
		return nil, fmt.Errorf("读取青龙订阅失败: %w", err)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return sqliteRowInt(rows[i], "id") < sqliteRowInt(rows[j], "id")
	})

	var result []model.Subscription
	for _, row := range rows {
		urlValue := strings.TrimSpace(firstNonEmptySQLiteString(row, "url"))
		if urlValue == "" {
			continue
		}

		schedule := strings.TrimSpace(firstNonEmptySQLiteString(row, "schedule"))
		if !ValidateSubscriptionSchedule(schedule) {
			schedule = ""
		}

		result = append(result, model.Subscription{
			ID:          uint(sqliteRowInt(row, "id")),
			Name:        strings.TrimSpace(firstNonEmptySQLiteString(row, "name")),
			Type:        inferQingLongSubscriptionType(urlValue, firstNonEmptySQLiteString(row, "type")),
			URL:         urlValue,
			Branch:      strings.TrimSpace(firstNonEmptySQLiteString(row, "branch")),
			Schedule:    schedule,
			Whitelist:   strings.TrimSpace(firstNonEmptySQLiteString(row, "whitelist")),
			Blacklist:   strings.TrimSpace(firstNonEmptySQLiteString(row, "blacklist")),
			DependOn:    strings.TrimSpace(firstNonEmptySQLiteString(row, "dependences", "depend_on")),
			AutoAddTask: sqliteRowInt(row, "autoAddCron") != 0 || sqliteRowBool(row, "auto_add_cron"),
			AutoDelTask: sqliteRowInt(row, "autoDelCron") != 0 || sqliteRowBool(row, "auto_del_cron"),
			Enabled:     !(sqliteRowBool(row, "is_disabled") || sqliteRowBool(row, "isDisabled")),
			Status:      0,
			Alias:       strings.TrimSpace(firstNonEmptySQLiteString(row, "alias")),
		})
	}

	return result, nil
}

func inferQingLongSubscriptionType(url, rawType string) string {
	rawType = strings.ToLower(strings.TrimSpace(rawType))
	if strings.Contains(rawType, "file") {
		return model.SubTypeSingleFile
	}
	if strings.HasSuffix(strings.ToLower(url), ".git") {
		return model.SubTypeGitRepo
	}
	return model.SubTypeGitRepo
}

func loadQingLongDependencies(db *sql.DB) ([]BackupDependency, error) {
	rows, err := loadSQLiteTableRows(db, "Dependences", "Dependencies")
	if err != nil {
		return nil, fmt.Errorf("读取青龙依赖失败: %w", err)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return sqliteRowInt(rows[i], "id") < sqliteRowInt(rows[j], "id")
	})

	var result []BackupDependency
	seen := map[string]struct{}{}
	for _, row := range rows {
		name := strings.TrimSpace(firstNonEmptySQLiteString(row, "name"))
		status := sqliteRowInt(row, "status")
		if name == "" || status != 1 {
			continue
		}
		mappedType := mapQingLongDependencyType(int(sqliteRowInt(row, "type")))
		if mappedType == "" {
			continue
		}
		key := mappedType + "::" + strings.ToLower(name)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, BackupDependency{
			Type:          mappedType,
			Name:          name,
			PythonVersion: "",
		})
	}

	return result, nil
}

func mapQingLongDependencyType(depType int) string {
	switch depType {
	case 0:
		return model.DepTypeNodeJS
	case 1:
		return model.DepTypePython
	default:
		return ""
	}
}

func loadQingLongApps(db *sql.DB) ([]BackupOpenApp, error) {
	rows, err := loadSQLiteTableRows(db, "Apps")
	if err != nil {
		return nil, fmt.Errorf("读取青龙应用失败: %w", err)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return sqliteRowInt(rows[i], "id") < sqliteRowInt(rows[j], "id")
	})

	var result []BackupOpenApp
	for _, row := range rows {
		clientID := strings.TrimSpace(firstNonEmptySQLiteString(row, "client_id", "clientId", "app_key"))
		clientSecret := strings.TrimSpace(firstNonEmptySQLiteString(row, "client_secret", "clientSecret", "app_secret"))
		if clientID == "" || clientSecret == "" {
			continue
		}

		scopeList := ""
		scopesJSON := strings.TrimSpace(firstNonEmptySQLiteString(row, "scopes"))
		if scopesJSON != "" {
			var scopes []string
			if err := json.Unmarshal([]byte(scopesJSON), &scopes); err == nil {
				scopeList = strings.Join(scopes, ",")
			}
		}

		result = append(result, BackupOpenApp{
			ID:        uint(sqliteRowInt(row, "id")),
			Name:      strings.TrimSpace(firstNonEmptySQLiteString(row, "name")),
			AppKey:    clientID,
			AppSecret: clientSecret,
			Scopes:    scopeList,
			Enabled:   true,
			RateLimit: 100,
		})
	}

	return result, nil
}

func hasAnyQingLongScriptFiles(dataDir string) bool {
	for _, path := range []string{
		filepath.Join(dataDir, "scripts"),
		filepath.Join(dataDir, "config", "extra.sh"),
		filepath.Join(dataDir, "config", "task_before.sh"),
		filepath.Join(dataDir, "config", "task_after.sh"),
		filepath.Join(dataDir, "deps"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func hasAnyQingLongLogFiles(dataDir string) bool {
	for _, path := range []string{filepath.Join(dataDir, "log"), filepath.Join(dataDir, "syslog")} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func restoreQingLongScripts(extractedDir string) error {
	dataDir, err := resolveQingLongDataDir(extractedDir)
	if err != nil {
		return err
	}

	return restoreDirectoryWithStage(config.C.Data.ScriptsDir, func(stageDir string) error {
		if err := copyDirectoryContentsFunc(filepath.Join(dataDir, "scripts"), stageDir); err != nil {
			return err
		}

		for _, hook := range []string{"extra.sh", "task_before.sh", "task_after.sh"} {
			sourcePath := filepath.Join(dataDir, "config", hook)
			if _, err := os.Stat(sourcePath); err == nil {
				if err := copyFileFunc(sourcePath, filepath.Join(stageDir, hook)); err != nil {
					return err
				}
			}
		}

		qlConfigDir := filepath.Join(stageDir, "ql-config")
		for _, fileName := range []string{"task_before.js", "task_before.py"} {
			sourcePath := filepath.Join(dataDir, "config", fileName)
			if _, err := os.Stat(sourcePath); err == nil {
				if err := copyFileFunc(sourcePath, filepath.Join(qlConfigDir, fileName)); err != nil {
					return err
				}
			}
		}

		depsDir := filepath.Join(dataDir, "deps")
		if entries, err := os.ReadDir(depsDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				sourcePath := filepath.Join(depsDir, entry.Name())
				targetPath := filepath.Join(stageDir, entry.Name())
				if _, err := os.Stat(targetPath); err == nil {
					continue
				}
				if err := copyFileFunc(sourcePath, targetPath); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func restoreQingLongLogs(extractedDir string) error {
	dataDir, err := resolveQingLongDataDir(extractedDir)
	if err != nil {
		return err
	}

	baseTarget := filepath.Join(config.C.Data.LogDir, "qinglong-import")
	if err := os.MkdirAll(baseTarget, 0o755); err != nil {
		return err
	}
	if err := copyDirectoryContents(filepath.Join(dataDir, "log"), filepath.Join(baseTarget, "log")); err != nil {
		return err
	}
	if err := copyDirectoryContents(filepath.Join(dataDir, "syslog"), filepath.Join(baseTarget, "syslog")); err != nil {
		return err
	}
	return nil
}

func loadSQLiteTableRows(db *sql.DB, tableNames ...string) ([]map[string]interface{}, error) {
	for _, tableName := range tableNames {
		query := fmt.Sprintf("SELECT * FROM %s", tableName)
		rows, err := db.Query(query)
		if err != nil {
			if isSQLiteMissingTableError(err) {
				continue
			}
			return nil, err
		}
		defer rows.Close()

		columns, err := rows.Columns()
		if err != nil {
			return nil, err
		}

		var result []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			valuePointers := make([]interface{}, len(columns))
			for i := range values {
				valuePointers[i] = &values[i]
			}
			if err := rows.Scan(valuePointers...); err != nil {
				return nil, err
			}

			row := make(map[string]interface{}, len(columns))
			for i, column := range columns {
				row[column] = normalizeSQLiteValue(values[i])
			}
			result = append(result, row)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return result, nil
	}

	return nil, nil
}

func normalizeSQLiteValue(value interface{}) interface{} {
	switch v := value.(type) {
	case []byte:
		return string(v)
	default:
		return v
	}
}

func isSQLiteMissingTableError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such table")
}

func firstNonEmptySQLiteString(row map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(sqliteRowString(row, key))
		if value != "" {
			return value
		}
	}
	return ""
}

func sqliteRowString(row map[string]interface{}, key string) string {
	value, exists := row[key]
	if !exists || value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func sqliteRowInt(row map[string]interface{}, key string) int64 {
	value, exists := row[key]
	if !exists || value == nil {
		return 0
	}

	switch v := value.(type) {
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case bool:
		if v {
			return 1
		}
		return 0
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(v)), 10, 64)
		return parsed
	}
}

func sqliteRowFloat(row map[string]interface{}, key string) float64 {
	value, exists := row[key]
	if !exists || value == nil {
		return 0
	}

	switch v := value.(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed
	default:
		parsed, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return parsed
	}
}

func sqliteRowBool(row map[string]interface{}, key string) bool {
	value, exists := row[key]
	if !exists || value == nil {
		return false
	}

	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		normalized := strings.TrimSpace(strings.ToLower(v))
		return normalized == "1" || normalized == "true" || normalized == "yes"
	default:
		return sqliteRowInt(row, key) != 0
	}
}
