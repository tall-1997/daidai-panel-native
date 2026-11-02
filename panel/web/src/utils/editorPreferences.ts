import { authApi } from '@/api/auth'

/**
 * 代码编辑器的「个人偏好」。
 *
 * 这一组东西原本散在 components/CodeEditor.vue 顶部（6 个具名导出，纯 localStorage）。
 * v3.2.4 起收敛到这里，原因是 issue #116 要求「换设备 / 换 IP 也记得住」——
 * 这就带来一次网络请求，而组件文件不是发请求的地方。
 *
 * 存储模型是**两层**：
 *   - localStorage 是首屏 / 离线 / 隐私模式下的缓存，读取永远是同步的（编辑器要立刻渲染）；
 *   - 服务端 `/api/auth/preferences` 是真源，`ensureEditorPreferencesLoaded()` 拉回来之后
 *     覆盖本地缓存并派发一次变更事件。
 * 这套「服务端为真源 + localStorage 只当加速缓存」的范式抄的是 utils/panelAppearance.ts
 * 里 panel_shape_style 那条链路，不要另发明一套。
 *
 * 「服务端为真源」只有一个例外：**服务端还没存过这个用户的偏好时**（响应里 stored 不为 true），
 * 同步方向是反的 —— 把本机这份上行迁移过去，而不是拿服务端下发的那套默认值覆盖本地。
 * 老用户升级到 v3.2.4 的那一刻，服务端对他们全都是「没存过」，照着覆盖等于把他们在
 * v3.2.2/3.2.3 调过的开关一次性抹平。详见 ensureEditorPreferencesLoaded。
 *
 * ⚠️ 引擎偏好（utils/editorEngine.ts 的 `dd:editor:engine`）**刻意不在这里、也不同步到服务端**：
 * 它带着 `auto` 的设备自适应语义（触摸设备与窄屏硬回落 CodeMirror），
 * 把桌面上显式选的 monaco 同步到手机，等于把手机用户锁死在一个用不了
 * 「长按菜单 / 选择手柄 / 拖动光标」的引擎上。引擎跟设备走，偏好跟人走。
 */

/** 自动换行。 */
export type EditorWordWrap = 'on' | 'off'

/**
 * 空白符显示模式（issue #116-4）。
 *   - selection：只有被选中的空白才画小圆点 / 箭头（Monaco 的原生默认，也是本项的默认值）
 *   - all：一直画
 *   - none：从不画
 */
export type EditorWhitespaceMode = 'none' | 'selection' | 'all'

/**
 * 缩进宽度（issue #116-1/3）。
 * 'auto' 表示按文件内容检测（见 utils/codeEditor.ts 的 detectIndentWidth）。
 */
export type EditorIndentWidth = 'auto' | 2 | 4 | 6 | 8

export interface EditorPreferences {
  word_wrap: EditorWordWrap
  minimap: boolean
  indent_guides: boolean
  whitespace: EditorWhitespaceMode
  indent_width: EditorIndentWidth
}

/**
 * 默认值刻意各不相同，判据是「打开它会不会打扰到不需要它的人」：
 *   - 自动换行：默认开，与 Monaco 时代逐字相同，换引擎不该让老用户观感变一次。
 *   - 缩进参考线：默认开。它就是为了让缩进层级一眼看清，噪声极低。
 *   - 缩略图：默认关。它在 Monaco 上要占正文右侧一条，窄屏尤其肉疼。
 *   - 空白符：默认「仅选中」。v3.2.2 时这一项默认是「关」，而 #116 的报告者说
 *     他要的从头到尾就是「选中的空白符用小圆点显示」，这也是 Monaco/VS Code 的原生默认。
 *   - 缩进宽度：默认「自动检测」。固定 2 会把 4 空格缩进的 Python 脚本画成每 2 列一条线。
 *
 * ⚠️ 服务端 server/handler/user_preference.go 里有一份逐字相同的默认值，改这里必须同时改那里，
 * 否则「从没存过偏好的用户」在拉到服务端值的瞬间观感会跳一下。
 */
