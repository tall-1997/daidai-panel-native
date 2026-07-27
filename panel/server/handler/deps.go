package handler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
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

type depLogBroadcaster struct {
	mu   sync.RWMutex
	subs map[chan string]struct{}
}

var (
	depLogStreams   = make(map[uint]*depLogBroadcaster)
	depLogStreamsMu sync.RWMutex
	depOperations   = make(map[uint]context.CancelFunc)
	depOpsMu        sync.Mutex

	dependencyInstallRunner  = installDependency
	dependencyExportTextFunc = buildDependencyExportText
)

const dependencyOperationTimeout = 20 * time.Minute

func getOrCreateBroadcaster(id uint) *depLogBroadcaster {
	depLogStreamsMu.Lock()
	defer depLogStreamsMu.Unlock()
	if b, ok := depLogStreams[id]; ok {
		return b
	}
	b := &depLogBroadcaster{subs: make(map[chan string]struct{})}
	depLogStreams[id] = b
	return b
}

func removeBroadcaster(id uint) {
	depLogStreamsMu.Lock()
	defer depLogStreamsMu.Unlock()
	if b, ok := depLogStreams[id]; ok {
		b.mu.Lock()
		for ch := range b.subs {
			close(ch)
		}
		b.mu.Unlock()
		delete(depLogStreams, id)
	}
}

func registerDepOperation(id uint, cancel context.CancelFunc) {
	depOpsMu.Lock()
	defer depOpsMu.Unlock()
	depOperations[id] = cancel
}

func unregisterDepOperation(id uint) {
	depOpsMu.Lock()
	defer depOpsMu.Unlock()
	delete(depOperations, id)
}

func cancelDepOperation(id uint) bool {
	depOpsMu.Lock()
	cancel, exists := depOperations[id]
	depOpsMu.Unlock()
	if !exists {
		return false
	}

	cancel()
	return true
}

func (b *depLogBroadcaster) subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *depLogBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

func (b *depLogBroadcaster) broadcast(line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *depLogBroadcaster) done() {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- "\x00DONE":
		default:
		}
	}
}

type DepsHandler struct{}

func NewDepsHandler() *DepsHandler {
	return &DepsHandler{}
}

func normalizeDependencyPythonVersion(depType, raw string) (string, error) {
	if depType != model.DepTypePython {
		return "", nil
	}
	return service.NormalizePythonVersionStrict(raw)
}

func dependencyPythonInstallVersions(depType string) []string {
	if depType != model.DepTypePython {
		return []string{""}
	}
	return service.SupportedPythonVersions()
}

func (h *DepsHandler) List(c *gin.Context) {
	depType := c.DefaultQuery("type", "nodejs")

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[depType] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}

	var deps []model.Dependency
	query := database.DB.Where("type = ?", depType)
	if depType == model.DepTypePython {
		pythonVersion, err := normalizeDependencyPythonVersion(depType, c.Query("python_version"))
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		query = query.Where("COALESCE(NULLIF(python_version, ''), ?) = ?", service.LegacyPythonVersion(), pythonVersion)
	}
	query.Order("created_at DESC").Find(&deps)

	data := make([]map[string]interface{}, len(deps))
	for i, d := range deps {
		data[i] = d.ToDict()
	}

	response.Success(c, gin.H{"data": data, "total": len(data)})
}

