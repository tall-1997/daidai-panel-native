import request from './request'

export type EnvPayload = {
  name: string
  value?: string
  remarks?: string
  group?: string
  groups?: string[]
  // 新建时直接标为重要（APP #16）。只在开着时带 true，没开就不带这个键，请求体与以前逐字节相同
  important?: boolean
}

/**
 * PUT /envs/:id 的请求体。字段全是可选的：服务端用指针字段判断，没传的字段不改
 * （App 的编辑只发前 5 个；App 详情弹层的「标为重要 / 取消重要」只发 important）。
 */
export type EnvUpdatePayload = {
  name?: string
  value?: string
  remarks?: string
  group?: string
  groups?: string[]
  enabled?: boolean
  // 「重要」标记（APP #16）：false 是合法修改（取消重要）。编辑弹窗只在用户真拨过开关时才带，
  // 免得弹窗里的旧值冲掉别处（APP、另一个标签页）刚改的标记
  important?: boolean
  // 契约 C5：env 的浮点排序值，同一个置顶桶里越小越靠前；服务端拒绝 NaN / Infinity（400）。
  // 只有用户在编辑弹窗里真的改了才带上，没改就别发，免得把值原样写回一遍。
  // ⚠️ 和下面 sort() 的 position（'before' | 'after'，插入方位）同名不同义，别混用。
  position?: number
}

/** /envs/names 的一项：变量名 + 全库同名条数（不随当前筛选变化） */
export type EnvNameOption = {
  name: string
  count: number
}

export const envApi = {
  // names 是【精确】匹配的变量名筛选（逗号分隔多值，之间是 OR），与 keyword / groups / enabled 之间是 AND。
  // 与 keyword 的 LIKE 模糊搜索不是一回事：names=JD_COOKIE 不会带出 JD_COOKIE_EXTRA。
  list(params?: { keyword?: string; group?: string; groups?: string; names?: string; enabled?: boolean; page?: number; page_size?: number; all?: 0 | 1 }) {
    return request.get('/envs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  get(id: number) {
    return request.get(`/envs/${id}`) as Promise<{ data: any }>
  },

  create(data: EnvPayload | EnvPayload[]) {
    return request.post('/envs', data) as Promise<{ message: string; data: any }>
  },

  update(id: number, data: EnvUpdatePayload) {
    return request.put(`/envs/${id}`, data) as Promise<{ message: string; data: any }>
  },

  delete(id: number) {
    return request.delete(`/envs/${id}`) as Promise<{ message: string }>
  },

  enable(id: number) {
    return request.put(`/envs/${id}/enable`) as Promise<{ message: string; data: any }>
  },

  disable(id: number) {
    return request.put(`/envs/${id}/disable`) as Promise<{ message: string; data: any }>
  },

  batchDelete(ids: number[]) {
    return request.delete('/envs/batch', { data: { ids } }) as Promise<{ message: string }>
  },

  batchRename(ids: number[], name: string) {
    return request.put('/envs/batch/rename', { ids, name }) as Promise<{ message: string }>
  },

  batchEnable(ids: number[]) {
    return request.put('/envs/batch/enable', { ids }) as Promise<{ message: string }>
  },

  batchDisable(ids: number[]) {
    return request.put('/envs/batch/disable', { ids }) as Promise<{ message: string }>
  },

  batchSetGroup(ids: number[], groups: string[]) {
    return request.put('/envs/batch/group', { ids, groups }) as Promise<{ message: string }>
  },

  // 契约 C4（与 /tasks/sort 同名同义）：position 缺省按 'before'，把 source 插到 target 前面；
  // 'after' 插到 target 后面。target 与 source 必须同一个置顶桶（sort_order 相同），跨桶服务端回 400。
  // targetId 留空 = 移到本桶末尾（此时 position 不起作用）：这是对外文档化的契约（apiData.ts 有写），
  // APP v1.4.0 起在没有筛选时拖到末尾就发空 target，别改语义。
  // Web 端已经不用它了：分页或筛选时，「本桶末尾」不等于可见列表的末尾，见 envs/index.vue 的 onEnd。
  // position 为 undefined 时 JSON 里不带这个键，和加这个参数之前发的请求逐字节相同。
  sort(sourceId: number, targetId?: number, position?: 'before' | 'after') {
    return request.put('/envs/sort', { source_id: sourceId, target_id: targetId, position }) as Promise<{ message: string }>
  },

  moveToTop(id: number) {
    return request.put(`/envs/${id}/move-top`) as Promise<{ message: string }>
  },

  cancelTop(id: number) {
    return request.put(`/envs/${id}/cancel-top`) as Promise<{ message: string }>
  },

  groups() {
    return request.get('/envs/groups') as Promise<{ data: string[] }>
  },

  names() {
    return request.get('/envs/names') as Promise<{ data: EnvNameOption[] }>
  },

  export(ids?: number[]) {
    return request.get('/envs/export', { params: ids?.length ? { ids: ids.join(',') } : undefined }) as Promise<{ data: Record<string, string> }>
  },

  exportAll(ids?: number[]) {
    return request.get('/envs/export-all', { params: ids?.length ? { ids: ids.join(',') } : undefined }) as Promise<{ data: any[] }>
  },

  exportFiles(format?: string, enabledOnly?: boolean, ids?: number[]) {
    return request.post('/envs/export-files', { format, enabled_only: enabledOnly, ids }) as Promise<{ data: Record<string, string> }>
  },

  import(envs: any[], mode?: string) {
    return request.post('/envs/import', { envs, mode }) as Promise<{ message: string; errors: string[] }>
  }
}
