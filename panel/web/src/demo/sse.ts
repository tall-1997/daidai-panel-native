import type {
  EventStreamConnection,
  EventStreamEvent,
  EventStreamHandlers,
} from '@/utils/sse'
import { db, findTask } from './db'
import { DEMO_BUILD_MARKER } from './marker'
import { finishDemoTaskRun } from './taskRuns'
import type { DemoTask } from './types'

/**
 * 演示环境的假实时日志流。
 *
 * 由 utils/sse.ts 的 openAuthorizedEventStream() 在编译期常量守卫里动态 import 进来，
 * 返回的对象与真实实现同形状（同样只有一个 close()、同样通过 handlers 回调派发事件），
 * 所以四个调用点一行都不用改：
 *   views/logs/index.vue:268          任务日志详情弹窗（运行中的那条）
 *   views/tasks/components/LogViewer.vue:164
 *   views/deps/index.vue:1555         依赖安装/卸载日志
 *   views/subscriptions/index.vue:758 订阅拉库日志
 *
 * ⚠️ 三个调用点对「一条消息」的理解并不相同，脚本必须分别照顾：
 *   - 任务日志流（logs / LogViewer）拿到的是【裸字节流】，消息之间不会自动补换行
 *     （LogViewer.vue:312-315 明确写了「不把每个 SSE 消息结束当成真实换行」），
 *     所以这条流的每一行必须自带结尾的 \n 或 \r；
 *   - 依赖日志（deps/index.vue:1560）是 `buffer.join("\n") + "\n"`；
 *   - 订阅拉库（subscriptions/index.vue:763）是「一条消息 = 一行」。
 *   后两者的行【不要】自带换行符。
 *
 * ⚠️ 全仓库没有任何 `new EventSource`：实时日志走的是 utils/sse.ts 里手写的
 *    fetch + response.body.getReader()。想让假日志跑起来，mock `window.EventSource`
 *    是无效的，只能走这条路。
 *
 * ⚠️ `POST /api/v1/android-runtime/install` 是【第二套独立的 SSE 解析器】
 *    （deps/index.vue:902-919，按 \n\n 切段、自解 data:、反转义字面 \n），
 *    和 utils/sse.ts 毫无关系。演示环境里那个按钮已经被 403 挡掉，
 *    这里【不要】为它再写一套 mock。
 */

// ---------------------------------------------------------------------------
// 调度器
// ---------------------------------------------------------------------------

/**
 * 结束哨兵。
 *
 * 服务端的三条流内部都是同一套结构：广播器往订阅 channel 里塞一个 "\x00DONE"，
 * 读端看到它才写出 `event: done`
 * （server/handler/subscription.go:89 与 :472、server/handler/deps.go:131 与 :382）。
 * 这里照抄这个形状，而不是让脚本直接以一个 done 事件收尾 —— 剧本本身只描述
 * 「服务端往流里写了什么」，「写完之后发什么 done」是调度器的事，两件事分开。
 */
const DONE_SENTINEL = '\x00DONE'

/**
 * 首个事件之前的等待。
 *
 * 真实实现要等 fetch 的响应头回来才会调 onOpen，不可能同步触发；
 * 这里同样推迟一拍，否则调用方还没拿到 connection 对象就先收到了事件。
 */
const OPEN_DELAY_MS = 120

interface DemoStreamStep {
  /** 与上一步之间的间隔（毫秒） */
  gap: number
  /** 这一步写进流里的内容；等于 DONE_SENTINEL 时表示收尾 */
  data: string
}

interface DemoStreamScript {
  steps: DemoStreamStep[]
  /** 哨兵被消费后 done 事件携带的 data */
  doneData: string
  /** 收尾前对内存数据做的处理（例如把运行中的任务落成一次成功执行） */
  onFinish?: () => void
}

/** 剧本的一行：`[与上一行的间隔, 内容]`，内容可以是回调以拿到时间戳与总时长 */
type StepSpec = [gap: number, text: string | ((stamp: string, totalSeconds: number) => string)]

