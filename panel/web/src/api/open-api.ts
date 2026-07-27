import request from './request'

export const openApiApi = {
  list: () => request.get('/open-api/apps'),
  create: (data: { name: string; scopes?: string; rate_limit?: number }) =>
    request.post('/open-api/apps', data),
  update: (id: number, data: { name?: string; scopes?: string; rate_limit?: number }) =>
    request.put(`/open-api/apps/${id}`, data),
  delete: (id: number) => request.delete(`/open-api/apps/${id}`),
  enable: (id: number) => request.put(`/open-api/apps/${id}/enable`),
  disable: (id: number) => request.put(`/open-api/apps/${id}/disable`),
  resetSecret: (id: number) => request.put(`/open-api/apps/${id}/reset-secret`),
  viewSecret: (id: number, password: string) =>
    request.post(`/open-api/apps/${id}/view-secret`, { password }),
  callLogs: (id: number, params?: { page?: number; page_size?: number }) =>
    request.get(`/open-api/apps/${id}/logs`, { params }),
}