export const EDITOR_PREFERENCES_DEFAULTS: Readonly<EditorPreferences> = Object.freeze({
  word_wrap: 'on',
  minimap: false,
  indent_guides: true,
  whitespace: 'selection',
  indent_width: 'auto'
})

/**
 * 偏好变更事件。
 *
 * 写完偏好必须派发它：两个编辑器页面各自持有一份 ref，靠监听它来跟随
 * 「在另一个页面改的」「刚从服务端拉回来的」变更。
 * 少了它的表现是「设置改了、正开着的那个编辑器纹丝不动」——不报错、构建全绿。
 * 范式对齐 utils/editorEngine.ts 的 EDITOR_ENGINE_CHANGE_EVENT。
 */
export const EDITOR_PREFERENCES_CHANGE_EVENT = 'dd:editor-preferences-change'

/** 齿轮菜单里「缩进宽度」的循环顺序。 */
export const EDITOR_INDENT_WIDTH_OPTIONS: readonly EditorIndentWidth[] = ['auto', 2, 4, 6, 8]

/** 齿轮菜单里「显示空白符」的循环顺序。 */
export const EDITOR_WHITESPACE_MODES: readonly EditorWhitespaceMode[] = ['selection', 'all', 'none']

/**
 * localStorage 键沿用 v3.2.2 的老键，老用户升级后偏好平滑保留。
 * whitespace 从二态变三态，读的时候兼容老值（见 parseWhitespace）。
 * indent_width 是本版新增的键。
 *
 * ⚠️ 「平滑保留」还有一半在 ensureEditorPreferencesLoaded 里：光沿用老键不够，
 *    还得保证首屏那次 GET 不会拿服务端默认值把这些老值就地改写掉，
 *    否则这里的兼容与 parseWhitespace 的老值映射都在同一次挂载里当场作废。
 */
const STORAGE_KEYS: Record<keyof EditorPreferences, string> = {
  word_wrap: 'dd:editor:word_wrap',
  minimap: 'dd:editor:minimap',
  indent_guides: 'dd:editor:indent_guides',
  whitespace: 'dd:editor:whitespace',
  indent_width: 'dd:editor:indent_width'
}

/**
 * 隐私模式 / 站点存储被禁用时 setItem 会直接抛错。这时本次切换仍然必须立刻生效，
 * 所以把写不进去的那一项在内存里留一份覆盖，读的时候以它为准。
 * 少了这一层的表现是：点一下开关，页面监听到变更事件回头读存储，读回来的还是旧值，
 * 开关当场弹回去 —— 看起来像点了没反应。
 */
const memoryOverrides: Partial<Record<keyof EditorPreferences, string>> = {}

function readRaw(key: keyof EditorPreferences): string | null {
  // 内存覆盖优先：它只在「存储写不进去」时才有值
  const override = memoryOverrides[key]
  if (override !== undefined) {
    return override
  }
  if (typeof window === 'undefined') {
    return null
  }
  try {
    return window.localStorage.getItem(STORAGE_KEYS[key])
  } catch {
    // 隐私模式 / 禁用站点存储时 getItem 会直接抛错，不能让它把调用方的 setup 整块炸掉
    return null
  }
}

function writeRaw(key: keyof EditorPreferences, value: string): void {
  if (typeof window === 'undefined') {
    return
  }
  try {
    window.localStorage.setItem(STORAGE_KEYS[key], value)
    delete memoryOverrides[key]
  } catch {
    memoryOverrides[key] = value
  }
}

function parseBool(raw: string | null, fallback: boolean): boolean {
  // 只认写死的 'on' / 'off'，其余（没写过、被人改成脏值、读不到）一律回落默认，
  // 这样「用户从没碰过这个开关」和「存储被清掉了」表现一致。
  if (raw === 'on') return true
  if (raw === 'off') return false
  return fallback
}

