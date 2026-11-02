import {
  defineComponent,
  h,
  inject,
  type Component,
  type CSSProperties,
  type InjectionKey,
  type PropType,
} from 'vue'
// 渲染函数里用的 EP 组件不经过 unplugin-vue-components（它只改写模板里的组件），样式要自己引。
// ⚠️ 但不能在本文件里引：本文件在首屏 chunk 里，这里一句 import 'element-plus/theme-chalk/el-button.css'
//    会把整份按钮样式（约 20 KB 未压缩）搬进 index.html 链着的首屏 CSS，登录页也得先下完它才渲染
//    （09-12 的 dist 里它是独立的懒加载 el-button-*.css，首屏 CSS 里只有 global.scss 自己那几条 .el-button 覆盖）。
//    所以由真正挂这几个占位组件的两个分发层（CodeEditor.vue / CodeDiffEditor.vue，都在懒加载 chunk 里）各引一次；
//    MonacoEditor / MonacoDiffEditor 只经分发层挂载，覆盖层里的按钮跟着沾光。
//    ElButton 的 JS 本来就在首屏（main.ts 引入的 ElMessageBox 内部就用它），这里 import 不增加体积。
import { ElButton } from 'element-plus'
import { isChunkLoadError } from '@/utils/chunkReload'
import {
  EDITOR_ENGINE_CHANGE_EVENT,
  persistEditorEngine,
  readStoredEditorEngine,
  resolveEditorEngine,
} from '@/utils/editorEngine'

/**
 * Monaco 的「什么时候提前拉」与「拉的时候 / 拉失败时编辑区显示什么」（issue #133 功能 2、#126）。
 *
 * 🔴 本文件在首屏 chunk 里（router/index.ts 静态引入它），所以它碰 Monaco 只能是动态 import：
 *    写一条 `import ... from '@/utils/monacoEngine'`（哪怕只是漏了 import type 的 type），
 *    monaco chunk 就会被 Rollup 提升成入口的静态依赖、进 index.html 的 modulepreload ——
 *    构建全绿、页面能用，纯静默劣化。checks.yml 的「Monaco 已打包且未进首屏」断言守的就是这件事。
 *
 * 一、预热
 *   v3.2.0 以前脚本页挂载时预加载过 Monaco，换 ESM 时一起删了；之后第一次进编辑页要现下现解析约 4 MB
 *   （未压缩），二进制 / Magisk 部署由 Go 直出、此前既没有缓存头也没有压缩，最长能白屏 4 秒。
 *   三个时机：
 *     - 空闲预热：router 的 preloadPanelRoutes 把菜单页 chunk 拉完之后再排一个空闲回调；
 *       只在那批菜单里有编辑器页（脚本管理 / 配置文件）时才排 —— viewer 进不去这两页，替他预热纯属白下。
 *       演示站关闭（每个访客都白下几 MB 不值得），只留悬停预热。
 *     - 悬停预热：router 的 preloadRouteByPath 命中编辑器页（菜单 hover / focus / 点击都会走到）。
 *     - 切到 Monaco 时立即预热：用户刚在齿轮菜单里选了它，意图最明确（MainLayout 挂监听）。
 *   门槛见 shouldWarmMonaco。触屏与窄屏已被 resolveEditorEngine 硬回落 CodeMirror，手机端天然不预热。
 *   ⚠️ 预热只决定**何时开始**：Monaco 模块的解析执行是一段拆不开的主线程长任务，空闲回调管不了它跑多久。
 *
 * 二、加载态 / 失败态占位组件
 *   CodeEditor / CodeDiffEditor 的 defineAsyncComponent 用它们当 loadingComponent / errorComponent；
 *   MonacoEditor / MonacoDiffEditor 在 monacoEngine 动态 import 失败时盖一层同样的失败态。
 *   四处共用这一份，文案与按钮行为只有一个真源。
 */

// ---------------------------------------------------------------------------
// 预热
// ---------------------------------------------------------------------------

/** 编辑器页的路由路径。改路由路径时这里要一起改，漏了的表现是悬停 / 空闲预热静默失效。 */
const MONACO_EDITOR_ROUTES: ReadonlySet<string> = new Set(['/scripts', '/config-file'])

const SLOW_EFFECTIVE_TYPES: ReadonlySet<string> = new Set(['slow-2g', '2g', '3g'])

const IDLE_WARMUP_TIMEOUT_MS = 5000

