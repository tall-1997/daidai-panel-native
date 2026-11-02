/**
 * 定时规则可视化编辑器的「模型 ↔ cron 表达式」互转（纯前端、零依赖）。
 *
 * 【它解决什么】
 * 「早上 9 点到晚上 10 点、每 10 分钟一次、只在工作日」——后端本来就能存能校验能调度
 * （`*\/10 9-22 * * 1-5`），缺的只是一个不用手打表达式的编辑入口。
 * 这里负责把界面上的分/时/星期选择拼成表达式，以及反过来把已有表达式还原成界面状态。
 *
 * 【和后端 describe() 的分工】
 * 规则行下方那条绿色徽标的人话描述**一直由后端 `POST /tasks/cron/parse` 下发**，
 * 本文件的 `describeCronVisual` 只服务于弹窗内的实时预览（每拨一下控件就要重算，
 * 走接口等于每拨一下打一个请求）。两边措辞可能略有出入，这是刻意的：
 * 弹窗预览描述的是「你正在配的模型」，规则行描述的是「已经落到表达式上的东西」。
 * 不要为了对齐而在前端再实现一份完整描述器去和后端打架。
 *
 * 【段数】
 * `withSeconds` 决定 6 段（秒 分 时 日 月 周）还是 5 段（分 时 日 月 周）。
 * ⚠️ 5 段补 6 段永远是**在开头补 `0`**，末尾补 `*` 会把「每分钟」变成「每秒」。
 * 仓库里 CronInput.vue、cronRandom.ts 都在反复强调这条，说明历史上踩过。
 *
 * ────────────── build / parse 契约（仓库无前端测试框架，契约写在这里） ──────────────
 *
 * 生成方向（buildCronExpression）：
 * - 「工作日」固定输出 `1-5`、「周末」固定输出 `0,6`，与后端出厂预设逐字节一致，
 *   这样反解回来能命中同一个快捷项，用户也不会觉得表达式被莫名改写。
 * - 等价形态统一：`*\/1` -> `*`（分钟步进、小时步进都适用）；升序去重后**整段是一条连续区间
 *   且不止一个值** -> `a-b`（所以 `1,2,3,4,5` -> `1-5`）；小时区间 `0-23` -> `*`；
 *   单值区间 `9-9` -> `9`；星期全选 7 天 -> `*`；每月全选 31 天 -> `*`。
 * - 「每 N 小时」输出小时段 `*\/N`（`0 0 *\/2 * * *` = 0、2、4…22 点整各一次），与后端出厂预设
 *   「每2小时」「每6小时」逐字节一致。注意它是**按 0 点起算的整点**，不是「从现在起每隔 N 小时」，
 *   而且步进每天重新起算：`*\/5` 当天最后一次是 20 点，到次日 0 点只隔 4 小时。
 * - 跨零点（`hourEnd < hourStart`，如 22 点到次日 6 点）单条 cron 表达不了，**返回空串**，
 *   由 UI 拦住并提示「请拆成两条规则」。照 RandomCronDialog 的既有做法，
 *   **不偷偷展开成两条**——展开出来的规则和用户在界面上看到的时段对不上，排障时极其费解。
 * - 日（dom）与星期（dow）在 cron 里同时被限定时取**并集**不是交集，所以
 *   `frequency` 把「每月几号」和「星期几」互斥掉：`month` 只写 dom，其余形态 dom 恒为 `*`。
 *
 * 反解方向（parseCronExpression）：是生成方向的严格逆——对本文件产出的任何表达式 e，
 * 都满足 `buildCronExpression(parseCronExpression(e)!) === e`。
 * （反过来 `parse(build(m))` 不保证等于 m：`小时列表 [9,10,11]` 会归一到 `小时区间 9-11`，
 * 这正是「等价形态统一」想要的结果。非规范写法如 `6,0`、`*\/1` 同样会被归一——小时段的
 * `*\/1` 会直接反解成「全天」，因为它生成出来本就是 `*`。）
 * 认不出的一律 `return null`，**绝不猜**：
 * - `L` / `W` / `#` / `?` 等高级写法、`JAN` / `MON` 英文别名；
 * - 混合形态 `9-11,21`；
 * - 带步进的区间 `9-22/2`：它是「区间内每 N 小时」，和 `*\/N` 不是同一个语义，模型里没有这一档，
 *   **不要顺手把它当成 step 档收下来**；
 * - 段数不是 5 或 6；6 段时秒位不是 `0`（`30 * * * * *` 这类没有对应控件）；
 * - 月份段不是 `*`（模型里没有「几月」）；
 * - 日段/星期段是步进 `*\/2`（这两段没有 step 控件，展开成一串数字等于改写用户表达式；
 *   小时段的 `*\/N` 有 step 档，是唯一被收下的步进小时形态）；
 * - 日段与星期段同时被限定（并集陷阱，见上）；
 * - 「每月几号」形态下时/分不是单点。
 * 返回 null 时调用方应以默认模型开局并提示「确认后会覆盖」，不能默认覆盖用户手写的东西。
 */

