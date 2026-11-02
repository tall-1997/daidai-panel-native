package handler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

// Playwright 运行环境一键安装（issue #142）。
//
// 做三件事：把 Chromium 需要的系统库登记成 Linux 依赖、装 Python 的 playwright、
// 把 Chromium 下载到数据卷。系统库登记之后，容器重建时启动校验会按记录在后台自动重装；
// 浏览器与 pip 包本来就在数据卷里，重建不丢。
//
// 为什么要单独开接口，而不是让前端调 POST /deps：包清单要按 os-release 的版本挑、
// 前置检查（发行版 / 架构 / root）必须在建任何记录之前做，浏览器下载也没法用 POST /deps 表达。

// 以下几项抽成包级变量，只为测试能在 Windows 开发机上模拟 Debian 12 容器、root / 非 root、包是否已装。
var (
	playwrightPlanFunc                   = service.PlanPlaywrightRuntime
	playwrightPrivilegeCheckFunc         = service.EnsureLinuxPackageManagerPrivilege
	playwrightDependencyInstalledFunc    = service.DependencyInstalledForPythonVersion
	playwrightDefaultPythonVersionFunc   = service.DefaultPythonVersion
	playwrightBrowserDownloadAppliesFunc = service.PlaywrightBrowserDownloadApplies
	playwrightBrowserInstallCommandFunc  = service.NewPlaywrightBrowserInstallCommand
)

// playwrightInstallMu 让一键安装的「查重 → 复用 / 新建 → 置为排队」整段串行：
// 连点两次时，第二次必须看到第一次已经排上的记录，而不是把同一条 failed 记录再复用一遍。
var playwrightInstallMu sync.Mutex

// playwrightStatusResponse 是 GET /deps/playwright 的响应：判定结果本身 + 三项就绪状态。
// 字段与前端 web/src/api/deps.ts 的 PlaywrightStatus 一一对应。
type playwrightStatusResponse struct {
	service.PlaywrightRuntimePlan
	PythonInstalled   bool `json:"python_installed"`
	BrowsersInstalled bool `json:"browsers_installed"`
	LinuxInstalled    int  `json:"linux_installed"`
	LinuxTotal        int  `json:"linux_total"`
}

func (h *DepsHandler) PlaywrightStatus(c *gin.Context) {
	plan := playwrightPlanFunc()
	// root 判定也并进 supported：降权部署（PUID/PGID）下按钮直接置灰并显示原因，
	// 不让用户点了才收到 400。文案复用 EnsureLinuxPackageManagerPrivilege（已按部署形态给出出路）。
	// 清单照常下发，前端仍能展示「系统库 x/y 已安装」。
	if plan.Supported {
		if err := playwrightPrivilegeCheckFunc(); err != nil {
			plan.Supported = false
			plan.Reason = err.Error()
		}
	}

	response.Success(c, playwrightStatusResponse{
		PlaywrightRuntimePlan: plan,
		PythonInstalled:       playwrightPythonInstalled(),
		BrowsersInstalled:     playwrightBrowsersInstalledIn(plan.BrowsersPath),
		LinuxInstalled:        countInstalledPlaywrightLinuxPackages(plan.Packages),
		LinuxTotal:            len(plan.Packages),
	})
}