export function openDemoEventStream(
  url: string,
  handlers: EventStreamHandlers = {}
): EventStreamConnection {
  // 这行 debug 顺带把哨兵字符串钉进产物：本文件是 demo 层的一个【独立入口】
  // （由 utils/sse.ts 动态 import），不引用一次哨兵，CI 那条「发布版不含 mock 代码」
  // 的断言就管不到它。详见 demo/marker.ts。
  // 用 console.debug 而不是 info：Chrome 默认日志级别不显示 Verbose，不会刷屏。
  console.debug(`[demo] 假实时日志流 ${DEMO_BUILD_MARKER}:`, url)

  const script = planDemoStream(url)
  let timer: ReturnType<typeof setTimeout> | null = null
  let closed = false
  let index = 0

  const emit = (event: EventStreamEvent) => {
    // 与 utils/sse.ts 的 dispatchEventSegment 收尾处完全一致的派发顺序。
    //
    // 这里刻意【不】把内容拼成 SSE 文本再回头解析一遍：那条路上每个 data 行都要
    // 逐行转义，稍有不慎就会把进度条里的裸 \r 洗掉（sse.ts:143-148 专门为它留了例外）。
    // 直接派发就没有任何裁剪环节，\r 一定原样送到 TerminalLineBuffer 手里。
    handlers.onEvent?.(event)
    if (event.event === 'message') {
      handlers.onMessage?.(event.data, event)
    }
  }

  const scheduleNext = () => {
    const next = script.steps[index]
    if (!next || closed) return
    timer = setTimeout(runNext, next.gap)
  }

  const runNext = () => {
    timer = null
    if (closed) return

    const current = script.steps[index]
    if (!current) return
    index += 1

    if (current.data === DONE_SENTINEL) {
      script.onFinish?.()
      emit({ event: 'done', data: script.doneData })
      return
    }

    emit({ event: 'message', data: current.data })
    scheduleNext()
  }

  timer = setTimeout(() => {
    timer = null
    if (closed) return
    handlers.onOpen?.()
    scheduleNext()
  }, OPEN_DELAY_MS)

  return {
    close() {
      closed = true
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
    },
  }
}

/**
 * 把剧本折成可调度的步骤序列。
 *
 * 总时长必须落在 3~5 秒：
 *   - 太快（秒关）会踩到 LogViewer.vue:185-198 的空重连保护——
 *     那条路径把「连不上又没有新数据」当成风暴来处理，会开始退避重连；
 *   - 太慢则访客在演示站上会以为卡住了。
 * 各条流的实际总时长见下面每个 plan* 函数末尾的注释。
 */
function buildScript(
  specs: StepSpec[],
  doneGap: number,
  doneData: string,
  onFinish?: () => void
): DemoStreamScript {
  const startedAt = Date.now()
  // 先把总时长算出来：正文最后一行要打印「耗时 Xs」，那必须是整条流的时长，
  // 只算到它自己那一步的话，日志里的耗时会比访客实际看到的短一截。
  const totalMs = specs.reduce((sum, spec) => sum + spec[0], 0) + doneGap
  const totalSeconds = Math.round(totalMs / 100) / 10

  const steps: DemoStreamStep[] = []
  let at = 0
  for (const [gap, text] of specs) {
    at += gap
    steps.push({
      gap,
      data: typeof text === 'function' ? text(formatStamp(startedAt + at), totalSeconds) : text,
    })
  }
  steps.push({ gap: doneGap, data: DONE_SENTINEL })

  return { steps, doneData, onFinish }
}

