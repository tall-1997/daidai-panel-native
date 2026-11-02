import { toast } from '@/utils/toast'

/**
 * 前端资源失效时「自动刷新一次」的兜底（issue #126）。
 *
 * 为什么需要：面板升级会整目录替换 web/（二进制 / Windows / Magisk 都是先删后拷），旧版 index.html
 * 引用的 hash 文件名在新版里全都不存在了。浏览器手里还是旧页面时（没关的标签页、缓存里的旧壳），
 * 之后的每一个动态 import 都会失败：切页没反应、编辑区空白、懒加载的弹窗打不开，而且没有一句人话的报错。
 * 这时唯一有效的动作是重新拉一次 index.html。本模块把它做成「自动、限次、不死循环」。
 *
 * 三个调用方共用 reloadOnce()：
 *   1. main.ts 监听 Vite 预加载助手派发的 `vite:preloadError`（任何动态 import 失败都会先到这里）；
 *   2. router.onError 命中 chunk 加载失败：刷新后直接落到用户要去的那一页；
 *   3. MainLayout 的版本自检（reloadIfWebVersionStale）：后端版本比本前端包构建时写死的版本新。
 *
 * ⚠️ 限次是硬约束：后端宕机、反代拦了 /assets、文件真的缺失时，刷新解决不了问题，不限次就是整页无限自刷。
 *    所以 60 秒内只自刷一次，时间戳记在 sessionStorage（按标签页隔离）。
 *    sessionStorage 读写不了（隐私模式、禁用站点数据）时**宁可不刷**：记不住「刚刷过」，放行就可能死循环。
 *    不刷的时候错误照常交给调用方自己的兜底（编辑器的失败态、切页失败的提示）。
 *
 * ⚠️ 页面上有未保存的内容时也不自动刷新（见 setUnsavedWork）：自动刷新会被任何一次 chunk 失败触发，
 *    包括路由的后台预加载、Monaco 预热、懒加载的弹窗，用户根本没点过刷新，没保存的内容却会被直接丢掉。
 *    这时改为弹一条带「刷新」按钮的提示，由用户保存后自己点；错误同样照常交给调用方的兜底。
 */

const RELOAD_STAMP_KEY = 'dd:chunk-reload:at'
const VERSION_PAIR_KEY = 'dd:chunk-reload:version-pair'
const RELOAD_COOLDOWN_MS = 60 * 1000
// 发起跳转后本页还活着多久，就认定这次跳转没发生（被 beforeunload 提示拦下了），见 reloadOnce 末尾
const PENDING_RELOAD_STALE_MS = 10 * 1000
// 「有未保存内容，暂不刷新」那条提示的停留时长，同时是它的去重窗口，见 notifyReloadDeferred
const DEFERRED_NOTICE_MS = 8 * 1000

/**
 * 三大浏览器对「动态 import 拉不下来」的报错措辞各不相同，只能按文案认；外加 Vite 自己的 CSS 预加载失败。
 * 刻意不认模块求值时抛出的普通异常：那是代码 bug，刷新解决不了。
 */
const CHUNK_LOAD_ERROR_PATTERNS: readonly RegExp[] = [
  // Chromium（Chrome / Edge）
  /Failed to fetch dynamically imported module/i,
  // Firefox
  /error loading dynamically imported module/i,
  // Safari
  /Importing a module script failed/i,
  // Vite 预加载助手：chunk 依赖的 CSS 那个 <link> 触发了 error（node_modules/vite 里的 preload helper）
  /Unable to preload CSS/i,
]

export function isChunkLoadError(error: unknown): boolean {
  let message = ''
  if (typeof error === 'string') {
    message = error
  } else if (error && typeof error === 'object' && 'message' in error) {
    message = String((error as { message?: unknown }).message ?? '')
  }
  return message !== '' && CHUNK_LOAD_ERROR_PATTERNS.some((pattern) => pattern.test(message))
}

/**
 * 「未保存工作」登记：有未保存内容的页面在「有改动」时登记，保存、切走文件、卸载时撤销。
 *
 * 登记期间两件事：
 *   1. reloadOnce 不自动刷新，改为提示用户保存后手动刷新（见 notifyReloadDeferred）；
 *   2. 挂一个 beforeunload 离开确认：关标签页、按 F5、点提示里的「刷新」都会先问一句。
 *      没有登记时摘掉：不拦没改动的页面，也不白挂监听（Firefox 下挂着 beforeunload 的页面进不了往返缓存）。
 *
 * 离开确认放在这里统一挂，而不是各页面各挂一个：两件事的触发条件是同一个，分开挂迟早会漂移成
 * 「提示说要保存、离开时却不拦」或者反过来。
 *
 * key 由调用方自选，一个页面一个、互不相同即可；同一个 key 反复登记是幂等的。
 * 漏撤的表现是「这个标签页再也不会自动刷新 + 离开时总被问一句」，所以卸载时一定要撤。
 */
