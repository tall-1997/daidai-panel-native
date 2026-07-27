package service

import (
	"log"
	"strings"

	"daidai-panel/database"
	"daidai-panel/model"
)

var dependencyInstalledFunc = DependencyInstalledForPythonVersion
var dependencyReinstallBatchFunc = reinstallDependenciesAsync
var dependencyRestartReinstallBatchFunc = reinstallDependenciesAfterRestartAsync

func ReconcileDependenciesAfterRestart() {
	var installed []model.Dependency
	database.DB.Where("status = ?", model.DepStatusInstalled).Find(&installed)
	reinstallAfterRestart := make([]model.Dependency, 0)
	scheduledRestartReinstallIDs := make(map[uint]struct{})

	for _, dep := range installed {
		if dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion) {
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

	if len(reinstallAfterRestart) > 0 {
		dependencyRestartReinstallBatchFunc(reinstallAfterRestart)
		log.Printf("dep verify: scheduled %d missing dependencies for automatic reinstall after restart", len(reinstallAfterRestart))
	}

	var stale []model.Dependency
	database.DB.Where("status IN ?", []string{model.DepStatusInstalling, model.DepStatusRemoving}).Find(&stale)

	toResume := make([]model.Dependency, 0, len(stale))
	for _, dep := range stale {
		if _, exists := scheduledRestartReinstallIDs[dep.ID]; exists {
			continue
		}

		if dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion) {
			nextLog := appendDependencyLog(dep.Log, "[启动校验] 检测到依赖已安装，已同步状态为已安装")
			database.DB.Model(&dep).Updates(map[string]interface{}{
				"status": model.DepStatusInstalled,
				"log":    nextLog,
			})
			log.Printf("dep verify: %s/%s was %s, reconciled to installed", dep.Type, dep.Name, dep.Status)
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
			log.Printf("dep verify: %s/%s was %s, resumed restore install after restart", dep.Type, dep.Name, dep.Status)
			continue
		}

		database.DB.Model(&dep).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    appendDependencyLog(dep.Log, "[启动校验] 操作因服务重启而中断"),
		})
		log.Printf("dep verify: %s/%s was %s, reset to failed", dep.Type, dep.Name, dep.Status)
	}

	if len(toResume) > 0 {
		dependencyReinstallBatchFunc(toResume)
		log.Printf("dep verify: resumed %d restored dependencies after restart", len(toResume))
	}
}

func shouldResumeRestoredDependency(dep model.Dependency) bool {
	return dep.Status == model.DepStatusInstalling && strings.Contains(dep.Log, "[恢复备份]")
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
