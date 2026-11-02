import { nextTick, onScopeDispose, readonly, ref, watch, type Ref } from 'vue'
import { readListPreference } from '@/utils/listPreferences'

/**
 * 日志自动跟随（issue #133 功能 1）。
 *
 * 语义（青龙式）：最新一行在可视区内就继续跟随、贴到底部；用户往上翻超过阈值就暂停，
 * 滚回距底不超过阈值又恢复。抽成组合式是因为全站有 6+ 处实时日志视图都要同一套判定
 * （任务实时日志、执行日志详情、依赖安装日志、订阅拉取日志、系统命令行、脚本调试 / 代码运行等）。
 *
 * 关键点：
 * - 区分「程序滚动」与「用户滚动」靠**写入后读回的 scrollTop 打标记**，而不是 scrollHeight——
 *   内容增长不改 scrollTop、不产生 scroll 事件，所以不会把跟随误判成暂停；程序贴底那一次 scroll
 *   事件靠位置比对认出来，即使同一帧里内容又长了一截也能认对。
 * - 数学是同步的，与模板无关，因此不用 IntersectionObserver 哨兵、也不默认挂 MutationObserver。
 * - 「整体重置内容」时（重连 / 切任务先清空正文），那一帧 scrollTop 会被浏览器夹到 0、触发一次非程序性
 *   scroll。容器此刻没有可滚动的范围，handleScroll 对这种 scroll 一律不改跟随态——只靠 keepFollowing 挡不住：
 *   事件到达时 begin(true, …) 早已把 live 置回 true，按「距底 0」算会把已暂停的用户误判回跟随。
 *   keepFollowing 仍然要传，它负责让 begin() 不按 running 重置 following。
 */

export interface LogAutoFollowOptions {
  /** 距底多少像素以内算「最新一行仍在可视区」。默认 40px（各日志容器的下内边距 + 行高算下来落在 33~40）。 */
  threshold?: number
}

export interface LogAutoFollowController {
  /** 当前是否处于跟随态（只读）。 */
  following: Readonly<Ref<boolean>>
  /**
   * 开始一次日志会话。
   * @param running 内容是否还会继续增长（即任务运行中）。false 时等同于一次静态展示。
   * @param o.keepFollowing 显式指定初始跟随态（重连时用来保住暂停/跟随选择）；缺省时跟 running 一致。
   */
  begin(running: boolean, o?: { keepFollowing?: boolean }): void
  /** 内容追加后调用：跟随中则在下一 tick 贴到底部；暂停时什么都不做。 */
  onContentChange(): void
  /** 手动跳到最新并恢复跟随（给可能的「↓ 最新」按钮用）。 */
  jumpToLatest(): void
  /** 带程序标记地写入 scrollTop（用于展开完整日志、重连恢复位置等补偿滚动）。 */
  setScrollTop(top: number): void
  /** 结束会话：冻结跟随态，此后滚动不再改变它，也不再自动贴底。 */
  end(): void
  /**
   * 打开一份【已结束】的日志、正文一次性写进去之后调用（#147）。
   * 账户偏好「打开已结束的日志时定位到底部」开着就贴底一次；关着什么都不做，保持 #133 起「停在顶部」。
   * 只贴这一次、不进入跟随：live / following 都不动（调用前已 end() 冻结），之后怎么滚全由用户。
   */
  revealFinished(): void
}

const DEFAULT_THRESHOLD = 40
// 程序写入后读回的 scrollTop 与实际 scroll 事件里的值允许 1px 误差（浏览器夹紧 / 亚像素）
const PROGRAMMATIC_EPSILON = 1