function parseWhitespace(raw: string | null): EditorWhitespaceMode {
  if (raw === 'none' || raw === 'selection' || raw === 'all') return raw
  // v3.2.2 时这一项是二态布尔，值写的是 'on' / 'off'。老用户浏览器里存的就是这两个，
  // 直接映射到新的三态上，不要让他们的选择因为升级而丢一次。
  if (raw === 'on') return 'all'
  if (raw === 'off') return 'none'
  return EDITOR_PREFERENCES_DEFAULTS.whitespace
}

function parseIndentWidth(raw: string | null): EditorIndentWidth {
  if (raw === 'auto') return 'auto'
  const parsed = Number(raw)
  if (parsed === 2 || parsed === 4 || parsed === 6 || parsed === 8) return parsed
  return EDITOR_PREFERENCES_DEFAULTS.indent_width
}

/** 把一项偏好序列化成 localStorage / 接口都用的那个字符串。 */
function serialize<K extends keyof EditorPreferences>(key: K, value: EditorPreferences[K]): string {
  if (key === 'minimap' || key === 'indent_guides') {
    return value ? 'on' : 'off'
  }
  return String(value)
}

/**
 * 同步读出当前偏好（走本地缓存）。
 *
 * 编辑器挂载时必须立刻拿到值，所以这里不等服务端；服务端值由
 * ensureEditorPreferencesLoaded() 异步拉回来之后，通过变更事件二次刷新。
 */
export function readEditorPreferences(): EditorPreferences {
  return {
    word_wrap: readRaw('word_wrap') === 'off' ? 'off' : 'on',
    minimap: parseBool(readRaw('minimap'), EDITOR_PREFERENCES_DEFAULTS.minimap),
    indent_guides: parseBool(readRaw('indent_guides'), EDITOR_PREFERENCES_DEFAULTS.indent_guides),
    whitespace: parseWhitespace(readRaw('whitespace')),
    indent_width: parseIndentWidth(readRaw('indent_width'))
  }
}

/**
 * 本次会话里被用户亲手改过的键。
 *
 * 它存在只为一件事：让「用户刚点的」赢过「更早发出的那次 GET」。
 * ensureEditorPreferencesLoaded 在 onMounted 里立刻发 GET，远程访问自建面板时
 * 一个来回三五百毫秒很常见；这段时间里用户点一下 Wrap，本地已经是 'off' 了，
 * 而那条**在点击之前**就发出去的 GET 带回来的还是服务端旧值 'on' ——
 * 照着写回去，按钮会自己弹回原位，此后本地 'on' / 服务端 'off' 一直不一致，
 * 而 setEditorPreference 的 catch 是空的，页面上一行报错都没有。
 *
 * ⚠️ 只在本次会话（本次页面加载）内有效，刻意不落存储：
 * 它记的是「这一次的 GET 已经过时了」，而不是用户的偏好本身。
 * 下一次刷新页面时服务端那份已经是最新的，再跳过写回反而会拦住跨设备同步。
 */
const locallyChanged = new Set<keyof EditorPreferences>()

/**
 * 改一项偏好：写本地缓存 → 派发变更事件 → 后台把这一项同步到服务端。
 *
 * 只提交改动的那一个键（服务端做字段级合并），这样两个标签页各改各的开关不会互相覆盖。
 * 同步失败刻意**不打扰用户**：本机已经生效了，弹一句「同步失败」除了制造焦虑没有别的作用；
 * 下一次改动会重试，真正需要排查时面板日志里有记录。
 */