export type CronVisualModel = {
  /** true -> 6 段（秒位固定为 0）；false -> 5 段 */
  withSeconds: boolean
  /** 频率：前五种是常见快捷形态，`interval` 是「分钟形态 + 小时形态 + 星期」三组控件全开的那一档 */
  frequency: 'minute' | 'hour' | 'day' | 'week' | 'month' | 'interval'
  minuteMode: 'step' | 'fixed' | 'list'
  /** 1..59，对应 `*\/N`；为 1 时输出 `*` */
  minuteStep: number
  minute: number
  minutes: number[]
  hourMode: 'all' | 'step' | 'range' | 'list' | 'fixed'
  /** 1..23，对应小时段 `*\/N`；为 1 时输出 `*`。注意是按 0 点起算的整点，且每天重新起算 */
  hourStep: number
  /** 对应 `9-22`，闭区间：22 点这一小时同样会执行 */
  hourStart: number
  hourEnd: number
  hour: number
  hours: number[]
  weekMode: 'all' | 'workday' | 'weekend' | 'custom'
  /** 0..6，0 = 周日 */
  weekdays: number[]
  /** 1..31，只在 `frequency === 'month'` 时参与生成 */
  monthDays: number[]
}

/** 0 = 周日，与 cron 的 dow 取值一一对应 */
export const WEEKDAY_LABELS = ['周日', '周一', '周二', '周三', '周四', '周五', '周六']

/**
 * 默认模型：「每天 00:00」，与 CronInput 里 DEFAULT_CRON_DAILY_MIDNIGHT 同一形态。
 *
 * 它只在两种场合出现：弹窗第一次开、以及反解失败兜底。选最保守的「每天 0 点」是因为
 * 反解失败时用户可能直接点确定，覆盖成一条看得懂的规则比覆盖成一条花哨规则安全。
 * 时段/步进那几个字段仍预置成 9-22、每 10 分钟、每 2 小时，切到「按时段间隔」就是本次需求的样例。
 */
export function createDefaultCronVisualModel(): CronVisualModel {
  return {
    withSeconds: true,
    frequency: 'day',
    minuteMode: 'fixed',
    minuteStep: 10,
    minute: 0,
    minutes: [0, 30],
    hourMode: 'fixed',
    hourStep: 2,
    hourStart: 9,
    hourEnd: 22,
    hour: 0,
    hours: [9, 18],
    weekMode: 'all',
    weekdays: [1, 2, 3, 4, 5],
    monthDays: [1]
  }
}

/** 取整 + 钳制；拿到 NaN（控件被清空过）时回落到 fallback，不让 NaN 漏进表达式 */
function clampInt(value: number, min: number, max: number, fallback: number): number {
  const num = Math.floor(Number(value))
  if (!Number.isFinite(num)) {
    return fallback
  }
  return Math.min(max, Math.max(min, num))
}

