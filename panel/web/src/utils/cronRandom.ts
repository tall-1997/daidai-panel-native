/**
 * 随机定时规则生成器（纯前端「生成器」语义，零依赖、可单测）。
 *
 * 【它解决什么】
 * 用户想让脚本每天在几个不固定的时间点跑，避免所有人都卡在整点一起打接口。
 * 这里点一次生成一组**固定**的 cron 表达式写回输入框，用户还能继续手改；
 * 后端调度、列表页「下次执行」都按普通 cron 处理，不需要任何后端改动。
 * 刻意不做「运行时随机」——那样列表页显示的下次执行时间和调度器实际触发对不上。
 *
 * 【为什么输出 6 段】
 * 后端 `server/pkg/cron/cron.go` 的 parserForParts 按段数分派，6 段 = 秒 分 时 日 月 周，
 * 调度器也开了 WithSeconds()，所以 6 段原生可用；而且只有 6 段才能表达「随机到秒」。
 * ⚠️ 6 段的第一段是「秒」：`* * * * * *` 是**每秒**不是每分钟。
 * 把 5 段补成 6 段时永远是**在开头补 `0`**，绝不能在末尾补 `*`。
 */

export type RandomCronOptions = {
  /** 每天几个时间点，1..6；超过可选小时数时会被钳制 */
  count: number
  /** 时间窗起始小时，默认 6 */
  startHour: number
  /** 时间窗结束小时（含），默认 23 */
  endHour: number
  /** 是否随机秒，默认 true；关掉时秒固定为 0 */
  randomizeSecond: boolean
  /** split=拆成 N 条（默认，每个点分秒各自独立）；merged=合并成一条（N 个小时共享同一分秒） */
  mode: 'split' | 'merged'
}

/** [min, max] 闭区间随机整数 */
function randInt(min: number, max: number): number {
  return min + Math.floor(Math.random() * (max - min + 1))
}

/**
 * 生成随机定时规则。
 *
 * 边界行为（前端无测试框架，契约写在这里）：
 * - `endHour < startHour`（跨零点）-> 返回 `[]`，**不会**偷偷展开成两段表达式。
 *   展开出来的规则和用户在界面上看到的「时间窗」对不上，排障时极其费解，
 *   所以这种入参交给 UI 拦住并给提示。
 * - `count` 超出 1..6 或超过窗口内可选小时数 -> 钳制，不报错。
 * - `mode: 'merged'` -> 永远只返回 1 条，形如 `10 50 9,21 * * *`。
 * - `mode: 'split'`  -> 返回 `count` 条，形如 `10 50 9 * * *`，按小时升序。
 */
export function generateRandomCron(opts: RandomCronOptions): string[] {
  const startHour = Math.min(23, Math.max(0, Math.floor(opts.startHour) || 0))
  const endHour = Math.min(23, Math.max(0, Math.floor(opts.endHour) || 0))

  // 跨零点直接判空，理由见上方注释
  if (endHour < startHour) {
    return []
  }

  const windowLength = endHour - startHour + 1
  // 时间点数不能多于窗口内的小时数，否则「无放回」抽样根本抽不出来
  const count = Math.min(Math.max(1, Math.floor(opts.count) || 1), 6, windowLength)

  // 【minGap 为什么不能省】
  // 朴素的独立抽样很容易抽出 9 点和 10 点这种紧挨着的时间点。两个点挤在一起，
  // 和只跑一次没有本质区别，用户会觉得「随机了个寂寞」，打散的意义直接没了。
  // 取「窗口长度 / 点数 / 2」= 平均间隔的一半，是个折中：既保证点与点明显分开，
  // 又留下足够随机余量（不至于退化成等距的固定排班）。
  const minGap = Math.max(1, Math.floor(windowLength / count / 2))

  // 【怎么在保证最小间隔的前提下仍然均匀随机】
  // 「抽了不合格就重抽」在窗口偏紧时可能长时间抽不中，不能用。
  // 这里做等价变换：先从一个缩短过的区间 [0, reducedSize) 里无放回抽 count 个数并升序，
  // 再给第 i 个数补回 i*(minGap-1) 的位移；位移后相邻两数之差自然 ≥ minGap。
  // reducedSize ≥ count 恒成立：minGap ≤ windowLength/(2*count)，
  // 于是 (count-1)*(minGap-1) < windowLength/2，剩下的一半一定装得下 count 个数。
  const minGapForCount = minGap - 1
  const reducedSize = Math.max(count, windowLength - (count - 1) * minGapForCount)

  const candidates: number[] = []
  for (let i = 0; i < reducedSize; i++) {
    candidates.push(i)
  }
  // 部分 Fisher-Yates：只把前 count 个位置洗出来就够了，不必洗完整个数组
  for (let i = 0; i < count; i++) {
    const j = randInt(i, candidates.length - 1)
    const swap = candidates[i]!
    candidates[i] = candidates[j]!
    candidates[j] = swap
  }

  const hours = candidates
    .slice(0, count)
    .sort((a, b) => a - b)
    .map((value, index) => startHour + value + index * minGapForCount)

  if (opts.mode === 'merged') {
    // 合并形态：N 个小时共享同一分/秒，形如 `10 50 9,21 * * *`
    const minute = randInt(0, 59)
    const second = opts.randomizeSecond ? randInt(0, 59) : 0
    return [`${second} ${minute} ${hours.join(',')} * * *`]
  }

  // 拆分形态（默认）：每个时间点的分/秒各自独立随机，分布比合并形态更散
  return hours.map(hour => {
    const minute = randInt(0, 59)
    const second = opts.randomizeSecond ? randInt(0, 59) : 0
    return `${second} ${minute} ${hour} * * *`
  })
}

