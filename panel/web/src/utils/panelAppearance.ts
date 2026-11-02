import { loadPanelSettings, type PanelSettingsPayload, type PanelShapeStyle } from './panelSettings'

export interface PanelAppearanceSettings extends PanelSettingsPayload {}

/**
 * 圆角三档刻度表。
 *
 * 刻度集中在这一处，不要把数值散到分支里：调一档、加一档都只改这里。
 * square 各项全 0，与 styles/global.scss `:root` 里的默认值一致 ——
 * 这样「没写过内联变量」和「显式写成直角」看到的是同一个结果。
 *
 * 对应关系（详见 spec/frontend/design-system.md）：
 * - control：按钮、输入框、tag、分段项、图标按钮、浮层菜单项
 * - surface：卡片、表格容器、弹窗、日志面板等容器类表面
 * - pill：状态 chip、计数角标、进度条这类天然胶囊
 * - mobileControl / mobileSurface（v3.3.1，issue #143）：≤768px 下「按钮」「卡片」两个角色的刻度。
 *   它们不直接被组件消费，而是由 global.scss 里的角色令牌 --dd-radius-button / --dd-radius-card
 *   在移动端媒体查询里取用（桌面上这两个角色令牌仍等于 control / surface）。
 *   square 档同样全 0：形状是全局配置，默认直角的面板在手机上也必须是直角，不能自己圆起来。
 */
const PANEL_SHAPE_RADIUS: Record<
  PanelShapeStyle,
  { control: string; surface: string; pill: string; mobileControl: string; mobileSurface: string }
> = {
  square: { control: '0', surface: '0', pill: '0', mobileControl: '0', mobileSurface: '0' },
  rounded: { control: '6px', surface: '10px', pill: '999px', mobileControl: '16px', mobileSurface: '20px' },
}

const DEFAULT_PANEL_SHAPE: PanelShapeStyle = 'square'

/**
 * 圆角风格的本机缓存键（命名沿用仓库既有的 `dd:<模块>:<名字>`）。
 *
 * 存在的唯一理由是首屏防闪形：panel-settings 是异步 fetch，首帧一定先按 :root 的
 * 直角画一遍，圆角用户会看到「先方后圆」的形状跳变，比闪色刺眼得多。
 * 这里只是一份加速缓存，服务端值一回来就以服务端为准。
 */
const PANEL_SHAPE_STORAGE_KEY = 'dd:appearance:shape'

const DEFAULT_LOG_BACKGROUND_COLOR_LIGHT = '#f8fafc'
const DEFAULT_LOG_BACKGROUND_COLOR_DARK = '#0f172a'
// 编辑器底色的「留空默认值」跟随面板明暗主题：浅色模式白底深字，深色模式深底浅字。
// 注意：只有 editor_background_color 留空时才走这里；用户在系统设置里显式设过颜色一律以用户值为准。
const DEFAULT_EDITOR_BACKGROUND_COLOR_LIGHT = '#ffffff'
const DEFAULT_EDITOR_BACKGROUND_COLOR_DARK = '#111827'
const DEFAULT_EDITOR_FOREGROUND_COLOR = '#e5e7eb'
const DEFAULT_LOG_TEXT_COLOR_LIGHT = '#111827'
const DEFAULT_LOG_TEXT_COLOR_DARK = '#e2e8f0'

/**
 * 面板外观（编辑器/日志底色等）变更事件。
 * 代码编辑器的主题是在挂载时一次性构建的（CodeMirror 的 extensions 只在建 state 时读一次），
 * 切主题或改配置后必须靠这个事件重新构建主题并通过 Compartment 换上去，
 * 否则已挂载的编辑器不会跟着变色。
 */
export const PANEL_APPEARANCE_CHANGE_EVENT = 'dd:panel-appearance-change'

function toCSSImageValue(image?: string) {
  const trimmed = image?.trim() || ''
  if (!trimmed) {
    return 'none'
  }

  return `url("${trimmed.replace(/"/g, '\\"')}")`
}

