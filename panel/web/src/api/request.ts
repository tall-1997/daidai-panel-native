import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import { useAuthStore } from '@/stores/auth'
import router from '@/router'

const request: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
    'X-Client-Type': 'web',
    'X-Client-App': 'daidai-panel-web'
  }
})

let isRefreshing = false

// 刷新 token 期间的排队请求：同时保存 resolve 与 reject。
// 刷新成功 → 用新 token 重放并 resolve；刷新失败 → 逐个 reject。
// 若只存 resolve（旧实现），刷新失败时清空队列会丢弃回调，既不 resolve 也不 reject，
// 导致并发请求永久挂起、loading 永不结束（会话被顶下线时的“无限加载”根因）。
interface PendingRequest {
  resolve: (token: string) => void
  reject: (error: unknown) => void
}
let pendingRequests: PendingRequest[] = []

function resolvePending(token: string) {
  pendingRequests.forEach(({ resolve }) => resolve(token))
  pendingRequests = []
}

function rejectPending(error: unknown) {
  pendingRequests.forEach(({ reject }) => reject(error))
  pendingRequests = []
}

request.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const authStore = useAuthStore()
    if (authStore.accessToken) {
      config.headers.Authorization = `Bearer ${authStore.accessToken}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

request.interceptors.response.use(
  (response: AxiosResponse) => response.data,
  async (error) => {
    const originalRequest = error.config

    if (error.response?.status === 401 && !originalRequest._retry) {
      const authStore = useAuthStore()

      if (!authStore.refreshToken) {
        authStore.clearAuth()
        router.push('/login')
        return Promise.reject(error)
      }

      if (isRefreshing) {
        // 已有请求在刷新 token，本请求入队等待
        return new Promise((resolve, reject) => {
          pendingRequests.push({
            resolve: (token: string) => {
              // 刷新成功后用新 token 重放；补 _retry 防重放结果再次进入刷新流程
              originalRequest._retry = true
              originalRequest.headers.Authorization = `Bearer ${token}`
              resolve(request(originalRequest))
            },
            reject,
          })
        })
      }

      originalRequest._retry = true
      isRefreshing = true

      try {
        const newToken = await authStore.refreshAccessToken()
        isRefreshing = false
        resolvePending(newToken)
        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return request(originalRequest)
      } catch {
        // 刷新失败（如会话被顶、refresh 已失效）：逐个 reject 排队请求，
        // 让每个调用方各自结束 loading/抛错，再统一清理登录态并跳转登录页
        isRefreshing = false
        rejectPending(error)
        authStore.clearAuth()
        router.push('/login')
        return Promise.reject(error)
      }
    }

    return Promise.reject(error)
  }
)

export default request
