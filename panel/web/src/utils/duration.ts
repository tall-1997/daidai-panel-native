/**
 * 耗时格式化工具。
 *
 * 背景：面板各页面原本各写各的，普遍直接甩一个 `xxx.toFixed(1) + 's'`，
 * 于是「上次运行耗时」会出现 `4711.9s` 这种数字 —— 用户得自己心算才知道是一个多小时。
 * 这里统一成紧凑组合单位，让人一眼能读。
 *
 * 输入单位固定为「秒」（服务端 `time.Since(...).Seconds()` 的产物，如
 * `task.last_running_time`、任务执行日志与订阅拉取日志的 `duration`）。
 * ⚠️ Open API 调用日志的 `duration` 是**毫秒**，不要直接丢进来。
 */

const SECONDS_PER_MINUTE = 60
const SECONDS_PER_HOUR = 3600

/**
 * 把 `[1, 60)` 秒截断到 1 位小数，上限硬钳在 `59.9`。
 *
 * 两层保护，缺一不可：
 * 1. 直接 `Math.floor(v * 10)` 会踩浮点精度 —— 某些值乘 10 后落在
 *    `28.999999999999996` 这种比整数略小的位置，截断就凭空少了 0.1。
 *    先 `toFixed(6)` 抹掉 1e-6 以下的噪声再截断。
 * 2. 但 `toFixed(6)` 本身是四舍五入，对无限逼近 60 的输入（如 `59.9999999999`）
 *    又会把它顶成 `600` 从而输出 `60.0s`。所以再用 `Math.min(599, ...)` 封死上界。
 */
function truncateToTenth(value: number): string {
  const tenths = Math.min(599, Math.floor(Number((value * 10).toFixed(6))))
  return (tenths / 10).toFixed(1)
}

/**
 * 把秒数格式化成紧凑的时分秒文本。
 *
 * 分档（每档只显示两个量级，再细就是噪声）：
 * - `0`            -> `0s`      （任务刚开始跑时服务端会把耗时重置为 0，显示 `0ms` 反而费解）
 * - `(0, 1)`       -> `856ms`
 * - `[1, 60)`      -> `12.3s`
 * - `[60, 3600)`   -> `5m12s`，整分时省略秒位 -> `5m`
 * - `>= 3600`      -> `1h18m`，整点时省略分位 -> `6h`（小时级别再精确到秒没有意义）
 *
 * **全档一律向下截断，不四舍五入**，理由有二：
 * 1. 语义上这是「已经跑了多久」的秒表读数，截断才不会虚报；
 * 2. 四舍五入会产生进位链，`59.95s` 会被渲染成 `60.0s`、`3599.6s` 会变成 `59m60s`，
 *    每一档都得写一遍进位兜底。截断从根上消掉了这类越界输出：
 *    ms 位最大 999、秒位最大 59.9、分位与秒位最大 59。
 *
 * 边界行为（无前端测试框架，契约写在这里）：
 * - `null` / `undefined` / 非 number / `NaN` / `±Infinity` / 负数 -> 返回 `fallback`（默认 `'-'`）。
 *   负数只可能来自时钟回拨等异常，宁可显示 `-` 也不显示 `-500ms`。
 * - 恰好 `60` -> `1m`；恰好 `3600` -> `1h`；恰好 `1` -> `1.0s`。
 * - `59.95` -> `59.9s`（不会变成 `60.0s`）；`0.9999` -> `999ms`（不会变成 `1000ms`）；
 *   `3599.999` -> `59m59s`（不会变成 `59m60s` 或 `60m`）。
 * - 小于 1 毫秒（如 `0.0004`）-> `0ms`，如实反映而不是伪造成 `1ms`。
 *
 * 调用方仍可沿用原来的 `x != null` 判断后再调用；这里做全量守卫只是为了兜底，
 * 直接把可能为 null 的值传进来同样安全。
 */
export function formatDuration(seconds: number | null | undefined, fallback = '-'): string {
  // 非法输入统一回落，避免把 NaN/Infinity 渲染到界面上
  if (typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) return fallback

  if (seconds === 0) return '0s'
  // 毫秒位同样钳在 999，保证 (0,1) 区间永远不会输出 "1000ms"
  if (seconds < 1) return `${Math.min(999, Math.floor(seconds * 1000))}ms`
  if (seconds < SECONDS_PER_MINUTE) return `${truncateToTenth(seconds)}s`

  if (seconds < SECONDS_PER_HOUR) {
    const minutes = Math.floor(seconds / SECONDS_PER_MINUTE)
    const restSeconds = Math.floor(seconds % SECONDS_PER_MINUTE)
    return restSeconds === 0 ? `${minutes}m` : `${minutes}m${restSeconds}s`
  }

  const hours = Math.floor(seconds / SECONDS_PER_HOUR)
  const restMinutes = Math.floor((seconds % SECONDS_PER_HOUR) / SECONDS_PER_MINUTE)
  return restMinutes === 0 ? `${hours}h` : `${hours}h${restMinutes}m`
}
