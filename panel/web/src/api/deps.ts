import request from './request'

export interface MirrorsResponse {
  pip_mirror: string
  npm_mirror: string
  linux_mirror: string
  linux_package_manager: string
  linux_distribution: string
  linux_mirror_supported: boolean
  linux_mirror_label: string
  linux_mirror_message: string
}

export interface PythonRuntimeInfo {
  version: string
  label: string
  default: boolean
  venv_path: string
  venv_healthy: boolean
  python_path: string
  pip_path: string
  available: boolean
  message: string
}

/**
 * 各依赖类型下「安装失败」的条数，三个键恒定存在（没有失败时是 0）。
 *
 * 它与 `list()` 的 type / python_version 筛选无关，永远是全量统计，
 * 其中 python 一档【跨所有 Python 版本】——这样三者之和才等于侧栏「依赖管理」角标
 * （服务端 deps_failed 就是跨类型汇总的），用户才能把角标数字和页面对上。
 */
export interface DepsFailedByType {
  nodejs: number
  python: number
  linux: number
}

/**
 * Playwright 运行环境的一键安装状态（v3.3.1，issue #142，GET /deps/playwright）。
 *
 * 一键安装做三件事：登记一批 Linux 系统库（容器重建后由启动校验在后台自动重装）、
 * 装 Python 的 playwright 包、把 Chromium 下载到数据卷（browsers_path）。
 * 只支持 Debian 12 版镜像（apt，amd64 / arm64，root 运行），其余情况 supported 为 false、原因写在 reason。
 */
export interface PlaywrightStatus {
  /** 当前环境能否一键安装；为 false 时按钮置灰，旁边显示 reason */
  supported: boolean
  /** 不支持的原因（非 Linux、Alpine、非 apt、非 Debian 12、架构不支持、非 root 等），支持时为空串 */
  reason: string
  /** os-release 里的 ID，如 debian / alpine */
  distribution: string
  /** os-release 里的 VERSION_ID，如 12 */
  version_id: string
  /** 运行架构，如 amd64 / arm64 */
  arch: string
  /** 要登记的系统包清单（Debian 12 bookworm） */
  packages: string[]
  /** Chromium 的下载目录（PLAYWRIGHT_BROWSERS_PATH 的实际取值），非容器部署时可能为空 */
  browsers_path: string
  /** 默认 Python 版本下是否已装 playwright 包 */
  python_installed: boolean
  /** browsers_path 下是否已有 chromium 浏览器 */
  browsers_installed: boolean
  /** packages 里已登记且确实已装的个数，与 linux_total 组成「系统库 x/y 已安装」 */
  linux_installed: number
  linux_total: number
}

export const depsApi = {
  list(type: string, pythonVersion?: string) {
    // failed_by_type 标成可选：老版本服务端没有这个字段，前端读不到时按全 0 处理
    return request.get('/deps', { params: { type, python_version: pythonVersion } }) as Promise<{ data: any[]; total: number; failed_by_type?: DepsFailedByType }>
  },

  create(type: string, names: string[], pythonVersion?: string) {
    return request.post('/deps', { type, names, python_version: pythonVersion }) as Promise<{ message: string; data: any[] }>
  },

  delete(id: number, force?: boolean) {
    return request.delete(`/deps/${id}`, { params: force ? { force: true } : undefined }) as Promise<{ message: string }>
  },

  batchDelete(ids: number[]) {
    return request.post('/deps/batch-delete', { ids }) as Promise<{ message: string }>
  },

  batchReinstall(ids: number[]) {
    return request.post('/deps/batch-reinstall', { ids }) as Promise<{ message: string }>
  },

  getStatus(id: number) {
    return request.get(`/deps/${id}/status`) as Promise<{ data: any }>
  },

  reinstall(id: number) {
    return request.put(`/deps/${id}/reinstall`) as Promise<{ message: string }>
  },

  exportList(type: string, pythonVersion?: string) {
    return request.get('/deps/export', { params: { type, python_version: pythonVersion }, responseType: 'blob' }) as Promise<Blob>
  },

  cancel(id: number) {
    return request.put(`/deps/${id}/cancel`) as Promise<{ message: string }>
  },

  pipList: (pythonVersion?: string) => request.get('/deps/pip', { params: { python_version: pythonVersion } }),
  npmList: () => request.get('/deps/npm'),

  pythonRuntimes() {
    return request.get('/deps/python-runtimes') as Promise<{ data: PythonRuntimeInfo[]; default_version: string }>
  },

  setDefaultPythonRuntime(version: string) {
    return request.put('/deps/python-runtime-default', { version }) as Promise<{ message: string; default_version: string }>
  },

  getMirrors() {
    return request.get('/deps/mirrors') as Promise<MirrorsResponse>
  },

  setMirrors(data: { pip_mirror?: string; npm_mirror?: string; linux_mirror?: string }) {
    return request.put('/deps/mirrors', data) as Promise<{ message: string }>
  },

  playwrightStatus() {
    return request.get('/deps/playwright') as Promise<PlaywrightStatus>
  },

  /**
   * 一键安装 Playwright 运行环境。成功回 201：data 是这次入队的依赖记录（系统包在前、Python 的 playwright 在后，
   * 按顺序在后台逐个安装，进度看依赖列表与各自的日志）；不支持时回 400 {error: 原因}，由调用方直接展示。
   * data 沿用 create() 的写法：依赖记录在前端没有单独的类型定义，页面按行对象使用。
   */
  installPlaywright() {
    return request.post('/deps/playwright/install') as Promise<{
      message: string
      data: any[]
      packages: string[]
      browsers_path: string
    }>
  },
}