function parseColor(color?: string) {
  const text = color?.trim() || ''
  if (!text) return null

  if (text.startsWith('#')) {
    const hex = text.slice(1)
    if (hex.length === 3) {
      const r = Number.parseInt(hex.charAt(0) + hex.charAt(0), 16)
      const g = Number.parseInt(hex.charAt(1) + hex.charAt(1), 16)
      const b = Number.parseInt(hex.charAt(2) + hex.charAt(2), 16)
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
    }
    if (hex.length === 6 || hex.length === 8) {
      const offset = hex.length === 8 ? 2 : 0
      const r = Number.parseInt(hex.slice(offset, offset + 2), 16)
      const g = Number.parseInt(hex.slice(offset + 2, offset + 4), 16)
      const b = Number.parseInt(hex.slice(offset + 4, offset + 6), 16)
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
    }
  }

  const match = text.match(/^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*[0-9.]+\s*)?\)$/i)
  if (!match) {
    return null
  }

  const r = Number.parseInt(match[1] ?? '', 10)
  const g = Number.parseInt(match[2] ?? '', 10)
  const b = Number.parseInt(match[3] ?? '', 10)
  return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
}

export function isDarkColor(background?: string) {
  const rgb = parseColor(background)
  if (!rgb) {
    // 解析不出来时按深色处理，与历史行为一致（前景色回退到浅字）
    return true
  }

  const toLinear = (channel: number) => {
    const value = channel / 255
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }

  const luminance = 0.2126 * toLinear(rgb.r) + 0.7152 * toLinear(rgb.g) + 0.0722 * toLinear(rgb.b)
  return luminance < 0.45
}

export function getReadableTextColor(background?: string) {
  return isDarkColor(background) ? DEFAULT_EDITOR_FOREGROUND_COLOR : DEFAULT_LOG_TEXT_COLOR_LIGHT
}

export function isPanelDarkMode() {
  return typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
}

export function getDefaultEditorBackgroundColor(isDark: boolean) {
  return isDark ? DEFAULT_EDITOR_BACKGROUND_COLOR_DARK : DEFAULT_EDITOR_BACKGROUND_COLOR_LIGHT
}

function getDefaultLogBackgroundColor(isDark: boolean) {
  return isDark ? DEFAULT_LOG_BACKGROUND_COLOR_DARK : DEFAULT_LOG_BACKGROUND_COLOR_LIGHT
}

function getDefaultLogTextColor(isDark: boolean) {
  return isDark ? DEFAULT_LOG_TEXT_COLOR_DARK : DEFAULT_LOG_TEXT_COLOR_LIGHT
}

// 服务端下发的值不可信（历史脏值、将来新增枚举值、大小写/空格），认不出来一律返回 null
// 交给调用方决定回退策略，而不是在这里直接兜成 square —— 见 lastAppliedShape 的说明。
function normalizePanelShape(raw?: string | null): PanelShapeStyle | null {
  const value = String(raw ?? '').trim().toLowerCase()
  if (value === 'rounded') return 'rounded'
  if (value === 'square') return 'square'
  return null
}

function readCachedPanelShape(): PanelShapeStyle {
  // 隐私模式 / 禁用站点存储时读 localStorage 会直接抛错，这里在模块初始化阶段执行，
  // 不兜住会让整个 main.ts 挂掉、页面全白
  try {
    if (typeof window === 'undefined') return DEFAULT_PANEL_SHAPE
    return normalizePanelShape(window.localStorage.getItem(PANEL_SHAPE_STORAGE_KEY)) ?? DEFAULT_PANEL_SHAPE
  } catch {
    return DEFAULT_PANEL_SHAPE
  }
}

// 最近一次显式取到的圆角风格。刻意与 lastAppliedSettings 分开记：
// 不是每次 applyPanelAppearance 都带着合法的 panel_shape_style —— panel-settings 拉取失败时传的是 null，
// 历史脏值 / 将来新增的枚举值也认不出来。跟着整份设置一起被覆盖的话，这些时候就会被拍回直角。
// ⇒ 只有本次真的带了合法值才更新，否则沿用上一次的结果。
// （v3.2.9 起 panel_shape_style 已并进设置页的 configForm、由「通用设置 → 面板外观」卡编辑，
//   设置页加载/保存时传进 applyPanelAppearance 的快照本身就带着已保存的值，见 useSettingsConfig.ts 的 appearanceSnapshot。）
let lastAppliedShape: PanelShapeStyle = readCachedPanelShape()

