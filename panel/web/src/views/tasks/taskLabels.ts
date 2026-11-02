const SUBSCRIPTION_LABEL_PREFIX = 'subscription:'
const SUBSCRIPTION_DISPLAY_LABEL = '订阅任务'
const TASK_GROUP_LABEL_PREFIX = '分组:'

function uniqueLabels(labels: string[]) {
  return Array.from(new Set(labels.filter(Boolean)))
}

// 判前缀的 trim 口径（#157 ③）：
// - `分组:` 先 trim 再判，与服务端（task_groups.go 的 taskGroupNameFromLabels、task_query.go 的展示）和 APP 一致。
//   带前导空格的脏分组 " 分组:X" 因此能正确进「任务分组」框，下次保存时被 mergeTaskLabels 写回干净的 `分组:X`；
// - `subscription:` 刻意【不】trim：服务端的订阅展示与订阅同步认领都不 trim，" subscription:N" 本来就不是生效的订阅关联，
//   前端单独 trim 会把它藏进内部标签，造出「表单里删不掉、列表里还显示」的假订阅标签。
export function isInternalTaskLabel(label: string) {
  return label.startsWith(SUBSCRIPTION_LABEL_PREFIX) || label.trim().startsWith(TASK_GROUP_LABEL_PREFIX)
}

export function getTaskGroupName(labels: string[] = []) {
  for (const label of labels) {
    if (!label) continue
    // trim 后再判前缀（口径见 isInternalTaskLabel 上方的说明）；名字为空的跳过接着找，与服务端一致
    const trimmed = label.trim()
    if (trimmed.startsWith(TASK_GROUP_LABEL_PREFIX)) {
      const group = trimmed.slice(TASK_GROUP_LABEL_PREFIX.length).trim()
      if (group) return group
    }
  }
  return ''
}

export function toTaskGroupLabel(groupName: string) {
  const normalized = groupName.trim()
  return normalized ? `${TASK_GROUP_LABEL_PREFIX}${normalized}` : ''
}

export function getDisplayTaskLabels(labels: string[] = []) {
  const displayLabels: string[] = []
  let hasSubscriptionLabel = false
  const groupName = getTaskGroupName(labels)

  for (const label of labels) {
    if (!label) continue
    if (label.startsWith(SUBSCRIPTION_LABEL_PREFIX)) {
      hasSubscriptionLabel = true
      continue
    }
    // 分组标签 trim 后判前缀（同 getTaskGroupName）：脏的 " 分组:X" 已经作为分组名前插，不能再当成自定义标签显示一遍
    if (label.trim().startsWith(TASK_GROUP_LABEL_PREFIX)) {
      continue
    }
    displayLabels.push(label)
  }

  if (groupName) {
    displayLabels.unshift(groupName)
  }

  if (hasSubscriptionLabel) {
    displayLabels.push(SUBSCRIPTION_DISPLAY_LABEL)
  }

  return uniqueLabels(displayLabels)
}

export type DisplayTaskLabelKind = 'group' | 'subscription' | 'custom'

export interface DisplayTaskLabelEntry {
  /** 展示文案，与 display_labels 里那一项逐字相同 */
  label: string
  /** 该项属于哪一类，决定「显示设置」里哪个开关管它，以及渲染成哪种配色 */
  kind: DisplayTaskLabelKind
  /** v-for 的 :key。带下标，保证同名两条不会撞 key */
  key: string
}

/**
 * 把后端下发的 display_labels 标注成「分组 / 订阅 / 自定义」三类，供列表页的标签分项隐藏使用。
 *
 * 返回值与 display_labels 【下标一一对齐】（空字符串项会被跳过），而不是切成三个桶：
 * 桶的做法把「类别」寄生在「字符串值」上，同名即同类 —— 订阅名等于分组名时，
 * 订阅那条必然被分组桶吞掉、订阅桶恒空，两个开关就串台成一个（issue #109-3）。
 *
 * display_labels 是一个扁平字符串数组、自身不带任何标记，前端只能靠两条外部证据反推：
 *   1) 分组：原始 labels 里那一项仍带 `分组:` 前缀，用 getTaskGroupName 取出显示名再精确比对
 *      （trim 后判前缀，与服务端同口径；带前导空格的脏分组也归进分组一类，#157 ③）。
 *      后端会把它 unshift 到 display_labels[0]，但这里【不按下标 0 猜】——
 *      任务没有分组时第 0 项就是普通自定义标签，按位置判会误伤。
 *   2) 订阅：后端新增的只读字段 subscription_labels（含订阅源已删除时的字面量「订阅任务」）。
 *
 * 定类别靠【名额消费】而不是集合成员判定：分组名只有 1 个名额（后端只 unshift 一条），
 * 订阅名的名额 = 它在 subscription_labels 里的出现次数。名额用完后同名的后续项自动落到 custom。
 * 这样「分组 娱乐 + 订阅 娱乐 + 自定义 娱乐」三条同名标签会各归各类，三个开关互不干扰。
 *
 * ⚠️ 同名多条时，谁先出现谁先被认领，无法还原后端那一条究竟来自哪个来源 ——
 * 但同名意味着文案完全一样，用户看到的结果没有差别，每个开关仍然恰好管住一条。
 *
 * ⚠️ subscription_labels 是后加的字段，老后端 / 未同步的演示站不会下发。
 * 这里对「缺失或不是数组」一律降级成空名额：订阅标签会被归进 custom 一类，
 * 表现为「关掉订阅标签开关时订阅名没被藏掉」，但界面照常渲染、不报错——宁可少隐藏也不能崩。
 */