// 没有 requestIdleCallback 的浏览器（Safari）用定时器兜底。比 router 那边的 500ms 长得多：
// 这一步是几 MB 的下载外加一段长任务，宁可晚一点，别跟首屏还没收尾的请求挤。
const IDLE_WARMUP_FALLBACK_DELAY_MS = 3000

// connection / deviceMemory 只有 Chromium 有，lib.dom 里也没有这两个成员，只能自己补形状。
// 其它浏览器上读出来是 undefined，对应的两条门槛自动放行，只剩引擎判断。
type NavigatorHints = Navigator & {
  connection?: { saveData?: boolean; effectiveType?: string }
  deviceMemory?: number
}

export function isMonacoEditorRoute(path: string): boolean {
  return MONACO_EDITOR_ROUTES.has(path)
}

/**
 * 该不该替当前用户预热。全部满足才返回 true：
 * 当前引擎解析为 monaco、没开省流量、网络不是 2g/3g、设备内存 > 2GB、页面可见。
 */
export function shouldWarmMonaco(): boolean {
  if (typeof window === 'undefined' || typeof document === 'undefined') return false
  if (resolveEditorEngine(readStoredEditorEngine()) !== 'monaco') return false
  if (document.visibilityState !== 'visible') return false
  const { connection, deviceMemory } = navigator as NavigatorHints
  if (connection?.saveData) return false
  if (connection?.effectiveType && SLOW_EFFECTIVE_TYPES.has(connection.effectiveType)) return false
  return (deviceMemory ?? 8) > 2
}

let warmupPromise: Promise<void> | null = null

/**
 * 并行拉 MonacoEditor.vue 与 monacoEngine（与 CodeEditor 分发层的 loader 拉的是同一对模块，
 * 真打开编辑器时两者都已在模块表里，当场 resolve）。记忆化：反复悬停不会重复下载。
 */
export function warmMonaco(): Promise<void> {
  if (!warmupPromise) {
    warmupPromise = Promise.all([
      import('@/components/MonacoEditor.vue'),
      import('@/utils/monacoEngine'),
    ]).then(
      () => undefined,
      (error: unknown) => {
        // 清掉记忆，下次悬停 / 真打开编辑器时还能再试。只打 warn，与路由预加载失败的约定一致：
        // 预热是锦上添花，失败不该打扰用户（真打开编辑器时分发层有自己的加载失败界面）。
        warmupPromise = null
        console.warn('Monaco 预热失败，将在打开编辑器时重试', error)
      },
    )
  }
  return warmupPromise
}

let idleWarmupScheduled = false

/** 空闲预热。由 router 的 preloadPanelRoutes 在菜单页 chunk 全部落地后调；整个页面生命周期只排一次。 */
export function scheduleIdleMonacoWarmup(): void {
  // 编译期常量守卫：演示站构建里整段变成 return，只留悬停预热
  if (import.meta.env.VITE_DEMO === '1') return
  if (idleWarmupScheduled || typeof window === 'undefined') return
  idleWarmupScheduled = true
  runWhenIdle(runIdleWarmup)
}

function runIdleWarmup() {
  if (document.visibilityState !== 'visible') {
    // 刚登录就切走了标签页：等它回到前台再排一次空闲回调，不在后台偷偷下几 MB
    const resume = () => {
      if (document.visibilityState !== 'visible') return
      document.removeEventListener('visibilitychange', resume)
      runWhenIdle(runIdleWarmup)
    }
    document.addEventListener('visibilitychange', resume)
    return
  }
  if (shouldWarmMonaco()) void warmMonaco()
}

function runWhenIdle(callback: () => void) {
  const idleWindow = window as Window & {
    requestIdleCallback?: (callback: () => void, options?: { timeout: number }) => number
  }
  if (typeof idleWindow.requestIdleCallback === 'function') {
    idleWindow.requestIdleCallback(() => callback(), { timeout: IDLE_WARMUP_TIMEOUT_MS })
    return
  }
  window.setTimeout(callback, IDLE_WARMUP_FALLBACK_DELAY_MS)
}

/**
 * 切到 Monaco 引擎时立即预热。返回摘除监听的函数（MainLayout 卸载时调）。
 */
export function warmMonacoOnEngineSwitch(): () => void {
  if (typeof window === 'undefined') return () => {}
  const onEngineChange = () => {
    // 用户刚亲手选的，不再看网络 / 内存门槛（开着的编辑器反正马上就要加载它）；
    // 但仍按解析结果判：选的是 codemirror，或 auto 在触屏 / 窄屏上解析成 CodeMirror 时不拉。
    if (resolveEditorEngine(readStoredEditorEngine()) === 'monaco') void warmMonaco()
  }
  window.addEventListener(EDITOR_ENGINE_CHANGE_EVENT, onEngineChange)
  return () => window.removeEventListener(EDITOR_ENGINE_CHANGE_EVENT, onEngineChange)
}