func (h *DepsHandler) Create(c *gin.Context) {
	var req struct {
		Type          string   `json:"type" binding:"required"`
		Names         []string `json:"names" binding:"required"`
		PythonVersion string   `json:"python_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[req.Type] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}
	created := []map[string]interface{}{}
	skipped := 0
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, ";|&`$(){}") {
			continue
		}

		for _, pythonVersion := range dependencyPythonInstallVersions(req.Type) {
			// Python 依赖按 PEP 503 归一化键去重：同名（忽略大小写/分隔符差异）已存在且
			// 已安装/安装中/排队中的，跳过、不重复安装。
			if req.Type == model.DepTypePython {
				if _, exists := service.FindExistingPythonDependency(name, pythonVersion,
					model.DepStatusInstalled, model.DepStatusInstalling, model.DepStatusQueued); exists {
					skipped++
					continue
				}
			}

			dep := model.Dependency{
				Type:          req.Type,
				Name:          name,
				PythonVersion: pythonVersion,
				Status:        model.DepStatusInstalling,
			}
			if err := database.DB.Create(&dep).Error; err != nil {
				continue
			}
			created = append(created, dep.ToDict())

			go dependencyInstallRunner(dep.ID, req.Type, name)
		}
	}

	message := fmt.Sprintf("已提交 %d 个依赖安装", len(created))
	if req.Type == model.DepTypePython && len(created) > 0 {
		message = fmt.Sprintf("已提交 %d 个 Python 版本依赖安装", len(created))
	}
	if skipped > 0 {
		message = fmt.Sprintf("%s，已存在跳过 %d 个", message, skipped)
	}
	response.Created(c, gin.H{
		"message": message,
		"data":    created,
	})
}

func (h *DepsHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status == model.DepStatusQueued || dep.Status == model.DepStatusInstalling || dep.Status == model.DepStatusRemoving {
		response.BadRequest(c, "依赖正在处理中")
		return
	}

	if c.Query("force") == "true" {
		database.DB.Delete(&dep)
		go forceUninstallDependency(dep.Type, dep.Name, dep.PythonVersion)
		response.Success(c, gin.H{"message": "强制卸载中"})
		return
	}

	database.DB.Model(&dep).Update("status", model.DepStatusRemoving)

	go uninstallDependency(dep.ID, dep.Type, dep.Name, dep.PythonVersion)

	response.Success(c, gin.H{"message": "卸载中"})
}

func (h *DepsHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var deps []model.Dependency
	database.DB.Where("id IN ? AND status NOT IN ?", req.IDs, []string{model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving}).Find(&deps)

	for _, dep := range deps {
		database.DB.Delete(&dep)
		go forceUninstallDependency(dep.Type, dep.Name, dep.PythonVersion)
	}

	response.Success(c, gin.H{"message": fmt.Sprintf("已提交 %d 个依赖卸载", len(deps))})
}

func (h *DepsHandler) GetStatus(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	response.Success(c, gin.H{"data": dep.ToDictWithLog()})
}