export function setEditorPreference<K extends keyof EditorPreferences>(
  key: K,
  value: EditorPreferences[K]
): void {
  // 先记脏：这一键从此不再接受首屏那次 GET 的写回（理由见 locallyChanged 的注释）。
  // 放在最前面是因为下面 dispatchEvent 是同步的，监听方回头读存储时状态必须已经一致。
  locallyChanged.add(key)
  writeRaw(key, serialize(key, value))
  // ⚠️ 派发放在存储写入之后、网络请求之前，且不受两者成败影响：
  // 隐私模式写不进存储、或者服务端不可达时，本次切换仍然必须立刻生效。
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(EDITOR_PREFERENCES_CHANGE_EVENT))
  }
  // 落到接口上的值与 localStorage 里的不是一回事：两个开关在接口上是 **JSON 布尔**
  // （服务端下发时也是布尔），只有 localStorage 才为了兼容 v3.2.2 的老键继续写 'on'/'off'。
  const wire: string | boolean =
    key === 'minimap' || key === 'indent_guides' ? (value as boolean) : String(value)
  void authApi.updatePreferences({ [key]: wire }).catch(() => {
    // 未登录 / 离线 / 服务端报错都走这里，静默即可（理由见上）
  })
}

let loadPromise: Promise<void> | null = null

/**
 * 把本机这 5 项一次性推到服务端（首次上行迁移）。
 *
 * 只在「服务端还没存过这份偏好」时调用，所以整份推上去不会覆盖任何人的选择。
 * 值的口径必须与 setEditorPreference 的上行格式**逐字一致**：两个开关发 JSON 布尔，
 * 其余发字符串（indent_width 在本地是数字 2/4/6/8，这里要转成 '2'/'4' 这样的字符串）。
 * 差一个字符的表现是服务端做字段级合并时把这一项静默丢掉，页面上没有任何报错。
 *
 * 刻意不写本地缓存、也不派发变更事件：推上去的本来就是本地当前值，
 * 页面上显示的也是它，派发只会让两个编辑器页面白刷一次。
 */
function migrateLocalPreferencesToServer(): Promise<void> {
  const local = readEditorPreferences()
  return authApi
    .updatePreferences({
      word_wrap: local.word_wrap,
      minimap: local.minimap,
      indent_guides: local.indent_guides,
      whitespace: local.whitespace,
      indent_width: String(local.indent_width)
    })
    .then(() => undefined)
    .catch(() => {
      // 离线 / 未登录 / 老服务端没这个接口都走这里：静默，本地值原样保留。
      // 下一次改动（setEditorPreference）会把改动的那一项重新推上去。
    })
}

/**
 * 从服务端拉一次偏好并同步到本地缓存（进程内只拉一次，重复调用复用同一个 Promise）。
 *
 * 由编辑器页面在 onMounted / onActivated 里调用 —— 刻意不放在 main.ts 的 bootstrap 里：
 * 编辑器不在首屏，没必要给每一次打开面板都加一个请求。
 * 拉失败（未登录、离线、老版本服务端没这个接口）就继续用本地缓存，不阻塞任何东西。
 *
 * 同步方向由响应里的 stored 决定，两条分支的取向完全相反，别把它当成可有可无的冗余字段：
 * 服务端对「没存过的用户」下发的是一整套默认值，与「用户主动选了默认值」逐字相同，
 * 客户端只能靠这个标记区分两者。
 */
