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
      eventName = value || 'message'
    } else if (field === 'data') {
      dataLines.push(value)
    }
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