const unsavedWorkKeys = new Set<string>()

function confirmLeaveWithUnsavedWork(event: BeforeUnloadEvent) {
  if (unsavedWorkKeys.size === 0) return
  // 现行规范与 Chrome / Edge 119+ 认 preventDefault；更老的 Chromium 只认给 returnValue 赋值，两个都写
  event.preventDefault()
  event.returnValue = true
}

export function setUnsavedWork(key: string, dirty: boolean): void {
  if (typeof window === 'undefined') return
  if (dirty) {
    unsavedWorkKeys.add(key)
  } else {
    unsavedWorkKeys.delete(key)
  }
  // 同一个监听函数重复 add 是空操作，所以这里不用记「之前挂没挂过」
  if (unsavedWorkKeys.size > 0) {
    window.addEventListener('beforeunload', confirmLeaveWithUnsavedWork)
  } else {
    window.removeEventListener('beforeunload', confirmLeaveWithUnsavedWork)
  }
}

export function hasUnsavedWork(): boolean {
  return unsavedWorkKeys.size > 0
}

/** 上一次「暂不刷新」提示弹出的时刻，给接连到达的同一批失败去重 */
let deferredNoticeAt = 0

/** 最近一次 reloadOnce 是否因「页面上有未保存的内容」而暂缓，见 wasLastReloadDeferred */
let lastReloadDeferred = false

/**
 * 有未保存内容时，用一条带「刷新」按钮的提示代替自动刷新。
 *
 * 去重窗口等于提示的停留时长：切页失败时 vite:preloadError 与 router.onError 会各调一次 reloadOnce，
 * 菜单预加载一批 chunk 也会接连失败，不去重就是一叠一模一样的提示。
 * 过了窗口还有新的失败，说明用户又碰到一处打不开的地方，再提示一次是应该的。
 *
 * 按钮原地刷新，不跳到调用方给的落点：用户多半是保存完才点，离触发失败的那次操作已经过了一阵，
 * 回到自己正在编辑的这一页，比跳去当时想打开的页更符合预期。
 * 没保存就点的话，上面的离开确认会先拦一道，用户仍能取消。
 * 不走 60 秒限次：这是用户自己点的，不存在循环。
 */
function notifyReloadDeferred(): void {
  const now = Date.now()
  const elapsed = now - deferredNoticeAt
  if (elapsed >= 0 && elapsed < DEFERRED_NOTICE_MS) return
  deferredNoticeAt = now
  toast.warning('面板已更新，保存后刷新页面即可', {
    duration: DEFERRED_NOTICE_MS,
    action: { text: '刷新', handler: () => window.location.reload() },
    // 用户提前点 × 关掉这条提示时，去重窗口要跟着撤掉：窗口本来就是照着「提示还在屏幕上」定的。
    // 不撤的话，窗口内再碰到一次失败，这条提示不会重弹，调用方又会因为「刚暂缓过」憋住自己的兜底提示
    // （见 wasLastReloadDeferred），用户点了菜单什么都看不到。
    // 只撤自己这一次：提示自然到期时窗口早已过去，清不清都一样；万一期间已经弹过新的一条
    // （只可能发生在窗口过完之后），deferredNoticeAt 记的是新那条的时刻，这里不能把它抹掉。
    onClose: () => {
      if (deferredNoticeAt === now) deferredNoticeAt = 0
    },
  })
}

/**
 * 最近一次 reloadOnce 调用是否因「页面上有未保存的内容」而暂缓 —— 也就是刚弹过上面那条提示。
 *
 * 给调用方分辨 reloadOnce 返回 false 的原因：暂缓时该闭嘴，让「保存后刷新页面即可」自己说话，
 * 再叠一条自己的失败提示只会互相矛盾；其余原因（离线、60 秒限次、sessionStorage 不可用）都没弹过提示，
 * 调用方照常走自己的兜底。
 * 只反映最近一次调用，所以要紧挨着 reloadOnce 的返回值读（两者之间必须是同步代码）。
 */