func (h *DepsHandler) LogStream(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "依赖不存在"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	if dep.Log != "" {
		for _, line := range strings.Split(dep.Log, "\n") {
			if line != "" {
				fmt.Fprintf(c.Writer, "data: %s\n\n", line)
			}
		}
		c.Writer.Flush()
	}

	if dep.Status != model.DepStatusInstalling && dep.Status != model.DepStatusRemoving {
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", dep.Status)
		c.Writer.Flush()
		return
	}

	depLogStreamsMu.RLock()
	b, exists := depLogStreams[uint(id)]
	depLogStreamsMu.RUnlock()

	if !exists {
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", dep.Status)
		c.Writer.Flush()
		return
	}

	sub := b.subscribe()
	defer b.unsubscribe(sub)

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
				var latest model.Dependency
				database.DB.First(&latest, id)
				fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", latest.Status)
				c.Writer.Flush()
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", line)
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

func (h *DepsHandler) Reinstall(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status == model.DepStatusQueued || dep.Status == model.DepStatusInstalling || dep.Status == model.DepStatusRemoving {
		response.BadRequest(c, "依赖正在处理中")
		return
	}

	database.DB.Model(&dep).Updates(map[string]interface{}{
		"status": model.DepStatusInstalling,
		"log":    "",
	})

	go dependencyInstallRunner(dep.ID, dep.Type, dep.Name)

	response.Success(c, gin.H{"message": "重新安装中"})
}

func appendDepsLog(existing, line string) string {
	existing = strings.TrimRight(existing, "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return existing
	}
	if existing == "" {
		return line
	}
	if strings.Contains(existing, line) {
		return existing
	}
	return existing + "\n" + line
}

func (h *DepsHandler) BatchReinstall(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var deps []model.Dependency
	database.DB.Where("id IN ? AND status NOT IN ?", req.IDs, []string{model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving}).Find(&deps)
	if len(deps) == 0 {
		response.BadRequest(c, "选中的依赖当前无法重装")
		return
	}

	depMap := make(map[uint]model.Dependency, len(deps))
	for _, dep := range deps {
		depMap[dep.ID] = dep
	}

	queue := make([]model.Dependency, 0, len(req.IDs))
	for _, id := range req.IDs {
		dep, ok := depMap[id]
		if !ok {
			continue
		}
		queue = append(queue, dep)
	}
	if len(queue) == 0 {
		response.BadRequest(c, "选中的依赖当前无法重装")
		return
	}

	for index, dep := range queue {
		database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
			"status": model.DepStatusQueued,
			"log":    appendDepsLog(dep.Log, fmt.Sprintf("[批量重装] 已加入顺序队列（%d/%d）", index+1, len(queue))),
		})
	}

	go func(ordered []model.Dependency) {
		for index, dep := range ordered {
			var current model.Dependency
			if err := database.DB.First(&current, dep.ID).Error; err != nil {
				continue
			}

			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusInstalling,
				"log":    appendDepsLog(current.Log, fmt.Sprintf("[批量重装] 开始执行（%d/%d）", index+1, len(ordered))),
			})

			dependencyInstallRunner(dep.ID, dep.Type, dep.Name)
		}
	}(queue)

	response.Success(c, gin.H{"message": fmt.Sprintf("已提交 %d 个依赖顺序重装", len(queue))})
}

func (h *DepsHandler) Cancel(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status != model.DepStatusInstalling && dep.Status != model.DepStatusRemoving {
		response.BadRequest(c, "当前依赖任务未在处理中")
		return
	}

	if !cancelDepOperation(uint(id)) {
		response.BadRequest(c, "当前依赖任务未在运行中")
		return
	}

	response.Success(c, gin.H{"message": "取消请求已提交"})
}

