export interface TerminalLogBufferOptions {
  maxChars?: number
  maxLines?: number
  marker?: string
}

export const DEFAULT_TRUNCATION_MARKER: string

export class TerminalLogBuffer {
  constructor(options?: TerminalLogBufferOptions)
  clear(): void
  set(text: unknown): void
  append(chunk: unknown): void
  readonly lineCount: number
  readonly charCount: number
  readonly text: string
}

export function normalizeTerminalText(text: unknown, options?: TerminalLogBufferOptions): string
