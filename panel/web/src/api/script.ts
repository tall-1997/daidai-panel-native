import request from './request'

function buildKeepaliveAuthHeaders() {
  const headers: Record<string, string> = {
    'X-Client-Type': 'web',
    'X-Client-App': 'daidai-panel-web'
  }

  if (typeof window !== 'undefined') {
    const accessToken = window.localStorage.getItem('access_token')
    if (accessToken) {
      headers.Authorization = `Bearer ${accessToken}`
    }
  }

  return headers
}

export const scriptApi = {
  list(params?: { keyword?: string }) {
    return request.get('/scripts', { params }) as Promise<{ data: any[] }>
  },

  tree() {
    return request.get('/scripts/tree') as Promise<{ data: any[] }>
  },

  getContent(path: string) {
    return request.get('/scripts/content', { params: { path } }) as Promise<{ data: { content: string; binary?: boolean; is_binary?: boolean; size: number } }>
  },

  download(path: string) {
    return request.get('/scripts/download', {
      params: { path, t: Date.now() },
      responseType: 'blob'
    }) as Promise<Blob>
  },

  saveContent(path: string, content: string, message?: string) {
    return request.put('/scripts/content', { path, content, message }) as Promise<{ message: string }>
  },

  upload(formData: FormData) {
    return request.post('/scripts/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    }) as Promise<{ message: string; path?: string; paths?: string[]; uploaded_count?: number }>
  },

  delete(path: string, type?: string) {
    return request.delete('/scripts', { params: { path, type: type || 'file' } }) as Promise<{ message: string }>
  },

  createDirectory(path: string) {
    return request.post('/scripts/directory', { path }) as Promise<{ message: string }>
  },

  rename(oldPath: string, newName: string) {
    return request.put('/scripts/rename', { old_path: oldPath, new_name: newName }) as Promise<{ message: string; new_path: string }>
  },

  move(sourcePath: string, targetDir: string) {
    return request.put('/scripts/move', { source_path: sourcePath, target_dir: targetDir }) as Promise<{ message: string }>
  },

  copy(sourcePath: string, targetPath: string) {
    return request.post('/scripts/copy', { source_path: sourcePath, target_path: targetPath }) as Promise<{ message: string }>
  },

  batchDelete(paths: string[]) {
    return request.delete('/scripts/batch', { data: { paths } }) as Promise<{ message: string }>
  },

  listVersions(path: string) {
    return request.get('/scripts/versions', { params: { path } }) as Promise<{ data: any[] }>
  },

  clearVersions(path: string) {
    return request.delete('/scripts/versions', { params: { path } }) as Promise<{ message: string; cleared_count: number }>
  },

  getVersion(id: number) {
    return request.get(`/scripts/versions/${id}`) as Promise<{ data: any }>
  },

  rollback(id: number) {
    return request.put(`/scripts/versions/${id}/rollback`) as Promise<{ message: string }>
  },

  debugRun(data: { path?: string; content?: string; language?: string }) {
    return request.post('/scripts/run', data) as Promise<{ message: string; run_id: string }>
  },

  runCode(code: string, language: string) {
    return request.post('/scripts/run-code', { code, language }) as Promise<{ message: string; run_id: string }>
  },

  debugLogs(runId: string) {
    return request.get(`/scripts/run/${runId}/logs`) as Promise<{ data: { logs: string[]; done: boolean; exit_code?: number; status?: string } }>
  },

  debugStop(runId: string) {
    return request.put(`/scripts/run/${runId}/stop`) as Promise<{ message: string }>
  },

  debugStopKeepalive(runId: string) {
    if (typeof window === 'undefined' || !runId) {
      return false
    }

    try {
      void fetch(`/api/scripts/run/${encodeURIComponent(runId)}/stop`, {
        method: 'PUT',
        keepalive: true,
        credentials: 'same-origin',
        headers: buildKeepaliveAuthHeaders(),
      })
      return true
    } catch {
      return false
    }
  },

  debugClear(runId: string) {
    return request.delete(`/scripts/run/${runId}`) as Promise<{ message: string }>
  },

  format(data: { content: string; language: string }) {
    return request.post('/scripts/format', data) as Promise<{ data: { content: string } }>
  }
}
