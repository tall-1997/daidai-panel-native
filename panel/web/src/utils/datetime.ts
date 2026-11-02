/**
 * 日期时间格式化与日期范围工具。
 *
 * 背景：面板各页面原本各写各的，同一个「创建时间」在不同页面长得不一样——
 *   - `new Date(v).toLocaleString()`                       users / subscriptions / profile
 *     跟着浏览器 locale 走，英文环境下会渲染成 `8/23/2026, 3:04:05 PM`
 *   - `new Date(v).toLocaleString('zh-CN')`                deps
 *     没关 hour12，会渲染成 `2026/8/23 下午3:04:05`
 *   - `new Date(v).toLocaleString('zh-CN', {hour12:false})` logs / dashboard / open-api / notifications
 *     `2026/8/23 15:04:05`，这是最接近工具型后台需要的一种
 *   - `new Date(v).toLocaleDateString('zh-CN')`            notifications 的「最近测试」
 *     全站唯一只给日期不给时间的，粒度和别处对不上
 *
 * 这里以 24 小时制、zh-CN、补零对齐为统一口径。
 *
 * ⚠️ 时区：本文件全部按【浏览器本地时区】渲染，而服务端另有一项「面板时区」配置
 * （`system_configs.panel_timezone`，默认 Asia/Shanghai），任务是否「今天该跑」由它判定。
 * 两者不一致时，「今天」的边界会错开。做日期范围筛选时必须在控件旁标注所用时区，
 * 否则用户会遇到「明明筛了今天却少了几条」而无从解释。
 */

/** 一天的毫秒数 */
const DAY_MS = 24 * 60 * 60 * 1000

function toDate(value: string | number | Date | null | undefined): Date | null {
  if (value === null || value === undefined || value === '') return null
  const date = value instanceof Date ? value : new Date(value)
  // Invalid Date 的 getTime() 是 NaN。后端偶尔会下发空字符串或 "0001-01-01T00:00:00Z"，
  // 直接渲染会在界面上出现 "Invalid Date"，比显示 "-" 糟糕得多。
  if (Number.isNaN(date.getTime())) return null
  // Go 的零值时间。数据库里没写过的 *time.Time 字段序列化出来就是它，
  // 语义是「没有」，不是「公元 1 年」。
  if (date.getFullYear() <= 1) return null
  return date
}

const pad = (n: number) => String(n).padStart(2, '0')

/**
 * `2026-08-23 15:04:05`
 *
 * 用连字符而不是 `toLocaleString` 的斜杠：等宽、可排序、月日恒为两位，
 * 表格里上下行对得齐（`2026/8/3` 与 `2026/12/23` 宽度不同，列会看着毛糙）。
 */
export function formatDateTime(
  value: string | number | Date | null | undefined,
  fallback = '-'
): string {
  const date = toDate(value)
  if (!date) return fallback
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    ` ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  )
}

/** `2026-08-23 15:04`（不要秒。给「下次执行」「最近测试」这类不需要秒精度的场景） */
export function formatDateTimeShort(
  value: string | number | Date | null | undefined,
  fallback = '-'
): string {
  const date = toDate(value)
  if (!date) return fallback
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    ` ${pad(date.getHours())}:${pad(date.getMinutes())}`
  )
}

/** `2026-08-23` */
export function formatDate(
  value: string | number | Date | null | undefined,
  fallback = '-'
): string {
  const date = toDate(value)
  if (!date) return fallback
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
}

/**
 * 相对时间：`刚刚` / `5 分钟前` / `3 小时前` / `昨天 15:04` / `2026-08-20 15:04`
 *
 * 只用于「最近发生过什么」的场景（最近登录、上次拉取、最近测试）。
 * 分档到 7 天为止，再久就直接给绝对时间——「37 天前」这种表述读者还得自己换算，
 * 不如直接给日期。
 *
 * 未来时间（下次执行、令牌有效期）不适用本函数：它只描述过去。
 */
export function formatRelativeTime(
  value: string | number | Date | null | undefined,
  fallback = '-'
): string {
  const date = toDate(value)
  if (!date) return fallback

  const diff = Date.now() - date.getTime()
  // 时钟回拨或服务端时间超前时 diff 会是负数，这时相对表述没有意义，退回绝对时间
  if (diff < 0) return formatDateTimeShort(date)

  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)} 分钟前`
  if (diff < DAY_MS) return `${Math.floor(diff / 3_600_000)} 小时前`

  // 「昨天」按自然日判断，而不是「24 小时以内」：
  // 今天 00:30 看昨天 23:50 的记录，差值只有 40 分钟，但用户心里那就是「昨天」。
  const startOfToday = startOfDay(new Date())
  const startOfYesterday = new Date(startOfToday.getTime() - DAY_MS)
  if (date >= startOfYesterday && date < startOfToday) {
    return `昨天 ${pad(date.getHours())}:${pad(date.getMinutes())}`
  }

  if (diff < 7 * DAY_MS) return `${Math.floor(diff / DAY_MS)} 天前`
  return formatDateTimeShort(date)
}

