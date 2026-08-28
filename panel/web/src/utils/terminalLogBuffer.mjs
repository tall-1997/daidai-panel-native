export const DEFAULT_TRUNCATION_MARKER = '[... earlier output truncated ...]'

export class TerminalLogBuffer {
  constructor(options = {}) {
    this.maxChars = Math.max(1, options.maxChars ?? 1_000_000)
    this.maxLines = Math.max(1, options.maxLines ?? 10_000)
    this.marker = options.marker ?? DEFAULT_TRUNCATION_MARKER
    this.clear()
  }

  clear() {
    this.lines = []
    this.head = 0
    this.headOffset = 0
    this.tail = ''
    this.contentChars = 0
    this.pendingCarriageReturn = false
    this.truncated = false
  }

  set(text) {
    this.clear()
    this.append(String(text ?? ''))
  }

  append(chunk) {
    chunk = String(chunk ?? '')
    let start = 0

    for (let index = 0; index < chunk.length; index++) {
      const char = chunk[index]
      if (this.pendingCarriageReturn) {
        if (char === '\n') {
          this.commitLine()
          this.pendingCarriageReturn = false
          start = index + 1
          continue
        }
        this.replaceTail('')
        this.pendingCarriageReturn = false
        start = index
      }

      if (char !== '\r' && char !== '\n') continue

      this.appendText(chunk.slice(start, index))
      if (char === '\r') {
        if (chunk[index + 1] === '\n') {
          this.commitLine()
          index++
          start = index + 1
        } else {
          this.pendingCarriageReturn = true
          start = index + 1
        }
      } else {
        this.commitLine()
        start = index + 1
      }
    }

    this.appendText(chunk.slice(start))
    this.trim()
  }

  appendText(text) {
    if (!text) return
    this.tail += text
    this.contentChars += text.length
  }

  replaceTail(text) {
    this.contentChars += text.length - this.tail.length
    this.tail = text
  }

  commitLine() {
    this.lines.push(this.tail)
    this.contentChars += 1
    this.tail = ''
  }

  trim() {
    const markerChars = this.marker.length + 1
    let maxContentChars = this.maxChars
    let maxContentLines = this.maxLines

    const exceedsLimit = () =>
      this.contentChars > maxContentChars || this.contentLineCount > maxContentLines

    if (exceedsLimit()) {
      this.truncated = true
      maxContentChars = Math.max(0, this.maxChars - markerChars)
      maxContentLines = Math.max(0, this.maxLines - 1)
    }

    while (this.head < this.lines.length && exceedsLimit()) {
      const line = this.lines[this.head]
      const available = line.length - this.headOffset + 1
      const excessChars = Math.max(0, this.contentChars - maxContentChars)
      const excessLines = Math.max(0, this.contentLineCount - maxContentLines)
      if (excessLines > 0 || excessChars >= available) {
        this.contentChars -= available
        this.head++
        this.headOffset = 0
      } else {
        let remove = Math.min(excessChars, line.length - this.headOffset)
        remove = completeSurrogateRemoval(line, this.headOffset, remove)
        this.headOffset += remove
        this.contentChars -= remove
      }
    }

    if (this.contentChars > maxContentChars) {
      let remove = Math.min(this.contentChars - maxContentChars, this.tail.length)
      remove = completeSurrogateRemoval(this.tail, 0, remove)
      this.tail = this.tail.slice(remove)
      this.contentChars -= remove
    }

    if (this.head > 1024 && this.head * 2 > this.lines.length) {
      this.lines = this.lines.slice(this.head)
      this.head = 0
    }
  }

  get contentLineCount() {
    return this.lines.length - this.head + 1
  }

  get lineCount() {
    return this.truncated ? this.contentLineCount + 1 : this.contentLineCount
  }

  get charCount() {
    return this.contentChars + (this.truncated ? this.marker.length + 1 : 0)
  }

  get text() {
    const activeLines = this.lines.slice(this.head)
    if (activeLines.length > 0 && this.headOffset > 0) {
      activeLines[0] = activeLines[0].slice(this.headOffset)
    }
    activeLines.push(this.tail)
    const content = activeLines.join('\n')
    return this.truncated ? `${this.marker}\n${content}` : content
  }
}

function completeSurrogateRemoval(text, offset, remove) {
  const boundary = offset + remove
  if (remove > 0 && boundary < text.length) {
    const previous = text.charCodeAt(boundary - 1)
    const next = text.charCodeAt(boundary)
    if (previous >= 0xd800 && previous <= 0xdbff && next >= 0xdc00 && next <= 0xdfff) {
      return remove + 1
    }
  }
  return remove
}

export function normalizeTerminalText(text, options) {
  const buffer = new TerminalLogBuffer(options)
  buffer.append(text)
  return buffer.text
}
