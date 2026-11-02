package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

var dependencyInstalledFunc = DependencyInstalledForPythonVersion
var dependencyReinstallBatchFunc = reinstallDependenciesAsync
var dependencyRestartReinstallBatchFunc = reinstallDependenciesAfterRestartAsync

// pythonInstalledPackagesFunc 一次列出某个 Python 版本已装的发行包（键是 PEP 503 规范名），只给启动校验用。
// 返回 nil 表示列举失败，这个版本退回逐条 dependencyInstalledFunc。
// 抽成包级变量是为了单测替换：有 Python 记录的启动校验用例必须打桩，否则会真去建 venv、跑 pip。
var pythonInstalledPackagesFunc = listPythonInstalledPackages

// pythonInstalledPackagesTimeout 只管 pip list 这一条命令（慢 NAS 上它也就几秒）。给足 60 秒：
// 设得太短，慢机器上会悄悄退回逐条校验，等于没修。
// 直接列时没有健康检查；走老路时，NewPipCommandForPythonVersion 返回前那遍 venv 健康检查（2 个子进程）不归它管。
var pythonInstalledPackagesTimeout = 60 * time.Second

// dependencyReconcileSlowLogThreshold：整轮启动校验超过这么久才打一行分类型耗时，正常启动不多刷日志。
var dependencyReconcileSlowLogThreshold = 3 * time.Second

// pipListHiddenPackageNames 是 pip list 写死不列出的几个名字（pip 源码里的 stdlib_pkgs，21.3 ~ 25.0 都一样），
// pip show 却照样查得到。快照里没有它们时不能直接判缺，要按老办法逐条问一次。
var pipListHiddenPackageNames = map[string]bool{"python": true, "wsgiref": true, "argparse": true}

