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

  // ---------------------------------------------------------------------------
  // 企业微信菜单触发链接（issue #145，v3.3.2）
  //
  // 这两条是管理接口（登录 + 管理员）。签发出来的链接本身是免鉴权的：
  // 企业微信内置浏览器打开菜单链接时带不了 Authorization 头，所以它靠票据验签立身。
  // ---------------------------------------------------------------------------

  /**
   * 为某个任务签发一条长期有效的触发链接。
   * 路径上的 id 是「以哪个应用的名义签发」，body 里的 task_id 才是票据真正绑住的任务，
   * 改 URL 上的 task_id 一定验签失败，一张票挪不到别的任务上。
   */
  issueTaskTriggerLink: (id: number, taskId: number) =>
    request.post(`/open-api/apps/${id}/task-trigger-link`, { task_id: taskId }) as Promise<{
      data: { url: string; path: string; task_id: number; task_name: string; app_name: string; notice: string }
    }>,

  /**
   * 作废全部触发链接：把「代次」+1，已发出去的链接立刻失效。
   * 票据是无状态的、不落库，所以只能整批作废，没有逐条撤销。
   */
  revokeTaskTriggerLinks: () =>
    request.post('/open-api/task-trigger-links/revoke') as Promise<{
      message: string
      data: { generation: number }
    }>,
}