func (h *DepsHandler) InstallPlaywright(c *gin.Context) {
	// 两道前置检查都必须排在建任何记录之前：原来 Alpine / 非 root 下点安装会先建出一批记录，
	// 再全部 failed，白白多出一串失败记录和侧栏角标。
	plan := playwrightPlanFunc()
	if !plan.Supported {
		response.BadRequest(c, plan.Reason)
		return
	}
	if err := playwrightPrivilegeCheckFunc(); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	playwrightInstallMu.Lock()
	queue, skipped, err := collectPlaywrightInstallQueue(plan.Packages)
	if err == nil {
		for index, dep := range queue {
			nextLog := appendDepsLog(dep.Log, fmt.Sprintf("[Playwright 一键安装] 已加入顺序队列（%d/%d）", index+1, len(queue)))
			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusQueued,
				"log":    nextLog,
			})
			queue[index].Status = model.DepStatusQueued
			queue[index].Log = nextLog
		}
	}
	playwrightInstallMu.Unlock()
	if err != nil {
		response.InternalError(c, "登记 Playwright 依赖失败，请稍后重试")
		return
	}

	if len(queue) > 0 {
		// 单个协程按「先系统库、后 Python」的顺序依次安装，写法照 BatchReinstall：
		// Linux 包本来就要串行（apt 锁），Python 放最后，是因为它后面接着下载的 Chromium
		// 启动自检时要用到这些系统库。
		go func(ordered []model.Dependency) {
			for index, dep := range ordered {
				var current model.Dependency
				if err := database.DB.First(&current, dep.ID).Error; err != nil {
					continue
				}
				// 排队期间记录的状态目前没有别的路径会改（删除、重装、取消都拒绝 queued），
				// 这里仍然只接手「还在排队」的记录：万一以后有了，也不会把别人正在处理的记录再装一遍。
				if current.Status != model.DepStatusQueued {
					continue
				}
				database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
					"status": model.DepStatusInstalling,
					"log":    appendDepsLog(current.Log, fmt.Sprintf("[Playwright 一键安装] 开始执行（%d/%d）", index+1, len(ordered))),
				})

				dependencyInstallRunner(dep.ID, dep.Type, dep.Name)
			}
		}(queue)
	}

	data := make([]map[string]interface{}, 0, len(queue))
	for i := range queue {
		data = append(data, queue[i].ToDict())
	}
	message := fmt.Sprintf("已加入安装队列，共 %d 项", len(queue))
	if skipped > 0 {
		message = fmt.Sprintf("%s，已就绪或正在处理的 %d 项已跳过", message, skipped)
	}
	response.Created(c, gin.H{
		"message":       message,
		"data":          data,
		"packages":      plan.Packages,
		"browsers_path": plan.BrowsersPath,
	})
}

// collectPlaywrightInstallQueue 逐项决定要入队的记录：先系统库（清单顺序），最后是 Python 的 playwright。
//
// 中途任何一步出错（比如 SQLite busy 建记录失败），要把这一次新建出来的记录删掉再返回：
// 新建的记录一出生就是 queued，而出错时调用方回 500、不会起安装协程，留下来就是没人接手的 queued，
// 删除、重装、取消、再点一键安装都把 queued 当「正在处理」拒掉。复用的旧记录这时还没被改过，原样保留。
// 删除本身失败时只能留给下次启动的 ReconcileDependenciesAfterRestart 收口（它会把 queued 置为 failed）。
func collectPlaywrightInstallQueue(packages []string) ([]model.Dependency, int, error) {
	queue := make([]model.Dependency, 0, len(packages)+1)
	skipped := 0
	var createdIDs []uint
	fail := func(err error) ([]model.Dependency, int, error) {
		if len(createdIDs) > 0 {
			database.DB.Where("id IN ?", createdIDs).Delete(&model.Dependency{})
		}
		return nil, 0, err
	}

	for _, name := range packages {
		dep, enqueue, err := resolvePlaywrightLinuxRecord(name, &createdIDs)
		if err != nil {
			return fail(err)
		}
		if !enqueue {
			skipped++
			continue
		}
		queue = append(queue, dep)
	}

	dep, enqueue, err := resolvePlaywrightPythonRecord(&createdIDs)
	if err != nil {
		return fail(err)
	}
	if enqueue {
		queue = append(queue, dep)
	} else {
		skipped++
	}
	return queue, skipped, nil
}