func ReconcileDependenciesAfterRestart() {
	recoverInterruptedDependencyOperations()
	startedAt := time.Now()
	// 判定耗时按依赖类型累加（Python 含建快照的那次 pip list），整轮超过阈值时打出来，给慢启动定位用（#156）。
	checkedCount := 0
	checkCostByType := map[string]time.Duration{}
	// 每个 Python 版本懒加载一次已装包快照，第一轮与 stale 轮共用；值为 nil 表示列举失败、这个版本逐条校验。
	// 以前每条 Python 依赖都要起 5 个子进程（判缺 12 个），慢 NAS 上几十条就能把面板拖住几分钟才开始监听。
	pythonSnapshots := map[string]map[string]bool{}
	// staleRound：第二轮（安装中 / 排队中 / 卸载中）传 true，第一轮（已安装）传 false。
	dependencyStillInstalled := func(dep model.Dependency, staleRound bool) bool {
		checkStartedAt := time.Now()
		defer func() {
			checkedCount++
			checkCostByType[dep.Type] += time.Since(checkStartedAt)
		}()

		// Node.js 只 stat 目录、Linux 只起一个 dpkg-query / apk，本来就便宜，照旧逐条判定。
		if dep.Type != model.DepTypePython {
			return dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion)
		}

		// stale 轮里名字带版本约束 / extras / url / marker（= > < ~ ! [ @ ; 任一字符）的不查快照，照旧逐条精确判定。
		// 快照只看主包在不在，可这些记录的操作还没做完：恢复备份时 requests==2.31.0 装到一半重启、环境里留着旧版 requests，
		// 按主包判会直接记成已安装，续装不再发生、钉住的版本一直装不上；排队中的 PyYAML==6.0 一步都没执行，也会被记成已安装。
		// 逐条 pip show 认不出这种写法、判为未装，于是带 [恢复备份] 的安装中续装、排队中置失败、其余的安装中与卸载中置失败，
		// 与改快照之前一致。
		// 不带这些字符的裸包名，主包就是这条记录本身，查快照与逐条问的结论相同；第一轮（已安装）照旧按主包查快照。
		if staleRound && strings.ContainsAny(dep.Name, "=><~![@;") {
			return dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion)
		}

		version := NormalizeDependencyPythonVersion(dep.PythonVersion)
		snapshot, loaded := pythonSnapshots[version]
		if !loaded {
			var err error
			snapshot, err = pythonInstalledPackagesFunc(version)
			if snapshot == nil {
				// 每个版本只打这一行：版本不支持、pip 坏了、超时、输出解析失败都在这里写明原因。
				log.Printf("[启动校验] 列举 Python %s 已安装的包失败，该版本依赖改为逐条校验：%v", version, err)
			}
			pythonSnapshots[version] = snapshot
		}
		if snapshot == nil {
			return dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion)
		}

		// 只看主包在不在：requests==2.31.0、httpx[http2]、name @ https://… 都按基础包名查表，与 POST /deps 查重同一个口径。
		// 以前逐条 pip show 认不出这种写法，每次重启都判缺、再在后台重装一遍。
		key := CanonicalizePythonPackageName(dep.Name)
		if snapshot[key] {
			return true
		}
		if pipListHiddenPackageNames[key] {
			return dependencyInstalledFunc(dep.Type, dep.Name, dep.PythonVersion)
		}
		return false
	}

	var installed []model.Dependency
	database.DB.Where("status = ?", model.DepStatusInstalled).Find(&installed)
	reinstallAfterRestart := make([]model.Dependency, 0)
	scheduledRestartReinstallIDs := make(map[uint]struct{})

	for _, dep := range installed {
		if dependencyStillInstalled(dep, false) {
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

	// queued 也必须在这里收口：排队（一键安装 Playwright、批量重装）只活在进程内存里的那个协程中，
	// 面板一重启协程就没了，剩下的记录会永远停在 queued —— 删除、重装、取消、再点一键安装都把 queued
	// 当成「正在处理」拒掉，侧栏角标也一直亮着，用户没有任何办法把它们清掉。
	var stale []model.Dependency
	database.DB.Where("status IN ?", []string{model.DepStatusInstalling, model.DepStatusRemoving, model.DepStatusQueued}).Find(&stale)

	toResume := make([]model.Dependency, 0, len(stale))
	for _, dep := range stale {
		if _, exists := scheduledRestartReinstallIDs[dep.ID]; exists {
			continue
		}

		if dependencyStillInstalled(dep, true) {
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

		// 上次重启前的「自动重新安装」失败留下的 installing 记录：并入本轮重启续装批次，
		// 既保留 installing 状态，也保证重复 reconcile 幂等（用例覆盖）。
		if shouldResumeRestartReinstall(dep) {
			reinstallAfterRestart = append(reinstallAfterRestart, dep)
			scheduledRestartReinstallIDs[dep.ID] = struct{}{}
			if dep.OperationID != "" {
				_ = DefaultOperationStore().Unknown(dep.OperationID, "DEPENDENCY_OPERATION_RECOVERED", int64(len(dep.Log)))
			}
			log.Printf("dep verify: %s/%s resumed automatic reinstall after restart", dep.Type, dep.Name)
			continue
		}

		// 排队中的记录一步都还没执行过，不自动续装而是置为 failed：保守、也不和别的恢复逻辑抢着装。
		// 用户再点一次一键安装（它会复用 failed 的记录）或逐条重装即可恢复。
		// 恢复备份续装那条分支只认 installing（shouldResumeRestoredDependency），不受影响。
		if dep.Status == model.DepStatusQueued {
			database.DB.Model(&dep).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    appendDependencyLog(dep.Log, "[启动校验] 排队中的任务因服务重启而中断，未执行，可重新安装"),
			})
			if dep.OperationID != "" {
				_ = DefaultOperationStore().Unknown(dep.OperationID, "DEPENDENCY_RECOVERY_REQUIRED", int64(len(dep.Log)))
			}
			log.Printf("dep verify: %s/%s was queued before restart, reset to failed", dep.Type, dep.Name)
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
		dependencyRestartReinstallBatchFunc(reinstallAfterRestart)
		log.Printf("dep verify: scheduled %d missing dependencies for automatic reinstall after restart", len(reinstallAfterRestart))
	}

	if len(toResume) > 0 {
		dependencyReinstallBatchFunc(toResume)
		log.Printf("dep verify: resumed %d restored dependencies after restart", len(toResume))
	}

	// 条数是两轮实际判定过的记录数（stale 轮通常为 0）；总耗时含写库，分类型耗时只算判定本身。
	if elapsed := time.Since(startedAt); elapsed >= dependencyReconcileSlowLogThreshold {
		log.Printf("[启动校验] 校验 %d 条已安装依赖耗时 %s（Python %s / Node.js %s / Linux %s）",
			checkedCount, elapsed.Round(time.Millisecond),
			checkCostByType[model.DepTypePython].Round(time.Millisecond),
			checkCostByType[model.DepTypeNodeJS].Round(time.Millisecond),
			checkCostByType[model.DepTypeLinux].Round(time.Millisecond))
	}
}

// listPythonInstalledPackages 跑一次 pip list --format=json，列出这个 Python 版本已装的发行包（键是 PEP 503 规范名）。
// 用的是安装依赖同一个 pip（NewPipCommandForPythonVersion），与逐条 pip show 判定的是同一个环境。
// 先直接用托管 venv 里的 pip 列（venv 健康时整个版本只起这 1 个子进程，#158），失败再走老路：
// NewPipCommandForPythonVersion 做健康检查、坏了就修 / 重建 venv，再列一次。三个坑（#156 核实记录），两次尝试都遵守：
//  1. 必须带 --disable-pip-version-check：pip ≤ 24.0 的普通 pip list 会联网检查自身版本，
//     venv 里的 pip 是建 venv 时带进来的、不随镜像升级；
//  2. 只解析 stdout：中断安装留下的 ~xxx 残目录会让 pip 往 stderr 打「Ignoring invalid distribution」，混进来 JSON 就解析失败；
//  3. 带超时、超时按进程组杀，pip 卡住也拖不住启动。
func listPythonInstalledPackages(pythonVersion string) (map[string]bool, error) {
	args := []string{"list", "--format=json", "--disable-pip-version-check"}
	// runPipList 跑一次 pip list 并解析，直接列与走老路共用：两次尝试只差「命令怎么构造」这一步。
	runPipList := func(cmd *exec.Cmd) (map[string]bool, error) {
		cmd.Env = SanitizePipEnv(os.Environ())
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// 复用 runNodeABICommand：带超时、超时按进程组杀，再用 WaitDelay 兜住攥着输出管道不放的孙进程。
		if err := runNodeABICommand(cmd, pythonInstalledPackagesTimeout); err != nil {
			// stderr 只带最后一行：pip 的报错结论在最后，前面多是告警。
			lastLine := strings.TrimSpace(stderr.String())
			if idx := strings.LastIndexByte(lastLine, '\n'); idx >= 0 {
				lastLine = strings.TrimSpace(lastLine[idx+1:])
			}
			return nil, fmt.Errorf("pip list 执行失败：%v %s", err, lastLine)
		}

		var rows []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
			return nil, fmt.Errorf("pip list 的输出不是预期的 JSON：%v", err)
		}
		installed := make(map[string]bool, len(rows))
		for _, row := range rows {
			if key := CanonicalizePythonPackageName(row.Name); key != "" {
				installed[key] = true
			}
		}
		return installed, nil
	}

	// 托管 venv 里已经有 pip 就直接列（#158）：省掉解析 pip 时那遍 venv 健康检查（python -c + pip --version，
	// 后者和 pip list 一样要导入几百个模块，慢 NAS 上一个就几秒）。找 pip 用的是老路里 Resolve 用的同一个函数，只看文件在不在。
	// 直接列失败（venv 坏了、换镜像后 Python 小版本对不上、pip 只剩半截、超时、输出不是 JSON……）不打日志，直接走下面的老路：
	// NewPipCommandForPythonVersion 会做健康检查，坏了就修 / 重建 venv，再列一次——「venv 损坏时发现并重建」靠的就是这一步，
	// 重建时 Ensure 自己会打日志。
	// 镜像不支持的版本不直接列：照旧由老路报「当前镜像不支持」，这个版本逐条校验。
	if PythonVersionSupportedByCurrentRuntime(pythonVersion) {
		if pipBin := resolveManagedPipBinaryInVenv(ManagedPythonVenvDir(pythonVersion)); pipBin != "" {
			if installed, err := runPipList(exec.Command(pipBin, args...)); err == nil {
				return installed, nil
			}
		}
	}

	cmd, err := NewPipCommandForPythonVersion(pythonVersion, args)
	if err != nil {
		return nil, err
	}
	return runPipList(cmd)
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

// shouldResumeRestartReinstall 识别「上次自动重新安装没跑完就重启」的记录：
// 只认带 [启动校验] + 自动重新安装 标记的 installing，避免把用户手动装的也一并续装。
func shouldResumeRestartReinstall(dep model.Dependency) bool {
	return dep.Status == model.DepStatusInstalling && strings.Contains(dep.Log, "[启动校验]") && strings.Contains(dep.Log, "自动重新安装")
}

// recoverInterruptedDependencyOperations 启动时把上次异常退出留下的 pending/running 依赖操作收口，
// 避免操作记录永远停在「进行中」（本机版 Android 提升）。
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
