import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig, AxiosResponse } from 'axios'
import { useAuthStore } from '@/stores/auth'
import router from '@/router'
import { getSessionGeneration, isCurrentSession, isRequestSessionCurrent } from '@/utils/authSession'

type SessionRequestConfig = InternalAxiosRequestConfig & {
  _retry?: boolean
  _sessionGeneration?: number
}

const request: AxiosInstance = axios.create({
  baseURL: '/api',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
    'X-Client-Type': 'web',
    'X-Client-App': 'daidai-panel-web'
  }
})

request.interceptors.request.use(
  (config: InternalAxiosRequestConfig) => {
    const authStore = useAuthStore()
    const sessionConfig = config as SessionRequestConfig
    sessionConfig._sessionGeneration ??= getSessionGeneration()
    if (!isCurrentSession(sessionConfig._sessionGeneration)) {
      throw new axios.CanceledError('认证会话已变更')
    }
    if (typeof window !== 'undefined' && window.location.pathname.startsWith('/local-ui/')) {
      const browserSession = window.sessionStorage.getItem('daidai_browser_session')
      if (browserSession) {
        config.headers['X-Daidai-Browser-Session'] = browserSession
      }
    }
    if (authStore.accessToken) {
      config.headers.Authorization = `Bearer ${authStore.accessToken}`
    }
    return config
  },
  (error) => Promise.reject(error)
)

request.interceptors.response.use(
  (response: AxiosResponse) => {
    const generation = (response.config as SessionRequestConfig)._sessionGeneration
    if (generation !== undefined && !isCurrentSession(generation)) {
      return Promise.reject(new axios.CanceledError('认证会话已变更'))
    }
    return response.data
  },
  async (error) => {
    const originalRequest = error.config as SessionRequestConfig | undefined
    const requestUrl = originalRequest?.url || ''
    const isAuthEndpoint = /\/auth\/(login|init|refresh|check-init|logout)/.test(requestUrl)

    if (error.response?.status === 401 && originalRequest && !originalRequest._retry && !isAuthEndpoint) {
      const generation = originalRequest._sessionGeneration
      if (!isRequestSessionCurrent(generation)) {
        return Promise.reject(new axios.CanceledError('认证会话已变更'))
      }

      const authStore = useAuthStore()

      if (!authStore.refreshToken) {
        authStore.clearAuth()
        void router.push('/login')
        return Promise.reject(error)
      }

      originalRequest._retry = true

      try {
        const newToken = await authStore.refreshAccessToken(generation)
        if (!isCurrentSession(generation)) {
          return Promise.reject(new axios.CanceledError('认证会话已变更'))
        }
        originalRequest.headers.Authorization = `Bearer ${newToken}`
        return request(originalRequest)
      } catch {
        if (isCurrentSession(generation)) {
          authStore.clearAuth()
          void router.push('/login')
        }
        return Promise.reject(error)
      }
    }

    return Promise.reject(error)
  }
)

export default request
