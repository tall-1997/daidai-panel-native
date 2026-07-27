import request from './request'

export interface BackupSelection {
  configs: boolean
  tasks: boolean
  subscriptions: boolean
  env_vars: boolean
  logs: boolean
  scripts: boolean
  dependencies: boolean
  task_views: boolean
}

export interface RestoreProgressState {
  active: boolean
  status: 'idle' | 'running' | 'completed' | 'failed'
  filename?: string
  source?: string
  selection?: Partial<BackupSelection>
  stage?: string
  message?: string
  percent: number
  error?: string
  started_at?: string
  updated_at?: string
}

export interface PanelUpdateStatus {
  status?: 'idle' | 'running' | 'restarting' | 'failed'
  phase?: string
  message?: string
  error?: string
  started_at?: string
  updated_at?: string
  deployment_type?: 'docker' | 'binary'
  container_name?: string
  image_name?: string
  pull_image_name?: string
  mirror_host?: string
  registry_url?: string
  release_version?: string
  asset_name?: string
  asset_url?: string
  install_dir?: string
  binary_name?: string
  update_manager?: 'panel' | 'watchtower'
  watchtower_response?: Record<string, any>
}

export interface SystemHealthItem {
  name: string
  status: string
  message?: string
}

export interface SystemHealthSnapshot {
  items: SystemHealthItem[]
  last_checked_at?: string
}

export interface ConfigScriptPayload {
  content: string
  path: string
}

export const systemApi = {
  info: () => request.get('/system/info'),
  machineCode: () => request.get('/system/machine-code'),
  dashboard: (range?: number) => request.get('/system/dashboard', { params: range ? { range } : undefined }),
  stats: () => request.get('/system/stats'),
  version: () => request.get('/system/version'),
  publicVersion: () => request.get('/system/public-version'),
  panelSettings: () => request.get('/system/panel-settings'),
  checkUpdate: () => request.get('/system/check-update'),
  updateStatus: () => request.get('/system/update-status'),
  updatePanel: () => request.post('/system/update'),
  restart: () => request.post('/system/restart'),
  panelLog: (params?: { lines?: number; keyword?: string; level?: 'debug' | 'info' | 'warn' | 'error' | '' }) =>
    request.get('/system/panel-log', { params }),
  backup: (password?: string, selection?: Partial<BackupSelection>, name?: string) => request.post('/system/backup', { password, selection, name }),
  backupList: () => request.get('/system/backups'),
  downloadBackup: (filename: string) =>
    request.get('/system/backup/download', {
      params: { filename },
      responseType: 'blob',
    }) as Promise<Blob>,
  restoreProgress: () => request.get('/system/restore/progress'),
  restore: (filename: string, password?: string) =>
    request.post('/system/restore', { filename, password }, { timeout: 0 }),
  deleteBackup: (filename: string) =>
    request.delete('/system/backup', { params: { filename } }),
  healthStatus: () => request.get('/system/health-check') as Promise<SystemHealthSnapshot>,
  healthCheck: () => request.post('/system/health-check') as Promise<SystemHealthSnapshot>,
  uploadBackup: (file: File, onProgress?: (percent: number) => void) => {
    const formData = new FormData()
    formData.append('file', file)
    return request.post('/system/backup/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' },
      timeout: 0,
      onUploadProgress: onProgress
        ? (e: any) => { if (e.total) onProgress(Math.round((e.loaded * 100) / e.total)) }
        : undefined,
    })
  },
}

export const configScriptApi = {
  get: () => request.get('/system/config-script') as Promise<ConfigScriptPayload>,
  save: (content: string) => request.put('/system/config-script', { content }) as Promise<{ message: string }>,
}

export const configApi = {
  list: () => request.get('/configs'),
  get: (key: string) => request.get(`/configs/${key}`),
  set: (data: { key: string; value: string; description?: string }) => request.post('/configs', data),
  batchSet: (configs: Record<string, string>) => request.put('/configs/batch', { configs }),
  delete: (key: string) => request.delete(`/configs/${key}`),
}

export const platformTokenApi = {
  platforms: () => request.get('/platform-tokens/platforms'),
  createPlatform: (data: { name: string; label?: string; icon?: string }) =>
    request.post('/platform-tokens/platforms', data),
  deletePlatform: (id: number) => request.delete(`/platform-tokens/platforms/${id}`),
  list: (platformId?: number) =>
    request.get('/platform-tokens', { params: platformId ? { platform_id: platformId } : {} }),
  create: (data: { platform_id: number; name: string; token: string; remarks?: string }) =>
    request.post('/platform-tokens', data),
  update: (id: number, data: { name?: string; token?: string; remarks?: string }) =>
    request.put(`/platform-tokens/${id}`, data),
  delete: (id: number) => request.delete(`/platform-tokens/${id}`),
  enable: (id: number) => request.put(`/platform-tokens/${id}/enable`),
  disable: (id: number) => request.put(`/platform-tokens/${id}/disable`),
}