export function classifyDisplayTaskLabels(
  displayLabels: string[] = [],
  rawLabels: string[] = [],
  subscriptionLabels?: unknown,
): DisplayTaskLabelEntry[] {
  const groupName = getTaskGroupName(rawLabels)

  // 订阅名建「名额表」而不是 Set：Set 只能回答「这个字符串是不是订阅名」，
  // 回答不了「并排的两条同名标签里，哪一条才是订阅那条」。
  const subscriptionQuota = new Map<string, number>()
  if (Array.isArray(subscriptionLabels)) {
    for (const item of subscriptionLabels) {
      if (typeof item !== 'string' || !item) continue
      subscriptionQuota.set(item, (subscriptionQuota.get(item) ?? 0) + 1)
    }
  }

  // 分组名额恒为 1：后端 buildPreparedTaskLabels 只会 unshift 一条分组名。
  let groupQuota = groupName ? 1 : 0

  const entries: DisplayTaskLabelEntry[] = []
  displayLabels.forEach((label, index) => {
    if (!label) return

    let kind: DisplayTaskLabelKind = 'custom'
    if (groupQuota > 0 && label === groupName) {
      groupQuota -= 1
      kind = 'group'
    } else {
      const remaining = subscriptionQuota.get(label) ?? 0
      if (remaining > 0) {
        subscriptionQuota.set(label, remaining - 1)
        kind = 'subscription'
      }
    }

    // key 必须带下标：同名两条只用 label 当 :key，Vue 会报重复 key 警告，
    // 且可能在开关切换时复用错节点（藏掉的和留下的是同一个字符串）。
    entries.push({ label, kind, key: `${index}-${kind}-${label}` })
  })

  return entries
}

/**
 * 任务的「启用开关」是否打开 —— 与运行态无关（issue #133）。
 *
 * ⚠️ 这不是标签逻辑。放在这里只是因为它要被列表页（index.vue）和详情弹窗（TaskDetail.vue）共用，
 * 而 tasks 目录下这两处已经共同引用的 helper 模块只有本文件；两边各抄一份迟早会改漏一边。
 *
 * 为什么不能再按 `status === 0` 判：status 一个字段同时承载「开关」和「运行态」。
 * 禁用任务被手动运行时，排队中 / 运行中的 status 是 0.5 / 2，与启用任务一模一样，
 * 于是运行期间下拉第一项显示成「禁用」（实际仍是禁用），点下去还会走「运行结束后禁用」的分支。
 * 服务端 v3.2.8 起在列表项与任务详情里下发 enabled（契约 C1）专门表达开关位；
 * 老后端 / 未同步的演示站不下发时退回原来的 status !== 0，行为与改动前逐字一致。
 */
export function isTaskSwitchOn(task: any): boolean {
  return typeof task?.enabled === 'boolean' ? task.enabled : task?.status !== 0
}

export function splitTaskLabels(labels: string[] = []) {
  const editableLabels: string[] = []
  const internalLabels: string[] = []
  const groupName = getTaskGroupName(labels)

  for (const label of labels) {
    if (!label) continue
    if (isInternalTaskLabel(label)) {
      internalLabels.push(label)
      continue
    }
    editableLabels.push(label)
  }

  return {
    editableLabels: uniqueLabels(editableLabels),
    internalLabels: uniqueLabels(internalLabels),
    groupName,
  }
}

export function mergeTaskLabels(editableLabels: string[] = [], internalLabels: string[] = [], groupName = '') {
  // 旧的分组标签（含带前导空格的脏分组）一律去掉，再按「任务分组」框的值写回一条干净的 `分组:X`（#157 ③）
  const merged = [...editableLabels, ...internalLabels.filter(label => !label.trim().startsWith(TASK_GROUP_LABEL_PREFIX))]
  const groupLabel = toTaskGroupLabel(groupName)
  if (groupLabel) merged.push(groupLabel)
  return uniqueLabels(merged)
}

/**
 * 规整「裸数组 [{ name, count }]」形状的接口响应：GET /tasks/groups（#130）与 GET /tasks/labels（#157）都是这个形状，
 * 任务表单的「已有分组」与标签选择器的「已有标签」共用。写法照 ViewManager.vue 的 normalizeTaskGroups：
 *   - 不是数组（演示站没注册时会兜底回 { data: [] } 这种对象）一律当成空；请求失败（老面板 404 等）由调用方 catch 静默；
 *   - name 必须是 trim 后非空的字符串，重名只留第一条（模板拿 name 当 :key，重复会让 Vue 报 Duplicate keys）；
 *   - count 不是有限数字时按 0。
 * 顺序保持服务端给的（按 name 字节序），要别的排法由调用方自己排。
 */
export function normalizeTaskNameCounts(raw: unknown): { name: string; count: number }[] {
  if (!Array.isArray(raw)) return []
  const seen = new Set<string>()
  const result: { name: string; count: number }[] = []
  for (const item of raw) {
    const name = typeof item?.name === 'string' ? item.name.trim() : ''
    if (!name || seen.has(name)) continue
    seen.add(name)
    const count = Number(item?.count)
    result.push({ name, count: Number.isFinite(count) ? count : 0 })
  }
  return result
}