export function wasLastReloadDeferred(): boolean {
  return lastReloadDeferred
}

/** 本页已经决定刷新、正等着下一个宏任务执行时的状态。见 reloadOnce 里「为什么推迟一拍」。 */
let pendingReload: { reason: string; target?: string } | null = null

/** 本页是否已经安排了自动刷新（还没真正跳走）。 */
export function isChunkReloadPending(): boolean {
  return pendingReload !== null
}

/**
 * 自动刷新一次。返回值表示「是否已安排刷新」：false 说明被限次、未保存内容或环境条件拦下了，
 * 调用方要自己兜底（不要 preventDefault，让失败照常露出来）。
 *
 * @param reason    写进控制台，便于事后判断是哪一路触发的（preload / route / version）
 * @param targetUrl 刷新后要落到的地址（带 base 的完整路径）；不给就原地 reload
 */
export function reloadOnce(reason: string, targetUrl?: string): boolean {
  // 抢在所有分支之前清零，只有下面的未保存分支会置回 true。这样每一条返回路径都不会漏设，
  // 调用方读到的一定是本次调用的结果（见 wasLastReloadDeferred）。
  lastReloadDeferred = false
  if (typeof window === 'undefined') return false

  if (pendingReload) {
    // 同一轮失败里后到的调用方知道得更具体（router.onError 知道用户要去哪一页），用它的落点
    if (targetUrl) pendingReload.target = targetUrl
    return true
  }

  // 明确离线时刷新只会换来浏览器自己的「无法访问」错误页，连面板都回不去了
  if (navigator.onLine === false) {
    console.warn(`[自动刷新] 已跳过（${reason}）：浏览器处于离线状态`)
    return false
  }

  const now = Date.now()
  let last: number
  try {
    last = Number(window.sessionStorage.getItem(RELOAD_STAMP_KEY))
  } catch {
    console.warn(`[自动刷新] 已跳过（${reason}）：sessionStorage 不可用，无法防止循环刷新`)
    return false
  }
  const elapsed = now - last
  // elapsed < 0 说明系统时钟往回拨过：当成没刷过，不能让一个「未来的时间戳」把自刷封住好几个小时
  if (last > 0 && elapsed >= 0 && elapsed < RELOAD_COOLDOWN_MS) {
    console.warn(
      `[自动刷新] 已跳过（${reason}）：${Math.round(elapsed / 1000)} 秒前刚自动刷新过，避免循环刷新`,
    )
    return false
  }

  // 排在限次与离线之后：那两种情况下本来就不会刷，也就不必提示「保存后刷新」。
  // 不写限次时间戳：这次没刷，不该占掉用户保存之后那次自动刷新的名额。
  if (hasUnsavedWork()) {
    lastReloadDeferred = true
    console.warn(`[自动刷新] 已暂缓（${reason}）：页面上有未保存的内容，改为提示保存后手动刷新`)
    notifyReloadDeferred()
    return false
  }

  try {
    window.sessionStorage.setItem(RELOAD_STAMP_KEY, String(now))
  } catch {
    console.warn(`[自动刷新] 已跳过（${reason}）：sessionStorage 不可用，无法防止循环刷新`)
    return false
  }

  pendingReload = { reason, target: targetUrl }
  console.warn(`[自动刷新] ${reason}：前端文件已失效或落后于后端版本，即将刷新页面`)
  // 为什么推迟到下一个宏任务才真正跳转：切页时 chunk 失败，Vite 的预加载助手会**先**同步派发
  // vite:preloadError（main.ts 在那里调到这里时还不知道用户要去哪一页），错误随后沿 Promise 链走到
  // router.onError，它才拿得到目标路由。整条链都是微任务，必然跑在这个 setTimeout 之前，
  // 所以 router.onError 来得及把落点补进 pendingReload。
  window.setTimeout(() => {
    const target = pendingReload?.target
    if (target) {
      window.location.assign(target)
    } else {
      window.location.reload()
    }
    // 跳转不一定真的发生：页面挂了 beforeunload 提示、用户在「离开此页？」里点了取消，本页就还活着。
    // 这时「等待刷新」的标记必须撤掉，否则 isChunkReloadPending() 永远为真、reloadOnce 永远返回 true ——
    // 之后每一次 chunk 失败都会被 main.ts 的监听 preventDefault 成 undefined，既不刷新也不报错：
    // 编辑器停在「加载中」、菜单点了没反应。
    // 推迟一段再撤而不是当场撤：正常刷新时新文档提交之前本页还会跑一小会儿，这期间陆续到达的同一批失败
    // （Promise.all 里的另一个 chunk 之类）仍应当成「马上就刷新了」静默吞掉，不该各自弹一遍失败态。
    window.setTimeout(() => {
      pendingReload = null
    }, PENDING_RELOAD_STALE_MS)
  }, 0)
  return true
}

