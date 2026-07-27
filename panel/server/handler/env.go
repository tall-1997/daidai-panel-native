package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

const (
	envNormalSortOrder    = 0
	envPinnedSortOrder    = 1
	envPositionStep       = 1000.0
	maxEnvRequestBodySize = 1 << 20
)

type EnvHandler struct{}

func NewEnvHandler() *EnvHandler {
	return &EnvHandler{}
}

func limitEnvRequestBody(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxEnvRequestBodySize)
}

func isRequestBodyTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

func orderedEnvQuery() *gorm.DB {
	return database.DB.Model(&model.EnvVar{}).
		Order("sort_order DESC, position ASC, created_at ASC, id ASC")
}

func normalizeEnvGroupValue(value string) string {
	return model.NormalizeEnvGroupValue(value)
}

func normalizeEnvGroupsPayload(group string, groups []string) string {
	if len(groups) > 0 {
		return model.JoinEnvGroups(groups)
	}
	return normalizeEnvGroupValue(group)
}

func parseEnvGroupFilter(rawValues ...string) []string {
	return model.SplitEnvGroups(strings.Join(rawValues, ","))
}

func applyEnvGroupFilters(query *gorm.DB, groups []string) *gorm.DB {
	if len(groups) == 0 {
		return query
	}

	clauses := make([]string, 0, len(groups))
	args := make([]interface{}, 0, len(groups))
	for _, group := range groups {
		clauses = append(clauses, "instr(',' || \"group\" || ',', ?) > 0")
		args = append(args, ","+group+",")
	}
	return query.Where("("+strings.Join(clauses, " OR ")+")", args...)
}

func envGroupValueFromImportItem(item map[string]interface{}) (string, bool) {
	if rawGroups, ok := item["groups"]; ok {
		switch groups := rawGroups.(type) {
		case []interface{}:
			values := make([]string, 0, len(groups))
			for _, value := range groups {
				if text, ok := value.(string); ok {
					values = append(values, text)
				}
			}
			return model.JoinEnvGroups(values), true
		case []string:
			return model.JoinEnvGroups(groups), true
		case string:
			return normalizeEnvGroupValue(groups), true
		}
	}

	group, ok := item["group"].(string)
	normalized := normalizeEnvGroupValue(group)
	return normalized, ok && normalized != ""
}

func nextEnvPosition(tx *gorm.DB, sortOrder int) (float64, error) {
	var last model.EnvVar
	err := tx.Where("sort_order = ?", sortOrder).
		Order("position DESC, id DESC").
		First(&last).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return envPositionStep, nil
		}
		return 0, err
	}
	return last.Position + envPositionStep, nil
}

func appendEnvToSortBucket(tx *gorm.DB, env *model.EnvVar, sortOrder int) error {
	if env == nil {
		return fmt.Errorf("环境变量不存在")
	}

	nextPos, err := nextEnvPosition(tx, sortOrder)
	if err != nil {
		return err
	}

	return tx.Model(env).Updates(map[string]interface{}{
		"sort_order": sortOrder,
		"position":   nextPos,
	}).Error
}