export function ensureEditorPreferencesLoaded(): Promise<void> {
  if (loadPromise) {
    return loadPromise
  }
  loadPromise = authApi
    .getPreferences()
    .then(res => {
      // 【分支一】服务端没存过（没有行 / 空串 / 脏 JSON，服务端一律回 stored:false）。
      // 这时下发的是默认值，绝不能拿它覆盖本地：升级那一刻**所有**老用户在服务端都是「没存过」，
      // 一个在 v3.2.2 把自动换行关掉的人，打开脚本页几十毫秒后就会眼睁睁看着它自己打开，
      // 而 localStorage 里的 'off' 已经被就地改写，刷新也回不去。
      // 所以这里方向反过来：把本机那份上行迁上去，一个 writeRaw 都不调。
      // ⚠️ 字段缺失（老服务端根本不认识 stored）也走这条，取最保守的一侧。
      if (res?.stored !== true) {
        return migrateLocalPreferencesToServer()
      }
      const editor = res.editor
      if (!editor || typeof editor !== 'object') {
        return
      }
      // 【分支二】服务端确实存过：逐项写回本地缓存。服务端认得的键才写，认不出来的原样留着，
      // 这样「服务端比前端老一个版本」时新键不会被清成默认值。
      // 每一项都要先跳过 locallyChanged：那是本次会话里用户亲手改过的键，
      // 这条 GET 在他点之前就发出去了，带回来的是过时的旧值（理由见 locallyChanged 的注释）。
      let applied = false
      if (!locallyChanged.has('word_wrap') && (editor.word_wrap === 'on' || editor.word_wrap === 'off')) {
        writeRaw('word_wrap', editor.word_wrap)
        applied = true
      }
      if (!locallyChanged.has('minimap') && typeof editor.minimap === 'boolean') {
        writeRaw('minimap', editor.minimap ? 'on' : 'off')
        applied = true
      }
      if (!locallyChanged.has('indent_guides') && typeof editor.indent_guides === 'boolean') {
        writeRaw('indent_guides', editor.indent_guides ? 'on' : 'off')
        applied = true
      }
      if (
        !locallyChanged.has('whitespace') &&
        (editor.whitespace === 'none' || editor.whitespace === 'selection' || editor.whitespace === 'all')
      ) {
        writeRaw('whitespace', editor.whitespace)
        applied = true
      }
      if (
        !locallyChanged.has('indent_width') &&
        editor.indent_width != null &&
        String(editor.indent_width) !== ''
      ) {
        writeRaw('indent_width', String(editor.indent_width))
        applied = true
      }
      // 一个键都没写进去（全被跳过 / 服务端一个认得的键都没下发）就别派发了：
      // 派了也没有任何值会变，只是让两个编辑器页面白刷一次。
      if (applied && typeof window !== 'undefined') {
        window.dispatchEvent(new Event(EDITOR_PREFERENCES_CHANGE_EVENT))
      }
    })
    .catch(() => {
      // 拉不到就继续用本地缓存。这里刻意**不**把 loadPromise 置回 null 重试：
      // 未登录 / 老服务端这类失败是稳定的，反复重试只会在每次切页时白发一轮请求。
    })
  return loadPromise
}

/**
 * 清掉「已经拉过」的记忆。
 *
 * 退出登录时必须调一次：换一个用户登进来，偏好也要跟着换成他自己的那份，
 * 否则会拿着上一个人的缓存不放。
 */
export function resetEditorPreferencesCache(): void {
  loadPromise = null
  // 脏键集合是「上一个账号在本次会话里改过什么」的记录，换人登进来必须一起清干净：
  // 留着它会让新用户存在服务端的那几项被当成「过时值」跳过写回，
  // 表现就是「换账号登录后，偏好没跟着人走」。
  locallyChanged.clear()
}

/**
 * 取循环列表里的下一档（齿轮菜单里「缩进宽度」「显示空白符」这两项是点一下换一档）。
 *
 * 放在这里而不是各页面各写一遍：两页复制一份的表现是循环顺序不一致，而且构建全绿。
 * indexOf 找不到时返回 -1，`(-1 + 1) % n === 0` 正好落回第一档 ——
 * 存储里被塞了脏值也不会卡在原地点不动。
 * 末尾的 `?? current` 只为满足 noUncheckedIndexedAccess：上面两个常量都是写死的非空数组。
 */
export function cycleEditorOption<T>(options: readonly T[], current: T): T {
  return options[(options.indexOf(current) + 1) % options.length] ?? current
}

/** 齿轮菜单 / 状态条上显示的缩进宽度文案。 */
export function editorIndentWidthLabel(value: EditorIndentWidth): string {
  return value === 'auto' ? '自动' : String(value)
}

/** 齿轮菜单 / 状态条上显示的空白符档位文案。 */
export function editorWhitespaceLabel(value: EditorWhitespaceMode): string {
  if (value === 'selection') return '仅选中'
  if (value === 'all') return '始终'
  return '关闭'
}