func (h *DepsHandler) PipList(c *gin.Context) {
	pythonVersion, err := service.NormalizePythonVersionStrict(c.Query("python_version"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	pipEnv := service.SanitizePipEnv(os.Environ())
	listCmd, err := service.NewPipCommandForPythonVersion(pythonVersion, []string{"list", "--format=json"})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	listCmd.Env = pipEnv
	out, err := listCmd.Output()
	if err != nil {
		response.InternalError(c, "pip 不可用")
		return
	}
	c.Data(200, "application/json", out)
}

func (h *DepsHandler) Export(c *gin.Context) {
	depType := c.DefaultQuery("type", model.DepTypeNodeJS)

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[depType] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}

	var deps []model.Dependency
	query := database.DB.Where("type = ? AND status = ?", depType, model.DepStatusInstalled)
	filenameType := depType
	if depType == model.DepTypePython {
		pythonVersion, err := normalizeDependencyPythonVersion(depType, c.Query("python_version"))
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		query = query.Where("COALESCE(NULLIF(python_version, ''), ?) = ?", service.LegacyPythonVersion(), pythonVersion)
		filenameType = depType + "-" + strings.ReplaceAll(pythonVersion, ".", "")
	}
	query.Order("name ASC").Find(&deps)

	text, err := dependencyExportTextFunc(depType, deps)
	if err != nil {
		response.InternalError(c, "导出依赖清单失败: "+err.Error())
		return
	}

	filename := fmt.Sprintf("dependencies-%s-%s.txt", filenameType, time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.String(200, text)
}

func (h *DepsHandler) PythonRuntimes(c *gin.Context) {
	response.Success(c, gin.H{
		"data":            service.PythonRuntimeInfos(),
		"default_version": service.DefaultPythonVersion(),
	})
}

func (h *DepsHandler) SetDefaultPythonRuntime(c *gin.Context) {
	var req struct {
		Version string `json:"version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}
	version, err := service.NormalizePythonVersionStrict(req.Version)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if !service.PythonVersionSupportedByCurrentRuntime(version) {
		response.BadRequest(c, fmt.Sprintf("当前镜像不支持 Python %s，请切换到对应 Python 版本镜像或 all 镜像", version))
		return
	}
	if err := model.SetConfig("python_default_version", version); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "默认 Python 版本已更新", "default_version": version})
}

func (h *DepsHandler) NpmList(c *gin.Context) {
	out, err := exec.Command("npm", "list", "-g", "--json", "--depth=0").Output()
	if err != nil {
		response.InternalError(c, "npm 不可用")
		return
	}
	c.Data(200, "application/json", out)
}

func (h *DepsHandler) GetMirrors(c *gin.Context) {
	result := gin.H{
		"pip_mirror":             service.CurrentEffectivePipMirror(),
		"npm_mirror":             service.CurrentEffectiveNpmMirror(),
		"linux_mirror":           "",
		"linux_package_manager":  "",
		"linux_distribution":     "",
		"linux_mirror_supported": false,
		"linux_mirror_label":     "Linux",
		"linux_mirror_message":   "",
	}

	linuxMirrorInfo := getLinuxMirrorInfo()
	result["linux_package_manager"] = linuxMirrorInfo.Manager
	result["linux_distribution"] = linuxMirrorInfo.Distribution
	result["linux_mirror"] = linuxMirrorInfo.Mirror
	result["linux_mirror_supported"] = linuxMirrorInfo.Supported
	result["linux_mirror_label"] = linuxMirrorInfo.Label
	result["linux_mirror_message"] = linuxMirrorInfo.Message

	response.Success(c, result)
}

func (h *DepsHandler) SetMirrors(c *gin.Context) {
	var req struct {
		PipMirror   *string `json:"pip_mirror"`
		NpmMirror   *string `json:"npm_mirror"`
		LinuxMirror *string `json:"linux_mirror"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var errors []string

	if req.PipMirror != nil {
		if err := service.SetPipMirror(*req.PipMirror); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if req.NpmMirror != nil {
		if err := service.SetNpmMirror(*req.NpmMirror); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if req.LinuxMirror != nil {
		mirror := strings.TrimSpace(*req.LinuxMirror)
		manager, err := detectLinuxPackageManager()
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			distribution := detectLinuxDistribution()
			if err := setLinuxMirror(manager, distribution, mirror); err != nil {
				errors = append(errors, "设置 Linux 镜像源失败: "+err.Error())
			}
		}
	}

	if len(errors) > 0 {
		response.BadRequest(c, strings.Join(errors, "; "))
		return
	}

	response.Success(c, gin.H{"message": "镜像源设置成功"})
}

func runCmdWithSSE(cmd *exec.Cmd, id uint, successStatus string, deleteOnSuccess bool) {
	broadcaster := getOrCreateBroadcaster(id)
	defer removeBroadcaster(id)

	service.SetPgid(cmd)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    err.Error(),
		})
		broadcaster.done()
		return
	}
	cmd.Stderr = cmd.Stdout

	ctx, cancel := context.WithTimeout(context.Background(), dependencyOperationTimeout)
	registerDepOperation(id, cancel)
	defer func() {
		cancel()
		unregisterDepOperation(id)
	}()

	if err := cmd.Start(); err != nil {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    err.Error(),
		})
		broadcaster.done()
		return
	}

	var logBuf strings.Builder
	var logMu sync.Mutex
	var existing model.Dependency
	if err := database.DB.Select("log").First(&existing, id).Error; err == nil && existing.Log != "" {
		logBuf.WriteString(existing.Log)
		if !strings.HasSuffix(existing.Log, "\n") {
			logBuf.WriteString("\n")
		}
	}
	lastPersistAt := time.Now()
	logDirty := false
	appendLine := func(line string, broadcast bool) {
		logMu.Lock()
		defer logMu.Unlock()

		logBuf.WriteString(line)
		logBuf.WriteString("\n")
		logDirty = true
		if broadcast {
			broadcaster.broadcast(line)
		}
	}
	flushLog := func(force bool) {
		logMu.Lock()
		defer logMu.Unlock()

		if !logDirty {
			return
		}
		if !force && time.Since(lastPersistAt) < 250*time.Millisecond {
			return
		}
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", logBuf.String())
		lastPersistAt = time.Now()
		logDirty = false
	}

	appendLine(fmt.Sprintf("[依赖任务已启动，超时阈值：%s]", dependencyOperationTimeout.Truncate(time.Second)), true)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)

		scanner := bufio.NewScanner(pipe)
		scanner.Buffer(make([]byte, 64*1024), 256*1024)
		for scanner.Scan() {
			appendLine(scanner.Text(), true)
			flushLog(false)
		}

		if err := scanner.Err(); err != nil {
			appendLine("[读取安装输出失败] "+err.Error(), true)
		}
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	status := successStatus
	waitErr := error(nil)
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		if cmd.Process != nil {
			service.KillProcessGroup(cmd.Process)
		}
		waitErr = <-waitCh
		if ctx.Err() == context.DeadlineExceeded {
			appendLine("[依赖任务已超时，进程已终止]", true)
			status = model.DepStatusFailed
		} else {
			appendLine("[依赖任务已取消]", true)
			status = model.DepStatusCancelled
		}
	}

	<-scanDone
	if waitErr != nil && status == successStatus {
		status = model.DepStatusFailed
		if hint := buildDependencyFailureHint(logBuf.String()); hint != "" {
			appendLine(hint, true)
		}
	}

	flushLog(true)

	if deleteOnSuccess && status == successStatus {
		database.DB.Delete(&model.Dependency{}, id)
	} else {
		logMu.Lock()
		finalLog := logBuf.String()
		logMu.Unlock()
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": status,
			"log":    finalLog,
		})
	}

	if status == successStatus {
		go service.SnapshotDepsToHost()
	}

	broadcaster.done()
}