/**
 * 把圆角刻度写进 documentElement 的 `--dd-radius-*`（三档刻度 + 两条移动端刻度）。
 *
 * 传了合法值就以它为准并记进本机缓存；不传（或值不认识）就沿用上一次的结果。
 * 两个调用方：
 * 1. main.ts 在 createApp 之前不传参同步调一次（用 localStorage 缓存防首屏闪形）；
 * 2. applyPanelAppearance() 拿 panel_shape_style 调：数据源是免登录的 panel-settings，
 *    或设置页的外观快照（圆角只取已保存的值）—— 「面板外观」卡保存成功后 useSettingsConfig.ts 的
 *    saveConfigKeys() 会重跑 applyPanelAppearance，切换后即时生效，无需刷新页面。
 *
 * square 这一档是显式写 `0` 而不是 removeProperty()：同一次会话里可能先写过 rounded
 * （改配置、切主题都会重跑），显式写死才是幂等的；靠移除内联覆盖去露出 :root 的 0，
 * 还要额外保证 :root 那三条永远存在，反而更脆。
 */
export function applyPanelShapeStyle(raw?: string | null) {
  const next = normalizePanelShape(raw)
  if (next) {
    lastAppliedShape = next
    try {
      window.localStorage.setItem(PANEL_SHAPE_STORAGE_KEY, next)
    } catch {
      // 隐私模式 / 禁用站点存储：写不进去只是下次首屏会闪一下形状，不影响本次生效
    }
  }

  if (typeof document === 'undefined') return
  const radius = PANEL_SHAPE_RADIUS[lastAppliedShape]
  const root = document.documentElement
  root.style.setProperty('--dd-radius-control', radius.control)
  root.style.setProperty('--dd-radius-surface', radius.surface)
  root.style.setProperty('--dd-radius-pill', radius.pill)
  // 移动端刻度必须和三档刻度在同一个函数里一起写：main.ts 首屏预热调用的就是这里，
  // 漏写的话 rounded 面板在手机上会先按 :root 的 0 画一帧直角卡片，再等 panel-settings 回来才变圆。
  // 与三档一样，global.scss 的 :root 里这两条【绝不能带 !important】，否则这里的内联普通声明写不进去。
  root.style.setProperty('--dd-radius-control-mobile', radius.mobileControl)
  root.style.setProperty('--dd-radius-surface-mobile', radius.mobileSurface)
}

// 最近一次显式传入的外观设置。
// 主题切换只需要重新推导「留空默认值」，不应该把用户已保存的自定义颜色一起抹掉，
// 所以 applyPanelAppearance() 不传参时复用这里的缓存。
let lastAppliedSettings: PanelAppearanceSettings | null = null

export function applyPanelAppearance(settings?: PanelAppearanceSettings | null) {
  if (typeof settings !== 'undefined') {
    lastAppliedSettings = settings
  }

  const effective = lastAppliedSettings
  const root = document.documentElement
  const isDark = root.classList.contains('dark')
  const editorBackground =
    effective?.editor_background_color?.trim() || getDefaultEditorBackgroundColor(isDark)
  const logBackground = effective?.log_background_color?.trim() || getDefaultLogBackgroundColor(isDark)
  root.style.setProperty('--dd-editor-bg-color', editorBackground)
  root.style.setProperty('--dd-editor-fg-color', getReadableTextColor(editorBackground))
  root.style.setProperty('--dd-log-bg-color', logBackground)
  root.style.setProperty('--dd-log-text-color', getReadableTextColor(logBackground) || getDefaultLogTextColor(isDark))
  root.style.setProperty('--dd-log-theme-mode', isDark ? 'dark' : 'light')
  root.style.setProperty('--dd-log-bg-image', toCSSImageValue(effective?.log_background_image))

  // 圆角刻度：本次带了合法值就更新，没带就沿用上一次的（理由见 lastAppliedShape）
  applyPanelShapeStyle(effective?.panel_shape_style)

  // CSS 变量已经写完，通知已挂载的代码编辑器实例重新构建并换上主题
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new CustomEvent(PANEL_APPEARANCE_CHANGE_EVENT))
  }
}