/** 升序去重 + 丢掉越界项 */
function normalizeList(values: number[] | undefined, min: number, max: number): number[] {
  const seen = new Set<number>()
  for (const raw of values || []) {
    const num = Math.floor(Number(raw))
    if (!Number.isFinite(num) || num < min || num > max) continue
    seen.add(num)
  }
  return [...seen].sort((a, b) => a - b)
}

/**
 * 升序去重后的列表 -> 段文本。
 * 整段恰好是一条连续区间（且不止一个值）时收成 `a-b`，于是 `1,2,3,4,5` 与 `1-5`
 * 只有一种写法，反解时不会出现「同一个语义两个表达式」。
 */
function formatList(values: number[]): string {
  if (values.length > 1 && values[values.length - 1]! - values[0]! === values.length - 1) {
    return `${values[0]}-${values[values.length - 1]}`
  }
  return values.join(',')
}

function expandRange(start: number, end: number): number[] {
  const values: number[] = []
  for (let value = start; value <= end; value++) {
    values.push(value)
  }
  return values
}

function pad2(value: number): string {
  return value < 10 ? `0${value}` : String(value)
}

/**
 * 小时步进实际会落在哪些整点：`*\/2` -> `[0,2,4,…,22]`。
 * cron 的步进是**从段起点（0 点）起算**并且每天重新起算，不是「从现在起每隔 N 小时」，
 * 所以 `*\/5` 的最后一次是 20 点，离次日 0 点只有 4 小时——文案里得把这事说清楚。
 */
function hourStepValues(step: number): number[] {
  const values: number[] = []
  for (let hour = 0; hour <= 23; hour += step) {
    values.push(hour)
  }
  return values
}

/** 步进小时列表的短写法：超过 4 个只写前三个 + `…` + 末位，免得预览被撑成一长串 */
function formatHourStepPreview(values: number[]): string {
  if (values.length <= 4) {
    return values.join('、')
  }
  return `${values[0]!}、${values[1]!}、${values[2]!}…${values[values.length - 1]!}`
}

// ───────────────────────────── 生成 ─────────────────────────────

/** 分钟段：step -> `*` / `*\/N`；fixed -> `30`；list -> `0,30`（连续时收成 `0-3`） */
function formatMinutePart(mode: CronVisualModel['minuteMode'], model: CronVisualModel): string {
  if (mode === 'step') {
    const step = clampInt(model.minuteStep, 1, 59, 1)
    // `*\/1` 与 `*` 等价，统一输出 `*`
    return step === 1 ? '*' : `*/${step}`
  }
  if (mode === 'list') {
    const list = normalizeList(model.minutes, 0, 59)
    // 一个都没勾时退回固定分钟，避免拼出空段让整条表达式作废
    return list.length === 0 ? String(clampInt(model.minute, 0, 59, 0)) : formatList(list)
  }
  return String(clampInt(model.minute, 0, 59, 0))
}

/**
 * 小时段：all -> `*`；step -> `*\/2`；range -> `9-22`；list -> `9,18`（连续时收成 `9-11`）；fixed -> `9`。
 * 跨零点返回 null，由 buildCronExpression 转成空串。
 */
function formatHourPart(mode: CronVisualModel['hourMode'], model: CronVisualModel): string | null {
  if (mode === 'all') {
    return '*'
  }
  if (mode === 'step') {
    const step = clampInt(model.hourStep, 1, 23, 1)
    // 与分钟步进同一套归一规则：`*\/1` 与 `*` 等价，统一输出 `*`
    return step === 1 ? '*' : `*/${step}`
  }
  if (mode === 'range') {
    const start = clampInt(model.hourStart, 0, 23, 0)
    const end = clampInt(model.hourEnd, 0, 23, 23)
    if (end < start) {
      return null
    }
    // 0-23 就是全天，`9-9` 就是 9 点：都归一到更短的写法，省得同一语义两种表达式
    if (start === 0 && end === 23) return '*'
    if (start === end) return String(start)
    return `${start}-${end}`
  }
  if (mode === 'list') {
    const list = normalizeList(model.hours, 0, 23)
    return list.length === 0 ? '*' : formatList(list)
  }
  return String(clampInt(model.hour, 0, 23, 0))
}

