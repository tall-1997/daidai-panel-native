import { authApi } from '@/api/auth'

/**
 * 列表页 / 日志查看等界面的「个人偏好」（服务端 list 组，稀疏存储）：
 * 定时任务页 / 环境变量页的每页条数，任务页视图栏「全部」「顶栏分组页签」的显隐，
 * 以及「打开已结束的日志时定位到底部」（#147）。
 * 写入有两种：setListPreferences 写本机缓存后发出去不管（即改即生效的开关用）；
 * saveListPreferences 等服务端写入结果、失败回滚本机缓存（视图管理弹窗的保存用，#157 ③）。
 *
 * v3.3.1（issue #143 桌面端第 1 条）起跟随账户：换浏览器、换域名 / IP 都还在。
 * 此前前 4 项散在三个页面里各自读写 localStorage，而 localStorage 按 origin 隔离，换个地址打开就全没了。
 * v3.3.3 的 log_open_at_bottom 是新键，没有历史本地值；APP 读写的也是它这一份。
 * 组名还叫 list，但以后再有跟账户走的零散界面开关，照样往这里加键，别按字面另开一组（两处真源）。
 *
 * 存储模型与 utils/editorPreferences.ts 同构（两层，顺序不能反）：
 *   - localStorage 是首屏 / 离线 / 隐私模式下的同步缓存（页面 setup 时必须立刻拿到条数，不能等网络）；
 *   - 服务端 `/api/auth/preferences` 的 list 组是真源，ensureListPreferencesLoaded() 拉回来后写回缓存。
 * 不要另发明一套；与 editor 的差别只有下面两点，都是刻意的：
 *
 * ① 服务端对 list 组做【稀疏存储】：只下发用户真的存过的键，不维护默认值。
 *    所以这里没有 editor 那种「整组 stored 标记」，而是逐键判断：服务端有这个键 → 下行写回；
 *    没有 → 本机老键里真有值才上行迁移。迁移精确到每一个键，本机什么都没有时一个请求都不发。
 *    这样做是因为 issue 的报告者正是多域名 / 多 IP 在用：组级 stored 下，第一个被打开的 origin
 *    哪怕从没改过设置，也会把默认值「占坑」写上去，其它 origin 的自定义值随后被全部冲掉。
 * ② 不派发变更事件：任务页、视图栏、环境变量页、个人设置页在挂载时调 ensure，
 *    等它 resolve 之后自己重读一遍并应用即可；执行日志页挂载时也 ensure，
 *    而 log_open_at_bottom 的消费方（useLogAutoFollow.revealFinished、任务日志 LogViewer、两处日志文件查看）
 *    都是「打开日志那一刻」同步读缓存，用不着事件。
 *
 * 🔴 验收口径：五个本地缓存键名（STORAGE_KEYS）在 web/src 里只允许出现在本文件。任何一处页面代码绕过 setListPreference(s)
 *    直接写 localStorage，用户的改动就只留在本机，下次加载时被服务端旧值静默改回去 —— 不报错、构建全绿。
 */

export interface ListPreferences {
  /** 定时任务页每页条数 */
  tasks_page_size: 10 | 20 | 50 | 100
  /** 环境变量页每页条数，'all' 表示分批拉全量、不显示分页器 */
  envs_page_size: '20' | '50' | '100' | 'all'
  /** 任务页视图栏里的「全部」是否隐藏 */
  tasks_view_all_hidden: boolean
  /** 任务页视图栏里的顶栏分组页签（来自任务 labels 的 `分组:` 标签）是否整体隐藏 */
  tasks_view_groups_hidden: boolean
  /** 打开【已结束】的日志时是否定位到底部（#147）；运行中的日志始终自动跟随，不看它 */
  log_open_at_bottom: boolean
}

type ListPreferenceKey = keyof ListPreferences
type ListPreferenceValue = ListPreferences[ListPreferenceKey]