func reorderEnvWithinSortBucket(tx *gorm.DB, sourceID uint, targetID *uint) error {
	var source model.EnvVar
	if err := tx.First(&source, sourceID).Error; err != nil {
		return fmt.Errorf("源环境变量不存在")
	}

	if targetID != nil && *targetID == source.ID {
		return nil
	}

	if targetID != nil {
		var target model.EnvVar
		if err := tx.First(&target, *targetID).Error; err != nil {
			return fmt.Errorf("目标环境变量不存在")
		}
		if target.SortOrder != source.SortOrder {
			return fmt.Errorf("置顶项和普通项请分别排序，需要跨区移动时请使用置顶按钮")
		}
	}

	var siblings []model.EnvVar
	if err := tx.Where("sort_order = ?", source.SortOrder).
		Order("position ASC, created_at ASC, id ASC").
		Find(&siblings).Error; err != nil {
		return err
	}

	ordered := make([]model.EnvVar, 0, len(siblings))
	insertIndex := len(siblings) - 1
	if insertIndex < 0 {
		insertIndex = 0
	}

	filtered := make([]model.EnvVar, 0, len(siblings))
	for _, item := range siblings {
		if item.ID == source.ID {
			continue
		}
		filtered = append(filtered, item)
	}

	insertIndex = len(filtered)
	if targetID != nil {
		insertIndex = -1
		for idx, item := range filtered {
			if item.ID == *targetID {
				insertIndex = idx
				break
			}
		}
		if insertIndex == -1 {
			return fmt.Errorf("目标环境变量不存在")
		}
	}

	ordered = append(ordered, filtered[:insertIndex]...)
	ordered = append(ordered, source)
	ordered = append(ordered, filtered[insertIndex:]...)

	for idx, item := range ordered {
		if err := tx.Model(&model.EnvVar{}).
			Where("id = ?", item.ID).
			Updates(map[string]interface{}{
				"sort_order": source.SortOrder,
				"position":   float64(idx+1) * envPositionStep,
			}).Error; err != nil {
			return err
		}
	}

	return nil
}

func (h *EnvHandler) List(c *gin.Context) {
	keyword := c.Query("keyword")
	groupFilters := parseEnvGroupFilter(append(c.QueryArray("groups"), c.Query("groups"), c.Query("group"))...)
	enabledRaw := c.Query("enabled")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	allRaw := strings.ToLower(strings.TrimSpace(c.Query("all")))
	wantAll := allRaw == "1" || allRaw == "true" || allRaw == "yes"

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	query := orderedEnvQuery()

	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("UPPER(name) LIKE UPPER(?) OR UPPER(remarks) LIKE UPPER(?) OR UPPER(value) LIKE UPPER(?) OR UPPER(\"group\") LIKE UPPER(?)", like, like, like, like)
	}
	query = applyEnvGroupFilters(query, groupFilters)
	if enabledRaw != "" {
		enabled, err := strconv.ParseBool(enabledRaw)
		if err == nil {
			query = query.Where("enabled = ?", enabled)
		}
	}

	var total int64
	query.Count(&total)

	var envs []model.EnvVar
	if wantAll {
		// 不再做客户端循环分页：服务端一次性返回全部，但仍设置硬上限保护内存。
		const envAllSafeLimit = 5000
		query.Limit(envAllSafeLimit).Find(&envs)
	} else {
		query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&envs)
	}

	data := make([]map[string]interface{}, len(envs))
	for i, e := range envs {
		data[i] = e.ToDict()
	}

	if wantAll {
		response.Paginated(c, data, total, 1, len(data))
		return
	}
	response.Paginated(c, data, total, page, pageSize)
}

