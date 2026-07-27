package service

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	cronu "daidai-panel/pkg/cron"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type Scheduler struct {
	mu               sync.Mutex
	cron             *cron.Cron
	entryMap         map[uint]cron.EntryID
	runningProcesses map[uint]*os.Process
	processLock      sync.Mutex
	semaphore        chan struct{}
	liveLogs         map[uint][]string
	liveDone         map[uint]bool
	logLock          sync.Mutex
	scriptsDir       string
	logDir           string
}

var scheduler *Scheduler

func InitScheduler() {
	maxConcurrent := model.GetRegisteredConfigInt("max_concurrent_tasks")

	scheduler = &Scheduler{
		cron:             cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.DefaultLogger))),
		entryMap:         make(map[uint]cron.EntryID),
		runningProcesses: make(map[uint]*os.Process),
		semaphore:        make(chan struct{}, maxConcurrent),
		liveLogs:         make(map[uint][]string),
		liveDone:         make(map[uint]bool),
		scriptsDir:       config.C.Data.ScriptsDir,
		logDir:           config.C.Data.LogDir,
	}

	var tasks []model.Task
	database.DB.Where("status = ?", model.TaskStatusEnabled).Find(&tasks)

	for _, task := range tasks {
		scheduler.AddJob(&task)
	}

	scheduler.cron.Start()
	log.Printf("scheduler started, loaded %d tasks", len(tasks))
}

func ShutdownScheduler() {
	if scheduler == nil {
		return
	}

	ctx := scheduler.cron.Stop()
	<-ctx.Done()

	deadline := time.After(60 * time.Second)
	for {
		scheduler.processLock.Lock()
		count := len(scheduler.runningProcesses)
		scheduler.processLock.Unlock()

		if count == 0 {
			break
		}

		select {
		case <-deadline:
			scheduler.processLock.Lock()
			for _, p := range scheduler.runningProcesses {
				p.Kill()
			}
			scheduler.processLock.Unlock()
			return
		default:
			time.Sleep(2 * time.Second)
		}
	}

	GetLogStreamManager().CloseAll()
}

func GetScheduler() *Scheduler {
	return scheduler
}

func (s *Scheduler) AddJob(task *model.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldID, exists := s.entryMap[task.ID]; exists {
		s.cron.Remove(oldID)
		delete(s.entryMap, task.ID)
	}

	if task.Status != model.TaskStatusEnabled {
		return
	}

	cronExpr := toCronV3(task.CronExpression)
	if cronExpr == "" {
		log.Printf("invalid cron expression for task %d: %s", task.ID, task.CronExpression)
		return
	}

	taskID := task.ID
	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.executeTask(taskID)
	})
	if err != nil {
		log.Printf("failed to add cron job for task %d: %v", task.ID, err)
		return
	}

	s.entryMap[task.ID] = entryID
}

func (s *Scheduler) UpdateJob(task *model.Task) {
	s.AddJob(task)
}

func (s *Scheduler) RemoveJob(taskID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if entryID, exists := s.entryMap[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryMap, taskID)
	}
}

func (s *Scheduler) RunTaskNow(task *model.Task) {
	go s.executeTask(task.ID)
}

func (s *Scheduler) StopRunningTask(taskID uint) bool {
	s.processLock.Lock()
	defer s.processLock.Unlock()

	if p, ok := s.runningProcesses[taskID]; ok {
		// 先打"手动停止"标记再 kill，保证完成块结算时标记已可见。
		markManualStop(taskID)
		KillProcessGroup(p)
		delete(s.runningProcesses, taskID)
		return true
	}
	return false
}

func (s *Scheduler) GetLiveLog(taskID uint) ([]string, bool) {
	s.logLock.Lock()
	defer s.logLock.Unlock()

	lines, _ := s.liveLogs[taskID]
	done, _ := s.liveDone[taskID]

	result := make([]string, len(lines))
	copy(result, lines)
	return result, done
}

