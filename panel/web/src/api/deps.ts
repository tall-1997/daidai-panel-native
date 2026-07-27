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

export const depsApi = {
  list(type: string, pythonVersion?: string) {
    return request.get('/deps', { params: { type, python_version: pythonVersion } }) as Promise<{ data: any[]; total: number }>
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
}
