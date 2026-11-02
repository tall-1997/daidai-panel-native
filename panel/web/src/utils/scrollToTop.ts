/**
 * 列表翻页后回到顶部（v3.3.1，issue #143 O3，桌面与移动端都做）。
 *
 * 为什么不能「滚某一个固定容器」：真正在滚的元素随断点和页面类型而变 ——
 *   - ≤768：外层的 .layout-main 在滚（MainLayout.vue），.route-shell 与页面根都是 overflow: visible；
 *   - ≥769 的 dd-scroll-page 页（deps / open-api / 设置等）：页面根自己在滚（global.scss）；
 *   - ≥769 的 dd-fixed-page 页（tasks / logs / envs / 订阅等）：页面根 overflow: hidden，
 *     滚的是它里面的 .table-card，el-table 定了高度时则是 .el-table__body-wrapper .el-scrollbar__wrap；
 *   - 桌面的 .layout-main / .route-shell 都是 overflow: hidden，scrollTop 恒为 0。
 * 所以这里把「可能在滚的」都收集起来，只处理 scrollTop > 0 的那几个，其余原样不动。
 */

/** 页面根内部、桌面 dd-fixed-page 下可能在滚的容器 */
const INNER_SCROLLER_SELECTOR = '.table-card, .el-table__body-wrapper .el-scrollbar__wrap'

/**
 * 找当前活动的页面根：.route-shell 下不在离场过渡中的那一个。
 * 切页时新旧两页会短暂并存（旧页挂 page-shell-leave-active 淡出，见 MainLayout.vue），不能取到旧的；
 * 被 keep-alive 缓存的页面 DOM 已脱离文档，querySelector 本来就找不到。
 */
function findActivePageRoot(): Element | null {
  return document.querySelector('.route-shell > :not(.page-shell-leave-active)')
}

/**
 * 翻页后把列表滚回顶部。只应挂在分页组件的 current-change / size-change 上，并在数据回来之后调用。
 *
 * 🔴 不能挂进 loadXxx 或 watch(page)：自动刷新、轮询也会调它们，用户正往下看着，会被一把拽回顶部。
 * EP 分页器只在「用户点页码 / 改每页条数 / total 变小导致页码被夹回」时才 emit 这两个事件，
 * 程序直接改 v-model 不会触发，所以挂在这两个事件上正好等于「用户翻页」。
 *
 * @param anchor 页面根元素（传页面根的 ref）；不传时自动取当前活动页面的根。
 * @param extraTargets 额外要一起回顶的滚动容器（上面几类覆盖不到的特殊页面用）。
 */
export function scrollListToTop(
  anchor?: Element | null,
  extraTargets?: Array<Element | null | undefined>
): void {
  if (typeof document === 'undefined') return
  const root = anchor ?? findActivePageRoot()
  if (!(root instanceof HTMLElement)) return
  // offsetParent 为 null：页面被 keep-alive 失活（DOM 已脱离文档）或被隐藏，量出来全是 0，滚了也白滚。
  // 也挡住「数据回来时用户已经切走了」的情况：异步翻页回调晚到时，别去动别的页面的滚动位置。
  if (root.offsetParent === null) return

  const targets = new Set<Element>()

  // ① 锚点自身及整条祖先链：覆盖移动端的 .layout-main、桌面 dd-scroll-page 的页面根。
  // 走到弹窗的遮罩层（.el-overlay）就停：锚点在弹窗里时，遮罩之外是弹窗背后的页面，
  // 在弹窗里翻页不该把背后列表的阅读位置也一并拽回顶部（本项目弹窗默认不 teleport，背后就是 .layout-main）。
  for (let el: Element | null = root; el; el = el.parentElement) {
    targets.add(el)
    if (el.classList.contains('el-overlay')) break
  }

  // ② 锚点内部的表格滚动容器：覆盖桌面 dd-fixed-page
  root.querySelectorAll(INNER_SCROLLER_SELECTOR).forEach((el) => targets.add(el))

  // ③ 调用方额外指定的容器
  extraTargets?.forEach((el) => {
    if (el) targets.add(el)
  })

  targets.forEach((el) => {
    if (el.scrollTop > 0) {
      // 一律瞬时，不做平滑滚动：翻页时内容整块替换，平滑滚动会和换数据叠在一起，画面发飘。
      // 刻意不用 scrollIntoView：它会连带滚动所有能滚的祖先，还可能横向滚动带 overflow 的容器
      // （理由同 views/scripts/components/ScriptsSidebar.vue 的 scrollRowIntoView）。
      el.scrollTo({ top: 0, behavior: 'auto' })
    }
  })
}