export async function fetchAndApplyPanelAppearance() {
  try {
    const settings = await loadPanelSettings()
    applyPanelAppearance(settings || null)
  } catch {
    // ignore startup appearance load failures
  }
}

/**
 * 界面动效偏好（个人设置页「界面动效」）。
 *
 * - system（默认）：<html> 上什么都不挂，跟随系统的 prefers-reduced-motion；
 * - always：挂 dd-motion-force，系统开着「减少动态效果」也照常播放动画；
 * - reduce：挂 dd-motion-off，不看系统，一律把动效压到几乎不可见。
 * 两个 class 的实际作用写在 styles/global.scss 文件末尾「减少动效」那一段。
 *
 * 刻意做成本机偏好（localStorage），不进服务端的 system_configs：
 * 它回答的是「这台设备前的人想不想看动画」，和 panel_shape_style 那种「整个面板长什么样」不是一类；
 * 放进系统配置还会让 APP 的 schema 驱动设置页多出一个点了没用的开关。
 * 默认必须是 system：「始终开启」会覆盖系统的无障碍偏好，只能由用户自己选。
 */
export type MotionPreference = 'system' | 'always' | 'reduce'

// 命名沿用 dd:appearance:* 这一组（与圆角缓存键 dd:appearance:shape 同级）
const MOTION_PREFERENCE_STORAGE_KEY = 'dd:appearance:motion'
const MOTION_FORCE_CLASS = 'dd-motion-force'
const MOTION_OFF_CLASS = 'dd-motion-off'

function normalizeMotionPreference(raw?: string | null): MotionPreference | null {
  const value = String(raw ?? '').trim().toLowerCase()
  if (value === 'system' || value === 'always' || value === 'reduce') return value
  return null
}

export function readMotionPreference(): MotionPreference {
  // 隐私模式 / 禁用站点存储时读 localStorage 会直接抛错；这里在 createApp 之前执行，不兜住会白屏
  try {
    if (typeof window === 'undefined') return 'system'
    return normalizeMotionPreference(window.localStorage.getItem(MOTION_PREFERENCE_STORAGE_KEY)) ?? 'system'
  } catch {
    return 'system'
  }
}

/**
 * JS 层判断「此刻要不要减少动效」，给 CSS 令牌管不到的地方用（跟随鼠标的位移、scrollTo 的 smooth 等）。
 *
 * 与 global.scss 里 dd-motion-force / dd-motion-off 两个 class 同一口径：
 *   始终开启 → false（系统开着「减少动态效果」也照常，否则全站动画都回来了、只有这里不动）；
 *   减少动效 → true（不看系统）；
 *   跟随系统 → 看 prefers-reduced-motion。
 *
 * 每次调用都现读而不是缓存：用户可能在页面开着的时候改系统设置，
 * 两个判断都很便宜（localStorage 与 matchMedia 浏览器内部都有缓存），不值得为它挂 change 监听。
 */
export function shouldReduceMotion(): boolean {
  const preference = readMotionPreference()
  if (preference === 'always') return false
  if (preference === 'reduce') return true
  if (typeof window === 'undefined') return false
  return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}

/**
 * 把动效偏好挂到 <html> 上，返回最终生效的档位。
 *
 * 传了合法值：以它为准并写进本机缓存（个人设置页切换时走这条，即时生效、无需刷新）；
 * 不传或值不认识：按本机缓存重放一遍（main.ts 在 createApp 之前走这条）。
 *
 * ⚠️ main.ts 里这一次必须在首帧之前同步执行：挂晚了，「始终开启」的用户首屏动画会先被系统设置
 *    压成 1ms，「减少动效」的用户则会先看到一遍完整动画 —— 与 applyPanelShapeStyle 防闪形同理。
 */
export function applyMotionPreference(raw?: string | null): MotionPreference {
  const explicit = normalizeMotionPreference(raw)
  const next = explicit ?? readMotionPreference()
  if (explicit) {
    try {
      window.localStorage.setItem(MOTION_PREFERENCE_STORAGE_KEY, explicit)
    } catch {
      // 写不进去（隐私模式）只是下次打开要重新选，不影响本次生效
    }
  }

  if (typeof document === 'undefined') return next
  const classList = document.documentElement.classList
  classList.toggle(MOTION_FORCE_CLASS, next === 'always')
  classList.toggle(MOTION_OFF_CLASS, next === 'reduce')
  return next
}