/** 星期段：工作日固定 `1-5`、周末固定 `0,6`，与后端出厂预设逐字节一致 */
function formatWeekPart(model: CronVisualModel): string {
  if (model.weekMode === 'workday') return '1-5'
  if (model.weekMode === 'weekend') return '0,6'
  if (model.weekMode === 'custom') {
    const list = normalizeList(model.weekdays, 0, 6)
    // 一天没勾 / 七天全勾都等于不限定星期
    if (list.length === 0 || list.length === 7) return '*'
    return formatList(list)
  }
  return '*'
}

/** 「每月几号」段；一天没勾或 31 天全勾都等于不限定日期 */
function formatMonthDayPart(model: CronVisualModel): string {
  const list = normalizeList(model.monthDays, 1, 31)
  if (list.length === 0 || list.length === 31) return '*'
  return formatList(list)
}

/**
 * 模型 -> cron 表达式。
 * 跨零点时段返回空串（见文件头契约），调用方应据此禁用「应用」并给提示。
 */
export function buildCronExpression(model: CronVisualModel): string {
  let minutePart: string
  let hourPart: string | null
  let dayPart = '*'
  let weekPart = '*'

  switch (model.frequency) {
    case 'minute':
      // 「每 N 分钟」：小时/日/星期全开，只有分钟是步进
      minutePart = formatMinutePart('step', model)
      hourPart = '*'
      break
    case 'hour':
      minutePart = formatMinutePart('fixed', model)
      hourPart = '*'
      break
    case 'day':
      minutePart = formatMinutePart('fixed', model)
      hourPart = formatHourPart('fixed', model)
      break
    case 'week':
      minutePart = formatMinutePart('fixed', model)
      hourPart = formatHourPart('fixed', model)
      weekPart = formatWeekPart(model)
      break
    case 'month':
      minutePart = formatMinutePart('fixed', model)
      hourPart = formatHourPart('fixed', model)
      // 只写 dom、weekPart 保持 `*`：两边同时限定在 cron 里是并集，用户会当成 AND
      dayPart = formatMonthDayPart(model)
      break
    default:
      // interval：分钟形态 + 小时形态（含「每 N 小时」）+ 星期三者全开，本次需求的主角
      minutePart = formatMinutePart(model.minuteMode, model)
      hourPart = formatHourPart(model.hourMode, model)
      weekPart = formatWeekPart(model)
      break
  }

  if (hourPart === null) {
    return ''
  }

  const fiveParts = `${minutePart} ${hourPart} ${dayPart} * ${weekPart}`
  // ⚠️ 补段永远在开头补 `0`，末尾补 `*` 会把「每分钟」变成「每秒」
  return model.withSeconds ? `0 ${fiveParts}` : fiveParts
}

// ───────────────────────────── 反解 ─────────────────────────────

const NUMBER_RE = /^\d+$/
const STEP_RE = /^\*\/(\d+)$/
const RANGE_RE = /^(\d+)-(\d+)$/

type ParsedSegment =
  | { kind: 'all' }
  | { kind: 'step'; step: number }
  | { kind: 'range'; start: number; end: number }
  /** 单个数字也走这里（values.length === 1），由调用方决定算 fixed 还是 list */
  | { kind: 'list'; values: number[] }

/**
 * 单段反解。只认四种规范形态，其余一律 null：
 * `L` / `W` / `#` / `?`、`JAN` / `MON` 英文别名、混合形态 `9-11,21`、带步进的区间 `9-22/2`
 * 都会在最后那轮「逐项必须是纯数字」里被挡掉。
 */