/** 两位补零，只为让预览里的时间点对齐好读 */
function pad2(value: string): string {
  return value.length === 1 ? `0${value}` : value
}

/**
 * 「每天 HH:MM」后面那个可选的 `:SS` 秒位。
 *
 * ⚠️ 与后端 `server/pkg/cron/cron.go` 的 dailySecondSuffix【同口径，改一边必须改另一边】：
 * 只有「秒是固定数字且不为 0」时才补秒位。
 *
 * 零秒刻意不补 —— 后端那边这么定是为了让 `0 30 9 * * *` 与 5 段的 `30 9 * * *` 描述一致，
 * 也不让出厂那批预设的描述凭空多出 `:00`。这边必须跟着不补：否则用户在随机弹层里
 * 关掉「随机到秒」生成 `0 30 9,21 * * *`，弹层预览写「每天 09:30:00」、点应用后规则条
 * （走后端 describe()）写「每天 09:30」，同一屏两个说法 —— 正是补秒位这件事本来要消灭的现象。
 */
function dailySecondSuffix(second: string): string {
  // 非纯数字（`*/10`、`0-30` 等）不补，对齐后端的 isNumeric 判定；
  // 那些形态在后端由 describeSimpleStep 或兜底分支接管，不走秒位拼接
  if (!/^\d+$/.test(second)) {
    return ''
  }
  // 「0」「00」都算零秒，等价于后端的 strings.Trim(second, "0") == ""
  if (Number(second) === 0) {
    return ''
  }
  return `:${pad2(second)}`
}

/**
 * 把本文件生成的表达式翻译成人话，供弹层实时预览用。
 *
 * 刻意在本地算而不是调后端 `POST /tasks/cron/parse`：预览会随着「点数 / 时间窗 / 随机到秒」
 * 每次改动重新生成，走接口等于每拨一下就打 N 个请求。
 * 这里只认本文件自己产出的 `秒 分 时 * * *` 形态，认不出来就原样返回表达式，不猜。
 */
export function describeRandomCron(expression: string): string {
  const parts = expression.trim().split(/\s+/)
  if (parts.length !== 6) {
    return expression
  }
  const second = parts[0]!
  const minute = parts[1]!
  const hour = parts[2]!
  // 小时段可能是 `9` 也可能是合并形态的 `9,21`
  const hours = hour.split(',').filter(Boolean)
  if (hours.length === 0) {
    return expression
  }
  // split（单个小时）和 merged（`9,21`）走的是同一条拼接：秒位由 dailySecondSuffix 统一决定，
  // 两种形态因此不可能出现「一个补秒一个不补」的分叉
  const secondSuffix = dailySecondSuffix(second)
  return `每天 ${hours.map(item => `${pad2(item)}:${pad2(minute)}${secondSuffix}`).join('、')}`
}
