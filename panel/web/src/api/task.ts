import request from './request'

export const taskApi = {
  list(params?: { keyword?: string; status?: number | string; label?: string; page?: number; page_size?: number; filters?: string; sort_rules?: string; all?: 0 | 1 }) {
    return request.get('/tasks', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  notificationChannels() {
    return request.get('/tasks/notification-channels') as Promise<{ data: { id: number; name: string; type: string; enabled: boolean }[] }>
  },

  create(data: any) {
    return request.post('/tasks', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: any) {
    return request.put(`/tasks/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/tasks/${id}`) as Promise<{ message: string }>
  },

  run(id: number) {
    return request.put(`/tasks/${id}/run`) as Promise<{ message: string }>
  },

  stop(id: number) {
    return request.put(`/tasks/${id}/stop`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/tasks/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/tasks/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  pin(id: number) {
    return request.put(`/tasks/${id}/pin`) as Promise<{ message: string }>
  },

  unpin(id: number) {
    return request.put(`/tasks/${id}/unpin`) as Promise<{ message: string }>
  },

  copy(id: number) {
    return request.post(`/tasks/${id}/copy`) as Promise<{ message: string; data: any }>
  },

  latestLog(id: number) {
    return request.get(`/tasks/${id}/latest-log`) as Promise<any>
  },

  liveLogs(id: number) {
    return request.get(`/tasks/${id}/live-logs`) as Promise<{ logs: string[]; done: boolean; status: number }>
  },

  logFiles(id: number) {
    return request.get(`/tasks/${id}/log-files`) as Promise<any[]>
  },

  logFileContent(id: number, filename: string, path?: string) {
    return request.get(`/tasks/${id}/log-files/${encodeURIComponent(filename)}`, { params: path ? { path } : undefined }) as Promise<{ filename: string; content: string }>
  },

  deleteLogFile(id: number, filename: string, path?: string) {
    return request.delete(`/tasks/${id}/log-files/${encodeURIComponent(filename)}`, { params: path ? { path } : undefined }) as Promise<{ message: string }>
  },

  stats(id: number, days?: number) {
    return request.get(`/tasks/${id}/stats`, { params: { days } }) as Promise<any>
  },

  batch(ids: number[], action: string) {
    return request.put('/tasks/batch', { ids, action }) as Promise<{ message: string; count: number }>
  },

  batchEnable(taskIds: number[]) {
    return request.put('/tasks/batch/enable', { task_ids: taskIds }) as Promise<{ message: string; success_count: number }>
  },

  batchDisable(taskIds: number[]) {
    return request.put('/tasks/batch/disable', { task_ids: taskIds }) as Promise<{ message: string; success_count: number }>
  },

  batchDelete(taskIds: number[]) {
    return request.delete('/tasks/batch/delete', { data: { task_ids: taskIds } }) as Promise<{ message: string; count: number }>
  },

  batchRun(taskIds: number[]) {
    return request.post('/tasks/batch/run', { task_ids: taskIds }) as Promise<{ message: string; count: number }>
  },

  batchAddLabels(taskIds: number[], labels: string[]) {
    return request.put('/tasks/batch/add-labels', { task_ids: taskIds, labels }) as Promise<{ message: string; success_count: number }>
  },

  cleanLogs(days?: number) {
    return request.delete('/tasks/clean-logs', { params: { days } }) as Promise<{ message: string }>
  },

  export() {
    return request.get('/tasks/export') as Promise<{ data: any[] }>
  },

  import(tasks: any[]) {
    return request.post('/tasks/import', { tasks }) as Promise<{ message: string; errors: string[] }>
  },

  cronParse(expression: string) {
    return request.post('/tasks/cron/parse', { expression }) as Promise<any>
  },

  cronTemplates() {
    return request.get('/tasks/cron/templates') as Promise<any[]>
  }
}