func buildDependencyFailureHint(logText string) string {
	lower := strings.ToLower(logText)
	switch {
	case strings.Contains(lower, "could not get lock") ||
		strings.Contains(lower, "unable to acquire the dpkg frontend lock") ||
		strings.Contains(lower, "unable to lock database") ||
		strings.Contains(lower, "another app is currently holding the yum lock"):
		return "[检测到系统包管理器锁冲突，请稍后重试，或先确认没有其他 apt/yum/dnf/apk 任务正在运行]"
	case strings.Contains(lower, "temporary failure resolving") ||
		strings.Contains(lower, "could not resolve") ||
		strings.Contains(lower, "connection timed out") ||
		strings.Contains(lower, "failed to fetch"):
		return "[检测到网络或镜像源异常，请检查 Linux 镜像源配置、代理设置和宿主机网络连通性]"
	case isAlpineGlibcIncompatible(lower):
		return "[当前容器使用 Alpine 镜像（musl libc），该依赖需要 glibc 环境，无法在 Alpine 上安装。请切换到 Debian 版镜像（如 linzixuan/daidai-panel:debian）后重试]"
	default:
		return ""
	}
}

func isAlpineGlibcIncompatible(lowerLog string) bool {
	if detectLinuxDistribution() != "alpine" {
		return false
	}
	glibcHints := []string{
		"no matching distribution",
		"resolutionimpossible",
		"not a supported wheel on this platform",
		"failed to build installable wheels",
		"manylinux",
	}
	for _, hint := range glibcHints {
		if strings.Contains(lowerLog, hint) {
			return true
		}
	}
	return false
}

