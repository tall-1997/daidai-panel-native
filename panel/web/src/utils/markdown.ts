import { escapeHtml } from './ansi'

/* ============================================================================
 * 极简 Markdown 渲染器
 * ----------------------------------------------------------------------------
 * 唯一使用方：设置页「发现新版本」弹窗里的更新日志。
 * 输入永远是 GitHub Release body，也就是 docs/release-notes/vX.Y.Z.md 的逐字副本
 * （release.yml 用 --notes-file 直接上传原文，中间不做任何加工）。
 *
 * 支持范围是按 71 份历史 notes 的实际语法统计框定的，不追求 CommonMark 完备：
 *   块级：h1~h3、`---` 分隔线、``` 围栏代码块、引用、无序 / 有序列表（含缩进嵌套）、
 *         GFM 表格、段落、HTML 注释（闭合的吃掉不显示；没闭合的按纯文本显示，见下面注释分支）
 *   内联：**加粗**、`行内代码`、[文字](链接)、裸 http(s) URL、*斜体*、反斜杠转义
 * 不支持（语料里一次都没出现过）：图片、删除线、脚注、任务列表、setext 标题、
 * 引用式链接、HTML 实体、双反引号行内代码、表格对齐冒号、链接文字里再嵌 加粗 / 代码。
 * 这些写法不会报错，只会原样当纯文本显示。
 *
 * 安全模型（这三条是本文件全部的风险点，改动时必须一起看）：
 *   1. **先转义再拼装，顺序不能反**。切块 → 每个文本片段过 escapeHtml → 再包上本文件
 *      自己写死的标签。输出里除了这些写死的标签，不存在任何来自原文的尖括号。
 *   2. **行内代码内部同样要转义**。notes 里有 `<style>` `<meta>` `<owner>` 这类真标签
 *      （20+ 处），漏一处就是货真价实的标签注入。
 *   3. **链接 href 只放行 http / https**，其余（相对路径、javascript: 等）降级成纯文本，
 *      只显示链接文字。放行的一律加 target="_blank" rel="noopener noreferrer"。
 * ============================================================================ */

/** 引用 / 列表互相嵌套的深度上限。正常 notes 到不了 3 层，这里纯粹是防畸形输入递归失控。 */
const MAX_BLOCK_DEPTH = 8
/** 加粗 / 斜体互相嵌套的深度上限。每层递归都在缩短字符串，这里同样只是兜底。 */
const MAX_INLINE_DEPTH = 4

