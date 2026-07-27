package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

const (
	systemHealthLastCheckedAtKey = "system_health_last_checked_at"
	systemHealthLastResultKey    = "system_health_last_result_json"
)

type systemHealthCheckItem struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type systemHealthSnapshot struct {
	Items         []systemHealthCheckItem `json:"items"`
	LastCheckedAt string                  `json:"last_checked_at,omitempty"`
}

var (
	// systemHealthCheckHTTPClient 默认为 nil，运行时按需用 service.NewHTTPClient 构建，
	// 这样能实时读取 system_configs.proxy_url，让 "网络代理" 配置变更立即对健康检查生效。
	// 单元测试可直接赋值非 nil 客户端进行 mock。
	systemHealthCheckHTTPClient *http.Client
	systemHealthCheckURL        = "https://www.baidu.com"
	// systemHealthGetResourceInfo 默认走真实资源采集，
	// 测试里可替换成固定值，避免健康检查回归用例受当前机器状态影响。
	systemHealthGetResourceInfo = service.GetResourceInfo
)

func resolveSystemHealthCheckClient() *http.Client {
	if systemHealthCheckHTTPClient != nil {
		return systemHealthCheckHTTPClient
	}
	return service.NewHTTPClient(3 * time.Second)
}

type SystemHandler struct{}

func NewSystemHandler() *SystemHandler {
	return &SystemHandler{}
}

func (h *SystemHandler) Info(c *gin.Context) {
	info := systemHealthGetResourceInfo()
	response.Success(c, gin.H{"data": info})
}

// MachineCode 单独返回面板机器码，便于外部工具通过接口直接获取（无需解析完整系统信息）。
func (h *SystemHandler) MachineCode(c *gin.Context) {
	code := service.EnsureMachineCode()
	response.Success(c, gin.H{"data": gin.H{"machine_code": code}})
}

func (h *SystemHandler) Dashboard(c *gin.Context) {
	var taskCount int64
	database.DB.Model(&model.Task{}).Count(&taskCount)

	var enabledTasks int64
	database.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusEnabled).Count(&enabledTasks)

	var runningTasks int64
	database.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusRunning).Count(&runningTasks)

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var todayLogs int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ?", today).Count(&todayLogs)

	var successLogs int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND status = ?", today, model.LogStatusSuccess).Count(&successLogs)

	var failedLogs int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND status = ?", today, model.LogStatusFailed).Count(&failedLogs)

	var abortedLogs int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND status = ?", today, model.LogStatusAborted).Count(&abortedLogs)

	var envCount int64
	database.DB.Model(&model.EnvVar{}).Count(&envCount)

	var subCount int64
	database.DB.Model(&model.Subscription{}).Count(&subCount)

	var prevTaskCount int64
	database.DB.Model(&model.Task{}).Where("created_at < ?", today).Count(&prevTaskCount)

	yesterday := today.AddDate(0, 0, -1)
	var yesterdayLogs int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ?", yesterday, today).Count(&yesterdayLogs)
	var yesterdaySuccess int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", yesterday, today, model.LogStatusSuccess).Count(&yesterdaySuccess)
	var yesterdayFailed int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", yesterday, today, model.LogStatusFailed).Count(&yesterdayFailed)
	var yesterdayAborted int64
	database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", yesterday, today, model.LogStatusAborted).Count(&yesterdayAborted)

	var recentLogs []model.TaskLog
	database.DB.Preload("Task").Order("created_at DESC").Limit(10).Find(&recentLogs)

	recentData := make([]map[string]interface{}, len(recentLogs))
	for i, l := range recentLogs {
		recentData[i] = l.ToDict()
	}

	rangeDays := 7
	if r := c.Query("range"); r != "" {
		if n, err := strconv.Atoi(r); err == nil && n > 0 && n <= 90 {
			rangeDays = n
		}
	}

	type DailyStat struct {
		Date    string `json:"date"`
		Success int64  `json:"success"`
		Failed  int64  `json:"failed"`
		Aborted int64  `json:"aborted"`
	}

	var dailyStats []DailyStat
	for i := rangeDays - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i)
		nextDay := day.Add(24 * time.Hour)
		date := day.Format("01-02")

		var s, f, a int64
		database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", day, nextDay, model.LogStatusSuccess).Count(&s)
		database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", day, nextDay, model.LogStatusFailed).Count(&f)
		database.DB.Model(&model.TaskLog{}).Where("created_at >= ? AND created_at < ? AND status = ?", day, nextDay, model.LogStatusAborted).Count(&a)
		dailyStats = append(dailyStats, DailyStat{Date: date, Success: s, Failed: f, Aborted: a})
	}

	response.Success(c, gin.H{
		"data": gin.H{
			"task_count":        taskCount,
			"enabled_tasks":     enabledTasks,
			"running_tasks":     runningTasks,
			"today_logs":        todayLogs,
			"success_logs":      successLogs,
			"failed_logs":       failedLogs,
			"aborted_logs":      abortedLogs,
			"env_count":         envCount,
			"sub_count":         subCount,
			"prev_task_count":   prevTaskCount,
			"yesterday_logs":    yesterdayLogs,
			"yesterday_success": yesterdaySuccess,
			"yesterday_failed":  yesterdayFailed,
			"yesterday_aborted": yesterdayAborted,
			"recent_logs":       recentData,
			"daily_stats":       dailyStats,
			"range_days":        rangeDays,
		},
	})
}