function parseSegment(raw: string, min: number, max: number): ParsedSegment | null {
  const text = raw.trim()
  if (!text) return null
  if (text === '*') return { kind: 'all' }

  const step = STEP_RE.exec(text)
  if (step) {
    const value = Number(step[1])
    // `*\/0` 非法；步进大于取值上限没有意义
    if (!Number.isFinite(value) || value < 1 || value > max) return null
    return { kind: 'step', step: value }
  }

  const range = RANGE_RE.exec(text)
  if (range) {
    const start = Number(range[1])
    const end = Number(range[2])
    if (start < min || start > max || end < min || end > max) return null
    // 倒序区间（robfig 也不接受）不认，免得反解出一个界面上表达不了的状态
    if (end < start) return null
    return { kind: 'range', start, end }
  }

  const values: number[] = []
  for (const item of text.split(',')) {
    if (!NUMBER_RE.test(item)) return null
    const value = Number(item)
    if (value < min || value > max) return null
    values.push(value)
  }
  if (values.length === 0) return null
  return { kind: 'list', values: [...new Set(values)].sort((a, b) => a - b) }
}

/** range 与 list 统一成数值数组，方便后面判断「是不是恰好等于 1-5 / 0,6」 */
function segmentValues(segment: ParsedSegment): number[] {
  if (segment.kind === 'range') return expandRange(segment.start, segment.end)
  if (segment.kind === 'list') return segment.values
  return []
}

function sameValues(values: number[], expected: number[]): boolean {
  return values.length === expected.length && values.every((value, index) => value === expected[index])
}

/**
 * cron 表达式 -> 模型；认不出返回 null（判据见文件头契约，绝不猜）。
 */
