import request from './request'

export const userApi = {
  list() {
    return request.get('/users') as Promise<{ data: any[] }>
  },

  create(data: { username: string; password: string; role?: string }) {
    return request.post('/users', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: { role?: string; enabled?: boolean }) {
    return request.put(`/users/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/users/${id}`) as Promise<{ message: string }>
  },

  resetPassword(id: number, password: string) {
    return request.put(`/users/${id}/reset-password`, { password }) as Promise<{ message: string }>
  }
}

export const securityApi = {
  loginLogs(params?: { page?: number; page_size?: number; username?: string }) {
    return request.get('/security/login-logs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  clearLoginLogs() {
    return request.delete('/security/login-logs') as Promise<{ message: string }>
  },

  sessions() {
    return request.get('/security/sessions') as Promise<{ data: any[] }>
  },

  revokeSession(id: number) {
    return request.delete(`/security/sessions/${id}`) as Promise<{ message: string }>
  },

  revokeAllSessions() {
    return request.delete('/security/sessions/others') as Promise<{ message: string }>
  },

  ipWhitelist() {
    return request.get('/security/ip-whitelist') as Promise<{ data: any[] }>
  },

  addIPWhitelist(data: { ip: string; remarks?: string }) {
    return request.post('/security/ip-whitelist', data) as Promise<{ message: string; data: any }>
  },

  removeIPWhitelist(id: number) {
    return request.delete(`/security/ip-whitelist/${id}`) as Promise<{ message: string }>
  },

  auditLogs(params?: { page?: number; page_size?: number; action?: string }) {
    return request.get('/security/audit-logs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  loginStats(days?: number) {
    return request.get('/security/login-stats', { params: { days } }) as Promise<{ data: any }>
  },

  setup2FA() {
    return request.post('/security/2fa/setup') as Promise<{ data: { secret: string; uri: string } }>
  },

  verify2FA(code: string) {
    return request.post('/security/2fa/verify', { code }) as Promise<{ message: string }>
  },

  disable2FA(code: string) {
    return request.delete('/security/2fa', { data: { code } }) as Promise<{ message: string }>
  },

  get2FAStatus() {
    return request.get('/security/2fa/status') as Promise<{ data: { enabled: boolean } }>
  }
}