func (h *EnvHandler) Create(c *gin.Context) {
	limitEnvRequestBody(c)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			response.BadRequest(c, "请求体过大（最大 1MB）")
			return
		}
		response.BadRequest(c, "请求参数错误")
		return
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		response.BadRequest(c, "请求内容为空")
		return
	}

	type envItem struct {
		Name    string   `json:"name"`
		Value   string   `json:"value"`
		Remarks string   `json:"remarks"`
		Group   string   `json:"group"`
		Groups  []string `json:"groups"`
	}

	var items []envItem

	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &items); err != nil {
			response.BadRequest(c, "请求参数错误")
			return
		}
	} else {
		var single envItem
		if err := json.Unmarshal(raw, &single); err != nil {
			response.BadRequest(c, "请求参数错误")
			return
		}
		items = []envItem{single}
	}

	if len(items) == 0 {
		response.BadRequest(c, "请求内容为空")
		return
	}

	results := []map[string]interface{}{}
	errors := []string{}
	createdCount := 0

	for i, item := range items {
		if item.Name == "" {
			errors = append(errors, fmt.Sprintf("第 %d 项: 缺少名称", i+1))
			continue
		}
		if !envNamePattern.MatchString(item.Name) {
			errors = append(errors, fmt.Sprintf("第 %d 项: 变量名 '%s' 格式无效", i+1, item.Name))
			continue
		}

		// 青龙风格：新建一律纯 insert。同 name 允许多条（多账号场景），
		// 运行时由 BuildManagedRuntimeEnvMap 按 name 分组再用 & 拼接暴露给脚本。
		// 如果插件需要按 (name, remarks) 原地刷新 token，请走 PUT /envs/:id。
		nextPos, err := nextEnvPosition(database.DB, envNormalSortOrder)
		if err != nil {
			errors = append(errors, fmt.Sprintf("item %d: %s", i+1, err.Error()))
			continue
		}

		env := model.EnvVar{
			Name:      item.Name,
			Value:     item.Value,
			Remarks:   item.Remarks,
			Group:     normalizeEnvGroupsPayload(item.Group, item.Groups),
			Enabled:   true,
			SortOrder: envNormalSortOrder,
			Position:  nextPos,
		}

		if err := database.DB.Create(&env).Error; err != nil {
			errors = append(errors, fmt.Sprintf("item %d: %s", i+1, err.Error()))
			continue
		}
		results = append(results, env.ToDict())
		createdCount++
	}

	if len(results) == 1 && len(errors) == 0 {
		response.Created(c, gin.H{"message": "创建成功", "data": results[0]})
		return
	}

	payload := gin.H{
		"message": fmt.Sprintf("新增 %d 条", createdCount),
		"data":    results,
		"errors":  errors,
		"created": createdCount,
	}
	if createdCount > 0 {
		response.Created(c, payload)
		return
	}
	response.Success(c, payload)
}

type updateEnvRequest struct {
	Name    *string   `json:"name"`
	Value   *string   `json:"value"`
	Remarks *string   `json:"remarks"`
	Group   *string   `json:"group"`
	Groups  *[]string `json:"groups"`
	Enabled *bool     `json:"enabled"`
}

func (h *EnvHandler) Update(c *gin.Context) {
	envID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var env model.EnvVar
	if err := database.DB.First(&env, envID).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}

	var req updateEnvRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	updates := make(map[string]interface{})

	if req.Name != nil {
		newName := strings.TrimSpace(*req.Name)
		if newName == "" {
			response.BadRequest(c, "变量名不能为空")
			return
		}
		if !envNamePattern.MatchString(newName) {
			response.BadRequest(c, "变量名格式无效")
			return
		}
		if newName != env.Name {
			updates["name"] = newName
		}
	}
	if req.Value != nil && *req.Value != env.Value {
		updates["value"] = *req.Value
	}
	if req.Remarks != nil && *req.Remarks != env.Remarks {
		updates["remarks"] = *req.Remarks
	}
	if req.Groups != nil {
		normalized := model.JoinEnvGroups(*req.Groups)
		if normalized != env.Group {
			updates["group"] = normalized
		}
	} else if req.Group != nil {
		normalized := normalizeEnvGroupValue(*req.Group)
		if normalized != env.Group {
			updates["group"] = normalized
		}
	}
	if req.Enabled != nil && *req.Enabled != env.Enabled {
		updates["enabled"] = *req.Enabled
	}

	// 青龙风格：(name, remarks) 不再是业务唯一键，同 name + 同 remarks 允许多条，
	// 因此 Update 不需要撞名检测。运行时按 name 分组，顺序由 position 决定。

	if len(updates) == 0 {
		response.Success(c, gin.H{"message": "未检测到字段变更", "data": env.ToDict()})
		return
	}

	if err := database.DB.Model(&env).Updates(updates).Error; err != nil {
		response.InternalError(c, "更新失败")
		return
	}

	database.DB.First(&env, envID)
	response.Success(c, gin.H{"message": "更新成功", "data": env.ToDict()})
}

