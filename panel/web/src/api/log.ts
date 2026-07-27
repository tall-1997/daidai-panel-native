import request from './request'

export const logApi = {
  list(params?: { task_id?: number; status?: number; keyword?: string; page?: number; page_size?: number }) {
    return request.get('/logs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  detail(id: number) {
    return request.get(`/logs/${id}`) as Promise<any>
  },

  delete(id: number) {
    return request.delete(`/logs/${id}`) as Promise<{ message: string }>
  },

  batchDelete(ids: number[]) {
    return request.post('/logs/batch-delete', { ids }) as Promise<{ message: string }>
  },

  clean(days?: number) {
    return request.delete('/logs/clean', { params: { days } }) as Promise<{ message: string }>
  }
}
