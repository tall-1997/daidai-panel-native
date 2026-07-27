/**
 * 从 axios 错误对象中提取后端返回的可读错误信息。
 * 优先级：response.data.error > response.data.message > err.message > fallback
 */
export function extractError(err: unknown, fallback = '操作失败'): string {
  if (!err) return fallback
  if (typeof err === 'string') {
    // ElMessageBox.confirm 取消时 reject 字符串 'cancel'
    if (err === 'cancel' || err === 'close') return ''
    return err
  }
  const anyErr = err as any
  if (anyErr === 'cancel' || anyErr?.toString?.() === 'cancel') return ''
  const data = anyErr?.response?.data
  if (data) {
    if (typeof data === 'string') return data
    if (data.error) return String(data.error)
    if (data.message) return String(data.message)
  }
  if (anyErr?.message) return String(anyErr.message)
  return fallback
}

/**
 * 判断错误是否为 ElMessageBox 取消
 */
export function isCancel(err: unknown): boolean {
  if (!err) return false
  if (err === 'cancel' || err === 'close') return true
  const anyErr = err as any
  const s = anyErr?.toString?.()
  return s === 'cancel' || s === 'close'
}