export function useLogAutoFollow(
  containerRef: Ref<HTMLElement | null | undefined>,
  opts: LogAutoFollowOptions = {},
): LogAutoFollowController {
  const threshold = opts.threshold ?? DEFAULT_THRESHOLD

  const following = ref(false)
  // 内容是否还会继续增长（任务运行中）。end() 后置 false：滚动不再改跟随态，也不再自动贴底。
  let live = false
  // 最近一次「程序写入 scrollTop」后读回的值，用来把自己造成的那次 scroll 事件与用户滚动区分开。
  let programmaticTop: number | null = null

  // 当前挂着监听的元素。destroy-on-close 弹窗每次重开会换一个新容器，靠 watch 重新挂/摘。
  let observed: HTMLElement | null = null
  let resizeObserver: ResizeObserver | null = null

  function stickToBottom() {
    const el = containerRef.value
    if (!el) return
    el.scrollTop = el.scrollHeight
    // 存的是浏览器夹紧之后读回的值，而不是刚写进去的 scrollHeight：
    // 比对 scroll 事件时才对得上（超长内容会被夹到最大可滚位置）。
    programmaticTop = el.scrollTop
  }

  function scheduleStickToBottom() {
    void nextTick(() => {
      // destroy-on-close 弹窗重开的第一帧容器可能还没挂上（与现有 LogViewer 的 rAF 兜底同一个坑）
      if (!containerRef.value) {
        if (typeof requestAnimationFrame === 'function') {
          requestAnimationFrame(() => {
            if (containerRef.value) stickToBottom()
          })
        }
        return
      }
      stickToBottom()
    })
  }

  function handleScroll() {
    const el = observed
    if (!el) return
    if (
      programmaticTop !== null &&
      Math.abs(el.scrollTop - programmaticTop) <= PROGRAMMATIC_EPSILON
    ) {
      // 这次 scroll 是我们自己写 scrollTop 造成的，不当作用户操作
      programmaticTop = null
      return
    }
    programmaticTop = null
    // 冻结后（end()）滚动不再改变跟随态
    if (!live) return
    // 没有可滚动的范围时用户不可能滚得动：这次 scroll 只可能是正文被清空后浏览器把 scrollTop 夹回 0，
    // 不能拿它改跟随态（否则重连清空正文的那一帧会把已暂停的用户误判回跟随，见文件头说明）
    if (el.scrollHeight - el.clientHeight <= PROGRAMMATIC_EPSILON) return
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight
    following.value = distanceToBottom <= threshold
  }

  function handleResize() {
    // 全屏切换、窗口缩放、移动端弹出键盘等导致高度变化时，跟随中就重新贴底
    if (live && following.value) stickToBottom()
  }

  function attach(el: HTMLElement) {
    observed = el
    el.addEventListener('scroll', handleScroll, { passive: true })
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(handleResize)
      resizeObserver.observe(el)
    }
    // 容器（重新）出现时，若正在跟随就立即贴底
    if (following.value) stickToBottom()
  }

  function detach() {
    if (observed) {
      observed.removeEventListener('scroll', handleScroll)
      observed = null
    }
    if (resizeObserver) {
      resizeObserver.disconnect()
      resizeObserver = null
    }
  }

  // 容器出现时挂监听、消失时摘监听，适配 destroy-on-close 弹窗；flush:'post' 保证 DOM 已就绪
  watch(
    containerRef,
    (el) => {
      detach()
      if (el) attach(el)
    },
    { flush: 'post', immediate: true },
  )

  function begin(running: boolean, o?: { keepFollowing?: boolean }) {
    live = running
    following.value = o?.keepFollowing ?? running
    if (following.value) scheduleStickToBottom()
  }

  function onContentChange() {
    if (live && following.value) scheduleStickToBottom()
  }

  function jumpToLatest() {
    following.value = true
    scheduleStickToBottom()
  }

  function setScrollTop(top: number) {
    const el = containerRef.value
    if (!el) return
    el.scrollTop = top
    programmaticTop = el.scrollTop
  }

  function end() {
    live = false
  }

  function revealFinished() {
    // 走 scheduleStickToBottom：nextTick 等正文 patch 完，容器还没挂上时再用 rAF 兜底（destroy-on-close 弹窗）。
    // stickToBottom 会打程序标记，而且此时 live=false，handleScroll 不会借这次 scroll 改跟随态。
    if (readListPreference('log_open_at_bottom')) scheduleStickToBottom()
  }

  onScopeDispose(() => {
    detach()
  })

  return {
    following: readonly(following),
    begin,
    onContentChange,
    jumpToLatest,
    setScrollTop,
    end,
    revealFinished,
  }
}
