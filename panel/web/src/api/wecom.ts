import request from './request'

// ============================================================================
// 企业微信接入配置（issue #145，v3.3.2）
//
// 对应服务端 server/handler/wecom_callback.go 里的那一组管理接口
// （GET/POST /wecom/triggers、PUT/DELETE /wecom/triggers/:id，登录 + 管理员）。
//
// ⚠️ 别看见 /wecom 前缀就以为整段免鉴权：同层的 /wecom/callback/:id 是给企业微信服务器
// 回调用的公开路由，这里这一组是普通的管理接口，两者只是共用一个前缀。
// ============================================================================

/**
 * 一条接入配置。
 *
 * 注意少了两个字段：CallbackToken 与 EncodingAESKey 服务端标了 `json:"-"`、ToDict 也没带，
 * **任何接口都不会回显**——泄漏任意一个都等于把这条公网入口交出去。
 * 所以前端只能写入、读不回来，编辑时留空即「不修改」（服务端 UpdateTrigger 对这两个键
 * 专门做了「传了空串一律当没传」的处理）。
 */
export interface WecomTrigger {
  id: number
  name: string
  corp_id: string
  agent_id: string
  /** 回放 /tasks 接口时用的开放 API 应用 ID，它的权限范围必须含 tasks */
  open_app_id: number
  /** 允许被触发的任务白名单，逗号（半角/全角）或换行分隔，留空 = 不限制 */
  task_whitelist: string
  enabled: boolean
  /**
   * 服务端按当前请求前缀拼好的回调路径（如 /api/v1/wecom/callback/3）。
   * 只有 list / create / update 三个接口会带它，不是库里的字段。
   * 前端不自己拼这个路径：面板同时挂着 /api 与 /api/v1 两套前缀，拼错了企业微信后台配不通。
   */
  callback_path?: string
  created_at?: string
  updated_at?: string
}

/**
 * 新建时的请求体。
 * 除 agent_id / task_whitelist / enabled 外都是服务端 binding:"required" 的必填项。
 */
export interface WecomTriggerCreatePayload {
  name: string
  corp_id: string
  agent_id?: string
  callback_token: string
  /** 企业微信后台给出的 43 位字符串，长度不对服务端直接 400 */
  encoding_aes_key: string
  open_app_id: number
  task_whitelist?: string
  enabled?: boolean
}

/**
 * 更新时的请求体：服务端是「按键更新」，请求里没出现的键一概不动已有值。
 * 所以这里全部可选，只传用户真正改过的字段。
 */
export type WecomTriggerUpdatePayload = Partial<WecomTriggerCreatePayload>

export const wecomTriggerApi = {
  list() {
    return request.get('/wecom/triggers') as Promise<{ data: WecomTrigger[] }>
  },

  create(data: WecomTriggerCreatePayload) {
    return request.post('/wecom/triggers', data) as Promise<{ message: string; data: WecomTrigger }>
  },

  update(id: number, data: WecomTriggerUpdatePayload) {
    return request.put(`/wecom/triggers/${id}`, data) as Promise<{ message: string; data: WecomTrigger }>
  },

  delete(id: number) {
    return request.delete(`/wecom/triggers/${id}`) as Promise<{ message: string }>
  },
}