func (h *SystemHandler) Stats(c *gin.Context) {
	var taskCount, enabledTasks, disabledTasks, runningTasks int64
	database.DB.Model(&model.Task{}).Count(&taskCount)
	database.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusEnabled).Count(&enabledTasks)
	database.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusDisabled).Count(&disabledTasks)
	database.DB.Model(&model.Task{}).Where("status = ?", model.TaskStatusRunning).Count(&runningTasks)

	var totalLogs, successLogs, failedLogs, abortedLogs int64
	database.DB.Model(&model.TaskLog{}).Count(&totalLogs)
	database.DB.Model(&model.TaskLog{}).Where("status = ?", model.LogStatusSuccess).Count(&successLogs)
	database.DB.Model(&model.TaskLog{}).Where("status = ?", model.LogStatusFailed).Count(&failedLogs)
	database.DB.Model(&model.TaskLog{}).Where("status = ?", model.LogStatusAborted).Count(&abortedLogs)

	successRate := 0.0
	// 成功率只统计自然完成的成功 / 失败，Aborted 单独统计，不拉低成功率。
	finishedLogs := successLogs + failedLogs
	if finishedLogs > 0 {
		successRate = float64(successLogs) / float64(finishedLogs) * 100
	}

	scriptCount := service.CountScriptFiles(config.C.Data.ScriptsDir)

	response.Success(c, gin.H{
		"data": gin.H{
			"tasks": gin.H{
				"total":    taskCount,
				"enabled":  enabledTasks,
				"disabled": disabledTasks,
				"running":  runningTasks,
			},
			"logs": gin.H{
				"total":        totalLogs,
				"success":      successLogs,
				"failed":       failedLogs,
				"aborted":      abortedLogs,
				"success_rate": successRate,
			},
			"scripts": gin.H{
				"total": scriptCount,
			},
		},
	})
}