/** `YYYY-MM-DD HH:MM:SS`，与 db.ts 里 buildLogContent 的时间戳格式保持一致 */
function formatStamp(ms: number): string {
  const at = new Date(ms)
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} `
    + `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`
}

// ---------------------------------------------------------------------------
// 路由
// ---------------------------------------------------------------------------

const TASK_LOG_PATTERN = /\/logs\/(\d+)\/stream$/
const DEPS_LOG_PATTERN = /\/deps\/(\d+)\/log-stream$/
const SUB_PULL_PATTERN = /\/subscriptions\/(\d+)\/pull-stream$/

function planDemoStream(url: string): DemoStreamScript {
  // 只看路径部分。订阅那条流的前缀来自 `import.meta.env.VITE_API_BASE || '/api/v1'`
  // （subscriptions/index.vue:756，全站唯一读 VITE_API_BASE 的地方）。
  // ⚠️ 演示构建【不要】注入 VITE_API_BASE：一旦注入，那一页的 SSE 路径就会和
  //    其余三条（写死 /api/v1）分叉，这里的正则也要跟着分叉。保持不注入，前缀恒为 /api/v1。
  const path = (url.split('?')[0] ?? '').replace(/^https?:\/\/[^/]+/, '')

  const taskLog = TASK_LOG_PATTERN.exec(path)
  if (taskLog) return planTaskLogStream(Number(taskLog[1]))

  const depsLog = DEPS_LOG_PATTERN.exec(path)
  if (depsLog) return planDepsLogStream(Number(depsLog[1]))

  const subPull = SUB_PULL_PATTERN.exec(path)
  if (subPull) return planSubscriptionPullStream(Number(subPull[1]))

  return planUnknownStream(path)
}

// ---------------------------------------------------------------------------
// 任务实时日志：/api/v1/logs/:taskId/stream
// ---------------------------------------------------------------------------

type DemoRunner = 'python' | 'node' | 'shell'

function runnerOf(command: string): DemoRunner {
  const head = command.trim().toLowerCase()
  if (head.startsWith('python')) return 'python'
  if (head.startsWith('node')) return 'node'
  return 'shell'
}

/**
 * 正文按解释器分三套。
 *
 * 一个 `bash ops/backup_data.sh` 的任务滚出 pip 风格的日志会当场穿帮，
 * 而演示站的 14 个任务恰好覆盖 bash / python3 / node 三种命令。
 *
 * 每套里都有一行【带裸 \r 的进度条】，而且刻意拆成四条独立消息按时间吐出来：
 * TerminalLineBuffer 会把裸 \r 当成「光标回到行首、等后续字符覆盖」
 * （utils/ansi.ts:330-331 与 :393-402），于是同一行数字会在原地跳动——
 * 这是整个演示里最能体现「实时」的一幕。
 * 反过来说：这些 \r 一个都不能被 trim 掉，否则进度条会摊成四行普通日志。
 */
function taskBodySpecs(runner: DemoRunner, task: DemoTask | undefined): StepSpec[] {
  if (runner === 'python') {
    const pythonVersion = task?.python_version || '3.12'
    return [
      [240, (at) => `[${at}] [INFO] 解释器 /opt/venv/py${pythonVersion.replace('.', '')}/bin/python (${pythonVersion})\n`],
      [220, (at) => `[${at}] [INFO] 环境变量已注入（19 项）\n`],
      [280, (at) => `[${at}] [INFO] 连接数据源 ops-readonly ... ok\n`],
      [300, (at) => `[${at}] [INFO] 待导出 3 张表，共 20000 行\n`],
      [260, '拉取记录  18% |███                 | 3600/20000\r'],
      [240, '拉取记录  46% |█████████           | 9200/20000\r'],
      [240, '拉取记录  74% |██████████████      | 14800/20000\r'],
      [240, '拉取记录 100% |████████████████████| 20000/20000\n'],
      [280, (at) => `[${at}] [INFO] 已写出 out/offline-report.xlsx（2.4 MB）\n`],
      [300, (at) => `[${at}] [INFO] 上传到对象存储 ... ok\n`],
    ]
  }

  if (runner === 'node') {
    return [
      [240, (at) => `[${at}] [INFO] node v24.21.0\n`],
      [220, (at) => `[${at}] [INFO] 环境变量已注入（19 项）\n`],
      [280, (at) => `[${at}] [INFO] 开始下载 GeoLite2-City.mmdb\n`],
      [300, (at) => `[${at}] [INFO] GET https://cdn.example.com/GeoLite2-City.mmdb 200\n`],
      [260, '下载中   8.2 MB / 62.4 MB [###                 ]\r'],
      [240, '下载中  24.6 MB / 62.4 MB [########            ]\r'],
      [240, '下载中  47.9 MB / 62.4 MB [###############     ]\r'],
      [240, '下载中  62.4 MB / 62.4 MB [####################]\n'],
      [280, (at) => `[${at}] [INFO] sha256 校验通过\n`],
      [300, (at) => `[${at}] [INFO] 已替换旧库，旧文件转存为 .bak\n`],
    ]
  }

  return [
    [200, '+ set -euo pipefail\n'],
    [180, '+ source ops/lib/common.sh\n'],
    [240, (at) => `[${at}] [INFO] 环境变量已注入（19 项）\n`],
    [260, (at) => `[${at}] [INFO] 可用磁盘 42.6 GB，开始处理\n`],
    [240, '处理进度  12% [##                  ]\r'],
    [230, '处理进度  37% [#######             ]\r'],
    [230, '处理进度  61% [############        ]\r'],
    [230, '处理进度  88% [#################   ]\r'],
    [230, '处理进度 100% [####################]\n'],
    [260, (at) => `[${at}] [INFO] 共处理 1284 项，跳过 0 项\n`],
  ]
}

/**
 * 总时长：shell 约 3.4 秒（120 打开 + 460 抬头 + 2300 正文 + 220 结束行 + 320 收尾），
 * python / node 约 3.7 秒（正文 2600）。三条都稳稳落在 C3 要求的 3~5 秒区间内。
 */
