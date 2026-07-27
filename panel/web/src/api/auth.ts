import request from './request'
import axios from 'axios'
import type { GeeTestValidateResult } from '@/utils/geetest'

export const authApi = {
  checkInit() {
    return request.get('/auth/check-init') as Promise<{ need_init: boolean }>
  },

  init(username: string, password: string) {
    return request.post('/auth/init', { username, password }) as Promise<{ message: string; user: any }>
  },

  login(username: string, password: string, totpCode?: string, captcha?: GeeTestValidateResult | null) {
    return request.post('/auth/login', {
      username,
      password,
      totp_code: totpCode || '',
      captcha: captcha || undefined
    }) as Promise<{
      message: string
      access_token: string
      refresh_token: string
      user: any
      two_factor_required?: boolean
      captcha_required?: boolean
    }>
  },

  logout() {
    return request.post('/auth/logout') as Promise<{ message: string }>
  },

  refresh() {
    const refreshToken = localStorage.getItem('refresh_token')
    return axios.post('/api/auth/refresh', null, {
      headers: {
        Authorization: `Bearer ${refreshToken}`,
        'X-Client-Type': 'web',
        'X-Client-App': 'daidai-panel-web'
      }
    }).then(res => res.data) as Promise<{ access_token: string }>
  },

  getUser() {
    return request.get('/auth/user') as Promise<{ user: any }>
  },

  changePassword(oldPassword: string, newPassword: string) {
    return request.put('/auth/password', {
      old_password: oldPassword,
      new_password: newPassword
    }) as Promise<{ message: string }>
  },

  captchaConfig(username?: string) {
    return request.get('/auth/captcha-config', {
      params: username ? { username } : undefined
    }) as Promise<{
      enabled: boolean
      captcha_id: string
      configured: boolean
      implemented: boolean
      required: boolean
      require_after_failures: number
      message: string
    }>
  },

  uploadAvatar(file: File) {
    const formData = new FormData()
    formData.append('avatar', file)
    return request.post('/auth/avatar', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    }) as Promise<{ message: string; avatar_url: string }>
  },

  deleteAvatar() {
    return request.delete('/auth/avatar') as Promise<{ message: string }>
  },

  changeUsername(username: string) {
    return request.put('/auth/username', { username }) as Promise<{ message: string; user: any }>
  }
}
