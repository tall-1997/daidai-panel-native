/**
 * 「下载原始日志」的前端侧：只负责换票 + 让浏览器自己去拉流。
 *
 * 为什么不用 axios 拉 blob：
 * 面板里其它下载（脚本、备份、依赖清单）都是 `responseType: 'blob'` + `URL.createObjectURL`，
 * 那条路会把整个文件先读进 JS 堆内存再复制一份到 blob。日志文件单个可以到 10MB 上限，
 * 而「下载原始日志」的存在意义正是拿到完整的大文件，用 blob 等于把服务端直传的收益全吃掉。
 * 这里改成浏览器原生下载：主进程直接边收边写磁盘，前端零额外内存。
 *
 * 代价是原生下载带不了 `Authorization` 头（项目用的是 Bearer JWT，不是 Cookie），
 * 所以要先用带头的普通请求换一张短期票据，再让浏览器去访问带票据的 URL。
 * 票据由后端 `pkg/dlticket` 签发：绑定单个文件、120 秒过期（见 handler/log_raw_download.go 的 rawLogTicketTTL）。
 */
export interface RawLogDownloadTicket {
  /** 已经拼好票据的下载地址，直接交给浏览器即可 */
  url: string
  /** 服务端给出的下载文件名 */
  filename: string
  /** 磁盘文件字节数 */
  size: number
  expires_at: string
  expires_in: number
}

/**
 * 用 `<a download>` 触发浏览器原生下载。
 * 不经过 XHR / fetch，文件内容不会进入 JS 内存。
 */
export function startRawLogDownload(ticket: RawLogDownloadTicket) {
  const anchor = document.createElement('a')
  anchor.href = ticket.url
  // 文件名由服务端决定（Content-Disposition 里也带了同一个名字）。
  // 这里显式赋值可以避开各浏览器对 filename*=UTF-8'' 解析的差异，中文名更稳。
  anchor.download = ticket.filename
  anchor.rel = 'noopener'
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
}

/** 前端本地生成的「折叠后」下载文件名：加 `-folded` 后缀，和原始文件区分开。 */
export function foldedLogDownloadName(base: string) {
  const safeBase = (base || 'log').replace(/[\\/:*?"<>|]/g, '_')
  const dotIndex = safeBase.lastIndexOf('.')
  if (dotIndex > 0) {
    return `${safeBase.slice(0, dotIndex)}-folded${safeBase.slice(dotIndex)}`
  }
  return `${safeBase}-folded.log`
}

/** 把文本存成本地文件（用于「折叠后」的下载，内容已经在内存里了）。 */
export function downloadTextAsFile(filename: string, text: string) {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.style.display = 'none'
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
  URL.revokeObjectURL(url)
}