function planTaskLogStream(taskId: number): DemoStreamScript {
  const task = findTask(taskId)
  const name = task?.name ?? `任务 #${taskId}`
  const command = task?.command ?? 'bash run.sh'

  const script = buildScript(
    [
      [120, (at) => `[${at}] ## 开始执行 ${name}\n`],
      [160, (at) => `[${at}] ## 命令：${command}\n`],
      [180, (at) => `[${at}] ## 工作目录：/opt/daidai/scripts\n`],
      ...taskBodySpecs(runnerOf(command), task),
      [220, (at, total) => `\n[${at}] ## 执行结束，退出码 0，耗时 ${total.toFixed(1)}s\n`],
    ],
    320,
    // 服务端在这条流上只会发 finished / reconnect（handler/log.go:101/138/150/161）。
    // 【绝对不能】用 reconnect：LogViewer.vue:185 收到它会进入退避重连，
    // 于是演示站会每隔几秒重放一次同样的日志，永远停不下来。
    'finished'
    // 第四个参数 onFinish 在下面补：收尾要把刚滚过的正文一起带上，得先拿到 steps。
  )

  // done 之后 LogViewer 会立刻回查 latest-log 并整体替换渲染内容，
  // 把这份正文留在日志上，访客看到的就还是刚才那一屏，而不是另一套措辞的版本。
  const transcript = script.steps
    .filter((step) => step.data !== DONE_SENTINEL)
    .map((step) => step.data)
    .join('')
  script.onFinish = () => {
    finishDemoTaskRun(taskId, transcript)
  }

  return script
}

// ---------------------------------------------------------------------------
// 依赖安装日志：/api/v1/deps/:id/log-stream
// ---------------------------------------------------------------------------

/**
 * 总时长约 3.1 秒。
 *
 * 演示环境里这条流其实走不到：fixture 没有 installing / removing 状态的依赖，
 * 而 `POST /deps`、`PUT /deps/:id/reinstall` 都被 403 挡掉了，
 * deps/index.vue:1537 会直接判定 logDone=true 走查询分支。
 * 保留它是为了让「fixture 以后加了一条安装中的依赖」不至于掉进 planUnknownStream。
 *
 * ⚠️ 这条流的行【不带】换行符：deps/index.vue:1560 是 `buffer.join("\n") + "\n"`。
 */
function planDepsLogStream(depId: number): DemoStreamScript {
  const dep = db().deps.find((row) => row.id === depId)
  const name = dep?.name ?? `dependency-${depId}`
  const type = dep?.type ?? 'python'

  if (type === 'nodejs') {
    return buildScript(
      [
        [240, `$ npm install ${name} --registry=https://registry.npmmirror.com`],
        [300, `npm http fetch GET 200 https://registry.npmmirror.com/${name} 214ms`],
        [320, 'added 1 package, and audited 132 packages in 2s'],
        [340, 'found 0 vulnerabilities'],
        [300, `[INFO] ${name} 安装完成`],
        [320, '[INFO] 已刷新依赖清单'],
        [340, '[INFO] 本次操作未修改 package-lock.json'],
        [300, '[INFO] 完成'],
      ],
      560,
      'installed'
    )
  }

  if (type === 'linux') {
    return buildScript(
      [
        [240, `$ apt-get install -y ${name}`],
        [300, 'Reading package lists... Done'],
        [320, 'Building dependency tree... Done'],
        [340, `The following NEW packages will be installed: ${name}`],
        [300, 'Need to get 1,284 kB of archives.'],
        [320, `Selecting previously unselected package ${name}.`],
        [340, `Setting up ${name} ...`],
        [300, '[INFO] 完成'],
      ],
      560,
      'installed'
    )
  }

  return buildScript(
    [
      [240, `$ pip install ${name}`],
      [300, 'Looking in indexes: https://pypi.tuna.tsinghua.edu.cn/simple'],
      [320, `Collecting ${name}`],
      [340, `  Downloading ${name}-2.32.3-py3-none-any.whl (64 kB)`],
      [300, '     ---------------------------------------- 64.9/64.9 kB 3.1 MB/s'],
      [320, `Installing collected packages: ${name}`],
      [340, `Successfully installed ${name}-2.32.3`],
      [300, '[INFO] 依赖安装完成'],
    ],
    560,
    // 服务端这里发的是依赖的最终状态（handler/deps.go:385）。
    // 只要不是 timeout / reconnect，deps/index.vue 就不会挂「日志流已断开」的提示。
    'installed'
  )
}