/**
 * 任务页每页条数的可选值。
 * ⚠️ 与服务端 server/handler/user_preference.go 的 list 白名单一一对应，改这里必须同时改那里，
 *    否则多出来的档位 PUT 上去会被 400、只在本机生效，换个浏览器就回到默认值。
 */
export const TASKS_PAGE_SIZE_OPTIONS: readonly ListPreferences['tasks_page_size'][] = [10, 20, 50, 100]

/** 环境变量页每页条数的可选值。白名单同上，与服务端逐项对应。 */
export const ENVS_PAGE_SIZE_OPTIONS: readonly ListPreferences['envs_page_size'][] = ['20', '50', '100', 'all']

/**
 * 默认值与改造前三个页面各自的回落值逐字相同（20 条 / '20' / 都显示），
 * 老用户升级后、服务端拉回来之前的第一眼观感不变。服务端不存默认值，只有这一份。
 * log_open_at_bottom 默认 false：保持 v3.2.8（#133）起「打开已结束的日志停在顶部」的行为。
 * APP 在账户没设过这个键时按它自己的现状（底部）处理，两端默认值不同是刻意的，别来「对齐」。
 */
export const LIST_PREFERENCES_DEFAULTS: Readonly<ListPreferences> = Object.freeze({
  tasks_page_size: 20,
  envs_page_size: '20',
  tasks_view_all_hidden: false,
  tasks_view_groups_hidden: false,
  log_open_at_bottom: false
})

/** 全部键，顺序无意义。供遍历与演示站 mock 的白名单校验使用。 */
export const LIST_PREFERENCE_KEYS: readonly ListPreferenceKey[] = [
  'tasks_page_size',
  'envs_page_size',
  'tasks_view_all_hidden',
  'tasks_view_groups_hidden',
  'log_open_at_bottom'
]

/**
 * 前 4 个 localStorage 键与取值格式都沿用改造前各页面的老写法（条数存字符串，开关存 '1' / '0'）：
 *   - 老值天然就是上行迁移的数据源，不需要另做一次搬家；
 *   - 回退到 v3.3.0 时老版本照样读得懂。
 * log_open_at_bottom 是新键，取值格式同其它开关。
 * 🔴 它【绝不能】复用 v3.2.8 前的 dd:tasks:log_follow：LogViewer 每次 setup 都会删那个老键，复用了等于每开一次日志就丢一次设置。
 * 🔴 这五个键名只允许出现在本文件（见文件头的验收口径）。
 */
const STORAGE_KEYS: Record<ListPreferenceKey, string> = {
  tasks_page_size: 'dd:tasks:page_size',
  envs_page_size: 'daidai-env-page-size',
  tasks_view_all_hidden: 'dd:tasks:view_all_hidden',
  tasks_view_groups_hidden: 'dd:tasks:view_groups_hidden',
  log_open_at_bottom: 'dd:log:open_at_bottom'
}

/**
 * 隐私模式 / 站点存储被禁用时 setItem 会直接抛错。这时本次改动仍然必须生效，
 * 所以把写不进去的那一项在内存里留一份覆盖，读的时候以它为准（同 editorPreferences.ts）。
 * 少了这一层的表现是：改了每页条数，页面回头一读还是旧值，看起来像没改上。
 */
const memoryOverrides: Partial<Record<ListPreferenceKey, string>> = {}

function readRaw(key: ListPreferenceKey): string | null {
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
    // 隐私模式下 getItem 会直接抛错，不能让它把调用方的 setup 整块炸掉
    //（环境变量页改造前的读取就没兜这一层，隐私模式下整页白屏）
    return null
  }
}

