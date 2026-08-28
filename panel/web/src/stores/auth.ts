import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api/auth'
import router from '@/router'
import type { GeeTestValidateResult } from '@/utils/geetest'
import {
  advanceSessionGeneration,
  getSessionGeneration,
  isCurrentSession,
  refreshSessionSingleFlight,
} from '@/utils/authSession'

interface User {
  id: number
  username: string
  role: string
  enabled: boolean
  avatar_url: string
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export const useAuthStore = defineStore('auth', () => {
  const validStoredToken = (value: string | null) => value && !['undefined', 'null'].includes(value) ? value : ''
  const accessToken = ref(validStoredToken(localStorage.getItem('access_token')))
  const refreshToken = ref(validStoredToken(localStorage.getItem('refresh_token')))
  const user = ref<User | null>(null)

  const isLoggedIn = computed(() => !!accessToken.value)

  function setTokens(access: string, refresh: string) {
    if (!access || !refresh) throw new Error('认证响应格式无效')
    advanceSessionGeneration()
    accessToken.value = access
    refreshToken.value = refresh
    localStorage.setItem('access_token', access)
    localStorage.setItem('refresh_token', refresh)
  }

  function setUser(u: User) {
    user.value = u
  }

  function clearAuth() {
    advanceSessionGeneration()
    accessToken.value = ''
    refreshToken.value = ''
    user.value = null
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
  }

  async function login(username: string, password: string, totpCode?: string, captcha?: GeeTestValidateResult | null) {
    const res = await authApi.login(username, password, totpCode, captcha)
    if (!res.user || typeof res.user !== 'object') throw new Error('认证响应格式无效')
    setTokens(res.access_token, res.refresh_token)
    setUser(res.user)
    return res
  }

  async function logout() {
    try {
      await authApi.logout()
    } finally {
      clearAuth()
      router.push('/login')
    }
  }

  async function fetchUser() {
    const res = await authApi.getUser()
    setUser(res.user)
  }

  async function refreshAccessToken(generation = getSessionGeneration()) {
    return refreshSessionSingleFlight(async () => {
      const res = await authApi.refresh()
      if (!res.access_token) throw new Error('刷新响应格式无效')
      if (!isCurrentSession(generation)) throw new Error('认证会话已变更')
      accessToken.value = res.access_token
      localStorage.setItem('access_token', res.access_token)
      if (res.refresh_token) {
        refreshToken.value = res.refresh_token
        localStorage.setItem('refresh_token', res.refresh_token)
      }
      return res.access_token
    }, generation)
  }

  return {
    accessToken,
    refreshToken,
    user,
    isLoggedIn,
    login,
    logout,
    fetchUser,
    refreshAccessToken,
    clearAuth,
    setTokens,
    setUser
  }
})