export function parseCronExpression(expr: string): CronVisualModel | null {
  const parts = (expr || '').trim().split(/\s+/).filter(Boolean)
  if (parts.length !== 5 && parts.length !== 6) {
    return null
  }

  const withSeconds = parts.length === 6
  // 6 段时秒位只认 `0`：`30 0 9 * * *`（每天 09:00:30）在可视化里没有对应控件
  if (withSeconds && parts[0] !== '0') {
    return null
  }

  const offset = withSeconds ? 1 : 0
  // 月份段没有对应控件，只认全开
  if (parts[offset + 3] !== '*') {
    return null
  }

  const minuteSeg = parseSegment(parts[offset]!, 0, 59)
  const hourSeg = parseSegment(parts[offset + 1]!, 0, 23)
  const daySeg = parseSegment(parts[offset + 2]!, 1, 31)
  const weekSeg = parseSegment(parts[offset + 4]!, 0, 6)
  if (!minuteSeg || !hourSeg || !daySeg || !weekSeg) {
    return null
  }

  // dom 与 dow 同时被限定时 cron 取【并集】不是交集，可视化里没法用「几号 + 星期几」表达
  if (daySeg.kind !== 'all' && weekSeg.kind !== 'all') {
    return null
  }
  // 日/星期没有步进模式：展开成一串数字等于改写用户的表达式（小时段有 step 档，见下）
  if (daySeg.kind === 'step' || weekSeg.kind === 'step') {
    return null
  }

  const model = createDefaultCronVisualModel()
  model.withSeconds = withSeconds

  // 分钟
  if (minuteSeg.kind === 'all') {
    model.minuteMode = 'step'
    model.minuteStep = 1
  } else if (minuteSeg.kind === 'step') {
    model.minuteMode = 'step'
    model.minuteStep = minuteSeg.step
  } else {
    const values = segmentValues(minuteSeg)
    if (values.length === 1) {
      model.minuteMode = 'fixed'
      model.minute = values[0]!
    } else {
      model.minuteMode = 'list'
      model.minutes = values
    }
  }

  // 小时
  if (hourSeg.kind === 'all') {
    model.hourMode = 'all'
  } else if (hourSeg.kind === 'step') {
    // `*\/1` 生成出来就是 `*`，直接归到「全天」，免得界面上出现一个等价于全天的「每 1 小时」
    if (hourSeg.step === 1) {
      model.hourMode = 'all'
    } else {
      model.hourMode = 'step'
      model.hourStep = hourSeg.step
    }
  } else if (hourSeg.kind === 'range') {
    model.hourMode = 'range'
    model.hourStart = hourSeg.start
    model.hourEnd = hourSeg.end
  } else {
    const values = segmentValues(hourSeg)
    if (values.length === 1) {
      model.hourMode = 'fixed'
      model.hour = values[0]!
    } else {
      model.hourMode = 'list'
      model.hours = values
    }
  }

  // 星期：`1-5` -> 工作日、`0,6`（含 `6,0`，parseSegment 已排序）-> 周末
  if (weekSeg.kind === 'all') {
    model.weekMode = 'all'
  } else {
    const values = segmentValues(weekSeg)
    if (sameValues(values, [1, 2, 3, 4, 5])) {
      model.weekMode = 'workday'
    } else if (sameValues(values, [0, 6])) {
      model.weekMode = 'weekend'
    } else if (values.length === 7) {
      model.weekMode = 'all'
    } else {
      model.weekMode = 'custom'
      model.weekdays = values
    }
  }

  // 「每月几号」：dom 被限定就只能是这一档，且时/分必须是单点（界面上只给了单点控件）
  if (daySeg.kind !== 'all') {
    if (model.minuteMode !== 'fixed' || model.hourMode !== 'fixed') {
      return null
    }
    model.frequency = 'month'
    model.monthDays = segmentValues(daySeg)
    return model
  }

  if (model.weekMode !== 'all') {
    // 星期被限定：时/分都是单点就是普通「每周」，否则要靠「按时段间隔」才表达得了
    model.frequency = model.minuteMode === 'fixed' && model.hourMode === 'fixed' ? 'week' : 'interval'
    return model
  }
  if (model.minuteMode === 'step' && model.hourMode === 'all') {
    model.frequency = 'minute'
    return model
  }
  if (model.minuteMode === 'fixed' && model.hourMode === 'all') {
    model.frequency = 'hour'
    return model
  }
  if (model.minuteMode === 'fixed' && model.hourMode === 'fixed') {
    model.frequency = 'day'
    return model
  }
  // 剩下的（分钟步进 + 限定时段、小时步进 `*\/2` 这类出厂预设……）只有「按时段间隔」表达得了
  model.frequency = 'interval'
  return model
}

// ───────────────────────────── 预览文案 ─────────────────────────────

/** 「工作日」「周末」「周一、周三」；不限定星期时返回「每天」 */
function weekLabel(model: CronVisualModel): string {
  if (model.weekMode === 'workday') return '工作日'
  if (model.weekMode === 'weekend') return '周末'
  if (model.weekMode === 'custom') {
    const list = normalizeList(model.weekdays, 0, 6)
    if (list.length === 0 || list.length === 7) return '每天'
    return list.map(day => WEEKDAY_LABELS[day]).join('、')
  }
  return '每天'
}

function hourLabel(model: CronVisualModel): string {
  if (model.hourMode === 'all') return '全天'
  if (model.hourMode === 'step') {
    const step = clampInt(model.hourStep, 1, 23, 1)
    // `*\/1` 生成出来是 `*`，措辞跟着表达式走，不然预览会说「每 1 小时」而表达式写着全天
    if (step === 1) return '全天'
    return `每 ${step} 小时（${formatHourStepPreview(hourStepValues(step))} 点）`
  }
  if (model.hourMode === 'range') {
    const start = clampInt(model.hourStart, 0, 23, 0)
    const end = clampInt(model.hourEnd, 0, 23, 23)
    // 起止同一小时时表达式归一成 `9`（见 formatHourPart），文案跟着表达式走：
    // 说「9-9 点」而表达式写着 `9`，用户会以为哪里配错了
    if (start === end) return `${start} 点`
    return `${start}-${end} 点`
  }
  if (model.hourMode === 'list') {
    const list = normalizeList(model.hours, 0, 23)
    return list.length === 0 ? '全天' : `${list.join('、')} 点`
  }
  return `${clampInt(model.hour, 0, 23, 0)} 点`
}

