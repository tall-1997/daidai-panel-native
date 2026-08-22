import request from './request'

export interface TerminalSession {
  id: string
  status: 'running' | 'exited' | 'stopped'
  shell: string
  pid: number
  created_at: string
  exit_code: number | null
  cursor?: number
  output?: Array<{ cursor: number; encoding: 'base64'; data: string }>
}

export const terminalApi = {
  create(rows: number, columns: number) {
    return request.post('/terminal/sessions', { rows, columns }) as Promise<{ data: TerminalSession }>
  },
  get(id: string, cursor: number) {
    return request.get(`/terminal/sessions/${id}`, { params: { cursor } }) as Promise<{ data: TerminalSession }>
  },
  input(id: string, data: string) {
    return request.post(`/terminal/sessions/${id}/input`, { data, encoding: 'utf8' }) as Promise<{ status: string }>
  },
  resize(id: string, rows: number, columns: number) {
    return request.put(`/terminal/sessions/${id}/resize`, { rows, columns }) as Promise<{ status: string }>
  },
  stop(id: string) {
    return request.put(`/terminal/sessions/${id}/stop`) as Promise<{ data: TerminalSession }>
  },
  remove(id: string) {
    return request.delete(`/terminal/sessions/${id}`) as Promise<{ status: string }>
  },
}