// resolvePlaywrightLinuxRecord 处理清单里的一个系统包：
//   - 正在排队 / 安装 / 卸载的不动（容器重建后启动校验正在重装的就属于这一类，再排一次等于装两遍）；
//   - 登记为已装且确实装着的跳过；登记为已装、实际却不在的复用原记录重装；
//   - failed / cancelled 的复用原记录，不再新建一条（否则失败记录和侧栏角标越积越多）；
//   - 没登记过的新建，新记录的 id 追加进 created，供 collectPlaywrightInstallQueue 出错时回滚。
func resolvePlaywrightLinuxRecord(name string, created *[]uint) (model.Dependency, bool, error) {
	if _, busy := findExistingDependency(model.DepTypeLinux, name, "",
		model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving); busy {
		return model.Dependency{}, false, nil
	}
	if dep, ok := findExistingDependency(model.DepTypeLinux, name, "", model.DepStatusInstalled); ok {
		if playwrightDependencyInstalledFunc(model.DepTypeLinux, name, "") {
			return model.Dependency{}, false, nil
		}
		return dep, true, nil
	}
	if dep, ok := findExistingDependency(model.DepTypeLinux, name, "",
		model.DepStatusFailed, model.DepStatusCancelled); ok {
		return dep, true, nil
	}
	dep, err := createDependencyRecord(model.DepTypeLinux, name, "", model.DepStatusQueued, "")
	if err != nil {
		return model.Dependency{}, false, err
	}
	*created = append(*created, dep.ID)
	return dep, true, nil
}

// resolvePlaywrightPythonRecord 处理默认 Python 版本的 playwright。
//
// 与系统库不同，已安装的也要重新入队：重装会在 pip 之后接着执行浏览器下载，
// 这正是补下浏览器的途径（老版本升级上来、浏览器原本在 /root/.cache 的用户，pip 记录是已安装，浏览器却早丢了）。
// 只有正在处理中的不动 —— 它跑完 pip 同样会接着下载浏览器。新建记录的 id 同样追加进 created。
func resolvePlaywrightPythonRecord(created *[]uint) (model.Dependency, bool, error) {
	version := playwrightDefaultPythonVersionFunc()
	if _, busy := service.FindExistingPythonDependency(service.PlaywrightPythonPackage, version,
		model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving); busy {
		return model.Dependency{}, false, nil
	}
	if dep, ok := service.FindExistingPythonDependency(service.PlaywrightPythonPackage, version); ok {
		return dep, true, nil
	}
	dep, err := createDependencyRecord(model.DepTypePython, service.PlaywrightPythonPackage, version, model.DepStatusQueued, "")
	if err != nil {
		return model.Dependency{}, false, err
	}
	*created = append(*created, dep.ID)
	return dep, true, nil
}

// countInstalledPlaywrightLinuxPackages 数清单里「登记为已安装、且确实装着」的包。
// 只登记不算（容器重建后 dpkg 里已经没有了），只装着不算（没登记，重建就丢）。
func countInstalledPlaywrightLinuxPackages(packages []string) int {
	if len(packages) == 0 {
		return 0
	}
	var registered []model.Dependency
	database.DB.Select("name").
		Where("type = ? AND status = ? AND name IN ?", model.DepTypeLinux, model.DepStatusInstalled, packages).
		Find(&registered)
	names := make(map[string]struct{}, len(registered))
	for _, dep := range registered {
		names[dep.Name] = struct{}{}
	}

	count := 0
	for _, name := range packages {
		if _, ok := names[name]; !ok {
			continue
		}
		if playwrightDependencyInstalledFunc(model.DepTypeLinux, name, "") {
			count++
		}
	}
	return count
}

// playwrightPythonInstalled 看默认 Python 版本里 playwright 是否登记为已安装且确实装着。
// 先查库、库里是已安装才去跑 pip show，避免每次打开依赖页都起一个子进程。
func playwrightPythonInstalled() bool {
	version := playwrightDefaultPythonVersionFunc()
	if _, ok := service.FindExistingPythonDependency(service.PlaywrightPythonPackage, version, model.DepStatusInstalled); !ok {
		return false
	}
	return playwrightDependencyInstalledFunc(model.DepTypePython, service.PlaywrightPythonPackage, version)
}

