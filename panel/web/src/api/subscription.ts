import request from './request'

export const subscriptionApi = {
  list(params?: { keyword?: string; type?: string; enabled?: boolean; page?: number; page_size?: number }) {
    return request.get('/subscriptions', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  create(data: any) {
    return request.post('/subscriptions', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/subscriptions/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/subscriptions/${id}`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/subscriptions/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/subscriptions/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  pull(id: number) {
    return request.put(`/subscriptions/${id}/pull`) as Promise<{ message: string }>
  },

  stopPull(id: number) {
    return request.put(`/subscriptions/${id}/pull/stop`) as Promise<{ message: string }>
  },

  logs(id: number, params?: { page?: number; page_size?: number }) {
    return request.get(`/subscriptions/${id}/logs`, { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  batchDelete(ids: number[]) {
    return request.delete('/subscriptions/batch', { data: { ids } }) as Promise<{ message: string }>
  }
}