function writeRaw(key: ListPreferenceKey, value: string): void {
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

/**
 * 校验「接口上的值」（服务端下发的 / 准备 PUT 上去的），不合法返回 undefined。
 * 口径与服务端白名单一致：条数是 JSON number，环境变量条数是 JSON string，其余开关都是 JSON bool。
 * 类型不对（比如条数传了字符串 "50"）一律视为不合法，不做宽松转换 —— 服务端对它同样回 400。
 * 导出给演示站 mock 复用，免得手抄一份白名单。
 */
export function parseListPreferenceWire<K extends ListPreferenceKey>(
  key: K,
  raw: unknown
): ListPreferences[K] | undefined {
  return parseWire(key, raw) as ListPreferences[K] | undefined
}

function parseWire(key: ListPreferenceKey, raw: unknown): ListPreferenceValue | undefined {
  if (key === 'tasks_page_size') {
    return TASKS_PAGE_SIZE_OPTIONS.find((option) => option === raw)
  }
  if (key === 'envs_page_size') {
    return ENVS_PAGE_SIZE_OPTIONS.find((option) => option === raw)
  }
  return typeof raw === 'boolean' ? raw : undefined
}

/**
 * 解析本机缓存里的老格式字符串。读不到、脏值一律返回 undefined ——
 * 调用方要靠它区分「本机真的存过」和「回落出来的默认值」，后者绝不能拿去上行迁移。
 */
function parseStored(key: ListPreferenceKey, raw: string | null): ListPreferenceValue | undefined {
  if (raw === null) {
    return undefined
  }
  if (key === 'tasks_page_size') {
    // 老代码就是 Number(raw) 再比白名单，口径保持一致；空串会得到 0，不在白名单里
    return parseWire(key, Number(raw))
  }
  if (key === 'envs_page_size') {
    return parseWire(key, raw)
  }
  // 开关：老代码只写 '1' / '0'（新键 log_open_at_bottom 沿用同一格式）。
  // '0' 也算「真的存过」（用户在视图管理里保存过「显示」、在个人设置里选过「停在开头」）
  if (raw === '1') return true
  if (raw === '0') return false
  return undefined
}

/** 序列化成 localStorage 里的老格式：条数转字符串，开关写 '1' / '0'。 */
function serialize(value: ListPreferenceValue): string {
  if (typeof value === 'boolean') {
    return value ? '1' : '0'
  }
  return String(value)
}

/**
 * 同步读一项偏好（走本地缓存）。页面 setup 时用它初始化，服务端值由 ensureListPreferencesLoaded() 异步补上。
 */
export function readListPreference<K extends ListPreferenceKey>(key: K): ListPreferences[K] {
  const stored = parseStored(key, readRaw(key))
  return (stored ?? LIST_PREFERENCES_DEFAULTS[key]) as ListPreferences[K]
}

/**
 * 本次会话里被用户亲手改过的键。
 *
 * 只为一件事：让「用户刚改的」赢过「更早发出的那次 GET」。ensure 在挂载时就发 GET，
 * 远程访问时一个来回几百毫秒，这期间用户改了每页条数，那条 GET 带回来的是服务端旧值，
 * 照着写回去，条数会自己弹回原位（理由与 editorPreferences.ts 的 locallyChanged 相同）。
 * 只在本次页面加载内有效，刻意不落存储；换账号时由 resetListPreferencesCache 清掉。
 */
const locallyChanged = new Set<ListPreferenceKey>()

/**
 * 一次改多项偏好：逐键校验 → 全部记脏 → 全部写本地缓存 → 后台只发【一次】PUT，带上这批全部键。
 *
 * 🔴 只在「用户动作」里调用（改每页条数这类即改即生效的开关），
 *    不要挂进 watch：程序化应用服务端值时也会触发 watch，结果是多发一次 PUT，
 *    还会把这一键误记成「本地改过」，挡住后面的下行同步。
 *    视图管理弹窗的保存（两个隐藏开关）要等写入结果再提示，走下面的 saveListPreferences，不走这里（#157 ③）。
 * 同一个动作里要改好几项时用它，不要连调几次 setListPreference：
 * 那样会同时发出好几个 PUT，请求数白白翻倍；合成一个请求，服务端也只需做一次「读 - 合并 - 写」。
 * 只提交这一批改动的键，服务端逐键合并，并用 preferenceWriteMu 把「读整行 - 合并 - 整列写回」串行化
 *（server/handler/user_preference.go）。所以同时到达的多个 PUT（两个标签页各改各的、ensure 的迁移补丁撞上
 * 紧接着的改动）只要改的是不同的键，就不会互相覆盖。串行化管不了先后：两个 PUT 改同一个键时，以服务端后处理的那个为准。
 * 同步失败刻意不打扰用户：本机已经生效了，下次改动会再推一次。
 */
export function setListPreferences(patch: Partial<ListPreferences>): void {
  const accepted: Record<string, ListPreferenceValue> = {}
  // 按白名单键遍历而不是遍历 patch：白名单外的键天然被忽略，不会写进本地缓存，也不会发给服务端吃 400。
  for (const key of LIST_PREFERENCE_KEYS) {
    // 运行期兜底：el-pagination 的 size-change 给的是 number，调用方断言成联合类型时可能混进白名单外的值。
    // 放行的话服务端对整个请求回 400（被静默吞掉，同一批里合法的键也跟着丢），
    // 本地又存了一个读回来会被当成脏值的字符串，两头都不对。所以非法的那一键单独丢掉，其余照发。
    // patch 里没有的键取到 undefined，同样在这里被跳过。
    const value = parseWire(key, patch[key])
    if (value === undefined) {
      continue
    }
    accepted[key] = value
    locallyChanged.add(key)
    writeRaw(key, serialize(value))
  }
  // 一个合法键都没有就不发：空 list 对服务端是 no-op，白跑一个请求
  if (Object.keys(accepted).length === 0) {
    return
  }
  void authApi.updateListPreferences(accepted).catch(() => {
    // 未登录 / 离线 / 服务端报错：静默，理由见上
  })
}

/** 改一项偏好，等价于只带这一个键的 setListPreferences（约束与说明见那里）。 */
export function setListPreference<K extends ListPreferenceKey>(key: K, value: ListPreferences[K]): void {
  const patch: Partial<ListPreferences> = {}
  patch[key] = value
  setListPreferences(patch)
}

/**
 * 与 setListPreferences 同一套校验与本机写入，但【等服务端写入结果】（#157 ③，视图管理弹窗的保存在用）：
 * 成功才 resolve；失败时把这次写过的本机键回滚到写之前的值，再把错误原样抛给调用方，由它报错、不关弹窗。
 *
 * 为什么视图管理要等：它原来走 setListPreferences，先提示「视图设置已保存」并当场隐藏页签，PUT 发出去就不管了；
 * PUT 一旦失败，刷新后 ensureListPreferencesLoaded 拿服务端的旧值把本机缓存冲回去，用户关掉的页签又出现了。
 * 其它调用方（每页条数这类即改即生效的开关）仍用 setListPreferences：同步失败对它们只是「换个浏览器不记得」，不值得打断用户。
 *
 * 约束同 setListPreferences：只在用户动作里调用、只传真的变了的键（服务端稀疏存储，没改也写等于替用户占坑）。
 * 回滚只动本机缓存与「本次会话改过」的记号；服务端那边 PUT 失败本来就没写进去。
 */
export async function saveListPreferences(patch: Partial<ListPreferences>): Promise<void> {
  const accepted: Record<string, ListPreferenceValue> = {}
  // 写之前的本机原值（null = 本机没存过）与这一键本来是否已记脏，回滚时原样放回去
  const previous: { key: ListPreferenceKey; raw: string | null; wasChanged: boolean }[] = []
  for (const key of LIST_PREFERENCE_KEYS) {
    const value = parseWire(key, patch[key])
    if (value === undefined) {
      continue
    }
    accepted[key] = value
    previous.push({ key, raw: readRaw(key), wasChanged: locallyChanged.has(key) })
    locallyChanged.add(key)
    writeRaw(key, serialize(value))
  }
  // 一个合法键都没有就不发（同 setListPreferences）
  if (previous.length === 0) {
    return
  }
  try {
    await authApi.updateListPreferences(accepted)
  } catch (err) {
    for (const { key, raw, wasChanged } of previous) {
      if (raw === null) {
        // 本机原来没有这个键：删掉，读的时候回落到默认值
        delete memoryOverrides[key]
        try {
          window.localStorage.removeItem(STORAGE_KEYS[key])
        } catch {
          // 隐私模式下存储不可用：上面已经清掉内存覆盖，读到的就是默认值
        }
      } else {
        writeRaw(key, raw)
      }
      // 回滚后这一键不再算「本次会话改过」：还在路上的那次 ensure 的 GET 回来时，要允许它把服务端值写回本机
      if (!wasChanged) {
        locallyChanged.delete(key)
      }
    }
    throw err
  }
}

let loadPromise: Promise<void> | null = null

/**
 * 从服务端拉一次偏好并与本地缓存对齐（进程内只拉一次，重复调用复用同一个 Promise）。
 *
 * 由任务页、视图栏、环境变量页、执行日志页、个人设置页在挂载时调用；刻意不放进 main.ts 的 bootstrap，不给其它页面加请求。
 * 失败（未登录、离线）就继续用本地缓存；刻意不把 loadPromise 置回 null 重试：
 * 这类失败是稳定的，反复重试只会在每次切页时白发一轮请求。
 *
 * resolve 时本地缓存已经是最终值，页面拿到之后自己 readListPreference 重读并应用。
 */
export function ensureListPreferencesLoaded(): Promise<void> {
  if (loadPromise) {
    return loadPromise
  }
  loadPromise = authApi
    .getPreferences()
    .then((res) => {
      const list = res?.list
      // 🔴 响应里没有 list 对象 = 老服务端（v3.3.0 及以前）或非预期形状：直接返回，【不上行】。
      // 老服务端的 PUT 不认识 list，会把整套 editor 默认值写进库、把 editor 的 stored 翻成 true，
      // 下次打开编辑器时本机的编辑器偏好会被默认值静默冲掉。
      if (!list || typeof list !== 'object' || Array.isArray(list)) {
        return
      }
      const migratePatch: Record<string, ListPreferenceValue> = {}
      for (const key of LIST_PREFERENCE_KEYS) {
        const serverValue = parseWire(key, list[key])
        if (serverValue !== undefined) {
          // 服务端存过这个键：服务端为准，写回本地缓存。
          // 本次会话里用户已经改过的键跳过 —— 这条 GET 在他改之前就发出去了，带回来的是旧值。
          if (!locallyChanged.has(key)) {
            writeRaw(key, serialize(serverValue))
          }
          continue
        }
        // 服务端没有这个键：本机老键里【真的有】合法值才迁上去；回落出来的默认值绝不上行，
        // 否则等于替用户在服务端「占坑」，挡住他在别的域名 / IP 上存过的自定义值。
        const localValue = parseStored(key, readRaw(key))
        if (localValue !== undefined) {
          migratePatch[key] = localValue
        }
      }
      // 补丁非空才发，而且只发一次。刻意不等它回来就 resolve：迁上去的就是本机当前值，本地什么都不用变，
      // 而环境变量页是 await 完 ensure 才发首个列表请求的，不该再多等一个 PUT 的往返。
      if (Object.keys(migratePatch).length > 0) {
        void authApi.updateListPreferences(migratePatch).catch(() => {
          // 离线 / 服务端报错：静默。本机值原样保留，下次加载时服务端仍没有这个键，会再迁一次。
        })
      }
    })
    .catch(() => {
      // 拉不到就继续用本地缓存（理由见函数注释）
    })
  return loadPromise
}

/**
 * 清掉「已经拉过」的记忆与本次会话的脏键记录。
 *
 * 退出登录时必须调（stores/auth.ts 的 clearAuth）：换一个用户登进来，偏好要跟着换成他自己的那份；
 * 留着脏键集合还会让新用户存在服务端的键被当成「过时值」跳过写回。
 * 只清记忆、不清 localStorage 里的值：它同时是本机的离线缓存，下次登录会被服务端值覆盖。
 */
export function resetListPreferencesCache(): void {
  loadPromise = null
  locallyChanged.clear()
}