// playwrightBrowsersInstalledIn 判断浏览器目录下有没有 Chromium（chromium-* 或 chromium_headless_shell-* 目录）。
// 目录为空或不是绝对路径（例如用户设成了 0，浏览器装进了 playwright 包自己的目录）时查不了，按未就绪处理。
func playwrightBrowsersInstalledIn(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" || !filepath.IsAbs(dir) {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "chromium") {
			return true
		}
	}
	return false
}

// playwrightBrowserDownloadSem 让 Chromium 下载步骤全进程同一时刻只跑一条（容量 1 的信号量）。
//
// 为什么要串行：all 镜像上 POST /deps 装 playwright 会按 Python 版本各建一条记录、各起一个协程，
// pip 装完几乎同时进入下载，而它们下到的是同一个 PLAYWRIGHT_BROWSERS_PATH。Playwright 自己会拿
// <浏览器目录>/__dirlock，抢不到的只重试大约 8 分钟就报 Lock file is already being held；
// 国内慢网络下第一条往往下不完，其余几条全部 failed。连点几次重装也是同样的局面。
// 用 channel 而不是 sync.Mutex：排队中的记录要能被取消、到超时能停下来（见 acquirePlaywrightBrowserDownloadSlot）。
var playwrightBrowserDownloadSem = make(chan struct{}, 1)

// playwrightBrowserDownloadWaitLine 是拿不到下载槽位、开始排队时写进日志的那一行。
const playwrightBrowserDownloadWaitLine = "[Playwright] 另一条记录正在下载 Chromium，排队等待……"