function minuteLabel(model: CronVisualModel): string {
  if (model.minuteMode === 'step') {
    const step = clampInt(model.minuteStep, 1, 59, 1)
    return step === 1 ? '每分钟执行一次' : `每 ${step} 分钟执行一次`
  }
  if (model.minuteMode === 'list') {
    const list = normalizeList(model.minutes, 0, 59)
    if (list.length > 0) {
      return `第 ${list.join('、')} 分钟执行`
    }
  }
  return `第 ${clampInt(model.minute, 0, 59, 0)} 分钟执行`
}

/**
 * 「星期」选了自定义却一天都没勾。
 *
 * 这种状态下 `formatWeekPart` 会退回 `*`（不限定星期，见那边的注释），表达式本身完全合法、
 * 后端也认，**所以生成逻辑一个字都不改**——拼空段反而会让整条表达式作废。
 * 但界面上一天没勾、预览却说「每天」，与用户预期正好相反，于是由 UI 拦住：
 * 预览改说「请至少选择一天」，并把「应用」按钮一起禁掉。
 *
 * 只在星期控件真的渲染时（每周 / 按时段间隔）才算数：其余频率下星期段根本不参与生成，
 * 拿它去禁用按钮会变成「按钮灰了却找不到哪里红」。
 */
export function hasEmptyWeekSelection(model: CronVisualModel): boolean {
  if (model.frequency !== 'week' && model.frequency !== 'interval') {
    return false
  }
  if (model.weekMode !== 'custom') {
    return false
  }
  return normalizeList(model.weekdays, 0, 6).length === 0
}

/**
 * 弹窗内的实时预览人话。只描述「当前模型」，规则行下方那条描述仍以后端下发的为准。
 */
export function describeCronVisual(model: CronVisualModel): string {
  if (!buildCronExpression(model)) {
    return '结束小时早于起始小时，单条 cron 表达不了跨零点的时段'
  }
  if (hasEmptyWeekSelection(model)) {
    return '星期选了「自定义」但一天都没勾，请至少选择一天'
  }

  const clock = `${pad2(clampInt(model.hour, 0, 23, 0))}:${pad2(clampInt(model.minute, 0, 59, 0))}`

  switch (model.frequency) {
    case 'minute': {
      const step = clampInt(model.minuteStep, 1, 59, 1)
      return step === 1 ? '每分钟执行一次' : `每 ${step} 分钟执行一次`
    }
    case 'hour':
      return `每小时的第 ${clampInt(model.minute, 0, 59, 0)} 分钟执行`
    case 'day':
      return `每天 ${clock} 执行`
    case 'week':
      return `${weekLabel(model)} ${clock} 执行`
    case 'month': {
      const days = normalizeList(model.monthDays, 1, 31)
      const dayText = days.length === 0 || days.length === 31 ? '每天' : `每月 ${days.join('、')} 号`
      return `${dayText} ${clock} 执行`
    }
    default: {
      // 不限定星期时不加「每天」前缀，否则会读成「每天全天」。
      // 判据取星期段本身而不是 weekMode：「自定义」把七天全勾上时星期段同样是 `*`
      // （用户从「每天」切过来就是这个状态），语义就是不限定，前缀也该跟着消失。
      const prefix = formatWeekPart(model) === '*' ? '' : `${weekLabel(model)} `
      const hourStep = clampInt(model.hourStep, 1, 23, 1)
      // 「每 2 小时」最容易被读成「从现在起每隔 2 小时」，实际是 0、2、4…22 点各触发一次，
      // 所以这一档把具体整点摆出来；分钟为 0 时说「整点」，比通用的「第 0 分钟执行」好读。
      // （step 为 1 时表达式就是 `*`，交给下面的通用分支说成「全天」）
      if (model.hourMode === 'step' && hourStep > 1 && model.minuteMode === 'fixed') {
        const at = clampInt(model.minute, 0, 59, 0)
        const clockText = at === 0 ? '整点' : `${at} 分`
        return `${prefix}每 ${hourStep} 小时（${formatHourStepPreview(hourStepValues(hourStep))} 点）${clockText}执行`
      }
      return `${prefix}${hourLabel(model)}，${minuteLabel(model)}`
    }
  }
}

