import { h } from 'vue'
import { ElMessage } from 'element-plus'
import type { MessageHandler } from 'element-plus'

/**
 * 轻提示统一出口。
 *
 * 【为什么要有这一层，而不是各页面直接 ElMessage】
 * 1. 时长应该跟「这条信息要不要读」挂钩。EP 默认一律 3000ms：成功提示嫌久（用户已经
 *    看到结果了），错误提示又嫌短（一句报错还没读完就没了）。这里按语义分档。
 * 2. 「带操作的轻提示」需要往 message 里塞 VNode 并拿到 handler 才能在点击后关掉，
 *    这段样板不该在每个调用点重写一遍。
 * 3. 一个统一出口意味着以后要换位置、加防重、接入无障碍播报，只改一处。
 *
 * 【与现存 ElMessage 调用点的关系】
 * 全站已有几百处直接 ElMessage 调用，本文件【不要求】它们立刻迁移 —— 观感与动效的
 * 统一是靠 global.scss 里的 `.el-message` 全局增强做的，那一层对新老调用点一视同仁。
 * 这个出口只在「需要按语义分档时长」或「需要带操作按钮」时才有必要用上。
 */

/** 按「这条信息要不要读完」分档，而不是一刀切 3000ms */
const DURATION = {
  /** 成功：用户已经从界面变化上看到结果了，提示只是确认，尽快让路 */
  success: 2500,
  info: 3000,
  /** 警告：通常带条件说明，要给读的时间 */
  warning: 4500,
  /** 错误：一句后端报错往往二三十个字，短了根本读不完 */
  error: 6000,
  /** 带操作：用户要先读懂、再决定点不点，最长 */
  action: 8000,
} as const

export interface ToastAction {
  /** 按钮文字，控制在 2-4 个字，长了会把提示条撑宽 */
  text: string
  /**
   * 返回值声明成 unknown 而不是 `void | Promise<void>`。
   *
   * 后者看着更严谨，实际会逼调用方到处加大括号：`() => router.push(x)` 返回
   * Promise<NavigationFailure|void>、`() => ElMessage.success(x)` 返回 MessageHandler、
   * `() => ElMessageBox.alert(...)` 返回 Promise<MessageBoxData>——这些都是最自然的写法，
   * 却全都通不过 void 检查，只能写成 `() => { router.push(x) }`。
   * 这里的返回值本来就没人用（下面是 `void action.handler()`），不该为它增加调用负担。
   */
  handler: () => unknown
  /** 点击后是否自动关掉提示，默认 true */
  closeOnClick?: boolean
}

export interface ToastOptions {
  /** 覆盖默认时长；传 0 表示不自动关闭（会自动显示关闭按钮） */
  duration?: number
  /**
   * 相同内容是否合并成一条并在右侧累加计数。
   * 批量操作里循环报错的场景开着它，否则会刷出一屏一模一样的提示。
   */
  grouping?: boolean
  /** 带操作的轻提示：在文字右侧挂一个可点击的次要动作 */
  action?: ToastAction
  /**
   * 提示关闭时回调。自动到期、用户点关闭按钮、点了操作按钮后自动关，三种情况都会触发。
   * 用于「提示还在不在」会影响后续行为的场景（比如按提示的停留时长做去重的调用方，
   * 提示被提前关掉后要把去重窗口撤掉）。
   */
  onClose?: () => void
}

type ToastType = 'success' | 'info' | 'warning' | 'error'

function resolveDuration(type: ToastType, options?: ToastOptions) {
  if (options?.duration !== undefined) return options.duration
  if (options?.action) return DURATION.action
  return DURATION[type]
}

function show(type: ToastType, message: string, options?: ToastOptions): MessageHandler {
  const duration = resolveDuration(type, options)

  if (!options?.action) {
    return ElMessage({
      type,
      message,
      duration,
      // 不自动关闭时必须给关闭按钮，否则提示会永久占住页面顶部
      showClose: duration === 0,
      grouping: options?.grouping ?? false,
      onClose: options?.onClose,
    })
  }

  const action = options.action
  // handler 要在 onClick 里被引用，而 onClick 只会在 ElMessage 返回之后才可能触发，
  // 所以这里先声明后赋值是安全的。
  let handler: MessageHandler | null = null

  const content = h('span', { class: 'dd-toast' }, [
    h('span', { class: 'dd-toast__text' }, message),
    h(
      'button',
      {
        class: 'dd-toast__action',
        type: 'button',
        onClick: () => {
          if (action.closeOnClick !== false) handler?.close()
          void action.handler()
        },
      },
      action.text
    ),
  ])

  handler = ElMessage({
    type,
    message: content,
    duration,
    // 带操作的提示停留更久，必须让用户能主动关掉
    showClose: true,
    // 带 VNode 的提示不能合并：EP 的 grouping 按内容判重，VNode 每次都是新对象，
    // 开着它只会让判重逻辑空转，还可能把不同的动作按钮错误地折叠到一起。
    grouping: false,
    onClose: options.onClose,
  })

  return handler
}

export const toast = {
  success: (message: string, options?: ToastOptions) => show('success', message, options),
  info: (message: string, options?: ToastOptions) => show('info', message, options),
  warning: (message: string, options?: ToastOptions) => show('warning', message, options),
  error: (message: string, options?: ToastOptions) => show('error', message, options),

  /**
   * 带操作的轻提示的快捷写法。
   * 语义默认是 info —— 「做完了一件事，顺手给你一个后续入口」。
   *
   * @example
   * toast.withAction('已导出 12 个任务', { text: '查看', handler: () => router.push('/tasks') })
   */
  withAction: (message: string, action: ToastAction, options?: Omit<ToastOptions, 'action'>) =>
    show('info', message, { ...options, action }),
}

export default toast