function normalizeVersion(value: unknown): string {
  return typeof value === 'string' ? value.trim().replace(/^v/i, '') : ''
}

type VersionTriple = readonly [number, number, number]

/** 只认完整的 x.y.z（已去掉前缀 v）；少一段、带后缀（开发版、预发布）、非数字的一律当解析不出 */
function parseVersion(normalized: string): VersionTriple | null {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(normalized)
  if (!match) return null
  return [Number(match[1]), Number(match[2]), Number(match[3])]
}

/** 逐段比数字：3.10.0 比 3.9.9 新，按字符串比会反过来 */
function isNewerVersion(candidate: VersionTriple, base: VersionTriple): boolean {
  const [candidateMajor, candidateMinor, candidatePatch] = candidate
  const [baseMajor, baseMinor, basePatch] = base
  if (candidateMajor !== baseMajor) return candidateMajor > baseMajor
  if (candidateMinor !== baseMinor) return candidateMinor > baseMinor
  return candidatePatch > basePatch
}

/**
 * 版本自检（契约 C10）：后端版本号比本前端包构建时写死的 __DD_WEB_VERSION__ 新时，自动刷新一次。
 *
 * 能发现的是「浏览器跑的是缓存里的旧前端壳」：侧栏徽标取的是后端版本，旧壳照样显示新版本号，用户看不出来。
 * 只能保护本版之后的升级：已经缓存下来的更老的壳里没有这段代码。
 *
 * 为什么只认「后端更新」，不认「不一致」：旧壳场景永远是前端旧、后端新。反过来后端比前端旧时，
 * 服务端发出来的前端本来就是这一版，刷新解决不了，多半是后端版本号没写对 —— 不带 VERSION 参数
 * 自建 Docker 镜像时是 Dockerfile 的默认值 2.2.18，Magisk 本地打包时是 build.sh 的默认值 0.0.0。
 * 按「不一致」比，这类部署每开一个新标签页都会白刷一次。两边相等、任一边不是完整的 x.y.z，都不动。
 *
 * 前提是发版时 web/package.json 与 server/handler/version.go 同步（release-preflight.ps1 校验这两处）。
 * 后端版本号写得比前端大时每开一个标签页仍会白刷一次；下面那道「同一对版本在本标签页只刷一次」的闸，
 * 至少保证不会每按一次 F5 都刷。
 * 由调用方用编译期常量守卫，只在发布版、非演示站调用（见 MainLayout 的 loadVersion）。
 *
 * @returns 是否已安排刷新
 */
export function reloadIfWebVersionStale(
  serverVersion: unknown,
  webVersion: string = __DD_WEB_VERSION__,
): boolean {
  if (typeof window === 'undefined') return false
  // 比较前两边都去掉前缀 v：后端下发的与 package.json 里的写法不保证一致
  const server = normalizeVersion(serverVersion)
  const web = normalizeVersion(webVersion)
  const serverParts = parseVersion(server)
  const webParts = parseVersion(web)
  // 老后端没下发版本号、构建时没读到 package.json、开发版带后缀：解析不出就一律不动
  if (!serverParts || !webParts || !isNewerVersion(serverParts, webParts)) return false

  const pair = `${web}->${server}`
  try {
    // 本标签页已经为「这一对版本」刷过一次还是对不上：说明服务端发出来的前端本身就是这个版本
    // （比如 web_dir 指向一份单独维护的旧前端），再刷也没用
    if (window.sessionStorage.getItem(VERSION_PAIR_KEY) === pair) return false
  } catch {
    return false
  }

  console.warn(`[自动刷新] 前端 ${web} 比后端 ${server} 旧，浏览器可能还在用升级前缓存的页面`)
  // 有未保存内容时 reloadOnce 改为提示并返回 false。这次没刷就不记这一对版本，下次加载页面时照常自检
  if (!reloadOnce('version')) return false
  try {
    window.sessionStorage.setItem(VERSION_PAIR_KEY, pair)
  } catch {
    // 记不住只是之后可能多刷一次，reloadOnce 自己的 60 秒限次仍然兜着
  }
  return true
}