/**
 * 面板主题档位（个人设置页「界面主题」，v3.3.2 / issue #145）。
 *
 * - light：显式明亮，不看系统；
 * - dark：显式暗夜，不看系统；
 * - system（缺键默认）：跟随操作系统的 prefers-color-scheme，系统日落转暗面板跟着转暗。
 *
 * 生效机制是给 <html> 加/去 class `dark`（不是 data-theme）：
 * element-plus/theme-chalk/dark/css-vars.css 与 global.scss、各页 <style> 里那些 `html.dark`
 * 段认的都是这一个 class，换成别的写法等于整套暗色样式全部失配。
 *
 * 与界面动效同理，刻意做成本机偏好（localStorage）、不进服务端：
 * 它回答的是「这块屏幕前的人现在想看亮的还是暗的」，笔记本和手机本就该各存各的；
 * 而且登录页也要能切主题，一旦挪进用户偏好表，未登录时根本读不到。
 *
 * ⚠️ 存储键沿用历史的裸 `theme`，不要改成 `dd:appearance:theme`：
 *    v3.3.2 之前的二态主题就写在这个键上，改键名等于把所有老用户的暗色偏好清零。
 * ⚠️ 缺键默认是 'system' 而不是 'light'：「跟随系统」这个功能要开箱即用，
 *    不能等用户先进一次设置页才生效。代价是「从没手动切过主题 + 系统是深色」的老用户
 *    升级后会被动变暗一次，这一点在发布说明里写明即可。
 */
export type ThemeMode = 'light' | 'dark' | 'system'

const THEME_MODE_STORAGE_KEY = 'theme'

function normalizeThemeMode(raw?: string | null): ThemeMode | null {
  const value = String(raw ?? '').trim().toLowerCase()
  if (value === 'light' || value === 'dark' || value === 'system') return value
  return null
}

/**
 * 系统此刻是不是深色。
 * 老 WebView 可能没有 matchMedia，可选链兜底成「不是深色」—— 拿不到就当系统永远是亮的，
 * 比整个启动流程抛异常白屏强。
 */
export function systemPrefersDark(): boolean {
  if (typeof window === 'undefined') return false
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false
}

export function readThemeMode(): ThemeMode {
  // 隐私模式 / 禁用站点存储时读 localStorage 会直接抛错；这里在 createApp 之前执行，不兜住会白屏
  try {
    if (typeof window === 'undefined') return 'system'
    return normalizeThemeMode(window.localStorage.getItem(THEME_MODE_STORAGE_KEY)) ?? 'system'
  } catch {
    return 'system'
  }
}

/**
 * 把主题档位落到 <html> 的 class `dark` 上，返回最终生效的档位。
 *
 * 传了合法值：以它为准并写进本机缓存（个人设置页、顶栏按钮切换时走这条，即时生效、无需刷新）；
 * 不传或值不认识：按本机缓存重放一遍（main.ts 在 createApp 之前走这条）。
 *
 * ⚠️ main.ts 里这一次必须在首帧之前同步执行：挂晚了，暗色（含跟随系统落到暗色）的用户
 *    首屏会先闪一帧白底 —— 与 applyPanelShapeStyle 防闪形、applyMotionPreference 同理。
 * ⚠️ 这里【只】管 class 与持久化，不碰编辑器/日志底色：那一发是 applyPanelAppearance()，
 *    由 stores/theme.ts 的 watch(isDark) 触发，切明暗时两者缺一不可。
 */
export function applyThemeMode(raw?: string | null): ThemeMode {
  const explicit = normalizeThemeMode(raw)
  const next = explicit ?? readThemeMode()
  if (explicit) {
    try {
      window.localStorage.setItem(THEME_MODE_STORAGE_KEY, explicit)
    } catch {
      // 写不进去（隐私模式）只是下次打开要重新选，不影响本次生效
    }
  }

  if (typeof document === 'undefined') return next
  // 跟随系统档在这里现读一次 matchMedia：首屏预热时 store 还没建起来，没别人能替它算这一下
  document.documentElement.classList.toggle(
    'dark',
    next === 'dark' || (next === 'system' && systemPrefersDark()),
  )
  return next
}