// acquirePlaywrightBrowserDownloadSlot 是浏览器下载步骤的 acquire：先试一次，拿不到就写一行排队提示再等，
// 等待期间 ctx 结束（用户点取消、整体超时）立刻返回 ctx.Err()，不会卡在队里。
func acquirePlaywrightBrowserDownloadSlot(ctx context.Context, waiting func(line string)) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	release := func() { <-playwrightBrowserDownloadSem }
	select {
	case playwrightBrowserDownloadSem <- struct{}{}:
		return release, nil
	default:
	}

	waiting(playwrightBrowserDownloadWaitLine)
	select {
	case playwrightBrowserDownloadSem <- struct{}{}:
		// 槽位空出来与 ctx 结束同时就绪时 select 随机选一边；这里补一次检查，
		// 已经取消 / 超时的记录不再占着槽位去启动下载。
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		return release, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// newPlaywrightBrowserDownloadStep 是挂在 playwright 这条 pip 记录后面的浏览器下载步骤。
func newPlaywrightBrowserDownloadStep(pythonVersion string) depFollowUpStep {
	return depFollowUpStep{
		build: func() (*exec.Cmd, string, error) {
			cmd, browsersPath, err := playwrightBrowserInstallCommandFunc(pythonVersion)
			if err != nil {
				return nil, "", err
			}
			return cmd, service.PlaywrightDownloadStartLine(browsersPath), nil
		},
		doneLine: service.PlaywrightDownloadReadyLine,
		acquire:  acquirePlaywrightBrowserDownloadSlot,
	}
}

// isPlaywrightBrowserDownloadFailure 判断这次失败是不是发生在浏览器下载阶段。
//
// 只看「本次运行」那一段日志（最后一个任务启动行之后）：批量重装、一键安装会把新一轮日志追加在旧日志后面，
// 上一轮失败留下的下载开始行不能算到这一轮头上。本段里有下载开始行、没有就绪行，
// 说明 pip 已经成功、失败发生在下载阶段。
func isPlaywrightBrowserDownloadFailure(logText string) bool {
	segment := logText
	if index := strings.LastIndex(logText, dependencyRunStartMarker); index >= 0 {
		segment = logText[index:]
	}
	return strings.Contains(segment, service.PlaywrightDownloadStartPrefix) &&
		!strings.Contains(segment, service.PlaywrightDownloadReadyLine)
}

// isPlaywrightBrowserDirLockConflict 判断这次下载失败是不是撞上了浏览器目录的安装锁。
//
// Playwright 下载前会拿 <浏览器目录>/__dirlock，抢不到时报 "An active lockfile is found at: …/__dirlock"
// （底层是 "Lock file is already being held"）。面板内的下载已经由 playwrightBrowserDownloadSem 串行，
// 还能撞上说明有面板管不到的下载在跑（系统终端、脚本里手动 playwright install 等）。
// 只看本次运行里最后一个下载开始行之后的输出，pip 阶段的日志不参与判断。
func isPlaywrightBrowserDirLockConflict(logText string) bool {
	segment := logText
	if index := strings.LastIndex(segment, dependencyRunStartMarker); index >= 0 {
		segment = segment[index:]
	}
	if index := strings.LastIndex(segment, service.PlaywrightDownloadStartPrefix); index >= 0 {
		segment = segment[index:]
	}
	lower := strings.ToLower(segment)
	return strings.Contains(lower, "lockfile") ||
		strings.Contains(lower, "__dirlock") ||
		strings.Contains(lower, "lock file is already being held")
}

// buildPlaywrightDownloadLockHint 是撞上安装锁时的提示：这不是网络问题，引去配代理 / 下载镜像只会白折腾。
// 面板内的下载已由 playwrightBrowserDownloadSem 串行，列表里的其它记录不可能同时在下，
// 说成「另一条记录在并行下载」会让人去依赖列表里找一条不存在的记录，所以要把人引向面板外的进程。
func buildPlaywrightDownloadLockHint() string {
	return "[Playwright 浏览器下载失败：浏览器目录正被另一个 playwright install 进程占用。" +
		"面板依赖管理里的下载已经排队串行，占用它的不是列表里的其它记录，" +
		"多半是系统命令行 / ddp shell、面板里运行的脚本或任务执行的 playwright install，或面板重启前没跑完的下载。" +
		"与网络、代理无关，不需要配置代理或下载镜像。等那个进程结束（或手动结束它）后重装本依赖即可]"
}

// buildPlaywrightDownloadFailureHint 要讲清楚两件事：失败的是浏览器下载、不是 pip；以及三条出路。
// 与其它归因一样是单行方括号文案，会被整行写进依赖日志。
//
// v3.3.2 补了「调大超时」这条（issue #146）：Chromium 有 150-300MB，而整条记录的超时默认只有 20 分钟
//（pip 安装 + 排队等待 + 下载共用），倒推下来平均速度要 ≥125-250KB/s 才下得完 ——
// 慢网下最常见的失败形态其实是撞超时，而原文案只讲了代理和下载镜像两条，把最常见的那条漏了。
// 「走官方下载源」也改成「走 Playwright 自己的下载源」：arm64 上面板已经默认走 npmmirror 镜像了。
func buildPlaywrightDownloadFailureHint() string {
	return "[Playwright 浏览器下载失败：pip 包 playwright 已经装好，失败的是随后下载 Chromium 这一步" +
		"（走 Playwright 自己的下载源，与 pip 镜像源无关，约 150-300MB）。" +
		"出路一：到「系统设置 → 代理设置」配置代理后重装本依赖；" +
		"出路二：在「环境变量」页添加 PLAYWRIGHT_DOWNLOAD_HOST，指向可用的 Playwright 下载镜像后重装本依赖；" +
		"出路三：若日志停在下载阶段被判超时，到「系统设置 → 任务运行 → 依赖安装超时(分钟)」调大后重装本依赖]"
}