// ---------------------------------------------------------------------------
// 订阅拉库日志：{VITE_API_BASE || /api/v1}/subscriptions/:id/pull-stream
// ---------------------------------------------------------------------------

/**
 * 总时长：git 仓库约 3.3 秒（配了 hook 脚本的多一行，约 3.6 秒），单文件约 3.1 秒。
 *
 * ⚠️ 这条流的行【不带】换行符：subscriptions/index.vue:763 是
 *    `pullLogLines.value.push(...buffer)`，一条消息就是一行。
 *    git 的进度同理拆成独立的行，而不是用 \r 覆盖 —— 那一页把每行渲染成一个 div，
 *    裸 \r 落进去只是个看不见的控制字符，没有覆盖效果。
 *
 * ⚠️ done 的 data 必须是 finished / not_running / closed 三者之一
 *    （subscriptions/index.vue:818-826）。发别的值会走到 `disconnected` 分支，
 *    弹窗状态机停在「连接中断」，永远等不到成功/失败的判定。
 *    这里用 finished，对应服务端消费到 \x00DONE 的那条路径
 *    （server/handler/subscription.go:472-473）。
 */
function planSubscriptionPullStream(subId: number): DemoStreamScript {
  const sub = db().subscriptions.find((row) => row.id === subId)
  const name = sub?.name ?? `订阅 #${subId}`
  const remote = sub?.url ?? 'https://github.com/example/ops-scripts.git'
  const branch = sub?.branch || 'main'
  const saveDir = sub?.save_dir || 'subscriptions'
  const hook = (sub?.hook_script ?? '').trim()

  // 单文件订阅根本不跑 git。fixture 里的 3 号订阅就是这一类，
  // 列表上明晃晃写着「单文件」，日志里却滚出 git fetch 的话当场穿帮。
  if (sub?.type === 'single-file') {
    const fileName = remote.split('/').pop() || 'script.py'
    return buildScript(
      [
        [200, `开始拉取订阅：${name}`],
        [340, `下载单文件：${remote}`],
        [380, 'HTTP/1.1 200 OK, content-length: 4218'],
        [400, `写入 ${saveDir}/${fileName}`],
        [360, '内容与上一次不同，已覆盖'],
        [380, '同步任务：新增 0 个，更新 1 个，删除 0 个'],
        [360, '拉取完成，更新 1 个文件'],
      ],
      560,
      'finished'
    )
  }

  const specs: StepSpec[] = [
    [160, `开始拉取订阅：${name}`],
    [240, `远端：${remote}`],
    [260, `git fetch --depth=1 origin ${branch}`],
    [300, 'remote: Enumerating objects: 128, done.'],
    [260, '接收对象: 34% (44/128)'],
    [240, '接收对象: 71% (91/128)'],
    [240, '接收对象: 100% (128/128), 412.36 KiB | 6.21 MiB/s, done.'],
    [300, `检出到 ${saveDir}`],
    [280, '白名单命中 6 个文件，黑名单排除 1 个'],
  ]
  if (hook) {
    specs.push([320, `执行 hook：${hook}`])
  }
  specs.push(
    [300, '同步任务：新增 0 个，更新 3 个，删除 0 个'],
    [260, '拉取完成，更新 3 个文件，新增任务 0 个']
  )

  return buildScript(specs, 360, 'finished')
}

// ---------------------------------------------------------------------------
// 兜底
// ---------------------------------------------------------------------------

/**
 * 没铺剧本的流。
 *
 * 同样要吐满 3 秒再收尾，理由和其它三条一样（秒关会触发 LogViewer 的空重连保护）。
 * done 用 finished：这个值对三个调用点都是安全的终态
 *   —— LogViewer 只把 reconnect 当重连信号、deps 只把 timeout 与 reconnect 当断流、
 *   订阅把 finished 当正常结束。
 *
 * 行尾统一带 `\n`：这里不知道自己落在哪条流上，而三种约定里只有任务日志流
 * 「不带换行就会把三行糊成一行」，另外两条顶多多出一个空行。取伤害较小的那一侧。
 */
function planUnknownStream(path: string): DemoStreamScript {
  return buildScript(
    [
      [200, '[demo] 演示环境没有为这条实时日志流铺设剧本\n'],
      [1200, `[demo] 请求路径：${path}\n`],
      [1200, '[demo] 连接会在稍后正常结束，不会触发重连\n'],
    ],
    600,
    'finished'
  )
}
