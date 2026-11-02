import request from './request'

// ============================================================================
// 通知渠道字段 schema
//
// 下面这几个类型与服务端 server/model/notify_channel_registry.go 里的
// NotifyFieldDefinition / NotifyChannelDefinition 一一对应，是
// GET /notifications/types 的真实响应形状。
//
// 这份 schema 由服务端持有唯一真源（并由 Go 测试与 notifier.go 实际读取的
// config 键双向绑死），Web 只负责渲染，不再保留任何硬编码副本。
//
// 不做版本兼容降级：Web 与服务端永远同版本发布 —— release 产物里 web/ 目录和
// 二进制来自同一次构建，Docker 镜像同理，面板自更新脚本也是整个 web/ 目录替换。
// 不存在「新 Web + 老服务端」的组合。
// ============================================================================

/**
 * 服务端只会下发这四种控件。
 * 渲染器对不认识的值一律降级成普通输入框（绝不隐藏字段），所以服务端若要加第五种，
 * 必须在同一个版本里同步更新这里。
 */
export type NotifyFieldWidget = 'input' | 'password' | 'textarea' | 'select'

export interface NotifyFieldOption {
  value: string
  label: string
}

/**
 * 字段显隐条件，语义固定为「单键等值命中」：
 * 同一渠道内 key 这个字段的当前值命中 values 之一时才显示本字段。
 * 服务端刻意不支持表达式，客户端也不要自行扩展。
 */
export interface NotifyFieldCondition {
  key: string
  values: string[]
}

export interface NotifyFieldDefinition {
  key: string
  label: string
  widget: NotifyFieldWidget
  placeholder?: string
  /** 留空时服务端会直接返回错误的字段 */
  required?: boolean
  /** 服务端在该字段为空时实际使用的回退值（填这个值与留空的最终行为一致） */
  default?: string
  options?: NotifyFieldOption[]
  show_when?: NotifyFieldCondition
}

export interface NotifyChannelDefinition {
  type: string
  name: string
  /** 语义图标名（如 mail / telegram），Web 目前用自己的配色表，暂未消费 */
  icon?: string
  fields?: NotifyFieldDefinition[]
}

/**
 * 渠道推送范围。
 *
 * - `default`：参与广播。资源告警、登录通知、静默更新结果，以及没有绑定渠道的任务通知
 *   都会发到这类渠道。
 * - `bound`：不参与广播，只有任务显式绑定它、或调用方显式传了 channel_id 时才推送。
 *   「一个脚本对应一个通知」靠的就是它。
 *
 * 服务端把空串也当作 `default`（老库升级后的存量渠道就是这种），
 * 所以读取时一律用 `row.push_scope === 'bound'` 判断，不要写成 `!== 'default'`。
 */
export type NotifyPushScope = 'default' | 'bound'

export const notificationApi = {
  list() {
    return request.get('/notifications') as Promise<{ data: any[] }>
  },

  create(data: { name: string; type: string; config: string; push_scope?: NotifyPushScope }) {
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
    return request.get('/notifications/types') as Promise<{ data: NotifyChannelDefinition[] }>
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
