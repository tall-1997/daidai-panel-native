const ANSI_COLORS: Record<number, string> = {
  30: '#4d4d4d', 31: '#cd3131', 32: '#0dbc79', 33: '#e5e510',
  34: '#2472c8', 35: '#bc3fbc', 36: '#11a8cd', 37: '#e5e5e5',
  90: '#666666', 91: '#f14c4c', 92: '#23d18b', 93: '#f5f543',
  94: '#3b8eea', 95: '#d670d6', 96: '#29b8db', 97: '#ffffff',
}

const ANSI_BG_COLORS: Record<number, string> = {
  40: '#4d4d4d', 41: '#cd3131', 42: '#0dbc79', 43: '#e5e510',
  44: '#2472c8', 45: '#bc3fbc', 46: '#11a8cd', 47: '#e5e5e5',
  100: '#666666', 101: '#f14c4c', 102: '#23d18b', 103: '#f5f543',
  104: '#3b8eea', 105: '#d670d6', 106: '#29b8db', 107: '#ffffff',
}

const ANSI_256_BASE_COLORS = [
  '#000000', '#800000', '#008000', '#808000', '#000080', '#800080', '#008080', '#c0c0c0',
  '#808080', '#ff0000', '#00ff00', '#ffff00', '#0000ff', '#ff00ff', '#00ffff', '#ffffff',
] as const

function escapeHtml(text: string): string {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function sanitizeLogSegment(text: string): string {
  // Drop unsupported ANSI cursor/control sequences so they do not leak into the UI.
  // eslint-disable-next-line no-control-regex
  return text
    .replace(/\x1b\][^\x07]*(?:\x07|\x1b\\)?/g, '')
    .replace(/\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]/g, '')
}

function ansi256ToHex(code: number): string {
  if (code >= 0 && code <= 15) {
    return ANSI_256_BASE_COLORS[code] ?? ''
  }

  if (code >= 16 && code <= 231) {
    const value = code - 16
    const red = Math.floor(value / 36)
    const green = Math.floor((value % 36) / 6)
    const blue = value % 6
    const toChannel = (n: number) => n === 0 ? 0 : 55 + n * 40
    return rgbToHex(toChannel(red), toChannel(green), toChannel(blue))
  }

  if (code >= 232 && code <= 255) {
    const channel = 8 + (code - 232) * 10
    return rgbToHex(channel, channel, channel)
  }

  return ''
}

function rgbToHex(red: number, green: number, blue: number): string {
  const toHex = (value: number) => Math.max(0, Math.min(255, value)).toString(16).padStart(2, '0')
  return `#${toHex(red)}${toHex(green)}${toHex(blue)}`
}

export function ansiToHtml(text: string): string {
  // eslint-disable-next-line no-control-regex
  const ansiRegex = /\x1b\[([0-9;]*)m/g

  let result = ''
  let lastIndex = 0
  let openSpans = 0
  let fg = ''
  let bg = ''
  let bold = false
  let dim = false
  let italic = false
  let underline = false

  function buildSpan(): string {
    const styles: string[] = []
    if (fg) styles.push(`color:${fg}`)
    if (bg) styles.push(`background-color:${bg}`)
    if (bold) styles.push('font-weight:bold')
    if (dim) styles.push('opacity:0.7')
    if (italic) styles.push('font-style:italic')
    if (underline) styles.push('text-decoration:underline')
    if (styles.length === 0) return ''
    return `<span style="${styles.join(';')}">`
  }

  let match: RegExpExecArray | null
  while ((match = ansiRegex.exec(text)) !== null) {
    const before = text.slice(lastIndex, match.index)
    if (before) result += escapeHtml(sanitizeLogSegment(before))
    lastIndex = match.index + match[0].length

    const codes = match[1]
      ? match[1].split(';').map(Number)
      : [0]

    for (let index = 0; index < codes.length; index++) {
      const code = codes[index] ?? 0
      if (code === 0) {
        fg = ''; bg = ''; bold = false; dim = false; italic = false; underline = false
      } else if (code === 1) {
        bold = true
      } else if (code === 2) {
        dim = true
      } else if (code === 3) {
        italic = true
      } else if (code === 4) {
        underline = true
      } else if (code === 22) {
        bold = false; dim = false
      } else if (code === 23) {
        italic = false
      } else if (code === 24) {
        underline = false
      } else if (code === 39) {
        fg = ''
      } else if (code === 49) {
        bg = ''
      } else if (ANSI_COLORS[code]) {
        fg = ANSI_COLORS[code]
      } else if (ANSI_BG_COLORS[code]) {
        bg = ANSI_BG_COLORS[code]
      } else if ((code === 38 || code === 48) && codes[index + 1] === 5) {
        const color = ansi256ToHex(codes[index + 2] ?? -1)
        if (color && code === 38) fg = color
        if (color && code === 48) bg = color
        index += 2
      } else if ((code === 38 || code === 48) && codes[index + 1] === 2) {
        const red = codes[index + 2]
        const green = codes[index + 3]
        const blue = codes[index + 4]
        if (typeof red === 'number' && typeof green === 'number' && typeof blue === 'number') {
          const color = rgbToHex(red, green, blue)
          if (code === 38) fg = color
          if (code === 48) bg = color
        }
        index += 4
      }
    }

    if (openSpans > 0) {
      result += '</span>'
      openSpans--
    }

    const span = buildSpan()
    if (span) {
      result += span
      openSpans++
    }
  }

  const remaining = text.slice(lastIndex)
  if (remaining) result += escapeHtml(sanitizeLogSegment(remaining))

  while (openSpans > 0) {
    result += '</span>'
    openSpans--
  }

  return result
}

/**
 * Check if text contains ANSI escape sequences.
 * Also matches incomplete sequences like [34m that lost the ESC byte
 * during transport (common in SSE/WebSocket).
 */
export function containsAnsi(text: string): boolean {
  // eslint-disable-next-line no-control-regex
  return /\x1b\[/.test(text) || /\[([0-9;]+)m/.test(text)
}

/**
 * Normalize broken ANSI codes where the ESC (\x1b) byte was stripped,
 * leaving bare sequences like [34m.
 */
export function normalizeAnsi(text: string): string {
  // Replace bare [<digits>m patterns with proper ESC sequences
  // without touching already valid ESC-prefixed sequences.
  // eslint-disable-next-line no-control-regex
  return text.replace(/(^|[^\x1b])\[([0-9;]+)m/g, '$1\x1b[$2m')
}