func (s *Scheduler) executeTask(taskID uint) {
	s.semaphore <- struct{}{}
	defer func() { <-s.semaphore }()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("task %d panicked: %v", taskID, r)
			database.DB.Model(&model.Task{}).Where("id = ?", taskID).Updates(map[string]interface{}{
				"status":          model.TaskStatusEnabled,
				"last_run_status": model.RunFailed,
				"pid":             gorm.Expr("NULL"),
				"log_path":        gorm.Expr("NULL"),
			})
			s.logLock.Lock()
			s.liveDone[taskID] = true
			s.logLock.Unlock()
		}
	}()

	s.executeTaskInner(taskID)
}

func (s *Scheduler) executeTaskInner(taskID uint) {
	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		log.Printf("task %d not found: %v", taskID, err)
		return
	}

	if !task.AllowMultipleInstances {
		s.processLock.Lock()
		if _, running := s.runningProcesses[taskID]; running {
			s.processLock.Unlock()
			return
		}
		s.processLock.Unlock()
	}

	if task.DependsOn != nil {
		var depTask model.Task
		if err := database.DB.First(&depTask, *task.DependsOn).Error; err == nil {
			if depTask.LastRunStatus == nil || *depTask.LastRunStatus != model.RunSuccess {
				status := model.LogStatusFailed
				taskLog := model.TaskLog{
					TaskID:    taskID,
					Content:   fmt.Sprintf("跳过: 依赖任务 '%s' 上次执行未成功", depTask.Name),
					Status:    &status,
					StartedAt: time.Now(),
				}
				now := time.Now()
				taskLog.EndedAt = &now
				database.DB.Create(&taskLog)
				return
			}
		}
	}

	commandPlan, err := ParseCommandExecutionPlan(task.Command, s.scriptsDir)
	if err != nil {
		log.Printf("task %d parse command failed: %v", taskID, err)
		return
	}

	maxLogSize := model.GetRegisteredConfigInt("max_log_content_size")

	if randomDelay := resolveTaskRandomDelaySeconds(&task, commandPlan); randomDelay > 0 {
		delay := rand.Intn(randomDelay) + 1
		time.Sleep(time.Duration(delay) * time.Second)
	}

	var envVarRecords []model.EnvVar
	// 使用稳定排序并按 name 分组合并，与 runtime_exec.BuildManagedRuntimeEnvMap 保持一致；
	// 避免原实现"后者覆盖前者"导致同名变量只有 1 个账号生效。
	database.DB.Where("enabled = ?", true).
		Order("sort_order DESC, position ASC, created_at ASC, id ASC").
		Find(&envVarRecords)
	envGrouped := make(map[string][]string)
	envOrder := make([]string, 0, len(envVarRecords))
	for _, e := range envVarRecords {
		if _, ok := envGrouped[e.Name]; !ok {
			envOrder = append(envOrder, e.Name)
		}
		envGrouped[e.Name] = append(envGrouped[e.Name], e.Value)
	}
	envVars := make(map[string]string, len(envGrouped))
	for _, name := range envOrder {
		envVars[name] = joinTaskEnvValues(envGrouped[name])
	}

	timeout := task.Timeout
	if timeout < 0 {
		timeout = 0
	}
	if commandPlan.TimeoutOverride != nil && *commandPlan.TimeoutOverride > 0 {
		timeout = *commandPlan.TimeoutOverride
	}

	logRelPath := GetRelativeLogPathForTask(&task)
	logFullPath := filepath.Join(s.logDir, logRelPath)

	status := model.LogStatusRunning
	taskLog := model.TaskLog{
		TaskID:    taskID,
		Status:    &status,
		LogPath:   &logRelPath,
		StartedAt: time.Now(),
	}
	database.DB.Create(&taskLog)

	now := time.Now()
	task.Status = model.TaskStatusRunning
	task.LastRunAt = &now
	task.LogPath = &logRelPath
	database.DB.Save(&task)

	s.logLock.Lock()
	s.liveLogs[taskID] = []string{}
	s.liveDone[taskID] = false
	s.logLock.Unlock()

	lsm := GetLogStreamManager()

	onOutput := func(chunk string) {
		lsm.Write(logFullPath, chunk)
		s.logLock.Lock()
		s.liveLogs[taskID] = append(s.liveLogs[taskID], chunk)
		s.logLock.Unlock()
	}

	startTime := time.Now()
	onOutput(fmt.Sprintf("=== 开始执行 [%s] ===\n", startTime.Format("2006-01-02 15:04:05")))

	if task.TaskBefore != nil && *task.TaskBefore != "" {
		onOutput("[执行前置脚本]\n")
		RunInlineScript(*task.TaskBefore, s.scriptsDir, envVars, 60, onOutput, commandPlan.ScriptArgs...)
	}

	RunHookScript("task_before.sh", s.scriptsDir, envVars, onOutput, commandPlan.ScriptArgs...)

	success := false
	retries := 0
	var lastExitCode int

	for retries <= task.MaxRetries {
		if retries > 0 {
			onOutput(fmt.Sprintf("[第 %d 次重试，等待 %d 秒]\n", retries, task.RetryInterval))
			time.Sleep(time.Duration(task.RetryInterval) * time.Second)
		}

		onStart := func(process *os.Process) {
			s.processLock.Lock()
			s.runningProcesses[taskID] = process
			pid := process.Pid
			s.processLock.Unlock()
			database.DB.Model(&task).Update("pid", pid)
		}
		result, _, err := RunCommandWithPlan(commandPlan, timeout, envVars, maxLogSize, onOutput, onStart)
		if err != nil {
			onOutput(fmt.Sprintf("[执行错误: %s]\n", err.Error()))
			retries++
			lastExitCode = 1
			continue
		}

		lastExitCode = result.ReturnCode
		if task.IsSuccessExitCode(result.ReturnCode) {
			success = true
			break
		}

		retries++
	}

	if task.TaskAfter != nil && *task.TaskAfter != "" {
		onOutput("[执行后置脚本]\n")
		RunInlineScript(*task.TaskAfter, s.scriptsDir, envVars, 60, onOutput, commandPlan.ScriptArgs...)
	}

	RunHookScript("task_after.sh", s.scriptsDir, envVars, onOutput, commandPlan.ScriptArgs...)
	RunHookScript("extra.sh", s.scriptsDir, envVars, onOutput, commandPlan.ScriptArgs...)

	endTime := time.Now()
	duration := endTime.Sub(startTime).Seconds()

	completionNote := ""
	if success && lastExitCode != 0 {
		completionNote = "（已按任务配置判定成功）"
	}
	onOutput(fmt.Sprintf("=== 执行结束 [%s] 耗时 %.2f 秒 退出码 %d%s ===\n",
		endTime.Format("2006-01-02 15:04:05"), duration, lastExitCode, completionNote))

	runStatus := model.RunSuccess
	logStatus := model.LogStatusSuccess
	if !success {
		runStatus = model.RunFailed
		logStatus = model.LogStatusFailed
	}

	// 主动停止：统一结算为 Aborted，避免混入成功或失败统计。
	// applyManualStopOverride 读即清标记，自然完成时返回原状态。
	runStatus, logStatus, _ = applyManualStopOverride(taskID, runStatus, logStatus)

	database.DB.Model(&task).Updates(map[string]interface{}{
		"status":            model.TaskStatusEnabled,
		"last_run_status":   runStatus,
		"last_running_time": duration,
		"pid":               gorm.Expr("NULL"),
		"log_path":          gorm.Expr("NULL"),
	})

	endNow := time.Now()
	database.DB.Model(&taskLog).Updates(map[string]interface{}{
		"ended_at": endNow,
		"duration": duration,
		"status":   logStatus,
	})

	s.processLock.Lock()
	delete(s.runningProcesses, taskID)
	s.processLock.Unlock()

	lsm.CloseStream(logFullPath)

	s.logLock.Lock()
	s.liveDone[taskID] = true
	s.logLock.Unlock()

	go func() {
		time.Sleep(60 * time.Second)
		s.logLock.Lock()
		delete(s.liveLogs, taskID)
		s.logLock.Unlock()
		time.Sleep(4 * time.Minute)
		s.logLock.Lock()
		delete(s.liveDone, taskID)
		s.logLock.Unlock()
	}()
}

