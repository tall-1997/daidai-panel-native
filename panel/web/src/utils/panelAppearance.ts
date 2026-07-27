import { loadPanelSettings, type PanelSettingsPayload } from './panelSettings'

export interface PanelAppearanceSettings extends PanelSettingsPayload {}

const DEFAULT_LOG_BACKGROUND_COLOR_LIGHT = '#f8fafc'
const DEFAULT_LOG_BACKGROUND_COLOR_DARK = '#0f172a'
const DEFAULT_EDITOR_BACKGROUND_COLOR = '#111827'
const DEFAULT_EDITOR_FOREGROUND_COLOR = '#e5e7eb'
const DEFAULT_LOG_TEXT_COLOR_LIGHT = '#111827'
const DEFAULT_LOG_TEXT_COLOR_DARK = '#e2e8f0'

function toCSSImageValue(image?: string) {
  const trimmed = image?.trim() || ''
  if (!trimmed) {
    return 'none'
  }

  return `url("${trimmed.replace(/"/g, '\\"')}")`
}

function parseColor(color?: string) {
  const text = color?.trim() || ''
  if (!text) return null

  if (text.startsWith('#')) {
    const hex = text.slice(1)
    if (hex.length === 3) {
      const r = Number.parseInt(hex.charAt(0) + hex.charAt(0), 16)
      const g = Number.parseInt(hex.charAt(1) + hex.charAt(1), 16)
      const b = Number.parseInt(hex.charAt(2) + hex.charAt(2), 16)
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
    }
    if (hex.length === 6 || hex.length === 8) {
      const offset = hex.length === 8 ? 2 : 0
      const r = Number.parseInt(hex.slice(offset, offset + 2), 16)
      const g = Number.parseInt(hex.slice(offset + 2, offset + 4), 16)
      const b = Number.parseInt(hex.slice(offset + 4, offset + 6), 16)
      return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
    }
  }

  const match = text.match(/^rgba?\(\s*(\d{1,3})\s*,\s*(\d{1,3})\s*,\s*(\d{1,3})(?:\s*,\s*[0-9.]+\s*)?\)$/i)
  if (!match) {
    return null
  }

  const r = Number.parseInt(match[1] ?? '', 10)
  const g = Number.parseInt(match[2] ?? '', 10)
  const b = Number.parseInt(match[3] ?? '', 10)
  return Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b) ? null : { r, g, b }
}

function getReadableTextColor(background?: string) {
  const rgb = parseColor(background)
  if (!rgb) {
    return DEFAULT_EDITOR_FOREGROUND_COLOR
  }

  const toLinear = (channel: number) => {
    const value = channel / 255
    return value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  }

  const luminance = 0.2126 * toLinear(rgb.r) + 0.7152 * toLinear(rgb.g) + 0.0722 * toLinear(rgb.b)
  return luminance < 0.45 ? DEFAULT_EDITOR_FOREGROUND_COLOR : '#111827'
}

function getDefaultLogBackgroundColor(isDark: boolean) {
  return isDark ? DEFAULT_LOG_BACKGROUND_COLOR_DARK : DEFAULT_LOG_BACKGROUND_COLOR_LIGHT
}

function getDefaultLogTextColor(isDark: boolean) {
  return isDark ? DEFAULT_LOG_TEXT_COLOR_DARK : DEFAULT_LOG_TEXT_COLOR_LIGHT
}

export function applyPanelAppearance(settings?: PanelAppearanceSettings | null) {
  const root = document.documentElement
  const isDark = root.classList.contains('dark')
  const editorBackground = settings?.editor_background_color?.trim() || DEFAULT_EDITOR_BACKGROUND_COLOR
  const logBackground = settings?.log_background_color?.trim() || getDefaultLogBackgroundColor(isDark)
  root.style.setProperty('--dd-editor-bg-color', editorBackground)
  root.style.setProperty('--dd-editor-fg-color', getReadableTextColor(editorBackground))
  root.style.setProperty('--dd-log-bg-color', logBackground)
  root.style.setProperty('--dd-log-text-color', getReadableTextColor(logBackground) || getDefaultLogTextColor(isDark))
  root.style.setProperty('--dd-log-theme-mode', isDark ? 'dark' : 'light')
  root.style.setProperty('--dd-log-bg-image', toCSSImageValue(settings?.log_background_image))
}

export async function fetchAndApplyPanelAppearance() {
  try {
    const settings = await loadPanelSettings()
    applyPanelAppearance(settings || null)
  } catch {
    // ignore startup appearance load failures
  }
}