func (h *SystemHandler) Backup(c *gin.Context) {
	var req struct {
		Password  string                  `json:"password"`
		Name      string                  `json:"name"`
		Selection service.BackupSelection `json:"selection"`
	}
	c.ShouldBindJSON(&req)

	filePath, err := service.CreateBackup(service.BackupCreateOptions{
		Password:  req.Password,
		Name:      req.Name,
		Selection: req.Selection.NormalizeDefaults(),
	})
	if err != nil {
		response.InternalError(c, "备份失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"message": "备份成功", "data": gin.H{"path": filePath}})
}

func (h *SystemHandler) BackupList(c *gin.Context) {
	backups, err := service.ListBackups()
	if err != nil {
		response.InternalError(c, "获取备份列表失败")
		return
	}
	response.Success(c, gin.H{"data": backups})
}

func (h *SystemHandler) Restore(c *gin.Context) {
	var req struct {
		Filename string `json:"filename" binding:"required"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	if err := service.RestoreBackup(req.Filename, req.Password); err != nil {
		response.InternalError(c, "恢复失败: "+err.Error())
		return
	}
	response.Success(c, gin.H{"message": "恢复成功"})
}

func (h *SystemHandler) RestoreProgress(c *gin.Context) {
	response.Success(c, gin.H{"data": service.CurrentRestoreProgress()})
}

func (h *SystemHandler) DeleteBackup(c *gin.Context) {
	filename := c.Query("filename")
	if filename == "" {
		response.BadRequest(c, "文件名不能为空")
		return
	}
	service.DeleteBackup(filename)
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *SystemHandler) UploadBackup(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.BadRequest(c, "请选择备份文件")
		return
	}

	if file.Size > 512*1024*1024 {
		response.BadRequest(c, "文件过大，最大 512MB")
		return
	}

	filename := filepath.Base(file.Filename)
	lowerName := strings.ToLower(filename)
	if !strings.HasSuffix(lowerName, ".json") &&
		!strings.HasSuffix(lowerName, ".enc") &&
		!strings.HasSuffix(lowerName, ".tgz") &&
		!strings.HasSuffix(lowerName, ".tar.gz") {
		response.BadRequest(c, "仅支持 .json、.enc、.tgz 或 .tar.gz 备份文件")
		return
	}

	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	os.MkdirAll(backupDir, 0755)
	dst := filepath.Join(backupDir, filename)

	if err := c.SaveUploadedFile(file, dst); err != nil {
		response.InternalError(c, "保存文件失败")
		return
	}

	response.Success(c, gin.H{"message": "上传成功", "data": gin.H{"filename": filename}})
}

func (h *SystemHandler) DownloadBackup(c *gin.Context) {
	filename := c.Param("filename")
	if filename == "" {
		filename = c.Query("filename")
	}
	if filename == "" {
		response.BadRequest(c, "文件名不能为空")
		return
	}

	baseName := filepath.Base(filename)
	if baseName != filename || strings.TrimSpace(baseName) == "." || strings.TrimSpace(baseName) == string(filepath.Separator) {
		response.BadRequest(c, "备份文件名无效")
		return
	}

	lowerName := strings.ToLower(baseName)
	if !strings.HasSuffix(lowerName, ".json") &&
		!strings.HasSuffix(lowerName, ".enc") &&
		!strings.HasSuffix(lowerName, ".tgz") &&
		!strings.HasSuffix(lowerName, ".tar.gz") {
		response.BadRequest(c, "仅支持下载 .json、.enc、.tgz 或 .tar.gz 备份文件")
		return
	}

	backupDir := filepath.Join(config.C.Data.Dir, "backups")
	filePath := filepath.Join(backupDir, baseName)
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			response.NotFound(c, "备份文件不存在")
			return
		}
		response.InternalError(c, "读取备份文件失败")
		return
	}
	if info.IsDir() {
		response.BadRequest(c, "备份文件名无效")
		return
	}

	c.Header("Cache-Control", "no-store, no-cache, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))
	c.FileAttachment(filePath, baseName)
}

func (h *SystemHandler) Version(c *gin.Context) {
	response.Success(c, gin.H{
		"data": gin.H{
			"version":     Version,
			"api_version": "v1",
			"framework":   "gin",
			"go_version":  service.GetResourceInfo().GoVersion,
		},
	})
}

func (h *SystemHandler) PublicVersion(c *gin.Context) {
	response.Success(c, gin.H{
		"version": Version,
		"data": gin.H{
			"version": Version,
		},
	})
}

func (h *SystemHandler) PanelSettings(c *gin.Context) {
	title := model.GetRegisteredConfig("panel_title")
	icon := model.GetRegisteredConfig("panel_icon")
	editorBackgroundColor := model.GetRegisteredConfig("editor_background_color")
	logBackgroundColor := model.GetRegisteredConfig("log_background_color")
	logBackgroundImage := model.GetRegisteredConfig("log_background_image")
	panelRuntimeMode := service.ResolvePanelRuntimeMode()
	panelServiceManager := model.GetRegisteredConfig("panel_service_manager")
	panelServiceName := model.GetRegisteredConfig("panel_service_name")
	response.Success(c, gin.H{
		"data": gin.H{
			"panel_title":             title,
			"panel_icon":              icon,
			"editor_background_color": editorBackgroundColor,
			"log_background_color":    logBackgroundColor,
			"log_background_image":    logBackgroundImage,
			"panel_runtime_mode":      panelRuntimeMode,
			"panel_service_manager":   panelServiceManager,
			"panel_service_name":      panelServiceName,
		},
	})
}

func (h *SystemHandler) CheckUpdate(c *gin.Context) {
	currentVersion := Version

	release, err := fetchLatestPanelRelease()
	if err != nil {
		response.InternalError(c, err.Error())
		return
	}

	latestVersion := release.version()
	hasUpdate := compareVersions(currentVersion, latestVersion)
	autoUpdateSupported := true
	updateDisabledReason := ""
	updateTarget := gin.H{}
	watchtowerCfg := currentWatchtowerRuntimeConfig()

	if watchtowerCfg.Managed {
		autoUpdateSupported = watchtowerCfg.ManualTriggerSupported
		if !watchtowerCfg.ManualTriggerSupported {
			updateDisabledReason = "当前由 Watchtower 托管自动更新；面板可展示更新状态，但未配置 Watchtower HTTP API 手动触发能力"
		}
		updateTarget = buildWatchtowerUpdateTarget(watchtowerCfg)
	} else {
		plan, planErr := buildPanelUpdatePlanForRelease(release)
		if planErr != nil {
			autoUpdateSupported = false
			updateDisabledReason = planErr.Error()
		} else {
			updateTarget = buildPanelUpdateTarget(plan)
		}
	}

	response.Success(c, gin.H{
		"data": gin.H{
			"current":                currentVersion,
			"latest":                 latestVersion,
			"has_update":             hasUpdate,
			"release_name":           release.Name,
			"release_url":            release.HTMLURL,
			"release_notes":          release.Body,
			"published_at":           release.PublishedAt,
			"auto_update_supported":  autoUpdateSupported,
			"update_disabled_reason": updateDisabledReason,
			"update_target":          updateTarget,
		},
	})
}

func (h *SystemHandler) UpdatePanel(c *gin.Context) {
	if watchtowerCfg := currentWatchtowerRuntimeConfig(); watchtowerCfg.Managed {
		result, err := triggerWatchtowerUpdate(watchtowerCfg)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}

		response.Success(c, gin.H{
			"message": "已触发 Watchtower 检查更新",
			"data": gin.H{
				"status":              "running",
				"phase":               "watchtower-triggered",
				"message":             "已请求 Watchtower 立即检查并执行容器更新",
				"deployment_type":     "docker",
				"update_manager":      panelUpdateManagerWatchtower,
				"watchtower_response": result,
			},
		})
		return
	}

	plan, err := buildPanelUpdatePlan()
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := panelUpdater.begin(plan); err != nil {
		respondUpdateConflict(c, err.Error())
		return
	}

	go executePanelUpdate(plan)

	response.Success(c, gin.H{
		"data": panelUpdater.snapshotCopy(),
	})
}

func (h *SystemHandler) Restart(c *gin.Context) {
	response.Success(c, gin.H{"message": "面板将在 2 秒后重启"})

	go func() {
		time.Sleep(2 * time.Second)
		os.Exit(1)
	}()
}

func (h *SystemHandler) PanelLog(c *gin.Context) {
	linesStr := c.DefaultQuery("lines", "100")
	keyword := c.Query("keyword")
	level := strings.ToLower(strings.TrimSpace(c.Query("level")))

	lines, _ := strconv.Atoi(linesStr)
	if lines <= 0 || lines > 10000 {
		lines = 100
	}

	logFile := filepath.Join(config.C.Data.Dir, "panel.log")
	file, err := os.Open(logFile)
	if err != nil {
		response.Success(c, gin.H{"data": gin.H{"logs": []string{}}})
		return
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if level != "" && !service.MatchPanelLogLevel(line, level) {
			continue
		}
		if keyword == "" || strings.Contains(line, keyword) {
			allLines = append(allLines, line)
		}
	}

	start := len(allLines) - lines
	if start < 0 {
		start = 0
	}

	response.Success(c, gin.H{
		"data": gin.H{
			"logs":  allLines[start:],
			"total": len(allLines),
			"level": level,
		},
	})
}

func runSystemHealthChecks() []systemHealthCheckItem {
	items := make([]systemHealthCheckItem, 0, 4)

	if err := database.DB.Exec("SELECT 1").Error; err != nil {
		items = append(items, systemHealthCheckItem{Name: "database", Status: "error", Message: err.Error()})
	} else {
		items = append(items, systemHealthCheckItem{Name: "database", Status: "ok"})
	}

	info := systemHealthGetResourceInfo()
	memThreshold := float64(model.GetRegisteredConfigInt("memory_warn"))
	if memThreshold <= 0 {
		memThreshold = 80
	}
	if info.MemoryTotal == 0 {
		items = append(items, systemHealthCheckItem{Name: "memory", Status: "warning", Message: "资源采集不可用"})
	} else if info.MemoryUsage > memThreshold {
		items = append(items, systemHealthCheckItem{Name: "memory", Status: "warning", Message: strconv.FormatFloat(info.MemoryUsage, 'f', 1, 64) + "%"})
	} else {
		items = append(items, systemHealthCheckItem{Name: "memory", Status: "ok", Message: strconv.FormatFloat(info.MemoryUsage, 'f', 1, 64) + "%"})
	}

	if sched := service.GetScheduler(); sched != nil {
		items = append(items, systemHealthCheckItem{Name: "scheduler", Status: "ok", Message: "运行中"})
	} else if schedV2 := service.GetSchedulerV2(); schedV2 != nil {
		items = append(items, systemHealthCheckItem{Name: "scheduler", Status: "ok", Message: "运行中"})
	} else {
		items = append(items, systemHealthCheckItem{Name: "scheduler", Status: "ok", Message: "空闲"})
	}

	if resp, err := resolveSystemHealthCheckClient().Get(systemHealthCheckURL); err != nil {
		items = append(items, systemHealthCheckItem{Name: "network", Status: "error", Message: "无法连接外部网络"})
	} else {
		resp.Body.Close()
		if resp.StatusCode >= http.StatusBadRequest {
			items = append(items, systemHealthCheckItem{Name: "network", Status: "error", Message: "网络检查返回状态异常"})
		} else {
			items = append(items, systemHealthCheckItem{Name: "network", Status: "ok"})
		}
	}

	return items
}

func loadSystemHealthSnapshot() systemHealthSnapshot {
	snapshot := systemHealthSnapshot{
		Items:         []systemHealthCheckItem{},
		LastCheckedAt: strings.TrimSpace(model.GetConfig(systemHealthLastCheckedAtKey, "")),
	}

	rawItems := strings.TrimSpace(model.GetConfig(systemHealthLastResultKey, ""))
	if rawItems == "" {
		return snapshot
	}

	if err := json.Unmarshal([]byte(rawItems), &snapshot.Items); err != nil {
		snapshot.Items = []systemHealthCheckItem{}
	}

	return snapshot
}

func saveSystemHealthSnapshot(items []systemHealthCheckItem, checkedAt time.Time) error {
	rawItems, err := json.Marshal(items)
	if err != nil {
		return err
	}

	if err := model.SetConfig(systemHealthLastResultKey, string(rawItems)); err != nil {
		return err
	}
	if err := model.SetConfig(systemHealthLastCheckedAtKey, checkedAt.Format(time.RFC3339)); err != nil {
		return err
	}
	return nil
}

func buildSystemHealthSnapshot(items []systemHealthCheckItem, checkedAt string) gin.H {
	return gin.H{
		"items":           items,
		"last_checked_at": checkedAt,
	}
}

func (h *SystemHandler) HealthCheck(c *gin.Context) {
	snapshot := loadSystemHealthSnapshot()
	response.Success(c, buildSystemHealthSnapshot(snapshot.Items, snapshot.LastCheckedAt))
}

func (h *SystemHandler) RunHealthCheck(c *gin.Context) {
	items := runSystemHealthChecks()
	checkedAt := time.Now()

	if err := saveSystemHealthSnapshot(items, checkedAt); err != nil {
		response.InternalError(c, "保存健康检查结果失败: "+err.Error())
		return
	}

	response.Success(c, buildSystemHealthSnapshot(items, checkedAt.Format(time.RFC3339)))
}

func (h *SystemHandler) GetConfigScript(c *gin.Context) {
	filePath := filepath.Join(config.C.Data.Dir, "config.sh")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			response.Success(c, gin.H{"content": "", "path": "config.sh"})
			return
		}
		response.InternalError(c, "读取配置文件失败")
		return
	}
	response.Success(c, gin.H{"content": string(data), "path": "config.sh"})
}

func (h *SystemHandler) SaveConfigScript(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}
	filePath := filepath.Join(config.C.Data.Dir, "config.sh")
	if err := os.WriteFile(filePath, []byte(req.Content), 0755); err != nil {
		response.InternalError(c, "保存配置文件失败")
		return
	}
	response.Success(c, gin.H{"message": "配置文件已保存"})
}

func (h *SystemHandler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/system/public-version", h.PublicVersion)
	r.GET("/system/panel-settings", h.PanelSettings)

	sys := r.Group("/system", middleware.JWTAuth())
	{
		sys.GET("/info", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.Info)
		sys.GET("/machine-code", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.MachineCode)
		sys.GET("/dashboard", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.Dashboard)
		sys.GET("/stats", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.Stats)
		sys.GET("/version", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.Version)
		sys.GET("/check-update", middleware.OpenAPIAccess("system"), middleware.RequireRole("viewer"), h.CheckUpdate)
		sys.GET("/health-check", middleware.RequireRole("viewer"), h.HealthCheck)
		sys.POST("/health-check", middleware.RequireRole("viewer"), h.RunHealthCheck)
		sys.GET("/update-status", middleware.RequireAdmin(), h.UpdateStatus)
		sys.POST("/update", middleware.RequireAdmin(), h.UpdatePanel)
		sys.POST("/restart", middleware.RequireAdmin(), h.Restart)
		sys.GET("/panel-log", middleware.RequireUserToken(), middleware.RequireAdmin(), h.PanelLog)
		sys.POST("/backup", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.Backup)
		sys.POST("/backup/upload", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.UploadBackup)
		sys.GET("/backups", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.BackupList)
		sys.GET("/backup/download", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.DownloadBackup)
		sys.GET("/backup/download/:filename", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.DownloadBackup)
		sys.GET("/restore/progress", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.RestoreProgress)
		sys.POST("/restore", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.Restore)
		sys.DELETE("/backup", middleware.OpenAPIAccess("backup"), middleware.RequireRole("admin"), h.DeleteBackup)
		sys.GET("/config-script", middleware.RequireRole("admin"), h.GetConfigScript)
		sys.PUT("/config-script", middleware.RequireRole("admin"), h.SaveConfigScript)
	}
}
