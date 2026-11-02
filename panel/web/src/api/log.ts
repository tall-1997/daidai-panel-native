import request from './request'
import type { RawLogDownloadTicket } from '@/utils/rawLogDownload'

export const logApi = {
  list(params?: {
    task_id?: number
    status?: number
    keyword?: string
    page?: number
    page_size?: number
    /**
     * 执行时间范围，RFC3339 串。用 utils/datetime.ts 的 toDateRangeParams 生成——
     * 它会把 el-date-picker 给回来的两个 00:00:00 的 Date 收拢成
     * 「起始日 00:00:00.000 ~ 结束日 23:59:59.999」，避免结束日当天整天被漏掉。
     * 服务端按 created_at 闭区间过滤（见 server/handler/log.go 的 List）。
     */
    start_time?: string
    end_time?: string
  }) {
    return request.get('/logs', { params }) as Promise<{ data: any[]; total: number; page: number; page_size: number }>
  },

  detail(id: number) {
    return request.get(`/logs/${id}`) as Promise<any>
  },

  // 换取「下载原始日志」的短期票据。真正的文件由浏览器原生下载去拉，不走 axios。
  rawDownloadTicket(id: number) {
    return request.get(`/logs/${id}/raw-ticket`) as Promise<RawLogDownloadTicket>
  },

  delete(id: number) {
    return request.delete(`/logs/${id}`) as Promise<{ message: string }>
  },

  batchDelete(ids: number[]) {
    return request.post('/logs/batch-delete', { ids }) as Promise<{ message: string }>
  },

  clean(days?: number) {
    return request.delete('/logs/clean', { params: { days } }) as Promise<{ message: string }>
  }
}
