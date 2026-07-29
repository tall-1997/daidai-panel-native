package service

import (
	"log"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

var dependencyReinstallBatchFunc = reinstallDependenciesAsync

type dependencyReconcileRunner struct {
	installed        func(depType, name, pythonVersion string) bool
	reinstall        func([]model.Dependency)
	restartReinstall func([]model.Dependency)
}

func ReconcileDependenciesAfterRestart() {
	reconcileDependenciesAfterRestart(dependencyReconcileRunner{
		installed:        DependencyInstalledForPythonVersion,
		reinstall:        reinstallDependenciesAsync,
		restartReinstall: reinstallDependenciesAfterRestartAsync,
	})
}

func reconcileDependenciesAfterRestart(runner dependencyReconcileRunner) {
	recoverInterruptedDependencyOperations()

	var installed []model.Dependency
	database.DB.Where("status = ?", model.DepStatusInstalled).Order("id ASC").Find(&installed)
	reinstallAfterRestart := make([]model.Dependency, 0)
	scheduledRestartReinstallIDs := make(map[uint]struct{})

	for _, dep := range installed {
		if runner.installed(dep.Type, dep.Name, dep.PythonVersion) {
			continue
		}

		var logMsg string
		switch dep.Type {
		case model.DepTypeLinux:
			logMsg = "[启动校验] 检测到 Linux 依赖在容器重建后丢失，已在重启后自动重新安装"
		case model.DepTypeNodeJS:
			logMsg = "[启动校验] 检测到 Node.js 依赖丢失（可能因重启容器重建），已自动重新安装"
		case model.DepTypePython:
			logMsg = "[启动校验] 检测到 Python 依赖丢失（可能因重启容器重建），已自动重新安装"
		default:
			logMsg = "[启动校验] 依赖未检测到，已自动重新安装"
		}

		nextLog := appendDependencyLog(dep.Log, logMsg)
		database.DB.Model(&dep).Updates(map[string]interface{}{
			"status": model.DepStatusInstalling,
			"log":    nextLog,
		})
		dep.Status = model.DepStatusInstalling
		dep.Log = nextLog
		reinstallAfterRestart = append(reinstallAfterRestart, dep)
		scheduledRestartReinstallIDs[dep.ID] = struct{}{}
		log.Printf("dep verify: %s/%s missing after restart, scheduled automatic reinstall", dep.Type, dep.Name)
	}

	var stale []model.Dependency
	database.DB.Where("status IN ?", []string{model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving}).Order("id ASC").Find(&stale)

	toResume := make([]model.Dependency, 0, len(stale))
	for _, dep := range stale {
		if _, exists := scheduledRestartReinstallIDs[dep.ID]; exists {
			continue
		}

		if runner.installed(dep.Type, dep.Name, dep.PythonVersion) {
			nextLog := appendDependencyLog(dep.Log, "[启动校验] 检测到依赖已安装，已同步状态为已安装")
			database.DB.Model(&dep).Updates(map[string]interface{}{
				"status": model.DepStatusInstalled,
				"log":    nextLog,
			})
			if dep.OperationID != "" {
				_ = DefaultOperationStore().Finish(dep.OperationID, 0, int64(len(nextLog)))
			}
			log.Printf("dep verify: %s/%s was %s, reconciled to installed", dep.Type, dep.Name, dep.Status)
			continue
		}

		if shouldResumeRestartReinstall(dep) {
			reinstallAfterRestart = append(reinstallAfterRestart, dep)
			scheduledRestartReinstallIDs[dep.ID] = struct{}{}
			log.Printf("dep verify: %s/%s resumed automatic reinstall after restart", dep.Type, dep.Name)
			continue
		}

		if shouldResumeRestoredDependency(dep) {
			nextLog := appendDependencyLog(dep.Log, "[启动校验] 检测到恢复任务未完成，已在重启后继续安装")
			database.DB.Model(&dep).Updates(map[string]interface{}{
				"status": model.DepStatusInstalling,
				"log":    nextLog,
			})
			dep.Log = nextLog
			toResume = append(toResume, dep)
			if dep.OperationID != "" {
				_ = DefaultOperationStore().Unknown(dep.OperationID, "DEPENDENCY_OPERATION_RECOVERED", int64(len(nextLog)))
			}
			log.Printf("dep verify: %s/%s was %s, resumed restore install after restart", dep.Type, dep.Name, dep.Status)
			continue
		}

		database.DB.Model(&dep).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    appendDependencyLog(dep.Log, "[启动校验] 操作因服务重启而中断"),
		})
		if dep.OperationID != "" {
			_ = DefaultOperationStore().Unknown(dep.OperationID, "DEPENDENCY_RECOVERY_REQUIRED", int64(len(dep.Log)))
		}
		log.Printf("dep verify: %s/%s was %s, reset to failed", dep.Type, dep.Name, dep.Status)
	}

	if len(reinstallAfterRestart) > 0 {
		runner.restartReinstall(reinstallAfterRestart)
		log.Printf("dep verify: scheduled %d missing dependencies for automatic reinstall after restart", len(reinstallAfterRestart))
	}

	if len(toResume) > 0 {
		runner.reinstall(toResume)
		log.Printf("dep verify: resumed %d restored dependencies after restart", len(toResume))
	}
}

func recoverInterruptedDependencyOperations() {
	if database.DB == nil {
		return
	}
	var operations []model.Operation
	database.DB.Where("kind = ? AND state IN ?", model.OperationKindDependency, []string{model.OperationStatePending, model.OperationStateRunning}).Order("sequence ASC, id ASC").Find(&operations)
	store := DefaultOperationStore()
	for _, op := range operations {
		var dep model.Dependency
		if err := database.DB.Where("operation_id = ?", op.ID).First(&dep).Error; err != nil {
			_ = store.Unknown(op.ID, "DEPENDENCY_OPERATION_ORPHANED", op.LogCursor)
			continue
		}
		switch dep.Status {
		case model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving:
			continue
		case model.DepStatusInstalled:
			_ = store.Finish(op.ID, 0, int64(len(dep.Log)))
		default:
			_ = store.Unknown(op.ID, "DEPENDENCY_OPERATION_RECOVERED", op.LogCursor)
		}
	}
}

func shouldResumeRestoredDependency(dep model.Dependency) bool {
	return dep.Status == model.DepStatusInstalling && strings.Contains(dep.Log, "[恢复备份]")
}

func shouldResumeRestartReinstall(dep model.Dependency) bool {
	return dep.Status == model.DepStatusInstalling && strings.Contains(dep.Log, "[启动校验]") && strings.Contains(dep.Log, "自动重新安装")
}

func appendDependencyLog(existing, line string) string {
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
