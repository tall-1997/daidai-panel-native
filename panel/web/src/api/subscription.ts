import request from './request'

// 订阅创建/更新的请求体。
//
// 本文件其余接口历史上一直用 `any`：订阅表单是把整个 editForm 展开提交的，字段多、
// 且会随表单增删而变。这里刻意只把**需要前后端逐字对齐的字段**单独声明出来，
// 其余仍原样透传，避免再维护一份注定滞后于表单的全量字段表。
export type SubscriptionPayload = Record<string, any> & {
  // 完整检出（v3.2.0 新增，对应 #110）：
  //   true  = 跳过 sparse-checkout，把整个仓库拉下来（体积可能很大）
  //   缺省 / false = 维持按「指定子目录 / 白名单 / 依赖规则」裁剪的稀疏检出
  // 默认必须是 false，存量订阅升级后行为才不变。
  // 字段名与后端 model.Subscription 的 json tag 逐字一致，改名要两边一起改。
  full_checkout?: boolean
  // 订阅级「自动添加定时任务」三态（v3.2.6 新增，对应 #119）：
  //   'inherit'  = 跟随全局设置 auto_add_cron（默认 true）
  //   'enabled'  = 该订阅强制开启，不看全局
  //   'disabled' = 该订阅强制关闭，不看全局
  // 不传或传非法值一律按 'inherit' 处理（后端静默归一）。
  // 旧布尔字段 auto_add_task 保留但已废弃，不再参与判定。
  auto_add_task_mode?: 'inherit' | 'enabled' | 'disabled'
  // 订阅级「自动删除失效任务」三态，取值与 auto_add_task_mode 完全一致，
  // inherit 时回落到全局设置 auto_del_cron。同样与后端 json tag 逐字一致。
  auto_del_task_mode?: 'inherit' | 'enabled' | 'disabled'
}

export const subscriptionApi = {
  list(params?: { keyword?: string; type?: string; enabled?: boolean; page?: number; page_size?: number }) {
    return request.get('/subscriptions', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  create(data: SubscriptionPayload) {
    return request.post('/subscriptions', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: SubscriptionPayload) {
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