func ensureTmpDir() {
	os.MkdirAll("/tmp", 0o1777)
}

func installDependency(id uint, depType, name string) {
	ensureTmpDir()
	var cmd *exec.Cmd
	pythonVersion := ""
	if depType == model.DepTypePython {
		var dep model.Dependency
		if err := database.DB.Select("python_version").First(&dep, id).Error; err == nil {
			pythonVersion = dep.PythonVersion
		}
		pythonVersion = service.NormalizePythonVersionOrDefault(pythonVersion)
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("python_version", pythonVersion)
	}
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		if notice := service.NodeInstallCompatibilityNotice(name); notice != "" {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", notice+"\n")
		}

		var err error
		cmd, err = service.NewNpmInstallCommand(name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipInstallCommandForPythonVersion(pythonVersion, name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
		cmd.Env = append(service.PipInstallEnv(service.AppendProxyEnv(os.Environ()), service.CurrentPipMirror()), "TMPDIR=/tmp")
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}

		initialLog := fmt.Sprintf("[Linux] 已检测到包管理器：%s", manager.Binary)
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", initialLog+"\n")

		cmd, err = buildLinuxPackageCommand(manager, "install", name, false)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    initialLog + "\n" + err.Error(),
			})
			return
		}
	default:
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    "不支持的类型",
		})
		return
	}

	runCmdWithSSE(cmd, id, model.DepStatusInstalled, false)
}

func uninstallDependency(id uint, depType, name, pythonVersion string) {
	var cmd *exec.Cmd
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		var err error
		cmd, err = service.NewNpmUninstallCommand(name, false)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipUninstallCommandForPythonVersion(pythonVersion, name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
		cmd.Env = service.SanitizePipEnv(service.AppendProxyEnv(os.Environ()))
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			database.DB.Delete(&model.Dependency{}, id)
			return
		}

		cmd, err = buildLinuxPackageCommand(manager, "remove", name, false)
		if err != nil {
			database.DB.Delete(&model.Dependency{}, id)
			return
		}
	default:
		database.DB.Delete(&model.Dependency{}, id)
		return
	}

	runCmdWithSSE(cmd, id, model.DepStatusInstalled, true)
}

func forceUninstallDependency(depType, name, pythonVersion string) {
	var cmd *exec.Cmd
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		var err error
		cmd, err = service.NewNpmUninstallCommand(name, true)
		if err != nil {
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipUninstallCommandForPythonVersion(pythonVersion, name, "--no-deps")
		if err != nil {
			return
		}
		cmd.Env = service.SanitizePipEnv(service.AppendProxyEnv(os.Environ()))
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			return
		}

		cmd, err = buildLinuxPackageCommand(manager, "remove", name, true)
		if err != nil {
			return
		}
	default:
		return
	}
	cmd.CombinedOutput()
}

func (h *DepsHandler) RegisterRoutes(r *gin.RouterGroup) {
	deps := r.Group("/deps", middleware.JWTAuth(), middleware.RequireAdmin())
	{
		deps.GET("", h.List)
		deps.POST("", h.Create)
		deps.POST("/batch-reinstall", h.BatchReinstall)
		deps.POST("/batch-delete", h.BatchDelete)
		deps.DELETE("/:id", h.Delete)
		deps.PUT("/:id/cancel", h.Cancel)
		deps.GET("/:id/status", h.GetStatus)
		deps.GET("/:id/log-stream", h.LogStream)
		deps.PUT("/:id/reinstall", h.Reinstall)
		deps.GET("/export", h.Export)

		deps.GET("/python-runtimes", h.PythonRuntimes)
		deps.PUT("/python-runtime-default", h.SetDefaultPythonRuntime)
		deps.GET("/pip", h.PipList)
		deps.GET("/npm", h.NpmList)

		deps.GET("/mirrors", h.GetMirrors)
		deps.PUT("/mirrors", h.SetMirrors)
	}
}
