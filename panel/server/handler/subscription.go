package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type subPullBroadcaster struct {
	mu          sync.RWMutex
	subs        map[chan string]struct{}
	log         strings.Builder
	entries     []subPullEvent
	cursor      int64
	operationID string
}

type subPullEvent struct {
	Cursor int64
	Line   string
}

var (
	subPullStreams   = make(map[uint]*subPullBroadcaster)
	subPullStreamsMu sync.RWMutex
)

func getOrCreateSubBroadcaster(id uint) *subPullBroadcaster {
	subPullStreamsMu.Lock()
	defer subPullStreamsMu.Unlock()
	if b, ok := subPullStreams[id]; ok {
		return b
	}
	b := &subPullBroadcaster{subs: make(map[chan string]struct{})}
	subPullStreams[id] = b
	return b
}

func removeSubBroadcaster(id uint) {
	subPullStreamsMu.Lock()
	defer subPullStreamsMu.Unlock()
	if b, ok := subPullStreams[id]; ok {
		b.mu.Lock()
		for ch := range b.subs {
			close(ch)
		}
		b.mu.Unlock()
		delete(subPullStreams, id)
	}
}

func (b *subPullBroadcaster) subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *subPullBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

func (b *subPullBroadcaster) broadcast(line string) {
	b.mu.Lock()
	b.cursor++
	cursor := b.cursor
	b.log.WriteString(line)
	b.log.WriteString("\n")
	b.entries = append(b.entries, subPullEvent{Cursor: cursor, Line: line})
	b.mu.Unlock()

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *subPullBroadcaster) setOperationID(operationID string) {
	b.mu.Lock()
	b.operationID = operationID
	b.mu.Unlock()
}

func (b *subPullBroadcaster) currentOperationID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.operationID
}

func (b *subPullBroadcaster) done() {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- "\x00DONE":
		default:
		}
	}
}

func (b *subPullBroadcaster) history() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.log.String()
}

func (b *subPullBroadcaster) historySince(cursor int64) []subPullEvent {
	b.mu.RLock()
	defer b.mu.RUnlock()
	items := make([]subPullEvent, 0, len(b.entries))
	for _, item := range b.entries {
		if item.Cursor > cursor {
			items = append(items, item)
		}
	}
	return items
}

type SubscriptionHandler struct{}

func NewSubscriptionHandler() *SubscriptionHandler {
	return &SubscriptionHandler{}
}

func normalizeSubscriptionAuthInput(authType string, sshKeyID *uint, authToken string) (string, *uint, string, error) {
	normalizedType := model.NormalizeSubscriptionAuthType(authType)
	trimmedToken := strings.TrimSpace(authToken)

	switch normalizedType {
	case "":
		return "", nil, "", nil
	case model.SubAuthTypeSSH:
		if sshKeyID == nil || *sshKeyID == 0 {
			return "", nil, "", fmt.Errorf("已选择 SSH 鉴权，请指定 SSH 密钥")
		}
		return normalizedType, sshKeyID, "", nil
	case model.SubAuthTypeToken:
		if trimmedToken == "" {
			return "", nil, "", fmt.Errorf("已选择 Token 鉴权，请填写访问令牌")
		}
		return normalizedType, nil, trimmedToken, nil
	default:
		return "", nil, "", fmt.Errorf("无效的仓库鉴权方式")
	}
}

func (h *SubscriptionHandler) List(c *gin.Context) {
	keyword := c.Query("keyword")
	subType := c.Query("type")
	enabledRaw := c.Query("enabled")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := database.DB.Model(&model.Subscription{})

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name LIKE ? OR url LIKE ?", like, like)
	}
	if subType != "" {
		query = query.Where("type = ?", subType)
	}
	if enabledRaw != "" {
		enabled, err := strconv.ParseBool(enabledRaw)
		if err == nil {
			query = query.Where("enabled = ?", enabled)
		}
	}

	var total int64
	query.Count(&total)

	var subs []model.Subscription
	query.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&subs)

	data := make([]map[string]interface{}, len(subs))
	for i, s := range subs {
		data[i] = s.ToDict()
	}

	response.Paginated(c, data, total, page, pageSize)
}

