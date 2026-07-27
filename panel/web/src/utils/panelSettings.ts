export interface PanelSettingsPayload {
  panel_title?: string
  panel_icon?: string
  editor_background_color?: string
  log_background_color?: string
  log_background_image?: string
}

let cachedPanelSettings: PanelSettingsPayload | null = null
let panelSettingsPromise: Promise<PanelSettingsPayload | null> | null = null

export function getCachedPanelSettings() {
  return cachedPanelSettings
}

export function getCachedPanelTitle() {
  return cachedPanelSettings?.panel_title?.trim() || '呆呆面板'
}

export async function loadPanelSettings(options?: { force?: boolean }) {
  if (!options?.force && panelSettingsPromise) {
    return panelSettingsPromise
  }

  const requestPromise = (async () => {
    try {
      const response = await fetch('/api/system/panel-settings', { cache: 'no-store' })
      if (!response.ok) {
        return cachedPanelSettings
      }

      const payload = await response.json() as { data?: PanelSettingsPayload }
      cachedPanelSettings = payload.data || null
      return cachedPanelSettings
    } catch {
      return cachedPanelSettings
    }
  })()

  panelSettingsPromise = requestPromise
  const result = await requestPromise
  if (!cachedPanelSettings) {
    panelSettingsPromise = null
  }
  return result
}