func (h *EnvHandler) Delete(c *gin.Context) {
	envID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("id = ?", envID).Delete(&model.EnvVar{})
	response.Success(c, gin.H{"message": "删除成功"})
}

// Get 根据 id 返回单个环境变量详情，便于外部脚本 / OpenAPI 调用方按 id 直接拉取。
func (h *EnvHandler) Get(c *gin.Context) {
	envID, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || envID == 0 {
		response.BadRequest(c, "环境变量 ID 无效")
		return
	}

	var env model.EnvVar
	if err := database.DB.First(&env, uint(envID)).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}

	response.Success(c, gin.H{"data": env.ToDict()})
}

func (h *EnvHandler) Enable(c *gin.Context) {
	envID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var env model.EnvVar
	if err := database.DB.First(&env, envID).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}
	database.DB.Model(&env).Update("enabled", true)
	env.Enabled = true
	response.Success(c, gin.H{"message": "已启用", "data": env.ToDict()})
}

func (h *EnvHandler) Disable(c *gin.Context) {
	envID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var env model.EnvVar
	if err := database.DB.First(&env, envID).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}
	database.DB.Model(&env).Update("enabled", false)
	env.Enabled = false
	response.Success(c, gin.H{"message": "已禁用", "data": env.ToDict()})
}

func (h *EnvHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Where("id IN ?", req.IDs).Delete(&model.EnvVar{})
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已删除 %d 个环境变量", result.RowsAffected),
	})
}

func (h *EnvHandler) BatchEnable(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Model(&model.EnvVar{}).Where("id IN ?", req.IDs).Update("enabled", true)
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已启用 %d 个环境变量", result.RowsAffected),
	})
}

func (h *EnvHandler) BatchDisable(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Model(&model.EnvVar{}).Where("id IN ?", req.IDs).Update("enabled", false)
	response.Success(c, gin.H{
		"message": fmt.Sprintf("已禁用 %d 个环境变量", result.RowsAffected),
	})
}

func (h *EnvHandler) BatchRename(c *gin.Context) {
	var req struct {
		IDs     []uint `json:"ids" binding:"required"`
		Name    string `json:"name"`
		Search  string `json:"search"`
		Replace string `json:"replace"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	directName := strings.TrimSpace(req.Name)
	if directName != "" {
		if !envNamePattern.MatchString(directName) {
			response.BadRequest(c, fmt.Sprintf("变量名 '%s' 格式无效", directName))
			return
		}
		if err := database.DB.Model(&model.EnvVar{}).Where("id IN ?", req.IDs).Update("name", directName).Error; err != nil {
			response.InternalError(c, "批量改名失败")
			return
		}
		response.Success(c, gin.H{"message": fmt.Sprintf("已将 %d 个变量重命名为 %s", len(req.IDs), directName)})
		return
	}

	search := strings.TrimSpace(req.Search)
	if search == "" {
		response.BadRequest(c, "查找内容不能为空")
		return
	}

	var envs []model.EnvVar
	if err := database.DB.Where("id IN ?", req.IDs).Find(&envs).Error; err != nil {
		response.InternalError(c, "批量改名失败")
		return
	}
	if len(envs) == 0 {
		response.NotFound(c, "未找到选中的环境变量")
		return
	}

	updates := make(map[uint]string, len(envs))
	for _, env := range envs {
		nextName := strings.ReplaceAll(env.Name, search, req.Replace)
		if nextName == env.Name {
			continue
		}
		if !envNamePattern.MatchString(nextName) {
			response.BadRequest(c, fmt.Sprintf("变量名 '%s' 修改后格式无效", nextName))
			return
		}
		updates[env.ID] = nextName
	}

	if len(updates) == 0 {
		response.BadRequest(c, "选中的变量名中未找到匹配内容")
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		for envID, nextName := range updates {
			if err := tx.Model(&model.EnvVar{}).Where("id = ?", envID).Update("name", nextName).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		response.InternalError(c, "批量改名失败")
		return
	}

	response.Success(c, gin.H{
		"message": fmt.Sprintf("已批量改名 %d 个环境变量", len(updates)),
	})
}

func (h *EnvHandler) Sort(c *gin.Context) {
	var req struct {
		SourceID uint  `json:"source_id" binding:"required"`
		TargetID *uint `json:"target_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var source model.EnvVar
	if err := database.DB.First(&source, req.SourceID).Error; err != nil {
		response.NotFound(c, "源环境变量不存在")
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		return reorderEnvWithinSortBucket(tx, req.SourceID, req.TargetID)
	}); err != nil {
		switch err.Error() {
		case "源环境变量不存在", "目标环境变量不存在":
			response.NotFound(c, err.Error())
		default:
			response.BadRequest(c, err.Error())
		}
		return
	}

	response.Success(c, gin.H{"message": "排序更新成功"})
}