const HEADING_PATTERN = /^ {0,3}(#{1,6})\s+(.*)$/
const THEMATIC_BREAK_PATTERN = /^ {0,3}(?:-{3,}|_{3,})\s*$/
const FENCE_PATTERN = /^ {0,3}(`{3,}|~{3,})/
const LIST_ITEM_PATTERN = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/
/** 链接与裸 URL 共用的放行规则：协议必须逐字是 http / https，把伪协议和相对路径一并挡在外面 */
const SAFE_URL_PATTERN = /^https?:\/\/\S+$/i
const BARE_URL_PATTERN = /^https?:\/\/[^\s<>"'`]+/i
/** 裸 URL 结尾常常粘着句读，这些字符不该被算进链接 */
const BARE_URL_TRAILING_PATTERN = /[.,;:!?'"）】》、。，；：！？)\]}]+$/
/** 反斜杠可以转义的字符集（CommonMark 的 ASCII 标点集） */
const ESCAPABLE_PATTERN = /[!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~]/

/* ---------------------------------------------------------------- 内联渲染 */

/**
 * 校验并返回可用的链接地址；不合格返回空串。
 *
 * 只放行 http / https 绝对地址：Release body 没有「源文件路径」这个上下文，
 * 相对链接（`](../task-not-running.md)` 这类）在 GitHub Release 页面上本身就是 404，
 * 到了面板里更没有可解析的基准；javascript: 之类伪协议则直接是安全问题。
 * 协议头被开头的 ^https?:// 钉死之后，后面写什么都改变不了协议，剩下的引号交给 escapeHtml。
 */
function safeUrl(raw: string): string {
  const url = raw.trim()
  if (!SAFE_URL_PATTERN.test(url)) return ''
  return url
}

function renderLink(label: string, href: string): string {
  const url = safeUrl(href)
  // 降级成纯文本：只显示链接文字，不留 <a>，用户也就点不动一个必然 404 的地址
  if (!url) return escapeHtml(label)
  return `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(label)}</a>`
}

/** 从 `[` 开始尝试匹配一个行内链接，失败返回 null（调用方会把 `[` 当普通字符） */
function matchLink(text: string, start: number): { label: string, href: string, end: number } | null {
  const labelEnd = text.indexOf(']', start + 1)
  if (labelEnd < 0 || text[labelEnd + 1] !== '(') return null
  const hrefEnd = text.indexOf(')', labelEnd + 2)
  if (hrefEnd < 0) return null
  const label = text.slice(start + 1, labelEnd)
  const href = text.slice(labelEnd + 2, hrefEnd)
  // 地址里带空白说明是 `](url "title")` 这种带标题的写法，语料里没有，不认它
  if (/\s/.test(href)) return null
  return { label, href, end: hrefEnd + 1 }
}

/**
 * 找 `*斜体*` 的闭合星号，找不到返回 -1。
 *
 * 两侧都要求紧贴非空白字符（CommonMark 的 flanking 规则）。这条不是洁癖：
 * notes 里 `0 5 * * * *`、`55 11 * * * jd_OnceApply.js` 这类 cron 表达式远比真正的斜体常见，
 * 松一点的规则会把整段 cron 吃成斜体。加了这条之后，星号两边都是空格的 cron 一个都匹配不上。
 */
function findEmphasisEnd(text: string, start: number): number {
  const next = text[start + 1]
  if (!next || next === '*' || /\s/.test(next)) return -1
  for (let i = start + 2; i < text.length; i++) {
    const char = text[i]
    if (char === '\n') return -1
    if (char !== '*') continue
    if (/\s/.test(text[i - 1] ?? ' ')) continue
    // 紧跟着还有一个星号说明这是 ** 的一半，不是斜体的收尾
    if (text[i + 1] === '*') continue
    return i
  }
  return -1
}

/**
 * 渲染一段内联文本。
 *
 * 单趟从左往右扫，普通字符先攒进 plain 缓冲，遇到语法标记时先把缓冲 escapeHtml 冲出去、
 * 再拼我们自己的标签 —— 这就是安全要求 1 里「先转义再拼装」的落点。
 * 优先级由分支顺序决定：转义 > 行内代码 > 链接 > 加粗 > 斜体 > 裸 URL。
 */
function renderInline(text: string, depth: number): string {
  if (depth > MAX_INLINE_DEPTH) return escapeHtml(text)

  let out = ''
  let plain = ''
  let i = 0

  const flush = () => {
    if (plain === '') return
    out += escapeHtml(plain)
    plain = ''
  }

  while (i < text.length) {
    const char = text[i] as string

    // 反斜杠转义。语料里 v2.1.0 / v2.2.6 用 `192.168.\*` `PIP\_\*` 这种写法躲斜体，
    // 不认它的话这些星号反而会被下面的斜体规则误配。
    if (char === '\\' && i + 1 < text.length && ESCAPABLE_PATTERN.test(text[i + 1] as string)) {
      plain += text[i + 1]
      i += 2
      continue
    }

    // 行内代码。内容照样要 escapeHtml —— 这是安全要求 2。
    if (char === '`') {
      const end = text.indexOf('`', i + 1)
      if (end > i + 1) {
        flush()
        out += `<code>${escapeHtml(text.slice(i + 1, end))}</code>`
        i = end + 1
        continue
      }
    }

    if (char === '[') {
      const link = matchLink(text, i)
      if (link) {
        flush()
        // 链接文字只做转义、不递归：递归会让 [https://a](https://b) 在 <a> 里再套一个 <a>
        out += renderLink(link.label, link.href)
        i = link.end
        continue
      }
    }

    if (char === '*' && text[i + 1] === '*') {
      const end = text.indexOf('**', i + 2)
      if (end > i + 2) {
        flush()
        out += `<strong>${renderInline(text.slice(i + 2, end), depth + 1)}</strong>`
        i = end + 2
        continue
      }
    }

    if (char === '*') {
      const end = findEmphasisEnd(text, i)
      if (end > 0) {
        flush()
        out += `<em>${renderInline(text.slice(i + 1, end), depth + 1)}</em>`
        i = end + 1
        continue
      }
    }

    // 裸 URL。前一个字符是字母数字时不认，避免把 xhttps://... 中间截一段出来。
    if ((char === 'h' || char === 'H') && !/[A-Za-z0-9]/.test(text[i - 1] ?? '')) {
      const matched = BARE_URL_PATTERN.exec(text.slice(i))
      if (matched) {
        const url = matched[0].replace(BARE_URL_TRAILING_PATTERN, '')
        if (safeUrl(url)) {
          flush()
          out += renderLink(url, url)
          i += url.length
          continue
        }
      }
    }

    plain += char
    i++
  }

  flush()
  return out
}

/* ------------------------------------------------------------------ 表格 */

/**
 * 按 `|` 切一行表格，并去掉首尾那对包裹用的空单元格。
 *
 * 切分是**反引号感知**的：行内代码里的 `|` 不是列分隔符。
 * 这不是假想需求 —— 语料里确实有 `ddp service install|uninstall|start|stop` 这类写法（v2.1.9），
 * 目前只出现在正文而不是表格单元格里，但裸 split('|') 一旦碰上就会把整行拆碎、整张表错列。
 */
function splitTableRow(line: string): string[] {
  const cells: string[] = []
  let current = ''
  let inCode = false
  for (let i = 0; i < line.length; i++) {
    const char = line[i] as string
    // GFM 的表格切分发生在内联解析之前，所以 `\|` 在任何位置（包括行内代码里）
    // 都只是一个普通竖线，这里跟着还原成 `|`
    if (char === '\\' && line[i + 1] === '|') {
      current += '|'
      i++
      continue
    }
    if (char === '`') {
      inCode = !inCode
      current += char
      continue
    }
    if (char === '|' && !inCode) {
      cells.push(current)
      current = ''
      continue
    }
    current += char
  }
  cells.push(current)
  if (cells.length > 0 && (cells[0] as string).trim() === '') cells.shift()
  if (cells.length > 0 && (cells[cells.length - 1] as string).trim() === '') cells.pop()
  return cells
}

/** 分隔行：每个单元格都只由 `-` 组成（允许对齐冒号，但对齐本身不实现） */
function isTableDelimiterRow(line: string): boolean {
  const trimmed = line.trim()
  if (!trimmed.startsWith('|') && !trimmed.includes('|')) return false
  if (!trimmed.includes('-')) return false
  const cells = splitTableRow(trimmed)
  if (cells.length === 0) return false
  return cells.every((cell) => /^:?-+:?$/.test(cell.trim()))
}

function renderTable(lines: string[], start: number): { html: string, next: number } {
  const header = splitTableRow((lines[start] as string).trim())
  const rows: string[][] = []
  let i = start + 2
  while (i < lines.length && (lines[i] as string).trim().startsWith('|')) {
    rows.push(splitTableRow((lines[i] as string).trim()))
    i++
  }

  const columns = header.length
  const head = header.map((cell) => `<th>${renderInline(cell.trim(), 0)}</th>`).join('')
  const body = rows.map((row) => {
    // 列数一律以表头为准：多写的单元格丢掉、少写的补空。
    // 否则某一行多敲一根竖线就会把这一行整体错开，比丢一格难看得多。
    let cells = ''
    for (let column = 0; column < columns; column++) {
      cells += `<td>${renderInline((row[column] ?? '').trim(), 0)}</td>`
    }
    return `<tr>${cells}</tr>`
  }).join('')

  // 外层的滚动容器不能省：弹窗只有 720px 宽，而 v3.1.0 那一版有 13 张表，
  // 宽表不给横向滚动会直接把弹窗撑破。
  return {
    html: `<div class="md-table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>`,
    next: i,
  }
}

/* ------------------------------------------------------------------ 块级 */

function isClosingFence(line: string, marker: string): boolean {
  const trimmed = line.trim()
  if (trimmed.length < marker.length) return false
  const char = marker[0]
  for (let i = 0; i < trimmed.length; i++) {
    if (trimmed[i] !== char) return false
  }
  return true
}

/** 这一行是不是「另一个块的开头」——段落和引用要靠它决定在哪里收尾 */
function isBlockBoundary(line: string): boolean {
  const trimmed = line.trimStart()
  if (trimmed === '') return true
  if (trimmed.startsWith('<!--')) return true
  if (trimmed.startsWith('>')) return true
  if (trimmed.startsWith('|')) return true
  if (FENCE_PATTERN.test(line)) return true
  if (THEMATIC_BREAK_PATTERN.test(line)) return true
  if (HEADING_PATTERN.test(line)) return true
  if (LIST_ITEM_PATTERN.test(line)) return true
  return false
}

function stripQuoteMarker(line: string): string {
  const trimmed = line.trimStart()
  if (!trimmed.startsWith('>')) return trimmed
  const rest = trimmed.slice(1)
  return rest.startsWith(' ') ? rest.slice(1) : rest
}

/**
 * 渲染一段列表。
 *
 * 条目按缩进分层：缩进比本级深的行归到当前条目，退格之后交回 renderBlocks 递归，
 * 于是「2 空格一层的嵌套列表」「条目里再挂一个代码块」这些写法都不用单独处理。
 */
function renderList(lines: string[], start: number, depth: number): { html: string, next: number } {
  const first = LIST_ITEM_PATTERN.exec(lines[start] as string) as RegExpExecArray
  const baseIndent = (first[1] as string).length
  const ordered = /\d/.test(first[2] as string)

  const items: { head: string[], child: string[] }[] = []
  let current: { head: string[], child: string[] } | null = null
  // 条目正文的起始列，子块按它退格
  let contentIndent = baseIndent + 2
  let i = start

  while (i < lines.length) {
    const line = lines[i] as string

    if (line.trim() === '') {
      // 空行本身不结束列表：要看它后面那段是不是还缩在条目里（松散列表 / 条目内多段）
      let lookahead = i + 1
      while (lookahead < lines.length && (lines[lookahead] as string).trim() === '') lookahead++
      if (lookahead >= lines.length) break
      const nextLine = lines[lookahead] as string
      const nextIndent = nextLine.length - nextLine.trimStart().length
      const nextItem = LIST_ITEM_PATTERN.exec(nextLine)
      const nextIsSibling = nextItem !== null && (nextItem[1] as string).length <= baseIndent + 1
      if (nextIndent < contentIndent && !nextIsSibling) break
      if (current) current.child.push('')
      i = lookahead
      continue
    }

    const item = LIST_ITEM_PATTERN.exec(line)
    const indent = line.length - line.trimStart().length

    // 同级条目：缩进不深于本级（+1 是容忍 ` - ` 这种多打一个空格的写法）
    if (item && (item[1] as string).length <= baseIndent + 1) {
      const marker = item[2] as string
      // ul 和 ol 互相切换时视为两段不同的列表，交回上层重新起一块
      if (/\d/.test(marker) !== ordered) break
      current = { head: [item[3] as string], child: [] }
      items.push(current)
      contentIndent = (item[1] as string).length + marker.length + 1
      i++
      continue
    }

    if (!current) break

    if (indent > baseIndent) {
      // 缩进比条目正文更深的行有两种：
      //   a) 真正的子块（嵌套列表、围栏代码块、引用、标题、表格、分隔线…）→ 下沉到 child，
      //      退格后交回 renderBlocks 递归；
      //   b) 条目正文的软换行（notes 里最常见的写法：一句话写不下，缩 3~4 空格接着写）
      //      → 并回 head，和 head 里其余行一样用 <br> 接。
      // 判据用 isBlockBoundary：只有它认的块起始才算 a。
      // 不区分的话，b 会被当成子块渲染成 <p>，配上 :deep(p){margin:8px 0} 就是一句话中间
      // 空出一整个段落间距 —— 而且和渲染器自身矛盾：段落内软换行、顶格 lazy continuation
      // 都是用 <br> 接的，唯独缩进续行变段落。
      // child 非空说明中间已经隔了空行或子块，那之后的文字是条目内的第二段，仍旧走 child。
      if (current.child.length === 0 && !isBlockBoundary(line)) {
        current.head.push(line.trim())
        i++
        continue
      }
      current.child.push(line.slice(Math.min(indent, contentIndent)))
      i++
      continue
    }

    // 顶格的普通文本 = markdown 的 lazy continuation，接到当前条目正文后面
    if (current.child.length === 0 && !isBlockBoundary(line)) {
      current.head.push(line.trim())
      i++
      continue
    }
    break
  }

  const tag = ordered ? 'ol' : 'ul'
  const startNumber = Number.parseInt(first[2] as string, 10)
  const startAttr = ordered && Number.isFinite(startNumber) && startNumber !== 1 ? ` start="${startNumber}"` : ''
  const body = items.map((item) => {
    const head = item.head.map((text) => renderInline(text, 0)).join('<br>')
    const child = item.child.some((line) => line.trim() !== '') ? renderBlocks(item.child, depth + 1) : ''
    return `<li>${head}${child}</li>`
  }).join('')

  return { html: `<${tag}${startAttr}>${body}</${tag}>`, next: i }
}

function renderBlocks(sourceLines: string[], depth: number): string {
  // 嵌套过深只可能是畸形输入，直接按纯文本收尾
  if (depth > MAX_BLOCK_DEPTH) return `<p>${escapeHtml(sourceLines.join('\n'))}</p>`

  // 先拷一份再解析：HTML 注释是按字符吞的，`-->` 之后的残余内容要写回那一行重新解析，
  // 拷贝是为了不改到调用方传进来的数组（renderList 的 item.child、renderMarkdown 的 split 结果）
  const lines = sourceLines.slice()
  let html = ''
  let i = 0

  while (i < lines.length) {
    const line = lines[i] as string

    if (line.trim() === '') {
      i++
      continue
    }

    // HTML 注释吃掉不显示。每份 notes 第 2 行都有 <!-- release-title: ... -->，
    // 那是给 release.yml 抽 Release 标题用的，显示出来只会在正文顶上多一行乱码。
    // 只认「行首（可带缩进）就是 <!--」的写法：行中间的 <!-- 不当注释处理，
    // 一来语料里没有，二来 `<!-- x -->` 这种行内代码不该被吞。
    if (line.trimStart().startsWith('<!--')) {
      // 按字符定位 `-->`，不是按行：同一行 `-->` 后面的残余内容必须留下来继续解析，
      // 否则 `<!-- release-title: 摘要 --> 本版还修了 X` 里的正文会凭空消失且毫无提示。
      const openAt = line.indexOf('<!--')
      let end = i
      let closeAt = line.indexOf('-->', openAt + 4)
      while (closeAt < 0 && end + 1 < lines.length) {
        end++
        closeAt = (lines[end] as string).indexOf('-->')
      }
      if (closeAt >= 0) {
        const rest = (lines[end] as string).slice(closeAt + 3)
        if (rest.trim() === '') {
          i = end + 1
        } else {
          // 残余内容写回该行，从这一行重新解析（`-->` 后面又是一条注释时也能接着吞）。
          // rest 一定比原行短，不会死循环。
          lines[end] = rest
          i = end
        }
        continue
      }
      // 走到这里说明注释没闭合。这时**不吞**，原样往下走（最终落到段落分支当纯文本显示）。
      // 理由：吃到文末的话，「编辑 release-title 时手滑删掉 -->」会让弹窗显示一份被截断
      // 且看不出被截断的日志 —— 渲染结果非空，v-if 为真，不会回落到 <pre>，用户根本发现不了
      // 自己少看了半份。preflight 那条断言只保证「存在一条闭合的 release-title 注释」，
      // 正文中间另起的坏注释它管不着。把 `<!-- ...` 原样显示出来，异常肉眼可见，正文一个字不丢。
    }

    const fence = FENCE_PATTERN.exec(line)
    if (fence) {
      const marker = fence[1] as string
      const body: string[] = []
      let end = i + 1
      while (end < lines.length && !isClosingFence(lines[end] as string, marker)) {
        body.push(lines[end] as string)
        end++
      }
      // 代码块整体只转义、不解析内联，围栏里的 *、`、[] 都该原样显示
      html += `<pre><code>${escapeHtml(body.join('\n'))}</code></pre>`
      i = end < lines.length ? end + 1 : lines.length
      continue
    }

    if (THEMATIC_BREAK_PATTERN.test(line)) {
      html += '<hr>'
      i++
      continue
    }

    const heading = HEADING_PATTERN.exec(line)
    if (heading) {
      // 语料里 h4+ 一次都没出现过；真出现了就并到 h3，不为它再造一档样式
      const level = Math.min((heading[1] as string).length, 3)
      html += `<h${level}>${renderInline((heading[2] as string).trim(), 0)}</h${level}>`
      i++
      continue
    }

    if (line.trimStart().startsWith('>')) {
      const body: string[] = []
      while (i < lines.length) {
        const quoteLine = lines[i] as string
        // 引用块内的普通行（lazy continuation）也收进来，但空行 / 其它块开头就收尾
        if (!quoteLine.trimStart().startsWith('>') && (body.length === 0 || isBlockBoundary(quoteLine))) break
        body.push(stripQuoteMarker(quoteLine))
        i++
      }
      html += `<blockquote>${renderBlocks(body, depth + 1)}</blockquote>`
      continue
    }

    // 表格：本行以 | 开头，且下一行是分隔行。少了后半个条件，正文里一句以 | 开头的话会被误判成表
    if (line.trimStart().startsWith('|') && i + 1 < lines.length && isTableDelimiterRow(lines[i + 1] as string)) {
      const table = renderTable(lines, i)
      html += table.html
      i = table.next
      continue
    }

    if (LIST_ITEM_PATTERN.test(line)) {
      const list = renderList(lines, i, depth)
      html += list.html
      i = list.next
      continue
    }

    // 段落。do-while 保证至少吃掉一行，否则边界判断一旦和上面的分派不同步就会死循环。
    const paragraph: string[] = []
    do {
      paragraph.push((lines[i] as string).trim())
      i++
    } while (i < lines.length && (lines[i] as string).trim() !== '' && !isBlockBoundary(lines[i] as string))
    // 段内换行按 GitHub 的口径当硬换行：Release 正文和 issue 评论走的是同一套渲染，
    // 单个换行就是换行。拼成一行反而会在中文之间插进多余的空格。
    html += `<p>${paragraph.map((text) => renderInline(text, 0)).join('<br>')}</p>`
  }

  return html
}

/* ------------------------------------------------------------------ 出口 */

/**
 * 把一份 Markdown 渲染成 HTML 字符串（供 v-html 使用）。
 *
 * 输入为空时返回空串，调用方据此回落到纯文本展示。
 * 本函数不吞异常：调用方必须自己套 try/catch —— 更新日志弹窗在有新版本时是自动弹的，
 * 渲染器一抛异常就等于所有人点「检查系统更新」都看到一块白。
 */
export function renderMarkdown(source: string): string {
  if (typeof source !== 'string') return ''
  const text = source.replace(/^﻿/, '').replace(/\r\n?/g, '\n')
  if (text.trim() === '') return ''
  return renderBlocks(text.split('\n'), 0)
}
