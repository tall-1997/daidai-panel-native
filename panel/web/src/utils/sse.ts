import router from '@/router'
import { useAuthStore } from '@/stores/auth'

export interface EventStreamEvent {
  event: string
  data: string
}

export interface EventStreamHandlers {
  onOpen?: () => void
  onMessage?: (data: string, event: EventStreamEvent) => void
  onEvent?: (event: EventStreamEvent) => void
  onError?: (error: Error) => void
}

export interface EventStreamConnection {
  close: () => void
}

export interface EventStreamRequestOptions {
  method?: string
  headers?: Record<string, string>
  body?: BodyInit | null
}

export function openAuthorizedEventStream(
  url: string,
  handlers: EventStreamHandlers = {},
  requestOptions: EventStreamRequestOptions = {}
): EventStreamConnection {
  // 在线演示 Demo（GitHub Pages 静态站）分叉：没有服务端，下面那发 fetch 必然 404。
  //
  // 为什么必须在【这个函数内部】分叉，而不是各调用点自己判断：
  //   这里是全站唯一的 SSE 传输层，四个调用点（logs/index.vue:268、
  //   tasks/components/LogViewer.vue:164、deps/index.vue:1555、
  //   subscriptions/index.vue:758）全部经由它。在这里换掉，四处同时生效且零改动。
  //   顺带一提：全仓库【没有任何】 `new EventSource`，所以 mock `window.EventSource`
  //   是无效的，别再往那个方向试。
  //
  // 守卫必须保持这个形状（编译期常量 + 动态 import）：
  //   - `import.meta.env.VITE_DEMO` 在发布版构建里被 define 成 ''（见 vite.config.ts），
  //     `'' === '1'` 恒假，rollup 会把整个 if 连同 demo chunk 一起剔除；
  //   - 不能改成运行期判断、也不能抽成返回 boolean 的 isDemo() 再判断（折叠不掉）；
  //   - 不能静态 import '@/demo/sse'（会把整个 demo 层拖进真实用户的产物）。
  //
  // 返回的假连接与真实实现同形状（只有一个 close()），调用方无感知。
  //
  // requestOptions 这里刻意不传下去：现有四个调用点都只用 (url, handlers)。
  // 以后真有调用点开始用 method / body 来区分流的话，要同步把它带给 openDemoEventStream，
  // 否则演示站会静默地按 GET 的语义放同一条假流。
  if (import.meta.env.VITE_DEMO === '1') {
    // 变量名刻意和下面真实实现里的 closed 区分开：这里是块级作用域，
    // 同名的话读代码时很容易误以为是同一个开关。
    let demoClosed = false
    let demoConnection: EventStreamConnection | null = null

    void import('@/demo/sse')
      .then(({ openDemoEventStream }) => {
        // chunk 还在路上时调用方就 close() 了（切任务、关弹窗都可能），此刻不能再起流
        if (demoClosed) return
        demoConnection = openDemoEventStream(url, handlers)
      })
      .catch((error: unknown) => {
        console.error('[demo] 假日志流加载失败:', error)
        handlers.onError?.(toError(error))
      })

    return {
      close() {
        demoClosed = true
        demoConnection?.close()
        demoConnection = null
      }
    }
  }

  const authStore = useAuthStore()
  const controller = new AbortController()
  let closed = false
  let retried = false

  const close = () => {
    if (closed) {
      return
    }
    closed = true
    controller.abort()
  }

  const connect = async () => {
    try {
      const headers: Record<string, string> = {
        Accept: 'text/event-stream',
        Authorization: `Bearer ${authStore.accessToken}`,
        'X-Client-Type': 'web',
        'X-Client-App': 'daidai-panel-web',
        ...(requestOptions.headers || {})
      }

      const response = await fetch(url, {
        method: requestOptions.method || 'GET',
        headers,
        body: requestOptions.body,
        cache: 'no-store',
        signal: controller.signal
      })

      if (response.status === 401 && !retried && authStore.refreshToken) {
        retried = true
        try {
          await authStore.refreshAccessToken()
        } catch {
          authStore.clearAuth()
          router.push('/login')
          throw new Error('登录已过期，请重新登录')
        }
        if (!closed) {
          await connect()
        }
        return
      }

      if (response.status === 401) {
        authStore.clearAuth()
        router.push('/login')
        throw new Error('登录已过期，请重新登录')
      }

      if (!response.ok || !response.body) {
        throw await buildResponseError(response)
      }

      handlers.onOpen?.()
      await consumeEventStream(response.body, handlers, controller.signal)
    } catch (error) {
      if (closed || controller.signal.aborted) {
        return
      }
      handlers.onError?.(toError(error))
    }
  }

  void connect()

  return { close }
}

async function consumeEventStream(
  body: ReadableStream<Uint8Array>,
  handlers: EventStreamHandlers,
  signal: AbortSignal
) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (!signal.aborted) {
    const { value, done } = await reader.read()
    if (done) {
      break
    }

    buffer += decoder.decode(value, { stream: true })

    const segments = buffer.split('\n\n')
    buffer = segments.pop() || ''

    for (const segment of segments) {
      dispatchEventSegment(segment, handlers)
    }
  }

  buffer += decoder.decode()
  if (buffer.trim()) {
    dispatchEventSegment(buffer, handlers)
  }
}

function dispatchEventSegment(segment: string, handlers: EventStreamHandlers) {
  let eventName = 'message'
  const dataLines: string[] = []
  // 服务端会周期性发送以 : 开头的心跳注释行来保活长连接（例如依赖安装日志流）。
  // 这类分段里没有任何 event/data 字段，必须整段丢弃：
  // 否则会被当成一条 data 为空的 message 派发出去，在日志里每隔 30 秒插入一个空行。
  let hasField = false

  for (const rawLine of segment.split('\n')) {
    // 注意：这里不能对 data 行直接 trimEnd()。
    // 任务日志里的进度条会把裸 \r 放在 data 内容末尾，用来表示“回到当前行开头覆盖”。
    // 如果这里把 \r 当普通空白删掉，前端日志组件就再也分不清“覆盖刷新”和“新增一行”了。
    let line = rawLine
    if (line.endsWith('\r') && !line.startsWith('data:')) {
      line = line.slice(0, -1)
    }
    if (!line || line.startsWith(':')) {
      continue
    }

    const colonIndex = line.indexOf(':')
    const field = colonIndex === -1 ? line : line.slice(0, colonIndex)
    let value = colonIndex === -1 ? '' : line.slice(colonIndex + 1)
    if (value.startsWith(' ')) {
      value = value.slice(1)
    }

    if (field === 'event') {
      hasField = true
      eventName = value || 'message'
    } else if (field === 'data') {
      hasField = true
      dataLines.push(value)
    }
  }

  if (!hasField) {
    return
  }

  const event = {
    event: eventName,
    data: dataLines.join('\n')
  }

  handlers.onEvent?.(event)
  if (event.event === 'message') {
    handlers.onMessage?.(event.data, event)
  }
}

async function buildResponseError(response: Response) {
  const contentType = response.headers.get('content-type') || ''

  if (contentType.includes('application/json')) {
    try {
      const data = await response.json() as { error?: string; message?: string }
      return new Error(data.error || data.message || `请求失败（${response.status}）`)
    } catch {
      return new Error(`请求失败（${response.status}）`)
    }
  }

  try {
    const text = (await response.text()).trim()
    return new Error(text || `请求失败（${response.status}）`)
  } catch {
    return new Error(`请求失败（${response.status}）`)
  }
}

function toError(error: unknown) {
  if (error instanceof Error) {
    return error
  }
  return new Error(String(error || '未知错误'))
}