func (h *SubscriptionHandler) Create(c *gin.Context) {
	var req struct {
		Name           string `json:"name" binding:"required"`
		Type           string `json:"type"`
		URL            string `json:"url" binding:"required"`
		Branch         string `json:"branch"`
		Schedule       string `json:"schedule"`
		Whitelist      string `json:"whitelist"`
		Blacklist      string `json:"blacklist"`
		DependOn       string `json:"depend_on"`
		HookScript     string `json:"hook_script"`
		AutoAddTask    bool   `json:"auto_add_task"`
		AutoDelTask    bool   `json:"auto_del_task"`
		SaveDir        string `json:"save_dir"`
		SubPath        string `json:"sub_path"`
		SSHKeyID       *uint  `json:"ssh_key_id"`
		AuthType       string `json:"auth_type"`
		AuthUsername   string `json:"auth_username"`
		AuthToken      string `json:"auth_token"`
		Alias          string `json:"alias"`
		ForceOverwrite *bool  `json:"force_overwrite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	if req.Type == "" {
		req.Type = model.SubTypeGitRepo
	}
	if !service.ValidateSubscriptionSchedule(req.Schedule) {
		response.BadRequest(c, "无效的订阅定时规则")
		return
	}
	authType, sshKeyID, authToken, err := normalizeSubscriptionAuthInput(req.AuthType, req.SSHKeyID, req.AuthToken)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	sub := model.Subscription{
		Name:           req.Name,
		Type:           req.Type,
		URL:            req.URL,
		Branch:         req.Branch,
		Schedule:       req.Schedule,
		Whitelist:      req.Whitelist,
		Blacklist:      req.Blacklist,
		DependOn:       req.DependOn,
		HookScript:     req.HookScript,
		AutoAddTask:    req.AutoAddTask,
		AutoDelTask:    req.AutoDelTask,
		Enabled:        true,
		SaveDir:        req.SaveDir,
		SubPath:        req.SubPath,
		SSHKeyID:       sshKeyID,
		AuthType:       authType,
		AuthUsername:   req.AuthUsername,
		Alias:          req.Alias,
		ForceOverwrite: req.ForceOverwrite,
	}

	tx := database.DB.Begin()
	if tx.Error != nil {
		response.InternalError(c, "创建订阅失败")
		return
	}
	if err := tx.Create(&sub).Error; err != nil {
		tx.Rollback()
		response.InternalError(c, "创建订阅失败")
		return
	}
	if authType == model.SubAuthTypeToken {
		if err := service.SealSubscriptionAuthToken(c.Request.Context(), &sub, authToken); err != nil {
			tx.Rollback()
			response.InternalError(c, "创建订阅失败")
			return
		}
		if err := tx.Model(&sub).Updates(map[string]interface{}{
			"auth_token":                 sub.AuthToken,
			"auth_token_secret_provider": sub.AuthTokenSecretProvider,
			"auth_token_secret_cipher":   sub.AuthTokenSecretCipher,
			"ssh_key_secret_provider":    sub.SSHKeySecretProvider,
			"ssh_key_secret_cipher":      sub.SSHKeySecretCipher,
		}).Error; err != nil {
			tx.Rollback()
			response.InternalError(c, "创建订阅失败")
			return
		}
	}
	if err := tx.Commit().Error; err != nil {
		response.InternalError(c, "创建订阅失败")
		return
	}

	if err := service.GetSubscriptionScheduler().AddOrUpdateJob(&sub); err != nil {
		response.InternalError(c, "创建订阅成功，但定时调度注册失败")
		return
	}

	response.Created(c, gin.H{"message": "创建成功", "data": sub.ToDict()})
}

func (h *SubscriptionHandler) Update(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var sub model.Subscription
	if err := database.DB.First(&sub, subID).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	allowed := map[string]bool{
		"name": true, "type": true, "url": true, "branch": true,
		"schedule": true, "whitelist": true, "blacklist": true,
		"depend_on": true, "hook_script": true, "auto_add_task": true, "auto_del_task": true,
		"save_dir": true, "sub_path": true, "ssh_key_id": true, "auth_type": true, "auth_username": true, "auth_token": true, "alias": true, "force_overwrite": true,
	}
	updates := make(map[string]interface{})
	for k, v := range req {
		if allowed[k] {
			updates[k] = v
		}
	}

	if schedule, ok := updates["schedule"].(string); ok {
		if !service.ValidateSubscriptionSchedule(schedule) {
			response.BadRequest(c, "无效的订阅定时规则")
			return
		}
	}

	_, hasAuthType := updates["auth_type"]
	_, hasSSHKeyID := updates["ssh_key_id"]
	_, hasAuthToken := updates["auth_token"]
	if hasAuthType || hasSSHKeyID || hasAuthToken {
		var rawSSHKeyID *uint
		if value, exists := updates["ssh_key_id"]; exists {
			switch typed := value.(type) {
			case nil:
				rawSSHKeyID = nil
			case float64:
				if typed > 0 {
					id := uint(typed)
					rawSSHKeyID = &id
				}
			}
		} else {
			rawSSHKeyID = sub.SSHKeyID
		}

		authType := sub.EffectiveAuthType()
		if value, exists := updates["auth_type"]; exists {
			text, ok := value.(string)
			if !ok {
				response.BadRequest(c, "无效的仓库鉴权方式")
				return
			}
			authType = text
		}

		authToken := strings.TrimSpace(sub.AuthToken)
		if authToken == "" && sub.HasAuthToken() {
			authToken = "existing-token"
		}
		newAuthToken := ""
		if value, exists := updates["auth_token"]; exists {
			text, ok := value.(string)
			if !ok {
				response.BadRequest(c, "无效的仓库访问令牌")
				return
			}
			if strings.TrimSpace(text) != "" {
				authToken = text
				newAuthToken = text
			} else if !sub.HasAuthToken() || sub.EffectiveAuthType() != model.SubAuthTypeToken {
				authToken = text
			}
		}

		normalizedType, normalizedSSHKeyID, _, err := normalizeSubscriptionAuthInput(authType, rawSSHKeyID, authToken)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		updates["auth_type"] = normalizedType
		updates["ssh_key_id"] = normalizedSSHKeyID
		delete(updates, "auth_token")
		if normalizedType != model.SubAuthTypeToken {
			service.ClearSubscriptionAuthSecrets(&sub)
			updates["auth_token"] = sub.AuthToken
			updates["auth_token_secret_provider"] = sub.AuthTokenSecretProvider
			updates["auth_token_secret_cipher"] = sub.AuthTokenSecretCipher
			updates["ssh_key_secret_provider"] = sub.SSHKeySecretProvider
			updates["ssh_key_secret_cipher"] = sub.SSHKeySecretCipher
		} else if newAuthToken != "" || strings.TrimSpace(sub.AuthToken) != "" {
			if newAuthToken == "" {
				newAuthToken = strings.TrimSpace(sub.AuthToken)
			}
			if err := service.SealSubscriptionAuthToken(c.Request.Context(), &sub, newAuthToken); err != nil {
				response.InternalError(c, "更新订阅失败")
				return
			}
			updates["auth_token"] = sub.AuthToken
			updates["auth_token_secret_provider"] = sub.AuthTokenSecretProvider
			updates["auth_token_secret_cipher"] = sub.AuthTokenSecretCipher
		}
	}

	if len(updates) > 0 {
		database.DB.Model(&sub).Updates(updates)
	}

	database.DB.First(&sub, subID)
	if err := service.GetSubscriptionScheduler().AddOrUpdateJob(&sub); err != nil {
		response.InternalError(c, "更新成功，但定时调度注册失败")
		return
	}
	response.Success(c, gin.H{"message": "更新成功", "data": sub.ToDict()})
}

func (h *SubscriptionHandler) Delete(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	service.GetSubscriptionScheduler().RemoveJob(uint(subID))
	database.DB.Where("id = ?", subID).Delete(&model.Subscription{})
	database.DB.Where("subscription_id = ?", subID).Delete(&model.SubLog{})
	response.Success(c, gin.H{"message": "删除成功"})
}

func (h *SubscriptionHandler) Enable(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var sub model.Subscription
	if err := database.DB.First(&sub, subID).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}
	database.DB.Model(&sub).Update("enabled", true)
	sub.Enabled = true
	if err := service.GetSubscriptionScheduler().AddOrUpdateJob(&sub); err != nil {
		response.InternalError(c, "启用成功，但定时调度注册失败")
		return
	}
	response.Success(c, gin.H{"message": "已启用", "data": sub.ToDict()})
}

func (h *SubscriptionHandler) Disable(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var sub model.Subscription
	if err := database.DB.First(&sub, subID).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}
	database.DB.Model(&sub).Update("enabled", false)
	sub.Enabled = false
	service.GetSubscriptionScheduler().RemoveJob(sub.ID)
	response.Success(c, gin.H{"message": "已禁用", "data": sub.ToDict()})
}

func (h *SubscriptionHandler) Pull(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var sub model.Subscription
	if err := database.DB.First(&sub, subID).Error; err != nil {
		response.NotFound(c, "订阅不存在")
		return
	}

	ctx, operation, err := service.BeginSubscriptionPull(uint(subID))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	broadcaster := getOrCreateSubBroadcaster(uint(subID))
	broadcaster.setOperationID(operation.ID)

	go func() {
		defer service.FinishSubscriptionPull(uint(subID))
		defer removeSubBroadcaster(uint(subID))
		service.PullSubscriptionWithOperation(ctx, &sub, func(line string) {
			broadcaster.broadcast(line)
		}, operation.ID)
		broadcaster.done()
	}()

	response.Success(c, gin.H{"message": "拉取任务已启动", "operation_id": operation.ID})
}

func (h *SubscriptionHandler) StopPull(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	if !service.IsSubscriptionPullRunning(uint(subID)) {
		response.BadRequest(c, "当前没有进行中的拉取任务")
		return
	}

	subPullStreamsMu.RLock()
	broadcaster, exists := subPullStreams[uint(subID)]
	subPullStreamsMu.RUnlock()
	if exists {
		broadcaster.broadcast("[停止请求] 正在终止当前拉取任务...")
	}

	operationID, ok := service.StopSubscriptionPullWithOperation(uint(subID))
	if !ok {
		response.BadRequest(c, "拉取任务停止失败")
		return
	}

	response.Success(c, gin.H{"message": "已发送停止请求", "operation_id": operationID})
}

func (h *SubscriptionHandler) PullStream(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	cursor := parseSSECursor(firstNonEmptyString(c.Query("cursor"), c.GetHeader("Last-Event-ID")))
	operationIDFilter := strings.TrimSpace(c.Query("operation_id"))

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	subPullStreamsMu.RLock()
	broadcaster, exists := subPullStreams[uint(subID)]
	subPullStreamsMu.RUnlock()

	if !exists {
		writePersistedSubscriptionLogEvents(c, uint(subID), operationIDFilter, cursor)
		fmt.Fprintf(c.Writer, "event: done\ndata: not_running\n\n")
		c.Writer.Flush()
		return
	}

	if operationID := broadcaster.currentOperationID(); operationID != "" {
		fmt.Fprintf(c.Writer, "event: operation\ndata: %s\n\n", operationID)
		c.Writer.Flush()
	}

	for _, item := range broadcaster.historySince(cursor) {
		if item.Line != "" {
			fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", item.Cursor, item.Line)
		}
		if item.Cursor > cursor {
			cursor = item.Cursor
		}
	}
	c.Writer.Flush()

	sub := broadcaster.subscribe()
	defer broadcaster.unsubscribe(sub)

	ctx := c.Request.Context()
	for {
		select {
		case line, ok := <-sub:
			if !ok {
				fmt.Fprintf(c.Writer, "event: done\ndata: closed\n\n")
				c.Writer.Flush()
				return
			}
			if line == "\x00DONE" {
				fmt.Fprintf(c.Writer, "event: done\ndata: finished\n\n")
				c.Writer.Flush()
				return
			}
			cursor++
			fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", cursor, line)
			c.Writer.Flush()
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Minute):
			fmt.Fprintf(c.Writer, "event: done\ndata: timeout\n\n")
			c.Writer.Flush()
			return
		}
	}
}

func parseSSECursor(value string) int64 {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed < 0 {
		return 0
	}
	return parsed
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writePersistedSubscriptionLogEvents(c *gin.Context, subID uint, operationID string, cursor int64) {
	query := database.DB.Model(&model.SubLog{}).Where("subscription_id = ?", subID)
	if operationID != "" {
		query = query.Where("operation_id = ?", operationID)
	}
	var log model.SubLog
	if err := query.Order("created_at DESC").First(&log).Error; err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(log.Content, "\n"), "\n")
	for index, line := range lines {
		lineCursor := int64(index + 1)
		if line == "" || lineCursor <= cursor {
			continue
		}
		fmt.Fprintf(c.Writer, "id: %d\ndata: %s\n\n", lineCursor, line)
	}
	c.Status(http.StatusOK)
}

func (h *SubscriptionHandler) Logs(c *gin.Context) {
	subID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := database.DB.Model(&model.SubLog{}).Where("subscription_id = ?", subID)
	if operationID := strings.TrimSpace(c.Query("operation_id")); operationID != "" {
		query = query.Where("operation_id = ?", operationID)
	}

	var total int64
	query.Count(&total)

	var logs []model.SubLog
	query.Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	data := make([]map[string]interface{}, len(logs))
	for i, l := range logs {
		data[i] = l.ToDict()
	}

	response.Paginated(c, data, total, page, pageSize)
}

func (h *SubscriptionHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Where("id IN ?", req.IDs).Delete(&model.Subscription{})
	database.DB.Where("subscription_id IN ?", req.IDs).Delete(&model.SubLog{})
	for _, id := range req.IDs {
		service.GetSubscriptionScheduler().RemoveJob(id)
	}

	response.Success(c, gin.H{
		"message": fmt.Sprintf("已删除 %d 个订阅", result.RowsAffected),
	})
}

func (h *SubscriptionHandler) RegisterRoutes(r *gin.RouterGroup) {
	subs := r.Group("/subscriptions", middleware.JWTAuth(), middleware.OpenAPIAccess("subscriptions"), middleware.RequireRole("operator"))
	{
		subs.GET("", h.List)
		subs.POST("", h.Create)
		subs.PUT("/:id", h.Update)
		subs.DELETE("/:id", h.Delete)
		subs.PUT("/:id/enable", h.Enable)
		subs.PUT("/:id/disable", h.Disable)
		subs.PUT("/:id/pull", h.Pull)
		subs.PUT("/:id/pull/stop", h.StopPull)
		subs.GET("/:id/pull-stream", h.PullStream)
		subs.GET("/:id/logs", h.Logs)
		subs.DELETE("/batch", h.BatchDelete)
	}
}
