import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { authApi } from '@/api/auth'
import { resetEditorPreferencesCache } from '@/utils/editorPreferences'
import { resetListPreferencesCache } from '@/utils/listPreferences'
import router from '@/router'
import type { GeeTestValidateResult } from '@/utils/geetest'

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
  const accessToken = ref(localStorage.getItem('access_token') || '')
  const refreshToken = ref(localStorage.getItem('refresh_token') || '')
  const user = ref<User | null>(null)

  const isLoggedIn = computed(() => !!accessToken.value)

  function setTokens(access: string, refresh: string) {
    accessToken.value = access
    refreshToken.value = refresh
    localStorage.setItem('access_token', access)
    localStorage.setItem('refresh_token', refresh)
  }

  function setUser(u: User) {
    user.value = u
  }

  function clearAuth() {
    accessToken.value = ''
    refreshToken.value = ''
    user.value = null
    localStorage.removeItem('access_token')
    localStorage.removeItem('refresh_token')
    // 编辑器偏好是**按用户**存的，退出时必须把「已经拉过服务端」的记忆清掉，
    // 否则换一个账号登进来会继续吃上一个人的那份缓存。
    // 只清记忆不清 localStorage 里的值：那份值同时还是本机的离线缓存，
    // 下次登录会被服务端值覆盖，没必要在这里制造一次闪变。
    resetEditorPreferencesCache()
    // 列表页偏好（每页条数、视图栏显隐，v3.3.1）同理，也是按用户存的
    resetListPreferencesCache()
  }

  async function login(username: string, password: string, totpCode?: string, captcha?: GeeTestValidateResult | null) {
    const res = await authApi.login(username, password, totpCode, captcha)
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

  async function refreshAccessToken() {
    const res = await authApi.refresh()
    accessToken.value = res.access_token
    localStorage.setItem('access_token', res.access_token)
    return res.access_token
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
