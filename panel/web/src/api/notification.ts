import request from './request'

export const notificationApi = {
  list() {
    return request.get('/notifications') as Promise<{ data: any[] }>
  },

  create(data: { name: string; type: string; config: string }) {
    return request.post('/notifications', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/notifications/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/notifications/${id}`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/notifications/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/notifications/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  test(id: number) {
    return request.post(`/notifications/${id}/test`) as Promise<{ message: string }>
  },

  types() {
    return request.get('/notifications/types') as Promise<{ data: { type: string; name: string }[] }>
  }
}

export const sshKeyApi = {
  list() {
    return request.get('/ssh-keys') as Promise<{ data: any[] }>
  },

  create(data: { name: string; private_key: string }) {
    return request.post('/ssh-keys', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/ssh-keys/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/ssh-keys/${id}`) as Promise<{ message: string }>
  },

  detail(id: number) {
    return request.get(`/ssh-keys/${id}`) as Promise<{ data: any }>
  }
}
