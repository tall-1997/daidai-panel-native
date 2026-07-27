import request from './request'

export interface TaskViewFilter {
  field: string
  operator: string
  value: string
}

export interface TaskViewSortRule {
  field: string
  direction: 'asc' | 'desc'
}

export interface TaskView {
  id: number
  name: string
  filters: string
  sort_rules: string
  hidden: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

export interface TaskViewReorderItem {
  id: number
  sort_order: number
  hidden?: boolean
}

export interface TaskViewReorderResponse {
  updated: number
  views: TaskView[]
}

export const taskViewApi = {
  list() {
    return request.get('/tasks/views') as Promise<TaskView[]>
  },

  create(data: { name: string; filters: string; sort_rules: string }) {
    return request.post('/tasks/views', data) as Promise<TaskView>
  },

  update(
    id: number,
    data: { name?: string; filters?: string; sort_rules?: string; hidden?: boolean; sort_order?: number }
  ) {
    return request.put(`/tasks/views/${id}`, data) as Promise<TaskView>
  },

  delete(id: number) {
    return request.delete(`/tasks/views/${id}`) as Promise<{ message: string }>
  },

  reorder(views: TaskViewReorderItem[]) {
    return request.put('/tasks/views/reorder', { views }) as Promise<TaskViewReorderResponse>
  }
}