/** 时段区间内当天最后一次触发落在第几分钟 */
function lastMinuteInHour(model: CronVisualModel): number {
  if (model.minuteMode === 'step') {
    const step = clampInt(model.minuteStep, 1, 59, 1)
    return Math.floor(59 / step) * step
  }
  if (model.minuteMode === 'list') {
    const list = normalizeList(model.minutes, 0, 59)
    if (list.length > 0) {
      return list[list.length - 1]!
    }
  }
  return clampInt(model.minute, 0, 59, 0)
}

/**
 * 时段歧义提示，两种：
 *
 * 1. 闭区间。robfig 的小时区间是**闭区间**，`*\/10 9-22` 最后一次触发是 **22:50**，
 *    而用户嘴里的「到晚上 10 点」按字面是 22:00 收尾。这里把差异摆到明面上让用户自己决定，
 *    **不偷偷替他改成 `9-21`**。
 * 2. 步进跨天。`*\/N` 是从 0 点起算的整点，且**每天重新起算**——`*\/5` 当天最后一次是 20 点，
 *    到次日 0 点只隔 4 小时。不提醒的话用户会以为「每 5 小时」是连续均匀的。
 *    N 能整除 24 时不提示：那种情况本来就是均匀的，具体整点在预览文案里已经列了。
 *
 * 只有「按时段间隔」下这两种小时形态才有坑，其余返回空串（UI 据此不渲染）。
 */
export function nextFireHint(model: CronVisualModel): string {
  if (model.frequency !== 'interval') {
    return ''
  }

  if (model.hourMode === 'step') {
    const step = clampInt(model.hourStep, 1, 23, 1)
    const values = hourStepValues(step)
    const last = values[values.length - 1]!
    const wrap = 24 - last
    // 步进能整除 24 时（1/2/3/4/6/8/12）间隔本来就是均匀的，预览已经把具体整点列出来了，不用再唠叨；
    // 除不尽时（`*\/5` -> 0、5、10、15、20）跨天那一段会短一截，这才是真正会让人算错的地方
    if (wrap === step) {
      return ''
    }
    return `每 ${step} 小时是从 0 点起算、并且每天重新起算的：当天在 ${formatHourStepPreview(values)} 点各触发一次，`
      + `${last} 点到次日 0 点只隔 ${wrap} 小时，不是 ${step} 小时。`
  }

  if (model.hourMode !== 'range') {
    return ''
  }
  const start = clampInt(model.hourStart, 0, 23, 0)
  const end = clampInt(model.hourEnd, 0, 23, 23)
  // 跨零点是错误态（由 UI 单独提示）；起止同一小时不存在闭区间歧义，两种都不提示
  if (end <= start) {
    return ''
  }

  const last = `${pad2(end)}:${pad2(lastMinuteInHour(model))}`
  return `${start}-${end} 点是闭区间：${end} 点这一小时同样会执行，当天最后一次是 ${last}。`
    + `如果你要的是「${pad2(end)}:00 就收尾」，把结束小时改成 ${end - 1} 点。`
}