func toCronV3(expression string) string {
	expression = strings.TrimSpace(expression)
	parts := strings.Fields(expression)

	result := cronu.Parse(expression)
	if !result.Valid {
		return ""
	}

	if len(parts) == 5 {
		return "0 " + expression
	}
	if len(parts) == 6 {
		return expression
	}
	return ""
}

func GetTaskStats(taskID uint, days int) map[string]interface{} {
	since := time.Now().AddDate(0, 0, -days)

	var task model.Task
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return nil
	}

	var logs []model.TaskLog
	database.DB.Where("task_id = ? AND started_at > ?", taskID, since).
		Order("started_at DESC").Find(&logs)

	totalRuns := len(logs)
	successRuns := 0
	failedRuns := 0
	abortedRuns := 0
	var totalDuration, maxDuration, minDuration float64
	minDuration = -1

	for _, l := range logs {
		if l.Status != nil {
			if *l.Status == model.LogStatusSuccess {
				successRuns++
			} else if *l.Status == model.LogStatusFailed {
				failedRuns++
			} else if *l.Status == model.LogStatusAborted {
				abortedRuns++
			}
		}
		if l.Duration != nil {
			totalDuration += *l.Duration
			if *l.Duration > maxDuration {
				maxDuration = *l.Duration
			}
			if minDuration < 0 || *l.Duration < minDuration {
				minDuration = *l.Duration
			}
		}
	}

	avgDuration := 0.0
	if totalRuns > 0 {
		avgDuration = totalDuration / float64(totalRuns)
	}
	if minDuration < 0 {
		minDuration = 0
	}

	successRate := 0.0
	// 成功率只统计自然完成的成功 / 失败，Aborted 单独统计，不拉低成功率。
	finishedRuns := successRuns + failedRuns
	if finishedRuns > 0 {
		successRate = float64(successRuns) / float64(finishedRuns) * 100
	}

	recentLogs := logs
	if len(recentLogs) > 10 {
		recentLogs = recentLogs[:10]
	}

	recentData := make([]map[string]interface{}, len(recentLogs))
	for i, l := range recentLogs {
		recentData[i] = map[string]interface{}{
			"id":         l.ID,
			"started_at": l.StartedAt,
			"ended_at":   l.EndedAt,
			"status":     l.Status,
			"duration":   l.Duration,
		}
	}

	return map[string]interface{}{
		"task_id":     taskID,
		"task_name":   task.Name,
		"period_days": days,
		"stats": map[string]interface{}{
			"total_runs":   totalRuns,
			"success_runs": successRuns,
			"failed_runs":  failedRuns,
			"aborted_runs": abortedRuns,
			"success_rate": roundFloat(successRate, 2),
			"avg_duration": roundFloat(avgDuration, 2),
			"max_duration": roundFloat(maxDuration, 2),
			"min_duration": roundFloat(minDuration, 2),
		},
		"recent_logs": recentData,
	}
}

func roundFloat(val float64, precision int) float64 {
	p := 1.0
	for i := 0; i < precision; i++ {
		p *= 10
	}
	result, _ := strconv.ParseFloat(fmt.Sprintf("%.*f", precision, val), 64)
	return result
}