// ---------------------------------------------------------------------------
// 加载态 / 失败态占位组件
// ---------------------------------------------------------------------------

/** 加载占位延迟多久才露出来（Vue 异步组件的 delay）。有缓存时编辑器直接出来，不闪一下占位。 */
export const MONACO_LOADING_DELAY_MS = 200

/**
 * 分发层把「重试」交给 errorComponent 的通道。
 * Vue 创建 errorComponent 时只给它一个 error prop（runtime-core 的 defineAsyncComponent），
 * 够不着分发层里的函数，所以 CodeEditor / CodeDiffEditor 用 provide 把重试递下来。
 */
export const MONACO_LOAD_RETRY_KEY: InjectionKey<() => void> = Symbol('dd:monaco-load-retry')

/**
 * 占位的三种摆法：
 *   - editor：替 CodeEditor 占 MonacoEditor 的位置，尺寸规则与 .code-editor-wrapper 一致；
 *   - diff：替 CodeDiffEditor 占 MonacoDiffEditor 的位置，尺寸与 .code-diff-wrapper 一致；
 *   - overlay：盖在已经挂上去的 Monaco 组件自己的编辑区上（monacoEngine 加载失败时）。
 */
type MonacoStateLayout = 'editor' | 'diff' | 'overlay'

// 渲染函数里只用到 size / type / onClick；宽化成 Component，免得 h() 在 EP 的 props 类型上走偏重载
const Button = ElButton as Component

// 与编辑器同一条底色 / 前景色变量：占位与随后挂上来的编辑器同色，换过去时不闪一块别的颜色。
// 回退值只在 panelAppearance 还没写入这两个变量之前兜底。
const surfaceStyle: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  alignItems: 'center',
  justifyContent: 'center',
  gap: '10px',
  boxSizing: 'border-box',
  padding: '24px',
  overflow: 'hidden',
  textAlign: 'center',
  fontSize: '13px',
  lineHeight: 1.6,
  background: 'var(--dd-editor-bg-color, var(--el-fill-color-light))',
  color: 'var(--dd-editor-fg-color, var(--el-text-color-regular))',
}

const titleStyle: CSSProperties = { fontSize: '14px', fontWeight: 600 }

const summaryStyle: CSSProperties = { maxWidth: '440px', opacity: 0.8 }

const detailStyle: CSSProperties = {
  maxWidth: '440px',
  fontFamily: 'var(--dd-font-mono)',
  fontSize: '12px',
  opacity: 0.6,
  wordBreak: 'break-all',
  // 浏览器的报错里带着完整的 chunk URL，可能很长：最多露两行，全文放在 title 里
  display: '-webkit-box',
  WebkitBoxOrient: 'vertical',
  WebkitLineClamp: 2,
  overflow: 'hidden',
}

const actionsStyle: CSSProperties = {
  display: 'flex',
  flexWrap: 'wrap',
  justifyContent: 'center',
  gap: '8px',
  marginTop: '4px',
}

// 与 MonacoEditor.vue / CodeMirrorEditor.vue 的 resolveMinHeight 同一套规则：
// 占位与编辑器吃同样的 minHeight / fillHeight 就占同样大的地方，编辑器挂上来时不跳。
function resolveEditorMinHeight(value: string | number | undefined, fillHeight: boolean) {
  if (typeof value === 'number') return `${value}px`
  if (typeof value === 'string' && value.trim()) return value
  return fillHeight ? '0px' : '400px'
}

function resolveBoxStyle(
  layout: MonacoStateLayout,
  minHeight: string | number | undefined,
  fillHeight: boolean,
): CSSProperties {
  if (layout === 'overlay') {
    // 宿主（.code-editor-wrapper / .code-diff-wrapper）都是 position: relative
    return { position: 'absolute', inset: 0, zIndex: 1 }
  }
  if (layout === 'diff') {
    return { width: '100%', height: '100%', minHeight: '420px' }
  }
  const resolved = resolveEditorMinHeight(minHeight, fillHeight)
  return fillHeight
    ? { width: '100%', flex: '1 1 auto', height: '100%', minHeight: resolved }
    : { width: '100%', height: resolved, minHeight: resolved }
}