/** 当天 00:00:00.000 */
export function startOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(0, 0, 0, 0)
  return d
}

/** 当天 23:59:59.999 */
export function endOfDay(date: Date): Date {
  const d = new Date(date)
  d.setHours(23, 59, 59, 999)
  return d
}

/**
 * 把日期范围转成后端参数。
 *
 * 两端都用【整天】边界：用户选「8-18 到 8-21」，心里想的是「这四天全部」，
 * 而 el-date-picker 的 daterange 给回来的两个 Date 时分秒都是 00:00:00，
 * 直接传出去会把结束日当天的记录整天漏掉——这是日期范围筛选最经典的一个 off-by-one。
 *
 * 序列化用 RFC3339（`toISOString`），Go 侧 `time.Parse(time.RFC3339, ...)` 直接吃。
 * 不用 `2026-08-23` 这种纯日期串：那样时区归属含糊，服务端只能猜。
 */
export function toDateRangeParams(
  range: [Date, Date] | null | undefined
): { start_time?: string; end_time?: string } {
  if (!range || range.length !== 2 || !range[0] || !range[1]) return {}
  return {
    start_time: startOfDay(range[0]).toISOString(),
    end_time: endOfDay(range[1]).toISOString(),
  }
}

export interface DateRangeShortcut {
  key: string
  label: string
  /** 返回 [起, 止]，两端都是当天的任意时刻，边界由 toDateRangeParams 统一收拢 */
  value: () => [Date, Date]
}

/**
 * 日期范围快捷项。
 *
 * 「近 N 天」一律【含今天】：用户说「近 7 天」指的是「今天往前数 7 天」，
 * 不含今天的话刚跑完的任务不出现在结果里，会被当成 bug。
 */
export const DATE_RANGE_SHORTCUTS: DateRangeShortcut[] = [
  {
    key: 'today',
    label: '今天',
    value: () => {
      const now = new Date()
      return [now, now]
    },
  },
  {
    key: 'yesterday',
    label: '昨天',
    value: () => {
      const yesterday = new Date(Date.now() - DAY_MS)
      return [yesterday, yesterday]
    },
  },
  {
    key: 'last7',
    label: '近 7 天',
    value: () => [new Date(Date.now() - 6 * DAY_MS), new Date()],
  },
  {
    key: 'last30',
    label: '近 30 天',
    value: () => [new Date(Date.now() - 29 * DAY_MS), new Date()],
  },
]

/**
 * 范围跨了几天（含首尾）。用于「共 N 天」这类汇总文案。
 * 同一天返回 1，而不是 0 —— 选中一天就是筛了一天的数据。
 */
export function countDaysInRange(range: [Date, Date] | null | undefined): number {
  if (!range || !range[0] || !range[1]) return 0
  const start = startOfDay(range[0]).getTime()
  const end = startOfDay(range[1]).getTime()
  return Math.floor((end - start) / DAY_MS) + 1
}