func (h *EnvHandler) Groups(c *gin.Context) {
	var rawGroups []string
	database.DB.Raw(`SELECT "group" FROM env_vars WHERE "group" != ''`).Scan(&rawGroups)

	groupSet := make(map[string]struct{})
	for _, raw := range rawGroups {
		for _, group := range model.SplitEnvGroups(raw) {
			groupSet[group] = struct{}{}
		}
	}

	groups := make([]string, 0, len(groupSet))
	for group := range groupSet {
		groups = append(groups, group)
	}
	sort.Strings(groups)

	response.Success(c, gin.H{"data": groups})
}

func parseEnvExportIDs(raw string) []uint {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	seen := make(map[uint]struct{}, len(fields))
	result := make([]uint, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		parsed, err := strconv.ParseUint(field, 10, 32)
		if err != nil || parsed == 0 {
			continue
		}
		id := uint(parsed)
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func applyEnvExportIDs(query *gorm.DB, ids []uint) *gorm.DB {
	if len(ids) == 0 {
		return query
	}
	return query.Where("id IN ?", ids)
}

func (h *EnvHandler) Export(c *gin.Context) {
	var envs []model.EnvVar
	query := applyEnvExportIDs(orderedEnvQuery(), parseEnvExportIDs(c.Query("ids")))
	query.Where("enabled = ?", true).Find(&envs)

	data := make(map[string]string)
	for _, e := range envs {
		data[e.Name] = e.Value
	}

	response.Success(c, gin.H{"data": data})
}

func (h *EnvHandler) ExportAll(c *gin.Context) {
	var envs []model.EnvVar
	applyEnvExportIDs(orderedEnvQuery(), parseEnvExportIDs(c.Query("ids"))).Find(&envs)

	data := make([]map[string]interface{}, len(envs))
	for i, e := range envs {
		data[i] = map[string]interface{}{
			"name":    e.Name,
			"value":   e.Value,
			"remarks": e.Remarks,
			"group":   e.Group,
			"groups":  model.SplitEnvGroups(e.Group),
			"enabled": e.Enabled,
		}
	}

	response.Success(c, gin.H{"data": data})
}

func (h *EnvHandler) ExportFiles(c *gin.Context) {
	var req struct {
		Format      string `json:"format"`
		EnabledOnly *bool  `json:"enabled_only"`
		IDs         []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Format = "all"
	}
	if req.Format == "" {
		req.Format = "all"
	}

	query := applyEnvExportIDs(orderedEnvQuery(), req.IDs)
	if len(req.IDs) == 0 && req.EnabledOnly != nil && *req.EnabledOnly {
		query = query.Where("enabled = ?", true)
	}

	var envs []model.EnvVar
	query.Find(&envs)

	grouped := groupEnvs(envs)

	result := make(map[string]string)
	if req.Format == "shell" || req.Format == "all" {
		result["shell"] = exportShell(grouped)
	}
	if req.Format == "js" || req.Format == "all" {
		result["js"] = exportJS(grouped)
	}
	if req.Format == "python" || req.Format == "all" {
		result["python"] = exportPython(grouped)
	}

	response.Success(c, gin.H{"data": result})
}

func groupEnvs(envs []model.EnvVar) map[string]string {
	grouped := make(map[string][]string)
	for _, e := range envs {
		grouped[e.Name] = append(grouped[e.Name], e.Value)
	}
	result := make(map[string]string)
	for name, vals := range grouped {
		result[name] = service.JoinTaskEnvValues(vals)
	}
	return result
}

func exportShell(envs map[string]string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# 呆呆面板 - 环境变量\n\n")

	keys := sortedKeys(envs)
	for _, k := range keys {
		v := envs[k]
		escaped := strings.ReplaceAll(v, "'", "'\\''")
		b.WriteString(fmt.Sprintf("export %s='%s'\n", k, escaped))
	}
	return b.String()
}

func exportJS(envs map[string]string) string {
	var b strings.Builder
	b.WriteString("// 呆呆面板 - 环境变量\n\n")

	keys := sortedKeys(envs)
	for _, k := range keys {
		v := envs[k]
		escaped := strings.ReplaceAll(v, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		b.WriteString(fmt.Sprintf("process.env.%s = \"%s\";\n", k, escaped))
	}
	return b.String()
}

func exportPython(envs map[string]string) string {
	var b strings.Builder
	b.WriteString("# -*- coding: utf-8 -*-\n")
	b.WriteString("# 呆呆面板 - 环境变量\n")
	b.WriteString("import os\n\n")

	keys := sortedKeys(envs)
	for _, k := range keys {
		v := envs[k]
		escaped := strings.ReplaceAll(v, "'", "\\'")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		b.WriteString(fmt.Sprintf("os.environ['%s'] = '%s'\n", k, escaped))
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (h *EnvHandler) Import(c *gin.Context) {
	var req struct {
		Envs []map[string]interface{} `json:"envs" binding:"required"`
		Mode string                   `json:"mode"`
	}
	limitEnvRequestBody(c)
	if err := c.ShouldBindJSON(&req); err != nil {
		if isRequestBodyTooLarge(err) {
			response.BadRequest(c, "请求体过大（最大 1MB）")
			return
		}
		response.BadRequest(c, "请求参数错误")
		return
	}

	if req.Mode == "" {
		req.Mode = "merge"
	}

	if req.Mode == "replace" {
		database.DB.Where("1 = 1").Delete(&model.EnvVar{})
	}

	imported := 0
	errors := []string{}

	for i, item := range req.Envs {
		name, _ := item["name"].(string)
		value, _ := item["value"].(string)
		if name == "" {
			errors = append(errors, fmt.Sprintf("第 %d 项: 缺少名称", i+1))
			continue
		}

		if !envNamePattern.MatchString(name) {
			errors = append(errors, fmt.Sprintf("第 %d 项: 名称 '%s' 格式无效", i+1, name))
			continue
		}

		remarks, _ := item["remarks"].(string)
		group, hasGroup := envGroupValueFromImportItem(item)

		enabled := true
		if statusVal, ok := item["status"].(float64); ok {
			enabled = statusVal == 0
		} else if enabledVal, ok := item["enabled"].(bool); ok {
			enabled = enabledVal
		}

		if req.Mode == "merge" {
			// Match the same business identity as POST /envs: (name, remarks).
			// On hit we overwrite value / group / enabled so imports keep the
			// row stable across token refreshes instead of accumulating
			// duplicates when the value changes.
			var existing model.EnvVar
			if database.DB.Where("name = ? AND remarks = ?", name, remarks).First(&existing).Error == nil {
				updates := map[string]interface{}{
					"value":   value,
					"enabled": enabled,
				}
				if hasGroup {
					updates["group"] = group
				}
				database.DB.Model(&existing).Updates(updates)
				imported++
				continue
			}
		}

		nextPos, err := nextEnvPosition(database.DB, envNormalSortOrder)
		if err != nil {
			errors = append(errors, fmt.Sprintf("item %d: %s", i+1, err.Error()))
			continue
		}

		env := model.EnvVar{
			Name:      name,
			Value:     value,
			Remarks:   remarks,
			Group:     group,
			Enabled:   enabled,
			SortOrder: envNormalSortOrder,
			Position:  nextPos,
		}
		if err := database.DB.Create(&env).Error; err != nil {
			errors = append(errors, fmt.Sprintf("item %d: %s", i+1, err.Error()))
			continue
		}
		imported++
	}

	if imported == 0 && len(errors) > 0 {
		response.BadRequest(c, "没有成功导入任何环境变量")
		return
	}

	c.JSON(201, gin.H{
		"message": fmt.Sprintf("成功导入 %d 个环境变量", imported),
		"errors":  errors,
	})
}

func (h *EnvHandler) MoveToTop(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var env model.EnvVar
	if err := database.DB.First(&env, id).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		var firstPinned model.EnvVar
		err := tx.Where("sort_order = ?", envPinnedSortOrder).
			Order("position ASC, id ASC").
			First(&firstPinned).Error

		newPos := envPositionStep
		if err == nil {
			newPos = firstPinned.Position - envPositionStep
		} else if err != gorm.ErrRecordNotFound {
			return err
		}

		return tx.Model(&env).Updates(map[string]interface{}{
			"sort_order": envPinnedSortOrder,
			"position":   newPos,
		}).Error
	}); err != nil {
		response.InternalError(c, "置顶失败")
		return
	}

	response.Success(c, gin.H{"message": "已置顶"})
}

func (h *EnvHandler) CancelMoveToTop(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var env model.EnvVar
	if err := database.DB.First(&env, id).Error; err != nil {
		response.NotFound(c, "环境变量不存在")
		return
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		return appendEnvToSortBucket(tx, &env, envNormalSortOrder)
	}); err != nil {
		response.InternalError(c, "取消置顶失败")
		return
	}

	response.Success(c, gin.H{"message": "已取消置顶"})
}

func (h *EnvHandler) BatchSetGroup(c *gin.Context) {
	var req struct {
		IDs    []uint   `json:"ids" binding:"required"`
		Group  string   `json:"group"`
		Groups []string `json:"groups"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	result := database.DB.Model(&model.EnvVar{}).
		Where("id IN ?", req.IDs).
		Updates(map[string]interface{}{"group": normalizeEnvGroupsPayload(req.Group, req.Groups)})
	if result.Error != nil {
		response.InternalError(c, "批量分组失败")
		return
	}

	response.Success(c, gin.H{"message": fmt.Sprintf("已更新 %d 个变量的分组", result.RowsAffected)})
}

func (h *EnvHandler) RegisterRoutes(r *gin.RouterGroup) {
	envs := r.Group("/envs", middleware.JWTAuth(), middleware.OpenAPIAccess("envs"), middleware.RequireRole("operator"))
	{
		envs.GET("", h.List)
		envs.GET("/:id", h.Get)
		envs.POST("", h.Create)
		envs.PUT("/:id", h.Update)
		envs.DELETE("/:id", h.Delete)
		envs.PUT("/:id/enable", h.Enable)
		envs.PUT("/:id/disable", h.Disable)
		envs.DELETE("/batch", h.BatchDelete)
		envs.PUT("/batch/rename", h.BatchRename)
		envs.PUT("/batch/enable", h.BatchEnable)
		envs.PUT("/batch/disable", h.BatchDisable)
		envs.PUT("/batch/group", h.BatchSetGroup)
		envs.GET("/export", h.Export)
		envs.PUT("/sort", h.Sort)
		envs.PUT("/:id/move-top", h.MoveToTop)
		envs.PUT("/:id/cancel-top", h.CancelMoveToTop)
		envs.GET("/groups", h.Groups)
		envs.GET("/export-all", h.ExportAll)
		envs.POST("/export-files", h.ExportFiles)
		envs.POST("/import", h.Import)
	}
}
