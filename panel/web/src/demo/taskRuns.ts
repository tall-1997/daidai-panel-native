import { appendTaskRunLog, db, isTaskActiveStatus, nowIso, settleTaskRunStatus } from './db'
import type { DemoTask, DemoTaskLog } from './types'
import { LOG_STATUS_SUCCESS, RUN_STATUS_SUCCESS, TASK_STATUS_DISABLED, TASK_STATUS_RUNNING } from './types'

/**
 * 「手动点了运行」这件事在演示环境里的生命周期。
 *
 * 为什么需要它：`handleRun()`（views/tasks/index.vue:429-433）拿到 200 之后会立刻
 * `task.status = 2` 并打开实时日志弹窗。如果 mock 的 run 端点像之前那样直接落一条
 * 【已完成】的日志，那个弹窗就永远等不到任何数据，演示里最有观赏性的一幕直接没了。
 *
 * 所以这里把一次手动运行拆成两段：
 *   1. startDemoTaskRun()  —— 落一条运行中的日志、把任务置为运行中；
 *   2. finishDemoTaskRun() —— 收尾成一次成功执行。
 *
 * 收尾有两条触发路径，谁先到算谁（幂等）：
 *   - 假日志流（demo/sse.ts）吐完最后一行时主动收尾，这是正常路径；
 *   - 兜底定时器，防止访客压根没打开日志弹窗时留下一条永远「运行中」的记录
 *     —— 那会让仪表盘的「运行中的任务」越点越多，且再也降不回去。
 */

/**
 * 注册表只记「这次运行的日志 + 兜底定时器」。
 *
 * 「跑完回到禁用还是启用」不在这里记：那就是任务的开关位，放在任务自己的 pending_disable 上（types.ts），
 * toTaskDict 算 enabled、运行中点启用 / 禁用、停止、跑完收尾都读写这一个字段（issue #133）。
 * 以前这里存一份运行前的 status（previousStatus），有三处对不上：运行中点「启用」「禁用」改不到它；
 * fixture 里那两个运行中任务根本没有注册表条目；db.ts 要读它得反过来 import 本文件（本文件已经 import 了 db.ts）。
 */
interface DemoTaskRun {
  log: DemoTaskLog
  timer: ReturnType<typeof setTimeout>
}

/**
 * 兜底收尾时间。
 *
 * 必须明显长于假日志流的总时长（约 3.4 秒），否则定时器会抢在流前面把日志收掉，
 * 访客会看到「日志还在滚，任务却已经显示成功了」。
 */
const AUTO_FINISH_MS = 9000

const runs = new Map<number, DemoTaskRun>()

/** 手动运行：落一条运行中的日志，并把任务置为运行中 */
export function startDemoTaskRun(task: DemoTask): DemoTaskLog {
  // 同一个任务连点两次「运行」：先把上一次收尾掉，避免留下两条运行中的日志。
  finishDemoTaskRun(task.id)

  // 复刻服务端 SchedulerV2.RunNow（issue #133）：禁用中的任务被手动运行，入队前先打「待禁用」标记。
  // 运行期间开关位据此仍是关（菜单给出「启用」），跑完 / 被停止据此落回禁用；运行中点「启用」撤掉它就落回启用。
  // 只在「不在跑」时重算：上面的收尾没找到运行中的日志时任务可能还停在运行中，那时保留已有标记（服务端「已有标记不重打」）。
  // 按 status 重算而不是只在禁用时置 true：顺手清掉万一残留在启用任务上的旧标记，免得这次跑完被错当成禁用。
  if (!isTaskActiveStatus(task.status)) {
    task.pending_disable = task.status === TASK_STATUS_DISABLED
  }

  // duration 传 0 ⇒ started_at 就是此刻，且 duration / ended_at 都是 null（运行中的语义）
  const log = appendTaskRunLog(task, 'running', 0)

  task.status = TASK_STATUS_RUNNING
  task.pid = 20000 + Math.floor(Math.random() * 20000)
  task.updated_at = nowIso()

  runs.set(task.id, {
    log,
    timer: setTimeout(() => {
      finishDemoTaskRun(task.id)
    }, AUTO_FINISH_MS),
  })

  return log
}

/**
 * 把「运行中」收尾成一次成功执行。
 *
 * 幂等：找不到运行中的日志就什么都不做，所以假日志流与兜底定时器谁先到都安全。
 *
 * @param transcript 假日志流刚刚滚过的正文。留在日志上是为了避免「画面突变」——
 *   LogViewer 收到 done 之后会立刻回查 latest-log 并整体替换渲染内容
 *   （LogViewer.vue:239-241 的 resetLogOutput + appendLogChunk），
 *   不留正文的话访客会看到刚看完的日志被换成另一套措辞的版本。
 */
export function finishDemoTaskRun(taskId: number, transcript?: string): void {
  const run = runs.get(taskId)
  if (run) {
    clearTimeout(run.timer)
    runs.delete(taskId)
  }

  const current = db()
  const task = current.tasks.find((row) => row.id === taskId)

  // 只认「当前这份内存数据里还在」的日志对象：横幅上的「重置演示数据」会整体换掉
  // state，此刻注册表里的旧引用必须原地作废，不能拿去改一条已经不存在的记录。
  // 没有注册表条目时（例如访客直接打开 fixture 里那两个运行中任务的实时日志），
  // 就按任务去找那条运行中的记录。
  const log = run && current.logs.includes(run.log)
    ? run.log
    : current.logs.find((row) => row.task_id === taskId && row.kind === 'running')

  if (!log) return

  // 真实耗时按 started_at 算。上限收到任务超时的九成：fixture 里的运行中日志是
  // 「已经跑了一会儿」的状态，访客把标签页挂在后台几十分钟再来点开的话，
  // 直接用真实差值会算出一个远超该任务超时时间的「成功」，一眼假。
  const elapsedSeconds = (Date.now() - new Date(log.started_at).getTime()) / 1000
  const cap = task && task.timeout > 0 ? task.timeout * 0.9 : Number.POSITIVE_INFINITY
  const duration = Math.round(Math.min(Math.max(elapsedSeconds, 0.1), cap) * 10) / 10

  log.kind = 'ok'
  log.status = LOG_STATUS_SUCCESS
  log.duration = duration
  log.ended_at = nowIso()
  if (transcript) log.content = transcript

  if (task) {
    // 按开关位落回（issue #133）：有待禁用标记 → 禁用，没有 → 启用；标记随这次结算一并清掉。
    // 口径见 db.ts 的 settleTaskRunStatus（与停止共用；服务端这两条路径用的也是同一个 ResolveTaskInactiveStatus）。
    settleTaskRunStatus(task)
    task.pid = null
    task.last_run_status = RUN_STATUS_SUCCESS
    task.last_running_time = duration
    task.updated_at = nowIso()
  }
}

/**
 * 「停止」按钮走的路径：只撤掉兜底定时器。
 *
 * 日志与任务状态由 adapter 的停止（`PUT /tasks/:id/stop` 与批量停止，见 stopDemoTask）自己改成「已终止」，
 * 这里如果不撤定时器，9 秒后它会把那条已经终止的记录又翻成成功。
 */
export function cancelDemoTaskRun(taskId: number): void {
  const run = runs.get(taskId)
  if (!run) return
  clearTimeout(run.timer)
  runs.delete(taskId)
}
