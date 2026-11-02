import request from './request'
import router from '@/router'
import { useAuthStore } from '@/stores/auth'

export interface AndroidRuntimePreset {
  name: string
  label: string
  arch: string
  url: string
  strip_components: number
  check_bin: string
  size_mb: number
  note?: string
}

export interface AndroidRuntimeItem {
  name: string
  installed: boolean
  path: string
  version: string
}

export interface AndroidRuntimeStatus {
  supported: boolean
  arch: string
  bin_dir: string
  termux_detected: boolean
  runtimes: AndroidRuntimeItem[]
  presets: AndroidRuntimePreset[]
}

async function authorizedFetch(input: string, init?: RequestInit) {
  const authStore = useAuthStore()

  const doFetch = async (token: string) => {
    const headers = new Headers(init?.headers || {})
    if (!headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json')
    }
    if (token) {
      headers.set('Authorization', `Bearer ${token}`)
    } else {
      headers.delete('Authorization')
    }
    return fetch(input, {
      ...init,
      headers,
    })
  }

  let response = await doFetch(authStore.accessToken)
  if (response.status !== 401) {
    return response
  }

  if (!authStore.refreshToken) {
    authStore.clearAuth()
    void router.push('/login')
    throw new Error('登录已过期，请重新登录')
  }

  try {
    const nextToken = await authStore.refreshAccessToken()
    response = await doFetch(nextToken)
  } catch {
    authStore.clearAuth()
    void router.push('/login')
    throw new Error('登录已过期，请重新登录')
  }

  return response
}

export const androidRuntimeApi = {
  status() {
    return request.get('/android-runtime/status') as Promise<{ data: AndroidRuntimeStatus }>
  },
  installStream(name: string, signal?: AbortSignal) {
    return authorizedFetch('/api/v1/android-runtime/install', {
      method: 'POST',
      signal,
      body: JSON.stringify({ name }),
    })
  },
  uninstall(name: string) {
    return request.post('/android-runtime/uninstall', { name }) as Promise<{ message: string }>
  },
}
