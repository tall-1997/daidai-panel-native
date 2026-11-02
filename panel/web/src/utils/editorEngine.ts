import { TABLET_BREAKPOINT } from '@/composables/useResponsive'

/**
 * 代码编辑器引擎偏好。
 *
 * 面板同时带着两个编辑器引擎：
 *   - CodeMirror 6：默认引擎，也是**移动端唯一**引擎。内容区是 contenteditable、选区就是原生
 *     Selection，所以「长按出系统菜单 / 双击出选择手柄 / 按住拖动光标」三条天然可用。
 *   - Monaco：桌面端可选。多光标、代码折叠、JSON 语言服务这类重型能力更强，
 *     但它是自绘编辑器，触摸事件由内部手势层接管，上面那三条**无论怎么配都做不到**。
 *
 * 所以这里不是「随便二选一」：auto 的解析规则里，触摸设备与窄屏是**硬回落**到 CodeMirror 的，
 * 见 resolveEditorEngine。
 *
 * 存储范式（键名、只认显式值、typeof window 前置分支 + try/catch）与
 * components/CodeEditor.vue 顶部那组自动换行/视图开关保持一致，不要另发明一套。
 * 单独放 utils 而不是塞进组件文件：切换引擎的入口在页面的齿轮下拉里，
 * 而读取方要在**两个**编辑器组件里都用到，塞进任一个组件都会绕成循环依赖。
 */

/** 用户在菜单里选的值。auto 表示「按设备自动挑」。 */
export type EditorEngine = 'auto' | 'codemirror' | 'monaco'

/** auto 解析之后的实际引擎，组件只吃这个。 */
export type ResolvedEditorEngine = 'codemirror' | 'monaco'

/**
 * 引擎偏好变更事件。
 *
 * persistEditorEngine 写完 localStorage 必须派发它，已挂载的编辑器组件监听它来重建实例。
 * 少了这一步的表现是「菜单里切了、后面开着的那个编辑器纹丝不动」——
 * 不报错，构建与类型检查全绿，只有人肉点一遍才发现。
 * 范式对齐 utils/panelAppearance.ts 的 PANEL_APPEARANCE_CHANGE_EVENT。
 */
export const EDITOR_ENGINE_CHANGE_EVENT = 'dd:editor-engine-change'

const EDITOR_ENGINE_STORAGE_KEY = 'dd:editor:engine'

export function readStoredEditorEngine(): EditorEngine {
  if (typeof window === 'undefined') {
    return 'auto'
  }
  try {
    // 只认写死的两个显式值，其余（没写过、被人改成脏值、将来删掉的旧引擎名、读不到）
    // 一律回落 'auto'。这样「用户从没碰过这个开关」和「存储被清掉了」表现一致。
    const raw = window.localStorage.getItem(EDITOR_ENGINE_STORAGE_KEY)
    if (raw === 'codemirror') return 'codemirror'
    if (raw === 'monaco') return 'monaco'
    return 'auto'
  } catch {
    // 隐私模式 / 禁用站点存储时 getItem 会抛错，不能让它把调用方的 setup 整块炸掉
    return 'auto'
  }
}

export function persistEditorEngine(value: EditorEngine): void {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(EDITOR_ENGINE_STORAGE_KEY, value)
  } catch {
    // 写失败只是这次记不住，不影响本次切换效果，静默忽略
  }
  // ⚠️ 派发放在 try/catch **之外**：存储写不进去（隐私模式）时，本次切换仍然必须生效，
  // 否则用户在无痕窗口里点了引擎切换会毫无反应。
  window.dispatchEvent(new Event(EDITOR_ENGINE_CHANGE_EVENT))
}

/**
 * 把用户偏好解析成实际引擎。
 *
 * 判定顺序（逐条都是短路返回）：
 *   1. 显式选过就照办，不再做任何设备判断——用户比我们更清楚他自己在什么设备上；
 *   2. 没有 window（SSR / 单测环境）→ CodeMirror，它是无条件能用的那个；
 *   3. 指针是粗指针（触摸）→ CodeMirror（硬回落，理由见文件头）；
 *   4. 视口宽度不超过平板断点 → CodeMirror；
 *   5. 其余（桌面 + 精确指针）→ Monaco。
 */
export function resolveEditorEngine(pref: EditorEngine): ResolvedEditorEngine {
  if (pref !== 'auto') {
    return pref
  }

  if (typeof window === 'undefined') {
    return 'codemirror'
  }

  // 先看指针类型再看宽度，顺序不能反：只看宽度会漏掉 1024~1366 的平板横屏和触屏笔电，
  // 它们宽得像桌面、却是手指在操作，把 Monaco 发过去等于把「长按菜单 / 选择手柄 / 拖动光标」
  // 这三条能力当场收回去，而这正是 v3.2.0 换引擎的全部理由。
  // matchMedia 用可选调用：老 WebView 里可能没有，没有就当它不是触摸设备继续往下走。
  if (window.matchMedia?.('(pointer: coarse)').matches) {
    return 'codemirror'
  }

  // 阈值从 useResponsive 借用，不在这里重抄一个 1024：抄一份就等于多了一个真源，
  // 将来调断点只改一边的表现是「侧栏已经收成移动版了，编辑器还在给 Monaco」。
  //
  // 刻意用 window.innerWidth 而不是 useResponsive() 返回的响应式 width：
  // 响应式解析会让「桌面窗口从 1100 拖到 900」当场拆掉 Monaco 重建 CodeMirror，
  // 撤销历史 / 光标 / 滚动位置全丢，而用户只是拖了下窗口。auto 只在挂载时解析一次。
  if (window.innerWidth <= TABLET_BREAKPOINT) {
    return 'codemirror'
  }

  return 'monaco'
}