/**
 * 分发层的 props 会被原样交给占位组件：loadingComponent 走 Vue 的 createInnerComp（连 props 带模板 ref），
 * errorComponent 经异步包装层的属性透传拿到同一批。这里只声明决定占位尺寸的两个，
 * 其余全靠 inheritAttrs: false 吞掉 —— 否则 modelValue（整份脚本正文）会被当成 DOM 属性写到占位 div 上。
 * class / style 是调用点给编辑器根节点的布局约束（ScriptsEditorPane 的 class="code-editor"、
 * 调试弹窗的内联高度），手动挂回占位根节点，并让内联 style 排在最后、压过占位自己的尺寸。
 */
const boxProps = {
  minHeight: { type: [String, Number] as PropType<string | number> },
  fillHeight: { type: Boolean, default: false },
}

function defineLoadingState(layout: 'editor' | 'diff') {
  return defineComponent({
    name: 'MonacoLoadingState',
    inheritAttrs: false,
    props: boxProps,
    setup(props, { attrs }) {
      return () =>
        h(
          'div',
          {
            class: attrs.class,
            style: [surfaceStyle, resolveBoxStyle(layout, props.minHeight, props.fillHeight), attrs.style],
            role: 'status',
            'aria-live': 'polite',
          },
          '编辑器加载中…',
        )
    },
  })
}

function describeLoadError(error: unknown) {
  const detail = error instanceof Error ? error.message : typeof error === 'string' ? error : ''
  const summary = isChunkLoadError(error)
    ? '编辑器文件没能下载下来：网络中断，或面板刚升级过而这个页面还是旧版本。刷新一次页面通常就能恢复。'
    : '编辑器初始化时出错了。可以先重试，或者改用 CodeMirror 继续编辑。'
  return { summary, detail }
}

function defineErrorState(layout: MonacoStateLayout) {
  return defineComponent({
    name: 'MonacoLoadErrorState',
    inheritAttrs: false,
    props: {
      ...boxProps,
      // Vue 的 InferPropType 遇到 PropType<unknown> 会改取 default 的类型；default 也标成 unknown，
      // 否则 prop 被推断成 null，调用点传 unknown 的 loadError 过不了类型检查。运行时默认值仍是 null。
      error: { type: null as unknown as PropType<unknown>, default: null as unknown },
      /** 覆盖层版本由 Monaco 组件传自己的重试；分发层的 errorComponent 拿不到 props，走 inject */
      retry: { type: Function as PropType<() => void> },
    },
    setup(props, { attrs }) {
      const injectedRetry = inject(MONACO_LOAD_RETRY_KEY, null)
      const handleRetry = () => {
        const retry = props.retry ?? injectedRetry
        retry?.()
      }
      // 走现有的引擎偏好链路：persistEditorEngine 写存储并派发 EDITOR_ENGINE_CHANGE_EVENT，
      // 分发层监听它重新解析、换成 CodeMirror 实现，本占位随之卸载。
      const handleUseCodeMirror = () => {
        persistEditorEngine('codemirror')
      }
      return () => {
        const { summary, detail } = describeLoadError(props.error)
        const canRetry = Boolean(props.retry ?? injectedRetry)
        return h(
          'div',
          {
            class: attrs.class,
            style: [surfaceStyle, resolveBoxStyle(layout, props.minHeight, props.fillHeight), attrs.style],
            role: 'alert',
          },
          [
            h('div', { style: titleStyle }, 'Monaco 编辑器加载失败'),
            h('div', { style: summaryStyle }, summary),
            detail ? h('div', { style: detailStyle, title: detail }, detail) : null,
            h('div', { style: actionsStyle }, [
              canRetry
                ? h(Button, { size: 'small', type: 'primary', onClick: handleRetry }, () => '重试')
                : null,
              h(Button, { size: 'small', onClick: handleUseCodeMirror }, () => '改用 CodeMirror'),
            ]),
          ],
        )
      }
    },
  })
}

/** CodeEditor 的 loadingComponent / errorComponent */
export const MonacoEditorLoading = defineLoadingState('editor')
export const MonacoEditorLoadError = defineErrorState('editor')

/** CodeDiffEditor 的 loadingComponent / errorComponent */
export const MonacoDiffLoading = defineLoadingState('diff')
export const MonacoDiffLoadError = defineErrorState('diff')

/** MonacoEditor / MonacoDiffEditor 在 monacoEngine 加载失败时盖在自己编辑区上的那一层 */
export const MonacoLoadErrorOverlay = defineErrorState('overlay')
